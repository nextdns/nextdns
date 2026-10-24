#!/bin/sh
# Validate the actual UCI/rpcd API in an isolated OpenWrt 24.10.7 container.
set -eu
cd "$(dirname "$0")/../../.."
case "$(docker info --format '{{.Architecture}}')" in
    aarch64|arm64)
        arch=arm64
        platform=linux/aarch64_generic
        image=openwrt/rootfs@sha256:45e4f7bb8d199e7e078ab48e01b9952362ad8b8190df946ecae8e1dd645ef22b
        ;;
    x86_64|amd64)
        arch=amd64
        platform=linux/amd64
        image=openwrt/rootfs@sha256:75ed3fb21d9fbc881d2ac16af0dca7bc1197047a3d1799258f36082376e81053
        ;;
    *) echo 'Unsupported Docker architecture' >&2; exit 1 ;;
esac
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go test -c -o "$scratch/openwrt.test" ./router/openwrt
docker run --rm --network none --platform "$platform" \
    --mount "type=bind,src=$scratch,dst=/test,readonly" \
    --env NEXTDNS_TEST_OPENWRT_RELAY=1 --entrypoint /bin/sh "$image" -ec '
    mkdir -p /var/run /var/lock /tmp/.uci
    touch /tmp/nextdns-relay-test-container
    ubusd &
    ready=0
    for n in 1 2 3 4 5; do
        if ubus list >/dev/null 2>&1; then ready=1; break; fi
        sleep 1
    done
    [ "$ready" = 1 ] || exit 1
    rpcd &
    ready=0
    for n in 1 2 3 4 5; do
        if ubus list uci 2>/dev/null | grep -q "^uci$"; then ready=1; break; fi
        sleep 1
    done
    [ "$ready" = 1 ] || exit 1
    /test/openwrt.test -test.v -test.run "^TestRelayOpenWrtUCI$"
    '
