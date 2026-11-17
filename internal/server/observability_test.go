package server

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

func TestStoreErrorCanceledReturns499WithoutInternalErrorLog(t *testing.T) {
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(old) })

	rec := httptest.NewRecorder()
	storeError(rec, fmt.Errorf("query: %w", context.Canceled))

	if rec.Code != statusClientClosedRequest {
		t.Fatalf("status = %d, want %d", rec.Code, statusClientClosedRequest)
	}
	if strings.Contains(logs.String(), "internal error") {
		t.Fatalf("unexpected internal error log: %q", logs.String())
	}
}

func TestStatusRecorderImplementsFlusherAndLogs5xx(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
	if _, ok := any(sr).(http.Flusher); !ok {
		t.Fatal("statusRecorder must implement http.Flusher")
	}
	sr.Flush()
	sr.WriteHeader(http.StatusInternalServerError)
	if sr.status != http.StatusInternalServerError {
		t.Fatalf("sr.status = %d, want 500", sr.status)
	}
}

func TestCountEventType(t *testing.T) {
	evs := []store.Event{
		{Type: "comment.added"},
		{Type: "backlog.added"},
		{Type: "backlog.added"},
	}
	if got := countEventType(evs, "backlog.added"); got != 2 {
		t.Fatalf("countEventType = %d, want 2", got)
	}
	if got := countEventType(nil, "backlog.added"); got != 0 {
		t.Fatalf("countEventType(nil) = %d, want 0", got)
	}
}
