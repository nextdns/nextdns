package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"os/exec"
)

func upgrade(args []string) error {
	return installer("upgrade")
}

func installer(cmd string) error {
	res, err := http.Get("https://nextdns.io/install")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var script bytes.Buffer
	if _, err := io.Copy(&script, res.Body); err != nil {
		return err
	}
	c := exec.Command("sh", "-c", script.String())
	c.Env = append(os.Environ(), "RUN_COMMAND="+cmd)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
