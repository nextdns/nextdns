# NextDNS CLI Client

NextDNS CLI is a command-line tool that allows you to use [NextDNS](https://nextdns.io)'s DNS-over-HTTPS (DoH) service with advanced capabilities. Although the most advanced features will only work with NextDNS, this program can work as a client for any DoH provider or a mix of NextDNS + another DNS (split horizon).

This CLI is mostly aimed at routers and UNIX based systems, but it is also a great client for Windows and macOS, especially for people who prefer a fully open-source client and don't mind the lack of a GUI.

See the [wiki](https://github.com/nextdns/nextdns/wiki) for installation and usage instructions.

### Shell completion

```sh
nextdns completion bash | sudo tee /etc/bash_completion.d/nextdns
nextdns completion zsh  > "${fpath[1]}/_nextdns"
nextdns completion fish > ~/.config/fish/completions/nextdns.fish
```

### OpenWrt IPv6 relay DNS

With `setup-router` enabled, NextDNS replaces the IPv6 DNS servers advertised by
OpenWrt's downstream RA/DHCPv6 relay or hybrid interfaces with a local router
address. This applies both when dnsmasq forwards to NextDNS and when NextDNS
serves port 53 directly. Explicit IPv4 DNS entries and the upstream master
interface are preserved.

The override requires odhcpd, netifd and rpcd's UCI interface (`ubus call uci get
'{"config":"dhcp"}'`). NextDNS prefers a local ULA and falls back to a link-local
address, checking UDP and TCP DNS before advertising it. Readiness checks retry
for up to five seconds per interface while dnsmasq starts, and stop on shutdown.
If the interface is down or no suitable resolver responds, setup reports an error
and leaves the original IPv6 DNS configuration in place. Restart NextDNS once the
interface is ready.

Original DNS options are saved in `/etc/nextdns-openwrt-relay.json` before changes
are applied. Shutdown restores those options, including their original UCI types,
unless they have subsequently been edited or the interface has been replaced.
The recovery file survives daemon restarts/reboots. Startup recovers it before
configuring dnsmasq, and retains it if restoration fails. It is removed only after UCI commit and odhcpd reload succeed.

Clients may retain earlier DNS advertisements until their lifetimes expire or
network configuration is renewed. This uses odhcpd's rewriting of existing RDNSS
and DHCPv6 DNS options; it does not intercept hardcoded DNS, add missing upstream
DNS options, or rewrite authenticated DHCPv6 replies. Router firewall rules must
allow clients to reach the advertised address on UDP and TCP port 53.

Run the unit and failure-path tests with `go test ./router/openwrt`. To check
actual UCI/rpcd list replacement and restoration in a disposable OpenWrt 24.10.7
container, run `router/openwrt/testdata/relay-integration.sh` (requires Go and
Docker). This container test does not exercise client-visible RA/DHCPv6 packets;
validate those on a routed test network before deployment.
