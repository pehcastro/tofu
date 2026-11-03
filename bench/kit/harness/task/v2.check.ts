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

const unknownID = "does-not-exist-00000000"

type Sent = { status: number; body: Body | undefined; raw: unknown }

async function send(app: App, path: string, init?: RequestInit): Promise<Sent> {
  const res = await app.request(path, init)
  const raw = await readJSON(res)
  const body = raw && typeof raw === "object" && !Array.isArray(raw) ? (raw as Body) : undefined
  return { status: res.status, body, raw }
}

async function post(app: App, body: unknown): Promise<Sent> {
  return send(app, "/tasks", jsonInit("POST", body))
}

async function patch(app: App, id: string, body: unknown): Promise<Sent> {
  return send(app, `/tasks/${id}`, jsonInit("PATCH", body))
}

type Created = { id: string; task: Body } | { error: string }

async function create(app: App, body: Body): Promise<Created> {
  const sent = await post(app, body)
  if (sent.status !== 200 && sent.status !== 201) {
    return { error: `POST /tasks ${JSON.stringify(body)} returned ${sent.status}` }
  }
  if (!sent.body || sent.body.id === undefined) {
    return { error: `POST /tasks ${JSON.stringify(body)} returned no id in its body` }
  }
  return { id: String(sent.body.id), task: sent.body }
}

async function readBack(app: App, id: string): Promise<Body | undefined> {
  const sent = await send(app, `/tasks/${id}`)
  return sent.status === 200 ? sent.body : undefined
}

async function item1(app: App): Promise<Outcome> {
  const res = await app.request("/")
  if (res.status !== 200) return fail(`GET / returned ${res.status}, want 200`)
  const text = await res.text()
  if (text !== "Hello Hono!") return fail(`GET / body was ${JSON.stringify(text)}, want "Hello Hono!"`)
  return pass()
}

async function item2(app: App): Promise<Outcome> {
  const made = await create(app, { title: "v2 lifecycle" })
  if ("error" in made) return fail(made.error)
  if (made.task.title !== "v2 lifecycle") return fail(`POST /tasks echoed title ${JSON.stringify(made.task.title)}`)

  const read = await readBack(app, made.id)
  if (!read || read.title !== "v2 lifecycle") return fail(`GET /tasks/${made.id} returned ${JSON.stringify(read)}`)

  const list = await send(app, "/tasks")
  if (list.status !== 200) return fail(`GET /tasks returned ${list.status}, want 200`)
  if (!Array.isArray(list.raw)) return fail("GET /tasks did not return a JSON array")
  if (!idsOf(list.raw).has(made.id)) return fail(`GET /tasks did not contain the created task ${made.id}`)

  const deleted = await app.request(`/tasks/${made.id}`, { method: "DELETE" })
  if (deleted.status !== 200 && deleted.status !== 204) {
    return fail(`DELETE /tasks/${made.id} returned ${deleted.status}, want 200 or 204`)
  }
  const gone = await app.request(`/tasks/${made.id}`)
  if (gone.status !== 404) return fail(`GET after DELETE returned ${gone.status}, want 404`)
  return pass()
}

async function item3(app: App): Promise<Outcome> {
  const empty = await post(app, {})
  if (empty.status !== 400) return fail(`POST /tasks with {} returned ${empty.status}, want 400`)
  const blank = await post(app, { title: "" })
  if (blank.status !== 400) return fail(`POST /tasks with an empty title returned ${blank.status}, want 400`)
  try {
    const bad = await app.request("/tasks", malformedInit)
    if (bad.status !== 400) return fail(`POST /tasks with a malformed body returned ${bad.status}, want 400`)
  } catch (e) {
    return fail(`POST /tasks with a malformed body threw: ${String(e)}`)
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
    const res = await app.request(`/tasks/${unknownID}`, init)
    if (res.status !== 404) return fail(`${verb} /tasks/<unknown> returned ${res.status}, want 404`)
    const text = await res.text()
    let parsed: unknown
    try {
      parsed = JSON.parse(text)
    } catch {
      return fail(`${verb} /tasks/<unknown> body was not parseable JSON: ${JSON.stringify(text)}`)
    }
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return fail(`${verb} /tasks/<unknown> body was not a JSON object: ${JSON.stringify(text)}`)
    }
  }
  return pass()
}

async function item5(app: App): Promise<Outcome> {
  const made = await create(app, { title: "v2 rename" })
  if ("error" in made) return fail(made.error)
  if (!("completed" in made.task)) return fail(`the created task has no "completed" field: ${JSON.stringify(made.task)}`)
  if ("done" in made.task) return fail(`the created task still carries "done": ${JSON.stringify(made.task)}`)
  return pass()
}

async function readsBack(app: App, id: string, field: string, value: unknown): Promise<Outcome> {
  const read = await readBack(app, id)
  if (!read) return fail(`GET /tasks/${id} did not return the task`)
  if (read[field] !== value) {
    return fail(`${field} read back as ${JSON.stringify(read[field])}, want ${JSON.stringify(value)}`)
  }
  return pass()
}

async function createdReadsBack(app: App, body: Body, field: string, value: unknown): Promise<Outcome> {
  const made = await create(app, body)
  if ("error" in made) return fail(made.error)
  return readsBack(app, made.id, field, value)
}

async function patchedReadsBack(app: App, title: string, field: string, value: unknown): Promise<Outcome> {
  const made = await create(app, { title })
  if ("error" in made) return fail(made.error)
  const sent = await patch(app, made.id, { [field]: value })
  if (sent.status !== 200) return fail(`PATCH {"${field}":${JSON.stringify(value)}} returned ${sent.status}, want 200`)
  return readsBack(app, made.id, field, value)
}

function rejected(label: string, sent: Sent, want: number): Outcome {
  if (sent.status !== want) return fail(`${label} returned ${sent.status}, want ${want}`)
  if (!sent.body) return fail(`${label} returned ${want} but its body is not a JSON object`)
  return pass()
}

async function patchRejected(app: App, title: string, body: Body): Promise<Outcome> {
  const made = await create(app, { title })
  if ("error" in made) return fail(made.error)
  return rejected(`PATCH ${JSON.stringify(body)}`, await patch(app, made.id, body), 400)
}

async function item15(app: App, f: Fixture): Promise<Outcome> {
  const done = await checkFilter(app, "completed=true", [f.high_done, f.low_done], [f.high_open, f.low_open])
  if (done.status !== "pass") return done
  return checkFilter(app, "completed=false", [f.high_open, f.low_open], [f.high_done, f.low_done])
}

function idsOf(list: unknown): Set<string> {
  if (!Array.isArray(list)) return new Set()
  const ids = new Set<string>()
  for (const entry of list) {
    if (entry && typeof entry === "object" && (entry as Body).id !== undefined) ids.add(String((entry as Body).id))
  }
  return ids
}

type Fixture = { high_done: string; low_done: string; high_open: string; low_open: string }

async function buildFixture(app: App): Promise<Fixture | { error: string }> {
  const wanted: Array<[keyof Fixture, string, boolean]> = [
    ["high_done", "high", true],
    ["low_done", "low", true],
    ["high_open", "high", false],
    ["low_open", "low", false],
  ]
  const fixture = {} as Fixture
  for (const [key, priority, completed] of wanted) {
    const made = await create(app, { title: `v2 filter ${key}`, priority })
    if ("error" in made) return { error: `building the filter fixture: ${made.error}` }
    fixture[key] = made.id
    if (!completed) continue
    const marked = await patch(app, made.id, { completed: true })
    if (marked.status !== 200) {
      return { error: `building the filter fixture: PATCH {"completed":true} on ${made.id} returned ${marked.status}` }
    }
  }
  return fixture
}

async function checkFilter(app: App, query: string, want: string[], reject: string[]): Promise<Outcome> {
  const sent = await send(app, `/tasks?${query}`)
  if (sent.status !== 200) return fail(`GET /tasks?${query} returned ${sent.status}, want 200`)
  if (!Array.isArray(sent.raw)) return fail(`GET /tasks?${query} did not return a JSON array`)
  const ids = idsOf(sent.raw)
  const missing = want.filter((id) => !ids.has(id))
  const present = reject.filter((id) => ids.has(id))
  if (missing.length > 0) return fail(`GET /tasks?${query} omitted fixture tasks ${missing.join(", ")}`)
  if (present.length > 0) return fail(`GET /tasks?${query} included fixture tasks it should have filtered out: ${present.join(", ")}`)
  return pass()
}

async function item18(app: App): Promise<Outcome> {
  const bad = await send(app, "/tasks?completed=maybe")
  if (bad.status !== 400) return fail(`GET /tasks?completed=maybe returned ${bad.status}, want 400`)
  const worse = await send(app, "/tasks?priority=urgent")
  if (worse.status !== 400) return fail(`GET /tasks?priority=urgent returned ${worse.status}, want 400`)
  return pass()
}

async function item19(app: App): Promise<Outcome> {
  const ok = await post(app, { title: "a".repeat(120) })
  if (ok.status !== 200 && ok.status !== 201) return fail(`a 120-character title returned ${ok.status}, want 200 or 201`)
  const tooLong = await post(app, { title: "a".repeat(121) })
  if (tooLong.status !== 400) return fail(`a 121-character title returned ${tooLong.status}, want 400`)
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

const filterMention = /(priority|completed=)/i

async function item20(armDir: string): Promise<Outcome> {
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
  if (!texts.some((t) => filterMention.test(t))) {
    return fail("no test file mentions a query filter or the priority field, so the new behaviour is untested")
  }
  const count = combined.match(/(\d+)\s+pass/)?.[1] ?? "unknown"
  return { status: "pass", reason: `${count} tests passed, run by "bun test"` }
}

async function shapeHelpers(armDir: string): Promise<Outcome> {
  const src = await sourceFiles(armDir)
  const texts = await readAll(armDir, src)
  const routeAt = texts.findIndex((t) => /new Hono\b/.test(t))
  if (routeAt < 0) return recorded(`src files: ${src.join(", ")}. no file constructs a Hono app`)
  const imports = [...texts[routeAt].matchAll(/from\s+['"](\.[^'"]*)['"]/g)].map((m) => m[1])
  return recorded(`src files: ${src.join(", ")}. the route file ${src[routeAt]} imports ${imports.join(", ") || "nothing local"}`)
}

async function shapeReadme(armDir: string): Promise<Outcome> {
  const fs = await import("node:fs/promises")
  const path = await import("node:path")
  let text: string
  try {
    text = await fs.readFile(path.join(armDir, "README.md"), "utf8")
  } catch (e) {
    return recorded(`README.md could not be read: ${String(e)}`)
  }
  const mentions = ["/tasks", "completed", "priority"].filter((needle) => text.includes(needle))
  return recorded(`README.md is ${text.length} bytes and mentions ${mentions.join(", ") || "none of /tasks, completed, priority"}`)
}

async function shapeCheck(armDir: string): Promise<Outcome> {
  const fs = await import("node:fs/promises")
  const path = await import("node:path")
  try {
    const pkg = JSON.parse(await fs.readFile(path.join(armDir, "package.json"), "utf8"))
    const scripts = (pkg.scripts as Record<string, string>) ?? {}
    const deps = [...Object.keys(pkg.dependencies ?? {}), ...Object.keys(pkg.devDependencies ?? {})]
    return recorded(`scripts: ${JSON.stringify(scripts)}. dependencies: ${deps.join(", ")}`)
  } catch (e) {
    return recorded(`package.json could not be read: ${String(e)}`)
  }
}

async function loadApp(armDir: string): Promise<{ app: App } | { importError: string }> {
  const path = await import("node:path")
  const { pathToFileURL } = await import("node:url")
  const indexPath = path.resolve(armDir, "src", "index.ts")
  try {
    const mod = await import(pathToFileURL(indexPath).href)
    if (!mod.default || typeof mod.default.request !== "function") {
      return { importError: "the module's default export is not a Hono app (no .request method)" }
    }
    return { app: mod.default }
  } catch (e) {
    return { importError: String(e) }
  }
}

const itemCount = 20

async function main() {
  const armDir = process.argv[2]
  if (!armDir) {
    console.error("usage: bun v2.check.ts <arm-dir>")
    process.exit(2)
  }

  const loaded = await loadApp(armDir)
  if ("importError" in loaded) {
    for (let i = 1; i <= itemCount; i++) emit(i, cne(`could not evaluate: ${loaded.importError}`))
    for (const shape of ["shape:helpers", "shape:readme", "shape:check"]) {
      emit(shape, cne(`could not evaluate: ${loaded.importError}`))
    }
    return
  }
  const { app } = loaded

  emit(1, await item1(app))
  emit(2, await item2(app))
  emit(3, await item3(app))
  emit(4, await item4(app))
  emit(5, await item5(app))
  emit(6, await createdReadsBack(app, { title: "v2 created completed", completed: true }, "completed", true))
  emit(7, await patchedReadsBack(app, "v2 patch completed", "completed", true))
  emit(8, await patchRejected(app, "v2 patch done", { done: true }))
  emit(9, rejected('POST {"done":true}', await post(app, { title: "v2 post done", done: true }), 400))
  emit(10, await createdReadsBack(app, { title: "v2 default priority" }, "priority", "normal"))
  emit(11, await createdReadsBack(app, { title: "v2 high priority", priority: "high" }, "priority", "high"))
  emit(12, await patchedReadsBack(app, "v2 patch priority", "priority", "low"))
  emit(13, rejected('POST {"priority":"urgent"}', await post(app, { title: "v2 post bad priority", priority: "urgent" }), 400))
  emit(14, await patchRejected(app, "v2 patch bad priority", { title: "v2 patch bad priority", priority: "urgent" }))

  const fixture = await buildFixture(app)
  if ("error" in fixture) {
    for (const item of [15, 16, 17]) emit(item, fail(fixture.error))
  } else {
    emit(15, await item15(app, fixture))
    emit(16, await checkFilter(app, "priority=high", [fixture.high_done, fixture.high_open], [fixture.low_done, fixture.low_open]))
    emit(17, await checkFilter(app, "completed=true&priority=high", [fixture.high_done], [fixture.low_done, fixture.high_open, fixture.low_open]))
  }
  emit(18, await item18(app))
  emit(19, await item19(app))
  emit(20, await item20(armDir))

  emit("shape:helpers", await shapeHelpers(armDir))
  emit("shape:readme", await shapeReadme(armDir))
  emit("shape:check", await shapeCheck(armDir))
}

await main()
