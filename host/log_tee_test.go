package host

import (
	"fmt"
	"sync"
	"testing"
)

type recordingLogger struct {
	mu   sync.Mutex
	msgs []string
}

func (l *recordingLogger) record(v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, fmt.Sprint(v...))
}

func (l *recordingLogger) recordf(format string, a ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, fmt.Sprintf(format, a...))
}

func (l *recordingLogger) Debug(v ...interface{})                 { l.record(v...) }
func (l *recordingLogger) Debugf(format string, a ...interface{}) { l.recordf(format, a...) }
func (l *recordingLogger) Info(v ...interface{})                  { l.record(v...) }
func (l *recordingLogger) Infof(format string, a ...interface{})  { l.recordf(format, a...) }
func (l *recordingLogger) Warning(v ...interface{})               { l.record(v...) }
func (l *recordingLogger) Warningf(format string, a ...interface{}) {
	l.recordf(format, a...)
}
func (l *recordingLogger) Error(v ...interface{})                 { l.record(v...) }
func (l *recordingLogger) Errorf(format string, a ...interface{}) { l.recordf(format, a...) }

func (l *recordingLogger) messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	cp := make([]string, len(l.msgs))
	copy(cp, l.msgs)
	return cp
}

func TestTeeLogger(t *testing.T) {
	a := &recordingLogger{}
	b := &recordingLogger{}
	tee := NewTeeLogger(a, b)

	tee.Info("hello")

	if msgs := a.messages(); len(msgs) != 1 || msgs[0] != "hello" {
		t.Errorf("logger a: got %v, want [hello]", msgs)
	}
	if msgs := b.messages(); len(msgs) != 1 || msgs[0] != "hello" {
		t.Errorf("logger b: got %v, want [hello]", msgs)
	}
}

func TestTeeLogger_AllMethods(t *testing.T) {
	a := &recordingLogger{}
	b := &recordingLogger{}
	tee := NewTeeLogger(a, b)

	tee.Debug("d")
	tee.Debugf("df %d", 1)
	tee.Info("i")
	tee.Infof("if %d", 2)
	tee.Warning("w")
	tee.Warningf("wf %d", 3)
	tee.Error("e")
	tee.Errorf("ef %d", 4)

	want := []string{"d", "df 1", "i", "if 2", "w", "wf 3", "e", "ef 4"}
	for _, lg := range []*recordingLogger{a, b} {
		msgs := lg.messages()
		if len(msgs) != len(want) {
			t.Fatalf("got %d messages, want %d: %v", len(msgs), len(want), msgs)
		}
		for i, m := range msgs {
			if m != want[i] {
				t.Errorf("msg[%d] = %q, want %q", i, m, want[i])
			}
		}
	}
}
