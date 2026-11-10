import { expect, test } from "bun:test";
import app from "../src/index";

const json = (method: string, body: unknown) => ({ method, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });

test("todos lifecycle", async () => {
  expect((await app.request("/todos", json("POST", { title: "" }))).status).toBe(400);
  const created = await app.request("/todos", json("POST", { title: "a" }));
  expect(created.status).toBe(201);
  const todo = await created.json();
  expect(todo.done).toBe(false);
  await app.request("/todos", json("POST", { title: "b", done: true }));
  const done = await (await app.request("/todos?done=true")).json();
  expect(done.every((t: any) => t.done)).toBe(true);
  expect((await app.request(`/todos/${todo.id}`)).status).toBe(200);
  expect((await app.request("/todos/nope")).status).toBe(404);
  const patched = await app.request(`/todos/${todo.id}`, json("PATCH", { done: true }));
  expect((await patched.json()).done).toBe(true);
  expect((await app.request(`/todos/${todo.id}`, json("PATCH", { title: "" }))).status).toBe(400);
  expect((await app.request(`/todos/${todo.id}`, { method: "DELETE" })).status).toBe(204);
  expect((await app.request(`/todos/${todo.id}`, { method: "DELETE" })).status).toBe(404);
});
