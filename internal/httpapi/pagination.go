package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultListPageSize = 100
	maxListPageSize     = 500
)

type listCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"s"`
	Key     string `json:"k"`
	ID      string `json:"i"`
	Desc    bool   `json:"d"`
}

type pageParams struct {
	Limit  int
	Cursor *listCursor
}

type PageInfo struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

func parsePageParams(r *http.Request, scope string) (pageParams, error) {
	limit := defaultListPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxListPageSize {
			return pageParams{}, errors.New("limit must be between 1 and 500")
		}
		limit = n
	}
	rawCursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if rawCursor == "" {
		return pageParams{Limit: limit}, nil
	}
	if len(rawCursor) > 1024 {
		return pageParams{}, errors.New("invalid cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(rawCursor)
	if err != nil {
		return pageParams{}, errors.New("invalid cursor")
	}
	var c listCursor
	if err := json.Unmarshal(decoded, &c); err != nil || c.Version != 1 || c.Scope != scope || c.Key == "" || c.ID == "" || len(c.Key) > 256 || len(c.ID) > 128 {
		return pageParams{}, errors.New("invalid cursor")
	}
	return pageParams{Limit: limit, Cursor: &c}, nil
}

func encodeListCursor(scope, key, id string, desc bool) string {
	b, _ := json.Marshal(listCursor{Version: 1, Scope: scope, Key: key, ID: id, Desc: desc})
	return base64.RawURLEncoding.EncodeToString(b)
}
