package httpapi

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"locallink/backend/internal/ws"
)

// Bundle 310 (TODO_310 Issue 2): a Get chat request that stays "Sent".
//
//   P2  a phone that (re)connects its socket while an open admin card is
//       meant for it gets the "admin_action" hint again (the hint used to go
//       out only once, when the card was made). At most once per 30 s.
//   P3  the live card says WHY each phone has not answered, from what the
//       server already knows: its answer parts, its "no copy" note, its
//       receipt, its app build and its last admin check-in.
//   P4  every target phone is on an app that cannot say "I have no copy"
//       (older than 3.0.101) and nothing came back in 30 min -> the card
//       says "Old app on these phones" instead of an endless "Sent". The
//       request stays open (no auto-cancel).

const (
	// seenNoteAppBuild310: 3.0.101 (build 172) is the first app that
	// uploads its signed "no copy" notes (Bundle 307 A1).
	seenNoteAppBuild310 = 172
	oldAppWait310       = 30 * time.Minute
	connectHintEvery310 = 30 * time.Second
	maxPhonesShown310   = 50
)

// chatPhone310 is one target phone on the live card.
type chatPhone310 struct {
	Device  string `json:"device"`
	Name    string `json:"name"`
	Why     string `json:"why"` // parts | empty | no_trust | looking | old | got | wait
	Build   int64  `json:"build,omitempty"`
	Version string `json:"version,omitempty"`
	Checkin string `json:"checkin,omitempty"`
}

func (a *API) ensureCheckin310() {
	_, _ = a.store.DB().Exec(`CREATE TABLE IF NOT EXISTS admin_action_checkin_310(
		device_id TEXT PRIMARY KEY,
		at TEXT NOT NULL)`)
}

// noteCheckin310: the phone asked the server for its admin cards now.
func (a *API) noteCheckin310(deviceID string) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || len(deviceID) > 200 {
		return
	}
	a.ensureCheckin310()
	_, _ = a.store.DB().Exec(`INSERT INTO admin_action_checkin_310(device_id,at) VALUES(?,?)
		ON CONFLICT(device_id) DO UPDATE SET at=excluded.at`, deviceID, nowStamp())
}

// appVersion310: LinkUp builds from 157 (3.0.86) follow 3.0.(build-71).
func appVersion310(build int64) string {
	if build >= 157 && build < 1000 {
		return fmt.Sprintf("3.0.%d", build-71)
	}
	return ""
}

func stamp310(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	return t, err == nil
}

// chatPhones310 (P3): why each target phone of an open request has not
// answered (yet). Order: sending parts > no copy > can't check > looking >
// old app > got the request > not checked in since the request.
func (a *API) chatPhones310(id, madeAt string, targets []adminActionTarget) []chatPhone310 {
	if len(targets) == 0 {
		return nil
	}
	db := a.store.DB()
	_, answerDevice, _ := a.bestAnswer(id)
	seen := map[string]string{}
	if rows, err := db.Query(`SELECT device_id,why FROM admin_action_seen_307 WHERE action_id=?`, id); err == nil {
		for rows.Next() {
			var d, why string
			if rows.Scan(&d, &why) == nil && (seen[d] == "" || why == "empty") {
				seen[d] = why
			}
		}
		rows.Close()
	}
	receipt := map[string]bool{}
	if rows, err := db.Query(`SELECT device_id FROM admin_action_receipts WHERE action_id=?`, id); err == nil {
		for rows.Next() {
			var d string
			if rows.Scan(&d) == nil {
				receipt[d] = true
			}
		}
		rows.Close()
	}
	made, madeOK := stamp310(madeAt)
	a.ensureCheckin310()
	out := make([]chatPhone310, 0, len(targets))
	done := map[string]bool{}
	for _, t := range targets {
		d := t.Device
		if d == "" || done[d] {
			continue
		}
		done[d] = true
		if len(out) >= maxPhonesShown310 {
			break
		}
		p := chatPhone310{Device: d, Name: a.devicePerson276(d)}
		_ = db.QueryRow(`SELECT build FROM device_builds WHERE device_id=?`, d).Scan(&p.Build)
		p.Version = appVersion310(p.Build)
		_ = db.QueryRow(`SELECT at FROM admin_action_checkin_310 WHERE device_id=?`, d).Scan(&p.Checkin)
		checked := false
		if at, ok := stamp310(p.Checkin); ok && madeOK && !at.Before(made) {
			checked = true
		}
		switch {
		case d == answerDevice:
			p.Why = "parts"
		case seen[d] == "empty":
			p.Why = "empty"
		case seen[d] == "no_trust":
			p.Why = "no_trust"
		case receipt[d]:
			p.Why = "looking"
		case p.Build > 0 && p.Build < seenNoteAppBuild310:
			p.Why = "old"
		case checked:
			p.Why = "got"
		default:
			p.Why = "wait"
		}
		out = append(out, p)
	}
	return out
}

// stuckOldApp310 (P4): every phone is "old" and the request is 30+ min old.
func stuckOldApp310(madeAt string, phones []chatPhone310, now time.Time) bool {
	if len(phones) == 0 {
		return false
	}
	made, ok := stamp310(madeAt)
	if !ok || now.Sub(made) < oldAppWait310 {
		return false
	}
	for _, p := range phones {
		if p.Why != "old" {
			return false
		}
	}
	return true
}

// connectHints310 remembers when a phone got its last connect hint
// (key = this API + device, so two servers in one process never mix).
var connectHints310 sync.Map

func (a *API) connectHintDue310(deviceID string, now time.Time) bool {
	key := fmt.Sprintf("%p|%s", a, deviceID)
	if v, ok := connectHints310.Load(key); ok {
		if last, _ := v.(time.Time); now.Sub(last) < connectHintEvery310 {
			return false
		}
	}
	connectHints310.Store(key, now)
	return true
}

// openCardFor310: an open admin card names this phone as a target.
func (a *API) openCardFor310(deviceID string) bool {
	if strings.TrimSpace(deviceID) == "" || len(deviceID) > 200 || strings.ContainsAny(deviceID, `"`) {
		return false
	}
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(deviceID)
	var n int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM admin_actions WHERE state='pending' AND targets_json LIKE ? ESCAPE '\'`,
		`%"device":"`+esc+`"%`).Scan(&n)
	return n > 0
}

// hintOnConnect310 (P2): the socket of a target phone just came up.
func (a *API) hintOnConnect310(deviceID string, client *ws.Client) {
	if client == nil || !a.openCardFor310(deviceID) || !a.connectHintDue310(deviceID, time.Now()) {
		return
	}
	_ = client.WriteText(mustJSON(WSMessage{Type: "admin_action"}))
}
