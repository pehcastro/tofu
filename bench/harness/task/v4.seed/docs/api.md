# API

Everything is under `/v1` and everything takes and returns JSON. Authentication is a bearer token that names an account.

## GET /v1/health

Returns `{ "ok": true }` and takes no credential.

## GET /v1/accounts/me

Returns the calling account: its id, its name and its plan.

## GET /v1/usage

Returns the row counts for the current billing window.

## POST /v1/reports

Creates a report from `{ "range": { "from": string, "to": string }, "groupBy": string[] }` and returns its id.

## POST /v1/exports

Creates an export of a report from `{ "reportId": string, "format": string }` and returns the export id and its state.

`format` is one of the export formats the build knows. A format the build does not know is rejected.

## GET /v1/exports/:id

Returns the export and, once it is ready, the bytes it produced.

## Errors

Every error is `{ "error": { "code": string, "message": string } }` with the HTTP status that matches. The codes are in `src/errors/codes.ts`.
