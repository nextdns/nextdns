//go:build !windows
// +build !windows

package host

import (
	stdlog "log"
	"os"
)

type consoleLogger struct {
	debug, info, warn, err *stdlog.Logger
}

func NewConsoleLogger(name string) Logger {
	return consoleLogger{
		debug: stdlog.New(os.Stderr, "DEBUG: ", stdlog.Ltime),
		info:  stdlog.New(os.Stderr, "INFO:  ", stdlog.Ltime),
		warn:  stdlog.New(os.Stderr, "WARN:  ", stdlog.Ltime),
		err:   stdlog.New(os.Stderr, "ERROR: ", stdlog.Ltime),
	}
}

func (l consoleLogger) Debug(v ...interface{}) {
	l.debug.Print(v...)
}

func (l consoleLogger) Debugf(format string, a ...interface{}) {
	l.debug.Printf(format, a...)
}

func (l consoleLogger) Info(v ...interface{}) {
	l.info.Print(v...)
}

func (l consoleLogger) Infof(format string, a ...interface{}) {
	l.info.Printf(format, a...)
}

func (l consoleLogger) Warning(v ...interface{}) {
	l.warn.Print(v...)
}

func (l consoleLogger) Warningf(format string, a ...interface{}) {
	l.warn.Printf(format, a...)
}

func (l consoleLogger) Error(v ...interface{}) {
	l.err.Print(v...)
}

func (l consoleLogger) Errorf(format string, a ...interface{}) {
	l.err.Printf(format, a...)
}
