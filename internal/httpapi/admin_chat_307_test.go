package httpapi

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Bundle 307 (TODO_307 Part A): Get chat closes by itself.

func (p phone220) seen307(actionID, why string) signedBlob {
	raw, _ := json.Marshal(adminSeenNote307{T: adminSeenType307, ID: actionID, Device: p.auth.Device.ID,
		User: p.auth.Account.ID, Why: why, At: time.Now().UTC().Format(time.RFC3339Nano)})
	return signedBlob{Body: string(raw), Sig: b64url(ed25519.Sign(p.sign, raw))}
}

func listRequests307(t *testing.T, s *blocks212) []chatRequestRow {
	t.Helper()
	res := call221(t, s.api.AdminChatRequestList, http.MethodGet, "/api/v1/admin/chat-requests", "", nil, owner221(s))
	if res.Code != http.StatusOK {
		t.Fatalf("list %d %s", res.Code, res.Body.String())
	}
	var out struct {
		Requests []chatRequestRow `json:"requests"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	return out.Requests
}

func row307(t *testing.T, s *blocks212, id string) chatRequestRow {
	t.Helper()
	for _, r := range listRequests307(t, s) {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("request %s not listed", id)
	return chatRequestRow{}
}

func TestSeenNotesCloseAsNoMessages307(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	bilal := addPhoneKeys220(t, s.api, s.bilal)
	id, _ := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})
	// bilal's key is known after his first sync.
	sync220(t, s.api, s.bilal, map[string]any{"signing_key": bilal.signingKey()})

	// A note with a wrong signature carried by ali is dropped.
	bad := bilal.seen307(id, "empty")
	bad.Sig = ali.seen307(id, "empty").Sig
	sync220(t, s.api, s.ali, map[string]any{"signing_key": ali.signingKey(), "seen": []signedBlob{bad}})
	if r := row307(t, s, id); r.State != "pending" || r.Empty || r.Phones != 2 || r.Reached != 0 {
		t.Fatalf("a bad note must not count: %+v", r)
	}
	// ali says "no copy here" itself: still waiting for bilal.
	sync220(t, s.api, s.ali, map[string]any{"seen": []signedBlob{ali.seen307(id, "empty")}})
	if r := row307(t, s, id); r.State != "pending" || r.Empty || r.Phones != 2 || r.Reached != 1 {
		t.Fatalf("one phone of two (live card: 1 of 2 phones): %+v", r)
	}
	// A phone that is not a target can't close it.
	stranger := registerDirectoryUser(t, s.api, "carol307", "dev-carol-307")
	carol := addPhoneKeys220(t, s.api, stranger)
	sync220(t, s.api, stranger, map[string]any{"signing_key": carol.signingKey(), "seen": []signedBlob{carol.seen307(id, "empty")}})
	if r := row307(t, s, id); r.State != "pending" {
		t.Fatalf("a stranger must not count: %+v", r)
	}
	// bilal's note comes through a relay (ali carries it): closed, empty.
	sync220(t, s.api, s.ali, map[string]any{"seen": []signedBlob{bilal.seen307(id, "empty")}})
	r := row307(t, s, id)
	if r.State != "cancelled" || !r.Empty {
		t.Fatalf("every phone said empty -> cancelled + empty: %+v", r)
	}
	// The stop note travels like a cancel.
	out := sync220(t, s.api, s.ali, map[string]any{"have": []string{id}})
	if len(out.States) != 1 || !strings.Contains(out.States[0].Body, `"state":"cancelled"`) {
		t.Fatalf("stop note %v", out.States)
	}
	// Notes for no_trust are kept but never close anything; a cancelled
	// request that isn't empty says so.
	id2, _ := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})
	sync220(t, s.api, s.ali, map[string]any{"seen": []signedBlob{ali.seen307(id2, "no_trust"), bilal.seen307(id2, "no_trust")}})
	if r := row307(t, s, id2); r.State != "pending" {
		t.Fatalf("no_trust never closes: %+v", r)
	}
	cancel := call221(t, s.api.AdminActionCancel, http.MethodPost, "/x", id2, nil, owner221(s))
	_ = cancel
	if r := row307(t, s, id2); r.Empty {
		t.Fatalf("a normal cancel is not empty: %+v", r)
	}
}

// adminChatCard307 is a get_chat card made on the admin phone (offline).
func (s *blocks212) adminChatCard307(t *testing.T) (adminActionBody, signedBlob) {
	t.Helper()
	cert := s.cert
	body := adminActionBody{V: 1, Kind: "admin_action", ID: uuid.NewString(), Action: "get_chat", Seq: time.Now().UnixMilli(),
		At: nowStamp(), By: "test-admin", Signer: "admin", Cert: &cert, Need: 1,
		To: []adminActionBox{{K: "abc", Box: "x"}, {K: "abd", Box: "y"}}, ReplyKey: "rk"}
	raw, _ := json.Marshal(body)
	return body, signedBlob{Body: string(raw), Sig: b64url(ed25519.Sign(s.phoneKey, raw))}
}

func TestOfflineRequestChatLabelAndFinalState307(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	bilal := addPhoneKeys220(t, s.api, s.bilal)
	c, card := s.adminChatCard307(t)
	// A relay brings the card first: listed, no chat yet.
	if out := sync220(t, s.api, s.bilal, map[string]any{"cards": []signedBlob{card}, "signing_key": bilal.signingKey()}); out.Applied != 1 {
		t.Fatalf("applied %d rejected %d", out.Applied, out.Rejected)
	}
	if r := row307(t, s, c.ID); r.Chat != nil || !r.Offline {
		t.Fatalf("before the label: %+v", r)
	}
	// Seen notes before the label are kept (targets unknown yet).
	sync220(t, s.api, s.ali, map[string]any{"signing_key": ali.signingKey(), "seen": []signedBlob{ali.seen307(c.ID, "empty")}})
	label := map[string]any{
		"id": c.ID, "user_id": "", "label": "ali212 & bilal212", "words": "Get chat \u00b7 Full chat",
		"devices": []string{s.ali.Device.ID, s.bilal.Device.ID},
		"detail": map[string]any{
			"chat": map[string]any{"kind": "direct", "users": []string{s.ali.Account.ID, s.bilal.Account.ID},
				"devices": []string{s.ali.Device.ID, s.bilal.Device.ID}},
			"from": "2026-11-01", "to": "2026-11-09", "plus_day": true,
		},
	}
	// From a stranger: ignored.
	sync220(t, s.api, s.bilal, map[string]any{"labels": []any{label}})
	if r := row307(t, s, c.ID); r.Chat != nil {
		t.Fatal("a label from a phone that is not the admin's is ignored")
	}
	sync220(t, s.api, s.adminDev, map[string]any{"labels": []any{label}})
	r := row307(t, s, c.ID)
	if r.Chat == nil || r.Chat.Kind != "direct" || len(r.Chat.Users) != 2 || r.From != "2026-11-01" || r.To != "2026-11-09" || r.Label != "ali212 & bilal212" {
		t.Fatalf("offline request chat: %+v", r)
	}
	var tj string
	_ = s.api.store.DB().QueryRow(`SELECT targets_json FROM admin_actions WHERE id=?`, c.ID).Scan(&tj)
	if !strings.Contains(tj, s.ali.Account.ID) || !strings.Contains(tj, s.bilal.Account.ID) {
		t.Fatalf("each phone gets its person: %s", tj)
	}
	// Final state from a stranger is ignored; from the admin phone it counts once.
	sync220(t, s.api, s.bilal, map[string]any{"closed": []any{map[string]any{"id": c.ID, "state": "cancelled", "empty": true}}})
	if r := row307(t, s, c.ID); r.State != "pending" || r.Empty {
		t.Fatalf("stranger closed it: %+v", r)
	}
	sync220(t, s.api, s.adminDev, map[string]any{"closed": []any{map[string]any{"id": c.ID, "state": "cancelled", "empty": true}}})
	if r := row307(t, s, c.ID); r.State != "cancelled" || !r.Empty {
		t.Fatalf("admin phone end state: %+v", r)
	}

	// Done from the admin phone.
	c2, card2 := s.adminChatCard307(t)
	sync220(t, s.api, s.adminDev, map[string]any{"cards": []signedBlob{card2}})
	sync220(t, s.api, s.adminDev, map[string]any{"closed": []any{map[string]any{"id": c2.ID, "state": "done"}}})
	if r := row307(t, s, c2.ID); r.State != "done" || r.Empty {
		t.Fatalf("done: %+v", r)
	}
	// Wrong words are refused.
	if s.api.applyClosed307(s.adminDev.Device.ID, adminClosed307{ID: c2.ID, State: "deleted"}) {
		t.Fatal("unknown state")
	}
}

func TestSeenNotesBeforeLabelCloseOnLabel307(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	bilal := addPhoneKeys220(t, s.api, s.bilal)
	c, card := s.adminChatCard307(t)
	sync220(t, s.api, s.bilal, map[string]any{"cards": []signedBlob{card}, "signing_key": bilal.signingKey()})
	sync220(t, s.api, s.ali, map[string]any{"signing_key": ali.signingKey(), "seen": []signedBlob{ali.seen307(c.ID, "empty")}})
	sync220(t, s.api, s.bilal, map[string]any{"seen": []signedBlob{bilal.seen307(c.ID, "empty")}})
	if r := row307(t, s, c.ID); r.State != "pending" {
		t.Fatalf("targets unknown before the label: %+v", r)
	}
	label := map[string]any{"id": c.ID, "label": "ali212 & bilal212", "devices": []string{s.ali.Device.ID, s.bilal.Device.ID},
		"detail": map[string]any{"chat": map[string]any{"kind": "direct", "devices": []string{s.ali.Device.ID, s.bilal.Device.ID}}}}
	sync220(t, s.api, s.adminDev, map[string]any{"labels": []any{label}})
	if r := row307(t, s, c.ID); r.State != "cancelled" || !r.Empty {
		t.Fatalf("closes when the label comes: %+v", r)
	}
}
