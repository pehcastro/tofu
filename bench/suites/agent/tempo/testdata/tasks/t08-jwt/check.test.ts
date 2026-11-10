import { expect, test } from "bun:test";
import app from "../src/index";

const post = (p: string, body: unknown) => app.request(p, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });

test("jwt auth", async () => {
  process.env.JWT_SECRET = "check-secret";
  expect((await post("/auth/signup", { email: "bad", password: "longenough" })).status).toBe(400);
  expect((await post("/auth/signup", { email: "a@b.co", password: "short" })).status).toBe(400);
  const s = await post("/auth/signup", { email: "a@b.co", password: "longenough" });
  expect(s.status).toBe(201);
  const user = await s.json();
  expect(JSON.stringify(user)).not.toContain("longenough");
  expect((await post("/auth/signup", { email: "a@b.co", password: "longenough" })).status).toBe(409);
  expect((await post("/auth/login", { email: "a@b.co", password: "wrongwrong" })).status).toBe(401);
  const pair = await (await post("/auth/login", { email: "a@b.co", password: "longenough" })).json();
  const me = await app.request("/me", { headers: { authorization: `Bearer ${pair.accessToken}` } });
  expect(me.status).toBe(200);
  expect((await me.json()).email).toBe("a@b.co");
  expect((await app.request("/me", { headers: { authorization: "Bearer x.y.z" } })).status).toBe(401);
  const next = await post("/auth/refresh", { refreshToken: pair.refreshToken });
  expect(next.status).toBe(200);
  expect((await post("/auth/refresh", { refreshToken: pair.refreshToken })).status).toBe(401);
  const np = await next.json();
  expect((await post("/auth/logout", { refreshToken: np.refreshToken })).status).toBe(204);
  expect((await post("/auth/refresh", { refreshToken: np.refreshToken })).status).toBe(401);
});
