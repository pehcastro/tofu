# Decisions

Standing decisions about this service. Each one is here because somebody built the other thing first and it was taken back out.

## 2026-03-02 nothing runs on a schedule

The process is started and stopped constantly, in development and once per test file, and it holds no timer. Anything that depends on the clock is worked out while a request is being served and never on a schedule.

A sweeper was merged in February and reverted eight days later. It fired after the test run had finished, kept the suite alive for its whole interval, and made every timing assertion depend on when the interval happened to land. Whatever the sweep was for, computing it on read gave the same answer and gave it exactly.

## 2026-04-11 the server never removes a note on its own initiative

A note leaves the store when a client asks for it to leave, and at no other moment.

Keeping a note out of a listing is not removing it. A note that has been kept out of one listing is still reachable through another, because a client that put it there by mistake otherwise has no way back and no way to see that it happened.

## 2026-05-20 no dependency

This service has no runtime dependency and is not going to acquire one. `src/router.ts` is the whole of the HTTP layer and it is enough. `package.json` carries a dev dependency on TypeScript and on the Bun types, and nothing else belongs there.

The build machine has no network. A package added here does not fail review, it fails to install.
