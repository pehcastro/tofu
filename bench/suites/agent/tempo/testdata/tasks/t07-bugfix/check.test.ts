import { expect, test } from "bun:test";
import app from "../src/index";

const post = (body: unknown) => app.request("/cart", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });

test("cart fixed", async () => {
  const a = await post({ name: "a", price: 10, qty: 2 });
  expect(a.status).toBe(201);
  const b = await post({ name: "b", price: 5 });
  const ia = await a.json();
  const ib = await b.json();
  expect(ia.id).not.toBe(ib.id);
  expect((await post({ price: 1 })).status).toBe(400);
  expect((await post({ name: "x", price: -1 })).status).toBe(400);
  expect((await (await app.request("/cart")).json()).total).toBe(25);
  expect((await (await app.request("/cart/discount/TEN")).json()).total).toBeCloseTo(22.5);
  expect((await app.request("/cart/999", { method: "DELETE" })).status).toBe(404);
  expect((await app.request(`/cart/${ia.id}`, { method: "DELETE" })).status).toBe(204);
  const left = (await (await app.request("/cart")).json()).items;
  expect(left.length).toBe(1);
  expect(left[0].id).toBe(ib.id);
});
