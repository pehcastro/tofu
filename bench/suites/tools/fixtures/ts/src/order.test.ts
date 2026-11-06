import { test } from "node:test";
import assert from "node:assert/strict";
import { Order } from "./order.ts";

test("total", () => {
  const got = new Order(3, 250).total();
  const want = 750;
  assert.equal(got, want, `got ${got}, want ${want}`);
});

test("discounted", () => {
  const got = new Order(3, 250).discounted();
  const want = 675;
  assert.equal(got, want, `got ${got}, want ${want}`);
});

test("shipping", { skip: "shipping rates are not modelled yet" }, () => {});
