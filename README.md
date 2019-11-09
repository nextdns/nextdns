# NextDNS CLI Client

This project is a DNS53 to DNS over HTTPS (DoH) proxy with advanced
capabilities to get the most out of NextDNS service. Although the most
advanced features will only work with NextDNS, this program can work
as a client for any DoH provider.

## Features

* Stub DNS53 to DoH proxy.
* Can run on single host or at router level.
* Multi upstream healthcheck / fallback.
* Conditional forwarder selection based on domain.
* Auto discovery and forwarding of LAN clients name and model.
* Conditional NextDNS configuration ID selection based on
  client subnet prefix or MAC address.

## Installation

First, optain a configration ID on [NextDNS](https://nextdns.io/).

### Install the daemon

#### RPM Based Distributions (RedHat, Fedora, Centos, …)

```
sudo curl -s https://nextdns.io/yum.repo -o /etc/yum.repos.d/nextdns.repo
sudo yum install -y nextdns
```

#### Deb Based Distributions (Debian, Ubuntu, …)

```
wget -qO - https://nextdns.io/repo.gpg | sudo apt-key add -
echo "deb https://nextdns.io/repo/deb stable main" | sudo tee /etc/apt/sources.list.d/nextdns.list
sudo apt install apt-transport-https # only necessary on Debian
sudo apt update
sudo apt install nextdns
```

#### MacOS

Install [homebrew](https://brew.sh) first.

```
brew install nextdns/tap/nextdns
```

#### Source code

Install [Go](https://golang.org).

```
go get -u github.com/nextdns/nextdns
go install github.com/nextdns/nextdns
```

### Setup and start NextDNS

```
sudo nextdns install -report-client-info -config <conf_id>
```

Note: if installed on a router, add `-listen :53` to have it listen on public
interfaces.

### Point resolver to NextDNS

Note: this command will alter your system DNS resolver configuration.

```
sudo nextdns activate
```

## Advanced Usages

### Conditional Configuration

When installed on a router, nextdns can apply different configuration based on
the LAN client using conditional configuration parameters. The `-config`
parameter can be specified several times with different configuration IDs and
conditions. Conditions can be subnet prefixes or MAC addresses.

If for instance, we want:
* Clients in the `10.0.4.0/24` subnet to have the `12345` configuration
* The host with the `00:1c:42:2e:60:4a` MAC address to have the `67890`
  configuration
* The rest of the network to have the `abcdef` configuration

The install command would be as follow:

```
sudo nextdns run \
    -listen :53 \
    -report-client-info \
    -config 10.0.4.0/24=12345 \
    -config 00:1c:42:2e:60:4a=67890 \
    -config abcdef
```

### Split Horizon

In case an internal domain is managed by a private DNS server, it is possible to
setup conditional forwarders. Conditional forwarders can be either plain old
DNS53 or DoH servers themselves:

```
sudo nextdns run \
    -listen :53 \
    -report-client-info \
    -config abcdef \
    -forwarder mycompany.com=1.2.3.4 \
    -forwarder mycompany2.com=https://doh.mycompany.com/dns-query
```

### Integration with dnsmasq

It is possible to run dnsmasq and nextdns together and still benefit from client
reporting and conditional configuration:

* Make sure nextdns is installed on a different port using 
  `-listen 127.0.0.1:5555` for instance.
* Add the following settings to dnsmasq parameters: 
  `--server '127.0.0.1#5555' --add-mac --add-subnet=32,128`

### Use with another DoH provider

The NextDNS DoH proxy can be used with other DoH providers by using the
forwarder parameter with no condition:

```
sudo nextdns run \
    -listen :53 \
    -forwarder https://1.1.1.1/dns-query
```

### Configuration file

At startup, dnsmasq reads /etc/nextdns.conf, if it exists. The format of this
file consists of one option per line, exactly as the options accepted by the run
sub-command without the leading `-`. Lines starting with # are comments and
ignored. 

Example configuration:

```
# Example configuration for NextDNS.

listen :5353
report-client-info yes

config 10.0.4.0/24=12345
config 00:1c:42:2e:60:4a=67890
config abcdef

forwarder mycompany.com=1.2.3.4
forwarder mycompany2.com=https://doh.mycompany.com/dns-query
```
