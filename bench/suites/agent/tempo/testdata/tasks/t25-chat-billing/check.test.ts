import { expect, test } from "bun:test";
import { tmpdir } from "node:os";
import { join } from "node:path";


process.env.CHAT_DB = join(tmpdir(), `chat-check-${Date.now()}.db`);

const next = (ws: WebSocket, type: string) =>
  new Promise<any>((resolve, reject) => {
    const t = setTimeout(() => reject(new Error(`no ${type}`)), 3000);
    const on = (e: MessageEvent) => {
      const m = JSON.parse(String(e.data));
      if (m.type === type) {
        clearTimeout(t);
        ws.removeEventListener("message", on);
        resolve(m);
      }
    };
    ws.addEventListener("message", on);
  });


process.env.INV_DB = join(tmpdir(), `inv-check-${Date.now()}.db`);


test("realtime chat", async () => {
  const mod = await import("../src/index");
  const server = Bun.serve({ port: 0, fetch: mod.default.fetch, websocket: mod.websocket });
  const base = `http://localhost:${server.port}`;
  try {
    const room = await (await fetch(`${base}/chat/rooms`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ name: "general" }) })).json();
    expect((await fetch(`${base}/chat/rooms`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ name: "general" }) })).status).toBe(409);
    const a = new WebSocket(`ws://localhost:${server.port}/chat/ws?room=${room.id}&user=ann`);
    await new Promise((r) => a.addEventListener("open", r));
    const presence = next(a, "presence");
    const b = new WebSocket(`ws://localhost:${server.port}/chat/ws?room=${room.id}&user=bob`);
    await new Promise((r) => b.addEventListener("open", r));
    const seen = await presence;
    expect(seen.users.length).toBeGreaterThanOrEqual(1);
    const got = next(b, "message");
    a.send(JSON.stringify({ type: "message", text: "hello" }));
    const m = await got;
    expect(m.text).toBe("hello");
    expect(m.user).toBe("ann");
    const err = next(a, "error");
    a.send(JSON.stringify({ type: "message", text: "" }));
    await err;
    const hist = await (await fetch(`${base}/chat/rooms/${room.id}/messages`)).json();
    const items = Array.isArray(hist) ? hist : hist.items;
    expect(items[0].text).toBe("hello");
    const closed = new Promise<number>((r) => b.addEventListener("close", (e) => r(e.code)));
    for (let i = 0; i < 12; i++) b.send(JSON.stringify({ type: "message", text: `spam ${i}` }));
    expect(await closed).toBe(1008);
    a.close();
  } finally {
    server.stop(true);
  }
});

test("invoicing", async () => {
  const app = (await import("../src/index")).default;
  const call = async (p: string, method = "GET", body?: unknown, headers: Record<string, string> = {}) => {
    const r = await app.request(p, { method, headers: { "content-type": "application/json", ...headers }, body: body === undefined ? undefined : JSON.stringify(body) });
    const text = await r.text();
    return { status: r.status, body: text ? JSON.parse(text) : null, text };
  };

  const c = await call("/billing/customers", "POST", { name: "Ann", email: "ann@x.co" });
  expect(c.status).toBe(201);
  expect((await call("/billing/customers", "POST", { name: "Ann2", email: "ann@x.co" })).status).toBe(409);
  expect((await call("/billing/customers", "POST", { name: "Bad", email: "nope" })).status).toBe(400);
  expect((await call("/billing/customers/999999")).status).toBe(404);

  const lines = [
    { description: "a", quantity: 3, unitCents: 1999, taxRate: 0.1 },
    { description: "b", quantity: 1, unitCents: 501, taxRate: 0.075 },
  ];
  const inv = await call("/billing/invoices", "POST", { customerId: c.body.id, currency: "USD", lines });
  expect(inv.status).toBe(201);
  expect(inv.body.subtotalCents).toBe(6498);
  expect(inv.body.taxCents).toBe(Math.round(5997 * 0.1) + Math.round(501 * 0.075));
  expect(inv.body.totalCents).toBe(6498 + inv.body.taxCents);
  expect(inv.body.status).toBe("draft");
  expect(inv.body.number).toBeNull();
  expect((await call("/billing/invoices", "POST", { customerId: c.body.id, currency: "JPY", lines })).status).toBe(400);
  expect((await call("/billing/invoices", "POST", { customerId: c.body.id, currency: "USD", lines: [{ ...lines[0], quantity: 0 }] })).status).toBe(400);
  expect((await call("/billing/invoices", "POST", { customerId: c.body.id, currency: "USD", lines: [{ ...lines[0], taxRate: 0.9 }] })).status).toBe(400);
  expect((await call("/billing/invoices", "POST", { customerId: 999999, currency: "USD", lines })).status).toBe(404);

  const id = inv.body.id;
  expect((await call(`/billing/invoices/${id}/payments`, "POST", { amountCents: 100 }, { "Idempotency-Key": "k0" })).status).toBe(409);
  const edited = await call(`/billing/invoices/${id}/lines`, "PUT", [{ description: "c", quantity: 2, unitCents: 1000, taxRate: 0 }]);
  const editedBody = Array.isArray(edited.body) ? edited.body : edited.body;
  expect(edited.status).toBe(200);
  expect(editedBody.totalCents).toBe(2000);

  const issued = await call(`/billing/invoices/${id}/issue`, "POST");
  expect(issued.status).toBe(200);
  expect(issued.body.number).toBe("INV-0001");
  expect((await call(`/billing/invoices/${id}/issue`, "POST")).status).toBe(409);
  expect((await call(`/billing/invoices/${id}/lines`, "PUT", lines)).status).toBe(409);

  const eur = await call("/billing/invoices", "POST", { customerId: c.body.id, currency: "EUR", lines });
  expect((await call(`/billing/invoices/${eur.body.id}/issue`, "POST")).body.number).toBe("INV-0001");
  const usd2 = await call("/billing/invoices", "POST", { customerId: c.body.id, currency: "USD", lines });
  expect((await call(`/billing/invoices/${usd2.body.id}/issue`, "POST")).body.number).toBe("INV-0002");

  expect((await call(`/billing/invoices/${id}/payments`, "POST", { amountCents: 500 })).status).toBe(400);
  expect((await call(`/billing/invoices/${id}/payments`, "POST", { amountCents: 0 }, { "Idempotency-Key": "kz" })).status).toBe(400);
  const p1 = await call(`/billing/invoices/${id}/payments`, "POST", { amountCents: 500 }, { "Idempotency-Key": "k1" });
  expect([200, 201]).toContain(p1.status);
  const again = await call(`/billing/invoices/${id}/payments`, "POST", { amountCents: 500 }, { "Idempotency-Key": "k1" });
  expect(again.status).toBe(p1.status);
  expect(again.text).toBe(p1.text);
  expect((await call(`/billing/invoices/${id}/payments`, "POST", { amountCents: 600 }, { "Idempotency-Key": "k1" })).status).toBe(409);
  expect((await call(`/billing/invoices/${id}/payments`, "POST", { amountCents: 1501 }, { "Idempotency-Key": "k2" })).status).toBe(422);

  const aging = (await call("/billing/reports/aging")).body;
  expect(aging.USD.balanceCents).toBe(1500 + usd2.body.totalCents);
  expect(aging.USD.count).toBe(2);
  expect(aging.EUR.count).toBe(1);

  await call(`/billing/invoices/${id}/payments`, "POST", { amountCents: 1500 }, { "Idempotency-Key": "k3" });
  const paid = (await call(`/billing/invoices?status=paid`)).body.items;
  expect(paid.length).toBe(1);
  expect(paid[0].paidCents).toBe(2000);
  expect((await call(`/billing/invoices/${id}/void`, "POST")).status).toBe(409);
  expect((await call(`/billing/invoices/${eur.body.id}/void`, "POST")).status).toBe(200);

  for (let i = 0; i < 23; i++) await call("/billing/invoices", "POST", { customerId: c.body.id, currency: "BRL", lines });
  const page1 = (await call("/billing/invoices?limit=10")).body;
  expect(page1.items.length).toBe(10);
  expect(page1.nextCursor).toBeTruthy();
  const seen = new Set(page1.items.map((x: any) => x.id));
  let cursor = page1.nextCursor;
  let total = 10;
  while (cursor) {
    const page = (await call(`/billing/invoices?limit=10&cursor=${encodeURIComponent(cursor)}`)).body;
    for (const x of page.items) {
      expect(seen.has(x.id)).toBe(false);
      seen.add(x.id);
    }
    total += page.items.length;
    cursor = page.nextCursor;
  }
  expect(total).toBe(26);
  expect((await call(`/billing/invoices?customerId=${c.body.id}&limit=100`)).body.items.length).toBe(26);
});
