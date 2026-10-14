use std::collections::{HashMap, HashSet};
use std::fs;
use std::mem::size_of;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use tree_sitter::{Parser, Query, QueryCursor, StreamingIterator};

use crate::syntax::{Language, SyntaxError};

const LARGEST_FILE: u64 = 1024 * 1024;
const RUST_TAGS: &str = include_str!("../queries/rust/tags.scm");
const TYPESCRIPT_TAGS: &str = include_str!("../queries/typescript/tags.scm");

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SymbolKind {
    Function,
    Method,
    Type,
    Constant,
    Module,
    Macro,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Symbol {
    pub name: Arc<str>,
    pub kind: SymbolKind,
    pub path: Arc<Path>,
    pub row: usize,
    pub col: usize,
}

#[derive(Clone, Default)]
pub struct Index {
    names: HashMap<Arc<str>, Vec<Symbol>>,
    files: Vec<Arc<Path>>,
}

#[derive(Clone, Copy)]
enum Role {
    Name,
    Definition(SymbolKind),
    Other,
}

struct Tags {
    language: Language,
    grammar: tree_sitter::Language,
    query: Query,
    roles: Vec<Role>,
}

struct Tagger {
    parser: Parser,
    tags: Vec<Tags>,
}

pub fn indexed(path: &Path) -> bool {
    Language::from_path(path).is_some_and(|language| {
        matches!(
            language,
            Language::Rust | Language::TypeScript | Language::Tsx
        )
    })
}

fn kin(a: Language, b: Language) -> bool {
    let script = |language| matches!(language, Language::TypeScript | Language::Tsx);
    a == b || script(a) && script(b)
}

fn role(capture: &str) -> Role {
    match capture {
        "name" => Role::Name,
        "definition.function" => Role::Definition(SymbolKind::Function),
        "definition.method" => Role::Definition(SymbolKind::Method),
        "definition.type" => Role::Definition(SymbolKind::Type),
        "definition.constant" => Role::Definition(SymbolKind::Constant),
        "definition.module" => Role::Definition(SymbolKind::Module),
        "definition.macro" => Role::Definition(SymbolKind::Macro),
        _ => Role::Other,
    }
}

fn read(path: &Path) -> Result<String, String> {
    let size = fs::metadata(path).map_err(|error| error.to_string())?.len();
    if size > LARGEST_FILE {
        return Err(format!("{size} bytes is over the {LARGEST_FILE} indexed"));
    }
    fs::read_to_string(path).map_err(|error| error.to_string())
}

impl Tagger {
    fn new() -> Result<Self, SyntaxError> {
        let grammars: [(Language, tree_sitter::Language, &str); 3] = [
            (Language::Rust, tree_sitter_rust::LANGUAGE.into(), RUST_TAGS),
            (
                Language::TypeScript,
                tree_sitter_typescript::LANGUAGE_TYPESCRIPT.into(),
                TYPESCRIPT_TAGS,
            ),
            (
                Language::Tsx,
                tree_sitter_typescript::LANGUAGE_TSX.into(),
                TYPESCRIPT_TAGS,
            ),
        ];
        let tags = grammars
            .into_iter()
            .map(|(language, grammar, source)| {
                let query = Query::new(&grammar, source).map_err(SyntaxError::Query)?;
                let roles = query
                    .capture_names()
                    .iter()
                    .map(|name| role(name))
                    .collect();
                Ok(Tags {
                    language,
                    grammar,
                    query,
                    roles,
                })
            })
            .collect::<Result<_, SyntaxError>>()?;
        Ok(Self {
            parser: Parser::new(),
            tags,
        })
    }

    fn tag(&mut self, path: &Arc<Path>, found: &mut Vec<Symbol>) -> Result<bool, SyntaxError> {
        let language = Language::from_path(path);
        let Some(tags) = self
            .tags
            .iter()
            .find(|tags| Some(tags.language) == language)
        else {
            return Ok(false);
        };
        let text = match read(path) {
            Ok(text) => text,
            Err(error) => {
                eprintln!("desk: symbols skipped {}: {error}", path.display());
                return Ok(false);
            }
        };
        self.parser
            .set_language(&tags.grammar)
            .map_err(SyntaxError::Language)?;
        let tree = self
            .parser
            .parse(&text, None)
            .ok_or(SyntaxError::ParseCancelled)?;
        let mut cursor = QueryCursor::new();
        let mut matches = cursor.matches(&tags.query, tree.root_node(), text.as_bytes());
        while let Some(matched) = matches.next() {
            let (mut name, mut kind) = (None, None);
            for capture in matched.captures {
                match tags.roles.get(capture.index as usize) {
                    Some(Role::Name) => name = Some(capture.node),
                    Some(Role::Definition(defined)) => kind = Some(*defined),
                    Some(Role::Other) | None => {}
                }
            }
            let (Some(node), Some(kind)) = (name, kind) else {
                continue;
            };
            let Some(spelled) = text.get(node.byte_range()) else {
                continue;
            };
            let (start, at) = (node.start_byte(), node.start_position());
            let line = start.saturating_sub(at.column);
            found.push(Symbol {
                name: spelled.into(),
                kind,
                path: path.clone(),
                row: at.row,
                col: text
                    .get(line..start)
                    .map_or(0, |before| before.chars().count()),
            });
        }
        Ok(true)
    }
}

impl Index {
    pub fn build(files: &[PathBuf]) -> Result<Self, SyntaxError> {
        let mut index = Self::default();
        index.retag(files)?;
        Ok(index)
    }

    pub fn retag(&mut self, files: &[PathBuf]) -> Result<(), SyntaxError> {
        let mut tagger = Tagger::new()?;
        if !self.files.is_empty() {
            let gone: HashSet<&Path> = files.iter().map(PathBuf::as_path).collect();
            self.files.retain(|file| !gone.contains(&**file));
            self.names.retain(|_, found| {
                found.retain(|symbol| !gone.contains(&*symbol.path));
                !found.is_empty()
            });
        }
        let mut found = Vec::new();
        for file in files {
            let path: Arc<Path> = Arc::from(file.as_path());
            if tagger.tag(&path, &mut found)? {
                self.files.push(path);
            }
            for symbol in found.drain(..) {
                self.names
                    .entry(symbol.name.clone())
                    .or_default()
                    .push(symbol);
            }
        }
        Ok(())
    }

    pub fn definitions<'a>(
        &'a self,
        name: &str,
        from: Language,
    ) -> impl Iterator<Item = &'a Symbol> + 'a {
        self.names
            .get(name)
            .into_iter()
            .flatten()
            .filter(move |symbol| {
                Language::from_path(&symbol.path).is_some_and(|language| kin(language, from))
            })
    }

    pub fn files(&self) -> usize {
        self.files.len()
    }

    pub fn symbols(&self) -> usize {
        self.names.values().map(Vec::len).sum()
    }

    pub fn bytes(&self) -> usize {
        let arc = 2 * size_of::<usize>();
        let slots = self.names.capacity() * size_of::<(Arc<str>, Vec<Symbol>)>();
        let lists: usize = self
            .names
            .iter()
            .map(|(name, found)| {
                found.capacity() * size_of::<Symbol>() + found.len() * (name.len() + arc)
            })
            .sum();
        let paths: usize = self
            .files
            .iter()
            .map(|path| path.as_os_str().len() + arc)
            .sum();
        slots + lists + paths + self.files.capacity() * size_of::<Arc<Path>>()
    }
}

#[cfg(test)]
mod tests {
    use std::error::Error;
    use std::fs;

    use super::SymbolKind::{Constant, Function, Macro, Method, Module, Type};
    use super::*;

    const RUST: &str = "mod inner {}
pub struct Point;
enum Shape { Round }
trait Draw {}
const LIMIT: u8 = 1;
macro_rules! shout { () => {} }
impl Point { fn new() -> Self { Point } }
impl Shape { fn new() -> Self { Shape::Round } }
const S: &str = \"\u{e9}\"; fn after() {}
";

    const TS: &str = "import { helper } from './b';
export function run() { return helper(); }
export const arrow = () => 1;
class Box { open() {} }
interface Shape {}
type Id = string;
enum Mode { On }
";

    type Row<'a> = (&'a str, &'a str, SymbolKind, usize, usize);

    fn place(index: &Index, name: &str, from: Language) -> Vec<(String, SymbolKind, usize, usize)> {
        let file = |symbol: &Symbol| {
            let name = symbol.path.file_name().unwrap_or_default();
            name.to_string_lossy().into_owned()
        };
        index
            .definitions(name, from)
            .map(|symbol| (file(symbol), symbol.kind, symbol.row, symbol.col))
            .collect()
    }

    fn single(index: &Index, from: Language, table: &[Row]) {
        for &(name, file, kind, row, col) in table {
            assert_eq!(
                place(index, name, from),
                [(file.to_owned(), kind, row, col)],
                "{name}"
            );
        }
    }

    #[test]
    fn rust_definitions_carry_kind_row_and_char_column() -> Result<(), Box<dyn Error>> {
        let dir = tempfile::tempdir()?;
        let file = dir.path().join("a.rs");
        fs::write(&file, RUST)?;
        let index = Index::build(&[file])?;
        single(
            &index,
            Language::Rust,
            &[
                ("inner", "a.rs", Module, 0, 4),
                ("Point", "a.rs", Type, 1, 11),
                ("Shape", "a.rs", Type, 2, 5),
                ("Round", "a.rs", Constant, 2, 13),
                ("Draw", "a.rs", Type, 3, 6),
                ("LIMIT", "a.rs", Constant, 4, 6),
                ("shout", "a.rs", Macro, 5, 13),
                ("after", "a.rs", Function, 8, 24),
            ],
        );
        let new = [
            ("a.rs".to_owned(), Function, 6, 16),
            ("a.rs".to_owned(), Function, 7, 16),
        ];
        assert_eq!(place(&index, "new", Language::Rust), new);
        assert!(place(&index, "Self", Language::Rust).is_empty());
        Ok(())
    }

    #[test]
    fn typescript_definitions_skip_imports_and_parse_tsx() -> Result<(), Box<dyn Error>> {
        let dir = tempfile::tempdir()?;
        let a = dir.path().join("a.ts");
        let b = dir.path().join("b.ts");
        let app = dir.path().join("app.tsx");
        fs::write(&a, TS)?;
        fs::write(&b, "export function helper() { return 1; }\n")?;
        fs::write(&app, "export function App() { return <div />; }\n")?;
        let index = Index::build(&[a, b, app])?;
        single(
            &index,
            Language::TypeScript,
            &[
                ("helper", "b.ts", Function, 0, 16),
                ("run", "a.ts", Function, 1, 16),
                ("arrow", "a.ts", Constant, 2, 13),
                ("Box", "a.ts", Type, 3, 6),
                ("open", "a.ts", Method, 3, 12),
                ("Shape", "a.ts", Type, 4, 10),
                ("Id", "a.ts", Type, 5, 5),
                ("Mode", "a.ts", Type, 6, 5),
            ],
        );
        let tsx = Language::Tsx;
        single(&index, tsx, &[("App", "app.tsx", Function, 0, 16)]);
        Ok(())
    }

    #[test]
    fn a_name_answers_only_in_its_own_language() -> Result<(), Box<dyn Error>> {
        let dir = tempfile::tempdir()?;
        let (rs, ts) = (dir.path().join("h.rs"), dir.path().join("h.ts"));
        fs::write(&rs, "fn helper() {}\n")?;
        fs::write(&ts, "function helper() {}\n")?;
        let index = Index::build(&[rs, ts])?;
        let (rust, tsx) = (Language::Rust, Language::Tsx);
        single(&index, rust, &[("helper", "h.rs", Function, 0, 3)]);
        single(&index, tsx, &[("helper", "h.ts", Function, 0, 9)]);
        Ok(())
    }

    #[test]
    fn a_missing_or_unsupported_file_is_skipped_not_fatal() -> Result<(), Box<dyn Error>> {
        let dir = tempfile::tempdir()?;
        let (kept, notes) = (dir.path().join("k.rs"), dir.path().join("n.md"));
        fs::write(&kept, "fn kept() {}\n")?;
        fs::write(&notes, "# kept\n")?;
        assert!(indexed(&kept) && !indexed(&notes));
        let index = Index::build(&[dir.path().join("gone.rs"), notes, kept])?;
        single(&index, Language::Rust, &[("kept", "k.rs", Function, 0, 3)]);
        assert_eq!((index.files(), index.symbols()), (1, 1));
        Ok(())
    }

    #[test]
    fn retag_moves_a_changed_file_and_drops_a_gone_name() -> Result<(), Box<dyn Error>> {
        let dir = tempfile::tempdir()?;
        let (moved, other) = (dir.path().join("m.rs"), dir.path().join("o.rs"));
        fs::write(&moved, "fn stay() {}\nfn gone() {}\n")?;
        fs::write(&other, "fn other() {}\n")?;
        let mut index = Index::build(&[moved.clone(), other])?;
        fs::write(&moved, "\n\nfn stay() {}\n")?;
        index.retag(&[moved])?;
        single(
            &index,
            Language::Rust,
            &[
                ("stay", "m.rs", Function, 2, 3),
                ("other", "o.rs", Function, 0, 3),
            ],
        );
        assert!(place(&index, "gone", Language::Rust).is_empty());
        assert_eq!((index.files(), index.symbols()), (2, 2));
        Ok(())
    }
}
