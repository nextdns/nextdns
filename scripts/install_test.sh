#!/bin/sh
# Tests for the latest-version lookup in install.sh.
#
# Runs install.sh in a sandbox PATH with curl/wget/openssl wrappers that
# redirect https://api.github.com to a local HTTP(S) server, so the real
# downloaders run against controlled responses. No external network is used.
#
# Usage: scripts/install_test.sh
#   INSTALL_TEST_SH="dash"         shell used to run install.sh (default: sh)
#   INSTALL_TEST_SH="busybox ash"  busybox ash; sed/grep/head/... also come
#                                  from busybox
# Requires: python3, openssl, curl, wget.

set -u

root=$(cd "$(dirname "$0")/.." && pwd)
script=$root/install.sh
tmp=$(mktemp -d)
server_pid=
trap 'test -z "$server_pid" || kill "$server_pid" 2>/dev/null; rm -rf "$tmp"' EXIT INT TERM

# shellcheck disable=SC2086
set -- ${INSTALL_TEST_SH:-sh}
test_sh=$(command -v "$1") || { echo "shell not found: $1"; exit 2; }
shift
test_sh_args="$*"
busybox=
case $(basename "$test_sh") in busybox) busybox=$test_sh ;; esac

real_curl=$(command -v curl)
real_wget=$(command -v wget)
real_openssl=$(command -v openssl)
real_python=$(command -v python3)

# Local server: plain HTTP and HTTPS (self-signed). The response depends on
# the content of $tmp/scenario.
"$real_openssl" req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=api.github.com \
    -keyout "$tmp/key.pem" -out "$tmp/cert.pem" >/dev/null 2>&1
"$real_python" - "$tmp" 2>"$tmp/server.log" <<'PY' &
import http.server, json, ssl, sys, threading
tmp = sys.argv[1]
RELEASE = '{\n  "url": "https://api.github.com/repos/nextdns/nextdns/releases/1",\n  "tag_name": "v1.49.0",\n  "name": "v1.49.0"\n}\n'
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, body, headers=()):
        body = body.encode()
        self.send_response(code)
        for k, v in headers: self.send_header(k, v)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def do_GET(self):
        s = open(tmp + "/scenario").read().strip()
        with open(tmp + "/requests", "a") as f: f.write(self.path + "\n")
        if self.path == "/repositories/1/releases/latest":
            return self.send(200, RELEASE)
        if s == "ok":
            self.send(200, RELEASE, [("Content-Type", "application/json")])
        elif s == "ratelimit":
            self.send(403, '{"message":"API rate limit exceeded for 203.0.113.7.","documentation_url":"https://docs.github.com/rest/using-the-rest-api/rate-limits-for-the-rest-api"}')
        elif s == "unavailable":
            self.send(503, "<html><body>Service Unavailable</body></html>\n")
        elif s == "redirect":
            self.send(301, '{"message":"Moved Permanently","url":"/repositories/1/releases/latest"}',
                      [("Location", "/repositories/1/releases/latest")])
        elif s == "html":
            self.send(200, "<html><body>Login to continue</body></html>\n")
        else:
            self.send(500, "unknown scenario\n")
http_srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
tls_srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain(tmp + "/cert.pem", tmp + "/key.pem")
tls_srv.socket = ctx.wrap_socket(tls_srv.socket, server_side=True)
threading.Thread(target=tls_srv.serve_forever, daemon=True).start()
with open(tmp + "/ports.tmp", "w") as f:
    f.write("%d %d\n" % (http_srv.server_address[1], tls_srv.server_address[1]))
import os; os.rename(tmp + "/ports.tmp", tmp + "/ports")
http_srv.serve_forever()
PY
server_pid=$!
i=0
while [ ! -f "$tmp/ports" ]; do
    i=$((i+1)); [ $i -lt 100 ] || { echo "server did not start"; exit 2; }
    sleep 0.1
done
read -r http_port tls_port < "$tmp/ports"
# A port with nothing listening (connection refused).
closed_port=$("$real_python" -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')

# Build the sandbox PATH with only the tools install.sh needs before the menu.
# $1: list of downloaders to provide (curl, wget, openssl, curl-broken).
make_bin() {
    bin=$tmp/bin
    rm -rf "$bin"; mkdir -p "$bin"
    for t in sed grep head cut tr cat uname dirname; do
        if [ "$busybox" ]; then
            ln -s "$busybox" "$bin/$t"
        else
            ln -s "$(command -v $t)" "$bin/$t"
        fi
    done
    for d in $1; do
        case $d in
        curl|wget)
            real=$real_curl; [ "$d" = wget ] && real=$real_wget
            cat > "$bin/$d" <<SH
#!/bin/sh
echo "$d \$*" >> "$tmp/calls"
for a; do shift
    case \$a in https://api.github.com/*) a=\$BASE/\${a#https://api.github.com/} ;; esac
    set -- "\$@" "\$a"
done
exec "$real" "\$@"
SH
            ;;
        curl-broken)
            cat > "$bin/curl" <<SH
#!/bin/sh
echo "curl \$*" >> "$tmp/calls"
echo "curl: error while loading shared libraries: libcurl.so.4" >&2
exit 127
SH
            ;;
        openssl)
            cat > "$bin/openssl" <<SH
#!/bin/sh
echo "openssl \$*" >> "$tmp/calls"
for a; do shift
    case \$a in api.github.com:443) a=127.0.0.1:\$OPENSSL_PORT ;; esac
    set -- "\$@" "\$a"
done
exec "$real_openssl" "\$@"
SH
            ;;
        esac
        [ ! -f "$bin/${d%-broken}" ] || chmod 755 "$bin/${d%-broken}"
    done
}

pass=0
fail=0
out=
code=

# run <scenario> <downloaders> <base> [VAR=value...]
run() {
    scenario=$1 downloaders=$2 base=$3
    shift 3
    echo "$scenario" > "$tmp/scenario"
    : > "$tmp/calls"
    make_bin "$downloaders"
    # shellcheck disable=SC2086
    out=$(printf 'e\n' | env -i PATH="$tmp/bin" HOME="$tmp" BASE="$base" \
        OPENSSL_PORT="${OPENSSL_PORT:-$tls_port}" \
        FORCE_OS=edgeos FORCE_GOARCH=mipsle_softfloat FORCE_GOOS=linux "$@" \
        "$test_sh" $test_sh_args "$script" 2>&1)
    code=$?
    # Strip colors.
    out=$(printf '%s\n' "$out" | sed 's/\x1b\[[0-9;]*m//g')
}

check() {
    name=$1 want_code=$2
    shift 2
    ok=1
    reason=
    if [ "$code" != "$want_code" ]; then
        ok=; reason="exit $code, want $want_code"
    fi
    for c; do
        case $c in
        !*) case $out in *"${c#!}"*) ok=; reason="${reason:+$reason; }unexpected '${c#!}'" ;; esac ;;
        *) case $out in *"$c"*) ;; *) ok=; reason="${reason:+$reason; }missing '$c'" ;; esac ;;
        esac
    done
    if [ "$ok" ]; then
        pass=$((pass+1)); echo "ok   $name"
    else
        fail=$((fail+1)); echo "FAIL $name: $reason"
        printf '%s\n' "$out" | sed 's/^/     | /'
    fi
}

no_calls() {
    if [ -s "$tmp/calls" ]; then
        code=calls; out="$out
downloader called: $(cat "$tmp/calls")"
    fi
}

h=http://127.0.0.1:$http_port
platform='!Cannot detect running environment'
hint='Set NEXTDNS_VERSION=x.y.z to install a specific version.'

# Successful lookups, one per downloader.
run ok "curl wget openssl" "$h"
check "curl: latest version" 0 "INSTALL_RELEASE: 1.49.0" "i) Install NextDNS"
run ok "wget openssl" "$h"
check "wget (no curl): latest version" 0 "INSTALL_RELEASE: 1.49.0"
run ok "openssl" "$h"
check "openssl (no curl/wget): latest version" 0 "INSTALL_RELEASE: 1.49.0"

# Downloader failures.
run ok "" "$h"
check "no downloader" 1 "ERROR: Cannot retrieve latest version: curl, wget or openssl is required" "$hint" "$platform" "!INFO: OS"
run ok "curl-broken wget" "$h"
check "curl broken" 1 "(curl exit 127): curl: error while loading shared libraries" "$hint" "$platform"
run ok "curl" "http://127.0.0.1:$closed_port"
check "curl: connection refused" 1 "(curl exit 7): curl: (7) Failed to connect" "$hint" "$platform"
run ok "wget" "http://127.0.0.1:$closed_port"
check "wget: connection refused" 1 "Cannot get latest version from api.github.com (wget exit 4)" "!exit 4):" "$hint" "$platform"
OPENSSL_PORT=$closed_port run ok "openssl" "$h"
check "openssl: connection refused" 1 "Cannot get latest version from api.github.com (openssl_get exit 1)" "!exit 1):" "$hint" "$platform"
run ok "curl" "https://127.0.0.1:$tls_port"
check "curl: TLS verification failure" 1 "(curl exit 60): curl: (60) SSL certificate problem" "$hint" "$platform"
run ok "wget" "https://127.0.0.1:$tls_port"
check "wget: TLS verification failure" 1 "Cannot get latest version from api.github.com (wget exit 5)" "!exit 5):" "$hint" "$platform"

# HTTP errors.
run ratelimit "curl" "$h"
check "curl: HTTP 403 rate limit" 1 "(curl exit 22): curl: (22) The requested URL returned error: 403" "$hint" "$platform"
run ratelimit "wget" "$h"
check "wget: HTTP 403 rate limit" 1 "(wget exit 8)" "$hint" "$platform"
run ratelimit "openssl" "$h"
check "openssl: HTTP 403 rate limit" 1 "(openssl_get exit" "): HTTP/1.0 403 Forbidden" "$hint" "$platform"
run unavailable "curl" "$h"
check "curl: HTTP 503" 1 "(curl exit 22): curl: (22) The requested URL returned error: 503" "$hint" "$platform"
run html "curl" "$h"
check "curl: HTTP 200 without tag_name" 1 "(curl exit 0): <html><body>Login to continue</body></html>" "$hint" "$platform"

# Redirects. wget follows redirects, curl (no -L) and openssl_get do not.
run redirect "wget" "$h"
check "wget: API redirect is followed" 0 "INSTALL_RELEASE: 1.49.0"
run redirect "curl" "$h"
check "curl: API redirect is reported" 1 '(curl exit 0): {"message":"Moved Permanently"' "$hint" "$platform"
run redirect "openssl" "$h"
check "openssl: API redirect is reported" 1 "(openssl_get exit" "): HTTP/1.0 301 Moved Permanently" "$hint" "$platform"

# Package metadata: the lookup only uses the GitHub API.
run ok "curl" "$h"
if grep -v -q 'https://api.github.com/repos/nextdns/nextdns/releases/latest' "$tmp/calls"; then
    code=other; out="$out
$(cat "$tmp/calls")"
fi
check "lookup only queries api.github.com" 0

# NEXTDNS_VERSION pinning skips the lookup entirely.
run ok "curl" "$h" NEXTDNS_VERSION=1.48.0
no_calls
check "pin: no lookup" 0 "INSTALL_RELEASE: 1.48.0"
run ok "curl" "http://127.0.0.1:$closed_port" NEXTDNS_VERSION=1.48.0
no_calls
check "pin: with unreachable GitHub" 0 "INSTALL_RELEASE: 1.48.0" "!ERROR"
run ok "" "$h" NEXTDNS_VERSION=1.48.0
check "pin: without any downloader" 0 "INSTALL_RELEASE: 1.48.0" "!ERROR"
run ok "curl" "$h" NEXTDNS_VERSION=master/0123abc
no_calls
check "pin: snapshot" 0 "INSTALL_RELEASE: master/0123abc"
run ok "curl" "$h" NEXTDNS_VERSION=
check "pin: empty NEXTDNS_VERSION does the lookup" 0 "INSTALL_RELEASE: 1.49.0"

# Platform detection failures are still reported as such.
run ok "curl" "$h" FORCE_OS=unknownos
check "platform: unknown bin location" 1 "Unknown bin location for unknownos" "Cannot detect running environment" "!Cannot get latest version"
run ok "curl" "$h" FORCE_GOOS= FORCE_GOARCH=
check "platform: detection works with lookup" 0 "INSTALL_RELEASE: 1.49.0"

echo "$pass passed, $fail failed ($test_sh${test_sh_args:+ $test_sh_args})"
[ "$fail" -eq 0 ]
