The cart API in `src/index.ts` has bugs. Users report:

- Two items added one after another get the same id.
- Adding an item should return 201, and it returns 200.
- The cart total ignores quantities.
- Deleting an item by id deletes the wrong item, and deleting an unknown id deletes the last item instead of returning 404 with `{"error":"not found"}`.
- The discount code `TEN` should take 10% off, and the total comes back negative.
- A POST without a name or with a negative price should return 400 with `{"error": string}`.

Find and fix every bug, and add tests with `bun test` that would have caught each one.
