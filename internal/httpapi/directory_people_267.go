package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

// Bundle 267: the Add Contact screen shows, by default, everyone on this
// server the requester can add: same rules as DirectoryByName (active
// account, directory sync on, findable by name, phone hidden unless public)
// minus the requester and the requester's own contacts. Paged by 50, name
// order. Anyone could already list these people by searching one letter.
const directoryPeoplePage267 = 50

func (a *API) DirectoryPeople(w http.ResponseWriter, r *http.Request) {
	_, requesterID, _, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	limit := directoryPeoplePage267
	if v, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit"))); err == nil && v > 0 && v < limit {
		limit = v
	}
	offset := 0
	if v, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset"))); err == nil && v > 0 {
		offset = v
	}
	rows, err := a.store.DB().Query(`SELECT p.user_id,COALESCE((SELECT d.id FROM devices d WHERE d.user_id=p.user_id AND COALESCE(d.status,'active')='active' ORDER BY d.last_seen_at DESC LIMIT 1),''),u.username,p.display_name,COALESCE(p.phone_number,''),COALESCE(p.phone_visibility,'contacts'),COALESCE(p.discoverable_by_phone,1),COALESCE(p.discoverable_by_name,1),COALESCE(p.directory_sync_enabled,1),COALESCE(p.profile_version,0),p.updated_at FROM profiles p JOIN users u ON u.id=p.user_id WHERE u.status='active' AND p.directory_sync_enabled=1 AND p.discoverable_by_name=1 AND p.user_id<>? AND p.user_id NOT IN (SELECT c.contact_user_id FROM account_contacts c WHERE c.user_id=?) ORDER BY p.display_name COLLATE NOCASE, u.username COLLATE NOCASE LIMIT ? OFFSET ?`, requesterID, requesterID, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list directory")
		return
	}
	out := make([]Profile, 0, limit)
	more := false
	for rows.Next() {
		var p Profile
		var byPhone, byName, sync int64
		if err := rows.Scan(&p.UserID, &p.DeviceID, &p.Username, &p.DisplayName, &p.PhoneNumber, &p.PhoneVisibility, &byPhone, &byName, &sync, &p.ProfileVersion, &p.UpdatedAt); err != nil {
			continue
		}
		if len(out) == limit {
			more = true
			break
		}
		if p.PhoneVisibility != "public" {
			p.PhoneNumber = ""
		}
		p.DiscoverableByPhone = byPhone != 0
		p.DiscoverableByName = byName != 0
		p.DirectorySyncEnabled = sync != 0
		out = append(out, p)
	}
	rows.Close()
	writeJSON(w, http.StatusOK, map[string]any{"profiles": out, "more": more})
}
