#!/bin/sh

# Update sources.list
repo=/etc/apt/sources.list.d/nextdns.list
if [ -f $repo ] \
       && grep -qE '(nextdns.io/repo|dl.bintray.com)' $repo; then
    cat <<EOF > $repo
# Added for NextDNS
deb [signed-by=/usr/share/keyrings/nextdns.gpg] https://repo.nextdns.io/deb stable main
EOF
    curl -sL https://repo.nextdns.io/nextdns.gpg > /usr/share/keyrings/nextdns.gpg
    if ! dpkg --compare-versions $(dpkg-query --showformat='${Version}' --show apt) ge 1.1; then
        ln -sf /usr/share/keyrings/nextdns.gpg /etc/apt/trusted.gpg.d/.
    fi
fi

nextdns install -bogus-priv

exit 0
