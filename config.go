package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
		// Read the existing configuration file
		existingConfig, err := os.ReadFile(c.File)
		if err != nil {
			return err
		}
		// Preserve lines that start with #
		var preservedLines []string
		for _, line := range strings.Split(string(existingConfig), "\n") {
			if strings.HasPrefix(line, "#") {
				preservedLines = append(preservedLines, line)
			}
		}
		// Save the new configuration
		if err := c.Save(); err != nil {
			return err
		}
		// Append preserved lines to the new configuration
		newConfig, err := os.ReadFile(c.File)
		if err != nil {
			return err
		}
		finalConfig := strings.Join(preservedLines, "\n") + "\n" + string(newConfig)
		return os.WriteFile(c.File, []byte(finalConfig), 0644)
	case "edit":
		var c config.Config
		c.Parse("nextdns config edit", nil, true)
		tmp, err := os.CreateTemp("", "")
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
