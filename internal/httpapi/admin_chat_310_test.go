package httpapi

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Bundle 310 (TODO_310 Issue 2): why a Get chat request waits.

func setBuild310(t *testing.T, s *blocks212, deviceID string, build int64) {
	t.Helper()
	if _, err := s.api.store.DB().Exec(`INSERT INTO device_builds(device_id,build,seen_at) VALUES(?,?,?)
		ON CONFLICT(device_id) DO UPDATE SET build=excluded.build`, deviceID, build, nowStamp()); err != nil {
		t.Fatal(err)
	}
}

func whyOf310(r chatRequestRow, deviceID string) string {
	for _, p := range r.Who {
		if p.Device == deviceID {
			return p.Why
		}
	}
	return "-"
}

func ageRequest310(t *testing.T, s *blocks212, id string, d time.Duration) {
	t.Helper()
	if _, err := s.api.store.DB().Exec(`UPDATE admin_actions SET made_at=? WHERE id=?`,
		time.Now().Add(-d).UTC().Format(time.RFC3339Nano), id); err != nil {
		t.Fatal(err)
	}
}

func TestWhyEachPhoneWaits310(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	id, card := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})
	a, b := s.ali.Device.ID, s.bilal.Device.ID

	r := row307(t, s, id)
	if len(r.Who) != 2 || whyOf310(r, a) != "wait" || whyOf310(r, b) != "wait" || r.Stuck != "" {
		t.Fatalf("fresh request: both phones not checked in yet: %+v", r.Who)
	}
	for _, p := range r.Who {
		if p.Name == "" {
			t.Fatalf("every phone has its owner's name: %+v", p)
		}
	}
	// ali checks in after the request was made: "got the request".
	sync220(t, s.api, s.ali, map[string]any{"signing_key": ali.signingKey()})
	if r = row307(t, s, id); whyOf310(r, a) != "got" || whyOf310(r, b) != "wait" {
		t.Fatalf("ali checked in: %+v", r.Who)
	}
	// bilal is on 3.0.100 (build 171): can't say "no copy" -> old app.
	setBuild310(t, s, b, 171)
	r = row307(t, s, id)
	if whyOf310(r, b) != "old" {
		t.Fatalf("bilal on 171 = old app: %+v", r.Who)
	}
	for _, p := range r.Who {
		if p.Device == b && (p.Build != 171 || p.Version != "3.0.100") {
			t.Fatalf("old app version shown: %+v", p)
		}
	}
	// ali has no copy -> "no copy" (the request stays open: bilal is old).
	sync220(t, s.api, s.ali, map[string]any{"seen": []signedBlob{ali.seen307(id, "empty")}})
	if r = row307(t, s, id); whyOf310(r, a) != "empty" || r.State != "pending" {
		t.Fatalf("ali said no copy: %+v %s", r.Who, r.State)
	}
	// Not every phone is old -> never "old_app", even after 30 min.
	ageRequest310(t, s, id, 40*time.Minute)
	if r = row307(t, s, id); r.Stuck != "" {
		t.Fatalf("one phone answered: not stuck on old apps: %+v", r)
	}
	// ali sends answer parts -> "sending parts".
	ans := id + ".ali"
	upload221(t, s, s.ali, part221(t, ali, card.ReplyKey, ans, id, 1, 2, map[string]any{"messages": []any{}}))
	if r = row307(t, s, id); whyOf310(r, a) != "parts" {
		t.Fatalf("ali sends parts: %+v", r.Who)
	}
}

func TestOldAppOnEveryPhoneAfter30Min310(t *testing.T) {
	s := setup212(t)
	addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	setBuild310(t, s, s.ali.Device.ID, 171)
	setBuild310(t, s, s.bilal.Device.ID, 170)
	id, _ := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})
	if r := row307(t, s, id); r.Stuck != "" || whyOf310(r, s.ali.Device.ID) != "old" {
		t.Fatalf("under 30 min: still Sent, phones say old app: %+v", r)
	}
	ageRequest310(t, s, id, 31*time.Minute)
	r := row307(t, s, id)
	if r.Stuck != "old_app" || r.State != "pending" {
		t.Fatalf("30 min + every phone old -> old_app, request stays open: %+v", r)
	}
	// One phone updates (3.0.102 = 173) -> no longer stuck.
	setBuild310(t, s, s.bilal.Device.ID, 173)
	if r = row307(t, s, id); r.Stuck != "" || whyOf310(r, s.bilal.Device.ID) != "wait" {
		t.Fatalf("an updated phone can answer: %+v", r)
	}
	// Finished requests carry no phone lines.
	call221(t, s.api.AdminActionCancel, "POST", "/x", id, nil, owner221(s))
	if r = row307(t, s, id); r.State != "cancelled" || len(r.Who) != 0 || r.Stuck != "" {
		t.Fatalf("cancelled: no why lines: %+v", r)
	}
}

func TestConnectHintOnlyForTargetsAndThrottled310(t *testing.T) {
	s := setup212(t)
	addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	if s.api.openCardFor310(s.ali.Device.ID) {
		t.Fatal("no open card yet")
	}
	id, _ := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})
	if !s.api.openCardFor310(s.ali.Device.ID) || !s.api.openCardFor310(s.bilal.Device.ID) {
		t.Fatal("both target phones have an open card")
	}
	if s.api.openCardFor310(s.adminDev.Device.ID) || s.api.openCardFor310("%") || s.api.openCardFor310("dev_ali_212") {
		t.Fatal("not a target / wildcards never match")
	}
	call221(t, s.api.AdminActionCancel, "POST", "/x", id, nil, owner221(s))
	if s.api.openCardFor310(s.ali.Device.ID) {
		t.Fatal("a cancelled card is not open")
	}
	now := time.Now()
	if !s.api.connectHintDue310("dev-x-310", now) || s.api.connectHintDue310("dev-x-310", now.Add(10*time.Second)) ||
		!s.api.connectHintDue310("dev-x-310", now.Add(31*time.Second)) {
		t.Fatal("one connect hint per 30 s")
	}
	if !s.api.connectHintDue310("dev-y-310", now) {
		t.Fatal("throttle is per phone")
	}
	src, _ := os.ReadFile("websocket.go")
	w := string(src)
	i, j := strings.Index(w, "a.pushNetworkPolicyToDevice(deviceID)"), strings.Index(w, "a.hintOnConnect310(deviceID, client)")
	if i < 0 || j < i {
		t.Fatal("websocket.go: the connect hint goes after ready + policy")
	}
	sync, _ := os.ReadFile("admin_actions_220.go")
	if strings.Count(string(sync), "a.noteCheckin310(") != 2 {
		t.Fatal("sync + mine record the admin check-in")
	}
}

func TestAppVersion310(t *testing.T) {
	for build, want := range map[int64]string{171: "3.0.100", 172: "3.0.101", 173: "3.0.102", 174: "3.0.103", 157: "3.0.86", 132: "", 0: ""} {
		if got := appVersion310(build); got != want {
			t.Fatalf("build %d -> %q, want %q", build, got, want)
		}
	}
}

// Web (TODO_310 V2/V3/E1/P3/P4).
func TestAdminWebConversations310(t *testing.T) {
	rawJS, err := os.ReadFile("adminweb/views.js")
	if err != nil {
		t.Fatal(err)
	}
	views := string(rawJS)
	rawCSS, _ := os.ReadFile("adminweb/styles.css")
	css := string(rawCSS)
	for _, want := range []string{
		// V2 voice bubble
		"function isVoice310(", "function bars310(", "function voice310(", "data-conv310-play", "new Audio(", "?download=1",
		// V3 bubbles like the app
		"class=\"ft310\"", "const showName = group && ", "kebab221",
		// short names
		"function shortName310(",
		// E1 live row
		"wlg307 n${live.length}", "live.slice(0, 3).map(liveCard307)", "function liveWide310(", "data-conv310-scroll",
		// P3 / P4
		"function why310(", "q.stuck === 'old_app'", "Update LinkUp on these phones",
		// P3 on the In progress card and in the open request
		"${cancel}</div>${phones310(q, st, true)}</div>", "<b>${head}</b>${phones310(q, st, true)}",
	} {
		if !strings.Contains(views, want) {
			t.Fatalf("views.js must contain %q", want)
		}
	}
	for _, want := range []string{
		".wlg307.n1 { grid-template-columns: 1fr; }", ".wlg307.n2 { grid-template-columns: repeat(2, minmax(0, 1fr)); }",
		".hc307.s-old", ".vo310", ".b221 .kebab221 { opacity: 0;", ".b221:hover .kebab221",
		"--a-surface: #faf8ff", "--a-surface: #121318",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("styles.css must contain %q", want)
		}
	}
}

func TestAdminWebConversationsSmoke310(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/conv_310_smoke.js", "adminweb/views.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "SMOKE_OK") {
		t.Fatalf("conversations smoke 310 failed: %v\n%s", err, out)
	}
}
