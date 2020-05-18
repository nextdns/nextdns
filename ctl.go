package main

import (
	"encoding/json"
	"flag"
	"fmt"

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
		return err
	}
	defer cl.Close()
	data, err := cl.Send(ctl.Event{
		Name: cmd,
	})
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
