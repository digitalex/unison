package server

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/digitalex/unison/internal/store"
)

func TestArtifactsAPI(t *testing.T) {
	ts, st := newServer(t)
	ctx := t.Context()
	u, _ := st.LoginWithProvider(ctx, "github", store.Profile{Subject: "ada", Name: "Ada"})
	userTok, _, _ := st.CreateSession(ctx, u.ID, store.SessionCLI, "")
	_, ws := call(t, "POST", ts.URL+"/api/workspaces", userTok, `{"name":"Artifacts"}`)
	wsID := ws["id"].(string)
	ada, token, _, _, _ := st.JoinAsUser(ctx, wsID, u.ID, "Ada", "🦊")
	item, _, err := st.AddItem(ctx, ada, "Design doc item", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	base := ts.URL + "/api/workspaces/" + wsID + "/artifacts"

	rawReq := func(method, url, tok, contentType string, body []byte, hdrs map[string]string) *http.Response {
		t.Helper()
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, url, rd)
		if err != nil {
			t.Fatal(err)
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for k, v := range hdrs {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	decodeMap := func(resp *http.Response) map[string]any {
		t.Helper()
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	// Open an SSE stream to verify artifact.updated events arrive live.
	streamClient := &http.Client{Timeout: 5 * time.Second}
	sseResp, err := streamClient.Get(ts.URL + "/api/workspaces/" + wsID + "/stream?token=" + token)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer sseResp.Body.Close()
	sseReader := bufio.NewReader(sseResp.Body)
	// Read ready event
	if line, err := sseReader.ReadString('\n'); err != nil || !strings.Contains(line, "ready") {
		t.Fatalf("SSE ready line = %q, %v", line, err)
	}

	readNextSSEType := func() string {
		t.Helper()
		for {
			line, err := sseReader.ReadString('\n')
			if err != nil {
				t.Fatalf("reading SSE: %v", err)
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				t.Fatalf("invalid SSE JSON %q: %v", line, err)
			}
			if ev.Type == "" {
				continue // skip ready event's data line
			}
			return ev.Type
		}
	}

	// 1. Raw body POST upload
	v1Body := []byte("# Architecture v1\n\nHello world.")
	v1Sum := sha256.Sum256(v1Body)
	v1Hex := hex.EncodeToString(v1Sum[:])
	v1ETag := `"` + v1Hex + `"`

	resp := rawReq("POST", fmt.Sprintf("%s?name=design-doc.md&title=System+Design&item_id=%d", base, item.ID), token, "application/octet-stream", v1Body, nil)
	created := decodeMap(resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST artifact = %d %v", resp.StatusCode, created)
	}
	if created["name"] != "design-doc.md" || created["title"] != "System Design" ||
		created["url"] != "/w/"+wsID+"/artifacts/design-doc.md" ||
		created["raw_url"] != "/api/workspaces/"+wsID+"/artifacts/design-doc.md" ||
		created["sha256"] != v1Hex || created["version"] != float64(1) {
		t.Fatalf("created metadata = %v", created)
	}
	if gotEv := readNextSSEType(); gotEv != "artifact.created" {
		t.Fatalf("SSE event after POST = %q, want artifact.created", gotEv)
	}

	// Duplicate POST returns 409 Conflict
	resp = rawReq("POST", base+"?name=DESIGN-DOC.MD", token, "application/octet-stream", v1Body, nil)
	dupOut := decodeMap(resp)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate POST = %d %v, want 409", resp.StatusCode, dupOut)
	}

	// Path traversal returns 400 Bad Request
	for _, bad := range []string{"..%2Fsecret.md", "foo..bar.md", "%20"} {
		resp = rawReq("POST", base+"?name="+bad, token, "application/octet-stream", v1Body, nil)
		_ = decodeMap(resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("POST bad name %q = %d, want 400", bad, resp.StatusCode)
		}
	}

	// Oversized upload returns 413
	big := make([]byte, store.MaxArtifactBytes+1024)
	resp = rawReq("POST", base+"?name=big.bin", token, "application/octet-stream", big, nil)
	_ = decodeMap(resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized POST = %d, want 413", resp.StatusCode)
	}

	// 2. Multipart upload of HTML prototype
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	_ = mw.WriteField("title", "Interactive Prototype")
	fw, _ := mw.CreateFormFile("file", "proto.html")
	_, _ = fw.Write([]byte("<!doctype html><script>window.x=1</script><h1>Proto</h1>"))
	_ = mw.Close()

	resp = rawReq("POST", base, token, mw.FormDataContentType(), mp.Bytes(), nil)
	proto := decodeMap(resp)
	if resp.StatusCode != http.StatusCreated || proto["name"] != "proto.html" || proto["title"] != "Interactive Prototype" {
		t.Fatalf("multipart POST = %d %v", resp.StatusCode, proto)
	}
	if gotEv := readNextSSEType(); gotEv != "artifact.created" {
		t.Fatalf("SSE event after multipart POST = %q, want artifact.created", gotEv)
	}

	// 3. GET list & ?item_id filter & ?meta=1
	resp = rawReq("GET", base, token, "", nil, nil)
	var list []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(list) != 2 {
		t.Fatalf("GET list = %d %v", resp.StatusCode, list)
	}

	resp = rawReq("GET", fmt.Sprintf("%s?item_id=%d", base, item.ID), token, "", nil, nil)
	var itemFiltered []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&itemFiltered)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(itemFiltered) != 1 || itemFiltered[0]["name"] != "design-doc.md" {
		t.Fatalf("GET list?item_id = %d %v", resp.StatusCode, itemFiltered)
	}

	resp = rawReq("GET", base+"/design-doc.md?meta=1&content=1", token, "", nil, nil)
	metaOut := decodeMap(resp)
	if resp.StatusCode != http.StatusOK || metaOut["sha256"] != v1Hex || metaOut["content"] != string(v1Body) {
		t.Fatalf("GET ?meta=1&content=1 = %d %v", resp.StatusCode, metaOut)
	}

	// 4. GET raw content: Cache-Control, strong ETag, CSP sandbox, nosniff, and 304 Not Modified!
	resp = rawReq("GET", base+"/design-doc.md", token, "", nil, nil)
	gotRaw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Equal(gotRaw, v1Body) {
		t.Fatalf("GET raw = %d %q", resp.StatusCode, gotRaw)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "private, no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "private, no-cache")
	}
	if et := resp.Header.Get("ETag"); et != v1ETag {
		t.Errorf("ETag = %q, want %q", et, v1ETag)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP = %q, want sandbox + default-src 'none'", csp)
	}
	if xcto := resp.Header.Get("X-Content-Type-Options"); xcto != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", xcto)
	}

	// Matching If-None-Match (single, comma-separated, *) -> 304 Not Modified with empty body
	for _, inm := range []string{v1ETag, `"other", ` + v1ETag, `*`} {
		resp = rawReq("GET", base+"/design-doc.md", token, "", nil, map[string]string{"If-None-Match": inm})
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotModified || len(b) != 0 || resp.Header.Get("ETag") != v1ETag {
			t.Errorf("GET with If-None-Match %q = %d (%d bytes, ETag=%q), want 304 empty", inm, resp.StatusCode, len(b), resp.Header.Get("ETag"))
		}
	}

	// 5. In-place PUT replace: keeps permalink, increments version, updates ETag, emits artifact.updated, and invalidates old ETag immediately!
	v2Body := []byte("# Architecture v2\n\nReplaced in-place!")
	v2Sum := sha256.Sum256(v2Body)
	v2Hex := hex.EncodeToString(v2Sum[:])
	v2ETag := `"` + v2Hex + `"`

	resp = rawReq("PUT", base+"/design-doc.md", token, "text/markdown", v2Body, nil)
	replaced := decodeMap(resp)
	if resp.StatusCode != http.StatusOK || replaced["version"] != float64(2) || replaced["sha256"] != v2Hex ||
		replaced["url"] != "/w/"+wsID+"/artifacts/design-doc.md" {
		t.Fatalf("PUT replace = %d %v", resp.StatusCode, replaced)
	}
	if gotEv := readNextSSEType(); gotEv != "artifact.updated" {
		t.Fatalf("SSE event after PUT = %q, want artifact.updated", gotEv)
	}

	// Old ETag now gets 200 OK with v2Body and new v2ETag!
	resp = rawReq("GET", base+"/design-doc.md", token, "", nil, map[string]string{"If-None-Match": v1ETag})
	gotAfterPut, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Equal(gotAfterPut, v2Body) || resp.Header.Get("ETag") != v2ETag {
		t.Fatalf("GET with stale ETag after PUT = %d %q ETag=%q, want 200 v2Body %q", resp.StatusCode, gotAfterPut, resp.Header.Get("ETag"), v2ETag)
	}
	// New ETag gets 304 Not Modified!
	resp = rawReq("GET", base+"/design-doc.md", token, "", nil, map[string]string{"If-None-Match": v2ETag})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("GET with fresh v2ETag = %d, want 304", resp.StatusCode)
	}

	// 6. PATCH metadata and base64 content
	v3Body := []byte("# Architecture v3 via PATCH")
	patchPayload := fmt.Sprintf(`{"title":"Updated Title","item_id":0,"content_base64":%q}`, base64.StdEncoding.EncodeToString(v3Body))
	resp = rawReq("PATCH", base+"/design-doc.md", token, "application/json", []byte(patchPayload), nil)
	patched := decodeMap(resp)
	if resp.StatusCode != http.StatusOK || patched["title"] != "Updated Title" || patched["version"] != float64(3) || patched["item_id"] != nil {
		t.Fatalf("PATCH = %d %v", resp.StatusCode, patched)
	}
	if gotEv := readNextSSEType(); gotEv != "artifact.updated" {
		t.Fatalf("SSE event after PATCH = %q, want artifact.updated", gotEv)
	}

	// 7. DELETE artifact
	resp = rawReq("DELETE", base+"/design-doc.md", token, "", nil, nil)
	delOut := decodeMap(resp)
	if resp.StatusCode != http.StatusOK || delOut["deleted"] != "design-doc.md" {
		t.Fatalf("DELETE = %d %v", resp.StatusCode, delOut)
	}
	if gotEv := readNextSSEType(); gotEv != "artifact.deleted" {
		t.Fatalf("SSE event after DELETE = %q, want artifact.deleted", gotEv)
	}
	resp = rawReq("GET", base+"/design-doc.md", token, "", nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET deleted artifact = %d, want 404", resp.StatusCode)
	}
}
