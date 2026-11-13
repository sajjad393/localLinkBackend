package httpapi

import (
	"fmt"
	"html"
	"strings"
)

// Bundle 299 (B): the public download page at http://linkup.local/download
// (also http://<server ip>/download, :8080 and /linkup keep working).
// No login, no JavaScript, no outside files (phones on the server's Wi-Fi
// have no internet). Light and dark follow the phone. English + Urdu steps,
// like the phone-to-phone page (bundle 210).

type linkupPageData299 struct {
	Has      bool
	Version  string
	Build    int64
	FileName string
	Size     int64
	AddedAt  string // "14 Nov 2026"
	QRSVG    string // the page link as a QR (to show another phone)
	Link     string
	Short    string // "linkup.local/download" when the name answers, else ""
}

func sizeLabel299(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	}
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%d KB", (n+1023)/1024)
}

const linkupLogo299 = `<svg class="logo" viewBox="0 0 96 96" aria-hidden="true"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#2f7bf6"/><stop offset="1" stop-color="#22d3ee"/></linearGradient></defs><rect width="96" height="96" rx="26" fill="#0e1a33"/><circle cx="37" cy="48" r="15" fill="none" stroke="url(#g)" stroke-width="8.4"/><circle cx="59" cy="48" r="15" fill="none" stroke="url(#g)" stroke-width="8.4"/></svg>`

const linkupCSS299 = `*{box-sizing:border-box}
:root{--bg:#f4f6fb;--card:#fff;--text:#111827;--muted:#5b6472;--line:#e2e6ec;--step:#eef3ff;--warn:#fff7e6;--warnt:#7a4800}
@media (prefers-color-scheme:dark){:root{--bg:#0b1020;--card:#141a2a;--text:#e8edf4;--muted:#9ca6b5;--line:#263049;--step:#1a2440;--warn:#2a2210;--warnt:#fcd58a}}
body{margin:0;font-family:Roboto,system-ui,-apple-system,"Segoe UI",sans-serif;background:var(--bg);color:var(--text)}
.hero{background:radial-gradient(circle at 15% 10%,rgba(47,123,246,.45),transparent 55%),radial-gradient(circle at 90% 100%,rgba(34,211,238,.35),transparent 55%),linear-gradient(160deg,#0a1022,#0e1a33);color:#fff;padding:34px 20px 74px;text-align:center}
.logo{width:88px;height:88px;display:block;margin:0 auto;border-radius:26px;box-shadow:0 10px 30px rgba(47,123,246,.45)}
h1{font-size:30px;margin:14px 0 4px;letter-spacing:.2px}
.tag{margin:0;color:rgba(255,255,255,.74);font-size:15px}
.w{max-width:480px;margin:-52px auto 0;padding:0 16px 36px}
.card{background:var(--card);border:1px solid var(--line);border-radius:22px;padding:20px;box-shadow:0 10px 30px rgba(10,16,34,.10)}
.ver{display:flex;justify-content:center;gap:8px;flex-wrap:wrap;margin:0 0 14px}
.pill{font-size:13px;font-weight:600;padding:5px 11px;border-radius:99px;background:var(--step);color:var(--muted)}
.pill.new{background:rgba(18,161,80,.14);color:#12a150}
.btn{display:flex;align-items:center;justify-content:center;gap:10px;padding:17px;border-radius:30px;background:linear-gradient(90deg,#2f7bf6,#22b8e8);color:#fff;font-size:19px;font-weight:700;text-decoration:none;box-shadow:0 8px 22px rgba(47,123,246,.35)}
.btn svg{width:22px;height:22px;fill:none;stroke:#fff;stroke-width:2.4;stroke-linecap:round;stroke-linejoin:round}
.small{font-size:13px;color:var(--muted);text-align:center;margin:10px 0 0}
h2{font-size:17px;margin:24px 4px 10px}
.steps{list-style:none;margin:0;padding:0}
.steps li{display:flex;gap:12px;align-items:flex-start;background:var(--card);border:1px solid var(--line);border-radius:16px;padding:12px 14px;margin:8px 0}
.n{flex:none;width:28px;height:28px;border-radius:50%;background:var(--step);color:#2f7bf6;font-weight:700;display:grid;place-items:center;font-size:14px}
.steps b{display:block;font-size:15px}
.ur{direction:rtl;text-align:right;color:var(--muted);font-size:15px;line-height:1.7;margin-top:2px;font-family:"Noto Naskh Arabic","Noto Sans Arabic",sans-serif}
.note{background:var(--warn);color:var(--warnt);border-radius:14px;padding:12px 14px;margin-top:16px;font-size:14px;line-height:1.45}
.share{display:flex;gap:14px;align-items:center;margin-top:16px;background:var(--card);border:1px solid var(--line);border-radius:16px;padding:12px}
.qr{width:92px;height:92px;flex:none;background:#fff;border-radius:10px;padding:6px}
.qr svg{width:100%;height:100%;display:block}
.share b{display:block;font-size:14px}
.share code{font-size:13px;color:#2f7bf6;word-break:break-all}
.share code.big{font-size:17px;font-weight:700;font-family:inherit}
.none{text-align:center;padding:8px 4px}
.none b{display:block;font-size:18px;margin-bottom:6px}
.foot{text-align:center;color:var(--muted);font-size:12px;margin-top:22px}`

// linkupPage299 is the whole page (one string, served with no-store).
func linkupPage299(p linkupPageData299) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light dark"><title>Get LinkUp</title><style>`)
	b.WriteString(linkupCSS299)
	b.WriteString(`</style></head><body><div class="hero">`)
	b.WriteString(linkupLogo299)
	b.WriteString(`<h1>LinkUp</h1><p class="tag">Chats and calls on your own Wi-Fi. No internet needed.</p></div><div class="w"><div class="card">`)
	if !p.Has {
		b.WriteString(`<div class="none"><b>LinkUp is not on this server yet</b><span class="small">Ask the admin to add it, then open this page again.</span></div></div>`)
	} else {
		v := html.EscapeString(p.Version)
		b.WriteString(`<div class="ver"><span class="pill new">Version ` + v + `</span>`)
		if s := sizeLabel299(p.Size); s != "" {
			b.WriteString(`<span class="pill">` + s + `</span>`)
		}
		if p.AddedAt != "" {
			b.WriteString(`<span class="pill">` + html.EscapeString(p.AddedAt) + `</span>`)
		}
		b.WriteString(`</div><a class="btn" href="/download/` + html.EscapeString(p.FileName) + `" download><svg viewBox="0 0 24 24"><path d="M12 4v11M7.5 11 12 15.5 16.5 11"/><path d="M5 20h14"/></svg>Download LinkUp</a>`)
		b.WriteString(`<p class="small">Already have LinkUp? Installing this updates it. Your chats stay.</p></div>`)
		b.WriteString(`<h2>How to install</h2><ol class="steps">`)
		step := func(n, en, ur string) {
			b.WriteString(`<li><span class="n">` + n + `</span><div><b>` + en + `</b><div class="ur">` + ur + `</div></div></li>`)
		}
		step("1", "Tap Download LinkUp", "Download LinkUp پر ٹیپ کریں")
		step("2", "Open the file when it finishes (or open Downloads)", "ڈاؤن لوڈ مکمل ہونے پر فائل کھولیں")
		step("3", "If asked, allow \"Install unknown apps\" for your browser, then tap Install", "اگر پوچھا جائے تو اجازت دیں، پھر Install دبائیں")
		b.WriteString(`</ol><div class="note">Only install LinkUp from this page or from a friend's LinkUp. Never from a stranger.</div>`)
	}
	if p.QRSVG != "" && p.Link != "" {
		b.WriteString(`<div class="share"><div class="qr">` + p.QRSVG + `</div><div><b>Give it to a friend</b><span class="small" style="text-align:left;display:block;margin:2px 0 4px">They scan this with the camera on the same Wi-Fi`)
		if p.Short != "" {
			b.WriteString(`, or type</span><code class="big">` + html.EscapeString(p.Short) + `</code><span class="small" style="text-align:left;display:block;margin:2px 0 0">` + html.EscapeString(p.Link) + `</span></div></div>`)
		} else {
			b.WriteString(`</span><code>` + html.EscapeString(p.Link) + `</code></div></div>`)
		}
	}
	b.WriteString(`<div class="foot">LinkUp server</div></div></body></html>`)
	return b.String()
}
