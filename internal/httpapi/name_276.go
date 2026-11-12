package httpapi

import (
	"log"
	"strings"
)

// Bundle 276: a name the phone gave itself ("My Android Phone (1a2b3c4d)",
// "My LinkUp phone", ...) is never shown as a person's name. One rule for the
// web, the admin answers and the group lists the phones read:
// display name -> username (no @) -> "LinkUp user".
// Same list as the app's isDefaultPhoneLabel255.

const linkUpUser276 = "LinkUp user"

func isDefaultPhoneName276(s string) bool {
	n := strings.ToLower(strings.TrimSpace(s))
	if n == "" {
		return true
	}
	return n == "my linkup phone" || n == "phone" || n == "linkup user" || strings.HasPrefix(n, "my android phone")
}

// personName276: display name, else username, else "LinkUp user".
func personName276(display, username string) string {
	if d := strings.TrimSpace(display); !isDefaultPhoneName276(d) {
		return d
	}
	if u := strings.TrimSpace(username); u != "" {
		return u
	}
	return linkUpUser276
}

// phoneLabel276: the phone's own name when a person gave it one, else
// "<person>'s phone", else "Phone".
func phoneLabel276(phone, person string) string {
	if p := strings.TrimSpace(phone); !isDefaultPhoneName276(p) {
		return p
	}
	if o := strings.TrimSpace(person); o != "" && o != linkUpUser276 {
		return o + "'s phone"
	}
	return "Phone"
}

// okNameSQL276: true in SQL when column x holds a real (not default) name.
func okNameSQL276(x string) string {
	return `(TRIM(COALESCE(` + x + `,''))<>'' AND LOWER(TRIM(` + x + `)) NOT IN ('my linkup phone','phone','linkup user') AND LOWER(TRIM(` + x + `)) NOT LIKE 'my android phone%')`
}

// fixDefaultProfileNames276 (once at start, safe to repeat): a profile whose
// name is a default phone name gets the username. A real name stays.
func (a *API) fixDefaultProfileNames276() {
	if a == nil || a.store == nil {
		return
	}
	res, err := a.store.DB().Exec(`UPDATE profiles SET display_name=COALESCE((SELECT u.username FROM users u WHERE u.id=profiles.user_id),display_name)
		WHERE NOT ` + okNameSQL276("display_name") + ` AND EXISTS(SELECT 1 FROM users u WHERE u.id=profiles.user_id AND TRIM(COALESCE(u.username,''))<>'')`)
	if err != nil {
		log.Printf("NAME_FIX_276 err=%v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("NAME_FIX_276 n=%d", n)
	}
}
