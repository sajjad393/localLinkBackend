package httpapi

import (
	"database/sql"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Bundle 221: the server is a postman. A message stays only until it is
// delivered (+7 days); undelivered ones go after 60 days. The chat list
// (conv_index) is never touched, so the Owner can still pick old chats and
// ask the phones for them.

const (
	postmanKeepDelivered   = 7 * 24 * time.Hour
	postmanKeepUndelivered = 60 * 24 * time.Hour
	postmanBatch           = 500
	postmanVacuumKey       = "postman_last_vacuum"
	postmanVacuumMinBytes  = 50 << 20
	postmanVacuumMinShare  = 0.20
	postmanVacuumHour      = 3
)

// PostmanResult is what one clean-up removed.
type PostmanResult struct {
	Direct int
	Group  int
	Files  int
	Freed  int64 // bytes of files removed from disk
}

func postmanTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// postmanIDs reads one batch of ids and closes the rows before returning
// (one open connection: never query while rows are open).
func (a *API) postmanIDs(query string, args ...any) ([]string, error) {
	rows, err := a.store.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	return ids, err
}

func postmanIn(ids []string) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}

// postmanFiles returns the file ids attached to these messages.
func (a *API) postmanFiles(table, column string, ids []string) ([]string, error) {
	in, args := postmanIn(ids)
	return a.postmanIDs(`SELECT DISTINCT file_id FROM `+table+` WHERE `+column+` IN (`+in+`)`, args...)
}

// postmanDropFiles removes files that no message uses any more (a file
// shared by another message stays).
func (a *API) postmanDropFiles(fileIDs []string) (int, int64, error) {
	if len(fileIDs) == 0 {
		return 0, 0, nil
	}
	type dead struct {
		id, path, thumb string
		size            int64
	}
	var list []dead
	in, args := postmanIn(fileIDs)
	rows, err := a.store.DB().Query(`
SELECT f.id, f.storage_path, COALESCE(f.thumbnail_path,''), f.storage_size + f.thumbnail_storage_size
FROM files f WHERE f.id IN (`+in+`)
  AND NOT EXISTS (SELECT 1 FROM message_attachments ma WHERE ma.file_id=f.id)
  AND NOT EXISTS (SELECT 1 FROM group_message_attachments ga WHERE ga.file_id=f.id)`, args...)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var d dead
		if err := rows.Scan(&d.id, &d.path, &d.thumb, &d.size); err != nil {
			rows.Close()
			return 0, 0, err
		}
		list = append(list, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, 0, err
	}
	removed := 0
	var freed int64
	for _, d := range list {
		res, err := a.store.DB().Exec(`DELETE FROM files WHERE id=?
  AND NOT EXISTS (SELECT 1 FROM message_attachments WHERE file_id=?)
  AND NOT EXISTS (SELECT 1 FROM group_message_attachments WHERE file_id=?)`, d.id, d.id, d.id)
		if err != nil {
			return removed, freed, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		if d.path != "" {
			_ = os.Remove(d.path)
		}
		if d.thumb != "" {
			_ = os.Remove(d.thumb)
		}
		removed++
		freed += d.size
	}
	return removed, freed, nil
}

func (a *API) postmanDeleteDirect(ids []string) (int, error) {
	files, err := a.postmanFiles("message_attachments", "message_id", ids)
	if err != nil {
		return 0, err
	}
	in, args := postmanIn(ids)
	tx, err := a.store.DB().Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM message_attachments WHERE message_id IN (`+in+`)`, args...); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM sync_events WHERE event_type IN ('message','message_status') AND event_id IN (`+in+`)`, args...); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE id IN (`+in+`)`, args...); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	n, _, err := a.postmanDropFiles(files)
	return n, err
}

func (a *API) postmanDeleteGroup(ids []string) (int, error) {
	files, err := a.postmanFiles("group_message_attachments", "group_message_id", ids)
	if err != nil {
		return 0, err
	}
	in, args := postmanIn(ids)
	tx, err := a.store.DB().Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{
		`DELETE FROM group_message_attachments WHERE group_message_id IN (` + in + `)`,
		`DELETE FROM group_receipts WHERE message_id IN (` + in + `)`,
		`DELETE FROM sync_events WHERE event_type='group_message' AND event_id IN (` + in + `)`,
		`DELETE FROM group_messages WHERE id IN (` + in + `)`,
	} {
		if _, err := tx.Exec(q, args...); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	n, _, err := a.postmanDropFiles(files)
	return n, err
}

// CleanupMessages221 removes delivered messages after 7 days and the rest
// after 60 days, in batches of 500 (short transactions).
func (a *API) CleanupMessages221(now time.Time) (PostmanResult, error) {
	var out PostmanResult
	started := time.Now()
	week := postmanTime(now.Add(-postmanKeepDelivered))
	old := postmanTime(now.Add(-postmanKeepUndelivered))
	before := a.postmanFileBytes()
	for {
		ids, err := a.postmanIDs(`
SELECT id FROM messages
WHERE (delivered_at IS NOT NULL AND delivered_at <> '' AND delivered_at <= ?)
   OR ((delivered_at IS NULL OR delivered_at = '') AND created_at <= ?)
LIMIT ?`, week, old, postmanBatch)
		if err != nil {
			return out, err
		}
		if len(ids) == 0 {
			break
		}
		n, err := a.postmanDeleteDirect(ids)
		if err != nil {
			return out, err
		}
		out.Direct += len(ids)
		out.Files += n
		if len(ids) < postmanBatch {
			break
		}
	}
	for {
		// A group message goes 7 days after every member phone that got it
		// (a sync event) has a delivered receipt; at most 60 days.
		ids, err := a.postmanIDs(`
SELECT gm.id FROM group_messages gm
WHERE gm.created_at <= ?
   OR (gm.created_at <= ? AND NOT EXISTS (
        SELECT 1 FROM sync_events se
        WHERE se.event_type='group_message' AND se.event_id=gm.id AND se.device_id <> gm.sender_id
          AND NOT EXISTS (SELECT 1 FROM group_receipts r
                          WHERE r.message_id=gm.id AND r.device_id=se.device_id
                            AND r.delivered_at <> '' AND r.delivered_at <= ?)))
LIMIT ?`, old, week, week, postmanBatch)
		if err != nil {
			return out, err
		}
		if len(ids) == 0 {
			break
		}
		n, err := a.postmanDeleteGroup(ids)
		if err != nil {
			return out, err
		}
		out.Group += len(ids)
		out.Files += n
		if len(ids) < postmanBatch {
			break
		}
	}
	out.Freed = before - a.postmanFileBytes()
	if out.Freed < 0 {
		out.Freed = 0
	}
	if out.Direct+out.Group+out.Files > 0 {
		log.Printf("POSTMAN_CLEAN direct=%d group=%d files=%d freedMB=%d took=%dms",
			out.Direct, out.Group, out.Files, out.Freed>>20, time.Since(started).Milliseconds())
	}
	return out, nil
}

func (a *API) postmanFileBytes() int64 {
	var n sql.NullInt64
	_ = a.store.DB().QueryRow(`SELECT SUM(storage_size + thumbnail_storage_size) FROM files`).Scan(&n)
	return n.Int64
}

// postmanDBBytes = (total, free) bytes of the database file.
func (a *API) postmanDBBytes() (int64, int64) {
	var pages, free, size int64
	_ = a.store.DB().QueryRow(`PRAGMA page_count`).Scan(&pages)
	_ = a.store.DB().QueryRow(`PRAGMA freelist_count`).Scan(&free)
	_ = a.store.DB().QueryRow(`PRAGMA page_size`).Scan(&size)
	return pages * size, free * size
}

// postmanShouldVacuum: from 03:00 (server time), once a day, when the free
// space is at least 50 MB or 20 % of the file.
func (a *API) postmanShouldVacuum(now time.Time) bool {
	local := now.Local()
	if local.Hour() < postmanVacuumHour {
		return false
	}
	var last string
	_ = a.store.DB().QueryRow(`SELECT value FROM admin_settings WHERE key=?`, postmanVacuumKey).Scan(&last)
	if last == local.Format("2006-01-02") {
		return false
	}
	total, free := a.postmanDBBytes()
	if total <= 0 {
		return false
	}
	return free >= postmanVacuumMinBytes || float64(free) >= postmanVacuumMinShare*float64(total)
}

// PostmanTick runs the clean-up and, at night, VACUUM. VACUUM waits for a
// running backup (VACUUM INTO) because the store has one connection.
func (a *API) PostmanTick(now time.Time) (PostmanResult, bool, error) {
	res, err := a.CleanupMessages221(now)
	if err != nil {
		return res, false, err
	}
	if n := a.CleanupAnswers221(now); n > 0 {
		log.Printf("POSTMAN_ANSWERS parts=%d", n)
	}
	if !a.postmanShouldVacuum(now) {
		return res, false, nil
	}
	started := time.Now()
	before, _ := a.postmanDBBytes()
	if _, err := a.store.DB().Exec(`VACUUM`); err != nil {
		return res, false, err
	}
	after, _ := a.postmanDBBytes()
	_, _ = a.store.DB().Exec(`INSERT INTO admin_settings(key,value,updated_at) VALUES(?,?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		postmanVacuumKey, now.Local().Format("2006-01-02"), postmanTime(now))
	log.Printf("POSTMAN_VACUUM before=%sMB after=%sMB took=%ds", strconv.FormatInt(before>>20, 10),
		strconv.FormatInt(after>>20, 10), int(time.Since(started).Seconds()))
	return res, true, nil
}
