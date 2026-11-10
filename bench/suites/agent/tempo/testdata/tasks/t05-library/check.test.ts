import { expect, test } from "bun:test";
import app from "../src/index";

const send = (path: string, method: string, body?: unknown) =>
  app.request(path, { method, headers: { "content-type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body) });

test("library", async () => {
  const author = await (await send("/authors", "POST", { name: "Le Guin", born: 1929 })).json();
  expect((await send("/books", "POST", { title: "x", authorId: "missing", year: 1, tags: [], available: true })).status).toBe(400);
  const bad = await (await send("/authors", "POST", { name: 5 })).json();
  expect(bad.error).toBe("validation");
  for (let i = 0; i < 12; i++) {
    const r = await send("/books", "POST", { title: `Earthsea ${i}`, authorId: author.id, year: 1968 + i, tags: i % 2 ? ["fantasy"] : ["sf"], available: true });
    expect(r.status).toBe(201);
  }
  const page = await (await send("/books?limit=5&page=2", "GET")).json();
  expect(page.items.length).toBe(5);
  expect(page.total).toBe(12);
  const fantasy = await (await send("/books?tag=fantasy&q=EARTHSEA", "GET")).json();
  expect(fantasy.total).toBe(6);
  expect((await send(`/authors/${author.id}`, "DELETE")).status).toBe(409);
  const book = page.items[0];
  const loan = await send("/loans", "POST", { bookId: book.id, member: "m1" });
  expect(loan.status).toBe(201);
  expect((await send("/loans", "POST", { bookId: book.id, member: "m2" })).status).toBe(409);
  const loanBody = await loan.json();
  const open = await (await send("/loans?member=m1&open=true", "GET")).json();
  expect(Array.isArray(open) ? open.length : open.items.length).toBe(1);
  expect((await send(`/loans/${loanBody.id}/return`, "POST")).status).toBe(200);
  expect((await send(`/loans/${loanBody.id}/return`, "POST")).status).toBe(409);
  expect((await (await send(`/books/${book.id}`, "GET")).json()).available).toBe(true);
  const byAuthor = await (await send(`/authors/${author.id}/books`, "GET")).json();
  expect(Array.isArray(byAuthor) ? byAuthor.length : byAuthor.items.length).toBeGreaterThan(0);
  expect((await send("/nowhere", "GET")).status).toBe(404);
  expect((await send("/books/nope", "GET")).status).toBe(404);
});
