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
apt update
apt install nextdns
```

#### Source code

```
# Install Go 1.13+
go get -u github.com/nextdns/nextdns
go install github.com/nextdns/nextdns
```

### Setup and start NextDNS

```
sudo nextdns install --config <conf id>
```

### Point resolver to NextDNS

Note: this command will alter your system DNS resolver configuration.

```
sudo nextdns activate
```
