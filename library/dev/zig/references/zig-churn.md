---
id: zig-churn
domain: dev
document: what changed in Zig from 0.13 to master, per version: the old form, the new form and the error the old form prints
---

# Zig churn, 0.13 to master

Write for the version the project names, and read every row from that version back. Stable is 0.17.1 (2026-11-02), master is 0.18.0-dev. A `0.N.0-dev` build leads up to 0.N.0: treat `0.16.0-dev.2905` as 0.16. A version newer than this page is unknown; read the std source at `.std_dir` from `zig env` instead of guessing. An error quoted here was printed by a 0.16-dev compiler or is a case in the compiler's test suite; a row with no error text has none verified. **Silent** means the old form compiles and does something else.

## 0.13

| Old | New |
|---|---|
| `std.ComptimeStringMap(T, kvs)` | `std.StaticStringMap(T).initComptime(kvs)`; old: `root source file struct 'std' has no member named 'ComptimeStringMap'` |
| `std.ChildProcess` | `std.process.Child` (0.13 to 0.15); see 0.16 |
| `zig-cache/` | `.zig-cache/` |

## 0.14

| Old | New |
|---|---|
| `.Struct`, `.Int`, `.Pointer` on `std.builtin.Type`; `.One`, `.Slice` on pointer size | `.@"struct"`, `.int`, `.pointer`; `.one`, `.slice`; old: `no field named 'Struct' in union 'builtin.Type'` |
| `@setCold(true)` | `@branchHint(.cold)` as the first statement; old: `invalid builtin function: '@setCold'` |
| `@export(foo, ...)` | `@export(&foo, ...)` |
| `b.addExecutable(.{ .root_source_file = ... })`, same on `addTest` | `.root_module = b.createModule(.{ .root_source_file = b.path("src/main.zig"), .target = target, .optimize = optimize })`; removed in 0.15: `no field named 'root_source_file' in struct 'Build.ExecutableOptions'` |
| `b.addStaticLibrary(...)` | `b.addLibrary(.{ .linkage = .static, .name = "x", .root_module = mod })`; gone by 0.15.2 |
| build.zig.zon `.name = "foo"` | `.name = .foo` and a `.fingerprint` the toolchain generates and nobody edits; old: `expected enum literal`, `missing top-level 'fingerprint' field` |
| `std.heap.GeneralPurposeAllocator(.{}){}` | `std.heap.DebugAllocator(.{}) = .init`; alias kept to 0.15.2, gone in 0.16: `root source file struct 'heap' has no member named 'GeneralPurposeAllocator'` |
| `list.popOrNull()` | `list.pop()`, returns `?T` |
| `std.mem.split`, `std.mem.tokenize` | `splitScalar`, `splitSequence`, `splitAny`, and the same for tokenize |
| a field and a declaration with one name | rename one; old: `duplicate struct member name` |

## 0.15: Writergate

| Old | New |
|---|---|
| `usingnamespace` | declare each name; old is a parse error |
| `async`, `await` | gone; 0.16: `io.async(f, .{args})` and `f.await(io)` |
| `std.io.getStdOut().writer()` | a caller buffer, `std.fs.File.stdout().writer(&buf)`, print through `&fw.interface`, then `flush()`; **silent** if the flush is missing |
| `std.io.bufferedWriter`, `std.BoundedArray` | a buffer passed to `.writer(&buf)`; `ArrayList.initBuffer(&buf)`; old: `root source file struct 'std' has no member named 'BoundedArray'` |
| `print("{}", .{x})` where `x` has `format` | `{f}`; `{any}` skips the method; 0.15 refuses `{}` with `ambiguous format string` |
| `format(self, comptime fmt, options, writer: anytype)` | `format(self: @This(), writer: *std.Io.Writer) std.Io.Writer.Error!void` |
| `std.ArrayList(T).init(gpa)`, `append(x)`, `deinit()` | `var l: std.ArrayList(T) = .empty;`, `append(gpa, x)`, `deinit(gpa)`; old: `has no member named 'init'`, `expected 2 argument(s), found 1` |
| arithmetic on `undefined` | never; old: `use of undefined value here causes illegal behavior` |

## 0.16: I/O as an interface

| Old | New |
|---|---|
| `pub fn main() !void` building its own allocator | `pub fn main(init: std.process.Init) !void`, with `init.gpa`, `init.io`, `init.arena`; `std.process.Init.Minimal` for raw args |
| `std.process.argsAlloc(gpa)`, `std.os.environ` | `init.minimal.args.toSlice(init.arena.allocator())`, `init.environ_map`; old: `root source file struct 'process' has no member named 'argsAlloc'` |
| `std.fs.cwd().openFile(p, .{})`, `file.close()`, `std.fs.File` | `std.Io.Dir.cwd().openFile(io, p, .{})`, `file.close(io)`, `std.Io.File`; old: `root source file struct 'fs' has no member named 'cwd'` or `'File'` |
| `std.io`, `fixedBufferStream` | `std.Io.Reader.fixed(data)`, `std.Io.Writer.fixed(buf)`; old: `root source file struct 'std' has no member named 'io'` |
| stdout writer | `var fw: std.Io.File.Writer = .init(.stdout(), io, &buf);` then `&fw.interface` and `flush()` |
| `std.Thread.Pool`, `std.Thread.Mutex` | `std.Io.Group` with `g.async(io, f, args)` and `g.await(io)`; `std.Io.Mutex`; old: `root source file struct 'Thread' has no member named 'Pool'` |
| `std.time.milliTimestamp()`, `std.Thread.sleep` | `std.Io.Clock` `.now(io)`, `io.sleep(...)`; old: `root source file struct 'time' has no member named 'milliTimestamp'` |
| `std.process.Child.init(argv, gpa)` | `std.process.spawn(io, .{ .argv = argv })`, `std.process.run(gpa, io, .{...})` |
| no `io` at hand | tests: `std.testing.io`; elsewhere `std.Io.Threaded` |
| `@Type(.{ .int = ... })` | `@Int(.unsigned, 10)`, `@Struct`, `@Enum` and the rest; old: `invalid builtin function: '@Type'` |
| `std.mem.indexOf` | `std.mem.find`; the old name is deprecated and still compiles |
| `std.AutoArrayHashMap(K, V).init(gpa)` | `std.array_hash_map.Auto(K, V)` at `.empty`, gpa on each call |
| `{}` on a type with `format` | `{f}`; **silent**: `{}` prints the fields |
| returning `&local` | return by value or allocate; old: `returning address of expired local variable` |
| `@cImport` | deprecated: a `c.h` and `b.addTranslateC`; removed in 0.17 |
| dependencies in the global cache | `zig-pkg/` beside build.zig, every dependency needs a `fingerprint` |

## 0.17

| Old | New |
|---|---|
| `errdefer \|err\| ...` | removed: split the function and `catch \|err\|` in the caller |
| `[1]u8{0} ** n` | `const a: [n]u8 = @splat(0);` |
| `void{}` | `{}` |
| `@intFromEnum`, `@enumFromInt` | `@backingInt`, `@fromBackingInt`; old deprecated, still compiles |
| `std.builtin`, `OptimizeMode.ReleaseSafe`, `builtin.mode == .ReleaseFast` | `std.lang`, `std.lang.Optimize.safe`, `.fast`; flags `-Doptimize=safe`, `-Osafe` |
| `std.heap.DebugAllocator` | `std.heap.SafeAllocator`, `.init(std.heap.page_allocator, .{})`, or `init.gpa`; old deprecated |
| `if (b.args) \|args\| run_cmd.addArgs(args);` | `run_cmd.addPassthruArgs();` |
| `std.fmt.allocPrint(gpa, f, a)` | `gpa.print(f, a)`; old deprecated |
| `@bitCast` on arrays and vectors | **silent**: now endian-agnostic |
| `@hasDecl(T, "private")` | **silent**: false for a non-pub declaration |
| `containsAtLeastScalar(u8, s, 1, 'a')` | `containsAtLeastScalar(u8, s, 'a', 1)`; **silent** when the types coerce |
| ZLS | does not work with 0.17.0 |

## Master, 0.18.0-dev

| Old | New |
|---|---|
| `packed struct(u8)` | `bitpack struct(u8)`; `zig fmt` rewrites it |
| the deprecated 0.17 names | marked for removal after 0.18.0 |
