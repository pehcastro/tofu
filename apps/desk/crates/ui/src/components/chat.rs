use std::cell::Cell;
use std::ops::Range;
use std::rc::Rc;

use desk_core::buffer::Buffer;
use desk_core::syntax::{Kind, Language, Syntax};
use desk_markdown::{
    self as md, Alignment, BlockKind, CodeKind, Inline, InlineKind, ListKind, Options,
};
use gpui::{
    AnyElement, App, Div, ElementId, FontStyle, FontWeight, HighlightStyle, InteractiveText, Rgba,
    SharedString, Stateful, StrikethroughStyle, StyledText, TextRun, Transformation,
    UnderlineStyle, div, font, prelude::*, px, radians, rgb_to_hsla, rgba,
};

use crate::components::avatar::spinner;
use crate::components::chip::{file_chip, mono};
use crate::components::code_editor::syntax_token;
use crate::components::find::{find_ranges, match_highlights};
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink, ring, tint};
use crate::components::size::{
    CALLS_GAP, CALLS_INDENT, CALLS_PAD_X, CALLS_PAD_Y, CAPTION_TEXT, CODE_FILL, FAIL_RING,
    FAIL_TINT, FIELD, FONT_BODY, FONT_CHAT, FONT_SMALL, FONT_TAB, FONT_WHO, FOOT_GAP, FOOT_TEXT,
    HOVER_TEXT, LEAD_GAP, MENU_PAD, NOTE_GAP, RADIUS_BUBBLE, RADIUS_CHIP_SMALL, SHELL_TEXT, T1, T2,
    T3, TOOL_GAP, YOU_GAP, YOU_MAX, YOU_PAD_BOTTOM, YOU_PAD_TOP, YOU_PAD_X,
};
use crate::metrics::ICON_SMALL;
use crate::theme::{ColorToken, Theme, WordToken};

pub const CHAT_LINE: f32 = 23.0;
const ROW_LINE: f32 = 20.0;
const CALL_LINE: f32 = 19.0;
const WHO_LINE: f32 = 18.0;
const WHO_GAP: f32 = 2.0;
const WHO_ITEMS: f32 = 8.0;
const YOU_FILL: f32 = 0.08;
const QUEUED_RING: f32 = 0.14;
const QUEUED_GAP: f32 = 16.0;
const ATTACH_GAP: f32 = 6.0;
const ROW_RADIUS: f32 = 9.0;
const ROW_ITEMS: f32 = 8.0;
const FAIL_PAD_X: f32 = 10.0;
const FAIL_PAD_Y: f32 = 6.0;
const MARK: f32 = 13.0;
const WAIT_DOT: f32 = 9.0;
const CALLS_SHADE: f32 = 0.2;
const CALL_TOOL: f32 = 44.0;
const CALL_NOTE_LEFT: f32 = 52.0;
const CALL_NOTE_TOP: f32 = 4.0;
const AGENT_FILL: f32 = 0.14;
const AGENT_TEXT: u32 = 0xcfc2f2ff;
const MENTION_FILL: u32 = 0xb9a6ea1f;
const MENTION_TEXT: u32 = 0xd6cbf5ff;
const PILL_PAD_X: f32 = 7.0;
const PILL_PAD_Y: f32 = 3.0;
const HEADING_GAP: f32 = 10.0;
const BULLETS_GAP: f32 = 4.0;
const BULLET_INDENT: f32 = 20.0;
const PARA_GAP: f32 = 6.0;
const CODE_PAD: char = '\u{202f}';
const QUOTE_PAD: f32 = 12.0;
const RULE_INK: f32 = 0.12;
const RULE_HEIGHT: f32 = 1.0;
const TEXT_LINE: f32 = 1.0;
const CELL_PAD_X: f32 = 10.0;
const CELL_PAD_Y: f32 = 4.0;
const TASK_BOX: f32 = 11.0;
const TASK_RADIUS: f32 = 3.0;
const LINK_SCHEMES: [&str; 3] = ["https://", "http://", "mailto:"];
const STRONG: Stress = Stress {
    strong: true,
    italic: false,
    struck: false,
};

pub type Marks = Vec<(Range<usize>, HighlightStyle)>;

pub const FIND_RESERVE: f32 = MENU_PAD * 4.0 + FIELD;

pub struct Hit {
    pub item: usize,
    pub piece: usize,
    pub range: Range<usize>,
}

pub fn find_hits(items: impl IntoIterator<Item = Vec<String>>, query: &str) -> Vec<Hit> {
    let mut hits = Vec::new();
    for (item, pieces) in items.into_iter().enumerate() {
        for (piece, text) in pieces.iter().enumerate() {
            hits.extend(find_ranges(text, query).into_iter().map(|range| Hit {
                item,
                piece,
                range,
            }));
        }
    }
    hits
}

pub fn hit_marks(hits: &[Hit], current: usize, item: usize, theme: &Theme) -> Vec<Marks> {
    let mine: Vec<(usize, &Hit)> = hits
        .iter()
        .enumerate()
        .filter(|(_, hit)| hit.item == item)
        .collect();
    let count = mine.iter().map(|(_, hit)| hit.piece + 1).max().unwrap_or(0);
    (0..count)
        .map(|piece| {
            let here: Vec<&(usize, &Hit)> =
                mine.iter().filter(|(_, hit)| hit.piece == piece).collect();
            let ranges: Vec<Range<usize>> = here.iter().map(|(_, hit)| hit.range.clone()).collect();
            let shown = here.iter().position(|(at, _)| *at == current);
            match_highlights(&ranges, shown, theme)
        })
        .collect()
}

pub struct Call {
    pub tool: SharedString,
    pub arg: SharedString,
    pub out: SharedString,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Face {
    Plain,
    Code,
    Mention,
}

#[derive(Clone, Copy, Default, PartialEq, Eq)]
pub struct Stress {
    pub strong: bool,
    pub italic: bool,
    pub struck: bool,
}

#[derive(Clone)]
pub struct Span {
    pub text: SharedString,
    pub face: Face,
    pub stress: Stress,
    pub link: Option<SharedString>,
}

impl Span {
    pub fn new(face: Face, text: impl Into<SharedString>) -> Self {
        Self {
            text: text.into(),
            face,
            stress: Stress::default(),
            link: None,
        }
    }
}

pub struct ListItem {
    pub task: Option<bool>,
    pub blocks: Vec<Block>,
}

pub struct Code {
    pub text: SharedString,
    pub paints: Vec<(Range<usize>, Kind)>,
}

pub enum Block {
    Para(Vec<Span>),
    Heading(Vec<Span>),
    List {
        start: Option<u32>,
        items: Vec<ListItem>,
    },
    Quote(Vec<Block>),
    Code(Code),
    Table {
        alignments: Vec<Alignment>,
        rows: Vec<Vec<Vec<Span>>>,
    },
    Rule,
    Source(SharedString),
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum AgentMark {
    Done,
    Working,
    Asking,
    Failed,
}

pub struct Agent {
    pub mark: AgentMark,
    pub name: SharedString,
    pub summary: SharedString,
    pub link: Option<SharedString>,
}

#[derive(Clone)]
pub enum Verdict {
    Exit(SharedString),
    Ask,
}

fn dim(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .flex_none()
        .text_color(ink(theme, T3))
        .child(text.into())
}

fn line(text: impl Into<SharedString>) -> Div {
    div().min_w_0().truncate().child(text.into())
}

fn agent(name: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .flex_none()
        .px(px(PILL_PAD_X))
        .py(px(PILL_PAD_Y))
        .rounded(px(RADIUS_CHIP_SMALL))
        .bg(tint(theme.color(ColorToken::Trace), AGENT_FILL))
        .text_color(rgba(AGENT_TEXT))
        .text_size(px(FONT_WHO))
        .font_weight(FontWeight::MEDIUM)
        .line_height(px(FONT_WHO))
        .child(name.into())
}

fn who(name: impl IntoElement, time: impl Into<SharedString>, traced: bool, theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(WHO_ITEMS))
        .mb(px(WHO_GAP))
        .text_size(px(FONT_WHO))
        .line_height(px(WHO_LINE))
        .text_color(ink(theme, CAPTION_TEXT))
        .child(name)
        .child(div().flex_none().child(time.into()))
        .when(traced, |who| {
            who.child(glyph(
                Glyph::Trace,
                ICON_SMALL,
                theme.color(ColorToken::Trace),
            ))
        })
}

fn marked(text: SharedString, marks: &[(Range<usize>, HighlightStyle)]) -> AnyElement {
    match marks.is_empty() {
        true => text.into_any_element(),
        false => StyledText::new(text)
            .with_highlights(marks.to_vec())
            .into_any_element(),
    }
}

fn bubble(theme: &Theme) -> Div {
    div()
        .min_w_0()
        .max_w(px(YOU_MAX))
        .rounded(px(RADIUS_BUBBLE))
        .px(px(YOU_PAD_X))
        .pt(px(YOU_PAD_TOP))
        .pb(px(YOU_PAD_BOTTOM))
        .text_size(px(FONT_CHAT))
        .line_height(px(CHAT_LINE))
        .text_color(theme.color(ColorToken::TextBase))
}

pub fn you(
    time: impl Into<SharedString>,
    text: impl Into<SharedString>,
    traced: bool,
    attached: Option<SharedString>,
    first: bool,
    marks: &[(Range<usize>, HighlightStyle)],
    theme: &Theme,
) -> Div {
    div()
        .flex()
        .flex_col()
        .items_end()
        .gap(px(ATTACH_GAP))
        .when(!first, |row| row.mt(px(YOU_GAP)))
        .children(attached.map(|name| {
            file_chip(
                glyph(Glyph::File, ICON_SMALL, ink(theme, CAPTION_TEXT)),
                name,
                theme,
            )
        }))
        .child(
            bubble(theme)
                .bg(ink(theme, YOU_FILL))
                .child(who("You", time, traced, theme))
                .child(marked(text.into(), marks)),
        )
}

pub fn queued(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div().flex().justify_end().mt(px(QUEUED_GAP)).child(
        bubble(theme)
            .shadow(vec![ring(ink(theme, QUEUED_RING))])
            .child(who("You", "queued", false, theme))
            .child(text.into()),
    )
}

pub fn markdown(source: &str) -> Vec<Block> {
    Reader {
        source,
        painted: true,
    }
    .blocks(&md::parse(source, Options::ALL).blocks)
}

pub struct Streaming {
    stream: md::Stream,
    text: String,
}

impl Default for Streaming {
    fn default() -> Self {
        Self {
            stream: md::Stream::new(Options::ALL),
            text: String::new(),
        }
    }
}

impl Streaming {
    pub fn blocks(&mut self, text: &str) -> Vec<Block> {
        match text.strip_prefix(self.text.as_str()) {
            Some(delta) => {
                self.stream.push(delta);
                self.text.push_str(delta);
            }
            None => {
                *self = Self::default();
                self.stream.push(text);
                self.text.push_str(text);
            }
        }
        let document = self.stream.snapshot();
        Reader {
            source: md::visible_prefix(&self.text),
            painted: false,
        }
        .blocks(&document.blocks)
    }
}

struct Reader<'a> {
    source: &'a str,
    painted: bool,
}

impl Reader<'_> {
    fn blocks(&self, blocks: &[md::Block]) -> Vec<Block> {
        blocks.iter().map(|block| self.block(block)).collect()
    }

    fn source(&self, range: &Range<usize>) -> SharedString {
        self.source
            .get(range.clone())
            .unwrap_or_default()
            .trim_end()
            .to_owned()
            .into()
    }

    fn block(&self, block: &md::Block) -> Block {
        match &block.kind {
            BlockKind::Heading { inlines, .. } => Block::Heading(self.spans(inlines, STRONG)),
            BlockKind::Paragraph(inlines) => Block::Para(self.spans(inlines, Stress::default())),
            BlockKind::ThematicBreak => Block::Rule,
            BlockKind::BlockQuote(inner) => Block::Quote(self.blocks(inner)),
            BlockKind::List(list) => Block::List {
                start: match list.kind {
                    ListKind::Ordered { start, .. } => Some(start),
                    ListKind::Bullet(_) => None,
                },
                items: list
                    .items
                    .iter()
                    .map(|item| ListItem {
                        task: item.task,
                        blocks: self.blocks(&item.blocks),
                    })
                    .collect(),
            },
            BlockKind::Code { kind, text } => Block::Code(self.code(kind, text)),
            BlockKind::Table(table) => Block::Table {
                alignments: table.alignments.clone(),
                rows: std::iter::once((&table.header, STRONG))
                    .chain(table.rows.iter().map(|row| (row, Stress::default())))
                    .map(|(row, stress)| {
                        row.iter()
                            .map(|cell| self.spans(&cell.inlines, stress))
                            .collect()
                    })
                    .collect(),
            },
            BlockKind::Html(_) | BlockKind::Component(_) => {
                Block::Source(self.source(&block.range))
            }
        }
    }

    fn spans(&self, inlines: &[Inline], stress: Stress) -> Vec<Span> {
        let mut spans = Vec::new();
        self.inlines(inlines, stress, None, &mut spans);
        spans
    }

    fn inlines(
        &self,
        inlines: &[Inline],
        stress: Stress,
        link: Option<&SharedString>,
        out: &mut Vec<Span>,
    ) {
        for inline in inlines {
            let span = |face, text: SharedString| Span {
                text,
                face,
                stress,
                link: link.cloned(),
            };
            match &inline.kind {
                InlineKind::Text(text) | InlineKind::Html(text) => {
                    out.push(span(Face::Plain, text.clone().into()));
                }
                InlineKind::Code(text) => out.push(span(Face::Code, text.clone().into())),
                InlineKind::Component(_) => out.push(span(Face::Code, self.source(&inline.range))),
                InlineKind::SoftBreak => out.push(span(Face::Plain, " ".into())),
                InlineKind::HardBreak => out.push(span(Face::Plain, "\n".into())),
                InlineKind::Emphasis(inner) => self.inlines(
                    inner,
                    Stress {
                        italic: true,
                        ..stress
                    },
                    link,
                    out,
                ),
                InlineKind::Strong(inner) => self.inlines(
                    inner,
                    Stress {
                        strong: true,
                        ..stress
                    },
                    link,
                    out,
                ),
                InlineKind::Strikethrough(inner) => self.inlines(
                    inner,
                    Stress {
                        struck: true,
                        ..stress
                    },
                    link,
                    out,
                ),
                InlineKind::Link(to) | InlineKind::Image(to) => {
                    let url = SharedString::from(to.destination.clone());
                    match to.children.is_empty() {
                        true => out.push(Span {
                            link: Some(url.clone()),
                            ..span(Face::Plain, url)
                        }),
                        false => self.inlines(&to.children, stress, Some(&url), out),
                    }
                }
                InlineKind::Autolink { destination, text } => out.push(Span {
                    link: Some(destination.clone().into()),
                    ..span(Face::Plain, text.clone().into())
                }),
            }
        }
    }

    fn code(&self, kind: &CodeKind, text: &str) -> Code {
        let text = text.strip_suffix('\n').unwrap_or(text);
        let language = match kind {
            CodeKind::Fenced { info } => fence_language(info),
            CodeKind::Indented => None,
        };
        Code {
            paints: language
                .filter(|_| self.painted)
                .map(|language| paints(language, text))
                .unwrap_or_default(),
            text: text.to_owned().into(),
        }
    }
}

fn fence_language(info: &str) -> Option<Language> {
    let word = info.split_whitespace().next()?.to_ascii_lowercase();
    match word.as_str() {
        "rust" | "rs" => Some(Language::Rust),
        "go" | "golang" => Some(Language::Go),
        "ts" | "typescript" => Some(Language::TypeScript),
        "tsx" => Some(Language::Tsx),
        "js" | "javascript" | "jsx" | "mjs" => Some(Language::JavaScript),
        "py" | "python" => Some(Language::Python),
        "json" => Some(Language::Json),
        "toml" => Some(Language::Toml),
        "md" | "markdown" => Some(Language::Markdown),
        "sh" | "bash" | "shell" | "zsh" => Some(Language::Bash),
        _ => None,
    }
}

fn paints(language: Language, text: &str) -> Vec<(Range<usize>, Kind)> {
    let syntax = match Syntax::new(language, &Buffer::from_text(text)) {
        Ok(syntax) => syntax,
        Err(error) => {
            eprintln!("desk: chat fence {language:?} drawn without colours: {error}");
            return Vec::new();
        }
    };
    let bytes: Vec<usize> = text
        .char_indices()
        .map(|(at, _)| at)
        .chain([text.len()])
        .collect();
    syntax
        .spans(0..text.split('\n').count())
        .into_iter()
        .filter_map(|span| {
            Some((
                *bytes.get(span.chars.start)?..*bytes.get(span.chars.end)?,
                span.kind,
            ))
        })
        .collect()
}

fn span_text(span: &Span) -> String {
    match span.face {
        Face::Plain => span.text.to_string(),
        Face::Code | Face::Mention => format!("{CODE_PAD}{}{CODE_PAD}", span.text),
    }
}

fn mark_runs(runs: Vec<TextRun>, marks: &[(Range<usize>, HighlightStyle)]) -> Vec<TextRun> {
    let mut marked = Vec::with_capacity(runs.len());
    let mut start = 0;
    for run in runs {
        let end = start + run.len;
        let mut cuts: Vec<usize> = marks
            .iter()
            .flat_map(|(range, _)| [range.start, range.end])
            .filter(|cut| *cut > start && *cut < end)
            .chain([end])
            .collect();
        cuts.sort_unstable();
        cuts.dedup();
        let mut from = start;
        for cut in cuts {
            let mut piece = run.clone();
            piece.len = cut - from;
            if let Some((_, style)) = marks
                .iter()
                .find(|(range, _)| range.start <= from && cut <= range.end)
            {
                piece.color = style.color.unwrap_or(piece.color);
                piece.background_color = style.background_color.or(piece.background_color);
            }
            marked.push(piece);
            from = cut;
        }
        start = end;
    }
    marked
}

fn run(span: &Span, len: usize, base: Rgba, theme: &Theme) -> TextRun {
    let mut face = match span.face {
        Face::Plain => font(theme.word(WordToken::ShapeFont)),
        Face::Code | Face::Mention => font(mono(theme)),
    };
    if span.stress.strong {
        face.weight = FontWeight::SEMIBOLD;
    }
    if span.stress.italic {
        face.style = FontStyle::Italic;
    }
    let (color, fill) = match (span.face, &span.link) {
        (Face::Mention, _) => (rgba(MENTION_TEXT), Some(rgba(MENTION_FILL))),
        (Face::Code, _) => (ink(theme, T1), Some(ink(theme, CODE_FILL))),
        (Face::Plain, Some(_)) => (theme.color(ColorToken::StatusAccent), None),
        (Face::Plain, None) if span.stress.strong => (theme.color(ColorToken::TextStrong), None),
        (Face::Plain, None) => (base, None),
    };
    TextRun {
        len,
        font: face,
        color: rgb_to_hsla(color),
        background_color: fill.map(rgb_to_hsla),
        underline: span.link.as_ref().map(|_| UnderlineStyle {
            thickness: px(TEXT_LINE),
            ..UnderlineStyle::default()
        }),
        strikethrough: span.stress.struck.then(|| StrikethroughStyle {
            thickness: px(TEXT_LINE),
            ..StrikethroughStyle::default()
        }),
        ..TextRun::default()
    }
}

fn code_runs(code: &Code, theme: &Theme) -> Vec<TextRun> {
    let face = font(mono(theme));
    let base = theme.color(ColorToken::TextBase);
    let run = |len, color: Rgba| TextRun {
        len,
        font: face.clone(),
        color: rgb_to_hsla(color),
        ..TextRun::default()
    };
    let (mut runs, mut at) = (Vec::new(), 0);
    for (range, kind) in &code.paints {
        if range.start > at {
            runs.push(run(range.start - at, base));
        }
        let from = range.start.max(at);
        if range.end > from {
            runs.push(run(range.end - from, theme.color(syntax_token(*kind))));
            at = range.end;
        }
    }
    if code.text.len() > at {
        runs.push(run(code.text.len() - at, base));
    }
    runs
}

fn open(url: &str, cx: &mut App) {
    let lower = url.to_ascii_lowercase();
    match LINK_SCHEMES.iter().any(|scheme| lower.starts_with(scheme)) {
        true => {
            eprintln!("desk: chat link opened {url}");
            cx.open_url(url);
        }
        false => eprintln!("desk: chat link {url} not opened, only http, https and mailto open"),
    }
}

pub fn pieces(body: &[Block]) -> Vec<String> {
    let mut found = Vec::new();
    gather(body, &mut found);
    found
}

fn gather(body: &[Block], found: &mut Vec<String>) {
    for block in body {
        match block {
            Block::Para(spans) | Block::Heading(spans) => {
                found.push(spans.iter().map(span_text).collect());
            }
            Block::List { items, .. } => items.iter().for_each(|item| gather(&item.blocks, found)),
            Block::Quote(inner) => gather(inner, found),
            Block::Code(code) => found.push(code.text.to_string()),
            Block::Table { rows, .. } => found.extend(
                rows.iter()
                    .flatten()
                    .map(|cell| cell.iter().map(span_text).collect()),
            ),
            Block::Rule => {}
            Block::Source(text) => found.push(text.to_string()),
        }
    }
}

struct Draw<'a> {
    marks: &'a [Marks],
    theme: &'a Theme,
    piece: usize,
    quoted: bool,
}

impl<'a> Draw<'a> {
    fn next(&mut self) -> (usize, &'a [(Range<usize>, HighlightStyle)]) {
        let at = self.piece;
        self.piece += 1;
        (at, self.marks.get(at).map_or(&[][..], Vec::as_slice))
    }

    fn blocks(&mut self, blocks: &[Block]) -> Vec<Div> {
        blocks
            .iter()
            .enumerate()
            .map(|(at, block)| self.block(block, at == 0))
            .collect()
    }

    fn prose(&mut self, spans: &[Span]) -> AnyElement {
        let (at, marks) = self.next();
        let base = match self.quoted {
            true => ink(self.theme, T2),
            false => self.theme.color(ColorToken::TextBase),
        };
        let pieces: Vec<String> = spans.iter().map(span_text).collect();
        let runs = spans
            .iter()
            .zip(&pieces)
            .map(|(span, piece)| run(span, piece.len(), base, self.theme))
            .collect();
        let text = StyledText::new(pieces.concat()).with_runs(mark_runs(runs, marks));
        let (mut ranges, mut urls, mut start) = (Vec::new(), Vec::new(), 0);
        for (span, piece) in spans.iter().zip(&pieces) {
            if let Some(url) = &span.link {
                ranges.push(start..start + piece.len());
                urls.push(url.clone());
            }
            start += piece.len();
        }
        match ranges.is_empty() {
            true => text.into_any_element(),
            false => InteractiveText::new(("chat-text", at), text)
                .on_click(ranges, move |picked, _, cx| {
                    if let Some(url) = urls.get(picked) {
                        open(url, cx);
                    }
                })
                .into_any_element(),
        }
    }

    fn marker(&self, number: Option<u64>, task: Option<bool>) -> Div {
        let theme = self.theme;
        let mark = div()
            .flex_none()
            .w(px(BULLET_INDENT))
            .h(px(CHAT_LINE))
            .flex()
            .items_center();
        match (task, number) {
            (Some(true), _) => mark.child(glyph(
                Glyph::Check,
                ICON_SMALL,
                theme.color(ColorToken::StatusLive),
            )),
            (Some(false), _) => mark.child(
                div()
                    .size(px(TASK_BOX))
                    .rounded(px(TASK_RADIUS))
                    .shadow(vec![ring(ink(theme, T3))]),
            ),
            (None, Some(number)) => mark.child(format!("{number}.")),
            (None, None) => mark.child("•"),
        }
    }

    fn block(&mut self, block: &Block, first: bool) -> Div {
        let theme = self.theme;
        let gap = match block {
            Block::Heading(_) => HEADING_GAP,
            Block::List { .. } => BULLETS_GAP,
            Block::Para(_)
            | Block::Quote(_)
            | Block::Code(_)
            | Block::Table { .. }
            | Block::Rule
            | Block::Source(_) => PARA_GAP,
        };
        let drawn = match block {
            Block::Para(spans) | Block::Heading(spans) => div().child(self.prose(spans)),
            Block::List { start, items } => {
                let mut number = start.map(u64::from);
                div().flex().flex_col().children(items.iter().map(|item| {
                    let shown = number;
                    number = number.map(|at| at.saturating_add(1));
                    div().flex().child(self.marker(shown, item.task)).child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .flex()
                            .flex_col()
                            .children(self.blocks(&item.blocks)),
                    )
                }))
            }
            Block::Quote(inner) => {
                let was = std::mem::replace(&mut self.quoted, true);
                let drawn = self.blocks(inner);
                self.quoted = was;
                div()
                    .pl(px(QUOTE_PAD))
                    .border_l_2()
                    .border_color(ink(theme, RULE_INK))
                    .flex()
                    .flex_col()
                    .children(drawn)
            }
            Block::Code(code) => {
                let (_, marks) = self.next();
                boxed(theme).bg(ink(theme, CODE_FILL)).child(
                    StyledText::new(code.text.clone())
                        .with_runs(mark_runs(code_runs(code, theme), marks)),
                )
            }
            Block::Table { alignments, rows } => div()
                .flex()
                .flex_col()
                .rounded(px(ROW_RADIUS))
                .border_1()
                .border_color(ink(theme, RULE_INK))
                .overflow_hidden()
                .children(rows.iter().enumerate().map(|(at, row)| {
                    div()
                        .flex()
                        .when(at == 0, |head| head.bg(ink(theme, CODE_FILL)))
                        .when(at > 0, |line| {
                            line.border_t_1().border_color(ink(theme, RULE_INK))
                        })
                        .children(row.iter().enumerate().map(|(column, cell)| {
                            let cell_box = div()
                                .flex_1()
                                .min_w_0()
                                .px(px(CELL_PAD_X))
                                .py(px(CELL_PAD_Y))
                                .child(self.prose(cell));
                            match alignments.get(column) {
                                Some(Alignment::Center) => cell_box.text_center(),
                                Some(Alignment::Right) => cell_box.text_right(),
                                Some(Alignment::Left | Alignment::None) | None => cell_box,
                            }
                        }))
                })),
            Block::Rule => div().h(px(RULE_HEIGHT)).bg(ink(theme, RULE_INK)),
            Block::Source(text) => {
                let (_, marks) = self.next();
                boxed(theme)
                    .shadow(vec![ring(ink(theme, QUEUED_RING))])
                    .text_color(ink(theme, T3))
                    .child(marked(text.clone(), marks))
            }
        };
        drawn.when(!first, |drawn| drawn.mt(px(gap)))
    }
}

fn boxed(theme: &Theme) -> Div {
    div()
        .min_w_0()
        .rounded(px(ROW_RADIUS))
        .px(px(CALLS_PAD_X))
        .py(px(CALLS_PAD_Y))
        .font_family(mono(theme))
        .text_size(px(FONT_TAB))
        .line_height(px(CALL_LINE))
}

pub fn lead(
    id: impl Into<ElementId>,
    time: impl Into<SharedString>,
    body: &[Block],
    marks: &[Marks],
    theme: &Theme,
) -> Div {
    let mut draw = Draw {
        marks,
        theme,
        piece: 0,
        quoted: false,
    };
    div()
        .mt(px(LEAD_GAP))
        .min_w_0()
        .text_size(px(FONT_CHAT))
        .line_height(px(CHAT_LINE))
        .text_color(theme.color(ColorToken::TextBase))
        .child(who(agent("lead", theme), time, false, theme))
        .child(div().id(id).flex().flex_col().children(draw.blocks(body)))
}

fn row(theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(ROW_ITEMS))
        .mt(px(TOOL_GAP))
        .min_w_0()
        .text_size(px(FONT_BODY))
        .line_height(px(ROW_LINE))
        .text_color(ink(theme, SHELL_TEXT))
}

fn chevron(opened: f32, theme: &Theme) -> impl IntoElement {
    glyph(Glyph::Chevron, ICON_SMALL, ink(theme, CAPTION_TEXT)).with_transformation(
        Transformation::rotate(radians(std::f32::consts::FRAC_PI_2 * opened)),
    )
}

pub fn tools(
    id: impl Into<ElementId>,
    count: impl Into<SharedString>,
    summary: impl Into<SharedString>,
    opened: f32,
    theme: &Theme,
) -> Stateful<Div> {
    row(theme)
        .id(id)
        .cursor_pointer()
        .hover(|style| style.text_color(ink(theme, HOVER_TEXT)))
        .child(chevron(opened, theme))
        .child(div().flex_none().child(count.into()))
        .child(line(summary).text_color(ink(theme, T3)))
}

fn tray() -> Div {
    div()
        .ml(px(CALLS_INDENT))
        .mt(px(NOTE_GAP))
        .px(px(CALLS_PAD_X))
        .py(px(CALLS_PAD_Y))
        .rounded(px(ROW_RADIUS))
        .bg(tint(rgba(0x000000ff), CALLS_SHADE))
        .flex()
        .flex_col()
        .gap(px(CALLS_GAP))
        .min_w_0()
}

pub fn calls(rows: &[Call], footnote: Option<SharedString>, theme: &Theme) -> Div {
    tray()
        .text_size(px(FONT_TAB))
        .line_height(px(CALL_LINE))
        .text_color(theme.color(ColorToken::TextBase))
        .children(rows.iter().map(|call| {
            div()
                .flex()
                .items_center()
                .gap(px(ROW_ITEMS))
                .min_w_0()
                .child(dim(call.tool.clone(), theme).w(px(CALL_TOOL)))
                .child(line(call.arg.clone()).flex_1().font_family(mono(theme)))
                .child(dim(call.out.clone(), theme).text_size(px(FONT_SMALL)))
        }))
        .children(footnote.map(|text| {
            div()
                .pt(px(CALL_NOTE_TOP))
                .pl(px(CALL_NOTE_LEFT))
                .text_size(px(FONT_SMALL))
                .line_height(px(CHAT_LINE))
                .text_color(ink(theme, T3))
                .child(text)
        }))
}

pub fn folding(natural: Rc<Cell<f32>>, opened: f32, content: Div) -> Div {
    div()
        .h(px(natural.get() * opened))
        .overflow_hidden()
        .on_children_prepainted(move |bounds, _, _| {
            if let Some(inner) = bounds.first() {
                natural.set(f32::from(inner.size.height));
            }
        })
        .child(content.opacity(opened))
}

fn agent_mark(id: impl Into<ElementId>, mark: AgentMark, theme: &Theme) -> AnyElement {
    match mark {
        AgentMark::Done => glyph(
            Glyph::Check,
            ICON_SMALL,
            theme.color(ColorToken::StatusLive),
        )
        .into_any_element(),
        AgentMark::Working => spinner(id, theme).into_any_element(),
        AgentMark::Asking => div()
            .flex_none()
            .size(px(MARK))
            .flex()
            .items_center()
            .justify_center()
            .child(
                div()
                    .size(px(WAIT_DOT))
                    .rounded_full()
                    .bg(theme.color(ColorToken::StatusWarn)),
            )
            .into_any_element(),
        AgentMark::Failed => div()
            .flex_none()
            .w(px(MARK))
            .flex()
            .justify_center()
            .font_family(mono(theme))
            .text_color(theme.color(ColorToken::StatusDanger))
            .child("!")
            .into_any_element(),
    }
}

pub fn agent_row(id: impl Into<ElementId>, line_of: &Agent, theme: &Theme) -> Div {
    row(theme)
        .text_color(ink(theme, HOVER_TEXT))
        .child(agent_mark(id, line_of.mark, theme))
        .child(agent(line_of.name.clone(), theme))
        .child(line(line_of.summary.clone()).text_color(ink(theme, T3)))
        .child(div().flex_1())
        .children(
            line_of
                .link
                .clone()
                .map(|link| dim(link, theme).text_size(px(FONT_SMALL))),
        )
}

pub fn cron_row(
    job: impl Into<SharedString>,
    schedule: impl Into<SharedString>,
    prompt: impl Into<SharedString>,
    theme: &Theme,
) -> Div {
    row(theme)
        .text_color(ink(theme, HOVER_TEXT))
        .child(
            div()
                .flex_none()
                .w(px(MARK))
                .flex()
                .justify_center()
                .child(glyph(Glyph::Cron, ICON_SMALL, ink(theme, T3))),
        )
        .child(agent(job, theme))
        .child(dim(schedule, theme))
        .child(line(prompt).text_color(ink(theme, T3)))
}

pub fn agents(
    id: impl Into<ElementId> + Clone,
    count: impl Into<SharedString>,
    summary: impl Into<SharedString>,
    opened: f32,
    theme: &Theme,
) -> Stateful<Div> {
    row(theme)
        .id(id.clone())
        .cursor_pointer()
        .text_color(ink(theme, HOVER_TEXT))
        .child(spinner(id, theme))
        .child(div().flex_none().child(count.into()))
        .child(line(summary).text_color(ink(theme, T3)))
        .child(div().flex_1())
        .child(chevron(opened, theme))
}

pub fn agent_list(tag: impl Into<SharedString>, lines: &[Agent], theme: &Theme) -> Div {
    let tag = tag.into();
    tray().children(
        lines
            .iter()
            .enumerate()
            .map(|(at, agent)| agent_row((tag.clone(), at), agent, theme).mt_0()),
    )
}

pub fn command(
    id: impl Into<ElementId>,
    busy: bool,
    text: impl Into<SharedString>,
    verdict: Option<Verdict>,
    tail: Option<SharedString>,
    theme: &Theme,
) -> Div {
    let mark = if busy {
        spinner(id, theme).into_any_element()
    } else {
        div()
            .flex_none()
            .w(px(MARK))
            .flex()
            .justify_center()
            .font_family(mono(theme))
            .text_color(ink(theme, T3))
            .child("⟩")
            .into_any_element()
    };
    let verdict = verdict.map(|verdict| {
        let (said, color) = match verdict {
            Verdict::Exit(code) => (code, theme.color(ColorToken::StatusDanger)),
            Verdict::Ask => ("ask".into(), theme.color(ColorToken::StatusWarn)),
        };
        div().flex_none().text_color(color).child(said)
    });
    row(theme)
        .child(mark)
        .child(
            line(text)
                .font_family(mono(theme))
                .text_size(px(FONT_TAB))
                .text_color(theme.color(ColorToken::TextBase)),
        )
        .children(verdict.map(|said| said.text_size(px(FONT_TAB))))
        .children(tail.map(|tail| dim(tail, theme).text_size(px(FONT_TAB))))
}

pub fn fail(
    text: impl Into<SharedString>,
    detail: impl Into<SharedString>,
    link: impl Into<SharedString>,
    theme: &Theme,
) -> Div {
    let (text, detail) = (text.into(), detail.into());
    let danger = theme.color(ColorToken::GitDeleted);
    div()
        .flex()
        .items_center()
        .gap(px(ROW_ITEMS))
        .mt(px(NOTE_GAP))
        .min_w_0()
        .px(px(FAIL_PAD_X))
        .py(px(FAIL_PAD_Y))
        .rounded(px(ROW_RADIUS))
        .bg(tint(danger, FAIL_TINT))
        .shadow(vec![ring(tint(danger, FAIL_RING))])
        .text_size(px(FONT_BODY))
        .line_height(px(ROW_LINE))
        .text_color(theme.color(ColorToken::TextBase))
        .child(
            div()
                .flex_none()
                .w(px(MARK))
                .flex()
                .justify_center()
                .font_family(mono(theme))
                .text_color(theme.color(ColorToken::StatusDanger))
                .child("!"),
        )
        .child(div().flex_1().min_w_0().child(
            StyledText::new(format!("{text}: {detail}")).with_highlights([(
                text.len() + 2..text.len() + 2 + detail.len(),
                HighlightStyle::from(ink(theme, T3)),
            )]),
        ))
        .child(dim(link, theme).text_size(px(FONT_SMALL)))
}

pub fn note(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .mt(px(NOTE_GAP))
        .min_w_0()
        .text_size(px(FONT_BODY))
        .line_height(px(ROW_LINE))
        .text_color(ink(theme, CAPTION_TEXT))
        .child(text.into())
}

pub fn foot(text: impl Into<SharedString>, theme: &Theme) -> Div {
    line(text)
        .mt(px(FOOT_GAP))
        .text_size(px(FONT_WHO))
        .text_color(ink(theme, FOOT_TEXT))
}
