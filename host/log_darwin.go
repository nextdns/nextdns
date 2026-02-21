package host

import (
	"fmt"
	"os"
	"os/exec"
	"unsafe"
)

//#include <syslog.h>
//#include <os/log.h>
//#include <stdlib.h>
//void doLog(int severity, const char *msg) {
//	syslog(severity|LOG_DAEMON, "%s", msg);
//	os_log_type_t t = OS_LOG_TYPE_DEFAULT;
//	switch (severity) {
//	case LOG_DEBUG:
//		t = OS_LOG_TYPE_DEBUG;
//		break;
//	case LOG_INFO:
//		t = OS_LOG_TYPE_INFO;
//		break;
//	case LOG_ERR:
//		t = OS_LOG_TYPE_ERROR;
//		break;
//	default:
//		t = OS_LOG_TYPE_DEFAULT;
//	}
//	os_log_with_type(OS_LOG_DEFAULT, t, "%{public}s", msg);
//}
import "C"

const kLOG_DAEMON = 0x18
const kLOG_ERR = 0x3
const kLOG_WARNING = 0x4
const kLOG_INFO = 0x6
const kLOG_DEBUG = 0x7

type macosLogger struct{}

func (l macosLogger) log(facility int, msg string) {
	cmsg := C.CString(msg)
	defer C.free(unsafe.Pointer(cmsg))
	C.doLog(C.int(facility), cmsg)
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
	predicate := fmt.Sprintf(`process == "%[1]s" OR sender == "%[1]s"`, process)
	return exec.Command("log", "show", "--info", "--debug", "--predicate", predicate, "--no-pager", "--style", "syslog").Output()
}

func FollowLog(process string) error {
	predicate := fmt.Sprintf(`process == "%[1]s" OR sender == "%[1]s"`, process)
	cmd := exec.Command("log", "stream", "--level", "debug", "--predicate", predicate, "--style", "syslog")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}
