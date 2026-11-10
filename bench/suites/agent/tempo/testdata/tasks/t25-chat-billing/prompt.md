Build one Hono app on Bun with these modules, each in its own folder under `src/`, mounted on one app exported as the default export of `src/index.ts`. Share what is shared (validation helpers, error shape, pagination) instead of writing it more than once.

## Module: chat, under `/chat`

Build a realtime chat server with Hono on Bun.

- Rooms: `POST /chat/rooms` `{name}` (201, 409 on duplicate name), `GET /chat/rooms`, `GET /chat/rooms/:id/messages?before=<id>&limit=n` (newest first, default 50, max 200), stored in SQLite (`bun:sqlite`, path from env `CHAT_DB`, default `chat.db`).
- WebSocket at `/chat/ws?room=<id>&user=<name>` using Hono's Bun websocket helper. Export `websocket` from `src/index.ts` beside the default app so `Bun.serve({ fetch: app.fetch, websocket })` works.
- Client messages are JSON: `{"type":"message","text":string}` (1 to 2000 characters), `{"type":"typing"}`. Server broadcasts to everyone in the room `{"type":"message","id","user","text","at"}` after storing it, `{"type":"typing","user"}` to others, and `{"type":"presence","users":[...]}` whenever someone joins or leaves.
- Invalid messages get `{"type":"error","error":string}` back to the sender only.
- Rate limit: more than 5 messages per second from one connection closes it with code 1008.

## Module: billing, under `/billing`

Build an invoicing API with Hono on Bun, stored in SQLite (`bun:sqlite`, path from env `INV_DB`, default `inv.db`).  All money is integer cents.

- Customers: `POST /billing/customers` `{name, email}` (201; 400 on a missing name or an email without `@`; 409 on a duplicate email), `GET /billing/customers/:id` (404 when missing).
- Invoices: `POST /billing/invoices` `{customerId, currency, lines:[{description, quantity, unitCents, taxRate}]}` where `currency` is one of `USD`, `EUR`, `BRL`, `quantity` is a positive integer, `unitCents` a non-negative integer, `taxRate` a number from 0 to 0.5. 201 returns the invoice; 400 on any invalid field; 404 on an unknown customer.
- An invoice is `{id, customerId, currency, status, lines, subtotalCents, taxCents, totalCents, paidCents, number}`. Per line, tax is `Math.round(quantity * unitCents * taxRate)`. `subtotalCents` sums `quantity * unitCents`, `taxCents` sums line taxes, `totalCents` is their sum. `number` is null until issued.
- Status moves `draft -> issued -> paid`, or `draft|issued -> void`. `POST /billing/invoices/:id/issue` gives the next number in the form `INV-0001`, counting per currency, and 409 when the invoice is not a draft. `POST /billing/invoices/:id/void` returns 409 on a paid invoice. Lines are editable only on a draft: `PUT /billing/invoices/:id/lines` with a body that is the array of lines, 409 otherwise. Issue, void and the lines edit return the updated invoice with 200.
- Payments: `POST /billing/invoices/:id/payments` `{amountCents}` with header `Idempotency-Key`. 400 without the header or with a non-positive amount, 409 unless the invoice is issued, 422 when the payment would exceed the remaining balance. The same key with the same body returns the first response again with the same status and body and records nothing new. The same key with a different body is 409. When `paidCents` reaches `totalCents` the status becomes `paid`.
- `GET /billing/invoices?status=&customerId=&limit=&cursor=` returns `{items, nextCursor}`, newest first, default limit 20, max 100; `nextCursor` is null on the last page.
- `GET /billing/reports/aging` returns, for issued invoices with a balance, `{currency: {count, balanceCents}}`.

## Tests

Write tests with `bun test` for every module, covering what each module's rules say.
