package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Ubios struct {
	OnError func(err error)

	once      sync.Once
	supported bool

	mu      sync.RWMutex
	macs    map[string][]string
	expires time.Time
	loading bool
	lastErr string

	// Only the loader accesses postgres. Once detected, PostgreSQL remains
	// authoritative for this source's lifetime, including during restarts.
	postgres bool
}

const (
	ubiosRefreshInterval = 5 * time.Minute
	ubiosRetryInterval   = 30 * time.Second
	ubiosQueryTimeout    = 10 * time.Second
	ubiosPostgresDir     = "/run/postgresql"
	ubiosPostgresPort    = "5434"
)

var errNoUbiosDatabase = errors.New("no UniFi Network database")

// Paths and queries are variables so tests can use isolated databases.
var (
	ubiosPostgresSocket = ubiosPostgresDir + "/.s.PGSQL." + ubiosPostgresPort
	ubiosPsqlPath       = "/usr/bin/psql"
	ubiosMongoPath      = "/usr/bin/mongo"
	ubiosMongoAddr      = "localhost:27117"
	ubiosQueryPostgres  = queryUbiosPostgres
	ubiosQueryMongo     = queryUbiosMongo
)

func isUnifi() bool {
	if st, _ := os.Stat("/data/unifi"); st != nil && st.IsDir() {
		return true
	}
	return exec.Command("ubnt-device-info", "firmware").Run() == nil
}

func (r *Ubios) refresh() {
	r.once.Do(func() { r.supported = isUnifi() })
	if !r.supported || !r.claimLoad() {
		return
	}
	// Clear ownership even if the loader or an error callback panics. The
	// proxy's existing panic handler can then recover without disabling refresh.
	defer func() {
		r.mu.Lock()
		r.loading = false
		r.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), ubiosQueryTimeout)
	defer cancel()
	macs, err := r.clientList(ctx)

	r.mu.Lock()
	r.expires = time.Now().Add(ubiosRetryInterval)
	var report error
	if err == nil {
		r.macs = macs
		r.expires = time.Now().Add(ubiosRefreshInterval)
		r.lastErr = ""
	} else if !(errors.Is(err, errNoUbiosDatabase) && r.macs == nil) {
		// A console without Network, or its database not yet started, is
		// expected until this source has successfully loaded a client list.
		if msg := err.Error(); msg != r.lastErr {
			r.lastErr = msg
			report = err
		}
	}
	r.mu.Unlock()
	if report != nil && r.OnError != nil {
		r.OnError(fmt.Errorf("clientList: %w", report))
	}
}

// Only one caller loads; concurrent lookups continue using the immutable
// previous snapshot. No database command runs while holding the cache lock.
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

func (r *Ubios) Name() string { return "ubios" }

func (r *Ubios) Visit(f func(name string, macs []string)) {
	r.refresh()
	r.mu.RLock()
	macs := r.macs
	r.mu.RUnlock()
	m := map[string][]string{}
	for mac, names := range macs {
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

func (r *Ubios) LookupAddr(addr string) []string { return nil }
func (r *Ubios) LookupHost(name string) []string { return nil }

func (r *Ubios) clientList(ctx context.Context) (map[string][]string, error) {
	if !r.postgres {
		_, err := os.Stat(ubiosPostgresSocket)
		switch {
		case err == nil:
			r.postgres = true
		case !errors.Is(err, os.ErrNotExist):
			// A failed check is not evidence that MongoDB is authoritative.
			return nil, fmt.Errorf("postgres socket: %w", err)
		}
	}
	query, backend := ubiosQueryMongo, "mongo"
	if r.postgres {
		query, backend = ubiosQueryPostgres, "postgres"
	}
	macs, err := query(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", backend, err)
	}
	return macs, nil
}

func queryUbiosPostgres(ctx context.Context) (map[string][]string, error) {
	cmd := exec.CommandContext(ctx, ubiosPsqlPath, "-X", "-q", "-A", "-t", "-w",
		"-v", "ON_ERROR_STOP=1",
		"-h", ubiosPostgresDir, "-p", ubiosPostgresPort,
		"-U", "unifi_network_admin", "-d", "unifi-network",
		"-c", `SELECT json_build_object('mac', mac, 'name', name)
			FROM config.user_client WHERE name <> ''`)
	cmd.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=5",
		"PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=5000 -c lock_timeout=2000")
	return runUbiosQuery(ctx, cmd)
}

func queryUbiosMongo(ctx context.Context) (map[string][]string, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", ubiosMongoAddr)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errNoUbiosDatabase, err)
	}
	c.Close()
	// Explicit iteration avoids the interactive shell's batch limit and
	// "Type it for more" message, so every client is emitted as JSON.
	cmd := exec.CommandContext(ctx, ubiosMongoPath, ubiosMongoAddr+"/ace", "--quiet", "--eval", `
		db.user.find({name: {$exists: true, $ne: ""}}, {_id:0, mac:1, name:1})
			.forEach(function(client) { printjsononeline(client); });`)
	return runUbiosQuery(ctx, cmd)
}

func runUbiosQuery(ctx context.Context, cmd *exec.Cmd) (map[string][]string, error) {
	// Bound waiting on inherited output pipes after the command is killed.
	cmd.WaitDelay = time.Second
	b, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			for _, line := range strings.Split(string(ee.Stderr), "\n") {
				if line == "" || line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(line, "perl: ") {
					continue
				}
				return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(line))
			}
		}
		return nil, err
	}
	return parseClientList(b)
}

// Parse the entire result before publishing it. A truncated or malformed
// result must not replace the last valid client list with a partial one.
func parseClientList(b []byte) (map[string][]string, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	macs := map[string][]string{}
	for {
		var rec struct{ MAC, Name string }
		if err := d.Decode(&rec); err != nil {
			if errors.Is(err, io.EOF) {
				return macs, nil
			}
			// Do not include record contents (client names/MACs) in errors.
			return nil, errors.New("invalid client list JSON")
		}
		if rec.MAC != "" && rec.Name != "" {
			mac := strings.ToLower(rec.MAC)
			macs[mac] = appendUniq(macs[mac], rec.Name)
		}
	}
}
