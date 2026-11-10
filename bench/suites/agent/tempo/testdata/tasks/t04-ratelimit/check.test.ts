import { expect, test } from "bun:test";
import app from "../src/index";
import { resetRateLimits, setNow } from "../src/ratelimit";

test("rate limit", async () => {
  let now = 1_000_000;
  setNow(() => now);
  resetRateLimits();
  const h = { headers: { "x-forwarded-for": "9.9.9.9" } };
  for (let i = 0; i < 5; i++) {
    const r = await app.request("/", h);
    expect(r.status).toBe(200);
    expect(r.headers.get("x-ratelimit-limit")).toBe("5");
  }
  const blocked = await app.request("/", h);
  expect(blocked.status).toBe(429);
  expect(Number(blocked.headers.get("retry-after"))).toBeGreaterThan(0);
  expect((await app.request("/", { headers: { "x-forwarded-for": "1.1.1.1" } })).status).toBe(200);
  now += 61_000;
  expect((await app.request("/", h)).status).toBe(200);
});
