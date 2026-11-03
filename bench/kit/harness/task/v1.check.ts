type Status = "pass" | "fail" | "could_not_evaluate" | "recorded"

type Line = { item: number | string; status: Status; reason?: string }

type Outcome = Omit<Line, "item">

type App = { request: (path: string, init?: RequestInit) => Promise<Response> }

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

async function createTask(app: App, title: string) {
  const res = await app.request("/tasks", jsonInit("POST", { title }))
  const body = (await readJSON(res)) as Record<string, unknown> | undefined
  const ok = (res.status === 200 || res.status === 201) && !!body && typeof body.id !== "undefined"
  return { ok, status: res.status, id: ok ? String((body as Record<string, unknown>).id) : undefined, body }
}

async function checkGetRoot(app: App): Promise<Outcome> {
  const res = await app.request("/")
  if (res.status !== 200) return fail(`GET / returned ${res.status}, want 200`)
  const text = await res.text()
  if (text !== "Hello Hono!") return fail(`GET / body was ${JSON.stringify(text)}, want "Hello Hono!"`)
  return pass()
}

async function item2(app: App) {
  const created = await createTask(app, "write the report")
  if (!created.ok) return { outcome: fail(`POST /tasks returned ${created.status} or no id in body`), id: undefined as string | undefined }
  const body = created.body as Record<string, unknown>
  if (body.title !== "write the report") return { outcome: fail(`POST /tasks response title was ${JSON.stringify(body.title)}`), id: created.id }
  return { outcome: pass(), id: created.id }
}

async function item3(app: App): Promise<Outcome> {
  const created = await createTask(app, "item3 fixture")
  if (!created.ok) return fail(`POST /tasks setup failed: status ${created.status}`)
  const res = await app.request(`/tasks/${created.id}`)
  if (res.status !== 200) return fail(`GET /tasks/<id> returned ${res.status}, want 200`)
  const body = (await readJSON(res)) as Record<string, unknown> | undefined
  if (!body || body.title !== "item3 fixture") return fail(`GET /tasks/<id> body was ${JSON.stringify(body)}`)
  return pass()
}

async function item4(app: App): Promise<Outcome> {
  const created = await createTask(app, "item4 fixture")
  if (!created.ok) return fail(`POST /tasks setup failed: status ${created.status}`)
  const res = await app.request("/tasks")
  if (res.status !== 200) return fail(`GET /tasks returned ${res.status}, want 200`)
  const body = await readJSON(res)
  if (!Array.isArray(body)) return fail("GET /tasks did not return a JSON array")
  const found = body.some((t) => t && typeof t === "object" && String((t as Record<string, unknown>).id) === created.id)
  if (!found) return fail(`GET /tasks did not contain the created task ${created.id}`)
  return pass()
}

async function checkPatchUpdatesField(app: App, label: string, field: string, value: unknown): Promise<Outcome> {
  const created = await createTask(app, `${label} fixture`)
  if (!created.ok) return fail(`POST /tasks setup failed: status ${created.status}`)
  const res = await app.request(`/tasks/${created.id}`, jsonInit("PATCH", { [field]: value }))
  if (res.status !== 200) return fail(`PATCH /tasks/<id> returned ${res.status}, want 200`)
  const read = await app.request(`/tasks/${created.id}`)
  const body = (await readJSON(read)) as Record<string, unknown> | undefined
  if (!body || body[field] !== value) return fail(`${field} after PATCH was ${JSON.stringify(body?.[field])}, want ${JSON.stringify(value)}`)
  return pass()
}

async function item7(app: App): Promise<Outcome> {
  const created = await createTask(app, "item7 fixture")
  if (!created.ok) return fail(`POST /tasks setup failed: status ${created.status}`)
  const res = await app.request(`/tasks/${created.id}`, { method: "DELETE" })
  if (res.status !== 200 && res.status !== 204) return fail(`DELETE /tasks/<id> returned ${res.status}, want 200 or 204`)
  const read = await app.request(`/tasks/${created.id}`)
  if (read.status !== 404) return fail(`GET /tasks/<id> after DELETE returned ${read.status}, want 404`)
  return pass()
}

async function item8(app: App): Promise<Outcome> {
  const a = await createTask(app, "item8 fixture a")
  const b = await createTask(app, "item8 fixture b")
  if (!a.ok || !b.ok) return fail(`POST /tasks setup failed: status ${a.status} and ${b.status}`)
  if (a.id === b.id) return fail(`two POST /tasks calls produced the same id ${a.id}`)
  return pass()
}

async function item9(app: App): Promise<Outcome> {
  const res = await app.request("/tasks", jsonInit("POST", {}))
  if (res.status !== 400) return fail(`POST /tasks with {} returned ${res.status}, want 400`)
  return pass()
}

async function item10(app: App): Promise<Outcome> {
  const res = await app.request("/tasks", jsonInit("POST", { title: "" }))
  if (res.status !== 400) return fail(`POST /tasks with an empty title returned ${res.status}, want 400`)
  return pass()
}

async function item11(app: App): Promise<Outcome> {
  const res = await app.request("/tasks", jsonInit("POST", { title: "   " }))
  return recorded(`POST /tasks with a whitespace-only title returned ${res.status}`)
}

async function item12(app: App): Promise<Outcome> {
  try {
    const res = await app.request("/tasks", malformedInit)
    if (res.status === 500) return fail("POST /tasks with a malformed body returned 500")
    if (res.status !== 400) return fail(`POST /tasks with a malformed body returned ${res.status}, want 400`)
    return pass()
  } catch (e) {
    return fail(`POST /tasks with a malformed body threw: ${String(e)}`)
  }
}

const unknownID = "does-not-exist-00000000"

async function collectionImplemented(app: App): Promise<boolean> {
  const res = await app.request("/tasks")
  return res.status !== 404
}

async function checkUnknownIDReturns404(app: App, verb: string, call: () => Promise<Response>): Promise<Outcome> {
  if (!(await collectionImplemented(app))) return fail("GET /tasks is not implemented, so this 404 is the framework fallback, not real handling")
  const res = await call()
  if (res.status !== 404) return fail(`${verb} /tasks/<unknown> returned ${res.status}, want 404`)
  return pass()
}

async function bodyLooksLikeJSONObjectOrArray(res: Response): Promise<string | undefined> {
  const text = await res.text()
  if (text === "") return "body was empty"
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return `body was not parseable JSON: ${JSON.stringify(text)}`
  }
  if (typeof parsed !== "object" || parsed === null) return `body was a JSON ${typeof parsed}, not an object`
  return undefined
}

async function item16(app: App): Promise<Outcome> {
  const calls: Array<[string, () => Promise<Response>]> = [
    ["POST {}", () => app.request("/tasks", jsonInit("POST", {}))],
    ["POST empty title", () => app.request("/tasks", jsonInit("POST", { title: "" }))],
    ["POST malformed", () => app.request("/tasks", malformedInit)],
    ["GET unknown", () => app.request(`/tasks/${unknownID}`)],
    ["PATCH unknown", () => app.request(`/tasks/${unknownID}`, jsonInit("PATCH", { title: "x" }))],
    ["DELETE unknown", () => app.request(`/tasks/${unknownID}`, { method: "DELETE" })],
  ]
  let checked = 0
  for (const [label, call] of calls) {
    const res = await call()
    if (res.status !== 400 && res.status !== 404) continue
    checked++
    const problem = await bodyLooksLikeJSONObjectOrArray(res)
    if (problem) return fail(`${label} (${res.status}): ${problem}`)
  }
  if (checked === 0) return cne("none of the 400/404 cases actually returned 400 or 404")
  return pass()
}

const storeLikePattern = /(store|\.db$|\.sqlite3?$|\.json$)/i

async function walk(dir: string, base: string, out: string[]) {
  const fs = await import("node:fs/promises")
  const path = await import("node:path")
  const entries = await fs.readdir(dir, { withFileTypes: true })
  for (const entry of entries) {
    if (entry.name === "node_modules" || entry.name === ".git") continue
    const full = path.join(dir, entry.name)
    const rel = path.join(base, entry.name)
    if (entry.isDirectory()) {
      await walk(full, rel, out)
    } else {
      out.push(rel)
    }
  }
}

async function item17(armDir: string, app: App): Promise<Outcome> {
  if (!(await collectionImplemented(app))) return fail("GET /tasks is not implemented, so in-memory storage cannot be confirmed")
  const path = await import("node:path")
  const files: string[] = []
  await walk(armDir, "", files)
  const suspects = files.filter((f) => {
    const base = path.basename(f).toLowerCase()
    if (base === "package.json" || base === "tsconfig.json" || base === "package-lock.json") return false
    if (f.startsWith(path.join("src"))) return storeLikePattern.test(base) && !base.endsWith(".ts")
    return storeLikePattern.test(base)
  })
  if (suspects.length > 0) return fail(`files that look like a store: ${suspects.join(", ")}`)

  const fs = await import("node:fs/promises")
  let pkg: Record<string, unknown>
  try {
    pkg = JSON.parse(await fs.readFile(path.join(armDir, "package.json"), "utf8"))
  } catch (e) {
    return cne(`could not read package.json: ${String(e)}`)
  }
  const seedDeps = new Set(["hono"])
  const seedDevDeps = new Set(["@types/bun"])
  const deps = Object.keys((pkg.dependencies as Record<string, unknown>) ?? {})
  const devDeps = Object.keys((pkg.devDependencies as Record<string, unknown>) ?? {})
  const added = [...deps.filter((d) => !seedDeps.has(d)), ...devDeps.filter((d) => !seedDevDeps.has(d))]
  if (added.length > 0) return fail(`dependencies added beyond the seed: ${added.join(", ")}`)
  return pass()
}

async function findTestFiles(armDir: string): Promise<string[]> {
  const files: string[] = []
  await walk(armDir, "", files)
  return files.filter((f) => /\.(test|spec)\.(ts|tsx|js)$/.test(f))
}

async function readTestFiles(armDir: string, testFiles: string[]): Promise<string[]> {
  const fs = await import("node:fs/promises")
  const path = await import("node:path")
  return Promise.all(testFiles.map((f) => fs.readFile(path.join(armDir, f), "utf8")))
}

async function item18(armDir: string, testFiles: string[]): Promise<Outcome> {
  if (testFiles.length === 0) return fail("no test files found under the arm's directory")
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
  const count = combined.match(/(\d+)\s+pass/)?.[1] ?? "unknown"
  return { status: "pass", reason: `${count} tests passed, run by "bun test"` }
}

async function item19(armDir: string, testFiles: string[]): Promise<Outcome> {
  if (testFiles.length === 0) return fail("no test files found under the arm's directory")
  const texts = await readTestFiles(armDir, testFiles)
  const hits = texts.some((text) => /\b400\b/.test(text) || /\b404\b/.test(text))
  if (!hits) return fail("no test file references 400 or 404, so no failure path looks covered")
  return pass()
}

async function item20(app: App, armDir: string, testFiles: string[]): Promise<Outcome> {
  if (testFiles.length === 0) return fail("no test file covers GET /, and no test files exist")
  const texts = await readTestFiles(armDir, testFiles)
  const covered = texts.some((text) => /request\(\s*['"]\/['"]/.test(text))
  if (!covered) return fail("no test file requests the root route '/'")
  const live = await checkGetRoot(app)
  if (live.status !== "pass") return fail(`GET / is covered by a test but currently ${live.status}: ${live.reason}`)
  return pass()
}

function idShape(id: string | undefined): Outcome {
  if (!id) return recorded("no id observed, item 2 did not produce one")
  if (/^\d+$/.test(id)) return recorded(`id "${id}" looks like a counter`)
  if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)) return recorded(`id "${id}" looks like a uuid`)
  return recorded(`id "${id}" is some other shape`)
}

async function fileShape(armDir: string): Promise<Outcome> {
  const path = await import("node:path")
  const files: string[] = []
  await walk(path.join(armDir, "src"), "src", files)
  const srcFiles = files.filter((f) => f.endsWith(".ts"))
  return recorded(`src has ${srcFiles.length} file(s): ${srcFiles.join(", ")}`)
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

async function main() {
  const armDir = process.argv[2]
  if (!armDir) {
    console.error("usage: bun v1.check.ts <arm-dir>")
    process.exit(2)
  }

  const loaded = await loadApp(armDir)
  if ("importError" in loaded) {
    for (let i = 1; i <= 20; i++) emit(i, cne(`could not evaluate: ${loaded.importError}`))
    emit("shape:id", cne(`could not evaluate: ${loaded.importError}`))
    emit("shape:files", cne(`could not evaluate: ${loaded.importError}`))
    return
  }
  const { app } = loaded

  emit(1, await checkGetRoot(app))
  const two = await item2(app)
  emit(2, two.outcome)
  emit(3, await item3(app))
  emit(4, await item4(app))
  emit(5, await checkPatchUpdatesField(app, "item5", "title", "renamed"))
  emit(6, await checkPatchUpdatesField(app, "item6", "done", true))
  emit(7, await item7(app))
  emit(8, await item8(app))
  emit(9, await item9(app))
  emit(10, await item10(app))
  emit(11, await item11(app))
  emit(12, await item12(app))
  emit(13, await checkUnknownIDReturns404(app, "GET", () => app.request(`/tasks/${unknownID}`)))
  emit(14, await checkUnknownIDReturns404(app, "PATCH", () => app.request(`/tasks/${unknownID}`, jsonInit("PATCH", { title: "x" }))))
  emit(15, await checkUnknownIDReturns404(app, "DELETE", () => app.request(`/tasks/${unknownID}`, { method: "DELETE" })))
  emit(16, await item16(app))
  emit(17, await item17(armDir, app))
  const testFiles = await findTestFiles(armDir)
  emit(18, await item18(armDir, testFiles))
  emit(19, await item19(armDir, testFiles))
  emit(20, await item20(app, armDir, testFiles))

  emit("shape:id", idShape(two.id))
  emit("shape:files", await fileShape(armDir))
}

await main()
