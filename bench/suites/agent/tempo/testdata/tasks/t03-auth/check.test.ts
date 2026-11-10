import { expect, test } from "bun:test";
import app from "../src/index";

test("bearer auth", async () => {
  process.env.API_TOKEN = "s3cret";
  expect((await app.request("/api/me")).status).toBe(401);
  expect((await app.request("/api/me", { headers: { authorization: "Bearer nope" } })).status).toBe(403);
  const ok = await app.request("/api/me", { headers: { authorization: "Bearer s3cret" } });
  expect(ok.status).toBe(200);
  expect((await ok.json()).ok).toBe(true);
  expect((await app.request("/")).status).toBe(200);
});
