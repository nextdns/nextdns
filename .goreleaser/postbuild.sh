#!/bin/sh

set -ex

for arch in arm64 arm_5 arm_6 arm_7 mips mipsle; do
    bin="dist/nextdns_linux_$arch/nextdns"
    echo "      • compressing                  $bin"
    upx -q --brute $bin
done