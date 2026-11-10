import { expect, test } from "bun:test";
import { tmpdir } from "node:os";
import { join } from "node:path";

const path = join(tmpdir(), `pm-check-${Date.now()}.db`);
process.env.PM_DB = path;
process.env.JWT_SECRET = "check-secret";

test("projects app", async () => {
  const { openDb } = await import("../src/db");
  openDb(path);
  const app = (await import("../src/index")).default;
  const call = (p: string, method: string, token?: string, body?: unknown) =>
    app.request(p, { method, headers: { "content-type": "application/json", ...(token ? { authorization: `Bearer ${token}` } : {}) }, body: body === undefined ? undefined : JSON.stringify(body) });
  const login = async (email: string, password: string) => (await (await call("/auth/login", "POST", undefined, { email, password })).json()).token
    ?? (await (await call("/auth/login", "POST", undefined, { email, password })).json()).accessToken;

  expect([200, 201]).toContain((await call("/auth/register", "POST", undefined, { orgName: "A", email: "o@a.co", password: "password1", name: "O" })).status);
  expect([200, 201]).toContain((await call("/auth/register", "POST", undefined, { orgName: "B", email: "o@b.co", password: "password1", name: "P" })).status);
  const ta = await login("o@a.co", "password1");
  const tb = await login("o@b.co", "password1");
  expect(typeof ta).toBe("string");
  expect((await call("/projects", "GET")).status).toBe(401);

  const member = await call("/users", "POST", ta, { email: "m@a.co", password: "password1", name: "M", role: "member" });
  expect([200, 201]).toContain(member.status);
  const memberBody = await member.json();
  const tm = await login("m@a.co", "password1");

  const proj = await (await call("/projects", "POST", ta, { name: "P1" })).json();
  expect((await call("/projects", "POST", tm, { name: "nope" })).status).toBe(403);
  expect((await call(`/projects/${proj.id}`, "GET", tb)).status).toBe(404);

  for (let i = 0; i < 25; i++) {
    const r = await call(`/projects/${proj.id}/tasks`, "POST", ta, { title: `Task ${i}`, description: i % 5 ? "x" : "Needle", status: i % 3 ? "todo" : "done", priority: (i % 4) + 1, assigneeId: memberBody.id });
    expect([200, 201]).toContain(r.status);
  }
  const page1 = await (await call(`/projects/${proj.id}/tasks?limit=10`, "GET", tm)).json();
  expect(page1.items.length).toBe(10);
  expect(page1.nextCursor).toBeTruthy();
  const page2 = await (await call(`/projects/${proj.id}/tasks?limit=10&cursor=${encodeURIComponent(page1.nextCursor)}`, "GET", tm)).json();
  expect(page2.items[0].id).not.toBe(page1.items[0].id);
  const needle = await (await call(`/projects/${proj.id}/tasks?q=needle&limit=100`, "GET", tm)).json();
  expect(needle.items.length).toBe(5);
  const done = await (await call(`/projects/${proj.id}/tasks?status=done&limit=100`, "GET", tm)).json();
  expect(done.items.every((t: any) => t.status === "done")).toBe(true);
  const sorted = await (await call(`/projects/${proj.id}/tasks?sort=priority&order=desc&limit=100`, "GET", tm)).json();
  expect(sorted.items[0].priority).toBe(4);
  expect((await call(`/projects/${proj.id}/tasks`, "POST", ta, { title: "x", status: "weird", priority: 1 })).status).toBe(400);
  const task = page1.items[0];
  expect([200, 201]).toContain((await call(`/tasks/${task.id}/comments`, "POST", tm, { text: "hi" })).status);
  expect((await call(`/tasks/${task.id}/comments`, "GET", tb)).status).toBe(404);
  const stats = await (await call("/stats", "GET", ta)).json();
  expect(JSON.stringify(stats)).toContain("done");
});
