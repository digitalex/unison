package analytics

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSend(t *testing.T) {
	var query, body string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	c := NewClient(Config{MeasurementID: "G-TEST", APISecret: "s3cret"})
	c.Endpoint = ts.URL
	if err := c.Send(t.Context(), "abc", "backlog_item_closed", map[string]string{"actor_type": "agent"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "measurement_id=G-TEST") || !strings.Contains(query, "api_secret=s3cret") {
		t.Errorf("query = %q", query)
	}
	var got struct {
		ClientID string `json:"client_id"`
		Events   []struct {
			Name   string            `json:"name"`
			Params map[string]string `json:"params"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.ClientID != "abc" || len(got.Events) != 1 || got.Events[0].Name != "backlog_item_closed" || got.Events[0].Params["actor_type"] != "agent" {
		t.Errorf("body = %s", body)
	}
}

func TestErrorsHideTheSecret(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	c := NewClient(Config{MeasurementID: "G-TEST", APISecret: "s3cret"})
	c.Endpoint = ts.URL
	err := c.Send(t.Context(), "abc", "x", nil)
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("bad status: %v", err)
	}
	ts.Close()
	err = c.Send(t.Context(), "abc", "x", nil)
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("connection failure: %v", err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := map[string]string{"UNISON_GA_MEASUREMENT_ID": "G-TEST"}
	if _, ok := ConfigFromEnv(func(k string) string { return env[k] }); ok {
		t.Error("ok without the API secret")
	}
	env["UNISON_GA_API_SECRET"] = "s"
	if _, ok := ConfigFromEnv(func(k string) string { return env[k] }); !ok {
		t.Error("not ok with both set")
	}
}
