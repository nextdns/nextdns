package main

import (
	"fmt"
	"os"

	"github.com/nextdns/nextdns/config"
	"github.com/nextdns/nextdns/host"
	"github.com/nextdns/nextdns/host/service"
)

func svc(args []string) error {
	cmd := args[0]
	args = args[1:]
	var c config.Config
	if cmd == "install" {
		// Reset the stored configuration when install is provided with
		// parameters
		useStorage := len(args) == 0
		c.Parse("nextdns "+cmd, args, useStorage)
	}

	svcArgs := []string{"run"}
	if c.File != "" {
		svcArgs = append(svcArgs, "-config-file", c.File)
	}
	s, err := host.NewService(service.Config{
		Name:        "nextdns",
		DisplayName: "NextDNS Proxy",
		Description: "NextDNS DNS53 to DoH proxy.",
		Arguments:   svcArgs,
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	switch cmd {
	case "install":
		_ = s.Stop()
		_ = s.Uninstall()
		if len(args) > 0 {
			if err := c.Save(); err != nil {
				fmt.Printf("Cannot write config: %v\n", err)
				os.Exit(1)
			}
		}
		err := s.Install()
		if err == nil {
			err = s.Start()
		}
		fmt.Printf("NextDNS installed and started using %s init\n", service.Name(s))
		return err
	case "uninstall":
		_ = deactivate()
		_ = s.Stop()
		return s.Uninstall()
	case "start":
		return s.Start()
	case "stop":
		return s.Stop()
	case "restart":
		return s.Restart()
	case "status":
		status := "unknown"
		s, err := s.Status()
		if err != nil {
			return err
		}
		switch s {
		case service.StatusRunning:
			status = "running"
		case service.StatusStopped:
			status = "stopped"
		case service.StatusNotInstalled:
			status = "not installed"
		}
		fmt.Println(status)
		return nil
	case "log":
		l, err := host.ReadLog("nextdns")
		fmt.Printf("%s", l)
		return err
	default:
		panic("unknown cmd: " + cmd)
	}
}
