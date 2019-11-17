#!/bin/sh

if nextdns status; then
    nextdns restart
fi