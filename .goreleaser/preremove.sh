#!/bin/sh

if nextdns status; then
    nextdns uninstall
fi