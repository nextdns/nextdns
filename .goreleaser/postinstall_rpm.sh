#!/bin/sh

# Update repository
if [ -d /etc/zypp/repos.d ]; then
    repo=/etc/zypp/repos.d/nextdns.repo
elif [ -d /etc/yum.repos.d ]; then
    repo=/etc/yum.repos.d/nextdns.repo
fi
if [ -n "$repo" ] \
       && [ -f "$repo" ] \
       && grep -qE '(nextdns.io/repo|dl.bintray.com)' $repo; then
    curl -sL https://repo.nextdns.io/nextdns.repo > $repo
fi

nextdns install -bogus-priv

exit 0
