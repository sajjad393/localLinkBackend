package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Bundle 204: friends see each other's profile picture.

// FriendAvatar: GET /api/v1/directory/user/{id}/avatar - the picture of a
// friend (account contact, either direction). Anyone else gets 404, so the
// endpoint never tells a stranger that a picture exists.
func (a *API) FriendAvatar(w http.ResponseWriter, r *http.Request) {
	_, requesterID, _, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" || len(userID) > 128 {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if userID != requesterID && !a.accountsAreContacts(requesterID, userID) {
		writeError(w, http.StatusNotFound, "profile image not found")
		return
	}
	a.serveAvatar(w, r, userID)
}

// accountsAreContacts: the two accounts are friends (either direction).
func (a *API) accountsAreContacts(userA, userB string) bool {
	if userA == "" || userB == "" {
		return false
	}
	var n int
	return a.store.DB().QueryRow(`SELECT 1 FROM account_contacts WHERE (user_id=? AND contact_user_id=?) OR (user_id=? AND contact_user_id=?) LIMIT 1`,
		userA, userB, userB, userA).Scan(&n) == nil
}

// DeleteProfileAvatar: DELETE /api/v1/profile/avatar - "Remove photo".
func (a *API) DeleteProfileAvatar(w http.ResponseWriter, r *http.Request) {
	_, userID, _, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var oldPath string
	_ = a.store.DB().QueryRow(`SELECT COALESCE(avatar_path,'') FROM profiles WHERE user_id=?`, userID).Scan(&oldPath)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := a.store.DB().Exec(`UPDATE profiles SET avatar_path='',avatar_content_type='',avatar_size=0,avatar_sha256='',avatar_updated_at=?,updated_at=? WHERE user_id=?`, now, now, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove profile image")
		return
	}
	if oldPath != "" {
		full := filepath.Join(a.fileRoot, filepath.FromSlash(oldPath))
		if ensureInside(a.fileRoot, full) == nil {
			_ = os.Remove(full)
		}
	}
	a.audit("profile_avatar_remove", userID, "")
	a.GetProfile(w, r)
}
