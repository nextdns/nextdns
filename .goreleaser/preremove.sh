#!/bin/sh

if nextdns status > /dev/null 2>&1; then
    nextdns uninstall
fi

exit 0