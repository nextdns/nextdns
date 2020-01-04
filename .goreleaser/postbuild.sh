#!/bin/sh

set -ex

ls -1 dist/nextdns_linux_{arm*,mips,mipsle}/nextdns | parallel upx -q --brute
