#!/usr/bin/env sh
set -eu

git config core.hooksPath .githooks
echo "Installed hooks via core.hooksPath=.githooks" >&2
