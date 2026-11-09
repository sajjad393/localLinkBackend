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
	// IsPrivate is per-lister: it gates re-sharing (search results / gossip
	// sync rows) of this entry on the account that holds it, NOT the
	// reciprocal side. Omitted from JSON when false so older clients are
	// byte-compatible.
	IsPrivate bool `json:"is_private,omitempty"`
	// Bundle 237: the contact's other phones on this server (any status,
	// newest first, max 10, never the current one). The app joins chats
	// with those old phone ids into ONE chat with this person. Omitted
	// when empty so older clients see the same bytes.
	PastDeviceIDs []string `json:"past_device_ids,omitempty"`
}

// maxPastDevices237 caps the old phone ids sent per contact.
const maxPastDevices237 = 10

func (a *API) accountContactsForUser(userID string) ([]AccountContact, error) {
	// Contact relationship model is MUTUAL: when A adds B, B sees A too.
	// Rows written before the reciprocal-add change are one-way; backfill the
	// missing reverse row on read so every legacy pair converges (idempotent
	// INSERT OR IGNORE keyed by the (user_id, contact_user_id) primary key).
	if _, err := a.store.DB().Exec(`INSERT OR IGNORE INTO account_contacts(user_id,contact_user_id,added_at)
		SELECT contact_user_id,user_id,added_at FROM account_contacts outc
		WHERE outc.contact_user_id=? AND NOT EXISTS(
			SELECT 1 FROM account_contacts inn WHERE inn.user_id=outc.contact_user_id AND inn.contact_user_id=outc.user_id)`, userID); err != nil {
		return nil, err
	}
	rows, err := a.store.DB().Query(`
		SELECT c.contact_user_id,
			COALESCE((SELECT d.id FROM devices d WHERE d.user_id=c.contact_user_id AND COALESCE(d.status,'active')='active' ORDER BY d.last_seen_at DESC LIMIT 1),''),
			c.added_at,
			COALESCE(c.is_private,0)
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
		var isPrivate int
		if err := rows.Scan(&c.UserID, &c.DeviceID, &c.AddedAt, &isPrivate); err != nil {
			return nil, err
		}
		c.IsPrivate = isPrivate != 0
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Close before the next queries (the store runs on one connection).
	rows.Close()
	for i := range out {
		past, err := a.pastDevices237(out[i].UserID, out[i].DeviceID)
		if err != nil {
			return nil, err
		}
		out[i].PastDeviceIDs = past
	}
	return out, nil
}

// pastDevices237 lists the user's other phone ids (any status), newest
// first, at most maxPastDevices237. A phone given to a new owner (235)
// has the new owner's user_id now, so it is never listed for the old one.
func (a *API) pastDevices237(userID, current string) ([]string, error) {
	rows, err := a.store.DB().Query(`SELECT id FROM devices
		WHERE user_id=? AND id<>?
		ORDER BY COALESCE(last_seen_at,'') DESC, COALESCE(created_at,'') DESC
		LIMIT ?`, userID, current, maxPastDevices237)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != "" {
			out = append(out, id)
		}
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
		UserID    string `json:"user_id"`
		IsPrivate *bool  `json:"is_private"`
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
	// Bundle 218: a friend saved as a phone-only id follows her to the real account.
	if offlineIDPattern.MatchString(contactID) {
		if real := a.resolveOffline218(contactID); real != "" {
			contactID = real
		}
		if contactID == userID {
			writeError(w, http.StatusBadRequest, "invalid contact")
			return
		}
	}
	var exists string
	if err := a.store.DB().QueryRow(`SELECT id FROM users WHERE id=? AND status='active'`, contactID).Scan(&exists); err != nil {
		writeErrorCode(w, http.StatusNotFound, "NOT_ON_SERVER", "user not found")
		return
	}
	// Mutual contacts: record the relationship in BOTH directions so the
	// added user sees the adder in their People/contacts list and their
	// fresh-install restore returns the pair. INSERT OR IGNORE keeps repeated
	// adds idempotent (no duplicate rows, no 409 churn).
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// A's row carries A's privacy mark for B; the reciprocal row always
	// defaults public (0) — one side may never impose privacy on the other
	// side's entry. A second call with an explicit is_private acts as a TOGGLE
	// on the existing row (UPSERT on the pair primary key), which is how the
	// app updates the flag without removing/re-adding the contact.
	isPrivate := 0
	if in.IsPrivate != nil && *in.IsPrivate {
		isPrivate = 1
	}
	if in.IsPrivate != nil {
		if _, err := a.store.DB().Exec(`INSERT INTO account_contacts(user_id,contact_user_id,added_at,is_private) VALUES(?,?,?,?)
			ON CONFLICT(user_id,contact_user_id) DO UPDATE SET is_private=excluded.is_private`, userID, contactID, now, isPrivate); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add contact")
			return
		}
		if _, err := a.store.DB().Exec(`INSERT OR IGNORE INTO account_contacts(user_id,contact_user_id,added_at) VALUES(?,?,?)`, contactID, userID, now); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add contact")
			return
		}
	} else {
		if _, err := a.store.DB().Exec(`INSERT OR IGNORE INTO account_contacts(user_id,contact_user_id,added_at) VALUES(?,?,?)`, userID, contactID, now); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add contact")
			return
		}
		if _, err := a.store.DB().Exec(`INSERT OR IGNORE INTO account_contacts(user_id,contact_user_id,added_at) VALUES(?,?,?)`, contactID, userID, now); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add contact")
			return
		}
	}
	a.audit("account_contact_added", userID, "contact="+contactID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user_id": contactID})
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
	// Mutual removal mirrors mutual add: deleting a contact ends the
	// relationship for both sides, otherwise the removed user would keep
	// seeing (and could keep contacting) the deleter.
	if _, err := a.store.DB().Exec(`DELETE FROM account_contacts WHERE (user_id=? AND contact_user_id=?) OR (user_id=? AND contact_user_id=?)`, userID, contactID, contactID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove contact")
		return
	}
	a.audit("account_contact_removed", userID, "contact="+contactID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
