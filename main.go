package main

import (
	"fmt"
	"os"
	"runtime"
)

var (
	version  = "dev"
	platform = runtime.GOOS
)

type command struct {
	name string
	run  func(cmd string) error
	desc string
}

var commands = []command{
	{"install", svc, "install service on the system"},
	{"uninstall", svc, "uninstall service from the system"},
	{"start", svc, "start installed service"},
	{"stop", svc, "stop installed service"},
	{"restart", svc, "restart installed service"},
	{"status", svc, "return service status"},
	{"run", svc, "run the daemon"},
	{"config", svc, "show configuration"},
	{"log", svc, "show service logs"},

	{"activate", activation, "setup the system to use NextDNS as a resolver"},
	{"deactivate", activation, "restore the resolver configuration"},

	{"version", showVersion, "show current version"},
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
		return
	}
	// Command not found
	showCommands()
}
