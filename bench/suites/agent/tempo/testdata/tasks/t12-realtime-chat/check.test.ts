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

test("realtime chat", async () => {
  const mod = await import("../src/index");
  const server = Bun.serve({ port: 0, fetch: mod.default.fetch, websocket: mod.websocket });
  const base = `http://localhost:${server.port}`;
  try {
    const room = await (await fetch(`${base}/rooms`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ name: "general" }) })).json();
    expect((await fetch(`${base}/rooms`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ name: "general" }) })).status).toBe(409);
    const a = new WebSocket(`ws://localhost:${server.port}/ws?room=${room.id}&user=ann`);
    await new Promise((r) => a.addEventListener("open", r));
    const presence = next(a, "presence");
    const b = new WebSocket(`ws://localhost:${server.port}/ws?room=${room.id}&user=bob`);
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
    const hist = await (await fetch(`${base}/rooms/${room.id}/messages`)).json();
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
