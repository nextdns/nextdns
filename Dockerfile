##
## build with:
## docker buildx build --platform=local --output dist .
##
## binary for your arch will be inside dist folder
##
## to build for OSX, run previously:
## docker buildx create --use --platform darwin/amd64
##

FROM --platform=$BUILDPLATFORM tonistiigi/xx:golang AS xgo

FROM --platform=$BUILDPLATFORM golang:1.14.2-alpine AS build

ENV CGO_ENABLED=0
COPY --from=xgo / /

ARG TARGETPLATFORM
RUN go env

WORKDIR /src
COPY . /src/

RUN go build -ldflags="-s -w" -o /go/bin/nextdns

FROM scratch AS binaries
COPY --from=build /go/bin/nextdns /
