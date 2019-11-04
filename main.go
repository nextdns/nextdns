package main

import (
	"fmt"
	"os"
)

var version = "dev"

type command struct {
	name string
	run  func(cmd string) error
	desc string
	post func() error
}

var commands = []command{
	{"install", svc, "install service on the system", func() error { return svc("start") }},
	{"uninstall", svc, "uninstall service from the system", func() error { _ = deactivate(""); return nil }},
	{"start", svc, "start installed service", nil},
	{"stop", svc, "stop installed service", nil},
	{"status", svc, "return service status", nil},
	{"run", svc, "run the daemon", nil},

	{"activate", activate, "setup the system to use NextDNS as a resolver", nil},
	{"deactivate", deactivate, "restore the resolver configuration", nil},

	{"version", showVersion, "show current version", nil},
}

func showCommands() {
	fmt.Println("Usage: nextdns <command> [arguments]")
	fmt.Println("")
	fmt.Println("The commands are:")
	fmt.Println("")
	for _, cmd := range commands {
		fmt.Printf("    %-15s %s\n", cmd.name, cmd.desc)
	}
	fmt.Println("")
	os.Exit(1)
}

func showVersion(string) error {
	fmt.Printf("nextdns version %s\n", version)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		showCommands()
	}
	cmd := os.Args[1]
	os.Args = append(os.Args[:1], os.Args[2:]...)
	for _, c := range commands {
		if c.name != cmd {
			continue
		}
		if err := c.run(c.name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if c.post != nil {
			if err := c.post(); err != nil {
				fmt.Fprintf(os.Stderr, "Post err: %v\n", err)
				os.Exit(1)
			}
		}
		return
	}
	// Command not found
	showCommands()
}
