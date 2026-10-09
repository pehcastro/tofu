mod ast;
mod block;
mod component;
mod html;
mod inline;
mod json;
mod resolve;
mod scan;
mod stream;

pub use ast::{
    Alignment, Block, BlockKind, Cell, CodeKind, Component, Document, Inline, InlineKind, Link,
    List, ListItem, ListKind, Table,
};
pub use component::{PropError, PropKind, PropSpec, PropValue, Registry, validate};
pub use html::to_html;
pub use json::{Json, JsonError, JsonErrorKind, parse_json};
pub use stream::{Stream, visible_prefix};

pub const MAX_NESTING: usize = 64;
pub const MAX_JSON_DEPTH: usize = 64;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Options {
    pub gfm: bool,
    pub components: bool,
}

impl Options {
    pub const COMMONMARK: Self = Self {
        gfm: false,
        components: false,
    };
    pub const GFM: Self = Self {
        gfm: true,
        components: false,
    };
    pub const ALL: Self = Self {
        gfm: true,
        components: true,
    };
}

pub fn parse(source: &str, options: Options) -> Document {
    let parsed = block::parse(source, 0, options);
    let refs = block::refs(&parsed.defs);
    Document {
        blocks: resolve::blocks(&parsed.blocks, &refs, options),
    }
}
