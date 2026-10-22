package httpapi

import (
	"net/http"
	"strings"
	"time"
)

// AccountContact is one entry of a user's server-backed contact list. The
// device id is resolved live so restores always point at the peer's most
// recently active device, not the device that was current when the contact
// was added.
type AccountContact struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
	AddedAt  string `json:"added_at"`
}

func (a *API) accountContactsForUser(userID string) ([]AccountContact, error) {
	rows, err := a.store.DB().Query(`
		SELECT c.contact_user_id,
			COALESCE((SELECT d.id FROM devices d WHERE d.user_id=c.contact_user_id AND COALESCE(d.status,'active')='active' ORDER BY d.last_seen_at DESC LIMIT 1),''),
			c.added_at
		FROM account_contacts c
		JOIN users u ON u.id=c.contact_user_id AND u.status='active'
		WHERE c.user_id=?
		ORDER BY c.added_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AccountContact, 0)
	for rows.Next() {
		var c AccountContact
		if err := rows.Scan(&c.UserID, &c.DeviceID, &c.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (a *API) ListAccountContacts(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := a.accountUserForRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	contacts, err := a.accountContactsForUser(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list contacts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": contacts})
}

func (a *API) AddAccountContact(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := a.accountUserForRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var in struct {
		UserID string `json:"user_id"`
	}
	if err := decodeJSON(r, &in, 4<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	contactID := strings.TrimSpace(in.UserID)
	if contactID == "" || contactID == userID || len(contactID) > 128 {
		writeError(w, http.StatusBadRequest, "invalid contact")
		return
	}
	var exists string
	if err := a.store.DB().QueryRow(`SELECT id FROM users WHERE id=? AND status='active'`, contactID).Scan(&exists); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if _, err := a.store.DB().Exec(`INSERT OR IGNORE INTO account_contacts(user_id,contact_user_id,added_at) VALUES(?,?,?)`, userID, contactID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add contact")
		return
	}
	a.audit("account_contact_added", userID, "contact="+contactID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) RemoveAccountContact(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := a.accountUserForRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	contactID := strings.TrimSpace(r.PathValue("user_id"))
	if contactID == "" || len(contactID) > 128 {
		writeError(w, http.StatusBadRequest, "invalid contact")
		return
	}
	if _, err := a.store.DB().Exec(`DELETE FROM account_contacts WHERE user_id=? AND contact_user_id=?`, userID, contactID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove contact")
		return
	}
	a.audit("account_contact_removed", userID, "contact="+contactID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
