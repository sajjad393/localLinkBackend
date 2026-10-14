package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"locallink/backend/internal/store"
)

// doJSON accepts an optional header map so legacy tests with no headers remain
// valid while newer tests can authenticate requests explicitly.
func doJSON(t *testing.T, handler http.HandlerFunc, method, path string, body any, headers ...map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if strings.HasPrefix(req.URL.Path, "/api/v1/directory/user/") {
		req.SetPathValue("id", strings.TrimPrefix(req.URL.Path, "/api/v1/directory/user/"))
	}
	if len(headers) > 0 && headers[0] != nil {
		for k, v := range headers[0] {
			req.Header.Set(k, v)
		}
	}
	res := httptest.NewRecorder()
	handler(res, req)
	return res
}

func newTestAPI(t *testing.T) (*store.Store, *API) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "locallink.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, New(st)
}

func newTestStore(t *testing.T) (*store.Store, error) {
	t.Helper()
	return store.Open(t.TempDir() + "/locallink.db")
}
