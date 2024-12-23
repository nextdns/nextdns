FROM alpine
ENTRYPOINT ["/usr/bin/nextdns"]
COPY nextdns /usr/bin/nextdns
