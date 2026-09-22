package report

const Viewer = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Tofu bench</title>
<style>
  :root{
    --bg:#0f1116; --panel:#161922; --line:#242a36; --ink:#e6e9ef; --dim:#8b93a3;
    --tofu:#7aa2f7; --good:#9ece6a; --bad:#f7768e; --warn:#e0af68;
  }
  *{box-sizing:border-box}
  body{margin:0;background:var(--bg);color:var(--ink);display:flex;
    font:14px/1.55 ui-sans-serif,-apple-system,"Segoe UI",Inter,system-ui,sans-serif}
  nav{width:250px;min-width:250px;height:100vh;overflow-y:auto;padding:20px 14px 60px;
    background:#0b0d12;border-right:1px solid var(--line);position:sticky;top:0}
  nav h1{font-size:15px;margin:0 0 2px}
  nav .stamp{color:var(--dim);font-size:12px;margin-bottom:16px}
  nav h2{font-size:11px;letter-spacing:.1em;text-transform:uppercase;color:var(--dim);
    font-weight:650;margin:18px 0 6px}
  nav button{display:block;width:100%;text-align:left;background:none;border:0;color:var(--ink);
    font:inherit;font-size:13px;padding:5px 8px;border-radius:6px;cursor:pointer}
  nav button:hover{background:#151922}
  nav button[aria-pressed=true]{background:#1b2331;font-weight:650}
  nav .when{color:var(--dim);font-size:12px;float:right;font-variant-numeric:tabular-nums}
  nav button.off{color:#7d5560;text-decoration:line-through}
  nav button.stale{color:var(--warn)}
  main{flex:1;max-width:1100px;padding:28px 34px 100px}
  .kicker{font-size:11px;letter-spacing:.12em;text-transform:uppercase;color:var(--dim)}
  h1.title{font-size:23px;margin:6px 0 0;letter-spacing:-.01em}
  .figure{font-size:64px;font-weight:700;line-height:1.05;margin:20px 0 6px;
    font-variant-numeric:tabular-nums;letter-spacing:-.02em}
  .says{font-size:17px;color:#c3cad6;max-width:64ch;margin:0 0 6px}
  .from{color:var(--dim);font-size:12.5px}
  .badge{display:inline-block;font-size:11px;padding:2px 9px;border-radius:999px;
    border:1px solid var(--line);color:var(--dim);margin-left:8px;vertical-align:middle}
  .badge.withdrawn{border-color:#7a2230;background:#2a1219;color:#ff9aa8}
  .badge.stale{border-color:#7a5a22;background:#261d10;color:#ffcc7a}
  .note{border-left:3px solid #7a2230;background:#180d11;color:#ffb3bd;
    padding:11px 14px;margin:18px 0;border-radius:0 8px 8px 0}
  .note.stale{border-left-color:#7a5a22;background:#17130c;color:#ffd79a}
  body.off .figure,body.off h1.title{text-decoration:line-through}
  body.off main{background:repeating-linear-gradient(135deg,transparent,transparent 18px,
    rgba(122,34,48,.07) 18px,rgba(122,34,48,.07) 36px)}
  h2.section{font-size:11px;letter-spacing:.1em;color:var(--dim);font-weight:650;
    margin:34px 0 12px;text-transform:uppercase}
  .big{display:grid;gap:13px;grid-template-columns:repeat(auto-fit,minmax(215px,1fr))}
  .card{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:16px 18px}
  .card .k{color:var(--dim);font-size:12px;letter-spacing:.04em}
  .card .v{font-size:30px;font-weight:680;line-height:1.15;margin:6px 0 2px;
    font-variant-numeric:tabular-nums;letter-spacing:-.02em}
  .card .n{color:var(--dim);font-size:12px}
  .card p{margin:6px 0 0;font-size:13px;color:#c3cad6}
  table{width:100%;border-collapse:collapse;font-variant-numeric:tabular-nums;margin:6px 0 18px}
  th{text-align:left;color:var(--dim);font-size:11px;letter-spacing:.06em;font-weight:600;
    padding:0 11px 8px;border-bottom:1px solid var(--line);text-transform:uppercase}
  td{padding:9px 11px;border-bottom:1px solid var(--line);font-size:13px;vertical-align:top}
  tbody tr:hover{background:#1a1e28}
  tr.off td,tr.off td button{color:#7d5560;text-decoration:line-through}
  tr.stale td{color:#a08a62}
  td button{background:none;border:0;color:var(--tofu);font:inherit;cursor:pointer;padding:0}
  .body h3{font-size:17px;margin:30px 0 8px;border-bottom:1px solid var(--line);padding-bottom:6px}
  .body h4{font-size:15px;margin:22px 0 6px;color:#cdd5e1}
  .body h5{font-size:13.5px;margin:18px 0 4px;color:#b9c2d0}
  .body p{max-width:84ch}
  .body blockquote{border-left:3px solid var(--line);margin:14px 0;padding:4px 14px;color:#a9b3c2}
  .body pre{background:#0b0d12;border:1px solid var(--line);border-radius:8px;padding:12px 14px;
    overflow-x:auto;font-size:12.5px}
  svg.chart{width:100%;height:auto;margin:4px 0 20px;background:var(--panel);
    border:1px solid var(--line);border-radius:12px}
  svg.chart rect{fill:var(--tofu)}
  svg.chart line{stroke:var(--line)}
  svg.chart text{fill:var(--dim);font-size:9px;text-anchor:middle}
  svg.chart text.value{fill:var(--ink)}
  svg.chart text.caption{text-anchor:start;font-size:10px}
  footer{color:var(--dim);font-size:12px;margin-top:50px;max-width:84ch}
</style>
</head>
<body>
<nav id="nav"></nav>
<main id="main"></main>
<script src="` + DataScript + `"></script>
<script>
const D = ` + DataGlobal + ` || {reports:[],counts:{},benches_with_no_dated_report:[]};
const el = (tag, cls, text) => { const e = document.createElement(tag);
  if (cls) e.className = cls; if (text !== undefined) e.textContent = text; return e; };
const svgEl = (tag, attrs, text) => { const e = document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (const k in attrs) e.setAttribute(k, attrs[k]);
  if (text !== undefined) e.textContent = text; return e; };
const key = r => r.package + " " + r.date + " " + r.source;
const stateClass = s => s === "stale" ? "stale" : s === "stands" ? "" : "off";
const card = (k, v, n) => { const c = el("div","card");
  c.append(el("div","k",k));
  if (v) c.append(el("div","v",v));
  c.append(el("div","n",n || "")); return c; };

function chart(c){
  const w = 780, h = 190, band = 54, n = c.values.length;
  const high = Math.max(...c.values, 1), slot = w/n, width = slot*0.7;
  const svg = svgEl("svg", {class:"chart", viewBox:"0 0 "+w+" "+(h+band),
    preserveAspectRatio:"xMidYMid meet", role:"img", "aria-label":c.title});
  c.values.forEach((v,i) => {
    const bar = Math.max(1, v/high*(h-14)), x = i*slot + (slot-width)/2;
    svg.append(svgEl("rect",{x:x, y:h-bar, width:width, height:bar, rx:2}));
    if (n <= 26) {
      svg.append(svgEl("text",{class:"value", x:x+width/2, y:h-bar-4}, String(v)));
      const label = c.labels[i].length > 17 ? c.labels[i].slice(0,16) + "…" : c.labels[i];
      svg.append(svgEl("text",{x:x+width/2, y:h+12,
        transform:"rotate(32 "+(x+width/2)+" "+(h+12)+")"}, label));
    }
  });
  svg.append(svgEl("line",{x1:0, y1:h, x2:w, y2:h}));
  svg.append(svgEl("text",{class:"caption", x:2, y:h+band-6},
    c.title + ", " + n + " rows, highest " + high + (c.unit ? " " + c.unit : "")));
  return svg;
}

function table(head, rows, rowClass, cellNode){
  const t = el("table"), thead = el("thead"), hr = el("tr");
  head.forEach(h => hr.append(el("th",null,h)));
  thead.append(hr); t.append(thead);
  const tb = el("tbody");
  rows.forEach((row,i) => {
    const tr = el("tr", rowClass ? rowClass(i) : null);
    row.forEach((c,j) => { const td = el("td");
      const node = cellNode ? cellNode(i,j,c) : null;
      if (node) td.append(node); else td.textContent = c;
      tr.append(td); });
    tb.append(tr);
  });
  t.append(tb); return t;
}

function body(blocks){
  const box = el("div","body");
  for (const b of blocks) {
    if (b.kind === "heading") box.append(el("h" + Math.min(5, Math.max(3, (b.level||2)+1)), null, b.text));
    else if (b.kind === "paragraph") box.append(el("p",null,b.text));
    else if (b.kind === "quote") box.append(el("blockquote",null,b.text));
    else if (b.kind === "code") box.append(el("pre",null,b.text));
    else if (b.kind === "list") { const ul = el("ul");
      (b.items||[]).forEach(i => ul.append(el("li",null,i))); box.append(ul); }
    else if (b.kind === "table") box.append(table(b.head||[], b.rows||[]));
    else if (b.kind === "chart" && b.chart) box.append(chart(b.chart));
  }
  return box;
}

function everything(){
  const main = document.getElementById("main");
  document.body.className = "";
  main.replaceChildren();
  main.append(el("div","kicker","bench, every dated report on disk"));
  main.append(el("h1","title","What bench has measured"));
  const c = D.counts, big = el("div","big");
  big.append(card("Dated reports", String(c.reports), "one file each, under bench/"));
  big.append(card("Benches with one", String(c.packages_with_a_dated_report), "measured and written up"));
  big.append(card("Benches with none", String(c.packages_with_no_dated_report), "named below, not hidden"));
  big.append(card("Withdrawn", String(c.withdrawn_whole_or_in_part), "whole or in part, not quotable"));
  big.append(card("Stale", String(c.stale), "measured against a corpus that has since moved"));
  big.append(card("No conclusion", String(c.unparsed), "no heading in the file names its own answer"));
  main.append(big);
  main.append(el("p","says","A struck row is withdrawn and may not be quoted. A withdrawal declared from outside a report lives in "
    + D.withdrawals_declared_from_outside_live_in + "; every other state here is declared by the report's own first lines."));
  main.append(el("h2","section","When the measuring happened"));
  main.append(chart(D.dates));
  main.append(el("h2","section","Every dated report, newest first"));
  main.append(table(["Bench","Date","Headline","What it found","Sample","State"],
    D.reports.map(r => [r.package, r.date, r.figure, r.conclusion, r.sample || "not stated", r.state]),
    i => stateClass(D.reports[i].state),
    (i,j) => { if (j) return null;
      const b = el("button",null,D.reports[i].package);
      b.onclick = () => go(key(D.reports[i])); return b; }));
  main.append(el("h2","section","Benches with no dated report"));
  main.append(table(["Bench","Kind","Why there is no report"],
    D.benches_with_no_dated_report.map(p => ["bench/" + p.package, p.kind, p.note])));
  main.append(el("footer", null, "Generated by " + D.generated_by
    + ". This page reads " + "` + DataScript + `" + " beside it and fetches nothing."));
}

function one(r){
  const main = document.getElementById("main");
  document.body.className = stateClass(r.state) === "off" ? "off" : "";
  main.replaceChildren();
  const kicker = el("div","kicker","bench " + r.package + "  " + r.date);
  if (r.state !== "stands") kicker.append(el("span","badge " + (r.state === "stale" ? "stale" : "withdrawn"), r.state));
  main.append(kicker);
  main.append(el("h1","title",r.title));
  main.append(el("div","figure",r.figure));
  main.append(el("p","says",r.says));
  main.append(el("div","from","Read from " + r.source + ", and " + r.built_from_is + "."));
  if (r.state !== "stands") {
    const note = el("div","note" + (r.state === "stale" ? " stale" : ""));
    note.append(el("strong",null,r.state));
    note.append(document.createTextNode(", said by " + r.state_source + ". " + r.state_note));
    main.append(note);
  }
  main.append(el("h2","section","What it rests on"));
  const big = el("div","big");
  big.append(card("Sample", "", r.sample || "not stated in the report"));
  big.append(card("Skips", "", r.skips || "the report names none"));
  const said = el("div","card");
  said.append(el("div","k","What the report says it found"));
  said.append(el("p",null,r.conclusion));
  big.append(said);
  main.append(big);
  if (r.conditions && r.conditions.length) {
    main.append(el("h2","section","Conditions"));
    const box = el("div","card");
    r.conditions.forEach(c => box.append(el("p",null,c)));
    main.append(box);
  }
  if (r.arms) {
    main.append(el("h2","section","The arms, side by side"));
    main.append(table(r.arms.head, r.arms.rows));
  }
  main.append(el("h2","section","The report"));
  main.append(body(r.body || []));
  main.append(el("footer", null, "Generated by " + D.generated_by + " from " + r.source
    + ". Nothing on this page is fetched when it opens."));
}

function nav(){
  const box = document.getElementById("nav");
  box.replaceChildren();
  box.append(el("h1",null,"Tofu bench"));
  box.append(el("div","stamp", D.counts.reports + " dated reports, "
    + D.counts.packages_with_a_dated_report + " benches, "
    + D.counts.packages_with_no_dated_report + " with none"));
  const all = el("button",null,"Everything");
  all.setAttribute("aria-pressed", String(where === ""));
  all.onclick = () => go("");
  box.append(all);
  for (const bench of [...new Set(D.reports.map(r => r.package))].sort()) {
    box.append(el("h2",null,bench));
    for (const r of D.reports.filter(x => x.package === bench)) {
      const b = el("button", stateClass(r.state), r.figure);
      b.append(el("span","when",r.date));
      b.setAttribute("aria-pressed", String(where === key(r)));
      b.onclick = () => go(key(r));
      box.append(b);
    }
  }
}

let where = "";
function go(k){ where = k; location.hash = encodeURIComponent(k); draw(); }
function draw(){
  const found = D.reports.find(r => key(r) === where);
  nav();
  if (found) one(found); else everything();
  window.scrollTo(0,0);
}
window.addEventListener("hashchange", () => {
  const k = decodeURIComponent(location.hash.replace(/^#/, ""));
  if (k !== where) { where = k; draw(); }
});
where = decodeURIComponent(location.hash.replace(/^#/, ""));
draw();
</script>
</body>
</html>
`
