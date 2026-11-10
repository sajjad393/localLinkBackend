package httpapi

import (
	"net/http"
	"strings"
)

// Bundle 259 (B1): "who owns this phone" for the app's chat list.
//
// A chat row is keyed by the friend's PHONE id. After a reinstall the app
// may have messages from a phone it has no card for (a friend's old phone,
// or someone who chatted but was never added). This route answers with the
// owner's directory card, the same card as /directory/user/{id}.
//
// Allowed only when the requester and the owner are contacts (either
// direction), or when a message exists between ANY phone of the requester
// and this phone id. Everyone else gets the SAME 404 as an unknown phone,
// so a stranger learns nothing. The number stays hidden unless public and
// directory sync off means 404 (the same rules as directoryProfile).
type deviceOwnerProfile259 struct {
	Profile
	AskedDeviceID string `json:"asked_device_id"`
}

func (a *API) DirectoryByDevice(w http.ResponseWriter, r *http.Request) {
	_, requesterID, _, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	deviceID := strings.TrimSpace(r.PathValue("id"))
	if deviceID == "" || len(deviceID) > 128 {
		writeError(w, http.StatusBadRequest, "invalid device id")
		return
	}
	notFound := func() { writeError(w, http.StatusNotFound, "user not found") }

	var owner string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(user_id,'') FROM devices WHERE id=?`, deviceID).Scan(&owner); err != nil || owner == "" {
		notFound()
		return
	}
	if owner != requesterID && !a.mayKnowDeviceOwner259(requesterID, owner, deviceID) {
		notFound()
		return
	}
	p, ok := a.directoryProfile(owner, requesterID)
	if !ok {
		notFound()
		return
	}
	writeJSON(w, http.StatusOK, deviceOwnerProfile259{Profile: p, AskedDeviceID: deviceID})
}

// mayKnowDeviceOwner259: contacts (either way), or a message between any
// phone of the requester and [deviceID].
func (a *API) mayKnowDeviceOwner259(requesterID, owner, deviceID string) bool {
	db := a.store.DB()
	var one int
	if err := db.QueryRow(`SELECT 1 FROM account_contacts WHERE (user_id=? AND contact_user_id=?) OR (user_id=? AND contact_user_id=?) LIMIT 1`,
		requesterID, owner, owner, requesterID).Scan(&one); err == nil {
		return true
	}
	// The requester's phones first (rows closed before the next query:
	// SetMaxOpenConns(1)).
	rows, err := db.Query(`SELECT id FROM devices WHERE user_id=? LIMIT 50`, requesterID)
	if err != nil {
		return false
	}
	phones := make([]string, 0, 4)
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			phones = append(phones, id)
		}
	}
	rows.Close()
	if len(phones) == 0 {
		return false
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(phones)), ",")
	args := make([]any, 0, 2*len(phones)+2)
	for _, p := range phones {
		args = append(args, p)
	}
	args = append(args, deviceID, deviceID)
	for _, p := range phones {
		args = append(args, p)
	}
	q := `SELECT 1 FROM messages WHERE (sender_id IN (` + marks + `) AND recipient_id=?) OR (sender_id=? AND recipient_id IN (` + marks + `)) LIMIT 1`
	return db.QueryRow(q, args...).Scan(&one) == nil
}
