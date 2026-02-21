package host

type teeLogger struct {
	loggers []Logger
}

// NewTeeLogger returns a Logger that fans out each call to all provided loggers.
func NewTeeLogger(loggers ...Logger) Logger {
	return teeLogger{loggers: loggers}
}

func (l teeLogger) Debug(v ...interface{}) {
	for _, lg := range l.loggers {
		lg.Debug(v...)
	}
}

func (l teeLogger) Debugf(format string, a ...interface{}) {
	for _, lg := range l.loggers {
		lg.Debugf(format, a...)
	}
}

func (l teeLogger) Info(v ...interface{}) {
	for _, lg := range l.loggers {
		lg.Info(v...)
	}
}

func (l teeLogger) Infof(format string, a ...interface{}) {
	for _, lg := range l.loggers {
		lg.Infof(format, a...)
	}
}

func (l teeLogger) Warning(v ...interface{}) {
	for _, lg := range l.loggers {
		lg.Warning(v...)
	}
}

func (l teeLogger) Warningf(format string, a ...interface{}) {
	for _, lg := range l.loggers {
		lg.Warningf(format, a...)
	}
}

func (l teeLogger) Error(v ...interface{}) {
	for _, lg := range l.loggers {
		lg.Error(v...)
	}
}

func (l teeLogger) Errorf(format string, a ...interface{}) {
	for _, lg := range l.loggers {
		lg.Errorf(format, a...)
	}
}
