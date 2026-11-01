package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Ubios struct {
	OnError func(err error)
	// OnRecover is called with the previous error when the client list loads
	// again after a failure.
	OnRecover func(err error)

	once      sync.Once
	supported bool

	mu      sync.RWMutex
	macs    map[string][]string
	expires time.Time
	retry   time.Duration
	loading bool
	errs    errorLatch
	// noDatabase counts consecutive loads that found no database.
	noDatabase int
}

const (
	ubiosRefreshInterval = 5 * time.Minute
	// ubiosRetryMin and ubiosRetryMax bound the retry delay after a failed
	// load. The delay doubles after each failure.
	ubiosRetryMin = 30 * time.Second
	ubiosRetryMax = 5 * time.Minute
	// ubiosQueryTimeout bounds one database query.
	ubiosQueryTimeout = 10 * time.Second

	// The PostgreSQL cluster of UniFi Network 11 and later.
	ubiosPostgresDir  = "/run/postgresql"
	ubiosPostgresPort = "5434"
)

// errNoUbiosDatabase means that no UniFi Network database runs on this device
// at the moment, for example on a console without UniFi Network, or at boot
// before the database starts. It is reported only if names were loaded
// before, because then a database was expected, and only if it lasts for
// more than one retry, so that a short database restart is not reported.
var errNoUbiosDatabase = errors.New("no UniFi Network database")

// These are variables so that tests can replace them.
var (
	ubiosPostgresSocket = ubiosPostgresDir + "/.s.PGSQL." + ubiosPostgresPort
	// ubiosPostgresLib holds one directory per installed PostgreSQL version.
	ubiosPostgresLib = "/usr/lib/postgresql"
	// ubiosPsqlWrapper is Debian's psql wrapper. findPsql returns it when no
	// versioned psql is installed.
	ubiosPsqlWrapper = "/usr/bin/psql"
	// The MongoDB server of UniFi Network 10 and earlier.
	ubiosMongoAddr     = "localhost:27117"
	ubiosMongoPath     = "/usr/bin/mongo"
	ubiosQueryPostgres = queryUbiosPostgres
	ubiosQueryMongo    = queryUbiosMongo
)

func isUnifi() bool {
	if st, _ := os.Stat("/data/unifi"); st != nil && st.IsDir() {
		return true
	}
	if err := exec.Command("ubnt-device-info", "firmware").Run(); err == nil {
		return true
	}
	return false
}

func (r *Ubios) init() {
	if isUnifi() {
		r.supported = true
	}
}

// refresh loads the client list when it expired. Only one caller loads at a
// time, and the load runs without holding the lock: other callers keep using
// the current list instead of waiting for the database.
func (r *Ubios) refresh() {
	r.once.Do(r.init)
	if !r.supported || !r.claimLoad() {
		return
	}

	macs, err := loadUbiosClientList()

	r.mu.Lock()
	report, recovered := r.storeLocked(time.Now(), macs, err)
	r.mu.Unlock()

	// Call the callbacks after the lock is released, so that a slow callback
	// does not block lookups.
	if report != nil && r.OnError != nil {
		r.OnError(fmt.Errorf("clientList: %v", report))
	}
	if recovered != nil && r.OnRecover != nil {
		r.OnRecover(recovered)
	}
}

// claimLoad reports whether the caller must load the list now. It returns
// false while the list is valid or while another caller loads it. The check
// under the read lock keeps the common case off the write lock.
func (r *Ubios) claimLoad() bool {
	r.mu.RLock()
	skip := r.loading || time.Now().Before(r.expires)
	r.mu.RUnlock()
	if skip {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loading || time.Now().Before(r.expires) {
		return false
	}
	r.loading = true
	return true
}

// loadUbiosClientList runs one bounded load. A panic becomes an error, so
// that the caller always clears the loading flag: the DNS proxy recovers
// panics per query, and a flag left set would stop all later loads.
func loadUbiosClientList() (macs map[string][]string, err error) {
	defer func() {
		if p := recover(); p != nil {
			macs, err = nil, fmt.Errorf("panic: %v", p)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), ubiosQueryTimeout)
	defer cancel()
	return ubiosClientList(ctx)
}

// storeLocked records the result of one load at time now, after the load
// finished. It returns the error to report and the recovered error, if any.
func (r *Ubios) storeLocked(now time.Time, macs map[string][]string, err error) (report, recovered error) {
	r.loading = false
	if errors.Is(err, errNoUbiosDatabase) {
		r.noDatabase++
	} else {
		r.noDatabase = 0
	}
	if err == nil {
		r.macs = macs
		r.retry = 0
		r.expires = now.Add(ubiosRefreshInterval)
		return r.errs.update(nil)
	}
	// Keep the last names. Retry sooner than the normal interval.
	r.retry = min(max(2*r.retry, ubiosRetryMin), ubiosRetryMax)
	r.expires = now.Add(r.retry)
	if errors.Is(err, errNoUbiosDatabase) && (r.macs == nil || r.noDatabase < 2) {
		// Nothing was ever loaded (a device without UniFi Network, or a
		// database that did not start yet), or the database is gone for
		// only one load. That is not a fault yet.
		return nil, nil
	}
	return r.errs.update(err)
}

func (r *Ubios) Name() string {
	return "ubios"
}

func (r *Ubios) Visit(f func(name string, macs []string)) {
	r.refresh()
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := map[string][]string{}
	for mac, names := range r.macs {
		for _, name := range names {
			m[name] = append(m[name], mac)
		}
	}
	for name, macs := range m {
		f(name, macs)
	}
}

func (r *Ubios) LookupMAC(mac string) []string {
	r.refresh()
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.macs[mac]
}

func (r *Ubios) LookupAddr(addr string) []string {
	return nil
}

func (r *Ubios) LookupHost(name string) []string {
	return nil
}

// ubiosClientList returns the client names set in the UniFi Network
// application. UniFi Network 11 and later keep them in PostgreSQL, older
// versions in MongoDB.
//
// When the PostgreSQL socket exists, only PostgreSQL is used. After the
// migration, the old MongoDB data stays on disk and is no longer updated, and
// the application starts mongod for a few seconds during each update, so
// MongoDB is not a fallback.
func ubiosClientList(ctx context.Context) (map[string][]string, error) {
	if _, err := os.Stat(ubiosPostgresSocket); err == nil {
		macs, err := ubiosQueryPostgres(ctx)
		if err != nil {
			return nil, fmt.Errorf("postgres: %w", err)
		}
		return macs, nil
	}
	macs, err := ubiosQueryMongo(ctx)
	if errors.Is(err, errNoUbiosDatabase) {
		return nil, fmt.Errorf("no PostgreSQL socket %s, and %w", ubiosPostgresSocket, err)
	}
	if err != nil {
		return nil, fmt.Errorf("mongo: %w", err)
	}
	return macs, nil
}

func queryUbiosPostgres(ctx context.Context) (map[string][]string, error) {
	cmd := exec.CommandContext(ctx, findPsql(), "-X", "-q", "-A", "-t", "-w",
		"-v", "ON_ERROR_STOP=1",
		"-h", ubiosPostgresDir, "-p", ubiosPostgresPort,
		"-U", "unifi_network_admin", "-d", "unifi-network",
		"-c", `SELECT json_build_object('mac', mac, 'name', name)
			FROM config.user_client WHERE name <> ''`)
	// The role can write: make any write fail, because this query only reads.
	// Also bound the connection and the statement on the server side.
	cmd.Env = append(os.Environ(),
		"PGCONNECT_TIMEOUT=5",
		"PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=5000 -c lock_timeout=2000")
	return runUbiosQuery(ctx, cmd)
}

// findPsql returns the executable psql binary of the newest installed
// PostgreSQL version. Calling it directly avoids Debian's /usr/bin/psql, which is a Perl
// wrapper that starts Perl on each call. The glob runs on each load, so a
// PostgreSQL upgrade does not need a restart.
func findPsql() string {
	best, bestVersion := ubiosPsqlWrapper, -1.0
	paths, _ := filepath.Glob(filepath.Join(ubiosPostgresLib, "*", "bin", "psql"))
	for _, p := range paths {
		if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
			continue
		}
		v, err := strconv.ParseFloat(filepath.Base(filepath.Dir(filepath.Dir(p))), 64)
		if err == nil && v > bestVersion {
			best, bestVersion = p, v
		}
	}
	return best
}

func queryUbiosMongo(ctx context.Context) (map[string][]string, error) {
	// Without a running mongod, there is no database to read. Do not start the
	// mongo shell in that case.
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", ubiosMongoAddr)
	if err != nil {
		return nil, noDatabaseError{err}
	}
	c.Close()
	cmd := exec.CommandContext(ctx, ubiosMongoPath, ubiosMongoAddr+"/ace", "--quiet", "--eval", `
		DBQuery.shellBatchSize = 1000;
		db.user.find({name: {$exists: true, $ne: ""}}, {_id:0, mac:1, name:1});`)
	return runUbiosQuery(ctx, cmd)
}

// noDatabaseError is errNoUbiosDatabase with the reason, for example the
// failed connection to mongod.
type noDatabaseError struct{ cause error }

func (e noDatabaseError) Error() string        { return errNoUbiosDatabase.Error() + ": " + e.cause.Error() }
func (e noDatabaseError) Is(target error) bool { return target == errNoUbiosDatabase }
func (e noDatabaseError) Unwrap() error        { return e.cause }

// runUbiosQuery runs a query command and parses its output. If the context
// ended, the error says so, because the killed process reports only a signal.
func runUbiosQuery(ctx context.Context, cmd *exec.Cmd) (map[string][]string, error) {
	// After a kill, do not wait longer than this for a child process that
	// still holds the output pipe.
	cmd.WaitDelay = time.Second
	b, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%v: %w", ctxErr, cmdError(err))
		}
		return nil, cmdError(err)
	}
	return parseClientList(b), nil
}

// cmdError adds the first meaningful line of the command's stderr to err,
// because an *exec.ExitError alone prints only the exit status. It skips Perl
// warnings and indented continuation lines, which appear when psql runs
// through Debian's Perl wrapper (when findPsql finds no versioned psql).
func cmdError(err error) error {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return err
	}
	for _, line := range strings.Split(string(ee.Stderr), "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(line, "perl: ") {
			continue
		}
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(line))
	}
	return err
}

// parseClientList parses a stream of {"mac": ..., "name": ...} JSON objects.
// Records without a MAC or a name are skipped.
func parseClientList(b []byte) map[string][]string {
	d := json.NewDecoder(bytes.NewReader(b))
	macs := map[string][]string{}
	for {
		// A new value for each record, so that a null or missing field does
		// not keep the value of the previous record.
		var rec struct {
			MAC  string
			Name string
		}
		if d.Decode(&rec) != nil {
			break
		}
		if rec.MAC == "" || rec.Name == "" {
			continue
		}
		mac := strings.ToLower(rec.MAC)
		macs[mac] = appendUniq(macs[mac], rec.Name)
	}
	return macs
}

// errorLatch remembers the last error of a periodic operation, so that a
// repeated error is reported once and the recovery after a failure is
// reported too.
type errorLatch struct {
	last error
}

// update records the result of one run. It returns err as report when the
// error text differs from the previous run. It returns the previous error as
// recovered when err is nil after a failed run.
func (l *errorLatch) update(err error) (report, recovered error) {
	prev := l.last
	l.last = err
	switch {
	case err == nil:
		return nil, prev
	case prev == nil || prev.Error() != err.Error():
		return err, nil
	}
	return nil, nil
}
