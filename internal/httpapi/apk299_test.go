package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// Bundle 299: the server APK (check, owner page, public page, phones).

// ------------------------------------------------------------ fake APKs

func cert299(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// axml299 builds a binary AndroidManifest.xml like aapt2 does (UTF-16 pool,
// resource map for versionCode/versionName, <manifest> start element).
func axml299(pkg string, code int64, name string) []byte {
	strs := []string{"versionCode", "versionName", "package", "manifest", pkg, name}
	le := binary.LittleEndian
	var data bytes.Buffer
	var offs []uint32
	for _, s := range strs {
		offs = append(offs, uint32(data.Len()))
		u := utf16.Encode([]rune(s))
		_ = binary.Write(&data, le, uint16(len(u)))
		for _, c := range u {
			_ = binary.Write(&data, le, c)
		}
		_ = binary.Write(&data, le, uint16(0))
	}
	for data.Len()%4 != 0 {
		data.WriteByte(0)
	}
	var pool bytes.Buffer
	hsize := 28
	start := hsize + 4*len(strs)
	_ = binary.Write(&pool, le, uint16(0x0001))
	_ = binary.Write(&pool, le, uint16(hsize))
	_ = binary.Write(&pool, le, uint32(start+data.Len()))
	_ = binary.Write(&pool, le, uint32(len(strs)))
	_ = binary.Write(&pool, le, uint32(0)) // styles
	_ = binary.Write(&pool, le, uint32(0)) // flags (UTF-16)
	_ = binary.Write(&pool, le, uint32(start))
	_ = binary.Write(&pool, le, uint32(0))
	for _, o := range offs {
		_ = binary.Write(&pool, le, o)
	}
	pool.Write(data.Bytes())

	var resmap bytes.Buffer
	_ = binary.Write(&resmap, le, uint16(0x0180))
	_ = binary.Write(&resmap, le, uint16(8))
	_ = binary.Write(&resmap, le, uint32(16))
	_ = binary.Write(&resmap, le, uint32(0x0101021b))
	_ = binary.Write(&resmap, le, uint32(0x0101021c))

	attr := func(b *bytes.Buffer, nameIdx, raw uint32, dtype byte, val uint32) {
		_ = binary.Write(b, le, uint32(0xffffffff)) // ns
		_ = binary.Write(b, le, nameIdx)
		_ = binary.Write(b, le, raw)
		_ = binary.Write(b, le, uint16(8))
		b.WriteByte(0)
		b.WriteByte(dtype)
		_ = binary.Write(b, le, val)
	}
	var attrs bytes.Buffer
	attr(&attrs, 0, 0xffffffff, 0x10, uint32(code))
	attr(&attrs, 1, 5, 0x03, 5)
	attr(&attrs, 2, 4, 0x03, 4)
	var el bytes.Buffer
	_ = binary.Write(&el, le, uint16(0x0102))
	_ = binary.Write(&el, le, uint16(16))
	_ = binary.Write(&el, le, uint32(16+20+attrs.Len()))
	_ = binary.Write(&el, le, uint32(1))          // line
	_ = binary.Write(&el, le, uint32(0xffffffff)) // comment
	_ = binary.Write(&el, le, uint32(0xffffffff)) // ns
	_ = binary.Write(&el, le, uint32(3))          // name "manifest"
	_ = binary.Write(&el, le, uint16(20))         // attrStart
	_ = binary.Write(&el, le, uint16(20))         // attrSize
	_ = binary.Write(&el, le, uint16(3))          // attrCount
	_ = binary.Write(&el, le, uint16(0))
	_ = binary.Write(&el, le, uint16(0))
	_ = binary.Write(&el, le, uint16(0))
	el.Write(attrs.Bytes())

	var out bytes.Buffer
	_ = binary.Write(&out, le, uint16(0x0003))
	_ = binary.Write(&out, le, uint16(8))
	_ = binary.Write(&out, le, uint32(8+pool.Len()+resmap.Len()+el.Len()))
	out.Write(pool.Bytes())
	out.Write(resmap.Bytes())
	out.Write(el.Bytes())
	return out.Bytes()
}

func lpw299(b []byte) []byte {
	out := make([]byte, 4, 4+len(b))
	binary.LittleEndian.PutUint32(out, uint32(len(b)))
	return append(out, b...)
}

// apk299Bytes: a zip with the manifest and (when cert != nil) a v2 APK
// Signing Block before the central directory.
func apk299Bytes(t *testing.T, pkg string, code int64, name string, cert []byte) []byte {
	t.Helper()
	var z bytes.Buffer
	zw := zip.NewWriter(&z)
	w, _ := zw.Create("AndroidManifest.xml")
	_, _ = w.Write(axml299(pkg, code, name))
	w, _ = zw.Create("classes.dex")
	_, _ = w.Write(bytes.Repeat([]byte("dex\n"), 64))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	raw := z.Bytes()
	if cert == nil {
		return raw
	}
	eocd := bytes.LastIndex(raw, []byte{0x50, 0x4b, 0x05, 0x06})
	cdOff := binary.LittleEndian.Uint32(raw[eocd+16:])
	// signed data = digests, certificates, attributes
	signed := append(append(lpw299(nil), lpw299(lpw299(cert))...), lpw299(nil)...)
	signer := append(append(lpw299(signed), lpw299(nil)...), lpw299([]byte("pubkey"))...)
	value := lpw299(lpw299(signer))
	pair := make([]byte, 12)
	binary.LittleEndian.PutUint64(pair, uint64(4+len(value)))
	binary.LittleEndian.PutUint32(pair[8:], 0x7109871a)
	pair = append(pair, value...)
	blockSize := uint64(len(pair) + 24)
	block := make([]byte, 8)
	binary.LittleEndian.PutUint64(block, blockSize)
	block = append(block, pair...)
	tail := make([]byte, 8)
	binary.LittleEndian.PutUint64(tail, blockSize)
	block = append(append(block, tail...), []byte("APK Sig Block 42")...)
	out := append([]byte{}, raw[:cdOff]...)
	out = append(out, block...)
	out = append(out, raw[cdOff:]...)
	e := eocd + len(block)
	binary.LittleEndian.PutUint32(out[e+16:], cdOff+uint32(len(block)))
	return out
}

func writeAPK299(t *testing.T, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.apk")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func certHex299(c []byte) string {
	s := sha256.Sum256(c)
	return hex.EncodeToString(s[:])
}

// ------------------------------------------------------------ check

func TestReadAPK299ReadsNameVersionAndKey(t *testing.T) {
	c := cert299(t, "sajjad")
	info, err := readAPK299(writeAPK299(t, apk299Bytes(t, "com.sajjad.linkup", 167, "3.0.96", c)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Package != "com.sajjad.linkup" || info.VersionCode != 167 || info.VersionName != "3.0.96" || info.CertSHA256 != certHex299(c) {
		t.Fatalf("got %+v", info)
	}
}

func TestReadAPK299Refusals(t *testing.T) {
	if _, err := readAPK299(writeAPK299(t, []byte("not a zip at all"))); err != errNotAPK299 {
		t.Fatalf("not zip: %v", err)
	}
	var z bytes.Buffer
	zw := zip.NewWriter(&z)
	w, _ := zw.Create("hello.txt")
	_, _ = w.Write([]byte("hi"))
	_ = zw.Close()
	if _, err := readAPK299(writeAPK299(t, z.Bytes())); err != errNotAPK299 {
		t.Fatalf("zip without manifest: %v", err)
	}
	if _, err := readAPK299(writeAPK299(t, apk299Bytes(t, "com.sajjad.linkup", 167, "3.0.96", nil))); err != errNoSigner299 {
		t.Fatalf("no v2 block: %v", err)
	}
}

func TestCheckAPK299Rules(t *testing.T) {
	ok := apkInfo299{Package: "com.sajjad.linkup", VersionCode: 167, VersionName: "3.0.96", CertSHA256: "aa"}
	if err := checkAPK299(ok, 0, "", ""); err != nil {
		t.Fatalf("first APK: %v", err)
	}
	if err := checkAPK299(ok, 166, "3.0.95", "AA"); err != nil {
		t.Fatalf("newer, same key: %v", err)
	}
	other := ok
	other.Package = "com.whatsapp"
	if err := checkAPK299(other, 0, "", ""); err == nil || !strings.Contains(err.Error(), "not LinkUp") {
		t.Fatalf("other app: %v", err)
	}
	if err := checkAPK299(ok, 167, "3.0.96", "aa"); err == nil || err.Error() != "This APK is LinkUp 3.0.96 (build 167). It must be newer than 3.0.96 (build 167)." {
		t.Fatalf("same build: %v", err)
	}
	if err := checkAPK299(ok, 166, "3.0.95", "bb"); err == nil || !strings.Contains(err.Error(), "different key") {
		t.Fatalf("other key: %v", err)
	}
}

func TestAPKFileName299IsSafe(t *testing.T) {
	if n := apkFileName299("3.0.96", 167); n != "LinkUp-3.0.96.apk" {
		t.Fatal(n)
	}
	if n := apkFileName299("../../etc/x y", 9); strings.ContainsAny(n, "/ \\") || strings.Contains(n, "..") {
		t.Fatal(n)
	}
	if n := apkFileName299("", 9); n != "LinkUp-9.apk" {
		t.Fatal(n)
	}
}

// ------------------------------------------------------------ handlers

type apkEnv299 struct {
	api   *API
	token string
}

func newAPKEnv299(t *testing.T) apkEnv299 {
	t.Helper()
	t.Setenv("LOCALLINK_APK_DIR", filepath.Join(t.TempDir(), "apk"))
	_, api := newTestAPI(t)
	if err := api.ensureBootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	return apkEnv299{api: api, token: adminLogin211(t, api, "10.29.9.1").Token}
}

func (e apkEnv299) upload(t *testing.T, b []byte) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("apk", "app-release.apk")
	_, _ = fw.Write(b)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/apk299", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+e.token)
	rec := httptest.NewRecorder()
	e.api.AdminAPK299Upload(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (e apkEnv299) get(t *testing.T, h http.HandlerFunc, path, file string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "192.168.18.40:5555"
	if file != "" {
		req.SetPathValue("file", file)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestAPK299UploadReplaceRemove(t *testing.T) {
	e := newAPKEnv299(t)
	key := cert299(t, "sajjad")
	dir := os.Getenv("LOCALLINK_APK_DIR")

	// Not LinkUp: refused, nothing saved.
	code, out := e.upload(t, apk299Bytes(t, "com.other.app", 200, "9.9", key))
	if code != http.StatusUnprocessableEntity || out["error"] != "This is not LinkUp. Pick the LinkUp APK." {
		t.Fatalf("other app: %d %v", code, out)
	}
	// First LinkUp APK.
	code, out = e.upload(t, apk299Bytes(t, "com.sajjad.linkup", 166, "3.0.95", key))
	if code != http.StatusOK || out["version"] != "3.0.95" {
		t.Fatalf("first: %d %v", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "LinkUp-3.0.95.apk")); err != nil {
		t.Fatal(err)
	}
	// Older or same: refused with both versions in plain words.
	code, out = e.upload(t, apk299Bytes(t, "com.sajjad.linkup", 166, "3.0.95", key))
	if code != http.StatusUnprocessableEntity || !strings.Contains(out["error"].(string), "must be newer than 3.0.95 (build 166)") {
		t.Fatalf("same: %d %v", code, out)
	}
	// Other key: refused.
	code, out = e.upload(t, apk299Bytes(t, "com.sajjad.linkup", 167, "3.0.96", cert299(t, "someone")))
	if code != http.StatusUnprocessableEntity || !strings.Contains(out["error"].(string), "different key") {
		t.Fatalf("key: %d %v", code, out)
	}
	// Newer: the old file is deleted, history keeps both.
	code, out = e.upload(t, apk299Bytes(t, "com.sajjad.linkup", 167, "3.0.96", key))
	if code != http.StatusOK {
		t.Fatalf("newer: %d %v", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "LinkUp-3.0.95.apk")); !os.IsNotExist(err) {
		t.Fatalf("old file must be deleted: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 1 {
		t.Fatalf("only the current file stays: %v", files)
	}
	hist := out["history"].([]any)
	if len(hist) != 2 || hist[0].(map[string]any)["version"] != "3.0.96" || hist[1].(map[string]any)["why"] != "replaced" {
		t.Fatalf("history: %v", hist)
	}
	if s, _ := out["qr_svg"].(string); !strings.HasPrefix(s, "<svg") {
		t.Fatalf("qr: %q", s)
	}
	// Remove.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/apk299", nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	e.api.AdminAPK299Delete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	files, _ = filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 0 {
		t.Fatalf("file must be gone: %v", files)
	}
	if e.api.currentAPK299() != nil {
		t.Fatal("no current after remove")
	}
}

func TestAPK299OwnerOnly(t *testing.T) {
	e := newAPKEnv299(t)
	if _, err := e.api.store.DB().Exec(`UPDATE admin_accounts SET role='operator'`); err != nil {
		t.Fatal(err)
	}
	rec := e.get(t, e.api.AdminAPK299, "/api/v1/admin/apk299", "", map[string]string{"Authorization": "Bearer " + e.token})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator GET: %d", rec.Code)
	}
	code, _ := e.upload(t, apk299Bytes(t, "com.sajjad.linkup", 166, "3.0.95", cert299(t, "k")))
	if code != http.StatusForbidden {
		t.Fatalf("operator upload: %d", code)
	}
	rec = e.get(t, e.api.AdminAPK299, "/api/v1/admin/apk299", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no sign-in: %d", rec.Code)
	}
}

func TestAPK299PublicPageFileAndCount(t *testing.T) {
	e := newAPKEnv299(t)
	page := e.get(t, e.api.LinkupPage299, "/download", "", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "<svg") || strings.Contains(page.Body.String(), "<script") {
		t.Fatalf("empty page: %d", page.Code)
	}
	b := apk299Bytes(t, "com.sajjad.linkup", 166, "3.0.95", cert299(t, "k"))
	if code, out := e.upload(t, b); code != http.StatusOK {
		t.Fatalf("upload %d %v", code, out)
	}
	page = e.get(t, e.api.LinkupPage299, "/LinkUp", "", nil)
	if !strings.Contains(page.Body.String(), `href="/download/LinkUp-3.0.95.apk"`) {
		t.Fatalf("page has no download link:\n%s", page.Body.String())
	}
	if csp := page.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("csp: %q", csp)
	}
	if e.get(t, e.api.LinkupFile299, "/download/old.apk", "old.apk", nil).Code != http.StatusNotFound {
		t.Fatal("unknown file must be 404")
	}
	f := e.get(t, e.api.LinkupFile299, "/download/LinkUp-3.0.95.apk", "LinkUp-3.0.95.apk", nil)
	if f.Code != http.StatusOK || !bytes.Equal(f.Body.Bytes(), b) || f.Header().Get("Content-Type") != "application/vnd.android.package-archive" {
		t.Fatalf("file: %d %d bytes %q", f.Code, f.Body.Len(), f.Header().Get("Content-Type"))
	}
	// Same phone again within 10 min and a resume: still 1.
	e.get(t, e.api.LinkupFile299, "/download/LinkUp-3.0.95.apk", "LinkUp-3.0.95.apk", nil)
	part := e.get(t, e.api.LinkupFile299, "/download/LinkUp-3.0.95.apk", "LinkUp-3.0.95.apk", map[string]string{"Range": "bytes=10-"})
	if part.Code != http.StatusPartialContent {
		t.Fatalf("resume: %d", part.Code)
	}
	var n int
	_ = e.api.store.DB().QueryRow(`SELECT downloads FROM app_apk_299 WHERE current=1`).Scan(&n)
	if n != 1 || e.api.currentAPK299().Downloads != 1 {
		t.Fatalf("downloads %d / %d", n, e.api.currentAPK299().Downloads)
	}
}

func TestAppLatest299(t *testing.T) {
	e := newAPKEnv299(t)
	user := registerUser(t, e.api, "ali299", "dev-ali-299")
	hdr := map[string]string{"X-Device-ID": user.Device.ID, "Authorization": "Bearer " + user.Token}
	if rec := e.get(t, e.api.AppLatest299, "/api/v1/app/latest", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no sign-in: %d", rec.Code)
	}
	if rec := e.get(t, e.api.AppLatest299, "/api/v1/app/latest", "", hdr); rec.Code != http.StatusNoContent {
		t.Fatalf("no APK: %d", rec.Code)
	}
	b := apk299Bytes(t, "com.sajjad.linkup", 166, "3.0.95", cert299(t, "k"))
	if code, _ := e.upload(t, b); code != http.StatusOK {
		t.Fatal("upload")
	}
	rec := e.get(t, e.api.AppLatest299, "/api/v1/app/latest", "", hdr)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	sum := sha256.Sum256(b)
	if rec.Code != http.StatusOK || out["build"] != float64(166) || out["sha256"] != hex.EncodeToString(sum[:]) || out["path"] != "/download/LinkUp-3.0.95.apk" {
		t.Fatalf("latest: %d %v", rec.Code, out)
	}
	if !versionGateExempt("/api/v1/app/latest") {
		t.Fatal("an old phone must still reach /api/v1/app/latest")
	}
}

func TestAPK299MemoryCopySurvivesRestart(t *testing.T) {
	e := newAPKEnv299(t)
	if code, _ := e.upload(t, apk299Bytes(t, "com.sajjad.linkup", 166, "3.0.95", cert299(t, "k"))); code != http.StatusOK {
		t.Fatal("upload")
	}
	fresh := New(e.api.store)
	cur := fresh.currentAPK299()
	if cur == nil || cur.Build != 166 {
		t.Fatalf("after restart: %+v", cur)
	}
}

func TestAPK299TooBigAndAudit(t *testing.T) {
	e := newAPKEnv299(t)
	old := apkMaxBytes299
	apkMaxBytes299 = 512
	code, out := e.upload(t, apk299Bytes(t, "com.sajjad.linkup", 166, "3.0.95", cert299(t, "k")))
	apkMaxBytes299 = old
	if code != http.StatusRequestEntityTooLarge || out["error"] != "Too big (max 300 MB)." {
		t.Fatalf("too big: %d %v", code, out)
	}
	files, _ := filepath.Glob(filepath.Join(os.Getenv("LOCALLINK_APK_DIR"), "*"))
	if len(files) != 0 {
		t.Fatalf("nothing may stay: %v", files)
	}
	if code, _ := e.upload(t, apk299Bytes(t, "com.sajjad.linkup", 166, "3.0.95", cert299(t, "k"))); code != http.StatusOK {
		t.Fatal("upload")
	}
	var n int
	_ = e.api.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE action='apk_added' AND target_id='LinkUp-3.0.95.apk'`).Scan(&n)
	if n != 1 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestAPK299ShortNameOnlyWhenOn(t *testing.T) {
	defer SetShortNames299(ShortNames299{HTTPPort: 8080})
	e := newAPKEnv299(t)
	SetShortNames299(ShortNames299{HTTPPort: 8080})
	if body := e.get(t, e.api.LinkupPage299, "/download", "", nil).Body.String(); strings.Contains(body, "linkup.local") {
		t.Fatal("name off: page must not show linkup.local")
	}
	if c := shortNamesHealth299("x"); c.Status != "healthy" || !strings.Contains(c.Detail, "phones type :8080") {
		t.Fatalf("health off-by-config: %+v", c)
	}
	SetShortNames299(ShortNames299{HTTPPort: 8080, Port80: true, MDNS: true, MDNSName: "linkup.local"})
	if body := e.get(t, e.api.LinkupPage299, "/download", "", nil).Body.String(); !strings.Contains(body, "linkup.local/download") {
		t.Fatal("name on: page shows linkup.local/download")
	}
	if shortLink299() != "linkup.local/download" {
		t.Fatal(shortLink299())
	}
	SetShortNames299(ShortNames299{HTTPPort: 8080, Port80Err: "address in use"})
	if c := shortNamesHealth299("x"); c.Status != "warning" || !strings.Contains(c.Detail, "must type :8080") {
		t.Fatalf("health port busy: %+v", c)
	}
	rec := e.get(t, e.api.AdminAPK299, "/api/v1/admin/apk299", "", map[string]string{"Authorization": "Bearer " + e.token})
	if !strings.Contains(rec.Body.String(), "must type :8080") {
		t.Fatalf("the App download page tells the owner: %s", rec.Body.String())
	}
}
