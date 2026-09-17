// Package admin exposes the one thing an operator cannot get from the outside:
// how much is in the store right now, and therefore how much a restart would
// destroy. Secrets live in memory only, so that number exists nowhere else —
// once the process is gone, so is the answer.
//
// It is deliberately not a route on the public mux. A route would be one nginx
// location away from the internet, and gating it with a token would put that
// token into shell history, scripts and logs. A unix socket cannot be proxied
// by accident and is guarded by file permissions: the socket is created 0600
// under a directory only root and the service user may enter, so "root only"
// is enforced by the kernel rather than by a secret string.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/sstark/gjfy/store"
)

// Path is where the socket lives when the service is run from the packaged
// systemd unit. /run is a tmpfs, so a stale socket never survives a reboot.
const Path = "/run/gjfy/stats.sock"

// route is the only thing served on the socket.
const route = "/stats"

// socketMode keeps the socket readable by its owner alone. The directory
// permissions are the real gate; this is the second one, so that a
// misconfigured RuntimeDirectory does not silently open the door.
const socketMode = 0o600

const (
	readHeaderTimeout = 5 * time.Second
	writeTimeout      = 10 * time.Second
	shutdownGrace     = 2 * time.Second
	dialTimeout       = 5 * time.Second
)

// Report answers "what is in there, and what does stopping cost".
type Report struct {
	Live int `json:"live"`
	// LostOnRestart repeats Live on purpose: it is the question being asked,
	// and the answer should not depend on the reader knowing that the store
	// has no persistence.
	LostOnRestart int               `json:"lost_on_restart"`
	MaxEntries    int               `json:"max_entries"`
	StartedAt     time.Time         `json:"started_at"`
	UptimeSeconds int64             `json:"uptime_seconds"`
	Created       uint64            `json:"created"`
	Claimed       uint64            `json:"claimed"`
	Expired       uint64            `json:"expired"`
	Entries       []store.EntryMeta `json:"entries"`
}

// Snapshot builds a report from the store.
func Snapshot(st *store.SecretStore, started time.Time, now time.Time) Report {
	s := st.Stats()
	return Report{
		Live:          s.Live,
		LostOnRestart: s.Live,
		MaxEntries:    s.MaxEntries,
		StartedAt:     started,
		UptimeSeconds: int64(now.Sub(started) / time.Second),
		Created:       s.Created,
		Claimed:       s.Claimed,
		Expired:       s.Expired,
		Entries:       s.Entries,
	}
}

// Handler serves the report. It is a whole mux rather than a bare handler so
// that anything but GET /stats is refused instead of answered.
func Handler(st *store.SecretStore, started time.Time) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(Snapshot(st, started, time.Now()))
	})
	return mux
}

// Serve starts the admin listener on a unix socket. An empty path means the
// feature is switched off, which is the default: a service that was not asked
// for an admin socket should not create one.
func Serve(path string, st *store.SecretStore, started time.Time) (*Server, error) {
	if path == "" {
		return nil, nil
	}
	if err := clearStaleSocket(path); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// Go unlinks the socket path when the listener closes, and it does so by
	// path, without checking that the file is still the one it created. Take
	// that away from it: by the time this server stops, the path may belong to
	// another instance, and removing its socket would leave the machine with
	// no way to ask anything at all. Close does the removal instead, after
	// checking the file is ours.
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	// Remember which file this listener created.
	id, err := fileID(path)
	if err != nil {
		// Nothing is removed here on purpose: without an identity there is no
		// way to tell our socket from one another instance has just put at
		// this path, and deleting a live one is the worse mistake. The file
		// left behind is harmless — the listener is closed, so the next start
		// gets a refused connection and clears it as stale.
		ln.Close()
		return nil, err
	}
	// Between bind and chmod the socket carries whatever the process umask
	// allowed. The window is closed for good here; the containing directory
	// is what protects it in between.
	if err := os.Chmod(path, socketMode); err != nil {
		ln.Close()
		// The listener no longer unlinks on close (see above), so clean up
		// after ourselves — otherwise the next start finds a socket it has to
		// reason about instead of a clean path. Same guard as in Close: only
		// the file this call created may be removed.
		removeIfOurs(path, id)
		return nil, fmt.Errorf("could not lock down %s: %w", path, err)
	}
	srv := &http.Server{
		Handler:           Handler(st, started),
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
	}
	s := &Server{srv: srv, ln: ln, path: path, id: id}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// The admin socket is a convenience: losing it must not take the
			// service, and the secrets it holds, down with it.
			fmt.Fprintf(os.Stderr, "stats socket stopped: %v\n", err)
		}
	}()
	return s, nil
}

// Server is a running admin listener.
type Server struct {
	srv  *http.Server
	ln   net.Listener
	path string
	id   fileIdent
}

// Close stops serving and removes the socket file.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	err := s.srv.Shutdown(ctx)
	// Shutdown closes the listener but, by the call above, no longer unlinks
	// the path. Remove it here, and only if it is still the file this server
	// created.
	removeIfOurs(s.path, s.id)
	return err
}

// removeIfOurs deletes a path only while it still holds the exact file the
// caller created. Between creating a socket and removing it, another instance
// may have taken the path over; deleting its socket would leave the machine
// with no way to ask anything at all.
func removeIfOurs(path string, id fileIdent) {
	if now, err := fileID(path); err == nil && now == id {
		os.Remove(path)
	}
}

// clearStaleSocket removes a socket left behind by an unclean stop, and only
// that.
//
// Two things must not happen here. Deleting a file that is not a socket turns
// a typo in a flag into data loss. Deleting a socket somebody is listening on
// silently detaches a running instance: bind would succeed, the older process
// would keep serving a path nothing points at, and the number it holds — the
// whole point of this socket — would become unreachable without the restart
// that destroys it.
func clearStaleSocket(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a socket, refusing to replace it", path)
	}
	conn, err := net.DialTimeout("unix", path, dialTimeout)
	if err == nil {
		conn.Close()
		return fmt.Errorf("%s is live: another instance is already listening there", path)
	}
	// Only two errors mean "nobody is home": the kernel refuses a socket with
	// no listener, and the file may have gone between the stat and the dial.
	// Anything else — no permission to connect, a listener whose backlog is
	// full — is a socket that may well be alive, and the answer to "is this
	// stale" is then "do not touch it".
	if !errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, syscall.ENOENT) {
		return fmt.Errorf("cannot tell whether %s is live (%w), leaving it alone", path, err)
	}
	return os.Remove(path)
}

// fileIdent identifies a file by device and inode. The two are kept apart
// rather than mixed into one number: inode numbers are 64 bit, and folding a
// device into them makes distinct files compare equal.
type fileIdent struct {
	dev uint64
	ino uint64
}

// fileID answers "is this still the socket I created". The path alone cannot:
// a path can be re-pointed at a new file at any moment.
func fileID(path string) (fileIdent, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileIdent{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdent{}, fmt.Errorf("cannot identify %s on this platform", path)
	}
	return fileIdent{dev: uint64(st.Dev), ino: uint64(st.Ino)}, nil
}

// Fetch reads a report from a running service.
func Fetch(path string) (Report, error) {
	var rep Report
	client := &http.Client{
		Timeout: dialTimeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", path)
			},
		},
	}
	// The host in the URL is ignored by the unix dialer but has to parse.
	resp, err := client.Get("http://gjfy" + route)
	if err != nil {
		return rep, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return rep, fmt.Errorf("stats socket answered %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&rep); err != nil {
		return rep, err
	}
	return rep, nil
}
