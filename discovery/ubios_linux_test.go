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

func Test_errorLatch(t *testing.T) {
	errA := errors.New("postgres: exit status 2")
	errB := errors.New("postgres: exit status 1")
	type result struct{ report, recovered error }
	var l errorLatch
	var got []result
	for _, err := range []error{nil, errA, errA, errA, nil, nil, errA, errB, errB} {
		report, recovered := l.update(err)
		got = append(got, result{report, recovered})
	}
	want := []result{
		{nil, nil},
		{errA, nil},
		{nil, nil},
		{nil, nil},
		{nil, errA},
		{nil, nil},
		{errA, nil},
		{errB, nil},
		{nil, nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("update() = %v, want %v", got, want)
	}
}

func Test_cmdError(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   string
	}{
		{"First line", `echo 'psql: error: connection failed' >&2; echo second >&2`, "exit status 2: psql: error: connection failed"},
		{"Perl locale warning first", `printf 'perl: warning: Setting locale failed.\nperl: warning: Please check that your locale settings:\n\tLANG = "xx_XX"\n    are supported and installed on your system.\nperl: warning: Falling back to the standard locale ("C").\npsql: error: database missing\n' >&2`, "exit status 2: psql: error: database missing"},
		{"SQL error with detail", `printf 'ERROR:  relation "config.user_client" does not exist\nLINE 2: FROM config.user_client\n             ^\n' >&2`, `exit status 2: ERROR:  relation "config.user_client" does not exist`},
		{"Empty stderr", `true`, "exit status 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := exec.Command("sh", "-c", tt.stderr+"; exit 2").Output()
			got := cmdError(err)
			if got.Error() != tt.want {
				t.Errorf("cmdError() = %q, want %q", got, tt.want)
			}
			var ee *exec.ExitError
			if !errors.As(got, &ee) {
				t.Error("cmdError() does not wrap *exec.ExitError")
			}
		})
	}

	_, err := exec.Command("/nonexistent/binary").Output()
	if got := cmdError(err).Error(); !strings.Contains(got, "no such file") {
		t.Errorf("cmdError() = %q", got)
	}
}

func Test_parseClientList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string][]string
	}{
		{"Empty", "", map[string][]string{}},
		{
			// Output of psql -A -t with json_build_object (UniFi Network 11+).
			"Postgres",
			`{"mac" : "00:00:5e:00:53:01", "name" : "Media Server"}
{"mac" : "00:00:5E:00:53:02", "name" : "Laptop (Wireless)"}
`,
			map[string][]string{
				"00:00:5e:00:53:01": {"Media Server"},
				"00:00:5e:00:53:02": {"Laptop (Wireless)"},
			},
		},
		{
			// Output of the mongo shell (UniFi Network 10 and earlier).
			"Mongo",
			`{ "mac" : "00:00:5e:00:53:01", "name" : "Media Server" }
{ "mac" : "00:00:5e:00:53:01", "name" : "Media Server" }
{ "mac" : "00:00:5e:00:53:03", "name" : "Phone" }
`,
			map[string][]string{
				"00:00:5e:00:53:01": {"Media Server"},
				"00:00:5e:00:53:03": {"Phone"},
			},
		},
		{
			// A null or missing field must not take the value of the
			// previous record.
			"Null fields",
			`{"mac" : "00:00:5e:00:53:01", "name" : "Media Server"}
{"mac" : null, "name" : "Orphan"}
{"name" : "No MAC"}
{"mac" : "00:00:5e:00:53:03", "name" : null}
`,
			map[string][]string{"00:00:5e:00:53:01": {"Media Server"}},
		},
		{
			"Malformed",
			`{"mac" : "00:00:5e:00:53:01", "name" : "Media Server"}
connect failed
{"mac" : "00:00:5e:00:53:03", "name" : "Phone"}
`,
			map[string][]string{"00:00:5e:00:53:01": {"Media Server"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseClientList([]byte(tt.input)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseClientList() = %v, want %v", got, tt.want)
			}
		})
	}
}

type queryFunc = func(context.Context) (map[string][]string, error)

// stubUbios replaces the socket path and the two queries for one test.
func stubUbios(t *testing.T, socket bool, pg, mongo queryFunc) {
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

// counted returns a query that records each call and returns the name.
func counted(calls *atomic.Int32, name string) queryFunc {
	return func(context.Context) (map[string][]string, error) {
		calls.Add(1)
		return map[string][]string{"00:00:5e:00:53:01": {name}}, nil
	}
}

func failing(calls *atomic.Int32, err error) queryFunc {
	return func(context.Context) (map[string][]string, error) {
		calls.Add(1)
		return nil, err
	}
}

// mustNotRun fails the test if the query runs.
func mustNotRun(t *testing.T, which string) queryFunc {
	return func(context.Context) (map[string][]string, error) {
		t.Errorf("%s query ran", which)
		return nil, errors.New("must not run")
	}
}

func Test_ubiosClientList(t *testing.T) {
	// pg and mongo say what each query returns: "" means it must not run.
	tests := []struct {
		name      string
		socket    bool
		pg, mongo string
		wantName  string
		wantErr   string
	}{
		{"Network 10", false, "", "names", "mongo", ""},
		{"No database", false, "", "no database", "", "no PostgreSQL socket "},
		{"Network 11", true, "names", "", "pg", ""},
		{"Network 11, postgres fails", true, "fail", "", "", "postgres: exit status 2"},
	}
	query := func(t *testing.T, calls *atomic.Int32, which, mode string) queryFunc {
		switch mode {
		case "names":
			return counted(calls, which)
		case "fail":
			return failing(calls, errors.New("exit status 2"))
		case "no database":
			return failing(calls, noDatabaseError{errors.New("connection refused")})
		}
		return mustNotRun(t, which)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pgCalls, mongoCalls atomic.Int32
			stubUbios(t, tt.socket, query(t, &pgCalls, "pg", tt.pg), query(t, &mongoCalls, "mongo", tt.mongo))
			macs, err := ubiosClientList(context.Background())
			for _, c := range []struct {
				which string
				mode  string
				calls *atomic.Int32
			}{{"pg", tt.pg, &pgCalls}, {"mongo", tt.mongo, &mongoCalls}} {
				if want := map[bool]int32{true: 1, false: 0}[c.mode != ""]; c.calls.Load() != want {
					t.Errorf("%s queries = %d, want %d", c.which, c.calls.Load(), want)
				}
			}
			if tt.wantErr != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got := macs["00:00:5e:00:53:01"]; !reflect.DeepEqual(got, []string{tt.wantName}) {
				t.Errorf("names = %v, want [%s]", got, tt.wantName)
			}
		})
	}
}

func newTestUbios() *Ubios {
	r := &Ubios{}
	r.once.Do(func() {}) // skip isUnifi
	r.supported = true
	return r
}

func TestUbios_refresh(t *testing.T) {
	var calls atomic.Int32
	var logged, recovered []string
	r := newTestUbios()
	r.OnError = func(err error) { logged = append(logged, err.Error()) }
	r.OnRecover = func(err error) { recovered = append(recovered, err.Error()) }

	stubUbios(t, true, counted(&calls, "pg"), mustNotRun(t, "mongo"))
	if got := r.LookupMAC("00:00:5e:00:53:01"); !reflect.DeepEqual(got, []string{"pg"}) {
		t.Fatalf("LookupMAC() = %v", got)
	}
	r.LookupMAC("00:00:5e:00:53:01")
	if n := calls.Load(); n != 1 {
		t.Fatalf("queries before expiry = %d, want 1", n)
	}

	// A failure keeps the names, logs once and retries after 30 s.
	ubiosQueryPostgres = failing(&calls, errors.New("exit status 2"))
	for range 3 {
		r.expires = time.Time{}
		before := time.Now()
		if got := r.LookupMAC("00:00:5e:00:53:01"); !reflect.DeepEqual(got, []string{"pg"}) {
			t.Fatalf("names after failure = %v, want the last names", got)
		}
		if r.expires.Sub(before) < ubiosRetryMin {
			t.Fatalf("retry delay = %v, want at least %v", r.expires.Sub(before), ubiosRetryMin)
		}
	}
	if !reflect.DeepEqual(logged, []string{"clientList: postgres: exit status 2"}) {
		t.Errorf("logged = %q", logged)
	}

	// Recovery: one recovery call, normal interval.
	ubiosQueryPostgres = counted(&calls, "pg2")
	r.expires = time.Time{}
	if got := r.LookupMAC("00:00:5e:00:53:01"); !reflect.DeepEqual(got, []string{"pg2"}) {
		t.Errorf("names after recovery = %v", got)
	}
	if !reflect.DeepEqual(recovered, []string{"postgres: exit status 2"}) || r.retry != 0 {
		t.Errorf("recovered = %q, retry = %v", recovered, r.retry)
	}
}

func TestUbios_noDatabaseIsSilent(t *testing.T) {
	var calls atomic.Int32
	r := newTestUbios()
	r.OnError = func(err error) { t.Errorf("OnError(%v) for a device without a database", err) }
	r.OnRecover = func(err error) { t.Errorf("OnRecover(%v) after a device without a database", err) }
	stubUbios(t, false, mustNotRun(t, "postgres"), failing(&calls, fmt.Errorf("%w (connection refused)", errNoUbiosDatabase)))
	for range 3 {
		r.expires = time.Time{}
		if got := r.LookupMAC("00:00:5e:00:53:01"); got != nil {
			t.Fatalf("LookupMAC() = %v", got)
		}
	}
	// The database appears, for example after boot: names, and no log line.
	stubUbios(t, true, counted(&calls, "pg"), mustNotRun(t, "mongo"))
	r.expires = time.Time{}
	if got := r.LookupMAC("00:00:5e:00:53:01"); !reflect.DeepEqual(got, []string{"pg"}) {
		t.Errorf("LookupMAC() = %v", got)
	}
}

// After names were loaded, a database that stays away for more than one retry
// (for example a stopped PostgreSQL cluster) is a fault: it is reported once,
// and so is the recovery.
func TestUbios_noDatabaseAfterNamesIsReported(t *testing.T) {
	var calls atomic.Int32
	var logged, recovered []string
	r := newTestUbios()
	r.OnError = func(err error) { logged = append(logged, err.Error()) }
	r.OnRecover = func(err error) { recovered = append(recovered, err.Error()) }
	stubUbios(t, true, counted(&calls, "pg"), mustNotRun(t, "mongo"))
	r.LookupMAC("00:00:5e:00:53:01")

	stubUbios(t, false, mustNotRun(t, "postgres"), failing(&calls, noDatabaseError{errors.New("connection refused")}))
	for i := range 3 {
		r.expires = time.Time{}
		if got := r.LookupMAC("00:00:5e:00:53:01"); !reflect.DeepEqual(got, []string{"pg"}) {
			t.Fatalf("LookupMAC() = %v, want the last names", got)
		}
		if i == 0 && len(logged) != 0 {
			t.Fatalf("logged after one load without a database: %q", logged)
		}
	}
	if len(logged) != 1 || !strings.HasPrefix(logged[0], "clientList: no PostgreSQL socket ") ||
		!strings.HasSuffix(logged[0], ", and no UniFi Network database: connection refused") {
		t.Errorf("logged = %q, want one line that names both checks", logged)
	}
	stubUbios(t, true, counted(&calls, "pg"), mustNotRun(t, "mongo"))
	r.expires = time.Time{}
	r.LookupMAC("00:00:5e:00:53:01")
	if len(recovered) != 1 {
		t.Errorf("recovered = %q, want one call", recovered)
	}
}

// A database that is gone for one load only (a short restart) is not
// reported, and so there is no recovery report either.
func TestUbios_shortDatabaseGapIsSilent(t *testing.T) {
	var calls atomic.Int32
	r := newTestUbios()
	r.OnError = func(err error) { t.Errorf("OnError(%v) for a short gap", err) }
	r.OnRecover = func(err error) { t.Errorf("OnRecover(%v) after a short gap", err) }
	stubUbios(t, true, counted(&calls, "pg"), mustNotRun(t, "mongo"))
	r.LookupMAC("00:00:5e:00:53:01")
	stubUbios(t, false, mustNotRun(t, "postgres"), failing(&calls, noDatabaseError{errors.New("connection refused")}))
	r.expires = time.Time{}
	r.LookupMAC("00:00:5e:00:53:01")
	stubUbios(t, true, counted(&calls, "pg"), mustNotRun(t, "mongo"))
	r.expires = time.Time{}
	r.LookupMAC("00:00:5e:00:53:01")
	if r.noDatabase != 0 {
		t.Errorf("noDatabase = %d after a success, want 0", r.noDatabase)
	}
}

// A panic in the load becomes a reported error, and later loads still run.
func TestUbios_panicInLoad(t *testing.T) {
	var logged []string
	r := newTestUbios()
	r.OnError = func(err error) { logged = append(logged, err.Error()) }
	stubUbios(t, true, func(context.Context) (map[string][]string, error) { panic("boom") }, mustNotRun(t, "mongo"))
	r.LookupMAC("00:00:5e:00:53:01")
	if !reflect.DeepEqual(logged, []string{"clientList: panic: boom"}) {
		t.Errorf("logged = %q", logged)
	}
	if r.loading {
		t.Fatal("loading flag still set after a panic")
	}
	var calls atomic.Int32
	ubiosQueryPostgres = counted(&calls, "pg")
	r.expires = time.Time{}
	if got := r.LookupMAC("00:00:5e:00:53:01"); !reflect.DeepEqual(got, []string{"pg"}) || calls.Load() != 1 {
		t.Errorf("load after panic: names = %v, queries = %d", got, calls.Load())
	}
}

func TestUbios_storeLocked(t *testing.T) {
	r := &Ubios{}
	now := time.Unix(1700000000, 0)
	fail := errors.New("exit status 2")
	var delays []time.Duration
	for range 6 {
		r.storeLocked(now, nil, fail)
		delays = append(delays, r.expires.Sub(now))
	}
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	if !reflect.DeepEqual(delays, want) {
		t.Errorf("retry delays = %v, want %v", delays, want)
	}
	// expires is computed from the time the load finished.
	later := now.Add(time.Hour)
	r.storeLocked(later, map[string][]string{}, nil)
	if !r.expires.Equal(later.Add(ubiosRefreshInterval)) {
		t.Errorf("expires = %v, want %v", r.expires, later.Add(ubiosRefreshInterval))
	}
}

// While one caller loads, other lookups must not wait for the database, and
// only one query may run.
func TestUbios_lookupDuringLoad(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	r := newTestUbios()
	stubUbios(t, true, counted(&calls, "old"), mustNotRun(t, "mongo"))
	r.LookupMAC("00:00:5e:00:53:01") // first load
	ubiosQueryPostgres = func(context.Context) (map[string][]string, error) {
		calls.Add(1)
		close(started)
		<-release
		return map[string][]string{"00:00:5e:00:53:01": {"new"}}, nil
	}
	r.mu.Lock()
	r.expires = time.Time{}
	r.mu.Unlock()

	loaderDone := make(chan struct{})
	go func() {
		defer close(loaderDone)
		r.LookupMAC("00:00:5e:00:53:01")
	}()
	<-started

	var wg sync.WaitGroup
	results := make(chan []string, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- r.LookupMAC("00:00:5e:00:53:01")
			r.Visit(func(string, []string) {})
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("lookups waited for the running load")
	}
	close(results)
	for got := range results {
		if !reflect.DeepEqual(got, []string{"old"}) {
			t.Errorf("LookupMAC() during load = %v, want [old]", got)
		}
	}
	// Keep the load running a little longer, then check that expires counts
	// from the end of the load, not from its start.
	time.Sleep(50 * time.Millisecond)
	released := time.Now()
	close(release)
	<-loaderDone
	r.mu.RLock()
	expires := r.expires
	r.mu.RUnlock()
	if expires.Before(released.Add(ubiosRefreshInterval)) {
		t.Errorf("expires = %v, before end of load + interval (%v)", expires, released.Add(ubiosRefreshInterval))
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("queries = %d, want 2 (first load and one refresh)", n)
	}
	if got := r.LookupMAC("00:00:5e:00:53:01"); !reflect.DeepEqual(got, []string{"new"}) {
		t.Errorf("LookupMAC() after load = %v, want [new]", got)
	}
}

// When many lookups find the list expired at the same moment, exactly one of
// them may load it.
func TestUbios_claimLoadOnce(t *testing.T) {
	r := newTestUbios()
	for round := range 200 {
		r.mu.Lock()
		r.loading, r.expires = false, time.Time{}
		r.mu.Unlock()
		start := make(chan struct{})
		var claimed atomic.Int32
		var wg sync.WaitGroup
		for range 50 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if r.claimLoad() {
					claimed.Add(1)
				}
			}()
		}
		close(start)
		wg.Wait()
		if n := claimed.Load(); n != 1 {
			t.Fatalf("round %d: %d callers claimed the load, want 1", round, n)
		}
	}
}

// The callbacks run after the lock is released: a callback that calls back
// into the source must not deadlock.
func TestUbios_callbacksOutsideLock(t *testing.T) {
	var calls atomic.Int32
	r := newTestUbios()
	r.OnError = func(error) { r.LookupMAC("00:00:5e:00:53:01") }
	stubUbios(t, true, failing(&calls, errors.New("exit status 2")), mustNotRun(t, "mongo"))
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.LookupMAC("00:00:5e:00:53:01")
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("OnError ran under the lock (deadlock)")
	}
}

// writeScript writes an executable shell script and returns its path.
func writeScript(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func Test_findPsql(t *testing.T) {
	old := ubiosPostgresLib
	t.Cleanup(func() { ubiosPostgresLib = old })
	ubiosPostgresLib = t.TempDir()
	if got := findPsql(); got != ubiosPsqlWrapper {
		t.Errorf("findPsql() with no versions = %q, want the wrapper", got)
	}
	for _, v := range []string{"9.6", "14", "16", "notaversion"} {
		writeScript(t, filepath.Join(ubiosPostgresLib, v, "bin", "psql"), "true")
	}
	if got, want := findPsql(), filepath.Join(ubiosPostgresLib, "16", "bin", "psql"); got != want {
		t.Errorf("findPsql() = %q, want %q", got, want)
	}
	// A newer psql that is not executable is skipped.
	p := filepath.Join(ubiosPostgresLib, "17", "bin", "psql")
	writeScript(t, p, "true")
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := findPsql(), filepath.Join(ubiosPostgresLib, "16", "bin", "psql"); got != want {
		t.Errorf("findPsql() with a non-executable 17 = %q, want %q", got, want)
	}
}

// fakePsql installs a fake psql as the newest PostgreSQL version.
func fakePsql(t *testing.T, body string) {
	t.Helper()
	old := ubiosPostgresLib
	t.Cleanup(func() { ubiosPostgresLib = old })
	ubiosPostgresLib = t.TempDir()
	writeScript(t, filepath.Join(ubiosPostgresLib, "16", "bin", "psql"), body)
}

func Test_queryUbiosPostgres(t *testing.T) {
	out := filepath.Join(t.TempDir(), "call")
	fakePsql(t, `printf '%s\n' "$@" > `+out+`; echo "PGOPTIONS=$PGOPTIONS" >> `+out+`; echo "PGCONNECT_TIMEOUT=$PGCONNECT_TIMEOUT" >> `+out+`
echo '{"mac" : "00:00:5E:00:53:01", "name" : "Media Server"}'`)
	macs, err := queryUbiosPostgres(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(macs, map[string][]string{"00:00:5e:00:53:01": {"Media Server"}}) {
		t.Errorf("macs = %v", macs)
	}
	b, _ := os.ReadFile(out)
	call := string(b)
	for _, want := range []string{"\n-w\n", "\n-h\n" + ubiosPostgresDir + "\n", "\n-p\n" + ubiosPostgresPort + "\n",
		"default_transaction_read_only=on", "statement_timeout=", "lock_timeout=", "PGCONNECT_TIMEOUT=5"} {
		if !strings.Contains(call, want) {
			t.Errorf("psql call lacks %q:\n%s", want, call)
		}
	}
}

func Test_queryUbiosPostgres_timeout(t *testing.T) {
	tests := []struct{ name, body string }{
		{"Hung psql", "exec sleep 30"},
		// A child that keeps the output pipe open after psql is killed.
		// It ends on its own 3 s later, so the test leaves nothing behind.
		{"Child holds the pipe", "sleep 3 &\nexec sleep 30"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakePsql(t, tt.body)
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := queryUbiosPostgres(ctx)
			if took := time.Since(start); took > 2500*time.Millisecond {
				t.Errorf("query returned after %v", took)
			}
			if err == nil || !strings.HasPrefix(err.Error(), "context deadline exceeded: ") {
				t.Errorf("err = %v, want a timeout error", err)
			}
		})
	}
}

func Test_queryUbiosMongo(t *testing.T) {
	oldAddr, oldPath := ubiosMongoAddr, ubiosMongoPath
	t.Cleanup(func() { ubiosMongoAddr, ubiosMongoPath = oldAddr, oldPath })

	// No mongod: "no database", and the mongo shell does not start. Port 0
	// always refuses connections, and no other process can listen on it.
	ubiosMongoAddr = "127.0.0.1:0"
	marker := filepath.Join(t.TempDir(), "started")
	ubiosMongoPath = writeScript(t, filepath.Join(t.TempDir(), "mongo"), "touch "+marker+"; exit 1")
	if _, err := queryUbiosMongo(context.Background()); !errors.Is(err, errNoUbiosDatabase) {
		t.Errorf("err = %v, want errNoUbiosDatabase", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the mongo shell started without a mongod")
	}

	// mongod listens: the shell runs with the same address and its output is parsed.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	ubiosMongoAddr = l.Addr().String()
	ubiosMongoPath = writeScript(t, filepath.Join(t.TempDir(), "mongo"), `[ "$1" = "`+ubiosMongoAddr+`/ace" ] || { echo "bad address $1" >&2; exit 3; }
echo '{ "mac" : "00:00:5e:00:53:03", "name" : "Phone" }'`)
	macs, err := queryUbiosMongo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(macs, map[string][]string{"00:00:5e:00:53:03": {"Phone"}}) {
		t.Errorf("macs = %v", macs)
	}
}

// The no-database message keeps its cause, also for a bare errNoUbiosDatabase.
func Test_ubiosClientList_noDatabaseMessage(t *testing.T) {
	for _, err := range []error{errNoUbiosDatabase, noDatabaseError{errors.New("connection refused")}} {
		var calls atomic.Int32
		stubUbios(t, false, mustNotRun(t, "postgres"), failing(&calls, err))
		_, got := ubiosClientList(context.Background())
		if !errors.Is(got, errNoUbiosDatabase) || strings.Contains(got.Error(), "<nil>") || !strings.HasSuffix(got.Error(), err.Error()) {
			t.Errorf("ubiosClientList() error = %q for %q", got, err)
		}
	}
}
