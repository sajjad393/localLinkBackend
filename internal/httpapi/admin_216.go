package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Bundle 216: names and photos for the admin pages (app + web), the link map
// built from what phones report, the admin photo route and the web fonts.
// Everything here is additive: new JSON fields only, no migration.

// ---------------------------------------------------------------- names

type person216 struct {
	UserID    string
	Username  string
	Display   string
	HasAvatar bool
}

type phone216 struct {
	DeviceID   string
	DeviceName string
	person216
}

func uniq216(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 200 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func placeholders216(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func chunks216(ids []string, size int) [][]string {
	var out [][]string
	for len(ids) > size {
		out = append(out, ids[:size])
		ids = ids[size:]
	}
	if len(ids) > 0 {
		out = append(out, ids)
	}
	return out
}

func anyArgs216(ids []string) []any {
	out := make([]any, len(ids))
	for i, v := range ids {
		out[i] = v
	}
	return out
}

// people216 returns display name (profile name, else username) per user id.
func (a *API) people216(userIDs []string) map[string]person216 {
	out := map[string]person216{}
	for _, part := range chunks216(uniq216(userIDs), 400) {
		rows, err := a.store.DB().Query(`SELECT u.id,u.username,COALESCE(p.display_name,''),CASE WHEN COALESCE(p.avatar_path,'')<>'' AND COALESCE(p.avatar_size,0)>0 THEN 'y' ELSE '' END
			FROM users u LEFT JOIN profiles p ON p.user_id=u.id WHERE u.id IN (`+placeholders216(len(part))+`)`, anyArgs216(part)...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var p person216
			var avatar string
			if rows.Scan(&p.UserID, &p.Username, &p.Display, &avatar) != nil {
				continue
			}
			p.Display = personName276(p.Display, p.Username) // Bundle 276
			p.HasAvatar = strings.TrimSpace(avatar) != ""
			out[p.UserID] = p
		}
		rows.Close()
	}
	return out
}

// phones216 returns phone name + owner per device id.
func (a *API) phones216(deviceIDs []string) map[string]phone216 {
	out := map[string]phone216{}
	var users []string
	for _, part := range chunks216(uniq216(deviceIDs), 400) {
		rows, err := a.store.DB().Query(`SELECT id,COALESCE(name,''),COALESCE(user_id,'') FROM devices WHERE id IN (`+placeholders216(len(part))+`)`, anyArgs216(part)...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var d phone216
			if rows.Scan(&d.DeviceID, &d.DeviceName, &d.UserID) != nil {
				continue
			}
			out[d.DeviceID] = d
			users = append(users, d.UserID)
		}
		rows.Close()
	}
	people := a.people216(users)
	for id, d := range out {
		if p, ok := people[d.UserID]; ok {
			d.person216 = p
		}
		d.DeviceName = phoneLabel276(d.DeviceName, d.Display) // Bundle 276
		out[id] = d
	}
	return out
}

// adminNames216: admin account id -> shown name (app admin = profile name).
func (a *API) adminNames216(adminIDs []string) map[string]string {
	out := map[string]string{}
	var users []string
	owner := map[string]string{}
	for _, part := range chunks216(uniq216(adminIDs), 400) {
		rows, err := a.store.DB().Query(`SELECT id,COALESCE(username,''),COALESCE(user_id,'') FROM admin_accounts WHERE id IN (`+placeholders216(len(part))+`)`, anyArgs216(part)...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id, name, userID string
			if rows.Scan(&id, &name, &userID) != nil {
				continue
			}
			out[id] = name
			if userID != "" {
				owner[id] = userID
				users = append(users, userID)
			}
		}
		rows.Close()
	}
	people := a.people216(users)
	for id, userID := range owner {
		if p, ok := people[userID]; ok && p.Display != "" {
			out[id] = p.Display
		}
	}
	return out
}

// targetName216 resolves an audit target: user, phone, admin, or "a|b".
func targetName216(raw string, people map[string]person216, phones map[string]phone216, admins map[string]string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "|")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case people[p].Display != "":
			names = append(names, people[p].Display)
		case phones[p].DeviceName != "":
			names = append(names, phones[p].DeviceName)
		case admins[p] != "":
			names = append(names, admins[p])
		default:
			return ""
		}
	}
	return strings.Join(names, " \u2194 ")
}

func transferKind216(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(ct, "image/"):
		return "photo"
	case strings.HasPrefix(ct, "video/"):
		return "video"
	case strings.HasPrefix(ct, "audio/"):
		return "voice"
	default:
		return "document"
	}
}

// ---------------------------------------------------------------- enrich

func (a *API) enrichUsers216(list []AdminUser) {
	ids := make([]string, len(list))
	for i, u := range list {
		ids[i] = u.ID
	}
	people := a.people216(ids)
	seen := map[string]string{}
	for _, part := range chunks216(uniq216(ids), 400) {
		rows, err := a.store.DB().Query(`SELECT user_id,MAX(last_seen_at) FROM devices WHERE user_id IN (`+placeholders216(len(part))+`) AND COALESCE(status,'active')='active' GROUP BY user_id`, anyArgs216(part)...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id, at string
			if rows.Scan(&id, &at) == nil {
				seen[id] = at
			}
		}
		rows.Close()
	}
	// Bundle 218: "Joined from offline · date".
	joined := map[string]string{}
	for _, part := range chunks216(uniq216(ids), 400) {
		rows, err := a.store.DB().Query(`SELECT user_id,MIN(linked_at) FROM offline_ids WHERE user_id IN (`+placeholders216(len(part))+`) GROUP BY user_id`, anyArgs216(part)...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id, at string
			if rows.Scan(&id, &at) == nil {
				joined[id] = at
			}
		}
		rows.Close()
	}
	for i := range list {
		list[i].JoinedOfflineAt = joined[list[i].ID]
		p := people[list[i].ID]
		list[i].DisplayName = p.Display
		if list[i].DisplayName == "" {
			list[i].DisplayName = list[i].Username
		}
		list[i].HasAvatar = p.HasAvatar
		list[i].LastSeenAt = seen[list[i].ID]
	}
}

func (a *API) enrichDevices216(list []AdminDevice) {
	users := make([]string, 0, len(list))
	ids := make([]string, 0, len(list))
	for _, d := range list {
		users = append(users, d.UserID)
		ids = append(ids, d.ID)
	}
	people := a.people216(users)
	builds := map[string]int64{}
	for _, part := range chunks216(uniq216(ids), 400) {
		rows, err := a.store.DB().Query(`SELECT device_id,MAX(build) FROM device_builds WHERE device_id IN (`+placeholders216(len(part))+`) GROUP BY device_id`, anyArgs216(part)...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id string
			var b int64
			if rows.Scan(&id, &b) == nil {
				builds[id] = b
			}
		}
		rows.Close()
	}
	for i := range list {
		if p, ok := people[list[i].UserID]; ok {
			list[i].DisplayName = p.Display
			list[i].HasAvatar = p.HasAvatar
		}
		list[i].Name = phoneLabel276(list[i].Name, firstNonEmpty216(list[i].DisplayName, list[i].Username)) // Bundle 276
		list[i].AppBuild = builds[list[i].ID]
	}
}

func (a *API) enrichSessions216(list []AdminAuthSession) {
	ids := make([]string, len(list))
	for i, s := range list {
		ids[i] = s.UserID
	}
	people := a.people216(ids)
	for i := range list {
		list[i].DisplayName = people[list[i].UserID].Display
		if list[i].DisplayName == "" {
			list[i].DisplayName = personName276("", list[i].Username)
		}
		list[i].DeviceName = phoneLabel276(list[i].DeviceName, list[i].DisplayName) // Bundle 276
	}
}

func (a *API) enrichCalls216(list []CallRecord) {
	ids := make([]string, 0, 2*len(list))
	for _, c := range list {
		ids = append(ids, c.CallerID, c.CalleeID)
	}
	phones := a.phones216(ids)
	for i := range list {
		if p, ok := phones[list[i].CallerID]; ok {
			list[i].CallerUserID, list[i].CallerName = p.UserID, firstNonEmpty216(p.Display, p.DeviceName)
		}
		if p, ok := phones[list[i].CalleeID]; ok {
			list[i].CalleeUserID, list[i].CalleeName = p.UserID, firstNonEmpty216(p.Display, p.DeviceName)
		}
	}
}

func (a *API) enrichSecurity216(list []AdminSecurityEvent) {
	users := make([]string, 0, len(list))
	devices := make([]string, 0, len(list))
	for _, e := range list {
		users = append(users, e.UserID)
		devices = append(devices, e.DeviceID)
	}
	people := a.people216(users)
	phones := a.phones216(devices)
	for i := range list {
		p := people[list[i].UserID]
		list[i].Username, list[i].DisplayName = p.Username, p.Display
		list[i].DeviceName = phones[list[i].DeviceID].DeviceName
	}
}

func (a *API) enrichLogs216(list []AdminLog) {
	admins := make([]string, 0, len(list))
	targets := make([]string, 0, len(list))
	for _, l := range list {
		admins = append(admins, l.AdminID)
		targets = append(targets, strings.Split(l.TargetID, "|")...)
	}
	adminNames := a.adminNames216(append(admins, targets...))
	people := a.people216(targets)
	phones := a.phones216(targets)
	for i := range list {
		list[i].AdminName = adminNames[list[i].AdminID]
		list[i].TargetName = targetName216(list[i].TargetID, people, phones, adminNames)
	}
}

func (a *API) enrichTransfers216(list []AdminTransfer) {
	ids := make([]string, len(list))
	for i, t := range list {
		ids[i] = t.OwnerID
	}
	phones := a.phones216(ids)
	for i := range list {
		p := phones[list[i].OwnerID]
		list[i].OwnerUserID = p.UserID
		list[i].OwnerName = firstNonEmpty216(p.Display, p.DeviceName)
		list[i].Kind = transferKind216(list[i].ContentType)
	}
}

func firstNonEmpty216(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// settingsExtras216 fills the last backup and the real server limits.
func (a *API) settingsExtras216(s *AdminSettings) {
	dir := strings.TrimSpace(os.Getenv("LOCALLINK_BACKUP_DIR"))
	if dir == "" {
		dir = "data/backups"
	}
	if entries, err := os.ReadDir(dir); err == nil {
		var newest time.Time
		var size int64
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(newest) {
				newest, size = info.ModTime(), info.Size()
			}
		}
		if !newest.IsZero() {
			s.LastBackupAt = newest.UTC().Format(time.RFC3339)
			s.LastBackupBytes = size
		}
	}
	s.MaxFileBytes = a.maxFileSize
	if s.MaxFileBytes <= 0 {
		s.MaxFileBytes = defaultMaxFileSize
	}
	s.DeviceQuotaBytes = a.deviceStorageQuota()
	s.TLSEnabled = strings.TrimSpace(os.Getenv("LOCALLINK_TLS_CERT")) != "" && strings.TrimSpace(os.Getenv("LOCALLINK_TLS_KEY")) != ""
}

// ---------------------------------------------------------------- avatar

// AdminUserAvatar: GET /api/v1/admin/users/{id}/avatar (any admin role).
func (a *API) AdminUserAvatar(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" || len(userID) > 128 || hasControlChars(userID) {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	a.serveAvatar(w, r, userID)
}

// ---------------------------------------------------------------- link map

// Neighbor is one phone a phone can reach without the server.
type Neighbor struct {
	DeviceID string `json:"device_id"`
	Link     string `json:"link"`
}

const (
	maxNeighbors216 = 32
	// Bundle 296 (H4): a phone's link report counts 45 s (was 4 min). The
	// app sends only proven links every 15 s and at once on a change, so a
	// living link is never older than ~15 s; 45 s = 3 reports.
	neighborTTL216    = 45 * time.Second
	linkMapOnline216  = 2 * time.Minute
	linkWifiDirect216 = "wifi_direct"
	linkMesh216       = "mesh"
	linkLan216        = "lan"
)

type neighborReport216 struct {
	at   time.Time
	list []Neighbor
}

type linkStore216 struct {
	mu      sync.Mutex
	reports map[string]neighborReport216
}

var linkStores216 sync.Map

func (a *API) linkStore() *linkStore216 {
	if s, ok := linkStores216.Load(a); ok {
		return s.(*linkStore216)
	}
	s, _ := linkStores216.LoadOrStore(a, &linkStore216{reports: map[string]neighborReport216{}})
	return s.(*linkStore216)
}

func validLink216(link string) bool {
	return link == linkWifiDirect216 || link == linkMesh216 || link == linkLan216
}

// noteNeighbors216 keeps the newest report of one phone (in memory only).
func (a *API) noteNeighbors216(deviceID string, list []Neighbor) {
	if deviceID == "" || list == nil {
		return
	}
	clean := make([]Neighbor, 0, len(list))
	seen := map[string]bool{}
	for _, n := range list {
		id := strings.TrimSpace(n.DeviceID)
		link := strings.TrimSpace(strings.ToLower(n.Link))
		if id == "" || id == deviceID || len(id) > 200 || !validLink216(link) || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, Neighbor{DeviceID: id, Link: link})
		if len(clean) >= maxNeighbors216 {
			break
		}
	}
	s := a.linkStore()
	s.mu.Lock()
	old, had := s.reports[deviceID]
	changed := !had || !sameNeighbors274(old.list, clean)
	s.reports[deviceID] = neighborReport216{at: time.Now(), list: clean}
	if len(s.reports) > 20000 {
		cut := time.Now().Add(-neighborTTL216)
		for k, v := range s.reports {
			if v.at.Before(cut) {
				delete(s.reports, k)
			}
		}
	}
	s.mu.Unlock()
	if changed {
		a.linkMapChanged274() // Bundle 274: a phone's links changed
	}
}

// onlineNow274 (Bundle 274 R-3): online = connected to the server now. The
// "seen in the last 2 min" rule is used only for a phone the server has no
// live state for (e.g. right after a restart).
func (a *API) onlineNow274(deviceID string, seenRecently bool) bool {
	if a.hub != nil && a.hub.HasDevice(deviceID) {
		return true
	}
	a.presenceMu.RLock()
	p, ok := a.presence[deviceID]
	a.presenceMu.RUnlock()
	if ok {
		return p.ServerConnected && seenRecently
	}
	return seenRecently
}

// sameNeighbors274: same phones with the same link words (any order).
func sameNeighbors274(a, b []Neighbor) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]string, len(a))
	for _, n := range a {
		m[n.DeviceID] = n.Link
	}
	for _, n := range b {
		if l, ok := m[n.DeviceID]; !ok || l != n.Link {
			return false
		}
	}
	return true
}

type linkNode216 struct {
	DeviceID    string `json:"device_id"`
	DeviceName  string `json:"device_name"`
	UserID      string `json:"user_id,omitempty"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	HasAvatar   bool   `json:"has_avatar"`
	AdminRole   string `json:"admin_role,omitempty"`
	Blocked     bool   `json:"blocked"`
	Online      bool   `json:"online"`
	Link        string `json:"link"`
}

type linkEdge216 struct {
	From string `json:"from"`
	To   string `json:"to"`
	Link string `json:"link"`
}

// AdminLinkMap: GET /api/v1/admin/link-map (any admin role).
func (a *API) AdminLinkMap(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdminPermission(w, r, "read"); !ok {
		return
	}
	nodes, edges, err := a.linkMap216(time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build link map")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "edges": edges, "server": "server"})
}

func (a *API) linkMap216(now time.Time) ([]linkNode216, []linkEdge216, error) {
	rows, err := a.store.DB().Query(`SELECT d.id,COALESCE(d.name,''),COALESCE(d.user_id,''),d.last_seen_at,COALESCE(u.status,'active'),
		COALESCE((SELECT aa.role FROM admin_accounts aa WHERE aa.user_id=d.user_id AND aa.status='active'),'')
		FROM devices d LEFT JOIN users u ON u.id=d.user_id
		WHERE COALESCE(d.status,'active')='active' AND COALESCE(d.revoked_at,'')='' AND COALESCE(u.status,'active') IN ('active','suspended')
		ORDER BY d.last_seen_at DESC`)
	if err != nil {
		return nil, nil, err
	}
	type raw struct {
		id, name, userID, seen, status, role string
	}
	var list []raw
	for rows.Next() {
		var x raw
		if rows.Scan(&x.id, &x.name, &x.userID, &x.seen, &x.status, &x.role) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	users := make([]string, 0, len(list))
	for _, x := range list {
		users = append(users, x.userID)
	}
	people := a.people216(users)
	cutoff := now.UTC().Add(-linkMapOnline216).Format(time.RFC3339Nano)
	s := a.linkStore()
	s.mu.Lock()
	reports := make(map[string]neighborReport216, len(s.reports))
	for k, v := range s.reports {
		if now.Sub(v.at) <= neighborTTL216 {
			reports[k] = v
		}
	}
	s.mu.Unlock()

	nodes := make([]linkNode216, 0, len(list))
	known := map[string]int{}
	for _, x := range list {
		if _, dup := known[x.id]; dup {
			continue
		}
		p := people[x.userID]
		n := linkNode216{DeviceID: x.id, DeviceName: phoneLabel276(x.name, p.Display), UserID: x.userID, Username: p.Username, DisplayName: p.Display,
			HasAvatar: p.HasAvatar, AdminRole: x.role, Blocked: x.status == "suspended", Online: a.onlineNow274(x.id, x.seen >= cutoff)}
		if n.Online {
			n.Link = "server"
		}
		known[x.id] = len(nodes)
		nodes = append(nodes, n)
	}
	edges := []linkEdge216{}
	seenEdge := map[string]bool{}
	for _, n := range nodes {
		if n.Online {
			edges = append(edges, linkEdge216{From: "server", To: n.DeviceID, Link: "server"})
		}
	}
	ids := make([]string, 0, len(reports))
	for id := range reports {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, from := range ids {
		fi, ok := known[from]
		if !ok {
			continue
		}
		// Bundle 296 (H4): only a phone on the server now can vouch for its
		// links (a phone that left may have lost them since).
		if !nodes[fi].Online {
			continue
		}
		for _, nb := range reports[from].list {
			ti, ok := known[nb.DeviceID]
			if !ok {
				continue
			}
			a1, b1 := from, nb.DeviceID
			if a1 > b1 {
				a1, b1 = b1, a1
			}
			key := a1 + "|" + b1
			if seenEdge[key] {
				continue
			}
			seenEdge[key] = true
			edges = append(edges, linkEdge216{From: from, To: nb.DeviceID, Link: nb.Link})
			// A phone that is not on the server shows the way it is reached.
			if !nodes[ti].Online && nodes[ti].Link == "" {
				nodes[ti].Link = nb.Link
			}
			if !nodes[fi].Online && nodes[fi].Link == "" {
				nodes[fi].Link = nb.Link
			}
		}
	}
	return nodes, edges, nil
}

// ---------------------------------------------------------------- fonts

//go:embed adminweb/fonts/*.ttf adminweb/fonts/OFL.txt
var adminFontsFS embed.FS

// AdminFont serves the Roboto files of the web panel (same server, no CDN).
func (a *API) AdminFont(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)
	if name == "" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(adminFontsFS, "adminweb/fonts/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := "font/ttf"
	if filepath.Ext(name) == ".txt" {
		ct = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
