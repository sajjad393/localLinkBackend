package httpapi

import "strings"

// Bundle 317 (G5): the reason code of a refused request ("file_gone"), read
// from its verified receipts (result "refused:<why>"); "" = not refused.
func (a *API) refused317(actionID string) string {
	var result string
	_ = a.store.DB().QueryRow(`SELECT result FROM admin_action_receipts
		WHERE action_id=? AND verified=1 AND result LIKE 'refused:%' ORDER BY at ASC LIMIT 1`, actionID).Scan(&result)
	return strings.TrimPrefix(result, "refused:")
}
