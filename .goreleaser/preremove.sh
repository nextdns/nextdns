#!/bin/sh

if nextdns status > /dev/null 2>&1; then
    nextdns deactivate
    nextdns stop
    nextdns uninstall
fi

exit 0
