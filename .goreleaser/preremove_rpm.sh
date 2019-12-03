#!/bin/sh

uninstall=0
upgrade=1

if [ "$1" = "$uninstall" ] && nextdns status > /dev/null 2>&1; then
    nextdns uninstall
fi

if [ "$1" = "$upgrade" ]; then
    nextdns install -report-client-info -bogus-priv
fi

exit 0