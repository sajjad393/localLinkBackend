package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Bundle 299: LinkUp from the server.
//   A  owner-only web page "App download": GET/POST/DELETE /api/v1/admin/apk299
//   B  public page /download (+ /Download, /linkup, /LinkUp) and the file
//      /download/<file> (resume works), no sign-in
//   C  GET /api/v1/app/latest for phones (version gate exempt)
//   N  "/" opens the admin panel; short names (linkup.local, port 80) are
//      reported here by cmd/server.
// Only the current APK is kept on disk (data/apk); older ones stay as
// history rows.

const apkDownloadEvery = 10 * time.Minute

// apkMaxBytes299 is a var only so a test can lower it.
var apkMaxBytes299 int64 = 300 << 20

type apkRow299 struct {
	ID           int64  `json:"id"`
	Version      string `json:"version"`
	Build        int64  `json:"build"`
	FileName     string `json:"file_name"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	CertSHA256   string `json:"cert_sha256"`
	AddedAt      string `json:"added_at"`
	AddedBy      string `json:"added_by"`
	Downloads    int64  `json:"downloads"`
	RemovedAt    string `json:"removed_at,omitempty"`
	RemovedBy    string `json:"removed_by,omitempty"`
	Why          string `json:"why,omitempty"`
	Current      bool   `json:"current"`
	PhonesBehind int    `json:"phones_behind,omitempty"`
}

type apkState299 struct {
	mu        sync.Mutex
	loaded    bool
	cur       *apkRow299
	uploading bool
	counted   map[string]time.Time // ip|file -> last counted download
}

// ShortNames299 is what cmd/server managed to start (shown on the web).
type ShortNames299 struct {
	HTTPPort  int    `json:"http_port"`
	Port80    bool   `json:"port80"`
	Port80Err string `json:"port80_error,omitempty"`
	MDNS      bool   `json:"mdns"`
	MDNSName  string `json:"mdns_name,omitempty"`
	MDNSErr   string `json:"mdns_error,omitempty"`
}

var (
	shortMu299 sync.RWMutex
	short299   = ShortNames299{HTTPPort: 8080}
)

// SetShortNames299 is called by cmd/server after it tried port 80 and the name.
func SetShortNames299(s ShortNames299) {
	shortMu299.Lock()
	short299 = s
	shortMu299.Unlock()
}

func shortNames299() ShortNames299 {
	shortMu299.RLock()
	defer shortMu299.RUnlock()
	return short299
}

func (a *API) apkDir299() string {
	if d := strings.TrimSpace(os.Getenv("LOCALLINK_APK_DIR")); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(filepath.Clean(a.fileRoot)), "apk")
}

const apkCols299 = `id,version,build,file_name,size,sha256,cert_sha256,added_at,added_by,downloads,removed_at,removed_by,why,current`

func scanAPK299(sc interface{ Scan(...any) error }) (apkRow299, error) {
	var r apkRow299
	var cur int
	err := sc.Scan(&r.ID, &r.Version, &r.Build, &r.FileName, &r.Size, &r.SHA256, &r.CertSHA256, &r.AddedAt, &r.AddedBy,
		&r.Downloads, &r.RemovedAt, &r.RemovedBy, &r.Why, &cur)
	r.Current = cur == 1
	return r, err
}

// currentAPK299 returns the memory copy (loaded once from the DB).
func (a *API) currentAPK299() *apkRow299 {
	a.apk299.mu.Lock()
	defer a.apk299.mu.Unlock()
	if !a.apk299.loaded {
		row, err := scanAPK299(a.store.DB().QueryRow(`SELECT ` + apkCols299 + ` FROM app_apk_299 WHERE current=1 ORDER BY id DESC LIMIT 1`))
		if err == nil {
			if _, statErr := os.Stat(filepath.Join(a.apkDir299(), row.FileName)); statErr == nil {
				a.apk299.cur = &row
			} else {
				log.Printf("APK_299 current file missing: %s", row.FileName)
			}
		}
		a.apk299.loaded = true
	}
	if a.apk299.cur == nil {
		return nil
	}
	c := *a.apk299.cur
	return &c
}

func (a *API) setCurrentAPK299(r *apkRow299) {
	a.apk299.mu.Lock()
	a.apk299.cur = r
	a.apk299.loaded = true
	a.apk299.mu.Unlock()
}

// CleanAPKTemp299 removes half-done uploads (server start).
func (a *API) CleanAPKTemp299() {
	matches, _ := filepath.Glob(filepath.Join(a.apkDir299(), "tmp-*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

func (a *API) requireOwner299(w http.ResponseWriter, r *http.Request) (AdminAccount, bool) {
	admin, ok := a.requireAdminPermission(w, r, "manage_settings")
	if !ok {
		return admin, false
	}
	if admin.Role != "owner" {
		writeError(w, http.StatusForbidden, "only the Owner can do this")
		return admin, false
	}
	return admin, true
}

// shortNamesHealth299: the port 80 + linkup.local line on the App download page.
// Warning only when one of them was tried and failed.
func shortNamesHealth299(stamp string) healthComponent {
	s := shortNames299()
	c := healthComponent{Key: "short_names_299", Label: "Short Address", Status: "healthy", UpdatedAt: stamp}
	var parts []string
	switch {
	case s.Port80:
		parts = append(parts, "port 80 on (no :8080 needed)")
	case s.Port80Err != "":
		c.Status = "warning"
		parts = append(parts, "port 80 off ("+s.Port80Err+"): phones must type :"+strconv.Itoa(s.HTTPPort))
	default:
		parts = append(parts, "port 80 not used: phones type :"+strconv.Itoa(s.HTTPPort))
	}
	switch {
	case s.MDNS:
		parts = append(parts, s.MDNSName+" answering")
	case s.MDNSErr != "":
		c.Status = "warning"
		parts = append(parts, "linkup.local off ("+s.MDNSErr+")")
	default:
		parts = append(parts, "linkup.local off")
	}
	c.Detail = strings.Join(parts, "; ")
	return c
}

// ---------------------------------------------------------------- links

func lanIPv4s299() []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if ip := n.IP.To4(); ip != nil && ip.IsPrivate() {
					out = append(out, ip.String())
				}
			}
		}
	}
	return out
}

// downloadLink299: the IP link (works on every phone). The host the admin
// used when it is a LAN IP, else the first LAN IP of this PC.
func downloadLink299(r *http.Request) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.To4() == nil {
		if ips := lanIPv4s299(); len(ips) > 0 {
			host = ips[0]
		} else if ip == nil {
			host = "127.0.0.1"
		}
	}
	return "http://" + host + portPart299() + "/download"
}

func portPart299() string {
	s := shortNames299()
	if s.Port80 {
		return ""
	}
	if s.HTTPPort == 0 || s.HTTPPort == 80 {
		return ""
	}
	return ":" + strconv.Itoa(s.HTTPPort)
}

// shortLink299: "linkup.local/download" while the name answers.
func shortLink299() string {
	s := shortNames299()
	if !s.MDNS || s.MDNSName == "" {
		return ""
	}
	return s.MDNSName + portPart299() + "/download"
}

// ---------------------------------------------------------------- A: admin

func (a *API) apkPayload299(r *http.Request) map[string]any {
	cur := a.currentAPK299()
	var curOut any
	if cur != nil {
		since := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
		_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM device_builds WHERE seen_at>=? AND build<?`, since, cur.Build).Scan(&cur.PhonesBehind)
		curOut = cur
	}
	history := []apkRow299{}
	if rows, err := a.store.DB().Query(`SELECT ` + apkCols299 + ` FROM app_apk_299 ORDER BY id DESC LIMIT 50`); err == nil {
		for rows.Next() {
			if h, err := scanAPK299(rows); err == nil {
				history = append(history, h)
			}
		}
		rows.Close()
	}
	link := downloadLink299(r)
	return map[string]any{
		"current": curOut, "link": link, "short": shortLink299(), "qr_svg": qrSVG299(link),
		"history": history, "names": shortNames299(),
		"address": shortNamesHealth299(time.Now().UTC().Format(time.RFC3339)),
	}
}

// AdminAPK299: GET /api/v1/admin/apk299 (owner).
func (a *API) AdminAPK299(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOwner299(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, a.apkPayload299(r))
}

func apkRefuse299(w http.ResponseWriter, status int, msg, hint string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg, "hint": hint})
}

var apkNameSafe299 = regexp.MustCompile(`[^0-9A-Za-z.+_-]`)

func apkFileName299(version string, build int64) string {
	v := apkNameSafe299.ReplaceAllString(version, "")
	v = strings.Trim(v, ".")
	if v == "" {
		v = strconv.FormatInt(build, 10)
	}
	return "LinkUp-" + v + ".apk"
}

// AdminAPK299Upload: POST /api/v1/admin/apk299 (owner), multipart field "apk".
func (a *API) AdminAPK299Upload(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireOwner299(w, r)
	if !ok {
		return
	}
	a.apk299.mu.Lock()
	if a.apk299.uploading {
		a.apk299.mu.Unlock()
		apkRefuse299(w, http.StatusConflict, "Another upload is running.", "Wait until it finishes, then try again.")
		return
	}
	a.apk299.uploading = true
	a.apk299.mu.Unlock()
	defer func() {
		a.apk299.mu.Lock()
		a.apk299.uploading = false
		a.apk299.mu.Unlock()
	}()
	// The server read limit is 5 min; a 300 MB upload on Wi-Fi may need more.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, apkMaxBytes299+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		apkRefuse299(w, http.StatusBadRequest, "The file is not an APK.", "Pick the .apk file from the GitHub build.")
		return
	}
	dir := a.apkDir299()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		apkRefuse299(w, http.StatusInternalServerError, "The server could not save the file.", err.Error())
		return
	}
	var tmp string
	var size int64
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			apkRefuse299(w, http.StatusBadRequest, "The upload stopped.", "Check the Wi-Fi and try again.")
			return
		}
		if part.FormName() != "apk" {
			part.Close()
			continue
		}
		f, err := os.CreateTemp(dir, "tmp-*.apk")
		if err != nil {
			apkRefuse299(w, http.StatusInternalServerError, "The server could not save the file.", "")
			return
		}
		tmp = f.Name()
		size, err = io.Copy(f, io.LimitReader(part, apkMaxBytes299+1))
		cerr := f.Close()
		part.Close()
		if err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(tmp)
			var maxErr *http.MaxBytesError
			switch {
			case errors.As(err, &maxErr):
				apkRefuse299(w, http.StatusRequestEntityTooLarge, "Too big (max 300 MB).", "")
			case errors.Is(err, syscall.ENOSPC):
				apkRefuse299(w, http.StatusInsufficientStorage, "The server disk is full.", "Free some space on the server PC, then try again.")
			default:
				apkRefuse299(w, http.StatusBadRequest, "The upload stopped.", "Check the Wi-Fi and try again.")
			}
			return
		}
		break
	}
	if tmp == "" {
		apkRefuse299(w, http.StatusBadRequest, "No file came.", "Choose the LinkUp APK.")
		return
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmp)
		}
	}()
	if size > apkMaxBytes299 {
		apkRefuse299(w, http.StatusRequestEntityTooLarge, "Too big (max 300 MB).", "")
		return
	}
	info, err := readAPK299(tmp)
	var prob apkProblem299
	if err != nil {
		if errors.As(err, &prob) {
			apkRefuse299(w, http.StatusUnprocessableEntity, prob.Error(), "The APK on the server stays as it is.")
		} else {
			apkRefuse299(w, http.StatusInternalServerError, "The server could not read the file.", "")
		}
		return
	}
	cur := a.currentAPK299()
	var curBuild int64
	curVersion := ""
	if cur != nil {
		curBuild, curVersion = cur.Build, cur.Version
	}
	var firstCert string
	_ = a.store.DB().QueryRow(`SELECT cert_sha256 FROM app_apk_299 ORDER BY id ASC LIMIT 1`).Scan(&firstCert)
	if err := checkAPK299(info, curBuild, curVersion, firstCert); err != nil {
		apkRefuse299(w, http.StatusUnprocessableEntity, err.Error(), "The APK on the server stays as it is.")
		return
	}
	sum, err := fileSHA256(tmp)
	if err != nil {
		apkRefuse299(w, http.StatusInternalServerError, "The server could not read the file.", "")
		return
	}
	name := apkFileName299(info.VersionName, info.VersionCode)
	final := filepath.Join(dir, name)
	if err := os.Rename(tmp, final); err != nil {
		apkRefuse299(w, http.StatusInternalServerError, "The server could not save the file.", "")
		return
	}
	keep = true
	now := time.Now().UTC().Format(time.RFC3339Nano)
	row := apkRow299{Version: info.VersionName, Build: info.VersionCode, FileName: name, Size: size, SHA256: sum,
		CertSHA256: info.CertSHA256, AddedAt: now, AddedBy: admin.Username, Current: true}
	err = a.withTx299(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE app_apk_299 SET current=0, why='replaced', removed_at=?, removed_by=? WHERE current=1`, now, admin.Username); err != nil {
			return err
		}
		res, err := tx.Exec(`INSERT INTO app_apk_299(version,build,file_name,size,sha256,cert_sha256,added_at,added_by,current) VALUES(?,?,?,?,?,?,?,?,1)`,
			row.Version, row.Build, row.FileName, row.Size, row.SHA256, row.CertSHA256, row.AddedAt, row.AddedBy)
		if err != nil {
			return err
		}
		row.ID, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		_ = os.Remove(final)
		apkRefuse299(w, http.StatusInternalServerError, "The server could not save the file.", "")
		return
	}
	if cur != nil && cur.FileName != name {
		_ = os.Remove(filepath.Join(dir, cur.FileName))
	}
	a.setCurrentAPK299(&row)
	a.adminAudit(admin.ID, "apk_added", name, fmt.Sprintf("v=%s b=%d size=%d", row.Version, row.Build, row.Size))
	log.Printf("APK_299 added %s build=%d by=%s", name, row.Build, admin.Username)
	out := a.apkPayload299(r)
	out["ok"] = true
	out["version"] = row.Version
	writeJSON(w, http.StatusOK, out)
}

func (a *API) withTx299(fn func(*sql.Tx) error) error {
	tx, err := a.store.DB().Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// AdminAPK299Delete: DELETE /api/v1/admin/apk299 (owner).
func (a *API) AdminAPK299Delete(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireOwner299(w, r)
	if !ok {
		return
	}
	cur := a.currentAPK299()
	if cur == nil {
		writeError(w, http.StatusNotFound, "There is no app on the server.")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := a.store.DB().Exec(`UPDATE app_apk_299 SET current=0, why='removed', removed_at=?, removed_by=? WHERE current=1`, now, admin.Username); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not remove it.")
		return
	}
	_ = os.Remove(filepath.Join(a.apkDir299(), cur.FileName))
	a.setCurrentAPK299(nil)
	a.adminAudit(admin.ID, "apk_removed", cur.FileName, fmt.Sprintf("v=%s b=%d", cur.Version, cur.Build))
	log.Printf("APK_299 removed %s by=%s", cur.FileName, admin.Username)
	out := a.apkPayload299(r)
	out["ok"] = true
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------- B: public

func publicHeaders299(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
}

// RootRedirect299: "/" (also linkup.local) opens the admin panel.
func (a *API) RootRedirect299(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/", http.StatusFound)
}

// LinkupPage299: GET /download, /Download, /linkup, /LinkUp (+ trailing /).
func (a *API) LinkupPage299(w http.ResponseWriter, r *http.Request) {
	if !a.allowSecurityAction("apk-page-299", registrationKey(r), 60) {
		writeError(w, http.StatusTooManyRequests, "Too many requests. Try again in a minute.")
		return
	}
	link := downloadLink299(r)
	p := linkupPageData299{Link: link, QRSVG: qrSVG299(link), Short: shortLink299()}
	if cur := a.currentAPK299(); cur != nil {
		p.Has = true
		p.Version = cur.Version
		p.Build = cur.Build
		p.FileName = cur.FileName
		p.Size = cur.Size
		if t, err := time.Parse(time.RFC3339Nano, cur.AddedAt); err == nil {
			p.AddedAt = t.Local().Format("2 Jan 2006")
		}
	}
	publicHeaders299(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, linkupPage299(p))
	}
}

// LinkupFile299: GET /download/{file} (+ /linkup/{file}): the current APK.
func (a *API) LinkupFile299(w http.ResponseWriter, r *http.Request) {
	ip := registrationKey(r)
	if !a.allowSecurityAction("apk-file-299", ip, 30) {
		writeError(w, http.StatusTooManyRequests, "Too many requests. Try again in a minute.")
		return
	}
	cur := a.currentAPK299()
	name := r.PathValue("file")
	if cur == nil || name != cur.FileName {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(a.apkDir299(), cur.FileName))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// A big APK on weak Wi-Fi can take longer than the 5 min write limit.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(60 * time.Minute))
	if r.Method == http.MethodGet && r.Header.Get("Range") == "" {
		a.countDownload299(ip, cur)
	}
	publicHeaders299(w)
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="`+cur.FileName+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", `"`+cur.SHA256+`"`)
	http.ServeContent(w, r, cur.FileName, st.ModTime(), f)
}

// countDownload299: +1 once per phone (IP) per 10 min.
func (a *API) countDownload299(ip string, cur *apkRow299) {
	key := ip + "|" + cur.FileName
	now := time.Now()
	a.apk299.mu.Lock()
	if a.apk299.counted == nil {
		a.apk299.counted = map[string]time.Time{}
	}
	if t, ok := a.apk299.counted[key]; ok && now.Sub(t) < apkDownloadEvery {
		a.apk299.mu.Unlock()
		return
	}
	a.apk299.counted[key] = now
	if len(a.apk299.counted) > 5000 {
		for k, t := range a.apk299.counted {
			if now.Sub(t) >= apkDownloadEvery {
				delete(a.apk299.counted, k)
			}
		}
	}
	if a.apk299.cur != nil && a.apk299.cur.ID == cur.ID {
		a.apk299.cur.Downloads++
	}
	a.apk299.mu.Unlock()
	_, _ = a.store.DB().Exec(`UPDATE app_apk_299 SET downloads=downloads+1 WHERE id=?`, cur.ID)
}

// ---------------------------------------------------------------- C: phones

// AppLatest299: GET /api/v1/app/latest (device sign-in). 204 = no APK.
func (a *API) AppLatest299(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authorizeDevice(r); !ok {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	cur := a.currentAPK299()
	if cur == nil {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": cur.Version, "build": cur.Build, "size": cur.Size, "sha256": cur.SHA256,
		"file": cur.FileName, "path": "/download/" + cur.FileName,
	})
}
