import { expect, test } from "bun:test";
import { tmpdir } from "node:os";
import { join } from "node:path";

const path = join(tmpdir(), `notes-check-${Date.now()}.db`);
process.env.NOTES_DB = path;

test("notes", async () => {
  const { openDb } = await import("../src/db");
  openDb(path);
  openDb(path);
  const app = (await import("../src/index")).default;
  const send = (p: string, method: string, body?: unknown) =>
    app.request(p, { method, headers: { "content-type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body) });
  const created = await send("/notes", "POST", { title: "Groceries", body: "Buy Tofu", tags: ["home"] });
  expect(created.status).toBe(201);
  const note = await created.json();
  expect(note.tags).toEqual(["home"]);
  await send("/notes", "POST", { title: "Work", body: "ship report", tags: ["job"] });
  expect((await (await send("/notes?q=tofu", "GET")).json()).length).toBe(1);
  expect((await (await send("/notes?tag=job", "GET")).json()).length).toBe(1);
  expect((await send(`/notes/${note.id}`, "PUT", { title: "G", body: "b", tags: [] })).status).toBe(200);
  expect((await send(`/notes/${note.id}`, "DELETE")).status).toBe(204);
  expect((await send(`/notes/${note.id}`, "GET")).status).toBe(404);
});
