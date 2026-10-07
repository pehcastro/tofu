use std::cmp::Reverse;
use std::fmt;
use std::ops::Range;
use std::path::Path;

use ropey::Rope;
use tree_sitter::{
    InputEdit, LanguageError, Node, Parser, Point, Query, QueryCursor, QueryError,
    StreamingIterator, Tree,
};

use crate::buffer::{Applied, Buffer};

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

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    Keyword,
    String,
    Number,
    Comment,
    Function,
    Type,
    Variable,
    Constant,
    Operator,
    Punctuation,
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

    fn grammar(self) -> (tree_sitter::Language, String) {
        let js = tree_sitter_javascript::HIGHLIGHT_QUERY;
        let jsx = tree_sitter_javascript::JSX_HIGHLIGHT_QUERY;
        let ts = tree_sitter_typescript::HIGHLIGHTS_QUERY;
        match self {
            Self::Rust => (
                tree_sitter_rust::LANGUAGE.into(),
                tree_sitter_rust::HIGHLIGHTS_QUERY.into(),
            ),
            Self::Go => (
                tree_sitter_go::LANGUAGE.into(),
                tree_sitter_go::HIGHLIGHTS_QUERY.into(),
            ),
            Self::TypeScript => (
                tree_sitter_typescript::LANGUAGE_TYPESCRIPT.into(),
                [js, ts].join("\n"),
            ),
            Self::Tsx => (
                tree_sitter_typescript::LANGUAGE_TSX.into(),
                [js, jsx, ts].join("\n"),
            ),
            Self::JavaScript => (
                tree_sitter_javascript::LANGUAGE.into(),
                [js, jsx].join("\n"),
            ),
            Self::Python => (
                tree_sitter_python::LANGUAGE.into(),
                tree_sitter_python::HIGHLIGHTS_QUERY.into(),
            ),
            Self::Json => (
                tree_sitter_json::LANGUAGE.into(),
                tree_sitter_json::HIGHLIGHTS_QUERY.into(),
            ),
            Self::Toml => (
                tree_sitter_toml_ng::LANGUAGE.into(),
                tree_sitter_toml_ng::HIGHLIGHTS_QUERY.into(),
            ),
            Self::Markdown => (
                tree_sitter_md::LANGUAGE.into(),
                tree_sitter_md::HIGHLIGHT_QUERY_BLOCK.into(),
            ),
            Self::Bash => (
                tree_sitter_bash::LANGUAGE.into(),
                tree_sitter_bash::HIGHLIGHT_QUERY.into(),
            ),
        }
    }
}

impl Kind {
    fn from_capture(name: &str) -> Option<Self> {
        match name.split('.').next()? {
            "keyword" => Some(Self::Keyword),
            "string" | "escape" => Some(Self::String),
            "number" | "float" => Some(Self::Number),
            "comment" => Some(Self::Comment),
            "function" | "method" => Some(Self::Function),
            "type" | "constructor" => Some(Self::Type),
            "variable" | "property" => Some(Self::Variable),
            "constant" | "boolean" => Some(Self::Constant),
            "operator" => Some(Self::Operator),
            "punctuation" => Some(Self::Punctuation),
            _ => None,
        }
    }

    pub fn name(self) -> &'static str {
        match self {
            Self::Keyword => "keyword",
            Self::String => "string",
            Self::Number => "number",
            Self::Comment => "comment",
            Self::Function => "function",
            Self::Type => "type",
            Self::Variable => "variable",
            Self::Constant => "constant",
            Self::Operator => "operator",
            Self::Punctuation => "punctuation",
        }
    }
}

pub struct Syntax {
    rope: Rope,
    parser: Parser,
    tree: Tree,
    query: Query,
    kinds: Vec<Option<Kind>>,
}

impl Syntax {
    pub fn new(language: Language, buffer: &Buffer) -> Result<Self, SyntaxError> {
        let (grammar, source) = language.grammar();
        let query = Query::new(&grammar, &source).map_err(SyntaxError::Query)?;
        let kinds = query
            .capture_names()
            .iter()
            .map(|name| Kind::from_capture(name))
            .collect();
        let mut parser = Parser::new();
        parser
            .set_language(&grammar)
            .map_err(SyntaxError::Language)?;
        let rope = buffer.rope().clone();
        let tree = parse(&mut parser, &rope, None)?;
        Ok(Self {
            rope,
            parser,
            tree,
            query,
            kinds,
        })
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
            self.tree.edit(&InputEdit {
                start_byte,
                old_end_byte,
                new_end_byte,
                start_position,
                old_end_position,
                new_end_position: point(&self.rope, new_end_byte),
            });
        }
        self.rope = buffer.rope().clone();
        self.tree = parse(&mut self.parser, &self.rope, Some(&self.tree))?;
        Ok(())
    }

    pub fn spans(&self, lines: Range<usize>) -> Vec<Span> {
        let rope = &self.rope;
        let last = rope.len_lines();
        let from = rope.line_to_byte(lines.start.min(last));
        let to = rope.line_to_byte(lines.end.min(last));
        let mut cursor = QueryCursor::new();
        cursor.set_byte_range(from..to);
        let mut captures = cursor.captures(&self.query, self.tree.root_node(), |node: Node| {
            rope.byte_slice(node.byte_range())
                .chunks()
                .map(str::as_bytes)
        });
        let mut found = Vec::new();
        while let Some((found_match, index)) = captures.next() {
            let Some(capture) = found_match.captures.get(*index) else {
                continue;
            };
            let Some(Some(kind)) = self.kinds.get(capture.index as usize) else {
                continue;
            };
            let kind = match capture.node.kind() {
                "integer_literal" | "float_literal" => &Kind::Number,
                _ => kind,
            };
            let range = capture.node.byte_range();
            let clipped = range.start.max(from)..range.end.min(to);
            if !clipped.is_empty() {
                found.push((clipped, found_match.pattern_index, *kind));
            }
        }
        found.sort_by_key(|(range, pattern, _)| (Reverse(range.len()), *pattern));
        let mut owner = vec![None; to - from];
        for (index, (range, _, _)) in found.iter().enumerate() {
            if let Some(bytes) = owner.get_mut(range.start - from..range.end - from) {
                bytes.fill(Some(index));
            }
        }
        let mut spans = Vec::new();
        let mut at = from;
        for run in owner.chunk_by(|a, b| a == b) {
            let end = at + run.len();
            if let Some(Some(index)) = run.first()
                && let Some((_, _, kind)) = found.get(*index)
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
