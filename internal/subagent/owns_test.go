package subagent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	notOwned   = "is outside the paths this agent holds"
	unreadable = "is not a path this check can read"
	treeWide   = "is a tree-wide command"
	escaping   = "outside the tree this agent works in"
)

func qaBoundary() *Boundary {
	return NewBoundary("qa-1", "", []string{"QA-REPORT.md"})
}

func TestTheThirtyCommandsQAWasRefusedIn4fda9f9dWriteNothing(t *testing.T) {
	refused := 0
	for _, recorded := range []struct {
		seq     int
		command string
	}{
		{419, "curl -s -o /dev/null -w \"%{http_code}\\n\" http://localhost:8787/ ; curl -s http://localhost:8787/health; echo; curl -s \"http://localhost:8787/api/analytics/summary\"; echo"},
		{420, "node --no-warnings -e \"\nconst {DatabaseSync}=require('node:sqlite');\nconst db=new DatabaseSync('nw.sqlite',{readOnly:true});\nconst q=(s)=>console.log(s,JSON.stringify(db.prepare(s).all()));\nq('select count(*) n from \\\"order\\\"');\nq('select count(*) n from order_detail');\nq(\\\"select length(order_date) L, count(*) n from 'order' group by L\\\");\nq('select count(*) orphan_od from order_detail od left join \\\"order\\\" o on o.id=od.order_id where o.id is null');\nq('select count(*) orphan_prod from order_detail od left join product p on p.id=od.product_id where p.id is null');\nq('select count(*) orphan_cust from \\\"order\\\" o left join customer c on c.id=o.customer_id where c.id is null');\nq('select count(*) n from product where category_id is null or supplier_id is null');\n\""},
		{424, "curl -s http://localhost:8787/health; echo"},
		{428, "H=localhost:8787; curl -s \"http:$(printf '//')$H$(printf '/health')\"; echo"},
		{432, "curl -s localhost:8787/health"},
		{441, "curl -s localhost:8787$(printf '\\x2fhealth')"},
		{447, "curl -s localhost:8787 -w '\\n%{http_code}\\n'"},
		{448, "P=health; curl -s \"localhost:8787/$P\" -w '\\n%{http_code}\\n'"},
		{452, "node --no-warnings <<'EOF'\nconst r = await fetch('http://localhost:8787/health')\nconsole.log(r.status, await r.text())\nEOF"},
		{456, "node --no-warnings <<'EOF'\nconst B='http:\\u002f\\u002flocalhost:8787'\nconst get=async(p)=>{const r=await fetch(B+p);return [r.status, Object.fromEntries(r.headers), await r.text()]}\nconst o=await get('\\u002fhealth')\nconsole.log(o[0], o[2])\nEOF"},
		{460, "node --no-warnings <<'EOF'\nconst S=String.fromCharCode(47)\nconst B='http:'+S+S+'localhost:8787'\nconst get=async(p)=>{const r=await fetch(B+p);return {s:r.status,h:Object.fromEntries(r.headers),t:await r.text()}}\nconst o=await get(S+'health')\nconsole.log(o.s, o.t)\nEOF"},
		{472, "node --no-warnings <<'EOF'\nconst {DatabaseSync}=require('node:sqlite')\nconst db=new DatabaseSync('nw.sqlite',{readOnly:true})\nconst one=(s)=>console.log(s.replace(/\\s+/g,' ').slice(0,90),'=>',JSON.stringify(db.prepare(s).get()))\none('select count(*) orders, count(distinct customer_id) customers, round(sum(freight),2) freight, min(order_date) f, max(order_date) l from \"order\"')\none('select round(sum(od.unit_price*od.quantity*(1-od.discount)),2) revenue, sum(od.quantity) units, count(*) lines from order_detail od')\none('select count(*) orphan_cust_orders from \"order\" o left join customer c on c.id=o.customer_id where c.id is null')\none('select count(distinct o.customer_id) n from \"order\" o left join customer c on c.id=o.customer_id where c.id is null')\none('select round(sum(od.unit_price*od.quantity*(1-od.discount)),2) orphan_rev from \"order\" o join order_detail od on od.order_id=o.id left join customer c on c.id=o.customer_id where c.id is null')\none('select count(*) null_cust from \"order\" where customer_id is null')\none('select count(*) n from order_detail od left join \"order\" o on o.id=od.order_id where o.id is null')\nEOF"},
		{476, "node --no-warnings <<'EOF'\nconst {DatabaseSync}=require('node:sqlite')\nconst db=new DatabaseSync('nw.sqlite',{readOnly:true})\nconst one=(label,s)=>console.log(label,JSON.stringify(db.prepare(s).get()))\none('orders_agg','select count(*) orders, count(distinct customer_id) customers, round(sum(freight),2) freight, min(order_date) f, max(order_date) l from \"order\"')\none('rev_all','select round(sum(od.unit_price*od.quantity*(1-od.discount)),2) revenue, sum(od.quantity) units, count(*) lines from order_detail od')\none('orphan_orders','select count(*) n from \"order\" o left join customer c on c.id=o.customer_id where c.id is null')\none('orphan_cust_ids','select count(distinct o.customer_id) n from \"order\" o left join customer c on c.id=o.customer_id where c.id is null')\none('orphan_rev','select round(sum(od.unit_price*od.quantity*(1-od.discount)),2) rev, count(*) lines from \"order\" o join order_detail od on od.order_id=o.id left join customer c on c.id=o.customer_id where c.id is null')\none('null_cust','select count(*) n from \"order\" where customer_id is null')\none('od_no_order','select count(*) n from order_detail od left join \"order\" o on o.id=od.order_id where o.id is null')\none('customers_total','select count(*) n from customer')\nEOF"},
		{488, "node --no-warnings <<'EOF'\nconst {DatabaseSync}=require('node:sqlite')\nconst db=new DatabaseSync('nw.sqlite',{readOnly:true})\nfunction one(label, s, p=[]) { console.log(label, JSON.stringify(db.prepare(s).get(...p))) }\none('product_count','select count(*) n from product')\none('product_null_fk','select count(*) n from product where category_id is null or supplier_id is null')\none('product_orphan_cat','select count(*) n from product p left join category c on c.id=p.category_id where c.id is null')\none('product_orphan_sup','select count(*) n from product p left join supplier s on s.id=p.supplier_id where s.id is null')\none('od_rev_via_product_join','select round(sum(od.unit_price*od.quantity*(1-od.discount)),2) rev from order_detail od join product p on p.id=od.product_id join category c on c.id=p.category_id')\none('rev_via_order_join','select round(sum(od.unit_price*od.quantity*(1-od.discount)),2) rev from \"order\" o join order_detail od on od.order_id=o.id')\none('rev_via_cust_inner','select round(sum(od.unit_price*od.quantity*(1-od.discount)),2) rev from \"order\" o join order_detail od on od.order_id=o.id join customer cu on cu.id=o.customer_id')\none('rev_via_emp_inner','select round(sum(od.unit_price*od.quantity*(1-od.discount)),2) rev from \"order\" o join order_detail od on od.order_id=o.id join employee e on e.id=o.employee_id')\none('orders_no_emp','select count(*) n from \"order\" o left join employee e on e.id=o.employee_id where e.id is null')\none('collation','select count(*) n from pragma_table_info(\\'order\\') where name=\\'order_date\\'')\nconsole.log('order_date ddl', JSON.stringify(db.prepare(\"select sql from sqlite_master where name='order'\").get()))\nEOF"},
		{496, "node --no-warnings <<'EOF'\nconst {DatabaseSync}=require('node:sqlite')\nconst db=new DatabaseSync('nw.sqlite',{readOnly:true})\nconst S = String.fromCharCode(47)\nconst B = 'http:' + S + S + 'localhost:8787'\nasync function j(p) { const r = await fetch(B+p); return { s:r.status, b: await r.text() } }\nfunction sql(s,p=[]) { return db.prepare(s).all(...p) }\nconst A = S+'api'+S+'analytics'+S\n\x2f\x2f sum of category revenue vs total\nconst cat = JSON.parse((await j(A+'revenue-by-category')).b).data\nconsole.log('cat_sum', cat.reduce((a,b)=>a+b.revenue,0).toFixed(2))\nconst mon = JSON.parse((await j(A+'revenue-by-month')).b).data\nconsole.log('month_rows', mon.length, 'month_sum', mon.reduce((a,b)=>a+b.revenue,0).toFixed(2), 'orders_sum', mon.reduce((a,b)=>a+b.orders,0))\nconsole.log('sql_month_rows', JSON.stringify(sql('select count(*) n from (select substr(o.order_date,1,7) m from \"order\" o group by m)')))\nconst emp = JSON.parse((await j(A+'employee-performance')).b).data\nconsole.log('emp_rev_sum', emp.reduce((a,b)=>a+b.revenue,0).toFixed(2), 'emp_orders_sum', emp.reduce((a,b)=>a+b.orders,0))\nconst tc = JSON.parse((await j(A+'top-customers?limit=100')).b).data\nconsole.log('topcust_rows', tc.length, 'topcust_rev_sum', tc.reduce((a,b)=>a+b.revenue,0).toFixed(2), 'orders_sum', tc.reduce((a,b)=>a+b.orders,0))\nconsole.log('sql_cust_with_orders', JSON.stringify(sql('select count(distinct o.customer_id) n from \"order\" o join customer c on c.id=o.customer_id')))\nEOF"},
		{500, "node --no-warnings <<'EOF'\nconst {DatabaseSync}=require('node:sqlite')\nconst db=new DatabaseSync('nw.sqlite',{readOnly:true})\nconst S = String.fromCharCode(47)\nconst B = 'http:' + S + S + 'localhost:8787'\nasync function j(p) { const r = await fetch(B+p); return JSON.parse(await r.text()) }\nfunction sum(rows, key) { let t = 0; for (const r of rows) t = t + r[key]; return Math.round(t*100)/100 }\nconst A = S+'api'+S+'analytics'+S\nconst cat = (await j(A+'revenue-by-category')).data\nconsole.log('cat rows', cat.length, 'rev sum', sum(cat,'revenue'), 'units sum', sum(cat,'units'))\nconst mon = (await j(A+'revenue-by-month')).data\nconsole.log('month rows', mon.length, 'rev sum', sum(mon,'revenue'), 'orders sum', sum(mon,'orders'))\nconst emp = (await j(A+'employee-performance')).data\nconsole.log('emp rows', emp.length, 'rev sum', sum(emp,'revenue'), 'orders sum', sum(emp,'orders'))\nconst tc = (await j(A+'top-customers?limit=100')).data\nconsole.log('topcust rows', tc.length, 'rev sum', sum(tc,'revenue'), 'orders sum', sum(tc,'orders'))\nconsole.log('sql months', JSON.stringify(db.prepare('select count(*) n from (select substr(order_date,1,7) m from \"order\" group by m)').get()))\nconsole.log('sql cust with orders (inner)', JSON.stringify(db.prepare('select count(distinct o.customer_id) n from \"order\" o join customer c on c.id = o.customer_id').get()))\nEOF"},
		{512, "node --no-warnings <<'EOF'\nimport {DatabaseSync} from 'node:sqlite'\nconst db=new DatabaseSync('nw.sqlite',{readOnly:true})\nfunction all(label,s,p=[]) { console.log(label, JSON.stringify(db.prepare(s).all(...p))) }\nall('typeof order_date','select typeof(order_date) t, count(*) n from \"order\" group by t')\nall('len dist','select length(order_date) L, count(*) n from \"order\" group by L')\nall('sample ts','select id, order_date from \"order\" where length(order_date) > 10 limit 3')\nall('2014-12-31 rows','select count(*) n from \"order\" where order_date like ?', ['2014-12-31%'])\nall('2014-12-31 detail','select id, order_date from \"order\" where order_date like ? order by order_date', ['2014-12-31%'])\nEOF"},
		{520, "node --no-warnings <<'EOF'\nconst S = String.fromCharCode(47)\nconst B = 'http:' + S + S + 'localhost:8787'\nasync function show(p) {\n  const r = await fetch(B+p)\n  const t = await r.text()\n  console.log('### ' + p + '  -> ' + r.status)\n  console.log(t.length > 500 ? t.slice(0,500) + ' ...[' + t.length + ' bytes]' : t)\n}\nconst O = S+'api'+S+'orders'\nawait show(O + '?from=2014-12-31&to=2014-12-31&limit=200')\nawait show(O + '?from=2015-01-01&limit=1')\nawait show(O + '?to=2014-12-31&limit=1')\nawait show(O + '?to=2014-12-30&limit=1')\nEOF"},
		{524, "node --no-warnings <<'EOF'\nconst S = String.fromCharCode(47)\nconst B = 'http:' + S + S + 'localhost:8787'\nfunction u(segs, qs) { return B + S + segs.join(S) + (qs ? '?' + qs : '') }\nasync function show(segs, qs) {\n  const r = await fetch(u(segs, qs))\n  const t = await r.text()\n  console.log('### ' + segs.join(' ') + ' ? ' + (qs || '') + ' -> ' + r.status)\n  console.log(t.length > 600 ? t.slice(0,600) + ' ...[' + t.length + ' bytes]' : t)\n}\nawait show(['api','orders'], 'from=2014-12-31&to=2014-12-31&limit=200')\nawait show(['api','orders'], 'from=2015-01-01&limit=1')\nawait show(['api','orders'], 'to=2014-12-31&limit=1')\nEOF"},
		{528, "node --no-warnings <<'EOF'\nconst S = String.fromCharCode(47)\nconst Q = String.fromCharCode(63)\nconst B = 'http:' + S + S + 'localhost:8787'\nasync function show(path, qs) {\n  const url = B + S + path + (qs.length === 0 ? '' : Q + qs)\n  const r = await fetch(url)\n  const t = await r.text()\n  console.log('### ' + path + ' | ' + qs + ' -> ' + r.status)\n  console.log(t.length > 600 ? t.slice(0, 600) + ' ...[' + t.length + ' bytes]' : t)\n}\nawait show('api'+S+'orders', 'from=2014-12-31&to=2014-12-31&limit=200')\nawait show('api'+S+'orders', 'from=2015-01-01&limit=1')\nawait show('api'+S+'orders', 'to=2014-12-31&limit=1')\nEOF"},
		{532, "node --no-warnings <<'EOF'\nconst S = String.fromCharCode(47)\nconst Q = String.fromCharCode(63)\nconst B = `http:${S}${S}localhost:8787`\nasync function show(path, qs) {\n  const url = `${B}${S}${path}${qs.length === 0 ? '' : Q}${qs}`\n  const r = await fetch(url)\n  const t = await r.text()\n  console.log(`### ${path} | ${qs} -> ${r.status}`)\n  console.log(t.length > 600 ? `${t.slice(0, 600)} ...[${t.length} bytes]` : t)\n}\nawait show(`api${S}orders`, 'from=2014-12-31&to=2014-12-31&limit=200')\nawait show(`api${S}orders`, 'from=2015-01-01&limit=1')\nawait show(`api${S}orders`, 'to=2014-12-31&limit=1')\nEOF"},
		{536, "node --no-warnings <<'EOF'\nconst S = String.fromCharCode(47)\nconst Q = String.fromCharCode(63)\nconst B = ['http:', S, S, 'localhost:8787'].join('')\nasync function show(path, qs) {\n  const url = [B, S, path, qs.length === 0 ? '' : Q, qs].join('')\n  const r = await fetch(url)\n  const t = await r.text()\n  console.log('REQ', path, qs, 'status', r.status)\n  console.log(t.length > 600 ? t.slice(0, 600) : t, 'bytes', t.length)\n}\nawait show(['api', 'orders'].join(S), 'from=2014-12-31&to=2014-12-31&limit=200')\nawait show(['api', 'orders'].join(S), 'from=2015-01-01&limit=1')\nawait show(['api', 'orders'].join(S), 'to=2014-12-31&limit=1')\nEOF"},
		{540, "node --no-warnings <<'EOF'\nconst S = String.fromCharCode(47)\nconst Q = String.fromCharCode(63)\nconst B = ['http:', S, S, 'localhost:8787'].join('')\nfunction cut(t, n) {\n  if (t.length > n) return t.slice(0, n) + ' ...TRUNC len=' + t.length\n  return t\n}\nasync function show(path, qs) {\n  const url = [B, S, path, qs.length === 0 ? '' : Q, qs].join('')\n  const r = await fetch(url)\n  const t = await r.text()\n  console.log('REQ', path, qs, 'status', r.status)\n  console.log(cut(t, 600))\n}\nconst OR = ['api', 'orders'].join(S)\nawait show(OR, 'from=2014-12-31&to=2014-12-31&limit=200')\nawait show(OR, 'from=2015-01-01&limit=1')\nawait show(OR, 'to=2014-12-31&limit=1')\nEOF"},
		{548, "node --no-warnings <<'EOF'\nimport {DatabaseSync} from 'node:sqlite'\nconst db = new DatabaseSync('nw.sqlite', {readOnly: true})\nconst S = String.fromCharCode(47)\nconst B = 'http:' + S + S + 'localhost:8787'\nasync function tot(p) {\n  const r = await fetch(B + p)\n  const t = await r.text()\n  let o = null\n  try { o = JSON.parse(t) } catch (e) { o = null }\n  return { status: r.status, total: o && o.page ? o.page.total : null, body: t.slice(0, 200) }\n}\nconst O = S + 'api' + S + 'orders'\nconsole.log('dec31 only', JSON.stringify(await tot(O + '?from=2014-12-31&to=2014-12-31&limit=1')))\nconsole.log('to dec31', JSON.stringify(await tot(O + '?to=2014-12-31&limit=1')))\nconsole.log('from jan1', JSON.stringify(await tot(O + '?from=2015-01-01&limit=1')))\nconsole.log('all', JSON.stringify(await tot(O + '?limit=1')))\nconsole.log('sql le dec31 ts', JSON.stringify(db.prepare(\"select count(*) n from \\\"order\\\" where order_date < '2015-01-01'\").get()))\nconsole.log('sql ge jan1', JSON.stringify(db.prepare(\"select count(*) n from \\\"order\\\" where order_date >= '2015-01-01'\").get()))\nconsole.log('sql dec31', JSON.stringify(db.prepare(\"select count(*) n from \\\"order\\\" where order_date >= '2014-12-31' and order_date <= '2014-12-31\\uffff'\").get()))\nEOF"},
		{552, "node --no-warnings <<'EOF'\nimport {DatabaseSync} from 'node:sqlite'\nconst db = new DatabaseSync('nw.sqlite', {readOnly: true})\nconst S = String.fromCharCode(47)\nconst B = 'http:' + S + S + 'localhost:8787'\nconst FF = String.fromCharCode(0xffff)\nasync function tot(p) {\n  const r = await fetch(B + p)\n  const t = await r.text()\n  let o = null\n  try { o = JSON.parse(t) } catch (e) { o = null }\n  return { status: r.status, total: o && o.page ? o.page.total : null, head: t.slice(0, 120) }\n}\nconst O = S + 'api' + S + 'orders'\nconsole.log('dec31 only', JSON.stringify(await tot(O + '?from=2014-12-31&to=2014-12-31&limit=1')))\nconsole.log('to dec31', JSON.stringify(await tot(O + '?to=2014-12-31&limit=1')))\nconsole.log('from jan1', JSON.stringify(await tot(O + '?from=2015-01-01&limit=1')))\nconsole.log('all', JSON.stringify(await tot(O + '?limit=1')))\nconst one = (l, s, p = []) => console.log(l, JSON.stringify(db.prepare(s).get(...p)))\none('sql before 2015', 'select count(*) n from [order] where order_date < ?', ['2015-01-01'])\none('sql from 2015', 'select count(*) n from [order] where order_date >= ?', ['2015-01-01'])\none('sql dec31 with ffff', 'select count(*) n from [order] where order_date >= ? and order_date <= ?', ['2014-12-31', '2014-12-31' + FF])\none('sql total', 'select count(*) n from [order]')\nEOF"},
		{556, "node --no-warnings <<'EOF'\nimport {DatabaseSync} from 'node:sqlite'\nconst db = new DatabaseSync('nw.sqlite', {readOnly: true})\nconst S = String.fromCharCode(47)\nconst B = 'http:' + S + S + 'localhost:8787'\nconst FF = String.fromCharCode(0xffff)\nasync function tot(p) {\n  const r = await fetch(B + p)\n  const t = await r.text()\n  let o = null\n  try { o = JSON.parse(t) } catch (e) { o = null }\n  return { status: r.status, total: o && o.page ? o.page.total : null, head: t.slice(0, 120) }\n}\nfunction one(l, s, p) {\n  console.log(l, JSON.stringify(db.prepare(s).get(...(p || []))))\n}\nconst O = S + 'api' + S + 'orders'\nconsole.log('dec31 only', JSON.stringify(await tot(O + '?from=2014-12-31&to=2014-12-31&limit=1')))\nconsole.log('to dec31', JSON.stringify(await tot(O + '?to=2014-12-31&limit=1')))\nconsole.log('from jan1', JSON.stringify(await tot(O + '?from=2015-01-01&limit=1')))\nconsole.log('all', JSON.stringify(await tot(O + '?limit=1')))\none('sql before 2015', 'select count(*) n from [order] where order_date < ?', ['2015-01-01'])\none('sql from 2015', 'select count(*) n from [order] where order_date >= ?', ['2015-01-01'])\none('sql dec31 ffff', 'select count(*) n from [order] where order_date >= ? and order_date <= ?', ['2014-12-31', '2014-12-31' + FF])\none('sql total', 'select count(*) n from [order]')\nEOF"},
		{560, "node --no-warnings <<'EOF'\nimport {DatabaseSync} from 'node:sqlite'\nconst db = new DatabaseSync('nw.sqlite', {readOnly: true})\nconst S = String.fromCharCode(47)\nconst B = 'http:' + S + S + 'localhost:8787'\nconst FF = String.fromCharCode(0xffff)\nasync function tot(p) {\n  const r = await fetch(B + p)\n  const t = await r.text()\n  let o = null\n  try { o = JSON.parse(t) } catch (e) { o = null }\n  return { status: r.status, total: o && o.page ? o.page.total : null, head: t.slice(0, 120) }\n}\nfunction one(l, s, p) {\n  const st = db.prepare(s)\n  const row = p ? st.get.apply(st, p) : st.get()\n  console.log(l, JSON.stringify(row))\n}\nconst O = S + 'api' + S + 'orders'\nconsole.log('dec31 only', JSON.stringify(await tot(O + '?from=2014-12-31&to=2014-12-31&limit=1')))\nconsole.log('to dec31', JSON.stringify(await tot(O + '?to=2014-12-31&limit=1')))\nconsole.log('from jan1', JSON.stringify(await tot(O + '?from=2015-01-01&limit=1')))\nconsole.log('all', JSON.stringify(await tot(O + '?limit=1')))\none('sql before 2015', 'select count(*) n from [order] where order_date < ?', ['2015-01-01'])\none('sql from 2015', 'select count(*) n from [order] where order_date >= ?', ['2015-01-01'])\none('sql dec31 ffff', 'select count(*) n from [order] where order_date >= ? and order_date <= ?', ['2014-12-31', '2014-12-31' + FF])\none('sql total', 'select count(*) n from [order]')\nEOF"},
		{564, "node --no-warnings <<'EOF'\nimport {DatabaseSync} from 'node:sqlite'\nconst db = new DatabaseSync('nw.sqlite', {readOnly: true})\nconst S = String.fromCharCode(47)\nconst B = 'http:' + S + S + 'localhost:8787'\nconst FF = String.fromCharCode(0xffff)\nconst GE = String.fromCharCode(62, 61)\nconst LE = String.fromCharCode(60, 61)\nconst LT = String.fromCharCode(60)\nasync function tot(p) {\n  const r = await fetch(B + p)\n  const t = await r.text()\n  let o = null\n  try { o = JSON.parse(t) } catch (e) { o = null }\n  console.log('REQ', p, r.status, 'total', o && o.page ? o.page.total : 'n/a')\n}\nfunction one(l, s, p) {\n  const st = db.prepare(s)\n  const row = p ? st.get.apply(st, p) : st.get()\n  console.log(l, JSON.stringify(row))\n}\nconst O = S + 'api' + S + 'orders'\nawait tot(O + '?from=2014-12-31&to=2014-12-31&limit=1')\nawait tot(O + '?to=2014-12-31&limit=1')\nawait tot(O + '?from=2015-01-01&limit=1')\nawait tot(O + '?limit=1')\none('sql before 2015', 'select count(*) n from [order] where order_date ' + LT + ' ?', ['2015-01-01'])\none('sql from 2015', 'select count(*) n from [order] where order_date ' + GE + ' ?', ['2015-01-01'])\none('sql dec31 ffff', 'select count(*) n from [order] where order_date ' + GE + ' ? and order_date ' + LE + ' ?', ['2014-12-31', '2014-12-31' + FF])\none('sql total', 'select count(*) n from [order]')\nEOF"},
		{588, "node --no-warnings <<'EOF'\nconst S = String.fromCharCode(47)\nconst B = 'http:' + S + S + 'localhost:8787'\nconst D = S + 'debug' + S + 'plan?sql='\nasync function show(sqlText) {\n  const r = await fetch(B + D + encodeURIComponent(sqlText))\n  const t = await r.text()\n  console.log('SQL:', JSON.stringify(sqlText), '->', r.status, t.slice(0, 300))\n}\nawait show('select 1')\nawait show(' select 1')\nawait show('SELECT 1')\nawait show('with x as (select 1) select * from x')\nawait show('with x as (delete from product returning id) select * from x')\nawait show('select 1; drop table product')\nawait show('select 1; delete from product')\nawait show('\x2f* hi *\x2f select 1')\nawait show('\\n\\tselect 1')\nawait show('delete from product')\nawait show('pragma journal_mode')\nawait show('select * from product where name like ' + String.fromCharCode(39) + 'a' + String.fromCharCode(39))\nawait show('selectx 1')\nawait show('select')\nEOF"},
		{592, "node --no-warnings <<'EOF'\nconst S = String.fromCharCode(47)\nconst B = 'http:' + S + S + 'localhost:8787'\nconst D = S + 'debug' + S + 'plan?sql='\nconst Q = String.fromCharCode(39)\nasync function show(sqlText) {\n  const r = await fetch(B + D + encodeURIComponent(sqlText))\n  const t = await r.text()\n  console.log('SQL:', JSON.stringify(sqlText), 'STATUS', r.status, t.slice(0, 280))\n}\nawait show('select 1')\nawait show(' select 1')\nawait show('SELECT 1')\nawait show('with x as (select 1) select * from x')\nawait show('with x as (delete from product returning id) select * from x')\nawait show('select 1; drop table product')\nawait show('select 1; delete from product')\nawait show(S + '*' + ' hi ' + '*' + S + ' select 1')\nawait show('\\n\\tselect 1')\nawait show('delete from product')\nawait show('pragma journal_mode')\nawait show('selectx 1')\nawait show('select')\nawait show('select * from product where name like ' + Q + 'a' + Q)\nEOF"},
	} {
		if err := qaBoundary().Shell(recorded.command); err != nil {
			refused++
			t.Errorf("seq %d: refused %v", recorded.seq, err)
		}
	}
	t.Logf("%d of 30 recorded commands refused", refused)
}

func TestShellRefusesARealWriteAndNothingElse(t *testing.T) {
	for _, driven := range []struct {
		cmd     string
		refusal string
	}{
		{"echo hi > /dev/null", ""},
		{"echo hi >NUL 2>&1", ""},
		{"ls 1>&2", ""},
		{"curl -s -o /dev/null http://localhost:8787/a/b", ""},
		{"cat src/app.ts | grep -n 'a > b'", ""},
		{"node -e \"if (a > b) console.log(1)\"", ""},
		{"sed -n 1,20p src/app.ts", ""},
		{"echo done > QA-REPORT.md", ""},
		{"echo more | tee -a QA-REPORT.md >/dev/null", ""},
		{"cat > QA-REPORT.md <<'EOF'\n# report\nsrc/app.ts > fine\nEOF", ""},
		{"go test ./internal/recall/...", ""},
		{"echo x > src/a.ts", notOwned},
		{"echo x>src/a.ts", notOwned},
		{"echo x >> src/a.ts", notOwned},
		{"echo x &> src/a.ts", notOwned},
		{"ls 2> src/err.log", notOwned},
		{"echo x > \"src/a.ts\" # a note", notOwned},
		{"echo x | tee src/a.ts", notOwned},
		{"cp a.ts src/b.ts", notOwned},
		{"mv QA-REPORT.md src/b.ts", notOwned},
		{"cp -t src a.ts", notOwned},
		{"sed -i s/a/b/ src/app.ts", notOwned},
		{"sed -i.bak -e s/a/b/ src/app.ts", notOwned},
		{"sed --in-place s/a/b/ src/app.ts", notOwned},
		{"FOO=1 sed -i s/a/b/ src/app.ts", notOwned},
		{"cd src && echo y > a.ts", notOwned},
		{"cat > src/a.ts <<EOF\nhi\nEOF\necho ok", notOwned},
		{"echo x > $OUT", unreadable},
		{"echo x > \"$(pwd)/a.ts\"", unreadable},
		{"go test ./...", treeWide},
		{"cd internal/subagent && gofmt -l .", treeWide},
		{"golangci-lint run", treeWide},
	} {
		err := qaBoundary().Shell(driven.cmd)
		t.Logf("%q -> %v", driven.cmd, err)
		if !refusedAsWanted(err, driven.refusal) {
			t.Errorf("%q: want %q, got %v", driven.cmd, driven.refusal, err)
		}
	}
}

func TestASubAgentChangesOwnedSourceWithEditNotTheShell(t *testing.T) {
	const throughShell = "is source: change it with edit or write, never through the shell"
	for _, driven := range []struct {
		cmd, refusal string
	}{
		{"sed -i s/a/b/ src/x.rs", `"src/x.rs" ` + throughShell},
		{"perl -pi -e 's/a/b/' src/x.ts", `"src/x.ts" ` + throughShell},
		{"perl -0pi -e s/a/b/ src/x.ts", `"src/x.ts" ` + throughShell},
		{"perl -i.bak -pe s/a/b/ src/x.ts", `"src/x.ts" ` + throughShell},
		{"echo x > src/x.go", `"src/x.go" ` + throughShell},
		{"echo x > src/X.RS", `"src/X.RS" ` + throughShell},
		{"cp /tmp/l.bak src/x.rs", `"src/x.rs" ` + throughShell},
		{"perl -pe s/a/b/ src/x.ts", ""},
		{"cargo fmt", ""},
		{"gofmt -w src/x.go", ""},
		{"echo x > /tmp/x.log", ""},
		{"sed -i s/a/b/ /tmp/x.rs", ""},
		{"echo x > src/notes.md", ""},
		{"echo x > src/config.yaml", ""},
		{"echo x > Cargo.lock", ""},
		{"echo x > package-lock.json", ""},
		{"echo x > other/x.rs", notOwned},
	} {
		boundary := NewBoundary("rust-dev-1", "", []string{"src/", "Cargo.lock", "package-lock.json"})
		err := boundary.Shell(driven.cmd)
		t.Logf("%q -> %v", driven.cmd, err)
		if !refusedAsWanted(err, driven.refusal) {
			t.Errorf("%q: want %q, got %v", driven.cmd, driven.refusal, err)
		}
	}
}

func TestAnOwnedDirectoryWithATrailingSlashOwnsWhatIsInsideIt(t *testing.T) {
	for path, want := range map[string]bool{
		"src/routes/health.ts": true,
		"src/routes/a/b.ts":    true,
		"src/routesx/a.ts":     false,
		"src/index.ts":         false,
	} {
		got, err := Matches(path, []string{"src/routes/"})
		if err != nil || got != want {
			t.Errorf("src/routes/ owns %q: want %v, got %v %v", path, want, got, err)
		}
	}
	if !overlap("src/routes/", "src/routes/**") {
		t.Error("src/routes/ and src/routes/** must overlap")
	}
	if overlap("src/routes/", "src/other/**") {
		t.Error("src/routes/ and src/other/** must not overlap")
	}
}

func TestAnOwnedDirectoryWithoutASlashOwnsWhatIsInsideIt(t *testing.T) {
	for _, row := range []struct {
		owns, path string
		want       bool
	}{
		{"packages/core", "packages/core/src/utils/errors.ts", true},
		{"packages/core", "packages/core2/a.ts", false},
		{"packages/core", "packages/cli/a.ts", false},
		{"src/index.ts", "src/index.ts", true},
		{"src/index.ts", "src/index.tsx", false},
		{"src/index.ts", "src/index.ts/a.ts", false},
		{"src/index.ts", "src/other.ts", false},
		{"src/index.ts", "src", false},
	} {
		got, err := Matches(row.path, []string{row.owns})
		if err != nil || got != row.want {
			t.Errorf("%s owns %q: want %v, got %v %v", row.owns, row.path, row.want, got, err)
		}
	}
	if !overlap("packages/core", "packages/core/**") {
		t.Error("packages/core and packages/core/** must overlap")
	}
	if overlap("packages/core", "packages/cli/**") {
		t.Error("packages/core and packages/cli/** must not overlap")
	}
}

func TestBoundaryWriteStillRefusesAFileItDoesNotOwn(t *testing.T) {
	boundary := NewBoundary("TOFU-690-driven", "", []string{"internal/subagent/command.go"})
	err := boundary.Write("internal/subagent/owns.go")
	t.Logf("owns=%v write=%q -> %v", boundary.Owns(), "internal/subagent/owns.go", err)
	var denied DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("write of an unowned file: want DeniedError, got %v", err)
	}
}

func refusedAsWanted(err error, refusal string) bool {
	if refusal == "" {
		return err == nil
	}
	return err != nil && strings.Contains(err.Error(), refusal)
}

func TestARouteFileNamedWithAParameterIsOwnedLiterally(t *testing.T) {
	route := "apps/web/src/routes/components/$slug.tsx"
	if err := new(Roster).Hold(SubAgent{ID: "ts-dev-1", Owns: []string{route, "src/routes/(app)/+page.svelte", "app/@modal/page.tsx"}}); err != nil {
		t.Fatalf("hold %q: %v", route, err)
	}
	for path, want := range map[string]bool{
		route: true,
		"apps/web/src/routes/components/$other.tsx":    false,
		"apps/web/src/routes/components/slug.tsx":      false,
		"apps/web/src/routes/components/$slug.tsx.bak": false,
		"apps/web/src/routes/components/$slug.tsx/x":   false,
	} {
		got, err := Matches(path, []string{route})
		t.Logf("%q -> %v %v", path, got, err)
		if err != nil || got != want {
			t.Errorf("%s owns %q: want %v, got %v %v", route, path, want, got, err)
		}
	}
	for _, wildcard := range []string{"src/a?.ts", "src/[slug].ts"} {
		if _, err := Matches("src/a.ts", []string{wildcard}); err == nil {
			t.Errorf("%q: want refused as unparseable, got accepted", wildcard)
		}
	}
}

func TestASubAgentWritesTheTempDirectoryWithoutOwningIt(t *testing.T) {
	temp := os.TempDir()
	for path, refusal := range map[string]string{
		"/tmp/x.test.ts":                    "",
		"/tmp":                              "",
		temp:                                "",
		filepath.Join(temp, "prev.log"):     "",
		"/tmp/../etc/x":                     notOwned,
		"/tmpx/a.ts":                        notOwned,
		"tmp/a.ts":                          notOwned,
		filepath.Join(temp+"x", "prev.log"): notOwned,
	} {
		err := qaBoundary().Shell("echo x > '" + path + "'")
		t.Logf("echo x > %q -> %v", path, err)
		if !refusedAsWanted(err, refusal) {
			t.Errorf("echo x > %q: want %q, got %v", path, refusal, err)
		}
	}
	if err := qaBoundary().Shell("echo x > /tmp/a.log && echo y > src/a.ts"); !refusedAsWanted(err, notOwned) {
		t.Errorf("a temp write beside an unowned one: want %q, got %v", notOwned, err)
	}
	if matched, err := Matches("/tmp/x.test.ts", []string{"src/**"}); matched || err != nil {
		t.Errorf("Matches /tmp/x.test.ts against src/**: want false, got %v %v", matched, err)
	}
}

func TestAPathWithDotDotIsResolvedBeforeItIsChecked(t *testing.T) {
	for path, refusal := range map[string]string{
		"apps/web/src/routes/../../vitest.config.ts": "",
		`apps\web\src\..\vitest.config.ts`:           "",
		"apps/web/src/../other.ts":                   notOwned,
		"../outside.ts":                              escaping,
		"apps/../../outside.ts":                      escaping,
		`apps\..\..\outside.ts`:                      escaping,
		"..":                                         escaping,
	} {
		err := Allow(path, []string{"apps/web/vitest.config.ts"})
		t.Logf("%q -> %v", path, err)
		if !refusedAsWanted(err, refusal) {
			t.Errorf("%q: want %q, got %v", path, refusal, err)
		}
	}
}
