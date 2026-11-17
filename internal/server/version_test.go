package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitalex/unison/internal/store"
	"github.com/digitalex/unison/internal/store/storetest"
)

func TestServerVersionHeaderAndEndpoint(t *testing.T) {
	st, err := store.Open(storetest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(st, nil)
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)

	// Unstamped dev build: no X-Unison-Version header, /api/version returns "dev".
	resp, err := http.Get(ts.URL + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Unison-Version"); got != "" {
		t.Fatalf("unstamped X-Unison-Version = %q, want empty", got)
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Version != "dev" {
		t.Fatalf("unstamped /api/version = %q, want dev", body.Version)
	}

	// Stamped build: sets X-Unison-Version on /api/ routes and returns it on /api/version.
	s.SetVersion("v1.2.3")
	resp2, err := http.Get(ts.URL + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if got := resp2.Header.Get("X-Unison-Version"); got != "v1.2.3" {
		t.Fatalf("stamped X-Unison-Version = %q, want v1.2.3", got)
	}
	if err := json.NewDecoder(resp2.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Version != "v1.2.3" {
		t.Fatalf("stamped /api/version = %q, want v1.2.3", body.Version)
	}
}
