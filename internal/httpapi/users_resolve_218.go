package httpapi

import (
	"net/http"
	"regexp"
	"strings"
)

// Bundle 218: a phone-only account id is offline_<12 hex>. Anything else in
// offline_user_id is ignored (never an error), so old or odd apps still join.
var offlineIDPattern = regexp.MustCompile(`^offline_[0-9a-f]{12}$`)

// recoveryFormat218 is the code format the app and the server both use.
var recoveryFormat218 = regexp.MustCompile(`^[A-Z0-9]{5}(-[A-Z0-9]{5}){3}$`)

func offlineID218(value string) string {
	v := strings.TrimSpace(value)
	if !offlineIDPattern.MatchString(v) {
		return ""
	}
	return v
}

// writeDeviceTaken218: 409 DEVICE_TAKEN, plus "owner" (the other @name)
// only when the stored hardware id equals the one this phone sent.
func writeDeviceTaken218(w http.ResponseWriter, message, owner, storedFP, sentFP string) {
	out := map[string]any{"ok": false, "error": message, "error_code": "DEVICE_TAKEN"}
	if owner != "" && strings.TrimSpace(sentFP) != "" && strings.TrimSpace(storedFP) == strings.TrimSpace(sentFP) {
		out["owner"] = owner
	}
	writeJSON(w, http.StatusConflict, out)
}

// resolveOffline218 returns the real account behind a linked offline id ("" = none).
func (a *API) resolveOffline218(offlineID string) string {
	var userID string
	_ = a.store.DB().QueryRow(`SELECT o.user_id FROM offline_ids o JOIN users u ON u.id=o.user_id WHERE o.offline_id=? AND u.status IN ('active','suspended')`, offlineID).Scan(&userID)
	return userID
}

// last4 returns the last 4 digits of a phone number, or "".
func last4(phone string) string {
	digits := make([]byte, 0, len(phone))
	for i := 0; i < len(phone); i++ {
		if phone[i] >= '0' && phone[i] <= '9' {
			digits = append(digits, phone[i])
		}
	}
	if len(digits) < 4 {
		return ""
	}
	return string(digits[len(digits)-4:])
}

// ResolveUsers218: POST /api/v1/users/resolve {"ids":[...]} → which ids
// (offline ids or phone ids) belong to which account now. Unknown ids are
// left out.
func (a *API) ResolveUsers218(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	if !a.allowSecurityAction("resolve", deviceID, 30) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "resolve rate limit exceeded")
		return
	}
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(r, &in, 32<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(in.IDs) > 100 {
		writeError(w, http.StatusBadRequest, "at most 100 ids")
		return
	}
	type found struct {
		ID       string `json:"id"`
		UserID   string `json:"user_id"`
		Username string `json:"username"`
		DeviceID string `json:"device_id"`
	}
	out := []found{}
	seen := map[string]bool{}
	for _, raw := range in.IDs {
		id := strings.TrimSpace(raw)
		if id == "" || len(id) > 128 || hasControlChars(id) || seen[id] {
			continue
		}
		seen[id] = true
		var f found
		var err error
		if offlineIDPattern.MatchString(id) {
			err = a.store.DB().QueryRow(`SELECT o.user_id,u.username,o.device_id FROM offline_ids o JOIN users u ON u.id=o.user_id WHERE o.offline_id=? AND u.status IN ('active','suspended')`, id).Scan(&f.UserID, &f.Username, &f.DeviceID)
		} else {
			err = a.store.DB().QueryRow(`SELECT d.user_id,u.username,d.id FROM devices d JOIN users u ON u.id=d.user_id WHERE d.id=? AND COALESCE(d.status,'active')='active'`, id).Scan(&f.UserID, &f.Username, &f.DeviceID)
		}
		if err != nil || f.UserID == "" {
			continue
		}
		f.ID = id
		out = append(out, f)
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": out})
}
