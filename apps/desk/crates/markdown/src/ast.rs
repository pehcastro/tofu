use std::ops::Range;

use crate::component::PropValue;

#[derive(Debug, Clone, PartialEq, Default)]
pub struct Document {
    pub blocks: Vec<Block>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Block {
    pub range: Range<usize>,
    pub kind: BlockKind,
}

#[derive(Debug, Clone, PartialEq)]
pub enum BlockKind {
    Heading { level: u8, inlines: Vec<Inline> },
    Paragraph(Vec<Inline>),
    ThematicBreak,
    BlockQuote(Vec<Block>),
    List(List),
    Code { kind: CodeKind, text: String },
    Html(String),
    Table(Table),
    Component(Component<Block>),
}

#[derive(Debug, Clone, PartialEq)]
pub enum CodeKind {
    Indented,
    Fenced { info: String },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ListKind {
    Bullet(char),
    Ordered { start: u32, delimiter: char },
}

#[derive(Debug, Clone, PartialEq)]
pub struct List {
    pub kind: ListKind,
    pub tight: bool,
    pub items: Vec<ListItem>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct ListItem {
    pub range: Range<usize>,
    pub task: Option<bool>,
    pub blocks: Vec<Block>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Alignment {
    None,
    Left,
    Center,
    Right,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Table {
    pub alignments: Vec<Alignment>,
    pub header: Vec<Cell>,
    pub rows: Vec<Vec<Cell>>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Cell {
    pub range: Range<usize>,
    pub inlines: Vec<Inline>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Inline {
    pub range: Range<usize>,
    pub kind: InlineKind,
}

#[derive(Debug, Clone, PartialEq)]
pub enum InlineKind {
    Text(String),
    Code(String),
    Emphasis(Vec<Inline>),
    Strong(Vec<Inline>),
    Strikethrough(Vec<Inline>),
    Link(Link),
    Image(Link),
    Autolink { destination: String, text: String },
    SoftBreak,
    HardBreak,
    Html(String),
    Component(Component<Inline>),
}

#[derive(Debug, Clone, PartialEq)]
pub struct Link {
    pub destination: String,
    pub title: String,
    pub children: Vec<Inline>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Component<C> {
    pub name: String,
    pub props: Vec<(String, PropValue)>,
    pub children: Vec<C>,
}
