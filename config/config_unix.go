// +build !windows,!freebsd

package config

func DefaultConfPath() string {
	return "/etc/nextdns.conf"
}
