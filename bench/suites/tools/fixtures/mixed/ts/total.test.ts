import { test } from "node:test";
import assert from "node:assert/strict";
import { total } from "./total.ts";

test("total", () => {
  const got = total(3, 250);
  const want = 750;
  assert.equal(got, want, `got ${got}, want ${want}`);
});
