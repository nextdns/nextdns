package main

import (
	"errors"
	"os"

	"github.com/nextdns/nextdns/config"
)

func cfg(args []string) error {
	args = args[1:]
	subCmd := "list"
	if len(args) > 0 {
		subCmd = args[0]
		args = args[1:]
	}
	switch subCmd {
	case "list":
		var c config.Config
		c.Parse("nextdns config list", args, true)
		return c.Write(os.Stdout)
	case "set":
		var c config.Config
		c.Parse("nextdns config set", args, true)
		return c.Save()
	default:
		return errors.New("usage: \n" +
			"  config [list]\n" +
			"  config set [options]")
	}
}
