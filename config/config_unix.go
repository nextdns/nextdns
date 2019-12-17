// +build !windows,!freebsd

package config

func defaultConfPath() string {
	return "/etc/nextdns.conf"
}
