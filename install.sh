#!/bin/sh

main() {
    OS=$(detect_os)
    GOARCH=$(detect_goarch)
    GOOS=$(detect_goos)
    NEXTDNS_BIN=$(bin_location)
    INSTALL_RELEASE=$(get_release)

    export NEXTDNS_INSTALLER=1

    log_info "OS: $OS"
    log_info "GOARCH: $GOARCH"
    log_info "GOOS: $GOOS"
    log_info "NEXTDNS_BIN: $NEXTDNS_BIN"
    log_info "INSTALL_RELEASE: $INSTALL_RELEASE"

    if [ -z "$OS" ] || [ -z "$GOARCH" ] || [ -z "$GOOS" ] || [ -z "$NEXTDNS_BIN" ] || [ -z "$INSTALL_RELEASE" ]; then
        log_error "Cannot detect running environment."
        exit 1
    fi

    case "$RUN_COMMAND" in
    install|upgrade|uninstall|configure) "$RUN_COMMAND"; exit ;;
    esac

    while true; do
        CURRENT_RELEASE=$(get_current_release)
        log_debug "Start install loop with CURRENT_RELEASE=$CURRENT_RELEASE"

        if [ "$CURRENT_RELEASE" ]; then
            if ! is_version_current; then
                log_debug "NextDNS is out of date ($CURRENT_RELEASE != $INSTALL_RELEASE)"
                menu \
                    u "Upgrade NextDNS from $CURRENT_RELEASE to $INSTALL_RELEASE" upgrade \
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
    if [ "$(get_current_release)" ]; then
        log_info "Already installed"
        return
    fi
    if type=$(install_type); then
        log_info "Installing NextDNS..."
        log_debug "Using $type install type"
        if "install_$type"; then
            if [ ! -x "$NEXTDNS_BIN" ]; then
                log_error "Installation failed: binary not installed in $NEXTDNS_BIN"
                return 1
            fi
            configure
            post_install
            exit 0
        fi
    else
        return $?
    fi
}

upgrade() {
    if [ "$(get_current_release)" = "$INSTALL_RELEASE" ]; then
        log_info "Already on the latest version"
        return
    fi
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
        for value in $2; do
            log_debug "Add arg -$1=$value"
            args="$args -$1=$value"
        done
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

    doc "Sending your devices name lets you filter analytics and logs by device."
    add_arg_bool_ask report-client-info 'Report device name?' true

    case $(guess_host_type) in
    router)
        add_arg setup-router true
        ;;
    unsure)
        doc "Accept DNS request from other network hosts."
        if [ "$(get_config_bool setup-router)" = "true" ]; then
            router_default=true
        fi
        if [ "$(ask_bool 'Setup as a router?' $router_default)" = "true" ]; then
            add_arg setup-router true
        fi
        ;;
    esac

    doc "Make nextdns CLI cache responses. This improves latency and reduces the amount"
    doc "of queries sent to NextDNS."
    if [ "$(guess_host_type)" = "router" ]; then
        doc "Note that enabling this feature will disable dnsmasq for DNS to avoid double"
        doc "caching."
    fi
    if [ "$(get_config cache-size)" != "0" ]; then
        cache_default=true
    fi
    if [ "$(ask_bool 'Enable caching?' $cache_default)" = "true" ]; then
        add_arg cache-size "10MB"

        doc "Instant refresh will force low TTL on responses sent to clients so they rely"
        doc "on CLI DNS cache. This will allow changes on your NextDNS config to be applied"
        doc "on you LAN hosts without having to wait for their cache to expire."
        if [ "$(get_config max-ttl)" = "5s" ]; then
            instant_refresh_default=true
        fi
        if [ "$(ask_bool 'Enable instant refresh?' $instant_refresh_default)" = "true" ]; then
            add_arg max-ttl "5s"
        fi
    fi

    if [ "$(guess_host_type)" != "router" ]; then
        doc "Changes DNS settings of the host automatically when nextdns is started."
        doc "If you say no here, you will have to manually configure DNS to 127.0.0.1."
        add_arg_bool_ask auto-activate 'Automatically setup local host DNS?' true
    fi
    # shellcheck disable=SC2086
    asroot "$NEXTDNS_BIN" install $args
}

post_install() {
    println
    println "Congratulations! NextDNS is now installed."
    println
    println "To upgrade/uninstall, run this command again and select the approriate option."
    println
    println "You can use the nextdns command to control the daemon."
    println "Here is a few important commands to know:"
    println
    println "# Start, stop, restart the daemon:"
    println "nextdns start"
    println "nextdns stop"
    println "nextdns restart"
    println
    println "# Configure the local host to point to NextDNS or not:"
    println "nextdns activate"
    println "nextdns deactivate"
    println
    println "# Explore daemon logs:"
    println "nextdns log"
    println
    println "# For more commands, use:"
    println "nextdns help"
    println
}

install_bin() {
    bin_path=$NEXTDNS_BIN
    if [ "$1" ]; then
        bin_path=$1
    fi
    log_debug "Installing $INSTALL_RELEASE binary for $GOOS/$GOARCH to $bin_path"
    case "$INSTALL_RELEASE" in
    */*)
        # Snapshot
        branch=${INSTALL_RELEASE%/*}
        hash=${INSTALL_RELEASE#*/}
        url="https://snapshot.nextdns.io/${branch}/nextdns-${hash}_${GOOS}_${GOARCH}.tar.gz"
        ;;
    *)
        url="https://github.com/nextdns/nextdns/releases/download/v${INSTALL_RELEASE}/nextdns_${INSTALL_RELEASE}_${GOOS}_${GOARCH}.tar.gz"
        ;;
    esac
    log_debug "Downloading $url"
    asroot mkdir -p "$(dirname "$bin_path")" &&
        curl -sL "$url" | asroot sh -c "tar Ozxf - nextdns > \"$bin_path\"" &&
        asroot chmod 755 "$bin_path"
}

upgrade_bin() {
    tmp=$NEXTDNS_BIN.tmp
    if install_bin "$tmp"; then
        asroot "$NEXTDNS_BIN" uninstall
        asroot mv "$tmp" "$NEXTDNS_BIN"
        asroot "$NEXTDNS_BIN" install
    fi
    log_debug "Removing spurious temporary install file"
    asroot rm -rf "$tmp"
}

uninstall_bin() {
    asroot "$NEXTDNS_BIN" uninstall
    asroot rm -f "$NEXTDNS_BIN"
}

install_rpm() {
    asroot curl -Ls https://repo.nextdns.io/nextdns.repo -o /etc/yum.repos.d/nextdns.repo &&
        asroot yum install -y nextdns
}

upgrade_rpm() {
    asroot yum update -y nextdns
}

uninstall_rpm() {
    asroot yum remove -y nextdns
}

install_zypper() {
    if asroot zypper repos | grep -q nextdns >/dev/null; then
        echo "Repository nextdns already exists. Skipping adding repository..."
    else
        asroot zypper ar -f -r https://repo.nextdns.io/nextdns.repo nextdns
    fi
    asroot zypper refresh && asroot zypper in -y nextdns
}

upgrade_zypper() {
    asroot zypper up nextdns
}

uninstall_zypper() {
    asroot zypper remove -y nextdns
    case $(ask_bool 'Do you want to remove the repository from the repositories list?' true) in
            true)
                asroot zypper removerepo nextdns
                ;;
        esac
}

install_deb() {
    # Fallback on curl, some debian based distrib don't have wget while debian
    # doesn't have curl by default.
    ( asroot wget -qO /usr/share/keyrings/nextdns.gpg https://repo.nextdns.io/nextdns.gpg ||
      asroot curl -sfL https://repo.nextdns.io/nextdns.gpg -o /usr/share/keyrings/nextdns.gpg ) &&
        asroot sh -c 'echo "deb [signed-by=/usr/share/keyrings/nextdns.gpg] https://repo.nextdns.io/deb stable main" > /etc/apt/sources.list.d/nextdns.list' &&
        (dpkg --compare-versions $(dpkg-query --showformat='${Version}' --show apt) ge 1.1 ||
         asroot ln -s /usr/share/keyrings/nextdns.gpg /etc/apt/trusted.gpg.d/.) &&
        (test "$OS" = "debian" && asroot apt-get -y install apt-transport-https || true) &&
        asroot apt-get update &&
        asroot apt-get install -y nextdns
}

upgrade_deb() {
    asroot apt-get update &&
        asroot apt-get install -y nextdns
}

uninstall_deb() {
    asroot apt-get remove -y nextdns
}

install_apk() {
    repo=https://repo.nextdns.io/apk
    asroot wget -O /etc/apk/keys/nextdns.pub https://repo.nextdns.io/nextdns.pub &&
        (grep -v $repo /etc/apk/repositories; echo $repo) | asroot tee /etc/apk/repositories >/dev/null &&
        asroot apk update &&
        asroot apk add nextdns
}

upgrade_apk() {
    asroot apk update && asroot apk upgrade nextdns
}

uninstall_apk() {
    asroot apk del nextdns
}

install_arch() {
    asroot pacman -Sy yay &&
        yay -Sy nextdns
}

upgrade_arch() {
    yay -Suy nextdns
}

uninstall_arch() {
    asroot pacman -R nextdns
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
    asroot "$NEXTDNS_BIN" install
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

install_opnsense() {
    # TODO: port install + UI
    install_bin
}

upgrade_opnsense() {
    # TODO: port upgrade
    upgrade_bin
}

uninstall_opnsense() {
    # TODO: port uninstall
    uninstall_bin
}

ubios_install_source() {
    echo "deb [signed-by=/usr/share/keyrings/nextdns.gpg] https://repo.nextdns.io/deb stable main" > /tmp/nextdns.list
    podman cp /tmp/nextdns.list unifi-os:/etc/apt/sources.list.d/nextdns.list
    rm -f /tmp/nextdns.list
    podman exec unifi-os apt-get install -y gnupg1 curl
    podman exec unifi-os curl -sfL https://repo.nextdns.io/nextdns.gpg -o /usr/share/keyrings/nextdns.gpg
    podman exec unifi-os apt-get update -o Dir::Etc::sourcelist="sources.list.d/nextdns.list" -o Dir::Etc::sourceparts="-" -o APT::Get::List-Cleanup="0"
}

install_ubios() {
    ubios_install_source
    podman exec unifi-os apt-get install -y nextdns
}

upgrade_ubios() {
    ubios_install_source
    podman exec unifi-os apt-get install --only-upgrade -y nextdns
}

uninstall_ubios() {
    podman exec unifi-os apt-get remove -y nextdns
}

install_type() {
    if [ "$FORCE_INSTALL_TYPE" ]; then
        echo "$FORCE_INSTALL_TYPE"; return 0
    fi
    case "$INSTALL_RELEASE" in
    */*)
        # Snapshot mode always use binary install
        echo "bin"; return 0
        ;;
    esac
    case $OS in
    centos|fedora|rhel)
        echo "rpm"
        ;;
    opensuse-tumbleweed|opensuse-leap|opensuse)
        echo "zypper"
        ;;
    debian|ubuntu|elementary|raspbian|linuxmint|pop|neon|sparky|vyos)
        echo "deb"
        ;;
    alpine)
        echo "apk"
        ;;
    arch|manjaro)
        #echo "arch" # TODO: fix AUR install
        echo "bin"
        ;;
    openwrt)
        # shellcheck disable=SC1091
        . /etc/os-release
        major=$(echo "$VERSION_ID" | cut -d. -f1)
        case $major in
            *[!0-9]*)
                if [ "$VERSION_ID" = "19.07.0-rc1" ]; then
                    # No opkg support before 19.07.0-rc2
                    echo "bin"
                else
                    # Likely 'snapshot' bulid in this case, but still > major version 19
                    echo "openwrt"
                fi
                ;;
            *)
                if [ "$major" -lt 19 ]; then
                    # No opkg support before 19.07.0-rc2
                    echo "bin"
                else
                    echo "openwrt"
                fi
                ;;
        esac
        ;;
    asuswrt-merlin)
        echo "merlin"
        ;;
    edgeos|synology|clear-linux-os|solus|openbsd|netbsd|overthebox)
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
    opnsense)
        echo "opnsense"
        ;;
    ubios)
        echo "ubios"
        ;;
    void)
        # TODO: pkg for xbps
        echo "bin"
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
            log_error "Invalid configuration ID."
            println
            println "ID format is 6 alphanumerical lowercase characters (example: 123abc)."
            println "Your ID can be found on the Setup tab of https://my.nextdns.io."
            println
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
    format=$1
    if [ $# -gt 0 ]; then
        shift
    fi
    # shellcheck disable=SC2059
    printf "$format" "$@" >&2
}

println() {
    format=$1
    if [ $# -gt 0 ]; then
        shift
    fi
    # shellcheck disable=SC2059
    printf "$format\n" "$@" >&2
}

doc() {
    # shellcheck disable=SC2059
    printf "\033[30;1m%s\033[0m\n" "$*" >&2
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
    armv6*|armv7*)
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
    armv8*|arm64)
        echo "arm64"
        ;;
    mips*)
        # TODO: detect hardfloat
        echo "$(uname -m)$(detect_endiannes)_softfloat"
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
    OpenBSD)
        echo "openbsd"
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
        GNU/Linux|Linux)
            if grep -q -e '^EdgeRouter' -e '^UniFiSecurityGateway' /etc/version 2> /dev/null; then
                echo "edgeos"; return 0
            fi
            if uname -u 2>/dev/null | grep -q '^synology'; then
                echo "synology"; return 0
            fi
            # shellcheck disable=SC1091
            dist=$(. /etc/os-release; echo "$ID")
            case $dist in
            ubios)
                if [ -z "$(command -v podman)" ]; then
                    log_error "This version of UnifiOS is not supported. Make sure you run version 1.7.0 or above."
                    return 1
                fi
                echo "$dist"; return 0
                ;;
            debian|ubuntu|elementary|raspbian|centos|fedora|rhel|arch|manjaro|openwrt|clear-linux-os|linuxmint|opensuse-tumbleweed|opensuse-leap|opensuse|solus|pop|neon|overthebox|sparky|vyos|void|alpine)
                echo "$dist"; return 0
                ;;
            esac
            # shellcheck disable=SC1091
            for dist in $(. /etc/os-release; echo "$ID_LIKE"); do
                case $dist in
                debian|ubuntu|rhel|fedora|openwrt)
                    log_debug "Using ID_LIKE"
                    echo "$dist"; return 0
                    ;;
                esac
            done
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
        if [ -x /usr/local/sbin/opnsense-version ]; then
            case $(/usr/local/sbin/opnsense-version -N) in
            OPNsense)
                echo "opnsense"; return 0
                ;;
            esac
        fi
        echo "freebsd"; return 0
        ;;
    NetBSD)
        echo "netbsd"; return 0
        ;;
    OpenBSD)
        echo "openbsd"; return 0
        ;;
    *)
    esac
    log_error "Unsupported OS: $(uname -o) $(grep ID "/etc/os-release" 2>/dev/null | xargs)"
    return 1
}

guess_host_type() {
    case $OS in
    pfsense|opnsense|openwrt|asuswrt-merlin|edgeos|ddwrt|synology|overthebox|ubios)
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
            println "\033[30;1m%s\033[0m" "$out"
            return $rt
        fi
    fi
}

bin_location() {
    case $OS in
    centos|fedora|rhel|debian|ubuntu|elementary|raspbian|arch|manjaro|clear-linux-os|linuxmint|opensuse-tumbleweed|opensuse-leap|opensuse|solus|pop|neon|sparky|vyos|void|alpine)
        echo "/usr/bin/nextdns"
        ;;
    openwrt|overthebox)
        echo "/usr/sbin/nextdns"
        ;;
    darwin|synology)
        echo "/usr/local/bin/nextdns"
        ;;
    asuswrt-merlin|ddwrt)
        echo "/jffs/nextdns/nextdns"
        ;;
    freebsd|pfsense|opnsense|netbsd|openbsd)
        echo "/usr/local/sbin/nextdns"
        ;;
    edgeos)
        echo "/config/nextdns/nextdns"
        ;;
    ubios)
        echo "/data/nextdns"
        ;;
    *)
        log_error "Unknown bin location for $OS"
        ;;
    esac
}

is_version_current() {
    case "$INSTALL_RELEASE" in
    */*)
        # Snapshot
        hash=${INSTALL_RELEASE#*/}
        test "v0.0.0-$hash" = "$CURRENT_RELEASE"
        ;;
    *)
        test "$INSTALL_RELEASE" = "$CURRENT_RELEASE"
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
        for cmd in curl wget openssl true; do
            # command is the "right" way but may be compiled out of busybox shell
            ! command -v $cmd > /dev/null 2>&1 || break
            ! which $cmd > /dev/null 2>&1 || break
        done
        case "$cmd" in
        curl) cmd="curl -A curl -s" ;;
        wget) cmd="wget -qO- -U curl" ;;
        openssl) cmd="openssl_get" ;;
        *)
            log_error "Cannot retrieve latest version"
            return
            ;;
        esac
        v=$($cmd "https://api.github.com/repos/nextdns/nextdns/releases/latest" | \
            grep '"tag_name":' | esed 's/.*"([^"]+)".*/\1/' | sed -e 's/^v//')
        if [ -z "$v" ]; then
            log_error "Cannot get latest version: $out"
        fi
        echo "$v"
    fi
}

esed() {
    if (echo | sed -E '' >/dev/null 2>&1); then
        sed -E "$@"
    else
        sed -r "$@"
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
