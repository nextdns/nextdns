package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/nextdns/nextdns/host/service"
)

func CreateWithTemplate(path, tmpl string, mode os.FileMode, c service.Config) error {
	if _, err := os.Stat(path); err == nil {
		return service.ErrAlreadyInstalled
	}

	dir := filepath.Dir(path)
	if st, err := os.Stat(dir); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	} else if !st.IsDir() {
		return fmt.Errorf("%s: not a directory", dir)
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
