#!/bin/sh

set -ex

upx -q --brute dist/nextdns_linux_{arm*,mips,mipsle}/nextdns
