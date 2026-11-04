package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Bundle 204: a friend's picture can be read; a stranger's cannot; Remove photo.
func TestFriendAvatarAndRemove204(t *testing.T) {
	st, a := newTestAPI(t)
	owner := registerDirectoryUser(t, a, "avatarowner", "avatar-owner")
	friend := registerDirectoryUser(t, a, "avatarfriend", "avatar-friend")
	stranger := registerDirectoryUser(t, a, "avatarstranger", "avatar-stranger")

	dir := filepath.Join(a.fileRoot, "profiles", owner.Account.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	img := []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3, 4}
	if err := os.WriteFile(filepath.Join(dir, "avatar.jpg"), img, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO profiles(user_id,display_name,avatar_path,avatar_content_type,avatar_size,avatar_sha256,avatar_updated_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET avatar_path=excluded.avatar_path,avatar_content_type=excluded.avatar_content_type,avatar_size=excluded.avatar_size,avatar_sha256=excluded.avatar_sha256`,
		owner.Account.ID, "Owner", "profiles/"+owner.Account.ID+"/avatar.jpg", "image/jpeg", len(img), "abc123", "2026-11-04T00:00:00Z", "2026-11-04T00:00:00Z", "2026-11-04T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO account_contacts(user_id,contact_user_id,added_at) VALUES(?,?,'2026-11-04T00:00:00Z')`, friend.Account.ID, owner.Account.ID); err != nil {
		t.Fatal(err)
	}

	get := func(auth AuthResponse, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/directory/user/"+id+"/avatar", nil)
		req.SetPathValue("id", id)
		for k, v := range directoryHeaders(auth) {
			req.Header.Set(k, v)
		}
		res := httptest.NewRecorder()
		a.FriendAvatar(res, req)
		return res
	}

	if res := get(friend, owner.Account.ID); res.Code != http.StatusOK || res.Body.Len() != len(img) || res.Header().Get("X-Avatar-SHA256") != "abc123" {
		t.Fatalf("friend: %d len=%d sha=%q", res.Code, res.Body.Len(), res.Header().Get("X-Avatar-SHA256"))
	}
	if res := get(stranger, owner.Account.ID); res.Code != http.StatusNotFound {
		t.Fatalf("stranger must get 404, got %d", res.Code)
	}
	if res := get(owner, owner.Account.ID); res.Code != http.StatusOK {
		t.Fatalf("own picture: %d", res.Code)
	}
	noAuth := httptest.NewRequest(http.MethodGet, "/api/v1/directory/user/x/avatar", nil)
	noAuth.SetPathValue("id", "x")
	rec := httptest.NewRecorder()
	a.FriendAvatar(rec, noAuth)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no login: %d", rec.Code)
	}

	del := doJSON(t, a.DeleteProfileAvatar, http.MethodDelete, "/api/v1/profile/avatar", nil, directoryHeaders(owner))
	if del.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", del.Code, del.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "avatar.jpg")); !os.IsNotExist(err) {
		t.Fatalf("file must be gone: %v", err)
	}
	if res := get(friend, owner.Account.ID); res.Code != http.StatusNotFound {
		t.Fatalf("after remove: %d", res.Code)
	}
}
