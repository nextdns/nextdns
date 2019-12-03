#!/bin/sh

# For deb, ignore upgrade actions
case "$1" in
upgrade|failed-upgrade|deconfigure) exit 0;;
esac

if nextdns status > /dev/null 2>&1; then
    nextdns uninstall
fi

exit 0