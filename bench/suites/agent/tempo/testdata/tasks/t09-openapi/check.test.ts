import { expect, test } from "bun:test";
import app from "../src/index";

const post = (body: unknown) => app.request("/products", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });

test("openapi products", async () => {
  expect((await post({ name: "a", price: 1, sku: "bad" })).status).toBe(400);
  const ok = await post({ name: "a", price: 1, sku: "ABC-1234" });
  expect([200, 201]).toContain(ok.status);
  const p = await ok.json();
  expect((await app.request(`/products/${p.id}`)).status).toBe(200);
  expect((await app.request("/products/nope")).status).toBe(404);
  const doc = await (await app.request("/openapi.json")).json();
  expect(doc.openapi).toBe("3.1.0");
  expect(doc.info.title).toBe("Products");
  const paths = Object.keys(doc.paths);
  expect(paths.some((x) => x === "/products")).toBe(true);
  expect(paths.some((x) => x.startsWith("/products/{"))).toBe(true);
  expect(doc.paths["/products"].post.requestBody).toBeDefined();
  expect((await app.request("/docs")).status).toBe(200);
});
