package main

import (
	"fmt"
	"log"
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
		c.Parse("nextdns "+cmd, args, true)
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
		log.Fatal(err)
	}

	switch cmd {
	case "install":
		_ = s.Stop()
		_ = s.Uninstall()
		if err := c.Save(); err != nil {
			fmt.Printf("Cannot write config: %v", err)
			os.Exit(1)
		}
		err := s.Install()
		if err == nil {
			err = s.Start()
		}
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
