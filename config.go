package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

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
	case "edit":
		var c config.Config
		c.Parse("nextdns config edit", nil, true)
		tmp, err := ioutil.TempFile("", "")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if err := c.Write(tmp); err != nil {
			tmp.Close()
			return err
		}
		tmp.Close()
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		cmd := exec.Command(editor, tmp.Name())
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %v", editor, err)
		}
		c = config.Config{}
		c.Parse("nextdns config edit", []string{"-config-file", tmp.Name()}, true)
		c.File = ""
		return c.Save()
	case "wizard":
		return installer("configure")
	default:
		return errors.New("usage: \n" +
			"  config list              list configuration options\n" +
			"  config set [options]     set a configuration option\n" +
			"                           (see config set -h for list of options)\n" +
			"  config edit              edit configuration using default editor\n" +
			"  config wizard            run the configuration wizard")
	}
}
