Build a small products API and publish its OpenAPI 3.1 document.

- `GET /products`, `POST /products` `{name: string, price: number > 0, sku: string matching ^[A-Z]{3}-\d{4}$}`, `GET /products/:id`, `DELETE /products/:id`. In-memory. Validation errors return 400 `{"error": "validation", "issues": [...]}`; unknown ids 404.
- Use `@hono/zod-openapi` (with zod) so the routes and the document come from the same schemas.
- `GET /openapi.json` returns the document: `openapi` is `3.1.0`, `info.title` is `Products`, and every route above appears under `paths` with its request body schema and response codes.
- `GET /docs` serves a page that renders the document (Swagger UI or Scalar via a CDN is fine).
- Write tests with `bun test`.
