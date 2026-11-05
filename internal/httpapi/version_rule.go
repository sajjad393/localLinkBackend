package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Bundle 213: admin stops old app versions.
//
// One signed "version rule" for the whole network: {v, kind:"version_rule",
// id, min_build, min_version, mode:"warn"|"stop", stop_at, very_old, seq,
// at, by, signer, cert}. Signed by the server key (web panel) or by an owner
// admin's phone key with a server-signed certificate (same trust as the
// 212 blocks). Phones carry it with no internet; the newest rule wins
// (seq, then time, then id). min_build 0 = no rule.

type versionRuleBody struct {
	V          int         `json:"v"`
	Kind       string      `json:"kind"`
	ID         string      `json:"id"`
	MinBuild   int64       `json:"min_build"`
	MinVersion string      `json:"min_version"`
	Mode       string      `json:"mode"`
	StopAt     string      `json:"stop_at"`
	VeryOld    bool        `json:"very_old"`
	Seq        int64       `json:"seq"`
	At         string      `json:"at"`
	By         string      `json:"by"`
	Signer     string      `json:"signer"`
	Cert       *signedBlob `json:"cert,omitempty"`
}

// stopped reports whether build is stopped by this rule at time now.
func (r versionRuleBody) stopped(build int64, now time.Time) bool {
	if r.MinBuild <= 0 || build >= r.MinBuild || r.Mode != "stop" {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, r.StopAt)
	if err != nil {
		return false
	}
	return !now.Before(at)
}

func (a *API) checkVersionRule(blob signedBlob) (versionRuleBody, string) {
	var rec versionRuleBody
	if len(blob.Body) > 16<<10 {
		return rec, "too_big"
	}
	if err := json.Unmarshal([]byte(blob.Body), &rec); err != nil {
		return rec, "bad_json"
	}
	if rec.V != 1 || rec.Kind != "version_rule" || strings.TrimSpace(rec.ID) == "" || rec.Seq < 1 || rec.MinBuild < 0 {
		return rec, "bad_fields"
	}
	if rec.Mode != "warn" && rec.Mode != "stop" {
		return rec, "bad_mode"
	}
	if _, err := time.Parse(time.RFC3339Nano, rec.At); err != nil {
		return rec, "bad_time"
	}
	if rec.Mode == "stop" && rec.MinBuild > 0 {
		if _, err := time.Parse(time.RFC3339Nano, rec.StopAt); err != nil {
			return rec, "bad_stop_at"
		}
	}
	switch rec.Signer {
	case "server":
		if !a.serverVerify(blob) {
			return rec, "bad_server_sig"
		}
		return rec, ""
	case "admin":
		if rec.Cert == nil || !a.serverVerify(*rec.Cert) {
			return rec, "bad_cert"
		}
		var cert adminCertBody
		if err := json.Unmarshal([]byte(rec.Cert.Body), &cert); err != nil || cert.Kind != "admin_cert" {
			return rec, "bad_cert"
		}
		var ended int
		_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_cert_ended WHERE cert_id=?`, cert.CertID).Scan(&ended)
		if ended > 0 {
			return rec, "cert_ended"
		}
		if cert.Role != "owner" {
			return rec, "not_owner"
		}
		if !verifyWithKey(cert.SigningKey, blob) {
			return rec, "bad_sig"
		}
		return rec, ""
	default:
		return rec, "bad_signer"
	}
}

// ---------------------------------------------------------------- cache

type versionRuleCache struct {
	mu       sync.Mutex
	loaded   time.Time
	rule     *versionRuleBody
	blob     *signedBlob
	builds   map[string]time.Time // device|build -> last write
	buildsMu sync.Mutex
}

var versionCaches sync.Map // *API -> *versionRuleCache

func (a *API) versionCache() *versionRuleCache {
	if c, ok := versionCaches.Load(a); ok {
		return c.(*versionRuleCache)
	}
	c, _ := versionCaches.LoadOrStore(a, &versionRuleCache{builds: map[string]time.Time{}})
	return c.(*versionRuleCache)
}

// currentVersionRule returns the saved rule (cached for 5 s).
func (a *API) currentVersionRule() (*versionRuleBody, *signedBlob) {
	c := a.versionCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.loaded) < 5*time.Second {
		return c.rule, c.blob
	}
	c.loaded = time.Now()
	c.rule, c.blob = nil, nil
	var b signedBlob
	if err := a.store.DB().QueryRow(`SELECT body,sig FROM version_rules WHERE id=1`).Scan(&b.Body, &b.Sig); err != nil {
		return nil, nil
	}
	var rec versionRuleBody
	if json.Unmarshal([]byte(b.Body), &rec) != nil {
		return nil, nil
	}
	c.rule, c.blob = &rec, &b
	return c.rule, c.blob
}

func (a *API) forgetVersionRule() {
	c := a.versionCache()
	c.mu.Lock()
	c.loaded = time.Time{}
	c.mu.Unlock()
}

// applyVersionRule saves the rule when it is newer than the saved one.
func (a *API) applyVersionRule(rec versionRuleBody, blob signedBlob) (bool, error) {
	tx, err := a.store.DB().Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var curSeq int64
	var curAt, curID string
	err = tx.QueryRow(`SELECT seq,at,record_id FROM version_rules WHERE id=1`).Scan(&curSeq, &curAt, &curID)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil && !adminBlockNewer(rec.Seq, rec.At, rec.ID, curSeq, curAt, curID) {
		return false, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`INSERT INTO version_rules(id,record_id,seq,at,body,sig,updated_at) VALUES(1,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET record_id=excluded.record_id,seq=excluded.seq,at=excluded.at,body=excluded.body,sig=excluded.sig,updated_at=excluded.updated_at`,
		rec.ID, rec.Seq, rec.At, blob.Body, blob.Sig, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	a.forgetVersionRule()
	a.adminAudit(rec.By, "version_rule", strconv.FormatInt(rec.MinBuild, 10),
		"mode="+rec.Mode+" stop_at="+rec.StopAt+" very_old="+strconv.FormatBool(rec.VeryOld)+" signer="+rec.Signer+" record="+rec.ID)
	return true, nil
}

// noteDeviceBuild remembers the build a phone sent (at most every 10 min).
func (a *API) noteDeviceBuild(deviceID string, build int64) {
	if deviceID == "" || len(deviceID) > 200 || build <= 0 {
		return
	}
	c := a.versionCache()
	key := deviceID + "|" + strconv.FormatInt(build, 10)
	c.buildsMu.Lock()
	last, ok := c.builds[key]
	if ok && time.Since(last) < 10*time.Minute {
		c.buildsMu.Unlock()
		return
	}
	c.builds[key] = time.Now()
	if len(c.builds) > 20000 {
		c.builds = map[string]time.Time{}
	}
	c.buildsMu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = a.store.DB().Exec(`INSERT INTO device_builds(device_id,build,seen_at) VALUES(?,?,?)
		ON CONFLICT(device_id) DO UPDATE SET build=excluded.build,seen_at=excluded.seen_at`, deviceID, build, now)
}

// ---------------------------------------------------------------- handlers

func versionRuleOut(blob *signedBlob) map[string]any {
	if blob == nil {
		return map[string]any{"rule": nil}
	}
	return map[string]any{"rule": *blob}
}

// VersionRule returns the current signed rule. No auth: phones that are
// stopped or very old must still be able to read it.
func (a *API) VersionRule(w http.ResponseWriter, r *http.Request) {
	_, blob := a.currentVersionRule()
	writeJSON(w, http.StatusOK, versionRuleOut(blob))
}

// VersionRuleSync: a phone sends the rule it carries (made offline by an
// admin phone) and gets the current one back.
func (a *API) VersionRuleSync(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authorizeDevice(r); !ok {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	var in struct {
		Rule *signedBlob `json:"rule"`
	}
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	applied := false
	reason := ""
	if in.Rule != nil {
		rec, why := a.checkVersionRule(*in.Rule)
		reason = why
		if why == "" {
			if ok, err := a.applyVersionRule(rec, *in.Rule); err == nil && ok {
				applied = true
			}
		}
	}
	out := versionRuleOut(func() *signedBlob { _, b := a.currentVersionRule(); return b }())
	out["applied"] = applied
	if reason != "" {
		out["rejected"] = reason
	}
	writeJSON(w, http.StatusOK, out)
}

type versionBuildCount struct {
	Build    int64  `json:"build"`
	Devices  int    `json:"devices"`
	LastSeen string `json:"last_seen"`
}

// AdminGetVersionRule: the web page (rule fields + phones by build, 30 days).
func (a *API) AdminGetVersionRule(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	rule, _ := a.currentVersionRule()
	builds := []versionBuildCount{}
	since := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	if rows, err := a.store.DB().Query(`SELECT build,COUNT(*),MAX(seen_at) FROM device_builds WHERE seen_at>=? GROUP BY build ORDER BY build DESC`, since); err == nil {
		for rows.Next() {
			var b versionBuildCount
			if rows.Scan(&b.Build, &b.Devices, &b.LastSeen) == nil {
				builds = append(builds, b)
			}
		}
		rows.Close()
	}
	var ruleOut any
	if rule != nil && rule.MinBuild > 0 {
		ruleOut = map[string]any{
			"min_build": rule.MinBuild, "min_version": rule.MinVersion, "mode": rule.Mode,
			"stop_at": rule.StopAt, "very_old": rule.VeryOld, "seq": rule.Seq, "at": rule.At,
			"by": rule.By, "signer": rule.Signer,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": ruleOut, "builds": builds})
}

// AdminSetVersionRule: owner only. A server-signed rule with seq = current + 1.
func (a *API) AdminSetVersionRule(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_settings")
	if !ok {
		return
	}
	if admin.Role != "owner" {
		writeError(w, http.StatusForbidden, "only the owner can set the minimum version")
		return
	}
	var in struct {
		MinBuild   int64  `json:"min_build"`
		MinVersion string `json:"min_version"`
		Mode       string `json:"mode"`
		StopDays   int    `json:"stop_days"`
		VeryOld    bool   `json:"very_old"`
		Remove     bool   `json:"remove"`
	}
	if err := decodeJSON(r, &in, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	now := time.Now().UTC()
	rec := versionRuleBody{V: 1, Kind: "version_rule", ID: uuid.NewString(), Seq: 1,
		At: now.Format(time.RFC3339Nano), By: admin.Username, Signer: "server", Mode: "warn"}
	if cur, _ := a.currentVersionRule(); cur != nil {
		rec.Seq = cur.Seq + 1
	}
	if !in.Remove {
		in.MinVersion = strings.TrimSpace(in.MinVersion)
		if in.MinBuild < 1 || in.MinBuild > 1_000_000 || in.MinVersion == "" || len(in.MinVersion) > 32 {
			writeError(w, http.StatusBadRequest, "invalid version")
			return
		}
		if in.Mode != "warn" && in.Mode != "stop" {
			writeError(w, http.StatusBadRequest, "mode must be warn or stop")
			return
		}
		if in.StopDays < 0 || in.StopDays > 30 {
			writeError(w, http.StatusBadRequest, "stop_days must be 0-30")
			return
		}
		rec.MinBuild, rec.MinVersion, rec.Mode, rec.VeryOld = in.MinBuild, in.MinVersion, in.Mode, in.VeryOld
		if in.Mode == "stop" {
			rec.StopAt = now.Add(time.Duration(in.StopDays) * 24 * time.Hour).Format(time.RFC3339Nano)
		}
	}
	raw, _ := json.Marshal(rec)
	blob, err := a.serverSign(string(raw))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign rule")
		return
	}
	if _, err := a.applyVersionRule(rec, blob); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save rule")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": blob, "seq": rec.Seq})
}
