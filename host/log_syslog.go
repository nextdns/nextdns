//go:build !windows && !darwin

package host

import (
	"fmt"
	"log/syslog"
)

type syslogLogger struct {
	syslog *syslog.Writer
}

func newSyslogLogger(name string) (Logger, error) {
	w, err := syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, name)
	if err != nil {
		return nil, err
	}
	return syslogLogger{syslog: w}, nil
}

func (l syslogLogger) Debug(v ...interface{}) {
	// Use notice instead of info as many systems filter < notice level
	_ = l.syslog.Debug(fmt.Sprint(v...))
}

func (l syslogLogger) Debugf(format string, a ...interface{}) {
	_ = l.syslog.Debug(fmt.Sprintf(format, a...))
}

func (l syslogLogger) Info(v ...interface{}) {
	// Use notice instead of info as many systems filter < notice level
	_ = l.syslog.Notice(fmt.Sprint(v...))
}

func (l syslogLogger) Infof(format string, a ...interface{}) {
	_ = l.syslog.Notice(fmt.Sprintf(format, a...))
}

func (l syslogLogger) Warning(v ...interface{}) {
	_ = l.syslog.Warning(fmt.Sprint(v...))
}

func (l syslogLogger) Warningf(format string, a ...interface{}) {
	_ = l.syslog.Warning(fmt.Sprintf(format, a...))
}

func (l syslogLogger) Error(v ...interface{}) {
	_ = l.syslog.Err(fmt.Sprint(v...))
}

func (l syslogLogger) Errorf(format string, a ...interface{}) {
	_ = l.syslog.Err(fmt.Sprintf(format, a...))
}
