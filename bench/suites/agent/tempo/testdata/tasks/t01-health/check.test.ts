import { expect, test } from "bun:test";
import app from "../src/index";

test("health", async () => {
  const res = await app.request("/health");
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(body.status).toBe("ok");
  expect(typeof body.uptime).toBe("number");
});
