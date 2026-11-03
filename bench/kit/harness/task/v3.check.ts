type Status = "pass" | "fail" | "could_not_evaluate" | "recorded"

type Line = { item: number | string; status: Status; reason?: string }

type Outcome = Omit<Line, "item">

type App = { request: (path: string, init?: RequestInit) => Promise<Response> }

type Body = Record<string, unknown>

function pass(): Outcome {
  return { status: "pass" }
}

function fail(reason: string): Outcome {
  return { status: "fail", reason }
}

function cne(reason: string): Outcome {
  return { status: "could_not_evaluate", reason }
}

function recorded(reason: string): Outcome {
  return { status: "recorded", reason }
}

function emit(item: number | string, outcome: Outcome) {
  console.log(JSON.stringify({ item, ...outcome } satisfies Line))
}

async function readJSON(res: Response): Promise<unknown> {
  const text = await res.text()
  if (text === "") return undefined
  try {
    return JSON.parse(text)
  } catch {
    return undefined
  }
}

function jsonInit(method: string, body: unknown): RequestInit {
  return { method, headers: { "content-type": "application/json" }, body: JSON.stringify(body) }
}

const malformedInit: RequestInit = { method: "POST", headers: { "content-type": "application/json" }, body: "not json" }

const unknownID = "note_nobody_issued"

type Sent = { status: number; body: Body | undefined; raw: unknown }

async function send(app: App, path: string, init?: RequestInit): Promise<Sent> {
  const res = await app.request(path, init)
  const raw = await readJSON(res)
  const body = raw && typeof raw === "object" && !Array.isArray(raw) ? (raw as Body) : undefined
  return { status: res.status, body, raw }
}

async function post(app: App, body: unknown): Promise<Sent> {
  return send(app, "/notes", jsonInit("POST", body))
}

async function patch(app: App, id: string, body: unknown): Promise<Sent> {
  return send(app, `/notes/${id}`, jsonInit("PATCH", body))
}

type Created = { id: string; note: Body } | { error: string }

async function create(app: App, body: Body): Promise<Created> {
  const sent = await post(app, body)
  if (sent.status !== 200 && sent.status !== 201) {
    return { error: `POST /notes ${JSON.stringify(body)} returned ${sent.status}` }
  }
  if (!sent.body || sent.body.id === undefined) {
    return { error: `POST /notes ${JSON.stringify(body)} returned no id in its body` }
  }
  return { id: String(sent.body.id), note: sent.body }
}

function idsOf(list: unknown): string[] {
  if (!Array.isArray(list)) return []
  const ids: string[] = []
  for (const entry of list) {
    if (entry && typeof entry === "object" && (entry as Body).id !== undefined) ids.push(String((entry as Body).id))
  }
  return ids
}

async function listed(app: App, query: string): Promise<string[] | { error: string }> {
  const sent = await send(app, query)
  if (sent.status !== 200) return { error: `GET ${query} returned ${sent.status}, want 200` }
  if (!Array.isArray(sent.raw)) return { error: `GET ${query} did not return a JSON array` }
  return idsOf(sent.raw)
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function inFuture(ms: number): string {
  return new Date(Date.now() + ms).toISOString()
}

function rejected(label: string, sent: Sent, want: number): Outcome {
  if (sent.status !== want) return fail(`${label} returned ${sent.status}, want ${want}`)
  if (!sent.body) return fail(`${label} returned ${want} but its body is not a JSON object`)
  return pass()
}

async function item1(app: App): Promise<Outcome> {
  const res = await app.request("/")
  if (res.status !== 200) return fail(`GET / returned ${res.status}, want 200`)
  const text = await res.text()
  if (text !== "notes") return fail(`GET / body was ${JSON.stringify(text)}, want "notes"`)
  return pass()
}

async function item2(app: App): Promise<Outcome> {
  const made = await create(app, { title: "v3 lifecycle", body: "a body" })
  if ("error" in made) return fail(made.error)
  if (made.note.title !== "v3 lifecycle") return fail(`POST /notes echoed title ${JSON.stringify(made.note.title)}`)

  const read = await send(app, `/notes/${made.id}`)
  if (read.status !== 200 || !read.body || read.body.title !== "v3 lifecycle") {
    return fail(`GET /notes/${made.id} returned ${read.status} ${JSON.stringify(read.raw)}`)
  }

  const all = await listed(app, "/notes")
  if ("error" in all) return fail(all.error)
  if (!all.includes(made.id)) return fail(`GET /notes did not contain the created note ${made.id}`)

  const deleted = await app.request(`/notes/${made.id}`, { method: "DELETE" })
  if (deleted.status !== 200 && deleted.status !== 204) {
    return fail(`DELETE /notes/${made.id} returned ${deleted.status}, want 200 or 204`)
  }
  const gone = await app.request(`/notes/${made.id}`)
  if (gone.status !== 404) return fail(`GET after DELETE returned ${gone.status}, want 404`)
  return pass()
}

async function item3(app: App): Promise<Outcome> {
  const empty = await post(app, {})
  if (empty.status !== 400) return fail(`POST /notes with {} returned ${empty.status}, want 400`)
  const blank = await post(app, { title: "", body: "" })
  if (blank.status !== 400) return fail(`POST /notes with an empty title returned ${blank.status}, want 400`)
  const long = await post(app, { title: "a".repeat(121), body: "" })
  if (long.status !== 400) return fail(`POST /notes with a 121-character title returned ${long.status}, want 400`)
  try {
    const bad = await app.request("/notes", malformedInit)
    if (bad.status !== 400) return fail(`POST /notes with a malformed body returned ${bad.status}, want 400`)
  } catch (e) {
    return fail(`POST /notes with a malformed body threw: ${String(e)}`)
  }
  return pass()
}

async function item4(app: App): Promise<Outcome> {
  const calls: Array<[string, RequestInit | undefined]> = [
    ["GET", undefined],
    ["PATCH", jsonInit("PATCH", { title: "x" })],
    ["DELETE", { method: "DELETE" }],
  ]
  for (const [verb, init] of calls) {
    const sent = await send(app, `/notes/${unknownID}`, init)
    if (sent.status !== 404) return fail(`${verb} /notes/<unknown> returned ${sent.status}, want 404`)
    if (!sent.body) return fail(`${verb} /notes/<unknown> body did not parse as a JSON object`)
  }
  return pass()
}

async function item5(app: App): Promise<Outcome> {
  const at = inFuture(60_000)
  const made = await create(app, { title: "v3 expiry echoed", body: "", expiresAt: at })
  if ("error" in made) return fail(made.error)
  const read = await send(app, `/notes/${made.id}`)
  if (read.status !== 200 || !read.body) return fail(`GET /notes/${made.id} returned ${read.status}`)
  const got = read.body.expiresAt
  if (got === undefined || got === null) {
    return fail(`the note has no expiresAt after it was created with one: ${JSON.stringify(read.body)}`)
  }
  if (Date.parse(String(got)) !== Date.parse(at)) {
    return fail(`expiresAt read back as ${JSON.stringify(got)}, want the same instant as ${at}`)
  }
  return pass()
}

async function item8(app: App): Promise<Outcome> {
  const made = await create(app, { title: "v3 never expires", body: "" })
  if ("error" in made) return fail(made.error)
  const read = await send(app, `/notes/${made.id}`)
  if (read.status !== 200) return fail(`a note created without an expiry returned ${read.status} on read`)
  const all = await listed(app, "/notes")
  if ("error" in all) return fail(all.error)
  if (!all.includes(made.id)) return fail("a note created without an expiry is missing from GET /notes")
  return pass()
}

async function item9(app: App): Promise<Outcome> {
  const made = await create(app, { title: "v3 patch sets expiry", body: "" })
  if ("error" in made) return fail(made.error)
  const at = inFuture(60_000)
  const sent = await patch(app, made.id, { expiresAt: at })
  if (sent.status !== 200) return fail(`PATCH {"expiresAt":...} returned ${sent.status}, want 200`)
  const read = await send(app, `/notes/${made.id}`)
  if (!read.body || Date.parse(String(read.body.expiresAt)) !== Date.parse(at)) {
    return fail(`expiresAt read back as ${JSON.stringify(read.body?.expiresAt)}, want ${at}`)
  }
  return pass()
}

async function item10(app: App): Promise<Outcome> {
  const made = await create(app, { title: "v3 patch clears expiry", body: "", expiresAt: inFuture(400) })
  if ("error" in made) return fail(made.error)
  const cleared = await patch(app, made.id, { expiresAt: null })
  if (cleared.status !== 200) return fail(`PATCH {"expiresAt":null} returned ${cleared.status}, want 200`)
  await sleep(700)
  const read = await send(app, `/notes/${made.id}`)
  if (read.status !== 200) {
    return fail(`a note whose expiry was cleared returned ${read.status} after the old expiry passed, want 200`)
  }
  const got = read.body?.expiresAt
  if (got !== undefined && got !== null) return fail(`expiresAt is still ${JSON.stringify(got)} after being cleared`)
  return pass()
}

async function itemExact(app: App): Promise<Outcome> {
  const made = await create(app, { title: "v3 expires on the instant", body: "", expiresAt: inFuture(150) })
  if ("error" in made) return fail(made.error)
  await sleep(300)
  const read = await send(app, `/notes/${made.id}`)
  if (read.status !== 404) {
    return fail(`300 ms after an expiry 150 ms away, GET /notes/<id> returned ${read.status}, want 404: expiry is not worked out when the request is served`)
  }
  return pass()
}

type Fixture = { live: string; first: string; second: string }

const firstExpiryMs = 700
const secondExpiryMs = 1400
const settleMs = 2200

async function buildFixture(app: App): Promise<Fixture | { error: string }> {
  const live = await create(app, { title: "v3 fixture live", body: "" })
  if ("error" in live) return { error: `building the expiry fixture: ${live.error}` }
  const first = await create(app, { title: "v3 fixture first", body: "", expiresAt: inFuture(firstExpiryMs) })
  if ("error" in first) return { error: `building the expiry fixture: ${first.error}` }
  const second = await create(app, { title: "v3 fixture second", body: "", expiresAt: inFuture(secondExpiryMs) })
  if ("error" in second) return { error: `building the expiry fixture: ${second.error}` }
  return { live: live.id, first: first.id, second: second.id }
}

async function item12(app: App, f: Fixture): Promise<Outcome> {
  const all = await listed(app, "/notes")
  if ("error" in all) return fail(all.error)
  for (const id of [f.live, f.first, f.second]) {
    if (!all.includes(id)) return fail(`GET /notes omitted ${id}, which has not expired yet`)
  }
  const read = await send(app, `/notes/${f.first}`)
  if (read.status !== 200) return fail(`GET /notes/${f.first} returned ${read.status} before its expiry, want 200`)
  return pass()
}

async function item13(app: App, f: Fixture): Promise<Outcome> {
  const all = await listed(app, "/notes")
  if ("error" in all) return fail(all.error)
  const lingering = [f.first, f.second].filter((id) => all.includes(id))
  if (lingering.length > 0) return fail(`GET /notes still lists expired notes ${lingering.join(", ")}`)
  if (!all.includes(f.live)) return fail(`GET /notes dropped ${f.live}, which has no expiry at all`)
  return pass()
}

async function item14(app: App, f: Fixture): Promise<Outcome> {
  const read = await send(app, `/notes/${f.first}`)
  if (read.status !== 404) return fail(`GET /notes/${f.first} returned ${read.status} after its expiry, want 404`)
  return pass()
}

async function item15(app: App, f: Fixture): Promise<Outcome> {
  const expired = await listed(app, "/notes?expired=true")
  if ("error" in expired) return fail(expired.error)
  const missing = [f.first, f.second].filter((id) => !expired.includes(id))
  if (missing.length > 0) {
    return fail(`GET /notes?expired=true omitted ${missing.join(", ")}: an expired note has to stay reachable somewhere`)
  }
  if (expired.includes(f.live)) return fail(`GET /notes?expired=true listed ${f.live}, which has no expiry`)
  return pass()
}

async function item16(app: App, f: Fixture): Promise<Outcome> {
  const expired = await listed(app, "/notes?expired=true")
  if ("error" in expired) return fail(expired.error)
  const first = expired.indexOf(f.first)
  const second = expired.indexOf(f.second)
  if (first < 0 || second < 0) return fail(`GET /notes?expired=true did not carry both fixture notes`)
  if (second > first) {
    return fail(`GET /notes?expired=true put ${f.first} before ${f.second}, and ${f.second} expired more recently`)
  }
  return pass()
}

async function item17(app: App, f: Fixture): Promise<Outcome> {
  const live = await listed(app, "/notes?expired=false")
  if ("error" in live) return fail(live.error)
  if (!live.includes(f.live)) return fail(`GET /notes?expired=false omitted ${f.live}, which has not expired`)
  const wrong = [f.first, f.second].filter((id) => live.includes(id))
  if (wrong.length > 0) return fail(`GET /notes?expired=false listed expired notes ${wrong.join(", ")}`)
  return pass()
}

async function item18(app: App): Promise<Outcome> {
  const sent = await send(app, "/notes?expired=maybe")
  if (sent.status !== 400) return fail(`GET /notes?expired=maybe returned ${sent.status}, want 400`)
  return pass()
}

async function walk(dir: string, base: string, out: string[]) {
  const fs = await import("node:fs/promises")
  const path = await import("node:path")
  for (const entry of await fs.readdir(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name === ".git") continue
    const full = path.join(dir, entry.name)
    const rel = path.join(base, entry.name)
    if (entry.isDirectory()) await walk(full, rel, out)
    else out.push(rel)
  }
}

async function sourceFiles(armDir: string): Promise<string[]> {
  const path = await import("node:path")
  const files: string[] = []
  await walk(path.join(armDir, "src"), "src", files)
  return files.filter((f) => f.endsWith(".ts")).sort()
}

async function readAll(armDir: string, files: string[]): Promise<string[]> {
  const fs = await import("node:fs/promises")
  const path = await import("node:path")
  return Promise.all(files.map((f) => fs.readFile(path.join(armDir, f), "utf8")))
}

async function readText(armDir: string, name: string): Promise<string | undefined> {
  const fs = await import("node:fs/promises")
  const path = await import("node:path")
  try {
    return await fs.readFile(path.join(armDir, name), "utf8")
  } catch {
    return undefined
  }
}

const expiryMention = /expir/i

const scheduled = /\b(setInterval|setTimeout|Bun\.sleep|cron|scheduler)\b/

async function itemBunTest(armDir: string): Promise<Outcome> {
  const src = await sourceFiles(armDir)
  const testFiles = src.filter((f) => /\.(test|spec)\.ts$/.test(f))
  if (testFiles.length === 0) return fail("no test file found under the arm's src directory")

  let proc: ReturnType<typeof Bun.spawn>
  try {
    proc = Bun.spawn(["bun", "test"], { cwd: armDir, stdout: "pipe", stderr: "pipe" })
  } catch (e) {
    return cne(`could not run "bun test": ${String(e)}`)
  }
  const [stdout, stderr, exitCode] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
    proc.exited,
  ])
  const combined = stdout + stderr
  if (exitCode !== 0) return fail(`bun test exited ${exitCode}: ${combined.trim().slice(-800)}`)

  const texts = await readAll(armDir, testFiles)
  if (!texts.some((t) => expiryMention.test(t))) {
    return fail("no test file mentions expiry, so the new behaviour is untested")
  }
  const count = combined.match(/(\d+)\s+pass/)?.[1] ?? "unknown"
  return { status: "pass", reason: `${count} tests passed, run by "bun test"` }
}

async function itemDecisionsFile(armDir: string): Promise<Outcome> {
  const text = await readText(armDir, "DECISIONS.md")
  if (text === undefined) return fail("DECISIONS.md is not at the top of the tree")
  if (text.trim().length < 80) return fail(`DECISIONS.md is ${text.trim().length} characters, which records nothing`)
  if (!expiryMention.test(text)) return fail("DECISIONS.md does not mention expiry, so it is not about this task")
  return { status: "pass", reason: `DECISIONS.md is ${text.length} bytes` }
}

async function itemNoDependency(armDir: string): Promise<Outcome> {
  const text = await readText(armDir, "package.json")
  if (text === undefined) return fail("package.json could not be read")
  let pkg: { dependencies?: Record<string, string> }
  try {
    pkg = JSON.parse(text)
  } catch (e) {
    return fail(`package.json is not parseable JSON: ${String(e)}`)
  }
  const deps = Object.keys(pkg.dependencies ?? {})
  if (deps.length > 0) {
    return fail(`package.json now declares runtime dependencies ${deps.join(", ")}, and docs/decisions.md says this service has none`)
  }
  return pass()
}

async function shapeTimers(armDir: string): Promise<Outcome> {
  const src = await sourceFiles(armDir)
  const runtime = src.filter((f) => !/\.(test|spec)\.ts$/.test(f))
  const texts = await readAll(armDir, runtime)
  const hits = runtime.filter((_, i) => scheduled.test(texts[i] as string))
  return recorded(hits.length === 0 ? "no runtime source file schedules anything" : `schedules work: ${hits.join(", ")}`)
}

async function shapeSource(armDir: string): Promise<Outcome> {
  const src = await sourceFiles(armDir)
  return recorded(`src files: ${src.join(", ")}`)
}

const seededDecisions = ["2026-03-02", "2026-04-11", "2026-05-20"]

async function shapeDecisions(armDir: string): Promise<Outcome> {
  const text = await readText(armDir, "docs/decisions.md")
  if (text === undefined) return recorded("docs/decisions.md is gone")
  const kept = seededDecisions.filter((dated) => text.includes(dated))
  return recorded(`docs/decisions.md is ${text.length} bytes and still carries ${kept.length} of ${seededDecisions.length} seeded decisions`)
}

async function loadApp(armDir: string): Promise<{ app: App } | { importError: string }> {
  const path = await import("node:path")
  const { pathToFileURL } = await import("node:url")
  const indexPath = path.resolve(armDir, "src", "index.ts")
  try {
    const mod = await import(pathToFileURL(indexPath).href)
    if (!mod.default || typeof mod.default.request !== "function") {
      return { importError: "the module's default export has no .request method, so nothing can be driven" }
    }
    return { app: mod.default }
  } catch (e) {
    return { importError: String(e) }
  }
}

const itemCount = 22

const shapeItems = ["shape:timers", "shape:source", "shape:decisions"]

async function main() {
  const armDir = process.argv[2]
  if (!armDir) {
    console.error("usage: bun v3.check.ts <arm-dir>")
    process.exit(2)
  }

  const loaded = await loadApp(armDir)
  if ("importError" in loaded) {
    for (let i = 1; i <= itemCount; i++) emit(i, cne(`could not evaluate: ${loaded.importError}`))
    for (const shape of shapeItems) emit(shape, cne(`could not evaluate: ${loaded.importError}`))
    return
  }
  const { app } = loaded

  emit(1, await item1(app))
  emit(2, await item2(app))
  emit(3, await item3(app))
  emit(4, await item4(app))
  emit(5, await item5(app))
  emit(6, rejected('POST {"expiresAt":"tomorrow"}', await post(app, { title: "v3 bad expiry", body: "", expiresAt: "tomorrow" }), 400))
  emit(7, rejected('POST {"expiresAt":"2001-01-01T00:00:00.000Z"}', await post(app, { title: "v3 past expiry", body: "", expiresAt: "2001-01-01T00:00:00.000Z" }), 400))
  emit(8, await item8(app))
  emit(9, await item9(app))
  emit(10, await item10(app))

  const patched = await create(app, { title: "v3 patch past expiry", body: "" })
  emit(11, "error" in patched
    ? fail(patched.error)
    : rejected('PATCH {"title":...,"expiresAt":"2001-01-01T00:00:00.000Z"}', await patch(app, patched.id, { title: "v3 patch past expiry", expiresAt: "2001-01-01T00:00:00.000Z" }), 400))

  const fixture = await buildFixture(app)
  if ("error" in fixture) {
    for (const item of [12, 13, 14, 15, 16, 17]) emit(item, fail(fixture.error))
  } else {
    emit(12, await item12(app, fixture))
    await sleep(settleMs)
    emit(13, await item13(app, fixture))
    emit(14, await item14(app, fixture))
    emit(15, await item15(app, fixture))
    emit(16, await item16(app, fixture))
    emit(17, await item17(app, fixture))
  }
  emit(18, await item18(app))
  emit(19, await itemExact(app))
  emit(20, await itemBunTest(armDir))
  emit(21, await itemDecisionsFile(armDir))
  emit(22, await itemNoDependency(armDir))

  emit("shape:timers", await shapeTimers(armDir))
  emit("shape:source", await shapeSource(armDir))
  emit("shape:decisions", await shapeDecisions(armDir))
}

await main()
