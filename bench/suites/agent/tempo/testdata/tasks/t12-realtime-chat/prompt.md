Build a realtime chat server with Hono on Bun.

- Rooms: `POST /rooms` `{name}` (201, 409 on duplicate name), `GET /rooms`, `GET /rooms/:id/messages?before=<id>&limit=n` (newest first, default 50, max 200), stored in SQLite (`bun:sqlite`, path from env `CHAT_DB`, default `chat.db`).
- WebSocket at `/ws?room=<id>&user=<name>` using Hono's Bun websocket helper. Export `websocket` from `src/index.ts` beside the default app so `Bun.serve({ fetch: app.fetch, websocket })` works.
- Client messages are JSON: `{"type":"message","text":string}` (1 to 2000 characters), `{"type":"typing"}`. Server broadcasts to everyone in the room `{"type":"message","id","user","text","at"}` after storing it, `{"type":"typing","user"}` to others, and `{"type":"presence","users":[...]}` whenever someone joins or leaves.
- Invalid messages get `{"type":"error","error":string}` back to the sender only.
- Rate limit: more than 5 messages per second from one connection closes it with code 1008.
- Write tests with `bun test` that start a real server on a random port, connect two clients, and check broadcast, presence, history and the rate limit.
