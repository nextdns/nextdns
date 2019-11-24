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

Create a configuration id on [NextDNS](https://nextdns.io) and use it here in
place of `conf_id`.

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

## Usage

The `nextdns` command is composed of sub commands:

```
Usage: nextdns <command> [arguments]

The commands are:

    install         install service on the system
    uninstall       uninstall service from the system
    start           start installed service
    stop            stop installed service
    status          return service status
    run             run the daemon
    activate        setup the system to use NextDNS as a resolver
    deactivate      restore the resolver configuration
    version         show current version
```

The `install`, `uninstall`, `start`, `stop` and `status` methods are to interact
with the OS service management system. It will be used to un/register and
start/stop the service.

The main sub-command to run the service is the `run` command. The run command
can be configured using options arguments or a configuration file (see
[Configuration file] below.

The `install` command takes the same arguments as the `run`. Arguments used with
the `install` command are used to call `run` when the system starts the service.

The `run` (and `install`) sub-command takes the following arguments:

```
  -bogus-priv
    	Bogus private reverse lookups.

    	All reverse lookups for private IP ranges (ie 192.168.x.x, etc.) are answered with
    	"no such domain" rather than being forwarded upstream. The set of prefixes affected
    	is the list given in RFC6303, for IPv4 and IPv6.
  -config value
    	NextDNS custom configuration id.

    	The configuration id can be prefixed with a condition that is match for each query:
    	* 10.0.3.0/24=abcdef: A CIDR can be used to restrict a configuration to a subnet.
    	* 00:1c:42:2e:60:4a=abcdef: A MAC address can be used to restrict configuration
    	 to a specific host on the LAN.

    	This parameter can be repeated. The first match wins.
  -config-file string
    	Path to configuration file. (default "/etc/nextdns.conf")
  -detect-captive-portals
    	Automatic detection of captive portals and fallback on system DNS to allow the connection.

    	Beware that enabling this feature can allow an attacker to force nextdns to disable DoH
    	and leak unencrypted DNS traffic.
  -forwarder value
    	A DNS server to use for a specified domain.

    	Forwarders can be defined to send proxy DNS traffic to an alternative DNS upstream
    	resolver for specific domains. The format of this parameter is
    	[DOMAIN=]SERVER_ADDR[,SERVER_ADDR...].

    	A SERVER_ADDR can ben either an IP for DNS53 (unencrypted UDP, TCP), or a https URL
    	for a DNS over HTTPS server. For DoH, a bootstrap IP can be specified as follow:
    	https://dns.nextdns.io#45.90.28.0. Several servers can be specified, separated by
    	comas to implement failover.
    	This parameter can be repeated. The first match wins.
  -hardened-privacy
    	When enabled, use DNS servers located in jurisdictions with strong privacy laws.
    	Available locations are: Switzerland, Iceland, Finland, Panama and Hong Kong.
  -listen string
    	Listen address for UDP DNS proxy server. (default "localhost:53")
  -log-queries
    	Log DNS query.
  -report-client-info
    	Embed clients information with queries.
  -timeout duration
    	Maximum duration allowed for a request before failing (default 5s)
```

Once installed, the `activate` sub-command can be used to configure the target
system DNS resolver to point on the local instance of `nextdns`.

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
DNS53 or DoH servers themselves. Several servers can be specified for failover and
several with different domain can be used; the first match wins.

```
sudo nextdns run \
    -listen :53 \
    -report-client-info \
    -config abcdef \
    -forwarder mycompany.com=1.2.3.4,1.2.3.5 \
    -forwarder mycompany2.com=https://doh.mycompany.com/dns-query#1.2.3.4
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

At startup, nextdns reads /etc/nextdns.conf, if it exists. The format of this
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

forwarder mycompany.com=1.2.3.4,1.2.3.5
forwarder mycompany2.com=https://doh.mycompany.com/dns-query#1.2.3.4
```
