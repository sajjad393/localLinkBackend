package httpapi

import (
	"bytes"
	"compress/gzip"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bundle 221: the Owner gets chats FROM THE PHONES (the server is only a
// postman now). A get_chat card goes to every phone (220 delivery); a phone
// of the chat builds the answer once: messages in parts, each part locked
// on its own for the reply key, so half an answer can already be read.
// Parts travel to the server and over Wi-Fi Direct / mesh; the server's
// signed "answer_ack" tells carriers what the admin side already has.

const (
	chatAnswerAppBuild     = 132
	answerPartMaxBytes     = 64 << 10 // one signed part (32 KB data + JSON)
	answerMaxParts         = 2000
	answerServerMaxBytes   = 20 << 20 // a chat answer kept by the server
	answerFileMaxBytes     = 72 << 20 // a full photo / video: 50 MB as base64 parts in boxes (about x1.4)
	answerKeepAfterOpen    = 24 * time.Hour
	answerKeepUnopened     = 30 * 24 * time.Hour
	answerBoxKeySetting    = "answer_box_key"
	answerFetchMaxBytes    = 8 << 20
	chatRequestDetailLimit = 2000
)

var chatActions = map[string]bool{"get_chat": true, "get_file": true, "delete_completely": true}

func init() {
	for a := range chatActions {
		adminActionNames[a] = true
	}
}

// chatRef says which chat (inside a box; the server and the phones agree).
type chatRef struct {
	Kind    string   `json:"kind"` // direct | group
	Users   []string `json:"users,omitempty"`
	Devices []string `json:"devices,omitempty"`
	GroupID string   `json:"group_id,omitempty"`
}

type answerPartBody struct {
	V        int    `json:"v"`
	Kind     string `json:"kind"`
	AnswerID string `json:"answer_id"`
	ActionID string `json:"action_id"`
	Device   string `json:"device"`
	Part     int    `json:"part"`
	Of       int    `json:"of"`
	Sha256   string `json:"sha256"`
	Data     string `json:"data"`
}

type answerAckBody struct {
	V        int    `json:"v"`
	Kind     string `json:"kind"`
	AnswerID string `json:"answer_id"`
	ActionID string `json:"action_id"`
	Have     string `json:"have"`
	Of       int    `json:"of"`
	All      bool   `json:"all"`
	At       string `json:"at"`
}

// ------------------------------------------------------------ server key

// answerBoxKey is the server's X25519 key that web requests are answered to.
func (a *API) answerBoxKey() ([]byte, string, error) {
	load := func() ([]byte, error) {
		var v string
		if err := a.store.DB().QueryRow(`SELECT value FROM admin_settings WHERE key=?`, answerBoxKeySetting).Scan(&v); err != nil {
			return nil, err
		}
		raw, err := unb64url(v)
		if err != nil || len(raw) != 32 {
			return nil, errors.New("answer key is damaged")
		}
		return raw, nil
	}
	priv, err := load()
	if errors.Is(err, sql.ErrNoRows) {
		seed := make([]byte, 32)
		if _, err := rand.Read(seed); err != nil {
			return nil, "", err
		}
		_, _ = a.store.DB().Exec(`INSERT OR IGNORE INTO admin_settings(key,value,updated_at) VALUES(?,?,?)`, answerBoxKeySetting, b64url(seed), nowStamp())
		priv, err = load()
	}
	if err != nil {
		return nil, "", err
	}
	k, err := ecdh.X25519().NewPrivateKey(priv)
	if err != nil {
		return nil, "", err
	}
	return priv, b64url(k.PublicKey().Bytes()), nil
}

// ---------------------------------------------------------- making cards

// makeChatCard is like makeServerAction (220) with a reply key and need.
func (a *API) makeChatCard(action string, admin AdminAccount, label, words, parent string, targets []adminActionTarget, need int, replyKey string) (adminActionBody, error) {
	body := adminActionBody{V: 1, Kind: "admin_action", ID: uuid.NewString(), Action: action,
		Seq: time.Now().UnixMilli(), At: nowStamp(), By: admin.Username, Signer: "server",
		To: []adminActionBox{}, Parent: parent, ReplyKey: replyKey}
	out := make([]adminActionTarget, 0, len(targets))
	for _, t := range targets {
		if key := a.identityKeyFor(t.Device); key != "" {
			plain, _ := json.Marshal(adminActionBoxPlain{Device: t.Device, User: t.User, Detail: t.Detail})
			if box, err := lockBox(key, plain); err == nil {
				body.To = append(body.To, adminActionBox{K: strings.ReplaceAll(uuid.NewString(), "-", "")[:12], Box: box})
				t.Boxed = true
			}
		}
		out = append(out, t)
	}
	sort.Slice(body.To, func(i, j int) bool { return body.To[i].K < body.To[j].K })
	if len(body.To) == 0 {
		return body, errors.New("no phone of this chat can be reached (no keys yet)")
	}
	body.Need = need
	if need <= 0 || need > len(body.To) {
		body.Need = len(body.To)
	}
	raw, _ := json.Marshal(body)
	if len(raw) > maxAdminActionCardBytes {
		return body, errors.New("too many phones in this chat for one request")
	}
	blob, err := a.serverSign(string(raw))
	if err != nil {
		return body, err
	}
	tj, _ := json.Marshal(out)
	if len(words) > chatRequestDetailLimit {
		words = words[:chatRequestDetailLimit]
	}
	if _, err := a.store.DB().Exec(`INSERT INTO admin_actions(id,action,body,sig,signer,made_by,made_at,need,done,state,parent,target_label,targets_json,detail,offline)
		VALUES(?,?,?,?,'server',?,?,?,0,'pending',?,?,?,?,0)`,
		body.ID, action, blob.Body, blob.Sig, admin.Username, body.At, body.Need, parent, label, string(tj), words); err != nil {
		return body, err
	}
	a.adminAudit(admin.ID, "admin_"+action, label, "card="+body.ID+" phones="+strconv.Itoa(len(body.To))+" "+words)
	a.pruneAdminActions()
	a.hintAdminActions()
	return body, nil
}

// resolveUserID accepts a user id or a device id (the chat list has devices).
func (a *API) resolveUserID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	var n int
	_ = a.store.DB().QueryRow(`SELECT COUNT(*) FROM users WHERE id=?`, id).Scan(&n)
	if n > 0 {
		return id
	}
	u, _ := a.deviceUser(id)
	return u
}

// allPhonesOf: every phone of a person the server knows (an old phone may
// still hold the chat).
func (a *API) allPhonesOf(userID string) []string {
	out := []string{}
	rows, err := a.store.DB().Query(`SELECT id FROM devices WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return out
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	rows.Close()
	return out
}

func (a *API) groupDevices(groupID string) []string {
	out := []string{}
	rows, err := a.store.DB().Query(`SELECT device_id FROM group_members WHERE group_id=? ORDER BY device_id`, groupID)
	if err != nil {
		return out
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	rows.Close()
	return out
}

type chatPickIn struct {
	Kind    string   `json:"kind"`
	UserA   string   `json:"user_a"`
	UserB   string   `json:"user_b"`
	GroupID string   `json:"group_id"`
	Range   string   `json:"range"` // full | 7 | 30 | dates
	From    string   `json:"from"`  // YYYY-MM-DD
	To      string   `json:"to"`
	Scope   string   `json:"scope"` // purge: message | full | range
	IDs     []string `json:"ids"`
}

type chatPick struct {
	ref     chatRef
	label   string
	targets []adminActionTarget
}

func (a *API) pickChat(in chatPickIn) (chatPick, string) {
	var p chatPick
	switch in.Kind {
	case "direct", "":
		ua, ub := a.resolveUserID(in.UserA), a.resolveUserID(in.UserB)
		if ua == "" || ub == "" || ua == ub {
			return p, "pick two different people"
		}
		p.ref = chatRef{Kind: "direct", Users: []string{ua, ub}}
		names := []string{}
		for _, u := range []string{ua, ub} {
			n := a.usernameOf(u)
			if n == "" {
				n = u
			}
			names = append(names, n)
			for _, d := range a.allPhonesOf(u) {
				p.ref.Devices = append(p.ref.Devices, d)
				p.targets = append(p.targets, adminActionTarget{Device: d, User: u})
			}
		}
		p.label = names[0] + " & " + names[1]
	case "group":
		gid := strings.TrimSpace(in.GroupID)
		var name string
		if gid == "" || a.store.DB().QueryRow(`SELECT COALESCE(name,'') FROM groups WHERE id=?`, gid).Scan(&name) != nil {
			return p, "group not found"
		}
		p.ref = chatRef{Kind: "group", GroupID: gid}
		for _, d := range a.groupDevices(gid) {
			u, _ := a.deviceUser(d)
			p.targets = append(p.targets, adminActionTarget{Device: d, User: u})
		}
		p.label = name
	default:
		return p, "kind must be direct or group"
	}
	if len(p.targets) == 0 {
		return p, "no phones in this chat"
	}
	return p, ""
}

func dayOf(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return "", false
	}
	return t.Format("2006-01-02"), true
}

// chatRange turns the request range into from/to days and words.
func chatRange(in chatPickIn, now time.Time) (string, string, string, bool) {
	switch in.Range {
	case "", "full":
		return "", "", "Full chat", true
	case "7", "30":
		days, _ := strconv.Atoi(in.Range)
		return now.AddDate(0, 0, -days).Format("2006-01-02"), now.Format("2006-01-02"), "Last " + in.Range + " days", true
	case "dates":
		from, ok1 := dayOf(in.From)
		to, ok2 := dayOf(in.To)
		if !ok1 || !ok2 || from == "" || to == "" || from > to {
			return "", "", "", false
		}
		return from, to, from + " - " + to, true
	}
	return "", "", "", false
}

func (a *API) requireOwnerChat(w http.ResponseWriter, r *http.Request) (AdminAccount, bool) {
	admin, ok := a.requireAdminPermission(w, r, conversationPermission)
	if !ok {
		return admin, false
	}
	if admin.Role != "owner" {
		writeError(w, http.StatusForbidden, "only the Owner can do this")
		return admin, false
	}
	return admin, true
}

// AdminChatRequestCreate: POST /api/v1/admin/chat-requests
func (a *API) AdminChatRequestCreate(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireOwnerChat(w, r)
	if !ok {
		return
	}
	var in chatPickIn
	if err := decodeJSON(r, &in, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pick, why := a.pickChat(in)
	if why != "" {
		writeError(w, http.StatusBadRequest, why)
		return
	}
	from, to, words, ok := chatRange(in, time.Now())
	if !ok {
		writeError(w, http.StatusBadRequest, "pick a valid date range")
		return
	}
	_, replyKey, err := a.answerBoxKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server key error")
		return
	}
	detail := map[string]any{"chat": pick.ref, "from": from, "to": to, "plus_day": true}
	for i := range pick.targets {
		pick.targets[i].Detail = detail
	}
	card, err := a.makeChatCard("get_chat", admin, pick.label, "Get chat \u00b7 "+words, "", pick.targets, 1, replyKey)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	a.rememberChat(card.ID, pick.ref, from, to)
	writeJSON(w, http.StatusCreated, map[string]any{"id": card.ID, "phones": len(card.To), "label": pick.label, "words": words})
}

// rememberChat keeps which chat a web request is about (the boxes are
// locked), so the Conversations list can show its status on the right row.
func (a *API) rememberChat(actionID string, ref chatRef, from, to string) {
	raw, _ := json.Marshal(ref)
	_, _ = a.store.DB().Exec(`INSERT OR REPLACE INTO admin_chat_request_meta(action_id,chat,from_day,to_day) VALUES(?,?,?,?)`, actionID, string(raw), from, to)
}

func (a *API) chatOf(actionID string) (*chatRef, string, string) {
	var raw, from, to string
	if a.store.DB().QueryRow(`SELECT chat,from_day,to_day FROM admin_chat_request_meta WHERE action_id=?`, actionID).Scan(&raw, &from, &to) != nil {
		return nil, "", ""
	}
	var ref chatRef
	if json.Unmarshal([]byte(raw), &ref) != nil {
		return nil, from, to
	}
	return &ref, from, to
}

// ---------------------------------------------------------- answer parts

func partRanges(parts []int) string {
	sort.Ints(parts)
	var b strings.Builder
	for i := 0; i < len(parts); {
		j := i
		for j+1 < len(parts) && parts[j+1] == parts[j]+1 {
			j++
		}
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		if i == j {
			b.WriteString(strconv.Itoa(parts[i]))
		} else {
			b.WriteString(strconv.Itoa(parts[i]) + "-" + strconv.Itoa(parts[j]))
		}
		i = j + 1
	}
	return b.String()
}

func (a *API) answerParts(answerID string) []int {
	out := []int{}
	rows, err := a.store.DB().Query(`SELECT part FROM admin_action_answers WHERE answer_id=? ORDER BY part`, answerID)
	if err != nil {
		return out
	}
	for rows.Next() {
		var p int
		if rows.Scan(&p) == nil {
			out = append(out, p)
		}
	}
	rows.Close()
	return out
}

// answerAck is the server-signed "the admin side has these parts".
func (a *API) answerAck(answerID string) (signedBlob, bool) {
	var actionID string
	var of int
	var complete sql.NullString
	if a.store.DB().QueryRow(`SELECT action_id,of,complete_at FROM admin_action_answer_meta WHERE answer_id=?`, answerID).Scan(&actionID, &of, &complete) != nil {
		return signedBlob{}, false
	}
	parts := a.answerParts(answerID)
	all := complete.Valid || (of > 0 && len(parts) >= of)
	have := partRanges(parts)
	if all {
		have = "1-" + strconv.Itoa(of)
	}
	raw, _ := json.Marshal(answerAckBody{V: 1, Kind: "answer_ack", AnswerID: answerID, ActionID: actionID, Have: have, Of: of, All: all, At: nowStamp()})
	blob, err := a.serverSign(string(raw))
	return blob, err == nil
}

func (a *API) answerBytes(answerID string) int64 {
	var n sql.NullInt64
	_ = a.store.DB().QueryRow(`SELECT SUM(LENGTH(body)) FROM admin_action_answers WHERE answer_id=?`, answerID).Scan(&n)
	return n.Int64
}

// takeAnswerPart checks and keeps one part. uploader = the phone sending it.
func (a *API) takeAnswerPart(blob signedBlob, uploader string) (string, string) {
	if len(blob.Body) > answerPartMaxBytes {
		return "", "too_big"
	}
	var p answerPartBody
	if json.Unmarshal([]byte(blob.Body), &p) != nil || p.V != 1 || p.Kind != "admin_answer_part" {
		return "", "bad_json"
	}
	if p.AnswerID == "" || p.ActionID == "" || p.Device == "" || p.Of < 1 || p.Of > answerMaxParts || p.Part < 1 || p.Part > p.Of {
		return "", "bad_fields"
	}
	sum := sha256.Sum256([]byte(p.Data))
	if hex.EncodeToString(sum[:]) != p.Sha256 {
		return p.AnswerID, "bad_sha"
	}
	var key string
	_ = a.store.DB().QueryRow(`SELECT signing_key FROM device_signing_keys WHERE device_id=?`, p.Device).Scan(&key)
	switch {
	case key != "":
		if !verifyWithKey(key, blob) {
			return p.AnswerID, "bad_sig"
		}
	case uploader != p.Device:
		return p.AnswerID, "unknown_key" // a carrier brought it: wait for the phone's key
	}
	var action string
	if a.store.DB().QueryRow(`SELECT action FROM admin_actions WHERE id=?`, p.ActionID).Scan(&action) == nil && !chatActions[action] {
		return p.AnswerID, "not_chat"
	}
	limit := int64(answerServerMaxBytes)
	if action == "get_file" {
		limit = answerFileMaxBytes
	}
	if a.answerBytes(p.AnswerID)+int64(len(blob.Body)) > limit {
		return p.AnswerID, "answer_full"
	}
	_, _ = a.store.DB().Exec(`INSERT INTO admin_action_answer_meta(answer_id,action_id,device_id,of,made_at) VALUES(?,?,?,?,?) ON CONFLICT(answer_id) DO NOTHING`,
		p.AnswerID, p.ActionID, p.Device, p.Of, nowStamp())
	if _, err := a.store.DB().Exec(`INSERT OR IGNORE INTO admin_action_answers(answer_id,action_id,part,of,sha256,body,sig,from_device,at) VALUES(?,?,?,?,?,?,?,?,?)`,
		p.AnswerID, p.ActionID, p.Part, p.Of, p.Sha256, blob.Body, blob.Sig, uploader, nowStamp()); err != nil {
		return p.AnswerID, "db"
	}
	var of int
	var complete sql.NullString
	_ = a.store.DB().QueryRow(`SELECT of,complete_at FROM admin_action_answer_meta WHERE answer_id=?`, p.AnswerID).Scan(&of, &complete)
	if !complete.Valid && len(a.answerParts(p.AnswerID)) >= of {
		_, _ = a.store.DB().Exec(`UPDATE admin_action_answer_meta SET complete_at=? WHERE answer_id=?`, nowStamp(), p.AnswerID)
		a.adminAudit("", "admin_chat_answer_complete", p.ActionID, "answer="+p.AnswerID+" parts="+strconv.Itoa(of)+" from="+p.Device)
	}
	return p.AnswerID, ""
}

type answersIn struct {
	Parts      []signedBlob `json:"parts"`
	Have       []string     `json:"have"` // answer ids the phone carries
	SigningKey string       `json:"signing_key"`
}

// AdminActionAnswers: POST /api/v1/admin-actions/answers (any signed-in
// phone: the target uploads its parts, carriers upload what they carry).
func (a *API) AdminActionAnswers(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	var in answersIn
	if err := decodeJSON(r, &in, 8<<20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	a.rememberSigningKey(deviceID, in.SigningKey)
	if len(in.Parts) > 200 {
		in.Parts = in.Parts[:200]
	}
	touched := map[string]bool{}
	rejected := map[string]int{}
	for _, b := range in.Parts {
		id, why := a.takeAnswerPart(b, deviceID)
		if id != "" {
			touched[id] = true
		}
		if why != "" {
			rejected[why]++
		}
	}
	for i, id := range in.Have {
		if i >= 500 {
			break
		}
		touched[id] = true
	}
	acks := []signedBlob{}
	for id := range touched {
		if ack, ok := a.answerAck(id); ok {
			acks = append(acks, ack)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"acks": acks, "rejected": rejected})
}

// adminPhoneOf is the phone whose admin cert signed an offline card.
func adminPhoneOf(body string) string {
	var c adminActionBody
	if json.Unmarshal([]byte(body), &c) != nil || c.Signer != "admin" || c.Cert == nil {
		return ""
	}
	var cert adminCertBody
	if json.Unmarshal([]byte(c.Cert.Body), &cert) != nil {
		return ""
	}
	return cert.DeviceID
}

type answersFetchIn struct {
	Have map[string]string `json:"have"` // answer id -> "1-40"
}

func parseRanges(s string) map[int]bool {
	out := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi := part, part
		if i := strings.IndexByte(part, '-'); i > 0 {
			lo, hi = part[:i], part[i+1:]
		}
		a, err1 := strconv.Atoi(lo)
		b, err2 := strconv.Atoi(hi)
		if err1 != nil || err2 != nil || b < a || b-a > answerMaxParts {
			continue
		}
		for x := a; x <= b; x++ {
			out[x] = true
		}
	}
	return out
}

// AdminActionAnswersFetch: POST /api/v1/admin-actions/answers/fetch - the
// admin's phone downloads the parts of answers to ITS offline cards (the
// server can't read them). When the phone has all parts the server copy goes.
func (a *API) AdminActionAnswersFetch(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := a.authorizeDevice(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	var in answersFetchIn
	if err := decodeJSON(r, &in, 1<<20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	type ans struct{ id, action string }
	var mine []ans
	rows, err := a.store.DB().Query(`SELECT m.answer_id, m.action_id, a.body FROM admin_action_answer_meta m JOIN admin_actions a ON a.id=m.action_id WHERE a.signer='admin' AND m.closed_at IS NULL`)
	if err == nil {
		for rows.Next() {
			var x ans
			var body string
			if rows.Scan(&x.id, &x.action, &body) == nil && adminPhoneOf(body) == deviceID {
				mine = append(mine, x)
			}
		}
		rows.Close()
	}
	parts := []signedBlob{}
	size := 0
	more := false
	for _, x := range mine {
		have := parseRanges(in.Have[x.id])
		var of int
		_ = a.store.DB().QueryRow(`SELECT of FROM admin_action_answer_meta WHERE answer_id=?`, x.id).Scan(&of)
		if of > 0 && len(have) >= of {
			// The admin's phone has it all: the server copy is not needed.
			_, _ = a.store.DB().Exec(`DELETE FROM admin_action_answers WHERE answer_id=?`, x.id)
			_, _ = a.store.DB().Exec(`UPDATE admin_action_answer_meta SET closed_at=? WHERE answer_id=?`, nowStamp(), x.id)
			continue
		}
		prow, err := a.store.DB().Query(`SELECT part, body, sig FROM admin_action_answers WHERE answer_id=? ORDER BY part`, x.id)
		if err != nil {
			continue
		}
		for prow.Next() {
			var n int
			var b signedBlob
			if prow.Scan(&n, &b.Body, &b.Sig) != nil || have[n] {
				continue
			}
			if size+len(b.Body) > answerFetchMaxBytes {
				more = true
				break
			}
			size += len(b.Body)
			parts = append(parts, b)
		}
		prow.Close()
		if more {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"parts": parts, "more": more})
}

// ------------------------------------------------------- reading (server)

type chatAnswerPlain struct {
	Part     int              `json:"part"`
	Of       int              `json:"of"`
	Chat     map[string]any   `json:"chat,omitempty"`
	From     string           `json:"from,omitempty"`
	To       string           `json:"to,omitempty"`
	Trimmed  bool             `json:"trimmed,omitempty"`
	Messages []map[string]any `json:"messages,omitempty"`
	File     map[string]any   `json:"file,omitempty"`
	Data     string           `json:"data,omitempty"`
}

func gunzipLimit(b []byte, limit int64) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(io.LimitReader(zr, limit))
}

// openAnswerPart opens one part locked for the server key.
func openAnswerPart(priv []byte, body string) (chatAnswerPlain, error) {
	var out chatAnswerPlain
	var p answerPartBody
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return out, err
	}
	raw, err := openBox(priv, p.Data)
	if err != nil {
		return out, err
	}
	plain, err := gunzipLimit(raw, 64<<20)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(plain, &out); err != nil {
		return out, err
	}
	out.Part, out.Of = p.Part, p.Of
	return out, nil
}

// bestAnswer is the answer of a card with the most parts.
func (a *API) bestAnswer(actionID string) (string, string, int) {
	var id, device string
	var of int
	_ = a.store.DB().QueryRow(`SELECT m.answer_id, m.device_id, m.of FROM admin_action_answer_meta m
		WHERE m.action_id=? AND m.closed_at IS NULL
		ORDER BY (SELECT COUNT(*) FROM admin_action_answers x WHERE x.answer_id=m.answer_id) DESC, m.made_at ASC LIMIT 1`, actionID).Scan(&id, &device, &of)
	return id, device, of
}

func (a *API) chatCard(w http.ResponseWriter, id string) (string, string, bool) {
	var action, body string
	if a.store.DB().QueryRow(`SELECT action, body FROM admin_actions WHERE id=?`, id).Scan(&action, &body) != nil || !chatActions[action] {
		writeError(w, http.StatusNotFound, "request not found")
		return "", "", false
	}
	return action, body, true
}

// AdminChatRequestAnswer: GET /api/v1/admin/chat-requests/{id}/answer -
// what has arrived so far (half an answer is readable).
func (a *API) AdminChatRequestAnswer(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireOwnerChat(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	action, body, ok := a.chatCard(w, id)
	if !ok {
		return
	}
	var card adminActionBody
	_ = json.Unmarshal([]byte(body), &card)
	priv, pub, err := a.answerBoxKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server key error")
		return
	}
	if card.ReplyKey != pub {
		writeErrorCode(w, http.StatusConflict, "answer_for_admin_phone", "This request was made on the admin's phone; the answer opens there.")
		return
	}
	answerID, device, of := a.bestAnswer(id)
	parts := []int{}
	if answerID != "" {
		parts = a.answerParts(answerID)
	}
	_, deviceName := a.deviceUser(device)
	resp := map[string]any{"id": id, "action": action, "answer_id": answerID, "from_device": device,
		"from_name": deviceName, "of": of, "have": partRanges(parts), "parts": len(parts),
		"complete": of > 0 && len(parts) >= of, "messages": []map[string]any{}}
	if ref, from, to := a.chatOf(id); ref != nil {
		resp["chat"], resp["from"], resp["to"] = ref, from, to
	}
	if answerID == "" {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	var opened sql.NullString
	_ = a.store.DB().QueryRow(`SELECT first_opened_at FROM admin_action_answer_meta WHERE answer_id=?`, answerID).Scan(&opened)
	if !opened.Valid {
		_, _ = a.store.DB().Exec(`UPDATE admin_action_answer_meta SET first_opened_at=? WHERE answer_id=?`, nowStamp(), answerID)
		a.adminAudit(admin.ID, "admin_chat_opened", id, "answer="+answerID+" parts="+strconv.Itoa(len(parts))+"/"+strconv.Itoa(of))
	}
	rows, err := a.store.DB().Query(`SELECT body FROM admin_action_answers WHERE answer_id=? ORDER BY part`, answerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read answer")
		return
	}
	var bodies []string
	for rows.Next() {
		var b string
		if rows.Scan(&b) == nil {
			bodies = append(bodies, b)
		}
	}
	rows.Close()
	messages := []map[string]any{}
	var fileMeta map[string]any
	var fileData bytes.Buffer
	broken := 0
	for _, b := range bodies {
		p, err := openAnswerPart(priv, b)
		if err != nil {
			broken++
			continue
		}
		if p.Part == 1 {
			if _, known := resp["chat"]; !known {
				if p.Chat != nil {
					resp["chat"] = p.Chat
				}
				resp["from"], resp["to"] = p.From, p.To
			}
			resp["trimmed"] = p.Trimmed
		}
		if p.File != nil {
			fileMeta = p.File
		}
		messages = append(messages, p.Messages...)
		if p.Data != "" {
			chunk, err := unb64url(p.Data)
			if err == nil {
				fileData.Write(chunk)
			}
		}
	}
	sort.SliceStable(messages, func(i, j int) bool {
		return strings.Compare(str(messages[i]["created_at"]), str(messages[j]["created_at"])) < 0
	})
	resp["messages"] = messages
	resp["broken"] = broken
	if action == "get_file" {
		resp["file"] = fileMeta
		if r.URL.Query().Get("download") == "1" && resp["complete"] == true && fileMeta != nil {
			ct := str(fileMeta["type"])
			if ct == "" {
				ct = "application/octet-stream"
			}
			w.Header().Set("Content-Type", ct)
			w.Header().Set("Content-Disposition", contentDisposition("inline", str(fileMeta["name"])))
			_, _ = w.Write(fileData.Bytes())
			return
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// AdminChatRequestClose: POST /api/v1/admin/chat-requests/{id}/close - the
// server copy goes now (else 24 h after the first open).
func (a *API) AdminChatRequestClose(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireOwnerChat(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, _, ok := a.chatCard(w, id); !ok {
		return
	}
	res, _ := a.store.DB().Exec(`DELETE FROM admin_action_answers WHERE action_id=?`, id)
	n, _ := res.RowsAffected()
	_, _ = a.store.DB().Exec(`UPDATE admin_action_answer_meta SET closed_at=? WHERE action_id=? AND closed_at IS NULL`, nowStamp(), id)
	a.adminAudit(admin.ID, "admin_chat_closed", id, "parts_deleted="+strconv.FormatInt(n, 10))
	writeJSON(w, http.StatusOK, map[string]any{"closed": true, "parts_deleted": n})
}

// AdminChatRequestFile: POST /api/v1/admin/chat-requests/{id}/file - the
// full photo / video from a phone of that chat.
func (a *API) AdminChatRequestFile(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireOwnerChat(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var label, tj, action string
	if a.store.DB().QueryRow(`SELECT action, target_label, targets_json FROM admin_actions WHERE id=?`, id).Scan(&action, &label, &tj) != nil || action != "get_chat" {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	var in struct {
		FileID    string `json:"file_id"`
		MessageID string `json:"message_id"`
		Name      string `json:"name"`
	}
	if err := decodeJSON(r, &in, 16<<10); err != nil || strings.TrimSpace(in.FileID) == "" {
		writeError(w, http.StatusBadRequest, "file_id is required")
		return
	}
	targets := []adminActionTarget{}
	_ = json.Unmarshal([]byte(tj), &targets)
	detail := map[string]any{"file_id": in.FileID, "message_id": in.MessageID}
	for i := range targets {
		targets[i].Detail = detail
	}
	_, replyKey, err := a.answerBoxKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server key error")
		return
	}
	words := "Get full photo"
	if in.Name != "" {
		words += " \u00b7 " + in.Name
	}
	card, err := a.makeChatCard("get_file", admin, label, words, id, targets, 1, replyKey)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	ref, _, _ := a.chatOf(id)
	if ref == nil {
		ref = &chatRef{}
	}
	a.rememberChat(card.ID, *ref, "", "")
	_, _ = a.store.DB().Exec(`UPDATE admin_chat_request_meta SET file_id=? WHERE action_id=?`, in.FileID, card.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"id": card.ID})
}

type chatRequestRow struct {
	ID       string   `json:"id"`
	Action   string   `json:"action"`
	Label    string   `json:"label"`
	Words    string   `json:"words"`
	State    string   `json:"state"`
	MadeAt   string   `json:"made_at"`
	MadeBy   string   `json:"made_by"`
	Parent   string   `json:"parent,omitempty"`
	Done     int      `json:"done"`
	Need     int      `json:"need"`
	Parts    int      `json:"parts"`
	Of       int      `json:"of"`
	FromName string   `json:"from_name,omitempty"`
	OldApp   int      `json:"old_app"`
	Offline  bool     `json:"offline"`
	Opened   bool     `json:"opened"`
	Closed   bool     `json:"closed"`
	Chat     *chatRef `json:"chat,omitempty"`
	FileID   string   `json:"file_id,omitempty"`
	From     string   `json:"from"`
	To       string   `json:"to"`
}

// AdminChatRequestList: GET /api/v1/admin/chat-requests
func (a *API) AdminChatRequestList(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireOwnerChat(w, r); !ok {
		return
	}
	rows, err := a.store.DB().Query(`SELECT id,action,target_label,detail,state,made_at,made_by,parent,done,need,offline,targets_json
		FROM admin_actions WHERE action IN ('get_chat','get_file','delete_completely') ORDER BY made_at DESC LIMIT 200`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list requests")
		return
	}
	type raw struct {
		row chatRequestRow
		tj  string
	}
	var list []raw
	for rows.Next() {
		var x raw
		var offline int
		if rows.Scan(&x.row.ID, &x.row.Action, &x.row.Label, &x.row.Words, &x.row.State, &x.row.MadeAt, &x.row.MadeBy, &x.row.Parent, &x.row.Done, &x.row.Need, &offline, &x.tj) == nil {
			x.row.Offline = offline == 1
			list = append(list, x)
		}
	}
	rows.Close()
	out := make([]chatRequestRow, 0, len(list))
	for _, x := range list {
		row := x.row
		row.Chat, row.From, row.To = a.chatOf(row.ID)
		_ = a.store.DB().QueryRow(`SELECT file_id FROM admin_chat_request_meta WHERE action_id=?`, row.ID).Scan(&row.FileID)
		if answerID, device, of := a.bestAnswer(row.ID); answerID != "" {
			row.Of = of
			row.Parts = len(a.answerParts(answerID))
			_, row.FromName = a.deviceUser(device)
			var opened, closed sql.NullString
			_ = a.store.DB().QueryRow(`SELECT first_opened_at, closed_at FROM admin_action_answer_meta WHERE answer_id=?`, answerID).Scan(&opened, &closed)
			row.Opened, row.Closed = opened.Valid, closed.Valid
		}
		targets := []adminActionTarget{}
		_ = json.Unmarshal([]byte(x.tj), &targets)
		if row.State == "pending" {
			for _, t := range targets {
				var build int64
				_ = a.store.DB().QueryRow(`SELECT build FROM device_builds WHERE device_id=?`, t.Device).Scan(&build)
				if build > 0 && build < chatAnswerAppBuild {
					row.OldApp++
				}
			}
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

// ------------------------------------------------------ delete completely

// AdminPurge: POST /api/v1/admin/purge - the server deletes its own copies
// now and sends a delete_completely card to every phone of the chat.
func (a *API) AdminPurge(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireOwnerChat(w, r)
	if !ok {
		return
	}
	var in chatPickIn
	if err := decodeJSON(r, &in, 1<<20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pick, why := a.pickChat(in)
	if why != "" {
		writeError(w, http.StatusBadRequest, why)
		return
	}
	detail := map[string]any{"chat": pick.ref, "scope": in.Scope}
	words := ""
	var from, to string
	switch in.Scope {
	case "message":
		if len(in.IDs) == 0 || len(in.IDs) > 500 {
			writeError(w, http.StatusBadRequest, "pick 1 to 500 messages")
			return
		}
		detail["ids"] = in.IDs
		words = strconv.Itoa(len(in.IDs)) + " message(s)"
	case "full":
		words = "Full chat"
	case "range":
		var ok bool
		var rw string
		from, to, rw, ok = chatRange(chatPickIn{Range: in.Range, From: in.From, To: in.To}, time.Now())
		if !ok || from == "" {
			writeError(w, http.StatusBadRequest, "pick a valid date range")
			return
		}
		detail["from"], detail["to"] = from, to
		words = rw
	default:
		writeError(w, http.StatusBadRequest, "scope must be message, full or range")
		return
	}
	removed, err := a.purgeServerCopies(pick.ref, in.Scope, in.IDs, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete server copies")
		return
	}
	a.adminAudit(admin.ID, "admin_purge", pick.label, "scope="+in.Scope+" "+words+" server_removed="+strconv.Itoa(removed))
	for i := range pick.targets {
		pick.targets[i].Detail = detail
	}
	card, err := a.makeChatCard("delete_completely", admin, pick.label, "Delete completely \u00b7 "+words, "", pick.targets, 0, "")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"removed_on_server": removed, "need": 0, "note": err.Error()})
		return
	}
	a.rememberChat(card.ID, pick.ref, from, to)
	writeJSON(w, http.StatusCreated, map[string]any{"id": card.ID, "removed_on_server": removed, "need": card.Need})
}

// purgeServerCopies deletes the server's rows of a chat (in batches).
func (a *API) purgeServerCopies(ref chatRef, scope string, ids []string, from, to string) (int, error) {
	var q string
	var args []any
	lo, hi := "", ""
	if scope == "range" {
		f, _ := time.Parse("2006-01-02", from)
		t, _ := time.Parse("2006-01-02", to)
		lo, hi = postmanTime(f.AddDate(0, 0, -1)), postmanTime(t.AddDate(0, 0, 2))
	}
	if ref.Kind == "group" {
		q, args = `SELECT id FROM group_messages WHERE group_id=?`, []any{ref.GroupID}
	} else {
		if len(ref.Devices) == 0 {
			return 0, nil
		}
		in, dargs := postmanIn(ref.Devices)
		q = `SELECT id FROM messages WHERE sender_id IN (` + in + `) AND recipient_id IN (` + in + `) AND sender_id <> recipient_id`
		args = append(append(args, dargs...), dargs...)
	}
	switch scope {
	case "message":
		in, iargs := postmanIn(ids)
		q += ` AND id IN (` + in + `)`
		args = append(args, iargs...)
	case "range":
		q += ` AND created_at >= ? AND created_at < ?`
		args = append(args, lo, hi)
	}
	all, err := a.postmanIDs(q, args...)
	if err != nil {
		return 0, err
	}
	pairs := map[[2]string]int{}
	if ref.Kind != "group" {
		// Only rows between the two people (not each person's other chats).
		users := map[string]string{}
		for _, d := range ref.Devices {
			users[d], _ = a.deviceUser(d)
		}
		keep := all[:0]
		for _, id := range all {
			var s, r string
			if a.store.DB().QueryRow(`SELECT sender_id, recipient_id FROM messages WHERE id=?`, id).Scan(&s, &r) == nil && users[s] != users[r] {
				keep = append(keep, id)
				if s > r {
					s, r = r, s
				}
				pairs[[2]string{s, r}]++
			}
		}
		all = keep
	}
	removed := 0
	for i := 0; i < len(all); i += postmanBatch {
		end := i + postmanBatch
		if end > len(all) {
			end = len(all)
		}
		batch := all[i:end]
		if ref.Kind == "group" {
			_, err = a.postmanDeleteGroup(batch)
		} else {
			_, err = a.postmanDeleteDirect(batch)
		}
		if err != nil {
			return removed, err
		}
		removed += len(batch)
	}
	if removed > 0 {
		if ref.Kind == "group" {
			_, _ = a.store.DB().Exec(`UPDATE conv_index SET count=MAX(count-?,0) WHERE kind='group' AND group_id=?`, removed, ref.GroupID)
		} else {
			for p, n := range pairs {
				_, _ = a.store.DB().Exec(`UPDATE conv_index SET count=MAX(count-?,0) WHERE kind='direct' AND a=? AND b=?`, n, p[0], p[1])
			}
		}
	}
	return removed, nil
}

// CleanupAnswers221: answer copies go 24 h after the first open, at Close,
// or 30 days after they arrived when nobody opened them.
func (a *API) CleanupAnswers221(now time.Time) int {
	open := postmanTime(now.Add(-answerKeepAfterOpen))
	old := postmanTime(now.Add(-answerKeepUnopened))
	ids, err := a.postmanIDs(`SELECT answer_id FROM admin_action_answer_meta
		WHERE closed_at IS NOT NULL OR (first_opened_at IS NOT NULL AND first_opened_at <= ?) OR (first_opened_at IS NULL AND made_at <> '' AND made_at <= ?)`, open, old)
	if err != nil || len(ids) == 0 {
		return 0
	}
	n := 0
	for _, id := range ids {
		res, err := a.store.DB().Exec(`DELETE FROM admin_action_answers WHERE answer_id=?`, id)
		if err == nil {
			c, _ := res.RowsAffected()
			n += int(c)
		}
		_, _ = a.store.DB().Exec(`UPDATE admin_action_answer_meta SET closed_at=COALESCE(closed_at,?) WHERE answer_id=?`, postmanTime(now), id)
	}
	return n
}
