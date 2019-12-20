package main

import (
	"errors"
	"flag"
	"os"

	"github.com/nextdns/nextdns/config"
)

func cfg(args []string) error {
	fs := flag.NewFlagSet(args[0], flag.ExitOnError)
	args = args[1:]
	configFile := fs.String("config-file", config.DefaultConfPath(), "Path to configuration file.")
	_ = fs.Parse(args)
	subCmd := "list"
	args = fs.Args()
	if len(args) > 0 {
		subCmd = args[0]
		args = args[1:]
	}
	switch subCmd {
	case "list":
		var c config.Config
		c.Parse([]string{"-config-file", *configFile})
		_ = c.Write(os.Stdout)
	case "set":
		if len(args) != 2 {
			return errors.New("usage: config set <name> <value>")
		}
		var c config.Config
		args = append(args, "-config-file", *configFile)
		args[0] = "-" + args[0]
		c.Parse(args)
		return c.Save()
	default:
		return errors.New("usage: \n" +
			"  config [--config-file=file] [list]\n" +
			"  config [--config-file=file] set <name> <value>")
	}

	return nil
}
