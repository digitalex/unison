package cli

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store"
)

// retryEnv is a workspace with Alex (a member) and Quill (an agent), with
// the retry pauses shortened so outages take milliseconds.
func retryEnv(t *testing.T) (e *testEnv, ws store.Workspace, human, agent string) {
	t.Helper()
	readDelay, base, limit := readRetryDelay, waitBackoffBase, waitBackoffMax
	readRetryDelay, waitBackoffBase, waitBackoffMax = 10*time.Millisecond, 20*time.Millisecond, 80*time.Millisecond
	t.Cleanup(func() { readRetryDelay, waitBackoffBase, waitBackoffMax = readDelay, base, limit })

	e = newTestEnv(t)
	human, agent = t.TempDir(), t.TempDir()
	e.login("Alex", "")
	if err := json.Unmarshal([]byte(e.ok(human, "workspace", "create", "Unison", "--json")), &ws); err != nil {
		t.Fatal(err)
	}
	url := e.url + "/w/" + ws.ID
	e.ok(human, "join", url, "--name", "Alex", "--account")
	e.ok(agent, "join", url, "--pass", e.passFor(human), "--name", "Quill")
	return e, ws, human, agent
}

// A read that gets no answer is asked once more; a write never is (#317).
func TestReadRetriedOnce(t *testing.T) {
	e, _, _, agent := retryEnv(t)

	// lose cuts off the next n requests (on a fresh connection, as in a new
	// CLI process: Go itself re-sends a read that failed on a reused one,
	// which is not what is tested) and returns what was lost.
	lose := func(n int) (lost func() []string) {
		http.DefaultTransport.(*http.Transport).CloseIdleConnections()
		var mu sync.Mutex
		var seen []string
		var up func()
		up = e.down(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen = append(seen, r.Method+" "+r.URL.Path)
			if len(seen) == n {
				up()
			}
			mu.Unlock()
			noAnswer(w, r)
		})
		return func() []string {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(seen)
		}
	}

	// One lost request: the retry gets through.
	lost := lose(1)
	if out := e.ok(agent, "who"); !strings.Contains(out, "Quill") || len(lost()) != 1 {
		t.Errorf("who after one lost request = %q (lost %v)", out, lost())
	}
	// Two in a row: one retry is all a read gets.
	lost = lose(2)
	if _, errOut, code := e.run(agent, "who"); code != 1 || !strings.Contains(errOut, "cannot reach the Unison server") || len(lost()) != 2 {
		t.Errorf("who after two lost requests: exit %d %q (lost %v)", code, errOut, lost())
	}
	// A write is sent once: it may have landed, and must not land twice.
	lost = lose(1)
	if _, errOut, code := e.run(agent, "chat", "send", "hello"); code != 1 || !strings.Contains(errOut, "cannot reach the Unison server") {
		t.Errorf("chat send with its request lost: exit %d %q", code, errOut)
	}
	if got := lost(); len(got) != 1 || !strings.HasPrefix(got[0], "POST ") {
		t.Errorf("a lost POST was sent %d times: %v", len(got), got)
	}
	e.outage.Store(nil) // back up, whatever happened above
	if out := e.ok(agent, "chat", "log"); strings.Contains(out, "hello") {
		t.Errorf("the lost message was posted after all: %q", out)
	}

	// A gateway's bare status reads as what it is, not as an empty error.
	up := e.down(emptyStatus(http.StatusBadGateway))
	if _, errOut, code := e.run(agent, "who"); code != 1 || !strings.Contains(errOut, "unison: HTTP 502 Bad Gateway\n") {
		t.Errorf("who behind a 502: exit %d %q", code, errOut)
	}
	up()
	up = e.down(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("<html><body><h1>Service Unavailable</h1></body></html>"))
	})
	if _, errOut, code := e.run(agent, "who"); code != 1 || !strings.Contains(errOut, "unison: HTTP 503 Service Unavailable\n") || strings.Contains(errOut, "<html>") {
		t.Errorf("who behind a 503 page: exit %d %q", code, errOut)
	}
	up()
}

// A parked wait rides out a server that goes away and comes back, and
// misses nothing that happened meanwhile (#317).
func TestWaitRidesOutAnOutage(t *testing.T) {
	e, ws, _, agent := retryEnv(t)
	alex := participantNamed(t, e, ws.ID, "Alex")

	// Down from the first request on: nothing answers, then a gateway
	// answers 503, then the server is back with a mention waiting.
	up := e.down(noAnswer)
	done := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		e.down(emptyStatus(http.StatusServiceUnavailable))
		time.Sleep(150 * time.Millisecond)
		_, _, err := e.st.Post(t.Context(), alex, store.NewMessage{Body: "@Quill are you there?"})
		up()
		done <- err
	}()
	out, errOut, code := e.run(agent, "wait", "--mentions", "--timeout", "10")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if code != 0 || !strings.Contains(out, "@Quill are you there?") {
		t.Fatalf("wait across an outage at start: exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	for _, want := range []string{"unison wait: server unreachable: ", "unison wait: HTTP 503 Service Unavailable; retrying in "} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want a line with %q", errOut, want)
		}
	}
	if strings.Contains(errOut, "is it running?") || strings.Contains(errOut, e.url) {
		t.Errorf("retry lines should be short: %q", errOut)
	}

	// The server restarts under a parked poll. What is posted while it is
	// away is delivered when it is back: the cursor survives failed polls.
	go func() {
		time.Sleep(200 * time.Millisecond)
		up := e.down(emptyStatus(http.StatusBadGateway))
		e.ts.CloseClientConnections()
		time.Sleep(200 * time.Millisecond)
		_, _, err := e.st.Post(t.Context(), alex, store.NewMessage{Body: "@Quill posted during the restart"})
		up()
		done <- err
	}()
	out, errOut, code = e.run(agent, "wait", "--mentions", "--timeout", "10")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if code != 0 || !strings.Contains(out, "@Quill posted during the restart") {
		t.Fatalf("wait across a restart: exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if !strings.Contains(errOut, "retrying in ") {
		t.Errorf("a restart under the poll logged no retry: %q", errOut)
	}
}

// An outage longer than --timeout ends the wait as a quiet one does (exit
// code 2); the server's own refusals end it at once (#317).
func TestWaitGivesUp(t *testing.T) {
	e, _, _, agent := retryEnv(t)

	up := e.down(noAnswer)
	start := time.Now()
	_, errOut, code := e.run(agent, "wait", "--timeout", "1")
	if took := time.Since(start); code != 2 || !strings.Contains(errOut, "the server could not be reached") || took > 3*time.Second {
		t.Errorf("wait through a whole outage: exit %d after %s, %q; want exit 2 at about 1s", code, took, errOut)
	}
	up()

	for _, tc := range []struct {
		status   int
		wantCode int
	}{
		{http.StatusForbidden, exitLoggedOut},
		{http.StatusNotFound, 1},
		{http.StatusInternalServerError, 1},
	} {
		up := e.down(emptyStatus(tc.status))
		start := time.Now()
		_, errOut, code := e.run(agent, "wait", "--timeout", "5")
		if took := time.Since(start); code != tc.wantCode || strings.Contains(errOut, "retrying") || took > 2*time.Second {
			t.Errorf("wait against a %d: exit %d after %s, %q; want exit %d at once", tc.status, code, took, errOut, tc.wantCode)
		}
		up()
	}
	// Logged out is still exit 4, not something to wait out.
	up = e.down(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"logged out after 24 hours without activity","code":"idle"}`))
	})
	if _, errOut, code := e.run(agent, "wait", "--timeout", "5"); code != exitLoggedOut || strings.Contains(errOut, "retrying") {
		t.Errorf("wait when logged out: exit %d %q, want %d", code, errOut, exitLoggedOut)
	}
	up()

	// And once the server is back, wait works as before.
	if _, _, code := e.run(agent, "wait", "--timeout", "1"); code != 2 {
		t.Errorf("a quiet wait afterwards: exit %d, want 2", code)
	}
}

func participantNamed(t *testing.T, e *testEnv, ws, name string) *store.Participant {
	t.Helper()
	list, err := e.st.ListParticipants(t.Context(), ws)
	if err != nil {
		t.Fatal(err)
	}
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	t.Fatalf("no participant named %s", name)
	return nil
}
