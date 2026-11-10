package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/digitalex/unison/internal/store"
)

// testPNG is a 1×1 PNG.
var testPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

func TestImagesAPI(t *testing.T) {
	ts, st := newServer(t)
	u, _ := st.LoginWithProvider(t.Context(), "github", store.Profile{Subject: "ada", Name: "Ada"})
	userTok, _, _ := st.CreateSession(t.Context(), u.ID, store.SessionCLI, "")
	_, ws := call(t, "POST", ts.URL+"/api/workspaces", userTok, `{"name":"Shots"}`)
	wsID := ws["id"].(string)
	_, token, _, _, _ := st.JoinAsUser(t.Context(), wsID, u.ID, "Ada", "")
	base := ts.URL + "/api/workspaces/" + wsID + "/images"

	upload := func(url, tok, contentType string, body []byte) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		req.Header.Set("Content-Type", contentType)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	if status, _ := upload(base, "", "image/png", testPNG); status != http.StatusUnauthorized {
		t.Errorf("anonymous upload = %d, want 401", status)
	}
	status, raw := upload(base+"?name=login.png", token, "application/octet-stream", testPNG)
	if status != http.StatusCreated || raw["name"] != "login.png" || raw["content_type"] != "image/png" ||
		raw["markdown"] != "![login.png]("+raw["url"].(string)+")" {
		t.Fatalf("raw upload = %d %v", status, raw)
	}

	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	mw.WriteField("note", "ignored")
	fw, _ := mw.CreateFormFile("file", "Screenshot 2026-11-10.png")
	fw.Write(testPNG)
	mw.Close()
	status, form := upload(base, token, mw.FormDataContentType(), mp.Bytes())
	if status != http.StatusCreated || form["name"] != "Screenshot 2026-11-10.png" {
		t.Fatalf("multipart upload = %d %v", status, form)
	}

	if status, out := upload(base, token, "image/png", []byte("<svg onload=alert(1)>")); status != http.StatusBadRequest {
		t.Errorf("non-image upload = %d %v, want 400", status, out)
	}
	big := append(append([]byte{}, testPNG...), make([]byte, store.MaxImageBytes+128<<10)...)
	if status, out := upload(base, token, "image/png", big); status != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized upload = %d %v, want 413", status, out)
	}

	get := func(url, tok string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", url, nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := get(ts.URL+raw["url"].(string), token)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	h := resp.Header
	if resp.StatusCode != 200 || !bytes.Equal(body, testPNG) || h.Get("Content-Type") != "image/png" ||
		h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("GET image = %d, %d bytes, headers %v", resp.StatusCode, len(body), h)
	}
	if resp := get(ts.URL+raw["url"].(string), ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous GET image = %d, want 401", resp.StatusCode)
	}
	if resp := get(base+"/999", token); resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing image = %d, want 404", resp.StatusCode)
	}
}
