package internal

import (
	"os"
	"text/template"
)

func WriteTemplate(path, tmpl string, data interface{}, mode os.FileMode) error {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()

	if err = t.Execute(f, data); err != nil {
		return err
	}

	return nil
}
