// Package launchd implements the macOS init system.

package launchd

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/nextdns/nextdns/host/service"
	"github.com/nextdns/nextdns/host/service/internal"
)

type Service struct {
	service.Config
	service.ConfigFileStorer
	Path string
}

func New(c service.Config) (Service, error) {
	if _, err := exec.LookPath("launchctl"); err != nil {
		return Service{}, service.ErrNotSuported
	}
	return Service{
		Config:           c,
		ConfigFileStorer: service.ConfigFileStorer{File: "/etc/" + c.Name + ".conf"},
		Path:             "/Library/LaunchDaemons/" + c.Name + ".plist",
	}, nil
}

func (s Service) Install() error {
	return internal.CreateWithTemplate(s.Path, tmpl, 0644, s.Config)
}

func (s Service) Uninstall() error {
	_ = s.Stop()
	return os.Remove(s.Path)
}

func (s Service) Status() (service.Status, error) {
	exitCode, out, err := internal.RunOutput("launchctl", "list", s.Name)
	if exitCode == 0 && err != nil {
		if !strings.Contains(err.Error(), "failed with StandardError") {
			return service.StatusUnknown, err
		}
	}

	re := regexp.MustCompile(`"PID" = ([0-9]+);`)
	matches := re.FindStringSubmatch(out)
	if len(matches) == 2 {
		return service.StatusRunning, nil
	}

	if _, err = os.Stat(s.Path); err == nil {
		return service.StatusStopped, nil
	}

	return service.StatusNotInstalled, nil
}

func (s Service) Start() error {
	return internal.Run("launchctl", "load", s.Path)
}

func (s Service) Stop() error {
	return internal.Run("launchctl", "unload", s.Path)
}

func (s Service) Restart() error {
	err := s.Stop()
	if err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return s.Start()
}

var tmpl = `<?xml version='1.0' encoding='UTF-8'?>
<!DOCTYPE plist PUBLIC "-//Apple Computer//DTD PLIST 1.0//EN"
"http://www.apple.com/DTDs/PropertyList-1.0.dtd" >
<plist version='1.0'>
	<dict>
		<key>EnvironmentVariables</key>
		<dict>
			<key>{{.RunModeEnv}}</key>
			<string>1</string>
		</dict>
		<key>Label</key>
		<string>{{html .Name}}</string>
		<key>ProgramArguments</key>
		<array>
			<string>{{html .Executable}}</string>
		{{range .Config.Arguments}}
			<string>{{html .}}</string>
		{{end}}
		</array>
		<key>KeepAlive</key>
		<true/>
		<key>Disabled</key>
		<false/>
	</dict>
</plist>
`
