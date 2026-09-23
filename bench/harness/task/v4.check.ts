type Status = "pass" | "fail" | "could_not_evaluate" | "recorded"

type Line = { item: number | string; status: Status; reason?: string }

type Outcome = Omit<Line, "item">

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

const answerFile = "ANSWER.json"

const answerKeys = ["status", "code", "decided_in", "symbol", "formats"] as const

const wantStatus = 403

const wantCode = "plan.capability.withheld"

const wantDecidedIn = "src/plan/overrides.ts"

const wantSymbol = "withheldByPlan"

const wantFormats = ["json", "ndjson", "parquet"]

const toleratedExtras = new Set([answerFile, "bun.lock", "bun.lockb", "package-lock.json"])

const ignoredDirectories = new Set([".git", "node_modules", ".tofu"])

async function walk(dir: string, base: string, out: string[]): Promise<void> {
  const fs = await import("node:fs/promises")
  const path = await import("node:path")
  for (const entry of await fs.readdir(dir, { withFileTypes: true })) {
    if (ignoredDirectories.has(entry.name)) continue
    const rel = base === "" ? entry.name : `${base}/${entry.name}`
    if (entry.isDirectory()) await walk(path.join(dir, entry.name), rel, out)
    else out.push(rel)
  }
}

async function digestOf(file: string): Promise<string> {
  const bytes = await Bun.file(file).arrayBuffer()
  const hashed = await crypto.subtle.digest("SHA-256", bytes)
  return [...new Uint8Array(hashed)].map((b) => b.toString(16).padStart(2, "0")).join("")
}

type Tree = Map<string, string>

async function treeOf(dir: string): Promise<Tree> {
  const path = await import("node:path")
  const files: string[] = []
  await walk(dir, "", files)
  const tree: Tree = new Map()
  for (const rel of files.sort()) tree.set(rel, await digestOf(path.join(dir, rel)))
  return tree
}

type Answer = Record<string, unknown>

async function readAnswer(armDir: string): Promise<Answer | { error: string }> {
  const path = await import("node:path")
  const file = Bun.file(path.join(armDir, answerFile))
  if (!(await file.exists())) return { error: `${answerFile} is not at the top of the project` }
  const text = await file.text()
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch (e) {
    return { error: `${answerFile} is not parseable JSON: ${String(e)}` }
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return { error: `${answerFile} is not a JSON object: ${text.slice(0, 200)}` }
  }
  return parsed as Answer
}

function itemKeys(answer: Answer): Outcome {
  const present = Object.keys(answer).sort()
  const want = [...answerKeys].sort()
  if (present.length !== want.length || present.some((key, i) => key !== want[i])) {
    return fail(`${answerFile} carries ${present.join(", ")}, want exactly ${want.join(", ")}`)
  }
  return pass()
}

function itemExactly(answer: Answer, key: string, want: string | number): Outcome {
  if (answer[key] !== want) {
    return fail(`${key} is ${JSON.stringify(answer[key])}, want ${JSON.stringify(want)}`)
  }
  return pass()
}

function normalisePath(raw: unknown): string {
  return String(raw).replaceAll("\\", "/").replace(/^\.\//, "").replace(/^\//, "")
}

function itemDecidedIn(answer: Answer): Outcome {
  const got = normalisePath(answer.decided_in)
  if (got !== wantDecidedIn) return fail(`decided_in is ${JSON.stringify(got)}, want ${JSON.stringify(wantDecidedIn)}`)
  return pass()
}

function itemFormats(answer: Answer): Outcome {
  const got = answer.formats
  if (!Array.isArray(got)) return fail(`formats is ${JSON.stringify(got)}, want an array of strings`)
  const listed = got.map(String)
  if (listed.length !== wantFormats.length || listed.some((format, i) => format !== wantFormats[i])) {
    return fail(`formats is ${JSON.stringify(listed)}, want ${JSON.stringify(wantFormats)} sorted alphabetically`)
  }
  return pass()
}

function itemUntouched(seed: Tree, arm: Tree): Outcome {
  const missing: string[] = []
  const changed: string[] = []
  for (const [rel, digest] of seed) {
    const got = arm.get(rel)
    if (got === undefined) missing.push(rel)
    else if (got !== digest) changed.push(rel)
  }
  if (missing.length > 0 || changed.length > 0) {
    const parts: string[] = []
    if (changed.length > 0) parts.push(`edited ${changed.join(", ")}`)
    if (missing.length > 0) parts.push(`removed ${missing.join(", ")}`)
    return fail(`the project was supposed to be left alone and it was ${parts.join("; ")}`)
  }
  return pass()
}

function itemNothingAdded(extras: string[]): Outcome {
  const unwanted = extras.filter((rel) => !toleratedExtras.has(rel))
  if (unwanted.length > 0) return fail(`files were added beyond ${answerFile}: ${unwanted.join(", ")}`)
  return pass()
}

const itemCount = 9

const shapeItems = ["shape:extras", "shape:answer"]

async function main() {
  const armDir = process.argv[2]
  if (!armDir) {
    console.error("usage: bun v4.check.ts <arm-dir>")
    process.exit(2)
  }
  const path = await import("node:path")
  const seedDir = path.join(import.meta.dir, "v4.seed")

  let seed: Tree
  let arm: Tree
  try {
    seed = await treeOf(seedDir)
    arm = await treeOf(armDir)
  } catch (e) {
    for (let i = 1; i <= itemCount; i++) emit(i, cne(`could not read a tree: ${String(e)}`))
    for (const shape of shapeItems) emit(shape, cne(`could not read a tree: ${String(e)}`))
    return
  }

  const answer = await readAnswer(armDir)
  if ("error" in answer) {
    emit(1, fail(answer.error))
    for (let i = 2; i <= 7; i++) emit(i, fail(answer.error))
  } else {
    emit(1, pass())
    emit(2, itemKeys(answer))
    emit(3, itemExactly(answer, "status", wantStatus))
    emit(4, itemExactly(answer, "code", wantCode))
    emit(5, itemDecidedIn(answer))
    emit(6, itemExactly(answer, "symbol", wantSymbol))
    emit(7, itemFormats(answer))
  }

  const extras = [...arm.keys()].filter((rel) => !seed.has(rel))
  emit(8, itemUntouched(seed, arm))
  emit(9, itemNothingAdded(extras))

  emit("shape:extras", recorded(extras.length === 0 ? "the project gained no file at all" : `files present that the seed did not carry: ${extras.join(", ")}`))
  emit("shape:answer", recorded(`the seed is ${seed.size} files and the arm tree is ${arm.size}`))
}

await main()
