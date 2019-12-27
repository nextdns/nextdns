package internal

import (
	"os"
	"text/template"

	"github.com/nextdns/nextdns/host/service"
)

func CreateWithTemplate(path, tmpl string, mode os.FileMode, c service.Config) error {
	if _, err := os.Stat(path); err == nil {
		return service.ErrAlreadyInstalled
	}

	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return err
	}

	ep, err := os.Executable()
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()

	if err = t.Execute(f, struct {
		service.Config
		Executable string
		RunModeEnv string
	}{
		c,
		ep,
		service.RunModeEnv,
	}); err != nil {
		return err
	}

	return nil
}
