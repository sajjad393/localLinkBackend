package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"locallink/backend/internal/store"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"
)

func TestE2EAttachmentServerStoresCiphertextOnly(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t.Setenv("LOCALLINK_FILES_DIR", filepath.Join(t.TempDir(), "files"))
	api := New(st)
	res := doJSON(t, api.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{"id": "a", "name": "a", "platform": "android"}, nil)
	var rp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &rp)
	plaintext := []byte("secret attachment plaintext")
	ciphertext := []byte("ciphertext-not-plaintext")
	body := bytes.NewBuffer(nil)
	mw := multipart.NewWriter(body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="blob.bin"`)
	h.Set("Content-Type", "application/octet-stream")
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(ciphertext)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/files", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+rp.Token)
	req.Header.Set("X-Device-ID", "a")
	req.Header.Set("X-File-Crypto-Version", "e2e:v1")
	req.Header.Set("X-Client-File-ID", "e2e-file-1")
	req.Header.Set("X-File-Original-Name", "secret.txt")
	req.Header.Set("X-File-Content-Type", "text/plain")
	req.Header.Set("X-File-Plaintext-Size", "27")
	req.Header.Set("X-File-Plaintext-SHA256", "9f94eab20ffd966e2e0b19d71df22604a335b6faceff09e6a8ad16bf58ed94fc")
	req.Header.Set("X-File-E2E-Scope", "direct")
	req.Header.Set("X-File-Key-Version", "1")
	req.Header.Set("X-File-Nonce", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 12)))
	req.Header.Set("X-File-Mac", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 16)))
	rr := httptest.NewRecorder()
	api.UploadFile(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload=%d %s", rr.Code, rr.Body.String())
	}
	var payload struct {
		File Attachment `json:"file"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(func() string {
		var p string
		_ = st.DB().QueryRow(`SELECT storage_path FROM files WHERE id=?`, payload.File.ID).Scan(&p)
		return p
	}())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, plaintext) {
		t.Fatal("server storage contains plaintext")
	}
	if !bytes.Equal(data, ciphertext) {
		t.Fatalf("stored bytes changed: %q", data)
	}
	if payload.File.ID == "" || payload.File.SHA256 == "" {
		t.Fatalf("unexpected attachment payload: %+v", payload.File)
	}
}
