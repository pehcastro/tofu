use std::path::Path;

use tree_sitter::Query;

use super::kind::{Capture, Kind};
use super::{Language, SyntaxError};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub(super) enum Source {
    Rust,
    Go,
    TypeScript,
    Tsx,
    JavaScript,
    Python,
    Json,
    Toml,
    Markdown,
    MarkdownInline,
    Bash,
    Css,
    Html,
    Yaml,
}

impl From<Language> for Source {
    fn from(language: Language) -> Self {
        match language {
            Language::Rust => Self::Rust,
            Language::Go => Self::Go,
            Language::TypeScript => Self::TypeScript,
            Language::Tsx => Self::Tsx,
            Language::JavaScript => Self::JavaScript,
            Language::Python => Self::Python,
            Language::Json => Self::Json,
            Language::Toml => Self::Toml,
            Language::Markdown => Self::Markdown,
            Language::Bash => Self::Bash,
        }
    }
}

#[derive(Clone, Copy)]
enum Precedence {
    First,
    Last,
}

pub(super) struct Highlights {
    pub(super) query: Query,
    pub(super) captures: Vec<Capture>,
    precedence: Precedence,
}

impl Highlights {
    fn new(
        owner: Source,
        language: &tree_sitter::Language,
        source: &str,
        precedence: Precedence,
    ) -> Result<Self, SyntaxError> {
        let query = compile(owner, language, source)?;
        let captures = query
            .capture_names()
            .iter()
            .map(|name| Kind::capture(name))
            .collect();
        Ok(Self {
            query,
            captures,
            precedence,
        })
    }

    pub(super) fn order(&self, pattern: usize) -> i64 {
        let pattern = i64::try_from(pattern).unwrap_or(i64::MAX);
        match self.precedence {
            Precedence::First => -pattern,
            Precedence::Last => pattern,
        }
    }
}

pub(super) struct Grammar {
    pub(super) source: Source,
    pub(super) language: tree_sitter::Language,
    pub(super) highlights: [Highlights; 2],
    pub(super) injections: Option<Query>,
}

struct Parts {
    language: tree_sitter::Language,
    precedence: Precedence,
    stock: &'static [&'static str],
    extension: &'static str,
    injections: &'static [&'static str],
}

impl Grammar {
    pub(super) fn new(source: Source) -> Result<Self, SyntaxError> {
        let Parts {
            language,
            precedence,
            stock,
            extension,
            injections,
        } = parts(source);
        let injections = match injections.join("\n") {
            text if text.is_empty() => None,
            text => Some(compile(source, &language, &text)?),
        };
        Ok(Self {
            source,
            highlights: [
                Highlights::new(source, &language, &stock.join("\n"), precedence)?,
                Highlights::new(source, &language, extension, Precedence::Last)?,
            ],
            language,
            injections,
        })
    }
}

fn compile(
    owner: Source,
    language: &tree_sitter::Language,
    text: &str,
) -> Result<Query, SyntaxError> {
    Query::new(language, text).or_else(|error| {
        eprintln!("desk: query error {owner:?}, opening plain: {error}");
        Query::new(language, "").map_err(SyntaxError::Query)
    })
}

impl Source {
    pub(super) fn from_path(path: &Path) -> Option<Self> {
        Language::from_path(path)
            .map(Self::from)
            .or_else(|| match path.extension()?.to_str()? {
                "css" => Some(Self::Css),
                "html" | "htm" => Some(Self::Html),
                "yml" | "yaml" => Some(Self::Yaml),
                _ => None,
            })
    }

    pub(super) fn from_name(name: &str) -> Option<Self> {
        match name.trim().to_ascii_lowercase().as_str() {
            "rust" | "rs" => Some(Self::Rust),
            "go" | "golang" => Some(Self::Go),
            "typescript" | "ts" => Some(Self::TypeScript),
            "tsx" => Some(Self::Tsx),
            "javascript" | "js" | "jsx" | "mjs" | "cjs" => Some(Self::JavaScript),
            "python" | "py" => Some(Self::Python),
            "json" | "jsonc" => Some(Self::Json),
            "toml" => Some(Self::Toml),
            "markdown" | "md" => Some(Self::Markdown),
            "markdown_inline" => Some(Self::MarkdownInline),
            "bash" | "sh" | "shell" | "zsh" | "console" => Some(Self::Bash),
            "css" | "styled" => Some(Self::Css),
            "html" | "htm" => Some(Self::Html),
            "yaml" | "yml" => Some(Self::Yaml),
            _ => None,
        }
    }
}

const JS: &str = tree_sitter_javascript::HIGHLIGHT_QUERY;
const JSX: &str = tree_sitter_javascript::JSX_HIGHLIGHT_QUERY;
const TS: &str = tree_sitter_typescript::HIGHLIGHTS_QUERY;
const JS_EXTENSION: &str = include_str!("../../queries/javascript/highlights.scm");
const TS_EXTENSION: &str = concat!(
    include_str!("../../queries/javascript/highlights.scm"),
    include_str!("../../queries/typescript/highlights.scm"),
);
const JS_INJECTIONS: &[&str] = &[
    tree_sitter_javascript::INJECTIONS_QUERY,
    include_str!("../../queries/javascript/injections.scm"),
];

fn parts(source: Source) -> Parts {
    match source {
        Source::MarkdownInline => Parts {
            language: tree_sitter_md::INLINE_LANGUAGE.into(),
            precedence: Precedence::Last,
            stock: &[tree_sitter_md::HIGHLIGHT_QUERY_INLINE],
            extension: include_str!("../../queries/markdown-inline/highlights.scm"),
            injections: &[],
        },
        Source::Rust => Parts {
            language: tree_sitter_rust::LANGUAGE.into(),
            precedence: Precedence::First,
            stock: &[tree_sitter_rust::HIGHLIGHTS_QUERY],
            extension: include_str!("../../queries/rust/highlights.scm"),
            injections: &[tree_sitter_rust::INJECTIONS_QUERY],
        },
        Source::Go => Parts {
            language: tree_sitter_go::LANGUAGE.into(),
            precedence: Precedence::First,
            stock: &[tree_sitter_go::HIGHLIGHTS_QUERY],
            extension: include_str!("../../queries/go/highlights.scm"),
            injections: &[],
        },
        Source::TypeScript => Parts {
            language: tree_sitter_typescript::LANGUAGE_TYPESCRIPT.into(),
            precedence: Precedence::Last,
            stock: &[JS, TS],
            extension: TS_EXTENSION,
            injections: JS_INJECTIONS,
        },
        Source::Tsx => Parts {
            language: tree_sitter_typescript::LANGUAGE_TSX.into(),
            precedence: Precedence::Last,
            stock: &[JS, JSX, TS],
            extension: TS_EXTENSION,
            injections: JS_INJECTIONS,
        },
        Source::JavaScript => Parts {
            language: tree_sitter_javascript::LANGUAGE.into(),
            precedence: Precedence::Last,
            stock: &[JS, JSX],
            extension: JS_EXTENSION,
            injections: JS_INJECTIONS,
        },
        Source::Python => Parts {
            language: tree_sitter_python::LANGUAGE.into(),
            precedence: Precedence::Last,
            stock: &[tree_sitter_python::HIGHLIGHTS_QUERY],
            extension: include_str!("../../queries/python/highlights.scm"),
            injections: &[],
        },
        Source::Json => Parts {
            language: tree_sitter_json::LANGUAGE.into(),
            precedence: Precedence::First,
            stock: &[tree_sitter_json::HIGHLIGHTS_QUERY],
            extension: include_str!("../../queries/json/highlights.scm"),
            injections: &[],
        },
        Source::Toml => Parts {
            language: tree_sitter_toml_ng::LANGUAGE.into(),
            precedence: Precedence::Last,
            stock: &[tree_sitter_toml_ng::HIGHLIGHTS_QUERY],
            extension: include_str!("../../queries/toml/highlights.scm"),
            injections: &[],
        },
        Source::Markdown => Parts {
            language: tree_sitter_md::LANGUAGE.into(),
            precedence: Precedence::Last,
            stock: &[tree_sitter_md::HIGHLIGHT_QUERY_BLOCK],
            extension: include_str!("../../queries/markdown/highlights.scm"),
            injections: &[tree_sitter_md::INJECTION_QUERY_BLOCK],
        },
        Source::Bash => Parts {
            language: tree_sitter_bash::LANGUAGE.into(),
            precedence: Precedence::Last,
            stock: &[tree_sitter_bash::HIGHLIGHT_QUERY],
            extension: include_str!("../../queries/bash/highlights.scm"),
            injections: &[],
        },
        Source::Css => Parts {
            language: tree_sitter_css::LANGUAGE.into(),
            precedence: Precedence::First,
            stock: &[tree_sitter_css::HIGHLIGHTS_QUERY],
            extension: include_str!("../../queries/css/highlights.scm"),
            injections: &[],
        },
        Source::Html => Parts {
            language: tree_sitter_html::LANGUAGE.into(),
            precedence: Precedence::Last,
            stock: &[tree_sitter_html::HIGHLIGHTS_QUERY],
            extension: include_str!("../../queries/html/highlights.scm"),
            injections: &[include_str!("../../queries/html/injections.scm")],
        },
        Source::Yaml => Parts {
            language: tree_sitter_yaml::LANGUAGE.into(),
            precedence: Precedence::Last,
            stock: &[tree_sitter_yaml::HIGHLIGHTS_QUERY],
            extension: include_str!("../../queries/yaml/highlights.scm"),
            injections: &[],
        },
    }
}
