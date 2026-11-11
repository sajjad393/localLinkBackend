package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

type people267 struct {
	Profiles []Profile `json:"profiles"`
	More     bool      `json:"more"`
}

func listPeople267(t *testing.T, a *API, h map[string]string, query string) people267 {
	t.Helper()
	res := doJSON(t, a.DirectoryPeople, http.MethodGet, "/api/v1/directory/people"+query, nil, h)
	if res.Code != http.StatusOK {
		t.Fatalf("people: %d %s", res.Code, res.Body.String())
	}
	var out people267
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func ids267(p people267) map[string]bool {
	m := map[string]bool{}
	for _, x := range p.Profiles {
		m[x.UserID] = true
	}
	return m
}

func TestDirectoryPeople267ExcludesSelfContactsAndHidden(t *testing.T) {
	_, a := newTestAPI(t)
	me := registerDirectoryUser(t, a, "peopleme", "people-me")
	friend := registerDirectoryUser(t, a, "peoplefriend", "people-friend")
	other := registerDirectoryUser(t, a, "peopleother", "people-other")
	hidden := registerDirectoryUser(t, a, "peoplehidden", "people-hidden")
	h := directoryHeaders(me)

	if res := doJSON(t, a.UpdateProfile, http.MethodPut, "/api/v1/profile", map[string]any{
		"display_name": "Hidden", "username": "peoplehidden", "discoverable_by_name": false, "directory_sync_enabled": true,
	}, directoryHeaders(hidden)); res.Code != http.StatusOK {
		t.Fatalf("hide: %d %s", res.Code, res.Body.String())
	}
	if res := doJSON(t, a.AddAccountContact, http.MethodPost, "/api/v1/account/contacts", map[string]any{"user_id": friend.Account.ID}, h); res.Code >= 300 {
		t.Fatalf("add contact: %d %s", res.Code, res.Body.String())
	}

	got := ids267(listPeople267(t, a, h, ""))
	if got[me.Account.ID] || got[friend.Account.ID] || got[hidden.Account.ID] {
		t.Fatalf("self / contact / hidden listed: %v", got)
	}
	if !got[other.Account.ID] {
		t.Fatalf("other not listed: %v", got)
	}
	// The friend sees nobody they already have (the add is both ways).
	fg := ids267(listPeople267(t, a, directoryHeaders(friend), ""))
	if fg[me.Account.ID] || fg[friend.Account.ID] || !fg[other.Account.ID] {
		t.Fatalf("friend list wrong: %v", fg)
	}
}

func TestDirectoryPeople267Paging(t *testing.T) {
	_, a := newTestAPI(t)
	me := registerDirectoryUser(t, a, "pagingme", "paging-me")
	for i := 0; i < 4; i++ { // 5 sign-ups per test is the rate limit
		registerDirectoryUser(t, a, fmt.Sprintf("paging%02d", i), fmt.Sprintf("paging-%02d", i))
	}
	h := directoryHeaders(me)
	first := listPeople267(t, a, h, "?limit=3")
	if len(first.Profiles) != 3 || !first.More {
		t.Fatalf("first page: %d more=%v", len(first.Profiles), first.More)
	}
	second := listPeople267(t, a, h, "?limit=3&offset=3")
	if len(second.Profiles) != 1 || second.More {
		t.Fatalf("second page: %d more=%v", len(second.Profiles), second.More)
	}
	seen := ids267(first)
	for id := range ids267(second) {
		if seen[id] {
			t.Fatalf("page overlap: %s", id)
		}
	}
	all := listPeople267(t, a, h, "?limit=500")
	if len(all.Profiles) != 4 || all.More {
		t.Fatalf("limit capped list: %d more=%v", len(all.Profiles), all.More)
	}
}

func TestDirectoryPeople267NeedsLogin(t *testing.T) {
	_, a := newTestAPI(t)
	res := doJSON(t, a.DirectoryPeople, http.MethodGet, "/api/v1/directory/people", nil, nil)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", res.Code)
	}
}
