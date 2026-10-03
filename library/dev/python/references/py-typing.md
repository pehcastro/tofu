---
id: py-typing
domain: dev
document: the typing best practices page of the Python typing documentation; agents-knowledge python-type-safety; trail of bits modern-python on type checkers; each ruff code checked against ruff rule on ruff 0.16.10
found: local clones of the collections and a saved copy of the typing page, kept outside this repository
---

# Types

Annotations are read by the type checker and, in some libraries, at run time. Neither makes a wrong value fail on its own: a value from outside is checked where it enters, and inside that line the types are trusted.

## Signatures

Annotate every public function, its parameters and its return, including `-> None`. Take the widest type the body needs and return the most precise one the caller can use:

| Parameter | Return |
| --- | --- |
| `Iterable[str]`, `Sequence[int]`, `Mapping[str, V]` from `collections.abc` | `list[str]`, `dict[str, int]` |
| `object` when any value is accepted, as for `str(x)` | the concrete type |

Use `Any` only where the type system cannot say the type; it switches the checker off for everything it touches. A callback whose result is ignored returns `object`. Avoid a union return type that forces every caller to check with `isinstance`.

## Spelling, by version

- From 3.9: `list[int]`, `dict[str, int]`, `type[C]`, never `typing.List`.
- From 3.10: `X | None` and `A | B`, never `Optional` or `Union`.
- From 3.11: `typing.Self` for a method returning its own class.
- From 3.12: `def first[T](items: Sequence[T]) -> T`, and `type Pair = tuple[int, int]`.

Below the `requires-python` line, keep the older form the project uses. `from __future__ import annotations` makes annotations strings, so new syntax in them parses on old versions, but pydantic, FastAPI and anything calling `get_type_hints` still evaluate them. ruff's `UP006`, `UP007`, `UP035` and `UP045` rewrite the old spellings when the target allows.

## Narrowing

A value typed `X | None` is checked before use: `if user is None: raise ...` narrows it for the rest of the block. `isinstance` narrows a union. `cast(T, v)` checks nothing at run time and is a claim the checker believes; use it only where you can say why it holds. `assert x is not None` narrows too and disappears under `-O`, so it never guards input.

## Shapes

| Data | Type |
| --- | --- |
| from outside: a request, a file, an environment | a pydantic model or a hand-checked parse at the boundary |
| owned and passed around inside | `@dataclass(frozen=True, slots=True)`, `slots` from 3.10 |
| a dict whose keys are fixed by a format you do not own | `TypedDict` |
| a closed set of values | `enum.Enum`, or `Literal["a", "b"]` for strings in a signature |
| a dependency a test swaps | a `Protocol` naming only the methods used |

A dataclass field with a mutable default needs `field(default_factory=list)`; a plain `[]` is refused when the class is created.

## Imports for types only

An import used only in annotations goes under `if TYPE_CHECKING:` when importing it at run time would make a cycle or cost start-up time. The annotations that use it then need quotes or the `__future__` import.

## The checker

Keep the project's checker and its settings: mypy reads `[tool.mypy]` or `mypy.ini`, pyright `[tool.pyright]` or `pyrightconfig.json`, ty `[tool.ty]`. A project on mypy with per-module strictness stays that way; a new module may follow the strictest setting already in use. Silence one finding with its code, `# type: ignore[arg-type]` or `# pyright: ignore[reportArgumentType]`, never bare; ruff's `PGH003` reports a bare `type: ignore`.
