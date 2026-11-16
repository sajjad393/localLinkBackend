package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Bundle 318 (TODO_318 K4/K5): "Get new messages only" from the Owner
// phone. The server passes `since` to the phones and the new-only answer
// facts back; a request without `since` (the web) is unchanged.

func TestNewOnlyRequestPassesSince318(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	_, card := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID,
		"since": "2026-11-15T05:05:00Z", "since_n": 4, "since_h": "0123456789abcdef"})
	plain := openMine220(t, ali, card)
	if plain.Detail["since"] != "2026-11-15T05:05:00Z" || plain.Detail["since_n"] != float64(4) ||
		plain.Detail["since_h"] != "0123456789abcdef" || plain.Detail["plus_day"] != true {
		t.Fatalf("detail %v", plain.Detail)
	}
}

func TestFullRequestHasNoSinceAndBadSinceIsDropped318(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	serverGroup314(t, s, "g1", "Site", s.ali.Device.ID, s.bilal.Device.ID)
	for i, in := range []map[string]any{
		{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID},
		{"kind": "group", "group_id": "g1", "since": "yesterday", "since_n": 3, "since_h": "x"},
	} {
		id, card := newChatRequest221(t, s, in)
		plain := openMine220(t, ali, card)
		for _, k := range []string{"since", "since_n", "since_h"} {
			if _, has := plain.Detail[k]; has {
				t.Fatalf("%d: %s must not travel: %v", i, k, plain.Detail)
			}
		}
		if _, err := s.api.store.DB().Exec(`UPDATE admin_actions SET state='done' WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewOnlyAnswerFactsReachTheOwner318(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	sync220(t, s.api, s.ali, map[string]any{"signing_key": ali.signingKey()})
	id, card := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID,
		"since": "2026-11-15T05:05:00Z", "since_n": 1, "since_h": "0123456789abcdef"})
	ans := id + "." + s.ali.Device.ID
	p1 := part221(t, ali, card.ReplyKey, ans, id, 1, 1, map[string]any{"chat": map[string]any{"kind": "direct"},
		"since": "2026-11-15T05:05:00Z", "days": map[string]any{"2026-11-14": 3, "2026-11-15": 1}, "up_to_date": true})
	upload221(t, s, s.ali, p1)
	res := call221(t, s.api.AdminChatRequestAnswer, http.MethodGet, "/x", id, nil, owner221(s))
	var out struct {
		Since    string         `json:"since"`
		Days     map[string]int `json:"days"`
		UpToDate bool           `json:"up_to_date"`
		Complete bool           `json:"complete"`
		Messages []any          `json:"messages"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if res.Code != 200 || !out.Complete || !out.UpToDate || out.Since != "2026-11-15T05:05:00Z" ||
		out.Days["2026-11-14"] != 3 || out.Days["2026-11-15"] != 1 || len(out.Messages) != 0 {
		t.Fatalf("%d %s", res.Code, res.Body.String())
	}
}

func TestFullAnswerHasNoNewOnlyFacts318(t *testing.T) {
	s := setup212(t)
	ali := addPhoneKeys220(t, s.api, s.ali)
	addPhoneKeys220(t, s.api, s.bilal)
	sync220(t, s.api, s.ali, map[string]any{"signing_key": ali.signingKey()})
	id, card := newChatRequest221(t, s, map[string]any{"kind": "direct", "user_a": s.ali.Account.ID, "user_b": s.bilal.Account.ID})
	ans := id + "." + s.ali.Device.ID
	p1 := part221(t, ali, card.ReplyKey, ans, id, 1, 1, map[string]any{"chat": map[string]any{"kind": "direct"},
		"messages": []any{map[string]any{"id": "m1", "created_at": "2026-11-01T10:00:00Z", "body": "hi"}}})
	upload221(t, s, s.ali, p1)
	res := call221(t, s.api.AdminChatRequestAnswer, http.MethodGet, "/x", id, nil, owner221(s))
	out := map[string]any{}
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	for _, k := range []string{"since", "days", "up_to_date"} {
		if _, has := out[k]; has {
			t.Fatalf("%s in a full answer: %s", k, res.Body.String())
		}
	}
}
