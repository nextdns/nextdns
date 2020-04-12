# NextDNS CLI Client

This project is a DNS53 to DNS over HTTPS (DoH) proxy with advanced
capabilities to get the most out of NextDNS service. Although the most
advanced features will only work with NextDNS, this program can work
as a client for any DoH provider.

## Install

First, obtain a configuration ID on [NextDNS](https://nextdns.io/).

Then follow the [installation instruction](https://github.com/nextdns/nextdns/wiki) for your platform or simply use our installer:

```
sh -c 'sh -c "$(curl -sL https://nextdns.io/install)"'
```

## Features

* Stub DNS53 to DoH proxy.
* Auto discovery and forwarding of LAN client's name and model.
* Supports a vast number of platforms / OS / routers.
* Can run on single host or at router level.
* Local cache
* Auto router setup (integrate with many different router firmware).
* Serve from /etc/hosts.
* Multi upstream healthcheck / fallback.
* Conditional forwarder selection based on domain.
* Conditional NextDNS configuration ID selection based on
  client subnet prefix or MAC address.
* Auto detection of captive portals.

### Supported Platforms

* Linux: Redhat, Fedora, CentOS, Debian, Ubuntu, Arch
* BSDs: FreeBSD, NetBSD, OpenBSD, DragonFly
* MacOS
* Windows
* OpenWRT (with LuCI UI)
* ASUS-Merlin
* pfSense
* Ubiquiti EdgeOS
* VyOS
* Synology
* Entware (WIP)
* DD-WRT (WIP)
* OpnSense (soon)
* Tomato (soon)
* QNAP (soon)

## Usage

The `nextdns` command is composed of sub commands:

```
Usage: nextdns <command> [arguments]

The commands are:

    install         install service on the system
    uninstall       uninstall service from the system
    start           start installed service
    stop            stop installed service
    restart         restart installed service
    status          return service status
    log             show service logs
    run             run the daemon
    config          manage configuration
    activate        setup the system to use NextDNS as a resolver
    deactivate      restore the resolver configuration
    version         show current version
```

The `install`, `uninstall`, `start`, `stop` and `status` methods are to interact
with the OS service management system. It will be used to un/register and
start/stop the service.

The `run` command starts the daemon in the foreground. It is meant to be called
from an init script. Use the `install` command to install one.

The `install` command takes the same arguments as the `run`. Arguments used with
the `install` command are used to call `run` when the system starts the service.

Once installed, you can edit the configuration using the `config set` command with
the same argument as the `run` command.

The `run`, `install` and `config` sub-commands takes the following arguments:

```
  -auto-activate
    	Run activate at startup and deactivate on exit.
  -bogus-priv
    	Bogus private reverse lookups.

    	All reverse lookups for private IP ranges (ie 192.168.x.x, etc.) are
    	answered with "no such domain" rather than being forwarded upstream.
    	The set of prefixes affected is the list given in RFC6303, for IPv4
    	and IPv6. (default true)
  -cache-max-age duration
    	If set to greater than 0, a cached entry will be considered stall after
    	this duration, even if the record's TTL is higher.
  -cache-size string
    	Set the size of the cache in byte. Use 0 to disable caching. The value
    	can be expressed with unit like kB, MB, GB. The cache is automatically
    	flushed when the pointed configuration is updated. (default "0")
  -config value
    	NextDNS custom configuration id.

    	The configuration id can be prefixed with a condition that is match for
    	each query:
    	* 10.0.3.0/24=abcdef: A CIDR can be used to restrict a configuration to
    	  a subnet.
    	* 00:1c:42:2e:60:4a=abcdef: A MAC address can be used to restrict
    	  configuration to a specific host on the LAN.

    	This parameter can be repeated. The first match wins.
  -config-file string
    	Custom path to configuration file.
  -detect-captive-portals
    	Automatic detection of captive portals and fallback on system DNS to
    	allow the connection to establish.

    	Beware that enabling this feature can allow an attacker to force nextdns
    	to disable DoH and leak unencrypted DNS traffic.
  -forwarder value
    	A DNS server to use for a specified domain.

    	Forwarders can be defined to send proxy DNS traffic to an alternative
    	DNS upstream resolver for specific domains. The format of this parameter
    	is [DOMAIN=]SERVER_ADDR[,SERVER_ADDR...].

    	A SERVER_ADDR can ben either an IP[:PORT] for DNS53 (unencrypted UDP,
    	TCP), or a HTTPS URL for a DNS over HTTPS server. For DoH, a bootstrap
    	IP can be specified as follow: https://dns.nextdns.io#45.90.28.0.
    	Several servers can be specified, separated by comas to implement
    	failover.
    	This parameter can be repeated. The first match wins.
  -hardened-privacy
    	When enabled, use DNS servers located in jurisdictions with strong
    	privacy laws. Available locations are: Switzerland, Iceland, Finland,
    	Panama and Hong Kong.
  -listen string
    	Listen address for UDP DNS proxy server. (default "localhost:53")
  -log-queries
    	Log DNS query.
  -max-ttl duration
    	If set to greater than 0, defines the maximum TTL value that will be
    	handed out to clients. The specified maximum TTL will be given to
    	clients instead of the true TTL value if it is lower. The true TTL
    	value is however kept in the cache to evaluate cache entries
    	freshness. This is best used in conjunction with the cache to force
    	clients not to rely on their own cache in order to pick up
    	configuration changes faster.
  -report-client-info
    	Embed clients information with queries.
  -setup-router
    	Automatically configure NextDNS for a router setup.
    	Common types of router are detected to integrate gracefuly. Changes
    	applies are undone on daemon exit. The listen option is ignored when
    	this option is used.
  -timeout duration
    	Maximum duration allowed for a request before failing. (default 5s)
  -use-hosts
    	Lookup /etc/hosts before sending queries to upstream resolver. (default true)
```

Once installed, the `activate` sub-command can be used to configure the target
system DNS resolver to point on the local instance of `nextdns`. This is a convenience
command to easily turn on and off nextdns on the host without killing the process.

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
sudo nextdns install \
    -setup-router \
    -report-client-info \
    -config 10.0.4.0/24=12345 \
    -config 00:1c:42:2e:60:4a=67890 \
    -config abcdef
```

Note: the `-setup-router` will auto-detect the type of router and apply the
appropriate changes to integrate with it.

### Split Horizon

In case an internal domain is managed by a private DNS server, it is possible to
setup conditional forwarders. Conditional forwarders can be either plain old
DNS53 or DoH servers themselves. Several servers can be specified for failover and
several with different domain can be used; the first match wins.

```
sudo nextdns install \
    -setup-router \
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
sudo nextdns install \
    -listen :53 \
    -forwarder https://1.1.1.1/dns-query
```

### Configuration file

At startup, nextdns reads its on disk configuration. The format of this file
consists of one option per line, exactly as the options accepted by the run
sub-command without the leading `-`. Lines starting with # are comments and
ignored. 

Example configuration:

```
# Example configuration for NextDNS.

listen :5353
setup-router yes
report-client-info yes

config 10.0.4.0/24=12345
config 00:1c:42:2e:60:4a=67890
config abcdef

forwarder mycompany.com=1.2.3.4,1.2.3.5
forwarder mycompany2.com=https://doh.mycompany.com/dns-query#1.2.3.4
```

Location and sometimes format of the configuration can vary from system to system.
It is advised to use the `nextdns config list` and `nextdns config set` commands
to interact with the configuration.
