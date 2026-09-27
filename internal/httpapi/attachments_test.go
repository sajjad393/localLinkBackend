package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"
	"time"

	"locallink/backend/internal/store"
)

func TestUploadDownloadAndAttachmentAuthorization(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t.Setenv("LOCALLINK_FILES_DIR", filepath.Join(t.TempDir(), "files"))
	api := New(st)
	register := func(id string) string {
		res := doJSON(t, api.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": id, "name": id, "platform": "android"}, nil)
		if res.Code != http.StatusCreated {
			t.Fatalf("register: %d %s", res.Code, res.Body.String())
		}
		var p struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p.Token
	}
	tokenA, tokenB, tokenC := register("a"), register("b"), register("c")
	headersA := map[string]string{"Authorization": "Bearer " + tokenA, "X-Device-ID": "a"}
	body := bytes.NewBuffer(nil)
	mw := multipart.NewWriter(body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="photo.txt"`)
	h.Set("Content-Type", "text/plain")
	part, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("hello-local-link"))
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/files", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for k, v := range headersA {
		req.Header.Set(k, v)
	}
	res := httptest.NewRecorder()
	api.UploadFile(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", res.Code, res.Body.String())
	}
	var payload struct {
		File Attachment `json:"file"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.File.ID == "" || payload.File.Size != int64(len("hello-local-link")) {
		t.Fatalf("bad metadata: %+v", payload.File)
	}
	msgRes := doJSON(t, api.CreateMessage, http.MethodPost, "/api/v1/messages", map[string]any{"id": "file-msg", "recipient_id": "b", "body": testDirectE2EEnvelope("file-msg", "a", "b"), "attachment_ids": []string{payload.File.ID}}, headersA)
	if msgRes.Code != http.StatusCreated {
		t.Fatalf("message attach: %d %s", msgRes.Code, msgRes.Body.String())
	}
	if !bytes.Contains(msgRes.Body.Bytes(), []byte(payload.File.ID)) {
		t.Fatal("attachment missing from message response")
	}
	// Recipient can download an attached file; unrelated device cannot.
	reqB := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+payload.File.ID+"/content", nil)
	reqB.SetPathValue("id", payload.File.ID)
	reqB.Header.Set("Authorization", "Bearer "+tokenB)
	reqB.Header.Set("X-Device-ID", "b")
	rb := httptest.NewRecorder()
	api.DownloadFile(rb, reqB)
	if rb.Code != http.StatusOK || rb.Body.String() != "hello-local-link" {
		t.Fatalf("recipient download: %d %s", rb.Code, rb.Body.String())
	}
	reqC := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+payload.File.ID+"/content", nil)
	reqC.SetPathValue("id", payload.File.ID)
	reqC.Header.Set("Authorization", "Bearer "+tokenC)
	reqC.Header.Set("X-Device-ID", "c")
	rc := httptest.NewRecorder()
	api.DownloadFile(rc, reqC)
	if rc.Code != http.StatusNotFound {
		t.Fatalf("unrelated device should not download: %d", rc.Code)
	}
}

func TestFileSizeLimit(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t.Setenv("LOCALLINK_FILES_DIR", filepath.Join(t.TempDir(), "files"))
	t.Setenv("LOCALLINK_MAX_FILE_MB", "1")
	api := New(st)
	res := doJSON(t, api.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": "a", "name": "a"}, nil)
	var p struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &p)
	data := bytes.Repeat([]byte("x"), 1024*1024+2)
	body := bytes.NewBuffer(nil)
	mw := multipart.NewWriter(body)
	part, _ := mw.CreateFormFile("file", "big.bin")
	_, _ = part.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/files", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+p.Token)
	req.Header.Set("X-Device-ID", "a")
	rr := httptest.NewRecorder()
	api.UploadFile(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected too large, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestCleanupOrphanFiles(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := filepath.Join(t.TempDir(), "files")
	t.Setenv("LOCALLINK_FILES_DIR", root)
	api := New(st)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := st.DB().Exec(`INSERT INTO devices(id,name,platform,created_at,last_seen_at,token_hash) VALUES(?,?,?,?,?,?)`, "owner", "Owner", "android", now, now, tokenHash("owner-token")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "old.bin")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("orphan"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339Nano)
	if _, err := st.DB().Exec(`INSERT INTO files(id,owner_id,original_name,content_type,size,sha256,storage_path,created_at,status) VALUES('orphan','owner','old.bin','application/octet-stream',6,'sha',?,?, 'ready')`, path, old); err != nil {
		t.Fatal(err)
	}
	removed, err := api.CleanupOrphanFiles(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("expected one orphan removed, got %d", removed)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("orphan file still exists: %v", err)
	}
}

func TestUploadClientFileIDIsIdempotentAndContentBound(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := filepath.Join(t.TempDir(), "files")
	t.Setenv("LOCALLINK_FILES_DIR", root)
	api := New(st)
	register := func(id string) string {
		res := doJSON(t, api.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": id, "name": id, "platform": "android"}, nil)
		if res.Code != http.StatusCreated {
			t.Fatalf("register %s: %d %s", id, res.Code, res.Body.String())
		}
		var p struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p.Token
	}
	tokenA := register("a")
	_ = register("b")
	upload := func(content string) *httptest.ResponseRecorder {
		body := bytes.NewBuffer(nil)
		mw := multipart.NewWriter(body)
		part, err := mw.CreateFormFile("file", "payload.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/files", body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+tokenA)
		req.Header.Set("X-Device-ID", "a")
		req.Header.Set("X-Client-File-ID", "stable-file-1")
		rr := httptest.NewRecorder()
		api.UploadFile(rr, req)
		return rr
	}
	first := upload("hello")
	if first.Code != http.StatusCreated {
		t.Fatalf("first upload: %d %s", first.Code, first.Body.String())
	}
	var p1 struct {
		File Attachment `json:"file"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &p1); err != nil {
		t.Fatal(err)
	}
	second := upload("hello")
	if second.Code != http.StatusOK {
		t.Fatalf("idempotent upload: %d %s", second.Code, second.Body.String())
	}
	var p2 struct {
		File Attachment `json:"file"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &p2); err != nil {
		t.Fatal(err)
	}
	if p2.File.ID != p1.File.ID || p2.File.SHA256 != p1.File.SHA256 {
		t.Fatalf("idempotent retry changed file: first=%+v second=%+v", p1.File, p2.File)
	}
	third := upload("different")
	if third.Code != http.StatusConflict {
		t.Fatalf("different content under same client id should conflict, got %d %s", third.Code, third.Body.String())
	}
}

func TestFileTransferConcurrencyGates(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t.Setenv("LOCALLINK_FILES_DIR", filepath.Join(t.TempDir(), "files"))
	a := New(st)

	for i := 0; i < cap(a.fileUploadGate); i++ {
		a.fileUploadGate <- struct{}{}
	}
	defer func() {
		for len(a.fileUploadGate) > 0 {
			<-a.fileUploadGate
		}
	}()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/files", nil)
	a.UploadFile(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("upload gate status=%d", w.Code)
	}

	for i := 0; i < cap(a.fileDownloadGate); i++ {
		a.fileDownloadGate <- struct{}{}
	}
	defer func() {
		for len(a.fileDownloadGate) > 0 {
			<-a.fileDownloadGate
		}
	}()
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/api/v1/files/x", nil)
	r.SetPathValue("id", "x")
	a.DownloadFile(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("download gate status=%d", w.Code)
	}
}
