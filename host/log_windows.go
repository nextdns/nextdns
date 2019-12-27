package host

import (
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
)

type windowsLogger struct {
	log debug.Log
}

func newConsoleLogger(name string) Logger {
	return windowsLogger{log: debug.New(name)}
}

func newServiceLogger(name string) (log.Logger, error) {
	err = eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return err
		}
	}
	el, err := logentlog.Open(name)
	if err != nil {
		return nil, err
	}
	return windowsLogger{log: el}
}

func (l windowsLogger) Info(v ...interface{}) {
	_ = l.log.Info(1, fmt.Sprint(v...))
}

func (l windowsLogger) Infof(format string, a ...interface{}) {
	_ = l.log.Info(1, fmt.Sprintf(format, a...))
}

func (l windowsLogger) Warning(v ...interface{}) {
	return l.log.Warning(2, fmt.Sprint(v...))
}

func (l windowsLogger) Warningf(format string, a ...interface{}) {
	return l.log.Warning(2, fmt.Sprintf(format, a...))
}

func (l windowsLogger) Error(v ...interface{}) {
	return l.log.Error(3, fmt.Sprint(v...))
}

func (l windowsLogger) Errorf(format string, a ...interface{}) {
	return l.log.Error(3, fmt.Sprintf(format, a...))
}




