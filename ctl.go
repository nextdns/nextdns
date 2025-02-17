package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"syscall"

	"github.com/nextdns/nextdns/config"
	"github.com/nextdns/nextdns/ctl"
)

func ctlCmd(args []string) error {
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	control := fs.String("control", config.DefaultControl, "Address to the control socket")
	_ = fs.Parse(args[1:])
	cl, err := ctl.Dial(*control)
	if err != nil {
		if os.Geteuid() != 0 {
			return syscall.Exec("/usr/bin/sudo", append([]string{"sudo", os.Args[0]}, args...), os.Environ())
		}
		return err
	}
	defer cl.Close()
	data, err := cl.Send(ctl.Event{
		Name: cmd,
	})
	if err != nil {
		return err
	}
	if s, ok := data.(string); ok {
		fmt.Println(s)
		return nil
	}
	b, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
