package host

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"
)

type windowsLogger struct {
	log debug.Log
}

func NewConsoleLogger(name string) Logger {
	return windowsLogger{log: debug.New(name)}
}

func newServiceLogger(name string) (Logger, error) {
	err := eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		if !strings.Contains(err.Error(), "exists") {
			return nil, err
		}
	}
	el, err := eventlog.Open(name)
	if err != nil {
		return nil, err
	}
	return windowsLogger{log: el}, nil
}

func (l windowsLogger) Debug(v ...interface{}) {
	_ = l.log.Info(1, fmt.Sprint(v...))
}

func (l windowsLogger) Debugf(format string, a ...interface{}) {
	_ = l.log.Info(1, fmt.Sprintf(format, a...))
}

func (l windowsLogger) Info(v ...interface{}) {
	_ = l.log.Info(1, fmt.Sprint(v...))
}

func (l windowsLogger) Infof(format string, a ...interface{}) {
	_ = l.log.Info(1, fmt.Sprintf(format, a...))
}

func (l windowsLogger) Warning(v ...interface{}) {
	l.log.Warning(2, fmt.Sprint(v...))
}

func (l windowsLogger) Warningf(format string, a ...interface{}) {
	l.log.Warning(2, fmt.Sprintf(format, a...))
}

func (l windowsLogger) Error(v ...interface{}) {
	l.log.Error(3, fmt.Sprint(v...))
}

func (l windowsLogger) Errorf(format string, a ...interface{}) {
	l.log.Error(3, fmt.Sprintf(format, a...))
}

func ReadLog(name string) ([]byte, error) {
	events, err := queryWindowsEvents(name, 200)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].RecordID < events[j].RecordID
	})
	var b bytes.Buffer
	for i, e := range events {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(e.Text)
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}

func FollowLog(name string) error {
	lastID := uint64(0)
	events, err := queryWindowsEvents(name, 1)
	if err != nil {
		return err
	}
	if len(events) > 0 {
		lastID = events[0].RecordID
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		events, err := queryWindowsEvents(name, 50)
		if err != nil {
			return err
		}
		var newEvents []windowsEvent
		for _, e := range events {
			if e.RecordID > lastID {
				newEvents = append(newEvents, e)
			}
		}
		if len(newEvents) == 0 {
			continue
		}
		sort.Slice(newEvents, func(i, j int) bool {
			return newEvents[i].RecordID < newEvents[j].RecordID
		})
		for _, e := range newEvents {
			if _, err := fmt.Fprintln(os.Stdout, e.Text); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(os.Stdout); err != nil {
				return err
			}
			lastID = e.RecordID
		}
	}
	return nil
}

type windowsEvent struct {
	RecordID uint64
	Text     string
}

func queryWindowsEvents(name string, count int) ([]windowsEvent, error) {
	if _, err := exec.LookPath("wevtutil"); err != nil {
		return nil, fmt.Errorf("Windows Event Log utility not found")
	}
	if count <= 0 {
		count = 1
	}
	q := fmt.Sprintf(`*[System[Provider[@Name="%s"]]]`, strings.ReplaceAll(name, `"`, `\"`))
	args := []string{
		"qe", "Application",
		"/q:" + q,
		"/f:text",
		"/rd:true",
		fmt.Sprintf("/c:%d", count),
	}
	out, err := exec.Command("wevtutil", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return nil, fmt.Errorf("query windows logs: %s", msg)
		}
		return nil, fmt.Errorf("query windows logs: %w", err)
	}
	return parseWindowsEventText(string(out)), nil
}

func parseWindowsEventText(out string) []windowsEvent {
	var blocks []string
	var current []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "Event[") && strings.HasSuffix(line, "]:") {
			if len(current) > 0 {
				blocks = append(blocks, strings.TrimSpace(strings.Join(current, "\n")))
			}
			current = []string{line}
			continue
		}
		if len(current) > 0 {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		blocks = append(blocks, strings.TrimSpace(strings.Join(current, "\n")))
	}

	events := make([]windowsEvent, 0, len(blocks))
	for _, block := range blocks {
		e := windowsEvent{Text: block}
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "Event Record ID:") {
				continue
			}
			id, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "Event Record ID:")), 10, 64)
			if err == nil {
				e.RecordID = id
			}
			break
		}
		events = append(events, e)
	}
	return events
}
