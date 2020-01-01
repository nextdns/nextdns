#!/bin/sh

set -e

log_debug() {
    if [ "$DEBUG" = "1" ]; then
        printf "\e[30;1mDEBUG: %s\e[0m\n" "$*" >&2
    fi
}

log_info() {
    printf "INFO: %s\n" "$*" >&2
}

log_error() {
    printf "\e[31mERROR: %s\e[0m\n" "$*" >&2
}

print() {
    # shellcheck disable=SC2059
    printf "$@" >&2
}

menu() {
    while true; do
        n=1
        odd=0
        for item in "$@"; do
            if [ "$odd" = "0" ]; then
                echo "$n) $item"
                n=$((n+1))
                odd=1
            else
                odd=0
            fi
        done
        print "Choice (default=1): "
        read -r choice
        if [ -z "$choice" ]; then
            choice=1
        fi
        n=1
        odd=0
        for cb in "$@"; do
            if [ "$odd" = "0" ]; then
                odd=1
            else
                if [ "$n" = "$choice" ]; then
                    if ! eval "$cb"; then
                        log_error "$cb: exit $?"
                    fi
                    break 2
                fi
                n=$((n+1))
                odd=0
            fi
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
        print "$msg"
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

detect_goarch() {
    case $(uname -m) in
    x86_64)
        echo "amd64"
        ;;
    armv*)
        # TODO: test arm features to upgrade
        echo "armv5"
        ;;
    aarch64)
        echo "arm64"
        ;;
    mips)
        echo "mipsle"
        ;;
    *)
        log_error "Unsupported GOARCH: $(uname -m)"
        return 1
        ;;
    esac
}

detect_goos() {
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
    case $(uname -s) in
    Linux)
        case $(uname -o) in
        GNU/Linux)
            if grep -q '^EdgeRouter' /etc/version 2> /dev/null; then
                echo "edgeos"; return 0
            fi
            dist=$(grep '^ID=' /etc/os-release | cut -d= -f2)
            case $dist in
            debian|ubuntu|centos|fedora|rhel|arch|openwrt)
                echo "$dist"; return 0
                ;;
            esac
            ;;
        ASUSWRT-Merlin)
            echo "asuswrt-merlin"; return 0
            ;;
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
        pfsense|openwrt|asuswrt-merlin)
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
    if [ "$(id -u)" = "0" ]; then
        "$@"
    elif [ "$(command -v sudo)" ]; then 
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
            status=$?
            printf "\e[30;1m%s\e[0m\n" "$out"
            return $status
        fi
    fi
}

bin_location() {
    case $OS in
    centos|fedora|rhel|debian|ubuntu|arch|openwrt)
        echo "/usr/bin/nextdns"
        ;;
    darwin)
        echo "/usr/local/bin/nextdns"
        ;;
    asuswrt-merlin)
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
    if [ -f "$NEXTDNS_BIN" ]; then
        $NEXTDNS_BIN version|cut -d' ' -f 3
    fi
}

get_latest_release() {
    curl --silent "https://api.github.com/repos/nextdns/nextdns/releases/latest" |
    grep '"tag_name":' |
    sed -E 's/.*"([^"]+)".*/\1/' |
    sed -e 's/^v//'
}

install_bin() {
    log_debug "Installing $LATEST_RELEASE binary for $GOOS/$GOARCH to $NEXTDNS_BIN"
    url="https://github.com/nextdns/nextdns/releases/download/v${LATEST_RELEASE}/nextdns_${LATEST_RELEASE}_${GOOS}_${GOARCH}.tar.gz"
    mkdir -p "$(dirname "$NEXTDNS_BIN")"
    curl -sfL "$url" | tar Ozxf - nextdns > "$NEXTDNS_BIN"
}

uninstall_bin() {
    asroot "$NEXTDNS_BIN" uninstall
    asroot rm -f "$NEXTDNS_BIN"
}

install_rpm() {
    sudo curl -s https://nextdns.io/yum.repo -o /etc/yum.repos.d/nextdns.repo
    sudo yum install -y nextdns
}

uninstall_rpm() {
    sudo yum uninstall -y nextdns
}

install_deb() {
    wget -qO - https://nextdns.io/repo.gpg | sudo apt-key add -
    echo "deb https://nextdns.io/repo/deb stable main" | sudo tee /etc/apt/sources.list.d/nextdns.list
    if [ "$OS" = "debian" ]; then
        sudo apt install apt-transport-https
    fi
    sudo apt update
    sudo apt install nextdns
}

uninstall_deb() {
    log_debug "Uninstalling deb"
    sudo apt remove nextdns
}

install_arch() {
    sudo pacman -S yay
    yay -S nextdns
}

uninstall_arch() {
    sudo pacman -R nextdns
}

install_openwrt() {
    opkg update
    opkg install nextdns
    case $(ask_bool 'Install the GUI?' true) in
    true)
        opkg install luci-app-nextdns
        ;;
    esac
}

uninstall_openwrt() {
    opkg remove nextdns
}

install_brew() {
    silent_exec brew install nextdns/tap/nextdns
}

uninstall_brew() {
    silent_exec brew uninstall nextdns/tap/nextdns
}

install_freebsd() {
    # TODO: port install + UI
    install_bin
}

uninstall_freebsd() {
    # TODO: port uninstall
    uninstall_bin
}

install_pfsense() {
    # TODO: port install + UI
    install_bin
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
    debian|ubuntu)
        echo "deb"
        ;;
    arch)
        echo "arch"
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
    asuswrt-merlin|edgeos)
        echo "bin"
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

install() {
    if type=$(install_type); then
        log_info "Installing NextDNS..."
        log_debug "Using $type install type"
        eval "install_$type"
    else
        return $?
    fi
}

uninstall() {
    if type=$(install_type); then
        log_info "Uninstalling NextDNS..."
        log_debug "Using $type uninstall type"
        eval "uninstall_$type"
    else
        return $?
    fi
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
    while [ -z "$CONFIG_ID" ]; do
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

configure() {
    log_debug "Start configure"
    args=""
    add_arg() {
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
    unsure)
        case $(ask_bool 'Listen on public interface? (say no if "no" running on a router)') in
            true)
                add_arg listen ':53'
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

quit() {
    exit 0
}

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
LATEST_RELEASE=$(get_latest_release)

while true; do
    CURRENT_RELEASE=$(get_current_release)
    log_debug "Start install loop with CURRENT_RELEASE=$CURRENT_RELEASE"

    if [ "$CURRENT_RELEASE" ]; then
        if [ "$CURRENT_RELEASE" = "$LATEST_RELEASE" ]; then
            log_debug "NextDNS is out of date ($CURRENT_RELEASE != $LATEST_RELEASE)"
            menu \
                "Upgrade NextDNS from $CURRENT_RELEASE to $LATEST_RELEASE" install \
                "Configure NextDNS" configure \
                "Uninstall NextDNS" uninstall \
                "Quit" quit
        else
            log_debug "NextDNS is up to date ($CURRENT_RELEASE)"
            menu \
                "Configure NextDNS" configure \
                "Uninstall NextDNS" uninstall \
                "Quit" quit
        fi
    else
        log_debug "NextDNS is not installed"
        menu \
            "Install NextDNS" install \
            "Quit" quit
    fi
done
