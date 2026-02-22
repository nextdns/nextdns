//go:build windows
// +build windows

package host

import (
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wtsUserName      = 5
	activeUserNoSess = 0xFFFFFFFF
)

const activeUserCacheTTL = 2 * time.Second

var (
	activeUserCache = newRefreshingStringCache(activeUserCacheTTL)

	kernel32                         = windows.NewLazySystemDLL("kernel32.dll")
	wtsapi32                         = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSGetActiveConsoleSessionID = kernel32.NewProc("WTSGetActiveConsoleSessionId")
	procWTSQuerySessionInformationW  = wtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory                = wtsapi32.NewProc("WTSFreeMemory")
)

// ActiveUser returns the active interactive username on Windows.
func ActiveUser() string {
	return activeUserCache.Get(activeUserUncached)
}

func activeUserUncached() string {
	sessionID, _, _ := procWTSGetActiveConsoleSessionID.Call()
	if uint32(sessionID) == activeUserNoSess {
		return ""
	}
	user, err := wtsQuerySessionInfo(uint32(sessionID), wtsUserName)
	if err != nil {
		return ""
	}
	return user
}

func wtsQuerySessionInfo(sessionID uint32, infoClass uint32) (string, error) {
	var buf *uint16
	var size uint32
	r1, _, err := procWTSQuerySessionInformationW.Call(
		0,
		uintptr(sessionID),
		uintptr(infoClass),
		uintptr(unsafe.Pointer(&buf)),
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 {
		return "", err
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(buf)))
	if buf == nil || size <= 2 {
		return "", nil
	}
	return strings.TrimSpace(windows.UTF16PtrToString(buf)), nil
}
