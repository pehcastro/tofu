# Decisions taken for expiry

## "out of the way" is hidden, not deleted

Point 3 of the task says an expired note must not linger, which reads either as removing it from the store or as keeping it out of the default listing. Point 4 settles it on its own, because a note that had been deleted could not appear under `GET /notes?expired=true`, and `docs/decisions.md` of 2026-04-11 says the same thing: the server never removes a note on its own initiative, and a note kept out of one listing stays reachable through another.

An expired note is therefore still in the store, still has its id, and is still returned by the expired listing. `GET /notes/:id` answers 404 for it because the task asks for that in as many words.

## expiry is worked out while the request is served

`docs/decisions.md` of 2026-03-02 forbids anything on a schedule, and point 3 of the task asks for a note to be expired from the moment its expiry passes rather than from some later moment. A sweeper cannot give that answer between two of its ticks. `hasExpired` in `src/store.ts` compares the expiry against the clock on every read, so the answer is exact and no timer exists.

## nothing was added to package.json

`docs/decisions.md` of 2026-05-20 says this service has no runtime dependency and the build machine has no network. Parsing and formatting a timestamp is `Date.parse` and `toISOString`, so no date library was needed.
