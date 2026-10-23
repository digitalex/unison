package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// call makes an API request and returns the status and decoded JSON body.
func call(t *testing.T, method, url, token, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func TestMetricsAPI(t *testing.T) {
	ts, st := newServer(t)
	ws, _, _ := st.CreateWorkspace(t.Context(), "W", "", "")
	_, token, _, _ := st.Join(t.Context(), ws.ID, "Ada", "")
	base := ts.URL + "/api/workspaces/" + ws.ID + "/metrics"

	if status, _ := call(t, "POST", base, "", `{"name":"Uptime"}`); status != http.StatusUnauthorized {
		t.Errorf("unauthenticated add = %d, want 401", status)
	}
	status, m := call(t, "POST", base, token, `{"name":"Uptime","direction":"increase","unit":"%","target":99.9}`)
	if status != http.StatusCreated || m["item_id"] == nil || m["target"] != 99.9 {
		t.Fatalf("add = %d %v", status, m)
	}
	id := int64(m["id"].(float64))
	itemID := int64(m["item_id"].(float64))
	if it, _, err := st.GetItem(t.Context(), ws.ID, itemID); err != nil || it.Title != "Track metric: Uptime" {
		t.Errorf("tracking item = %+v, %v", it, err)
	}
	if status, _ := call(t, "POST", base, token, `{"name":"uptime"}`); status != http.StatusConflict {
		t.Errorf("duplicate add = %d, want 409", status)
	}
	if status, _ := call(t, "POST", base, token, `{"name":"Speed","direction":"faster"}`); status != http.StatusBadRequest {
		t.Errorf("bad direction = %d, want 400", status)
	}
	metricURL := base + "/" + strconv.FormatInt(id, 10)
	if status, got := call(t, "PATCH", metricURL, token, `{"target":null,"unit":"percent"}`); status != http.StatusOK || got["target"] != nil || got["unit"] != "percent" {
		t.Errorf("edit = %d %v", status, got)
	}
	if status, got := call(t, "GET", metricURL, "", ""); status != http.StatusOK || got["name"] != "Uptime" {
		t.Errorf("get = %d %v", status, got)
	}
	if status, _ := call(t, "DELETE", metricURL, token, ""); status != http.StatusOK {
		t.Errorf("delete = %d", status)
	}
	if status, _ := call(t, "GET", metricURL, "", ""); status != http.StatusNotFound {
		t.Errorf("get deleted = %d, want 404", status)
	}
	if status, _ := call(t, "GET", base+"/abc", "", ""); status != http.StatusBadRequest {
		t.Errorf("non-numeric id = %d, want 400", status)
	}
	if _, body := get(t, base); body != "[]\n" {
		t.Errorf("list after delete = %q", body)
	}
}
