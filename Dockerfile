FROM alpine
COPY nextdns /usr/bin/nextdns
ENTRYPOINT ["/usr/bin/nextdns"]
CMD ["run"]
