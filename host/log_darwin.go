package host

import (
	"fmt"
	"os"
	"os/exec"
)

//#include <syslog.h>
//void doLog(int facility, const char *msg) {
//	syslog(facility, "%s", msg);
//}
import "C"

const kLOG_DAEMON = 0x18
const kLOG_ERR = 0x3
const kLOG_WARNING = 0x4
const kLOG_INFO = 0x6
const kLOG_DEBUG = 0x7

type macosLogger struct{}

func (l macosLogger) log(facility int, msg string) {
	C.doLog(C.int(facility|kLOG_DAEMON), C.CString(msg))
}

func (l macosLogger) Debug(v ...interface{}) {
	l.log(kLOG_DEBUG, fmt.Sprint(v...))
}

func (l macosLogger) Debugf(format string, a ...interface{}) {
	l.log(kLOG_DEBUG, fmt.Sprintf(format, a...))
}

func (l macosLogger) Info(v ...interface{}) {
	l.log(kLOG_INFO, fmt.Sprint(v...))
}

func (l macosLogger) Infof(format string, a ...interface{}) {
	l.log(kLOG_INFO, fmt.Sprintf(format, a...))
}

func (l macosLogger) Warning(v ...interface{}) {
	l.log(kLOG_WARNING, fmt.Sprint(v...))
}

func (l macosLogger) Warningf(format string, a ...interface{}) {
	l.log(kLOG_WARNING, fmt.Sprintf(format, a...))
}

func (l macosLogger) Error(v ...interface{}) {
	l.log(kLOG_ERR, fmt.Sprint(v...))
}

func (l macosLogger) Errorf(format string, a ...interface{}) {
	l.log(kLOG_ERR, fmt.Sprintf(format, a...))
}

func newServiceLogger(name string) (Logger, error) {
	return macosLogger{}, nil
}

func ReadLog(process string) ([]byte, error) {
	return exec.Command("log", "show", "--info", "--debug", "--process", process, "--no-pager", "--style", "syslog").Output()
}

func FollowLog(process string) error {
	cmd := exec.Command("log", "stream", "--level", "debug", "--process", process, "--style", "syslog")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}
