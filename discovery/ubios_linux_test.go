package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testUbiosMAC = "00:00:5e:00:53:01"

type ubiosQueryFunc = func(context.Context) (map[string][]string, error)

func stubUbios(t *testing.T, socket bool, pg, mongo ubiosQueryFunc) {
	t.Helper()
	oldSocket, oldPG, oldMongo := ubiosPostgresSocket, ubiosQueryPostgres, ubiosQueryMongo
	t.Cleanup(func() {
		ubiosPostgresSocket, ubiosQueryPostgres, ubiosQueryMongo = oldSocket, oldPG, oldMongo
	})
	ubiosPostgresSocket = filepath.Join(t.TempDir(), ".s.PGSQL.5434")
	if socket {
		if err := os.WriteFile(ubiosPostgresSocket, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ubiosQueryPostgres, ubiosQueryMongo = pg, mongo
}

func ubiosNames(name string) map[string][]string {
	return map[string][]string{testUbiosMAC: {name}}
}

func newTestUbios() *Ubios {
	r := &Ubios{supported: true}
	r.once.Do(func() {})
	return r
}

func expireUbios(r *Ubios) {
	r.mu.Lock()
	r.expires = time.Time{}
	r.mu.Unlock()
}

func TestUbios_backend(t *testing.T) {
	for _, socket := range []bool{false, true} {
		t.Run(fmt.Sprint(socket), func(t *testing.T) {
			var pgCalls, mongoCalls int
			stubUbios(t, socket,
				func(context.Context) (map[string][]string, error) { pgCalls++; return ubiosNames("pg"), nil },
				func(context.Context) (map[string][]string, error) { mongoCalls++; return ubiosNames("mongo"), nil })
			r := newTestUbios()
			got, err := r.clientList(context.Background())
			want := "mongo"
			if socket {
				want = "pg"
			}
			if err != nil || !reflect.DeepEqual(got, ubiosNames(want)) || pgCalls+mongoCalls != 1 {
				t.Fatalf("names=%v err=%v pg=%d mongo=%d", got, err, pgCalls, mongoCalls)
			}
			// Legacy systems can migrate while NextDNS stays running.
			if !socket {
				if err := os.WriteFile(ubiosPostgresSocket, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				got, err = r.clientList(context.Background())
				if err != nil || !reflect.DeepEqual(got, ubiosNames("pg")) {
					t.Fatalf("migration: names=%v err=%v", got, err)
				}
			}
		})
	}
}

func TestUbios_postgresRemainsAuthoritative(t *testing.T) {
	// A socket is sufficient evidence even if the first query fails. Do not
	// consult stale MongoDB during a later PostgreSQL restart.
	for _, firstFails := range []bool{false, true} {
		t.Run(fmt.Sprint(firstFails), func(t *testing.T) {
			fail := errors.New("database unavailable")
			pgErr := error(nil)
			if firstFails {
				pgErr = fail
			}
			stubUbios(t, true,
				func(context.Context) (map[string][]string, error) { return ubiosNames("pg"), pgErr },
				func(context.Context) (map[string][]string, error) { t.Error("stale MongoDB queried"); return nil, nil })
			r := newTestUbios()
			r.clientList(context.Background())
			if err := os.Remove(ubiosPostgresSocket); err != nil {
				t.Fatal(err)
			}
			pgErr = fail
			if got, err := r.clientList(context.Background()); got != nil || !errors.Is(err, fail) {
				t.Fatalf("names=%v err=%v", got, err)
			}
			pgErr = nil
			if got, err := r.clientList(context.Background()); err != nil || !reflect.DeepEqual(got, ubiosNames("pg")) {
				t.Fatalf("recovery: names=%v err=%v", got, err)
			}
		})
	}
}

func TestUbios_socketCheckFailure(t *testing.T) {
	unexpected := func(context.Context) (map[string][]string, error) {
		t.Error("database queried after failed stat")
		return nil, nil
	}
	stubUbios(t, false, unexpected, unexpected)
	// A symlink loop reliably produces a stat error even when tests run as root.
	if err := os.Symlink(ubiosPostgresSocket, ubiosPostgresSocket); err != nil {
		t.Fatal(err)
	}
	if _, err := newTestUbios().clientList(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "postgres socket:") {
		t.Fatalf("err=%v", err)
	}
}

func Test_parseClientList(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		want        map[string][]string
		invalid     bool
	}{
		{"empty", " \n", map[string][]string{}, false},
		{"records", `{"mac":"00:00:5E:00:53:01","name":"Office"}
{"mac":"00:00:5e:00:53:01","name":"Office"}
{"mac":"00:00:5e:00:53:01","name":"Other"}`, map[string][]string{testUbiosMAC: {"Office", "Other"}}, false},
		{"null and missing", `{"mac":"00:00:5e:00:53:01","name":"Office"}
{"mac":null,"name":"orphan"}
{"name":"orphan"}
{"mac":"00:00:5e:00:53:02","name":null}
{"mac":"00:00:5e:00:53:03"}
null`, ubiosNames("Office"), false},
		{"malformed", `{"mac":"00:00:5e:00:53:01","name":"private"} warning`, nil, true},
		{"truncated", `{"mac":"00:00:5e:00:53:01","name":"private"`, nil, true},
		{"wrong type", `{"mac":42,"name":"private"}`, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseClientList([]byte(tt.input))
			if (err != nil) != tt.invalid || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("names=%v err=%v", got, err)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("error exposed client data")
			}
		})
	}
}

func TestUbios_refresh(t *testing.T) {
	var calls, reports int
	result, queryErr := ubiosNames("old"), error(nil)
	stubUbios(t, false, nil, func(ctx context.Context) (map[string][]string, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("load has no deadline")
		}
		return result, queryErr
	})
	r := newTestUbios()
	r.OnError = func(error) {
		reports++
		// Both the error hook and Visit callbacks may re-enter discovery.
		r.LookupMAC(testUbiosMAC)
	}
	before := time.Now()
	if got := r.LookupMAC(testUbiosMAC); !reflect.DeepEqual(got, []string{"old"}) {
		t.Fatal(got)
	}
	if r.expires.Before(before.Add(ubiosRefreshInterval)) {
		t.Fatal("successful load did not use normal interval")
	}
	r.LookupMAC(testUbiosMAC)
	if calls != 1 {
		t.Fatal("unexpired cache reloaded")
	}
	// Parse failure, missing database, and command failure retain the full cache.
	for _, err := range []error{errors.New("invalid client list JSON"), errNoUbiosDatabase, errors.New("command failed")} {
		result, queryErr = nil, err
		oldReports := reports
		for range 2 {
			expireUbios(r)
			before := time.Now()
			if got := r.LookupMAC(testUbiosMAC); !reflect.DeepEqual(got, []string{"old"}) {
				t.Fatalf("cache lost after %v: %v", err, got)
			}
			if r.expires.Before(before.Add(ubiosRetryInterval)) || r.expires.After(time.Now().Add(ubiosRetryInterval)) {
				t.Fatal("failure did not use fixed retry interval")
			}
		}
		if reports != oldReports+1 {
			t.Fatal("repeated error was not deduplicated")
		}
	}
	result, queryErr = ubiosNames("new"), nil
	expireUbios(r)
	r.Visit(func(name string, macs []string) {
		if name != "new" {
			t.Fatal(name)
		}
		expireUbios(r)
		r.LookupMAC(testUbiosMAC)
	})
	if r.lastErr != "" {
		t.Fatal("successful load did not reset the error")
	}
	oldReports := reports
	queryErr = errors.New("command failed")
	expireUbios(r)
	r.LookupMAC(testUbiosMAC)
	if reports != oldReports+1 {
		t.Fatal("error after recovery not reported")
	}
}

func TestUbios_noDatabaseAtStartup(t *testing.T) {
	var queryErr error = errNoUbiosDatabase
	stubUbios(t, false, nil, func(context.Context) (map[string][]string, error) {
		if queryErr != nil {
			return nil, queryErr
		}
		return map[string][]string{}, nil
	})
	r := newTestUbios()
	var reports int
	r.OnError = func(error) { reports++ }
	for range 3 {
		expireUbios(r)
		r.refresh()
	}
	if reports != 0 {
		t.Fatal("no-database console logged an error")
	}
	queryErr = nil
	expireUbios(r)
	r.refresh()
	queryErr = errNoUbiosDatabase
	expireUbios(r)
	r.refresh()
	if reports != 1 {
		t.Fatal("missing database after an empty successful load was not reported")
	}
}

func TestUbios_panicReleasesLoader(t *testing.T) {
	for _, where := range []string{"query", "callback"} {
		t.Run(where, func(t *testing.T) {
			stubUbios(t, false, nil, func(context.Context) (map[string][]string, error) {
				if where == "query" {
					panic("query")
				}
				return nil, errors.New("failure")
			})
			r := newTestUbios()
			r.OnError = func(error) { panic("callback") }
			func() {
				defer func() {
					if recover() == nil {
						t.Error("panic was swallowed")
					}
				}()
				r.refresh()
			}()
			if r.loading {
				t.Fatal("loader remains claimed")
			}
			ubiosQueryMongo = func(context.Context) (map[string][]string, error) { return ubiosNames("recovered"), nil }
			expireUbios(r)
			if got := r.LookupMAC(testUbiosMAC); !reflect.DeepEqual(got, []string{"recovered"}) {
				t.Fatal(got)
			}
		})
	}
}

func TestUbios_concurrentRefresh(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	stubUbios(t, false, nil, func(context.Context) (map[string][]string, error) {
		calls.Add(1)
		close(started)
		<-release
		return ubiosNames("new"), nil
	})
	r := newTestUbios()
	r.macs = ubiosNames("old")
	loaderDone := make(chan struct{})
	go func() { defer close(loaderDone); r.refresh() }()
	<-started
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := r.LookupMAC(testUbiosMAC); !reflect.DeepEqual(got, []string{"old"}) {
				t.Errorf("names=%v", got)
			}
			r.Visit(func(string, []string) {})
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(release)
		<-loaderDone
		<-done
		t.Fatal("concurrent lookups blocked on database")
	}
	finished := time.Now()
	close(release)
	<-loaderDone
	if calls.Load() != 1 || r.expires.Before(finished.Add(ubiosRefreshInterval)) {
		t.Fatalf("calls=%d expires=%v", calls.Load(), r.expires)
	}
	if got := r.LookupMAC(testUbiosMAC); !reflect.DeepEqual(got, []string{"new"}) {
		t.Fatal(got)
	}
}

func TestUbios_claimLoadOnce(t *testing.T) {
	r := newTestUbios()
	for range 100 {
		r.loading = false
		var count atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range 30 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if r.claimLoad() {
					count.Add(1)
				}
			}()
		}
		close(start)
		wg.Wait()
		if count.Load() != 1 {
			t.Fatalf("%d loaders", count.Load())
		}
	}
}

func writeUbiosScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "database client")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func Test_queryUbiosPostgres(t *testing.T) {
	old := ubiosPsqlPath
	t.Cleanup(func() { ubiosPsqlPath = old })
	capture := filepath.Join(t.TempDir(), "args")
	t.Setenv("CAPTURE", capture)
	// Inherited options must not override the read-only setting or timeouts.
	t.Setenv("PGOPTIONS", "-c default_transaction_read_only=off")
	t.Setenv("PGCONNECT_TIMEOUT", "0")
	ubiosPsqlPath = writeUbiosScript(t, `printf '%s\n' "$@" > "$CAPTURE"
printf '%s\n' "$PGOPTIONS" "$PGCONNECT_TIMEOUT" >> "$CAPTURE"
echo '{"mac":"00:00:5e:00:53:01","name":"Office"}'`)
	got, err := queryUbiosPostgres(context.Background())
	if err != nil || !reflect.DeepEqual(got, ubiosNames("Office")) {
		t.Fatalf("names=%v err=%v", got, err)
	}
	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"-X\n-q\n-A\n-t\n-w\n-v\nON_ERROR_STOP=1\n",
		"-h\n/run/postgresql\n-p\n5434\n-U\nunifi_network_admin\n-d\nunifi-network\n-c\n",
		"FROM config.user_client WHERE name <> ''",
		"-c default_transaction_read_only=on -c statement_timeout=5000 -c lock_timeout=2000\n5\n",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("command lacks %q", want)
		}
	}
}

func Test_queryUbiosMongo(t *testing.T) {
	oldAddr, oldPath := ubiosMongoAddr, ubiosMongoPath
	t.Cleanup(func() { ubiosMongoAddr, ubiosMongoPath = oldAddr, oldPath })
	capture := filepath.Join(t.TempDir(), "args")
	t.Setenv("CAPTURE", capture)
	ubiosMongoPath = writeUbiosScript(t, `printf '%s\n' "$@" > "$CAPTURE"
echo '{"mac":"00:00:5e:00:53:01","name":"Office"}'`)
	ubiosMongoAddr = "127.0.0.1:0"
	if _, err := queryUbiosMongo(context.Background()); !errors.Is(err, errNoUbiosDatabase) {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(capture); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("mongo shell ran without a database")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ubiosMongoAddr = l.Addr().String()
	got, err := queryUbiosMongo(context.Background())
	if err != nil || !reflect.DeepEqual(got, ubiosNames("Office")) {
		t.Fatalf("names=%v err=%v", got, err)
	}
	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{ubiosMongoAddr + "/ace\n--quiet\n--eval\n", `db.user.find({name: {$exists: true, $ne: ""}}, {_id:0, mac:1, name:1})`, ".forEach(function(client) { printjsononeline(client); });"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("command lacks %q", want)
		}
	}
}

func Test_runUbiosQuery_errors(t *testing.T) {
	for _, tt := range []struct{ name, script, want string }{
		{"stderr", "echo 'psql: error: unavailable' >&2; exit 2", "exit status 2: psql: error: unavailable"},
		{"wrapper warnings", "printf 'perl: warning: locale\n    continuation\npsql: error: unavailable\n' >&2; exit 2", "exit status 2: psql: error: unavailable"},
		{"no stderr", "exit 2", "exit status 2"},
		{"partial output", `echo '{"mac":"00:00:5e:00:53:01","name":"private"}'; echo broken`, "invalid client list JSON"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runUbiosQuery(context.Background(), exec.Command(writeUbiosScript(t, tt.script)))
			if got != nil || err == nil || err.Error() != tt.want {
				t.Fatalf("names=%v err=%v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runUbiosQuery(ctx, exec.CommandContext(ctx, "/nonexistent")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func Test_runUbiosQuery_timeout(t *testing.T) {
	for _, body := range []string{"exec sleep 30", "sleep 3 &\nexec sleep 30"} {
		t.Run(body, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := runUbiosQuery(ctx, exec.CommandContext(ctx, writeUbiosScript(t, body)))
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("err=%v", err)
			}
			if time.Since(start) > 2500*time.Millisecond {
				t.Fatal("command or inherited pipe exceeded deadline allowance")
			}
		})
	}
}
