package admin

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sstark/gjfy/store"
)

func testStore(t *testing.T) *store.SecretStore {
	t.Helper()
	st := store.New(0)
	if _, err := st.NewEntry("hunter2", 1, 7, "a@example.org", "id1"); err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	return st
}

// A path short enough for a unix socket: the sun_path limit is ~104 bytes and
// t.TempDir() under a long test name can get close.
func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gjfy")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

func TestServeAnswersOverTheSocket(t *testing.T) {
	path := socketPath(t)
	st := testStore(t)
	started := time.Now().Add(-time.Hour)

	srv, err := Serve(path, st, started)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer srv.Close()

	rep, err := Fetch(path)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if rep.Live != 1 || rep.LostOnRestart != 1 {
		t.Errorf("live/lost: got %d/%d, wanted 1/1", rep.Live, rep.LostOnRestart)
	}
	if rep.Created != 1 {
		t.Errorf("created: got %d, wanted 1", rep.Created)
	}
	if rep.UptimeSeconds < 3500 {
		t.Errorf("uptime: got %ds, wanted about an hour", rep.UptimeSeconds)
	}
	if len(rep.Entries) != 1 || rep.Entries[0].Bytes != len("hunter2") {
		t.Errorf("entries: %+v", rep.Entries)
	}
}

// The socket is the whole access control: anyone who can open it can read the
// report, so nobody but its owner may be able to open it.
func TestSocketIsOwnerOnly(t *testing.T) {
	path := socketPath(t)
	srv, err := Serve(path, testStore(t), time.Now())
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer srv.Close()

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != socketMode {
		t.Errorf("socket mode: got %04o, wanted %04o", perm, socketMode)
	}
}

func TestServeWithoutPathIsOff(t *testing.T) {
	srv, err := Serve("", testStore(t), time.Now())
	if err != nil {
		t.Fatalf("Serve(\"\"): %v", err)
	}
	if srv != nil {
		t.Error("an empty path should not start a listener")
	}
	// Close on the nil server is what the caller does on shutdown.
	if err := srv.Close(); err != nil {
		t.Errorf("Close on a disabled server: %v", err)
	}
}

// A socket left behind by an unclean stop must not keep the service from
// starting: bind would fail with "address already in use" forever.
func TestServeReplacesAStaleSocket(t *testing.T) {
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// Close the listener the way a killed process would: the file stays.
	if f, err := ln.(*net.UnixListener).File(); err == nil {
		f.Close()
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale socket did not survive: %v", err)
	}

	srv, err := Serve(path, testStore(t), time.Now())
	if err != nil {
		t.Fatalf("Serve over a stale socket: %v", err)
	}
	defer srv.Close()
	if _, err := Fetch(path); err != nil {
		t.Errorf("Fetch after replacing a stale socket: %v", err)
	}
}

// Anything that is not a socket belongs to someone else. A typo in the flag
// must not delete it.
func TestServeRefusesToReplaceARegularFile(t *testing.T) {
	path := socketPath(t)
	if err := os.WriteFile(path, []byte("important"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Serve(path, testStore(t), time.Now()); err == nil {
		t.Fatal("Serve replaced a regular file instead of refusing")
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "important" {
		t.Errorf("the file was touched: %q, %v", b, err)
	}
}

func TestCloseRemovesTheSocket(t *testing.T) {
	path := socketPath(t)
	srv, err := Serve(path, testStore(t), time.Now())
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("socket still there after Close: %v", err)
	}
}

func TestHandlerServesOnlyGetStats(t *testing.T) {
	h := Handler(testStore(t), time.Now())
	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/stats", http.StatusOK},
		{http.MethodPost, "/stats", http.StatusMethodNotAllowed},
		{http.MethodGet, "/", http.StatusNotFound},
		{http.MethodGet, "/secret", http.StatusNotFound},
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(c.method, c.path, nil))
		if rr.Code != c.want {
			t.Errorf("%s %s: got %d, wanted %d", c.method, c.path, rr.Code, c.want)
		}
	}
}

// Same guarantee as in the store, checked once more on the wire: the report
// an operator reads carries no secret and no id.
func TestReportLeaksNeitherSecretNorID(t *testing.T) {
	st := store.New(0)
	if _, err := st.NewEntry("hunter2-the-actual-secret", 1, 7, "a@example.org", "the-secret-id"); err != nil {
		t.Fatalf("NewEntry: %v", err)
	}
	rr := httptest.NewRecorder()
	Handler(st, time.Now()).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/stats", nil))
	body := rr.Body.String()
	for _, forbidden := range []string{"hunter2-the-actual-secret", "the-secret-id"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("report contains %q: %s", forbidden, body)
		}
	}
	var rep Report
	if err := json.Unmarshal([]byte(body), &rep); err != nil {
		t.Errorf("report is not valid json: %v", err)
	}
}

func TestFetchReportsAMissingSocket(t *testing.T) {
	if _, err := Fetch(filepath.Join(t.TempDir(), "absent.sock")); err == nil {
		t.Error("Fetch against a missing socket returned no error")
	}
}

// Taking over the path of a running instance detaches it: bind succeeds, the
// older process keeps serving a socket nothing points at, and the number it
// holds can no longer be read at all — the one number this socket exists for.
func TestServeRefusesToHijackALiveSocket(t *testing.T) {
	path := socketPath(t)
	first, err := Serve(path, testStore(t), time.Now())
	if err != nil {
		t.Fatalf("first Serve: %v", err)
	}
	defer first.Close()

	if _, err := Serve(path, store.New(0), time.Now()); err == nil {
		t.Fatal("a second instance took over a live socket")
	}
	// The first one is still the one answering, with its own store.
	rep, err := Fetch(path)
	if err != nil {
		t.Fatalf("Fetch after the refused takeover: %v", err)
	}
	if rep.Live != 1 {
		t.Errorf("live: got %d, wanted the first instance's 1", rep.Live)
	}
}

// And if a takeover happens anyway — by a process that does not go through
// Serve — stopping must not delete the newcomer's socket on the way out.
func TestCloseLeavesAForeignSocketAlone(t *testing.T) {
	path := socketPath(t)
	srv, err := Serve(path, testStore(t), time.Now())
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	// Wait until the listener is actually registered with the server:
	// Shutdown only closes listeners it knows about, and without this the
	// test would pass for the wrong reason.
	if _, err := Fetch(path); err != nil {
		t.Fatalf("Fetch before the swap: %v", err)
	}
	// Replace the file behind the path, the way a careless second instance
	// would: the old listener keeps its own, now unnamed, socket.
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("second listener: %v", err)
	}
	defer ln.Close()

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Close removed a socket it did not create: %v", err)
	}
}

// A socket nobody can connect to is not the same as a dead one: refusing to
// connect for want of permission, or because the backlog is full, says nothing
// about whether a process is behind it. Guessing wrong deletes a live socket.
func TestServeLeavesASocketItCannotProbe(t *testing.T) {
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root connects regardless of permissions")
	}

	if _, err := Serve(path, testStore(t), time.Now()); err == nil {
		t.Fatal("Serve took over a socket it could not probe")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the unprobeable socket was deleted: %v", err)
	}
}

// The counterpart: a socket with no listener behind it is stale and must not
// block a start forever.
func TestServeTakesOverARefusedSocket(t *testing.T) {
	path := socketPath(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()

	srv, err := Serve(path, testStore(t), time.Now())
	if err != nil {
		t.Fatalf("Serve over a refused socket: %v", err)
	}
	defer srv.Close()
	if _, err := Fetch(path); err != nil {
		t.Errorf("Fetch after taking over: %v", err)
	}
}

// Two different files never share an identity, and one file keeps its own
// across calls: that is the whole contract Close relies on.
func TestFileIDDistinguishesFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	idA, err := fileID(a)
	if err != nil {
		t.Fatalf("fileID(a): %v", err)
	}
	idB, err := fileID(b)
	if err != nil {
		t.Fatalf("fileID(b): %v", err)
	}
	if idA == idB {
		t.Errorf("two files share an identity: %+v", idA)
	}
	again, err := fileID(a)
	if err != nil {
		t.Fatalf("fileID(a) again: %v", err)
	}
	if again != idA {
		t.Errorf("identity of one file changed between calls: %+v vs %+v", idA, again)
	}
	// A path re-pointed at another file must not keep the old identity.
	if err := os.Rename(b, a); err != nil {
		t.Fatalf("rename: %v", err)
	}
	after, err := fileID(a)
	if err != nil {
		t.Fatalf("fileID after rename: %v", err)
	}
	if after == idA {
		t.Error("identity survived the file behind the path being replaced")
	}
}

// Every removal in this package goes through removeIfOurs, including the one
// on the failed-lockdown path, which no test can reach directly.
func TestRemoveIfOursOnlyRemovesTheSameFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	id, err := fileID(path)
	if err != nil {
		t.Fatalf("fileID: %v", err)
	}

	// A different file at the same path must survive.
	other := filepath.Join(dir, "other")
	if err := os.WriteFile(other, nil, 0o600); err != nil {
		t.Fatalf("write other: %v", err)
	}
	if err := os.Rename(other, path); err != nil {
		t.Fatalf("rename: %v", err)
	}
	removeIfOurs(path, id)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a file that was not ours got removed: %v", err)
	}

	// Ours does not.
	nowID, err := fileID(path)
	if err != nil {
		t.Fatalf("fileID again: %v", err)
	}
	removeIfOurs(path, nowID)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("our own file survived: %v", err)
	}
	// A missing path is not an error.
	removeIfOurs(path, nowID)
}
