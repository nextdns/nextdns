#!/bin/sh

main() {
    OS=$(detect_os)
    GOARCH=$(detect_goarch)
    GOOS=$(detect_goos)

    export NEXTDNS_INSTALLER=1

    log_info "OS: $OS"
    log_info "GOARCH: $GOARCH"
    log_info "GOOS: $GOOS"

    if [ -z "$OS" ] || [ -z "$GOARCH" ] || [ -z "$GOOS" ]; then
        log_error "Cannot detect running environement."
        exit 1
    fi

    NEXTDNS_BIN=$(bin_location)
    LATEST_RELEASE=$(get_release)

    while true; do
        CURRENT_RELEASE=$(get_current_release)
        log_debug "Start install loop with CURRENT_RELEASE=$CURRENT_RELEASE"

        if [ "$CURRENT_RELEASE" ]; then
            if [ "$CURRENT_RELEASE" != "$LATEST_RELEASE" ]; then
                log_debug "NextDNS is out of date ($CURRENT_RELEASE != $LATEST_RELEASE)"
                menu \
                    u "Upgrade NextDNS from $CURRENT_RELEASE to $LATEST_RELEASE" upgrade \
                    c "Configure NextDNS" configure \
                    r "Remove NextDNS" uninstall \
                    e "Exit" exit
            else
                log_debug "NextDNS is up to date ($CURRENT_RELEASE)"
                menu \
                    c "Configure NextDNS" configure \
                    r "Remove NextDNS" uninstall \
                    e "Exit" exit
            fi
        else
            log_debug "NextDNS is not installed"
            menu \
                i "Install NextDNS" install \
                e "Exit" exit
        fi
    done
}

install() {
    if type=$(install_type); then
        log_info "Installing NextDNS..."
        log_debug "Using $type install type"
        "install_$type" &&
            configure
    else
        return $?
    fi
}

upgrade() {
    if type=$(install_type); then
        log_info "Upgrading NextDNS..."
        log_debug "Using $type install type"
        "upgrade_$type"
    else
        return $?
    fi
}

uninstall() {
    if type=$(install_type); then
        log_info "Uninstalling NextDNS..."
        log_debug "Using $type uninstall type"
        "uninstall_$type"
    else
        return $?
    fi
}

configure() {
    log_debug "Start configure"
    args=""
    add_arg() {
        log_debug "Add arg -$1=$2"
        args="$args -$1=$2"
    }
    add_arg_bool_ask() {
        arg=$1
        msg=$2
        default=$3
        if [ -z "$default" ]; then
            default=$(get_config_bool "$arg")
        fi
        # shellcheck disable=SC2046
        add_arg "$arg" $(ask_bool "$msg" "$default")
    }
    add_arg config "$(get_config_id)"
    add_arg_bool_ask report-client-info 'Report device name?' true
    add_arg_bool_ask hardened-privacy 'Enable hardened privacy mode (may increase latency)?'
    case $(guess_host_type) in
    workstation)
        add_arg_bool_ask detect-captive-portals 'Detect captive portals?' true
        ;;
    router)
        add_arg setup-router true 
        ;;
    unsure)
        case $(ask_bool 'Setup as a router?') in
            true)
                add_arg setup-router true 
                ;;
            false)
                add_arg_bool_ask detect-captive-portals 'Detect captive portals?'
                ;;
        esac
        ;;
    esac
    add_arg_bool_ask auto-activate 'Automatically configure host DNS on daemon startup?' true
    # shellcheck disable=SC2086
    asroot "$NEXTDNS_BIN" install $args
}

install_bin() {
    bin_path=$NEXTDNS_BIN
    if [ "$1" ]; then
        bin_path=$1
    fi
    log_debug "Installing $LATEST_RELEASE binary for $GOOS/$GOARCH to $bin_path"
    url="https://github.com/nextdns/nextdns/releases/download/v${LATEST_RELEASE}/nextdns_${LATEST_RELEASE}_${GOOS}_${GOARCH}.tar.gz"
    log_debug "Downloading $url"
    mkdir -p "$(dirname "$bin_path")" &&
        curl -sL "$url" | asroot sh -c "tar Ozxf - nextdns > \"$bin_path\"" &&
        asroot chmod 755 "$bin_path"
}

upgrade_bin() {
    tmp=$NEXTDNS_BIN.tmp
    if install_bin "$tmp"; then
        asroot "$NEXTDNS_BIN" uninstall
        mv "$tmp" "$NEXTDNS_BIN"
        asroot "$NEXTDNS_BIN" install
    fi
    rm -rf "$tmp"
}

uninstall_bin() {
    asroot "$NEXTDNS_BIN" uninstall
    asroot rm -f "$NEXTDNS_BIN"
}

install_rpm() {
    sudo curl -s https://nextdns.io/yum.repo -o /etc/yum.repos.d/nextdns.repo &&
        sudo yum install -y nextdns
}

upgrade_rpm() {
    sudo yum upgrade -y nextdns
}

uninstall_rpm() {
    sudo yum uninstall -y nextdns
}

install_deb() {
    # Fallback on curl, some debian based distrib don't have wget while debian
    # doesn't have curl by default.
    ( wget -qO - https://nextdns.io/repo.gpg || curl -sfL https://nextdns.io/repo.gpg ) | sudo apt-key add - &&
        sudo sh -c 'echo "deb https://nextdns.io/repo/deb stable main" > /etc/apt/sources.list.d/nextdns.list' &&
        (test "$OS" = "debian" && sudo apt install apt-transport-https || true) &&
        sudo apt update &&
        sudo apt install -y nextdns
}

upgrade_deb() {
    sudo apt remove -y nextdns
}

uninstall_deb() {
    log_debug "Uninstalling deb"
    sudo apt upgrade -y nextdns
}

install_arch() {
    sudo pacman -Sy yay &&
        yay -Sy nextdns
}

upgrade_arch() {
    yay -Suy nextdns
}

uninstall_arch() {
    sudo pacman -R nextdns
}

install_merlin_path() {
    # Add next to Merlin's path
    mkdir -p /tmp/opt/sbin
    ln -sf "$NEXTDNS_BIN" /tmp/opt/sbin/nextdns
}

install_merlin() {
    if install_bin; then
        install_merlin_path
    fi
}

uninstall_merlin() {
    uninstall_bin
    rm -f /tmp/opt/sbin/nextdns
}

upgrade_merlin() {
    if upgrade_bin; then
        install_merlin_path
    fi
}

install_openwrt() {
    opkg update &&
        opkg install nextdns
    rt=$?
    if [ $rt -eq 0 ]; then
        case $(ask_bool 'Install the GUI?' true) in
        true)
            opkg install luci-app-nextdns
            rt=$?
            ;;
        esac
    fi
    return $rt
}

upgrade_openwrt() {
    opkg update &&
        opkg upgrade nextdns
}

uninstall_openwrt() {
    opkg remove nextdns
}

install_ddwrt() {
    if [ "$(nvram get enable_jffs2)" = "0" ]; then
        log_error "JFFS support not enabled"
        log_info "To enabled JFFS:"
        log_info " 1. On the router web page click on Administration."
        log_info " 2. Scroll down until you see JFFS2 Support section."
        log_info " 3. Click Enable JFFS."
        log_info " 4. Click Save."
        log_info " 5. Wait couple seconds, then click Apply."
        log_info " 6. Wait again. Go back to the Enable JFFS section, and enable Clean JFFS."
        log_info " 7. Do not click Save. Click Apply instead."
        log_info " 8. Wait till you get the web-GUI back, then disable Clean JFFS again."
        log_info " 9. Click Save."
        log_info "10. Relaunch this installer."
        exit 1
    fi
    mkdir -p /jffs/nextdns &&
        openssl_get https://curl.haxx.se/ca/cacert.pem | http_body > /jffs/nextdns/ca.pem &&
        install_bin
}

upgrade_ddwrt() {
    upgrade_bin
}

uninstall_ddwrt() {
    uninstall_bin
    rm -rf /jffs/nextdns
}

install_brew() {
    silent_exec brew install nextdns/tap/nextdns
}

upgrade_brew() {
    silent_exec brew upgrade nextdns/tap/nextdns
    sudo "$NEXTDNS_BIN" install
}

uninstall_brew() {
    silent_exec brew uninstall nextdns/tap/nextdns
}

install_freebsd() {
    # TODO: port install
    install_bin
}

upgrade_freebsd() {
    # TODO: port upgrade
    upgrade_bin
}

uninstall_freebsd() {
    # TODO: port uninstall
    uninstall_bin
}

install_pfsense() {
    # TODO: port install + UI
    install_bin
}

upgrade_pfsense() {
    # TODO: port upgrade
    upgrade_bin
}

uninstall_pfsense() {
    # TODO: port uninstall
    uninstall_bin
}

install_type() {
    case $OS in
    centos|fedora|rhel)
        echo "rpm"
        ;;
    debian|ubuntu|raspbian)
        echo "deb"
        ;;
    arch|manjaro)
        #echo "arch" # TODO: fix AUR install
        echo "bin"
        ;;
    openwrt)
        # shellcheck disable=SC1091
        . /etc/os-release
        major=$(echo "$VERSION_ID" | cut -d. -f1)
        if [ "$major" -lt 19 ] || [ "$VERSION_ID" = "19.07.0-rc1" ]; then
            # No opkg support before 19.07.0-rc2
            echo "bin"
        else
            echo "openwrt"
        fi
        ;;
    asuswrt-merlin)
        echo "merlin"
        ;;
    edgeos|synology)
        echo "bin"
        ;;
    ddwrt)
        echo "ddwrt"
        ;;
    darwin)
        if [ -x /usr/local/bin/brew ]; then
            echo "brew"
        else
            log_debug "Homebrew not installed, fallback on binary install"
            echo "bin"
        fi
        ;;
    freebsd)
        echo "freebsd"
        ;;
    pfsense)
        echo "pfsense"
        ;;
    *)
        log_error "Unsupported installation for $(detect_os)"
        return 1
        ;;
    esac
}

get_config() {
    "$NEXTDNS_BIN" config | grep -E "^$1 " | cut -d' ' -f 2
}

get_config_bool() {
    val=$(get_config "$1")
    case $val in
        true|false)
            echo "$val"
            ;;
    esac
    echo "$2"
}

get_config_id() {
    log_debug "Get configuration ID"
    while [ -z "$CONFIG_ID" ]; do
        default=
        prev_id=$(get_config config)
        if [ "$prev_id" ]; then
            log_debug "Previous config ID: $prev_id"
            default=" (default=$prev_id)"
        fi
        print "NextDNS Configuration ID%s: " "$default"
        read -r id
        if [ -z "$id" ]; then
            id=$prev_id
        fi
        if echo "$id" | grep -qE '^[0-9a-f]{6}$'; then
            CONFIG_ID=$id
            break
        else
            echo "Invalid configuration ID."
            echo
            echo "ID format is 6 alphanumerical lowercase characters (example: 123abc)."
            echo "Your ID can be found on the Setup tab of https://my.nextdns.io."
            echo
        fi
    done
    echo "$CONFIG_ID"
}

log_debug() {
    if [ "$DEBUG" = "1" ]; then
        printf "\033[30;1mDEBUG: %s\033[0m\n" "$*" >&2
    fi
}

log_info() {
    printf "INFO: %s\n" "$*" >&2
}

log_error() {
    printf "\033[31mERROR: %s\033[0m\n" "$*" >&2
}

print() {
    # shellcheck disable=SC2059
    printf "$@" >&2
}

menu() {
    while true; do
        n=0
        default=
        for item in "$@"; do
            case $((n%3)) in
            0)
                key=$item
                if [ -z "$default" ]; then
                    default=$key
                fi
                ;;
            1)
                echo "$key) $item"
                ;;
            esac
            n=$((n+1))
        done
        print "Choice (default=%s): " "$default"
        read -r choice
        if [ -z "$choice" ]; then
            choice=$default
        fi
        n=0
        for item in "$@"; do
            case $((n%3)) in
            0)
                key=$item
                ;;
            2)
                if [ "$key" = "$choice" ]; then
                    if ! "$item"; then
                        log_error "$item: exit $?"
                    fi
                    break 2
                fi
                ;;
            esac
            n=$((n+1))
        done
        echo "Invalid choice"
    done
}

ask_bool() {
    msg=$1
    default=$2
    case $default in
    true)
        msg="$msg [Y|n]: "
        ;;
    false)
        msg="$msg [y|N]: "
        ;;
    *)
        msg="$msg (y/n): "
    esac
    while true; do
        print "%s" "$msg"
        read -r answer
        if [ -z "$answer" ]; then
            answer=$default
        fi
        case $answer in
        y|Y|yes|YES|true)
            echo "true"
            return 0
            ;;
        n|N|no|NO|false)
            echo "false"
            return 0
            ;;
        *)
            echo "Invalid input, use yes or no"
            ;;
        esac
    done
}

detect_endiannes() {
    if ! hexdump /dev/null 2>/dev/null; then
        # Some firmware do not contain hexdump, for those, try to detect endiannes
        # differently
        case $(cat /proc/cpuinfo) in
        *BCM5300*)
            # RT-AC66U does not support merlin version over 380.70 which
            # lack hexdump command.
            echo "le"
            ;;
        *)
            log_error "Cannot determine endiannes"
            return 1
            ;;
        esac
        return 0
    fi
    case $(hexdump -s 5 -n 1 -e '"%x"' /bin/sh | head -c1) in
    1)
        echo "le"
        ;;
    2)
        echo ""
        ;;
    esac
}

detect_goarch() {
    if [ "$FORCE_GOARCH" ]; then
        echo "$FORCE_GOARCH"; return 0
    fi
    case $(uname -m) in
    x86_64|amd64)
        echo "amd64"
        ;;
    i386|i686)
        echo "386"
        ;;
    arm)
        # Freebsd does not include arm version
        case "$(sysctl -b hw.model 2>/dev/null)" in
        *A9*)
            echo "armv7"
            ;;
        *)
            # Unknown version, fallback to the lowest
            echo "armv5"
            ;;
        esac
        ;;
    armv5*)
        echo "armv5"
        ;;
    armv6*|armv7*|armv8*)
        if grep -q vfp /proc/cpuinfo 2>/dev/null; then
            echo "armv$(uname -m | sed -e 's/[[:alpha:]]//g')"
        else
            # Soft floating point
            echo "armv5"
        fi
        ;;
    aarch64)
        case "$(uname -o 2>/dev/null)" in
        ASUSWRT-Merlin*)
            # XXX when using arm64 build on ASUS AC66U and ACG86U, we get Go error:
            # "out of memory allocating heap arena metadata".
            echo "armv7"
            ;;
        *)
            echo "arm64"
            ;;
        esac
        ;;
    arm64)
        echo "arm64"
        ;;
    mips*)
        echo "$(uname -m)$(detect_endiannes)"
        ;;
    *)
        log_error "Unsupported GOARCH: $(uname -m)"
        return 1
        ;;
    esac
}

detect_goos() {
    if [ "$FORCE_GOOS" ]; then
        echo "$FORCE_GOOS"; return 0
    fi
    case $(uname -s) in
    Linux)
        echo "linux"
        ;;
    Darwin)
        echo "darwin"
        ;;
    FreeBSD)
        echo "freebsd"
        ;;
    NetBSD)
        echo "netbsd"
        ;;
    *)
        log_error "Unsupported GOOS: $(uname -s)"
        return 1
    esac
}

detect_os() {
    if [ "$FORCE_OS" ]; then
        echo "$FORCE_OS"; return 0
    fi
    case $(uname -s) in
    Linux)
        case $(uname -o) in
        GNU/Linux)
            if grep -q '^EdgeRouter' /etc/version 2> /dev/null; then
                echo "edgeos"; return 0
            fi
            if uname -u 2>/dev/null | grep -q '^synology'; then
                echo "synology"; return 0
            fi
            # shellcheck disable=SC1091
            dist=$(. /etc/os-release; echo "$ID")
            case $dist in
            debian|ubuntu|raspbian|centos|fedora|rhel|arch|manjaro|openwrt)
                echo "$dist"; return 0
                ;;
            esac
            ;;
        ASUSWRT-Merlin*)
            echo "asuswrt-merlin"; return 0
            ;;
        DD-WRT)
            echo "ddwrt"; return 0
        esac
        ;;
    Darwin)
        echo "darwin"; return 0
        ;;
    FreeBSD)
        if [ -f /etc/platform ]; then
            case $(cat /etc/platform) in
            pfSense)
                echo "pfsense"; return 0
                ;;
            esac
        fi
        echo "freebsd"; return 0
        ;;
    NetBSD)
        echo "netbsd"; return 0
        ;;
    *)
    esac
    log_error "Unsupported OS: $(uname -s)"
    return 1
}

guess_host_type() {
    case $OS in
        pfsense|openwrt|asuswrt-merlin|edgeos|ddwrt|synology)
            echo "router"
            ;;
        darwin)
            echo "workstation"
            ;;
        *)
            echo "unsure"
            ;;
    esac
}

asroot() {
    # Some platform (merlin) do not have the "id" command and $USER report a non root username with uid 0.
    if [ "$(grep '^Uid:' /proc/$$/status 2>/dev/null|cut -f2)" = "0" ] || [ "$USER" = "root" ] || [ "$(id -u 2>/dev/null)" = "0" ]; then
        "$@"
    elif [ "$(command -v sudo 2>/dev/null)" ]; then 
        sudo "$@"
    else
        echo "Root required"
        su -m root -c "$*"
    fi
}

silent_exec() {
    if [ "$DEBUG" = 1 ]; then
        "$@"
    else
        if ! out=$("$@" 2>&1); then
            rt=$?
            printf "\033[30;1m%s\033[0m\n" "$out"
            return $rt
        fi
    fi
}

bin_location() {
    case $OS in
    centos|fedora|rhel|debian|ubuntu|raspbian|arch|manjaro|openwrt)
        echo "/usr/bin/nextdns"
        ;;
    darwin|synology)
        echo "/usr/local/bin/nextdns"
        ;;
    asuswrt-merlin|ddwrt)
        echo "/jffs/nextdns/nextdns"
        ;;
    freebsd|pfsense)
        echo "/usr/local/sbin/nextdns"
        ;;
    edgeos)
        echo "/config/nextdns/nextdns"
        ;;
    *)
        log_error "Unknown bin location for $OS"
        ;;
    esac
}

get_current_release() {
    if [ -x "$NEXTDNS_BIN" ]; then
        $NEXTDNS_BIN version|cut -d' ' -f 3
    fi
}

get_release() {
    if [ "$NEXTDNS_VERSION" ]; then
        echo "$NEXTDNS_VERSION"
    else
        curl="curl -s"
        if [ -z "$(command -v curl >/dev/null 2>&1)" ]; then
            curl="openssl_get"
        fi
        $curl "https://api.github.com/repos/nextdns/nextdns/releases/latest" |
            grep '"tag_name":' |
            sed -E 's/.*"([^"]+)".*/\1/' |
            sed -e 's/^v//'
    fi
}

http_redirect() {
    while read -r header; do
        case $header in
            Location:*)
                echo "${header#Location: }"
                return
            ;;
        esac
        if [ "$header" = "" ]; then
            break
        fi
    done
    cat > /dev/null
    return 1
}

http_body() {
    sed -n '/^\r/,$p' | sed 1d
}

openssl_get() {
    host=${1#https://*} # https://dom.com/path -> dom.com/path
    path=/${host#*/}    # dom.com/path -> /path
    host=${host%$path}  # dom.com/path -> dom.com
    printf "GET %s HTTP/1.0\nHost: %s\nUser-Agent: curl\n\n" "$path" "$host" |
        openssl s_client -quiet -connect "$host:443" 2>/dev/null
}

main
