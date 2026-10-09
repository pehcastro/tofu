mod grammar;
mod kind;

use std::cell::RefCell;
use std::cmp::Reverse;
use std::collections::{BTreeSet, HashMap};
use std::fmt;
use std::ops::Range;
use std::path::Path;

use ropey::Rope;
use tree_sitter::{
    InputEdit, LanguageError, Node, Parser, Point, QueryCursor, QueryError, StreamingIterator, Tree,
};

use crate::buffer::{Applied, Buffer};
use grammar::{Grammar, Source};
use kind::Capture;
pub use kind::Kind;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Language {
    Rust,
    Go,
    TypeScript,
    Tsx,
    JavaScript,
    Python,
    Json,
    Toml,
    Markdown,
    Bash,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Span {
    pub chars: Range<usize>,
    pub kind: Kind,
}

#[derive(Debug)]
pub enum SyntaxError {
    Language(LanguageError),
    Query(QueryError),
    ParseCancelled,
    OutOfRange { index: usize, len: usize },
}

impl fmt::Display for SyntaxError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Language(error) => write!(f, "{error}"),
            Self::Query(error) => write!(f, "{error}"),
            Self::ParseCancelled => write!(f, "the parse was cancelled"),
            Self::OutOfRange { index, len } => write!(f, "index {index} is past the end {len}"),
        }
    }
}

impl std::error::Error for SyntaxError {}

impl Language {
    pub fn from_path(path: &Path) -> Option<Self> {
        match path.extension()?.to_str()? {
            "rs" => Some(Self::Rust),
            "go" => Some(Self::Go),
            "ts" | "mts" | "cts" => Some(Self::TypeScript),
            "tsx" => Some(Self::Tsx),
            "js" | "mjs" | "cjs" | "jsx" => Some(Self::JavaScript),
            "py" | "pyi" => Some(Self::Python),
            "json" => Some(Self::Json),
            "toml" => Some(Self::Toml),
            "md" | "markdown" => Some(Self::Markdown),
            "sh" | "bash" => Some(Self::Bash),
            _ => None,
        }
    }
}

struct Layer {
    source: Source,
    tree: Tree,
    bytes: Range<usize>,
}

struct Paint {
    depth: u8,
    bytes: Range<usize>,
    rank: (usize, i64),
    kind: Option<Kind>,
}

const CHUNK_LINES: usize = 64;

pub struct Syntax {
    rope: Rope,
    parser: Parser,
    injected: Parser,
    tree: Tree,
    grammars: Vec<Grammar>,
    layers: Vec<Layer>,
    chunks: RefCell<HashMap<usize, Vec<Span>>>,
}

impl Syntax {
    pub fn new(language: Language, buffer: &Buffer) -> Result<Self, SyntaxError> {
        Self::from_source(language.into(), buffer)
    }

    pub fn for_path(path: &Path, buffer: &Buffer) -> Option<Result<Self, SyntaxError>> {
        Source::from_path(path).map(|source| Self::from_source(source, buffer))
    }

    fn from_source(source: Source, buffer: &Buffer) -> Result<Self, SyntaxError> {
        let root = Grammar::new(source)?;
        let mut parser = Parser::new();
        parser
            .set_language(&root.language)
            .map_err(SyntaxError::Language)?;
        let rope = buffer.rope().clone();
        let tree = parse(&mut parser, &rope, None)?;
        let mut syntax = Self {
            rope,
            parser,
            injected: Parser::new(),
            tree,
            grammars: vec![root],
            layers: Vec::new(),
            chunks: RefCell::default(),
        };
        syntax.inject()?;
        Ok(syntax)
    }

    pub fn sync(&mut self, buffer: &mut Buffer) -> Result<(), SyntaxError> {
        let applied = buffer.take_applied();
        if applied.is_empty() {
            return Ok(());
        }
        for Applied { chars, text } in &applied {
            let len = self.rope.len_chars();
            if chars.start > chars.end || chars.end > len {
                return Err(SyntaxError::OutOfRange {
                    index: chars.end,
                    len,
                });
            }
            let start_byte = self.rope.char_to_byte(chars.start);
            let old_end_byte = self.rope.char_to_byte(chars.end);
            let start_position = point(&self.rope, start_byte);
            let old_end_position = point(&self.rope, old_end_byte);
            self.rope.remove(chars.clone());
            self.rope.insert(chars.start, text);
            let new_end_byte = start_byte + text.len();
            let edit = InputEdit {
                start_byte,
                old_end_byte,
                new_end_byte,
                start_position,
                old_end_position,
                new_end_position: point(&self.rope, new_end_byte),
            };
            self.tree.edit(&edit);
            for layer in &mut self.layers {
                layer.tree.edit(&edit);
            }
        }
        self.rope = buffer.rope().clone();
        self.chunks.get_mut().clear();
        self.tree = parse(&mut self.parser, &self.rope, Some(&self.tree))?;
        self.inject()
    }

    pub fn report(&self) -> String {
        let unmatched: BTreeSet<&str> = self
            .grammars
            .iter()
            .flat_map(|grammar| &grammar.highlights)
            .flat_map(|highlights| {
                let names = highlights.query.capture_names();
                names
                    .iter()
                    .zip(&highlights.captures)
                    .filter(|(_, capture)| **capture == Capture::Unknown)
                    .map(|(name, _)| *name)
            })
            .collect();
        let grammars: Vec<String> = self
            .grammars
            .iter()
            .map(|grammar| format!("{:?}", grammar.source))
            .collect();
        format!(
            "{} with {} injected layers, {} captures matched no theme token{}{}",
            grammars.join(" + "),
            self.layers.len(),
            unmatched.len(),
            if unmatched.is_empty() { "" } else { ": " },
            unmatched.into_iter().collect::<Vec<_>>().join(", ")
        )
    }

    fn grammar(&self, source: Source) -> Option<&Grammar> {
        self.grammars
            .iter()
            .find(|grammar| grammar.source == source)
    }

    fn inject(&mut self) -> Result<(), SyntaxError> {
        let Some(root) = self.grammars.first() else {
            return Ok(());
        };
        let found = injections(root, &self.tree, &self.rope);
        let mut old: HashMap<(Source, Vec<tree_sitter::Range>), Tree> =
            std::mem::take(&mut self.layers)
                .into_iter()
                .map(|layer| ((layer.source, layer.tree.included_ranges()), layer.tree))
                .collect();
        for (source, ranges) in found {
            if self.grammar(source).is_none() {
                self.grammars.push(Grammar::new(source)?);
            }
            let (Some(language), Some(first), Some(last)) = (
                self.grammar(source).map(|grammar| grammar.language.clone()),
                ranges.first(),
                ranges.last(),
            ) else {
                continue;
            };
            let bytes = first.start_byte..last.end_byte;
            self.injected
                .set_language(&language)
                .map_err(SyntaxError::Language)?;
            if self.injected.set_included_ranges(&ranges).is_err() {
                continue;
            }
            let previous = old.remove(&(source, ranges));
            let tree = parse(&mut self.injected, &self.rope, previous.as_ref())?;
            self.layers.push(Layer {
                source,
                tree,
                bytes,
            });
        }
        Ok(())
    }

    pub fn spans(&self, lines: Range<usize>) -> Vec<Span> {
        let last = self.rope.len_lines();
        let chars = self.rope.line_to_char(lines.start.min(last))
            ..self.rope.line_to_char(lines.end.min(last));
        let mut spans = Vec::new();
        let mut chunks = self.chunks.borrow_mut();
        for chunk in lines.start / CHUNK_LINES..lines.end.min(last).div_ceil(CHUNK_LINES) {
            let found = chunks.entry(chunk).or_insert_with(|| {
                self.paint_lines(chunk * CHUNK_LINES..(chunk + 1) * CHUNK_LINES)
            });
            let first = found.partition_point(|span| span.chars.end <= chars.start);
            spans.extend(
                found
                    .iter()
                    .skip(first)
                    .take_while(|span| span.chars.start < chars.end)
                    .cloned(),
            );
        }
        spans
    }

    fn paint_lines(&self, lines: Range<usize>) -> Vec<Span> {
        let rope = &self.rope;
        let last = rope.len_lines();
        let from = rope.line_to_byte(lines.start.min(last));
        let to = rope.line_to_byte(lines.end.min(last));
        let mut found = Vec::new();
        if let Some(root) = self.grammars.first() {
            self.paints(root, &self.tree, 0, from..to, &mut found);
        }
        for layer in &self.layers {
            if layer.bytes.start >= to || layer.bytes.end <= from {
                continue;
            }
            if let Some(grammar) = self.grammar(layer.source) {
                self.paints(grammar, &layer.tree, 1, from..to, &mut found);
            }
        }
        found.sort_by_key(|paint| (paint.depth, Reverse(paint.bytes.len()), paint.rank));
        let mut owner = vec![None; to - from];
        for (index, paint) in found.iter().enumerate() {
            if let Some(bytes) = owner.get_mut(paint.bytes.start - from..paint.bytes.end - from) {
                bytes.fill(Some(index));
            }
        }
        let mut spans = Vec::new();
        let mut at = from;
        for run in owner.chunk_by(|a, b| a == b) {
            let end = at + run.len();
            if let Some(Some(index)) = run.first()
                && let Some(Paint {
                    kind: Some(kind), ..
                }) = found.get(*index)
                && !rope.byte_slice(at..end).chars().all(char::is_whitespace)
            {
                spans.push(Span {
                    chars: rope.byte_to_char(at)..rope.byte_to_char(end),
                    kind: *kind,
                });
            }
            at = end;
        }
        spans
    }

    fn paints(
        &self,
        grammar: &Grammar,
        tree: &Tree,
        depth: u8,
        bytes: Range<usize>,
        found: &mut Vec<Paint>,
    ) {
        let rope = &self.rope;
        for (rank, highlights) in grammar.highlights.iter().enumerate() {
            let mut cursor = QueryCursor::new();
            cursor.set_byte_range(bytes.clone());
            let root = tree.root_node();
            let mut captures = cursor.captures(&highlights.query, root, |node: Node| {
                rope.byte_slice(node.byte_range())
                    .chunks()
                    .map(str::as_bytes)
            });
            while let Some((found_match, index)) = captures.next() {
                let Some(capture) = found_match.captures.get(*index) else {
                    continue;
                };
                let kind = match highlights.captures.get(capture.index as usize) {
                    Some(Capture::Colour(kind)) => Some(*kind),
                    Some(Capture::Clear) => None,
                    Some(Capture::Unknown | Capture::Internal) | None => continue,
                };
                let range = capture.node.byte_range();
                let clipped = range.start.max(bytes.start)..range.end.min(bytes.end);
                if !clipped.is_empty() {
                    found.push(Paint {
                        depth,
                        bytes: clipped,
                        rank: (rank, highlights.order(found_match.pattern_index)),
                        kind,
                    });
                }
            }
        }
    }
}

type Injection = (Source, Vec<tree_sitter::Range>);

fn injections(grammar: &Grammar, tree: &Tree, rope: &Rope) -> Vec<Injection> {
    let Some(query) = &grammar.injections else {
        return Vec::new();
    };
    let content = query.capture_index_for_name("injection.content");
    let named = query.capture_index_for_name("injection.language");
    let text = |node: Node| rope.byte_slice(node.byte_range()).to_string();
    let mut cursor = QueryCursor::new();
    let mut matches = cursor.matches(query, tree.root_node(), |node: Node| {
        rope.byte_slice(node.byte_range())
            .chunks()
            .map(str::as_bytes)
    });
    let mut combined: Vec<(usize, Source, Vec<tree_sitter::Range>)> = Vec::new();
    let mut single = Vec::new();
    while let Some(found) = matches.next() {
        let settings = query.property_settings(found.pattern_index);
        let set = |key: &str| settings.iter().find(|setting| &*setting.key == key);
        let language = set("injection.language")
            .and_then(|setting| setting.value.as_deref().map(str::to_owned))
            .or_else(|| {
                found
                    .captures
                    .iter()
                    .find(|capture| Some(capture.index) == named)
                    .map(|capture| text(capture.node))
            });
        let Some(source) = language.as_deref().and_then(Source::from_name) else {
            continue;
        };
        let whole = set("injection.include-children").is_some();
        let ranges: Vec<tree_sitter::Range> = found
            .captures
            .iter()
            .filter(|capture| Some(capture.index) == content)
            .flat_map(|capture| content_ranges(capture.node, whole))
            .collect();
        if ranges.is_empty() {
            continue;
        }
        if set("injection.combined").is_none() {
            single.push((source, ranges));
            continue;
        }
        match combined
            .iter_mut()
            .find(|(pattern, known, _)| *pattern == found.pattern_index && *known == source)
        {
            Some((_, _, all)) => all.extend(ranges),
            None => combined.push((found.pattern_index, source, ranges)),
        }
    }
    single.extend(combined.into_iter().map(|(_, source, mut ranges)| {
        ranges.sort_by_key(|range| range.start_byte);
        ranges.dedup_by(|next, kept| next.start_byte < kept.end_byte);
        (source, ranges)
    }));
    single
}

fn content_ranges(node: Node, whole: bool) -> Vec<tree_sitter::Range> {
    let mut ranges = Vec::new();
    let mut rest = node.range();
    let mut walker = node.walk();
    for child in node.named_children(&mut walker).filter(|_| !whole) {
        let child = child.range();
        if rest.start_byte < child.start_byte {
            ranges.push(tree_sitter::Range {
                end_byte: child.start_byte,
                end_point: child.start_point,
                ..rest
            });
        }
        rest.start_byte = child.end_byte;
        rest.start_point = child.end_point;
    }
    if rest.start_byte < rest.end_byte {
        ranges.push(rest);
    }
    ranges
}

fn parse(parser: &mut Parser, rope: &Rope, old: Option<&Tree>) -> Result<Tree, SyntaxError> {
    let len = rope.len_bytes();
    let mut read = |byte: usize, _: Point| -> &[u8] {
        if byte >= len {
            return &[];
        }
        let (chunk, start, _, _) = rope.chunk_at_byte(byte);
        chunk.as_bytes().get(byte - start..).unwrap_or_default()
    };
    parser
        .parse_with_options(&mut read, old, None)
        .ok_or(SyntaxError::ParseCancelled)
}

fn point(rope: &Rope, byte: usize) -> Point {
    let row = rope.byte_to_line(byte);
    Point::new(row, byte - rope.line_to_byte(row))
}
