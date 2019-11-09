##
## build with:
## docker build --platform=local --output dist .
##
## binary for your arch will be inside dist folder
##


FROM --platform=$BUILDPLATFORM golang:1.13-alpine AS build

ENV CGO_ENABLED=0
WORKDIR /src
COPY . /src/

RUN go install

FROM scratch AS binaries
COPY --from=build /go/bin/nextdns /
