package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"os/exec"
)

func upgrade(args []string) error {
	res, err := http.Get("https://nextdns.io/install")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var script bytes.Buffer
	if _, err := io.Copy(&script, res.Body); err != nil {
		return err
	}
	cmd := exec.Command("sh", "-c", script.String())
	cmd.Env = append(os.Environ(), "RUN_COMMAND=upgrade")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
