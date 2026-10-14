package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func registerAuditDevice(t *testing.T, a *API, id string) string {
	t.Helper()
	res := doJSON(t, a.RegisterDevice, http.MethodPost, "/api/v1/devices", map[string]any{
		"id": id, "name": id, "platform": "android",
	}, nil)
	if res.Code != http.StatusCreated {
		t.Fatalf("register %s: %d %s", id, res.Code, res.Body.String())
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Token
}

// Group payload crypto was retired by migration 54 ("retire-group-payload-crypto").
// This test locks in the plaintext group flow that replaces the old envelope
// epoch flow: owner creates a group, posts a plaintext message, members can
// read it, outsiders cannot.
func TestGroupPlaintextFlowAfterCryptoRetirement(t *testing.T) {
	_, a := newTestAPI(t)
	tokenA := registerAuditDevice(t, a, "a")
	tokenB := registerAuditDevice(t, a, "b")
	tokenC := registerAuditDevice(t, a, "c")
	headersA := map[string]string{"Authorization": "Bearer " + tokenA, "X-Device-ID": "a", "Content-Type": "application/json"}
	headersB := map[string]string{"Authorization": "Bearer " + tokenB, "X-Device-ID": "b"}
	headersC := map[string]string{"Authorization": "Bearer " + tokenC, "X-Device-ID": "c"}

	created := doJSON(t, a.CreateGroup, http.MethodPost, "/api/v1/groups", map[string]any{"name": "Plain", "member_ids": []string{"b"}}, headersA)
	if created.Code != http.StatusCreated {
		t.Fatalf("create group: %d %s", created.Code, created.Body.String())
	}
	var group Group
	if err := json.Unmarshal(created.Body.Bytes(), &group); err != nil {
		t.Fatal(err)
	}

	sent := doJSON(t, a.CreateGroupMessage, http.MethodPost, "/api/v1/group-messages", map[string]any{
		"id": "retire-1", "group_id": group.ID, "body": "plaintext hello", "created_at": "2026-09-20T00:00:00Z",
	}, headersA)
	if sent.Code != http.StatusCreated {
		t.Fatalf("group message: %d %s", sent.Code, sent.Body.String())
	}

	listed := doJSON(t, a.ListGroupMessages, http.MethodGet, "/api/v1/group-messages?group_id="+group.ID, nil, headersB)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "plaintext hello") {
		t.Fatalf("member could not read group history: %d %s", listed.Code, listed.Body.String())
	}

	outsider := doJSON(t, a.ListGroupMessages, http.MethodGet, "/api/v1/group-messages?group_id="+group.ID, nil, headersC)
	if outsider.Code == http.StatusOK && strings.Contains(outsider.Body.String(), "plaintext hello") {
		t.Fatalf("non-member could read group history: %d %s", outsider.Code, outsider.Body.String())
	}
}

// The retired key-envelope schema must stay retired once migration 54 has run,
// on both fresh installs and upgraded databases.
func TestGroupKeyEnvelopeSchemaStaysRetired(t *testing.T) {
	st, a := newTestAPI(t)
	_ = a
	var name string
	err := st.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='group_key_envelopes'`).Scan(&name)
	if err == nil {
		t.Fatalf("group_key_envelopes table must not exist after crypto retirement")
	}
}
