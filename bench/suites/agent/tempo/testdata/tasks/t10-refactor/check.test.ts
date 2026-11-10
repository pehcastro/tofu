import { expect, test } from "bun:test";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import app from "../src/index";

const post = (p: string, body: unknown) => app.request(p, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });

test("structure", () => {
  const root = join(import.meta.dir, "..", "src");
  expect(existsSync(join(root, "routes"))).toBe(true);
  expect(readdirSync(join(root, "routes")).length).toBeGreaterThanOrEqual(3);
  expect(existsSync(join(root, "lib", "validate.ts"))).toBe(true);
  expect(existsSync(join(root, "lib", "store.ts"))).toBe(true);
  expect(readFileSync(join(root, "index.ts"), "utf8").split("\n").length).toBeLessThan(60);
});

test("behaviour unchanged", async () => {
  expect((await post("/users", { name: "", email: "a@b" })).status).toBe(400);
  expect(await (await post("/users", { name: "a", email: "nope" })).json()).toEqual({ error: "email is invalid" });
  const u = await (await post("/users", { name: "a", email: "a@b" })).json();
  expect(await (await app.request("/users/x")).json()).toEqual({ error: "bad id" });
  expect(await (await app.request("/users/99")).json()).toEqual({ error: "user not found" });
  expect((await post("/posts", { title: "t", body: "b", userId: 99 })).status).toBe(404);
  const p = await (await post("/posts", { title: "t", body: "b", userId: u.id })).json();
  expect((await post(`/posts/${p.id}/comments`, { text: "" })).status).toBe(400);
  const cm = await (await post(`/posts/${p.id}/comments`, { text: "hi" })).json();
  const full = await (await app.request(`/posts/${p.id}`)).json();
  expect(full.comments.length).toBe(1);
  expect((await (await app.request(`/posts?userId=${u.id}`)).json()).length).toBe(1);
  expect((await app.request(`/comments/${cm.id}`, { method: "DELETE" })).status).toBe(204);
  expect(await (await app.request(`/comments/${cm.id}`, { method: "DELETE" })).json()).toEqual({ error: "comment not found" });
  expect((await app.request(`/posts/${p.id}`, { method: "DELETE" })).status).toBe(204);
  expect((await app.request(`/users/${u.id}`, { method: "DELETE" })).status).toBe(204);
});
