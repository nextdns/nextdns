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
brew install rs/tap/jplot
```

#### Source code

Install [Go](https://golang.org).

```
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
