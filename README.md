# NextDNS UNIX Client

## Installation

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
sudo nextdns install --report-client-info --config <conf_id>
```

Note: if installed on a router, add `--listen :53` to have it listen on public
interfaces.

### Point resolver to NextDNS

Note: this command will alter your system DNS resolver configuration.

```
sudo nextdns activate
```

## Advanced Usages

### Conditional Configuration

When installed on a router, nextdns can apply different configuration based on
the LAN client using conditional configuration parameters. THe `--config`
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
    --listen :53 \
    --report-client-info \
    --config 10.0.4.0/24=12345 \
    --config 00:1c:42:2e:60:4a=67890 \
    --config abcdef
```

### Integration with dnsmasq

It is possible to run dnsmasq and nextdns together and still benefit from client
reporting and conditional configuration:

* Make sure nextdns is installed on a different port using 
  `--listen 127.0.0.1:5555` for instance.
* Add the following settings to dnsmasq parameters: 
  `--server '127.0.0.1#5555' --add-mac --add-subnet=32,128`

