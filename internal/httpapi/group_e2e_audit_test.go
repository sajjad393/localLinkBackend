package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
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

func TestGroupKeyEnvelopeIsRecipientScopedAndCiphertextOnly(t *testing.T) {
	_, a := newTestAPI(t)
	tokenA := registerAuditDevice(t, a, "a")
	tokenB := registerAuditDevice(t, a, "b")
	tokenC := registerAuditDevice(t, a, "c")
	headersA := map[string]string{"Authorization": "Bearer " + tokenA, "X-Device-ID": "a", "Content-Type": "application/json"}
	headersB := map[string]string{"Authorization": "Bearer " + tokenB, "X-Device-ID": "b"}
	headersC := map[string]string{"Authorization": "Bearer " + tokenC, "X-Device-ID": "c"}

	created := doJSON(t, a.CreateGroup, http.MethodPost, "/api/v1/groups", map[string]any{"name": "E2E", "member_ids": []string{"b"}}, headersA)
	if created.Code != http.StatusCreated {
		t.Fatalf("create group: %d %s", created.Code, created.Body.String())
	}
	var group Group
	if err := json.Unmarshal(created.Body.Bytes(), &group); err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"group_id":    group.ID,
		"key_version": 1,
		"envelopes":   []map[string]string{{"recipient_id": "b", "envelope": "gke:v1:{\"ciphertext\":\"ciphertext-only\"}"}},
	}
	stored := doJSON(t, a.DistributeGroupKeyEnvelopes, http.MethodPost, "/api/v1/groups/keys", body, headersA)
	if stored.Code != http.StatusCreated {
		t.Fatalf("store envelope: %d %s", stored.Code, stored.Body.String())
	}

	listed := doJSON(t, a.ListGroupKeyEnvelopes, http.MethodGet, "/api/v1/groups/keys?group_id="+group.ID, nil, headersB)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "gke:v1:") || !strings.Contains(listed.Body.String(), "ciphertext-only") {
		t.Fatalf("recipient could not retrieve ciphertext envelope: %d %s", listed.Code, listed.Body.String())
	}
	if strings.Contains(listed.Body.String(), "plaintext-group-key") {
		t.Fatal("plaintext group key leaked through key endpoint")
	}

	outsider := doJSON(t, a.ListGroupKeyEnvelopes, http.MethodGet, "/api/v1/groups/keys?group_id="+group.ID, nil, headersC)
	if outsider.Code != http.StatusForbidden {
		t.Fatalf("outsider expected forbidden, got %d", outsider.Code)
	}

	conflict := doJSON(t, a.DistributeGroupKeyEnvelopes, http.MethodPost, "/api/v1/groups/keys", map[string]any{
		"group_id": group.ID, "key_version": 1, "envelopes": []map[string]string{{"recipient_id": "b", "envelope": "gke:v1:second"}},
	}, headersA)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("duplicate key version expected conflict, got %d", conflict.Code)
	}

	removed := doJSON(t, a.RemoveGroupMember, http.MethodDelete, "/api/v1/groups/members?group_id="+group.ID+"&device_id=b", nil, headersA)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove member: %d %s", removed.Code, removed.Body.String())
	}
	revoked := doJSON(t, a.ListGroupKeyEnvelopes, http.MethodGet, "/api/v1/groups/keys?group_id="+group.ID, nil, headersB)
	if revoked.Code != http.StatusForbidden {
		t.Fatalf("removed member expected forbidden, got %d", revoked.Code)
	}
}

func TestGroupMessagesRequireE2EEnvelopeAndAuthenticatedCreatedAt(t *testing.T) {
	_, a := newTestAPI(t)
	tokenA := registerAuditDevice(t, a, "a")
	tokenB := registerAuditDevice(t, a, "b")
	headersA := map[string]string{"Authorization": "Bearer " + tokenA, "X-Device-ID": "a", "Content-Type": "application/json"}
	_ = tokenB
	created := doJSON(t, a.CreateGroup, http.MethodPost, "/api/v1/groups", map[string]any{"name": "E2E", "member_ids": []string{"b"}}, headersA)
	var group Group
	if err := json.Unmarshal(created.Body.Bytes(), &group); err != nil {
		t.Fatal(err)
	}
	key := doJSON(t, a.DistributeGroupKeyEnvelopes, http.MethodPost, "/api/v1/groups/keys", map[string]any{
		"group_id": group.ID, "key_version": 1, "envelopes": []map[string]string{{"recipient_id": "b", "envelope": "gke:v1:{\"ciphertext\":\"test\"}"}},
	}, headersA)
	if key.Code != http.StatusCreated {
		t.Fatalf("initial group key distribution: %d %s", key.Code, key.Body.String())
	}

	plaintext := doJSON(t, a.CreateGroupMessage, http.MethodPost, "/api/v1/group-messages", map[string]any{
		"id": "g-plain", "group_id": group.ID, "body": "hello", "created_at": "2026-09-20T00:00:00Z",
	}, headersA)
	if plaintext.Code != http.StatusBadRequest {
		t.Fatalf("plaintext group body should be rejected, got %d", plaintext.Code)
	}

	valid := doJSON(t, a.CreateGroupMessage, http.MethodPost, "/api/v1/group-messages", map[string]any{
		"id": "g-valid", "group_id": group.ID, "body": `gme:v1:{"version":1,"type":"group_message","key_version":1}`, "created_at": "2026-09-20T00:00:00+00:00",
	}, headersA)
	if valid.Code != http.StatusCreated {
		t.Fatalf("encrypted group body should be accepted: %d %s", valid.Code, valid.Body.String())
	}
	if !strings.Contains(valid.Body.String(), `"created_at":"2026-09-20T00:00:00Z"`) {
		t.Fatalf("created_at was not normalized: %s", valid.Body.String())
	}
}

func TestGroupMembershipChangeBlocksStaleEpochAndRejoinCannotReuseRevokedEnvelope(t *testing.T) {
	_, a := newTestAPI(t)
	tokenA := registerAuditDevice(t, a, "a")
	_ = registerAuditDevice(t, a, "b")
	_ = registerAuditDevice(t, a, "c")
	headersA := map[string]string{"Authorization": "Bearer " + tokenA, "X-Device-ID": "a", "Content-Type": "application/json"}
	created := doJSON(t, a.CreateGroup, http.MethodPost, "/api/v1/groups", map[string]any{"name": "Epoch", "member_ids": []string{"b"}}, headersA)
	if created.Code != http.StatusCreated {
		t.Fatalf("create group: %d %s", created.Code, created.Body.String())
	}
	var group Group
	if err := json.Unmarshal(created.Body.Bytes(), &group); err != nil {
		t.Fatal(err)
	}
	fakeEnvelope := func(recipient, suffix string) map[string]string {
		return map[string]string{"recipient_id": recipient, "envelope": "gke:v1:{\"ciphertext\":\"" + suffix + "\"}"}
	}
	distribute := func(version int, envelopes ...map[string]string) int {
		res := doJSON(t, a.DistributeGroupKeyEnvelopes, http.MethodPost, "/api/v1/groups/keys", map[string]any{"group_id": group.ID, "key_version": version, "envelopes": envelopes}, headersA)
		return res.Code
	}
	if code := distribute(1, fakeEnvelope("b", "v1b")); code != http.StatusCreated {
		t.Fatalf("initial key distribution: %d", code)
	}
	message := func(id string, version int) int {
		res := doJSON(t, a.CreateGroupMessage, http.MethodPost, "/api/v1/group-messages", map[string]any{
			"id": id, "group_id": group.ID, "body": `gme:v1:{"version":1,"type":"group_message","key_version":` + strconv.Itoa(version) + `}`, "created_at": "2026-09-20T00:00:00Z",
		}, headersA)
		return res.Code
	}
	if code := message("epoch-1", 1); code != http.StatusCreated {
		t.Fatalf("v1 message expected created, got %d", code)
	}
	added := doJSON(t, a.AddGroupMember, http.MethodPost, "/api/v1/groups/members?group_id="+group.ID, map[string]any{"device_id": "c"}, headersA)
	if added.Code != http.StatusCreated {
		t.Fatalf("add member: %d %s", added.Code, added.Body.String())
	}
	if code := message("stale-1", 1); code != http.StatusConflict {
		t.Fatalf("stale v1 message after add expected conflict, got %d", code)
	}
	if code := distribute(2, fakeEnvelope("b", "v2b"), fakeEnvelope("c", "v2c")); code != http.StatusCreated {
		t.Fatalf("v2 distribution: %d", code)
	}
	if code := message("epoch-2", 2); code != http.StatusCreated {
		t.Fatalf("v2 message expected created, got %d", code)
	}
	removed := doJSON(t, a.RemoveGroupMember, http.MethodDelete, "/api/v1/groups/members?group_id="+group.ID+"&device_id=b", nil, headersA)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove member: %d %s", removed.Code, removed.Body.String())
	}
	if code := message("stale-2", 2); code != http.StatusConflict {
		t.Fatalf("stale v2 message after remove expected conflict, got %d", code)
	}
	var oldEnvelopeCount int
	if err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM group_key_envelopes WHERE group_id=? AND recipient_device_id='b'`, group.ID).Scan(&oldEnvelopeCount); err != nil {
		t.Fatal(err)
	}
	if oldEnvelopeCount != 0 {
		t.Fatalf("removed member retained %d key envelopes", oldEnvelopeCount)
	}
	if code := distribute(3, fakeEnvelope("c", "v3c")); code != http.StatusCreated {
		t.Fatalf("v3 distribution: %d", code)
	}
	if code := message("epoch-3", 3); code != http.StatusCreated {
		t.Fatalf("v3 message expected created, got %d", code)
	}
	readded := doJSON(t, a.AddGroupMember, http.MethodPost, "/api/v1/groups/members?group_id="+group.ID, map[string]any{"device_id": "b"}, headersA)
	if readded.Code != http.StatusCreated {
		t.Fatalf("re-add member: %d %s", readded.Code, readded.Body.String())
	}
	var rejoinOld int
	if err := a.store.DB().QueryRow(`SELECT COUNT(*) FROM group_key_envelopes WHERE group_id=? AND recipient_device_id='b' AND key_version<4`, group.ID).Scan(&rejoinOld); err != nil {
		t.Fatal(err)
	}
	if rejoinOld != 0 {
		t.Fatalf("rejoined member regained %d old key envelopes", rejoinOld)
	}
}
