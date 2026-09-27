package httpapi

import (
	"database/sql"
	"net/http"
	"strings"
)

func (a *API) DirectoryByPhone(w http.ResponseWriter, r *http.Request) {
	_, requesterID, _, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	phone := normalizePhoneNumber(r.URL.Query().Get("phone"))
	if phone == "" {
		writeError(w, http.StatusBadRequest, "invalid phone number")
		return
	}
	var p Profile
	var dphone, dname, sync int64
	var deviceID string
	err := a.store.DB().QueryRow(`SELECT p.user_id,COALESCE((SELECT d.id FROM devices d WHERE d.user_id=p.user_id AND COALESCE(d.status,'active')='active' ORDER BY d.last_seen_at DESC LIMIT 1),''),p.display_name,u.username,COALESCE(p.phone_number,''),COALESCE(p.phone_visibility,'contacts'),COALESCE(p.discoverable_by_phone,1),COALESCE(p.discoverable_by_name,1),COALESCE(p.directory_sync_enabled,1),COALESCE(p.profile_version,0),p.updated_at FROM profiles p JOIN users u ON u.id=p.user_id WHERE p.phone_number=? AND u.status='active'`, phone).Scan(&p.UserID, &deviceID, &p.DisplayName, &p.Username, &p.PhoneNumber, &p.PhoneVisibility, &dphone, &dname, &sync, &p.ProfileVersion, &p.UpdatedAt)
	if err == sql.ErrNoRows || dphone == 0 || sync == 0 {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to search directory")
		return
	}
	p.DeviceID = deviceID
	if p.PhoneVisibility != "public" && p.UserID != requesterID {
		p.PhoneNumber = ""
	}
	p.DiscoverableByPhone = dphone != 0
	p.DiscoverableByName = dname != 0
	p.DirectorySyncEnabled = sync != 0
	writeJSON(w, http.StatusOK, p)
}

func (a *API) DirectoryByUserID(w http.ResponseWriter, r *http.Request) {
	_, requesterID, _, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	p, ok := a.directoryProfile(userID, requesterID)
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) DirectoryByName(w http.ResponseWriter, r *http.Request) {
	_, requesterID, _, ok := a.currentAccountForDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 80 {
		writeError(w, http.StatusBadRequest, "invalid search query")
		return
	}
	rows, err := a.store.DB().Query(`SELECT p.user_id,COALESCE((SELECT d.id FROM devices d WHERE d.user_id=p.user_id AND COALESCE(d.status,'active')='active' ORDER BY d.last_seen_at DESC LIMIT 1),''),u.username,p.display_name,COALESCE(p.phone_number,''),COALESCE(p.phone_visibility,'contacts'),COALESCE(p.discoverable_by_phone,1),COALESCE(p.discoverable_by_name,1),COALESCE(p.directory_sync_enabled,1),COALESCE(p.profile_version,0),p.updated_at FROM profiles p JOIN users u ON u.id=p.user_id WHERE u.status='active' AND p.directory_sync_enabled=1 AND p.discoverable_by_name=1 AND (p.display_name LIKE ? COLLATE NOCASE OR u.username LIKE ? COLLATE NOCASE) ORDER BY p.display_name COLLATE NOCASE LIMIT 50`, "%"+q+"%", "%"+q+"%")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to search directory")
		return
	}
	defer rows.Close()
	out := make([]Profile, 0, 50)
	for rows.Next() {
		var p Profile
		var byPhone, byName, sync int64
		if err := rows.Scan(&p.UserID, &p.DeviceID, &p.Username, &p.DisplayName, &p.PhoneNumber, &p.PhoneVisibility, &byPhone, &byName, &sync, &p.ProfileVersion, &p.UpdatedAt); err != nil {
			continue
		}
		if p.UserID != requesterID && p.PhoneVisibility != "public" {
			p.PhoneNumber = ""
		}
		p.DiscoverableByPhone = byPhone != 0
		p.DiscoverableByName = byName != 0
		p.DirectorySyncEnabled = sync != 0
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": out})
}

func (a *API) directoryProfile(userID, requesterID string) (Profile, bool) {
	var p Profile
	var byPhone, byName, sync int64
	err := a.store.DB().QueryRow(`SELECT p.user_id,COALESCE((SELECT d.id FROM devices d WHERE d.user_id=p.user_id AND COALESCE(d.status,'active')='active' ORDER BY d.last_seen_at DESC LIMIT 1),''),u.username,p.display_name,COALESCE(p.phone_number,''),COALESCE(p.phone_visibility,'contacts'),COALESCE(p.discoverable_by_phone,1),COALESCE(p.discoverable_by_name,1),COALESCE(p.directory_sync_enabled,1),COALESCE(p.profile_version,0),p.updated_at FROM profiles p JOIN users u ON u.id=p.user_id WHERE p.user_id=? AND u.status='active'`, userID).Scan(&p.UserID, &p.DeviceID, &p.Username, &p.DisplayName, &p.PhoneNumber, &p.PhoneVisibility, &byPhone, &byName, &sync, &p.ProfileVersion, &p.UpdatedAt)
	if err != nil {
		return Profile{}, false
	}
	if requesterID != userID && sync == 0 {
		return Profile{}, false
	}
	if requesterID != userID && p.PhoneVisibility != "public" {
		p.PhoneNumber = ""
	}
	p.DiscoverableByPhone = byPhone != 0
	p.DiscoverableByName = byName != 0
	p.DirectorySyncEnabled = sync != 0
	return p, true
}
