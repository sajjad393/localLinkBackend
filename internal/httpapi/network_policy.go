package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type networkPolicyRequest struct {
	WiFiRadioPolicy *string `json:"wifi_radio_policy"`
}

type effectiveNetworkPolicy struct {
	UserID                string `json:"user_id,omitempty"`
	DeviceID              string `json:"device_id,omitempty"`
	UserWiFiRadioPolicy   string `json:"user_wifi_radio_policy,omitempty"`
	DeviceWiFiRadioPolicy string `json:"device_wifi_radio_policy,omitempty"`
	WiFiRadioPolicy       string `json:"wifi_radio_policy"`
	WiFiRadioSource       string `json:"wifi_radio_source"`
	UserPolicyVersion     int    `json:"user_policy_version,omitempty"`
	DevicePolicyVersion   int    `json:"device_policy_version,omitempty"`
	UpdatedAt             string `json:"updated_at"`
	LocalOnly             bool   `json:"local_only"`
}

const (
	policyWifiAllowed   = "allowed"
	policyWifiForcedOn  = "forced_on"
	policyWifiForcedOff = "forced_off"
)

func normalizeWifiPolicy(raw *string) (string, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return policyWifiAllowed, nil
	}
	v := strings.ToLower(strings.TrimSpace(*raw))
	if v != policyWifiAllowed && v != policyWifiForcedOn && v != policyWifiForcedOff {
		return "", fmt.Errorf("invalid Wi-Fi radio policy")
	}
	return v, nil
}

func (a *API) AdminGetUserNetworkPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("id"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user id required")
		return
	}
	p, err := a.userNetworkPolicy(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read network policy")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) AdminUpdateUserNetworkPolicy(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_network_policy")
	if !ok {
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("id"))
	if userID == "" || !a.userExists(userID) {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var p networkPolicyRequest
	if err := decodeJSON(r, &p, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid network policy request")
		return
	}
	wifi, err := normalizeWifiPolicy(p.WiFiRadioPolicy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = a.store.DB().Exec(`INSERT INTO admin_user_network_policies(user_id,wifi_radio_policy,version,updated_by,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET wifi_radio_policy=excluded.wifi_radio_policy,version=admin_user_network_policies.version+1,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, userID, wifi, 1, admin.ID, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update network policy")
		return
	}
	a.adminAudit(admin.ID, "update_user_network_policy", userID, "wifi_radio_policy="+wifi)
	a.pushNetworkPolicyToUser(userID)
	p2, _ := a.userNetworkPolicy(userID)
	writeJSON(w, http.StatusOK, p2)
}

func (a *API) AdminGetDeviceNetworkPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	deviceID := strings.TrimSpace(r.URL.Query().Get("id"))
	if deviceID == "" {
		writeError(w, http.StatusBadRequest, "device id required")
		return
	}
	p, err := a.effectiveNetworkPolicy(deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read network policy")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) AdminUpdateDeviceNetworkPolicy(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdminPermission(w, r, "manage_network_policy")
	if !ok {
		return
	}
	deviceID := strings.TrimSpace(r.URL.Query().Get("id"))
	if deviceID == "" || !a.deviceExists(deviceID) {
		writeError(w, http.StatusBadRequest, "invalid device id")
		return
	}
	var p networkPolicyRequest
	if err := decodeJSON(r, &p, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid network policy request")
		return
	}
	wifi, err := normalizeWifiPolicy(p.WiFiRadioPolicy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = a.store.DB().Exec(`INSERT INTO admin_device_network_policies(device_id,wifi_radio_policy,version,updated_by,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(device_id) DO UPDATE SET wifi_radio_policy=excluded.wifi_radio_policy,version=admin_device_network_policies.version+1,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, deviceID, wifi, 1, admin.ID, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update network policy")
		return
	}
	a.adminAudit(admin.ID, "update_device_network_policy", deviceID, "wifi_radio_policy="+wifi)
	a.pushNetworkPolicyToDevice(deviceID)
	p2, _ := a.effectiveNetworkPolicy(deviceID)
	writeJSON(w, http.StatusOK, p2)
}

func (a *API) DeviceGetNetworkPolicy(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		return
	}
	p, err := a.effectiveNetworkPolicy(deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read network policy")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (a *API) userNetworkPolicy(userID string) (*effectiveNetworkPolicy, error) {
	p := &effectiveNetworkPolicy{UserID: userID, WiFiRadioPolicy: policyWifiAllowed, WiFiRadioSource: "local", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), LocalOnly: true}
	row := a.store.DB().QueryRow(`SELECT COALESCE(wifi_radio_policy,''),version,updated_at FROM admin_user_network_policies WHERE user_id=?`, userID)
	if err := row.Scan(&p.UserWiFiRadioPolicy, &p.UserPolicyVersion, &p.UpdatedAt); err != nil {
		// No policy row is the normal local-default state.
		if err.Error() != "sql: no rows in result set" {
			return nil, err
		}
	}
	if p.UserWiFiRadioPolicy != "" {
		p.WiFiRadioPolicy = p.UserWiFiRadioPolicy
		p.WiFiRadioSource = "user_admin"
	}
	return p, nil
}

func (a *API) effectiveNetworkPolicy(deviceID string) (*effectiveNetworkPolicy, error) {
	var userID string
	if err := a.store.DB().QueryRow(`SELECT COALESCE(user_id,'') FROM devices WHERE id=?`, deviceID).Scan(&userID); err != nil {
		return nil, err
	}
	p := &effectiveNetworkPolicy{DeviceID: deviceID, UserID: userID, WiFiRadioPolicy: policyWifiAllowed, WiFiRadioSource: "local", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), LocalOnly: true}
	if userID != "" {
		if user, err := a.userNetworkPolicy(userID); err == nil {
			p.UserWiFiRadioPolicy = user.UserWiFiRadioPolicy
			p.UserPolicyVersion = user.UserPolicyVersion
			if user.UserWiFiRadioPolicy != "" {
				p.WiFiRadioPolicy = user.WiFiRadioPolicy
				p.WiFiRadioSource = "user_admin"
			}
			p.UpdatedAt = user.UpdatedAt
		}
	}
	row := a.store.DB().QueryRow(`SELECT COALESCE(wifi_radio_policy,''),version,updated_at FROM admin_device_network_policies WHERE device_id=?`, deviceID)
	if err := row.Scan(&p.DeviceWiFiRadioPolicy, &p.DevicePolicyVersion, &p.UpdatedAt); err == nil && p.DeviceWiFiRadioPolicy != "" {
		p.WiFiRadioPolicy = p.DeviceWiFiRadioPolicy
		p.WiFiRadioSource = "device_admin"
	}
	return p, nil
}

func (a *API) pushNetworkPolicyToDevice(deviceID string) {
	p, err := a.effectiveNetworkPolicy(deviceID)
	if err != nil {
		return
	}
	payload := mustJSON(WSMessage{Type: "network_policy_update", DeviceID: deviceID, Body: string(mustJSON(p))})
	a.hub.SendTo(deviceID, payload)
}

func (a *API) pushNetworkPolicyToUser(userID string) {
	rows, err := a.store.DB().Query(`SELECT id FROM devices WHERE user_id=? AND COALESCE(status,'active')='active'`, userID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			a.pushNetworkPolicyToDevice(id)
		}
	}
}
