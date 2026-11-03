package report

import (
	_ "embed"
	"encoding/base64"
)

//go:embed fonts/inter-latin.woff2
var interLatin []byte

func fontFace() string {
	return "@font-face{font-family:Inter;font-style:normal;font-weight:400 600;font-display:swap;src:url(data:font/woff2;base64," +
		base64.StdEncoding.EncodeToString(interLatin) + ") format('woff2');" + latinRange + "}"
}

const latinRange = "unicode-range:U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+0304,U+0308,U+0329,U+2000-206F,U+20AC,U+2122,U+2191,U+2193,U+2212,U+2215,U+FEFF,U+FFFD"

const stylesheet = `
:root{
  --bg:#fafafa;--surface:#fff;--text:#111;--muted:#6b7280;--line:#e5e7eb;--accent:#2563eb;
  --ahead-text:#15803d;--ahead-bg:#dcfce7;--behind-text:#b91c1c;--behind-bg:#fee2e2;--level-text:#4b5563;--level-bg:#f3f4f6;
  --radius:12px;
  --font:Inter, system-ui, -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
  --mono:ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
@media (prefers-color-scheme: dark){:root{
  --bg:#0b0b0c;--surface:#141416;--text:#ededed;--muted:#8b8b93;--line:#26262b;--accent:#60a5fa;
  --ahead-text:#4ade80;--ahead-bg:#14532d40;--behind-text:#f87171;--behind-bg:#7f1d1d40;--level-text:#a1a1aa;--level-bg:#27272a;
}}
*{box-sizing:border-box}
html{color-scheme:light dark}
body{margin:0;background:var(--bg);color:var(--text);font:400 15px/1.5 var(--font)}
main{max-width:960px;margin:0 auto;padding:40px 32px 64px}
@media (max-width:600px){main{padding:24px 16px 48px}}
h1{font-size:22px;font-weight:600;margin:0 0 8px;letter-spacing:-.01em}
h2{font-size:17px;font-weight:600;margin:0 0 12px}
h3{font-size:15px;font-weight:600;margin:20px 0 8px}
section{margin-top:40px}
a{color:var(--accent);text-decoration:none}
a:hover{text-decoration:underline}
nav.index{display:flex;flex-wrap:wrap;gap:4px 14px;font-size:13px;margin-bottom:24px}
.num,.fig,.stat-value,td,.reading{font-feature-settings:"tnum";font-variant-numeric:tabular-nums}
.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:12px;margin:16px 0}
.stat{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:16px 18px;
  box-shadow:0 1px 2px rgba(0,0,0,.04)}
.stat-value{font-size:28px;font-weight:600;letter-spacing:-.02em}
.stat-line{font-size:13px;color:var(--muted)}
.tldr{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:20px;margin:0;list-style:none}
.tldr li{margin:2px 0}
.cards{display:grid;gap:12px;margin:0 0 20px;grid-template-columns:repeat(var(--cols,3),minmax(0,1fr))}
@media (max-width:760px){.cards{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media (max-width:480px){.cards{grid-template-columns:minmax(0,1fr)}}
.card{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:14px 16px}
.card-title{font-size:20px;font-weight:600;margin:0}
.card-description{font-size:13px;color:var(--muted);margin:0}
.table-container{width:100%;overflow-x:auto}
.table{width:100%;border-collapse:collapse;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius)}
.table-caption{caption-side:top;text-align:left;font-size:13px;color:var(--muted);padding-bottom:6px}
.table-head{font-size:12px;font-weight:500;text-transform:uppercase;letter-spacing:.04em;color:var(--muted);
  text-align:left;padding:10px 12px;border-bottom:1px solid var(--line);white-space:nowrap}
.table-cell{padding:10px 12px;border-bottom:1px solid var(--line);vertical-align:top}
.table-row:last-child .table-cell{border-bottom:0}
.num{text-align:right;white-space:nowrap}
.tofu{font-weight:500}
.best{font-weight:600}
.pill{display:inline-block;border-radius:999px;padding:1px 10px;font-size:13px;white-space:nowrap;
  background:var(--level-bg);color:var(--level-text);margin:1px 0}
.pill.win{background:var(--ahead-bg);color:var(--ahead-text)}
.pill.loss{background:var(--behind-bg);color:var(--behind-text)}
.win .fig{color:var(--ahead-text)}
.loss .fig{color:var(--behind-text)}
.missing,.detail,.lede,.tag,.path,code{color:var(--muted)}
.detail{display:block;font-size:13px}
.lede{font-size:13px;margin:0 0 16px}
.tag{font-size:12px;margin-top:6px}
code,.path{font-family:var(--mono);font-size:12px}
.badge{display:inline-block;border:1px solid var(--line);border-radius:999px;padding:0 8px;font-size:12px;color:var(--muted)}
details{margin-top:8px}
details>summary{cursor:pointer;font-size:13px;color:var(--muted)}
details[open]>summary{margin-bottom:8px}
details.older>summary{font-size:15px;color:var(--text);font-weight:600}
details.report{border-bottom:1px solid var(--line);padding:8px 0}
ul{padding-left:18px;margin:0}
blockquote{margin:0 0 10px;padding:8px 12px;background:var(--bg);border:1px solid var(--line);border-radius:8px}
blockquote s.fell{color:var(--behind-text)}
blockquote cite{display:block;font-style:normal;font-size:12px;margin-top:4px;color:var(--muted);font-family:var(--mono)}
svg.gapchart{width:100%;height:auto;margin:6px 0}
svg.gapchart text{fill:var(--muted);font-size:11px;font-family:var(--font)}
svg.gapchart text.point{fill:var(--text);text-anchor:end}
svg.gapchart text.reading{fill:var(--text);text-anchor:start}
svg.gapchart rect.free{fill:var(--muted)}
svg.gapchart rect.judged{fill:var(--accent)}
footer{margin-top:48px;color:var(--muted);font-size:12px}
`

const openTarget = `
const openFor = () => { const target = document.getElementById(location.hash.slice(1)); if (target && target.tagName === 'DETAILS') target.open = true; };
addEventListener('hashchange', openFor);
openFor();
`
