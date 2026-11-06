use std::cell::{Cell, RefCell};
use std::collections::HashMap;
use std::ops::Range;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::sync::Arc;
use std::time::{Duration, Instant};

use desk_core::buffer::Buffer;
use desk_core::syntax::{Kind, Language, Syntax};
use desk_markdown::{
    self as md, Alignment, BlockKind, CodeKind, Component, Inline, InlineKind, ListKind, Options,
    Registry,
};
use desk_motion::tokens::{PANEL_IN_MS, PANEL_OUT_MS};
use desk_motion::{Glide, GlideKind, Phase, Presence, reduced_motion, spin};
use gpui::{
    AnimationExt, AnyElement, App, Axis, Bounds, ClipboardItem, Corners, DispatchPhase, Div,
    ElementId, Entity, FontStyle, FontWeight, HighlightStyle, InteractiveText, MouseMoveEvent,
    Pixels, Point, Rgba, ScrollHandle, SharedString, Stateful, StrikethroughStyle, StyledText,
    TextLayout, TextRun, Transformation, TransformationMatrix, UnderlineStyle, Window, canvas, div,
    font, point, prelude::*, px, radians, relative, rgb_to_hsla, rgba, size,
};

use crate::components::avatar::{AgentKind, spinner};
use crate::components::button::{ButtonKind, button};
use crate::components::chip::{self, code as token_chip, file_chip, mono};
use crate::components::code_editor::syntax_token;
use crate::components::find::{find_ranges, match_highlights};
use crate::components::flow::FlowBlock;
use crate::components::glyph::Glyph;
use crate::components::hovercard::{Subject, confirmed, hover, link_card, link_key};
use crate::components::math::{self, Math};
use crate::components::media_view::{self, OpenMedia};
use crate::components::paint::{glyph, ink, ring, tint};
use crate::components::rich::{self, Rich};
use crate::components::scroll::scrollbar;
use crate::components::select::{Piece, selectable};
use crate::components::size::{
    CALLS_GAP, CALLS_INDENT, CALLS_PAD_X, CALLS_PAD_Y, CAPTION_TEXT, CODE_FILL, FAIL_RING,
    FAIL_TINT, FCHIP, FIELD, FONT_BODY, FONT_CHAT, FONT_SMALL, FONT_TAB, FONT_WHO, FOOT_GAP,
    FOOT_TEXT, GRID_GAP, HOVER, HOVER_TEXT, LEAD_GAP, MENU_PAD, NOTE_GAP, RADIUS_BUBBLE,
    RADIUS_CHIP_SMALL, SELECTION_FILL, SHELL_TEXT, T1, T2, T3, TOOL_GAP, YOU_GAP, YOU_MAX,
    YOU_PAD_BOTTOM, YOU_PAD_TOP, YOU_PAD_X,
};
use crate::components::tooltip::{Edge, hover_card};
use crate::live::ActiveTheme;
use crate::metrics::{HAIRLINE, ICON_SMALL, ICON_TINY};
use crate::theme::{ColorToken, Theme, WordToken};

pub const CHAT_LINE: f32 = 23.0;
const ROW_LINE: f32 = 20.0;
const FORK_RULE: f32 = 0.07;
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
const ITEM_GAP: f32 = 4.0;
const BULLET_INDENT: f32 = 20.0;
const CODE_PAD: &str = "\u{202f}\u{202f}";
const GLYPH_SLOT: &str = "\u{a0}\u{a0}";
const LINK_SLOT: &str = "\u{202f}\u{a0}\u{a0}";
const IMAGE_OPEN: &str = "[Image #";
const INLINE_GLYPH: f32 = 11.0;
const PILL_INSET: f32 = 2.5;
const PATH_FILL: f32 = 0.14;
const CELL_CHAR: f32 = 7.5;
const CELL_MIN: f32 = 72.0;
const CELL_MAX: f32 = 320.0;
const QUOTE_PAD: f32 = 12.0;
const RULE_INK: f32 = 0.12;
const RULE_HEIGHT: f32 = 1.0;
const TEXT_LINE: f32 = 1.0;
const CELL_PAD_X: f32 = 10.0;
const CELL_PAD_Y: f32 = 4.0;
const RAIL_THICK: f32 = 3.0;
const RAIL_INSET: f32 = 3.0;
const RAIL_THUMB_MIN: f32 = 24.0;
const TASK_BOX: f32 = 11.0;
const TASK_RADIUS: f32 = 3.0;
const LINK_SCHEMES: [&str; 3] = ["https://", "http://", "mailto:"];
const MATH_SLOT: char = '\u{a0}';
const MONO_ADVANCE_EM: f32 = 0.6;
const MATH_AXIS_EM: f32 = 0.3;
const H1_SCALE: f32 = 1.5;
const H2_SCALE: f32 = 1.25;
const H3_SCALE: f32 = 1.1;
const FLOW_ZOOM: f32 = 0.8;
const FLOW_PAD: f32 = 16.0;
const FLOW_FENCE: &str = "flow";
const CARD_GROUP: &str = "chat-tool-card";
const CARD_SPOT: &str = "chat-card-spot";
const MENTION: &str = "Mention in chat";
const OBJECT_MAX: f32 = 240.0;
const CARD_PAD: f32 = 6.0;
const STACK_GAP: f32 = 3.0;
const STACK_PAD: f32 = 5.0;
const STACK_INSET: f32 = 2.0;
const CODE_BLOCK_PAD: f32 = 8.0;
const SECTION_LINES: usize = 12;
const SHIMMER_BAND: f32 = 0.3;
const SHIMMER_PERIOD: Duration = Duration::from_millis(1400);
const STRONG: Stress = Stress {
    strong: true,
    italic: false,
    struck: false,
};

pub type Marks = Vec<(Range<usize>, HighlightStyle)>;
pub type OnLink = Rc<dyn Fn(&str, &mut Window, &mut App)>;

enum Click {
    Url(SharedString),
    Media(Arc<Path>),
}

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
    Path,
    LinkTail,
    Mention,
    Math,
    Image(u32),
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
    pub math: Option<Rc<Math>>,
}

impl Span {
    pub fn new(face: Face, text: impl Into<SharedString>) -> Self {
        Self {
            text: text.into(),
            face,
            stress: Stress::default(),
            link: None,
            math: None,
        }
    }
}

#[derive(Clone)]
pub struct ListItem {
    pub task: Option<bool>,
    pub blocks: Vec<Block>,
}

#[derive(Clone)]
pub struct Code {
    pub text: SharedString,
    pub label: Option<SharedString>,
    pub paints: Vec<(Range<usize>, Kind)>,
}

#[derive(Clone)]
pub enum Block {
    Para(Vec<Span>),
    Heading {
        level: u8,
        spans: Vec<Span>,
    },
    List {
        start: Option<u32>,
        items: Vec<ListItem>,
    },
    Quote(Vec<Block>),
    Code(Code),
    Table {
        alignments: Vec<Alignment>,
        rows: Vec<Vec<Vec<Span>>>,
        scroll: ScrollHandle,
    },
    Rule,
    Source {
        text: SharedString,
        refused: Option<SharedString>,
    },
    Component(Rich, Vec<Block>),
    Math(Rc<Math>),
    Flow(Rc<FlowBlock>),
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
    pub tone: Option<Rgba>,
}

const NAME_HASH_START: u64 = 0xcbf2_9ce4_8422_2325;
const NAME_HASH_STEP: u64 = 0x0100_0000_01b3;

pub fn name_tone(name: &str, theme: &Theme) -> Rgba {
    let hash = name.bytes().fold(NAME_HASH_START, |hash, byte| {
        (hash ^ u64::from(byte)).wrapping_mul(NAME_HASH_STEP)
    });
    let palette = AgentKind::ALL;
    let at = usize::try_from(hash % palette.len() as u64).unwrap_or_default();
    palette
        .get(at)
        .copied()
        .unwrap_or(AgentKind::GoDev)
        .color(theme)
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

fn agent(name: impl Into<SharedString>, tone: Option<Rgba>, theme: &Theme) -> Div {
    div()
        .flex_none()
        .px(px(PILL_PAD_X))
        .py(px(PILL_PAD_Y))
        .rounded(px(RADIUS_CHIP_SMALL))
        .bg(tint(
            tone.unwrap_or(theme.color(ColorToken::Trace)),
            AGENT_FILL,
        ))
        .text_color(tone.unwrap_or(rgba(AGENT_TEXT)))
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

pub struct Said<'a> {
    pub text: SharedString,
    pub images: &'a [(u32, PathBuf)],
}

impl From<SharedString> for Said<'_> {
    fn from(text: SharedString) -> Self {
        Said { text, images: &[] }
    }
}

impl From<&'static str> for Said<'_> {
    fn from(text: &'static str) -> Self {
        SharedString::from(text).into()
    }
}

impl From<String> for Said<'_> {
    fn from(text: String) -> Self {
        SharedString::from(text).into()
    }
}

pub fn you<'a>(
    time: impl Into<SharedString>,
    said: impl Into<Said<'a>>,
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
                .child({
                    let said = said.into();
                    spoken(said.text, said.images, marks, theme)
                }),
        )
}

fn image_split(text: &str) -> Vec<(Face, SharedString)> {
    let mut found = Vec::new();
    let mut rest = text;
    while let Some(at) = rest.find(IMAGE_OPEN) {
        let (before, from) = rest.split_at(at);
        let after = from.get(IMAGE_OPEN.len()..).unwrap_or_default();
        let picked = after
            .split_once(']')
            .filter(|(n, _)| !n.is_empty() && n.bytes().all(|byte| byte.is_ascii_digit()))
            .and_then(|(n, tail)| Some((n.parse::<u32>().ok()?, tail)));
        let Some((n, tail)) = picked else {
            found.push((Face::Plain, format!("{before}{IMAGE_OPEN}").into()));
            rest = after;
            continue;
        };
        if !before.is_empty() {
            found.push((Face::Plain, before.to_owned().into()));
        }
        found.push((Face::Image(n), format!("Image {n}").into()));
        rest = tail;
    }
    if !rest.is_empty() {
        found.push((Face::Plain, rest.to_owned().into()));
    }
    found
}

fn moved_marks(
    spans: &[Span],
    pieces: &[String],
    marks: &[(Range<usize>, HighlightStyle)],
) -> Vec<(Range<usize>, HighlightStyle)> {
    let (mut from, mut to, mut edges) = (0, 0, Vec::new());
    for (span, piece) in spans.iter().zip(pieces) {
        let len = match span.face {
            Face::Image(n) => format!("{IMAGE_OPEN}{n}]").len(),
            _ => span.text.len(),
        };
        edges.push((from..from + len, to, span.face == Face::Plain));
        (from, to) = (from + len, to + piece.len());
    }
    let place = |at: usize| {
        edges
            .iter()
            .find(|(source, _, _)| source.contains(&at))
            .map_or(to, |(source, shown, plain)| match plain {
                true => shown + (at - source.start),
                false => *shown,
            })
    };
    marks
        .iter()
        .map(|(range, style)| {
            let start = place(range.start);
            (start..place(range.end).max(start), *style)
        })
        .collect()
}

fn targeted(
    index: usize,
    targets: &[(Range<usize>, Subject)],
    placed: &TextLayout,
) -> Option<(Subject, Bounds<gpui::Pixels>)> {
    let (range, subject) = targets.iter().find(|(range, _)| range.contains(&index))?;
    let start = placed.position_for_index(range.start)?;
    let end = placed.position_for_index(range.end)?;
    let whole = placed.bounds();
    let (left, right) = match start.y == end.y {
        true => (start.x, end.x),
        false => (whole.left(), whole.right()),
    };
    let line = Bounds::from_corners(
        point(left, start.y),
        point(right, end.y + placed.line_height()),
    );
    Some((subject.clone(), line))
}

fn spoken(
    text: SharedString,
    images: &[(u32, PathBuf)],
    marks: &[(Range<usize>, HighlightStyle)],
    theme: &Theme,
) -> AnyElement {
    let split = image_split(&text);
    if !split.iter().any(|(face, _)| matches!(face, Face::Image(_))) {
        return marked(text, marks);
    }
    let spans: Vec<Span> = split
        .into_iter()
        .map(|(face, piece)| Span::new(face, piece))
        .collect();
    let pieces: Vec<String> = spans.iter().map(span_text).collect();
    let base = theme.color(ColorToken::TextBase);
    let runs = spans
        .iter()
        .zip(&pieces)
        .map(|(span, piece)| run(span, piece.len(), base, theme))
        .collect();
    let key = link_key(&text, 0);
    let shown = StyledText::new(pieces.concat())
        .with_runs(mark_runs(runs, &moved_marks(&spans, &pieces, marks)));
    let (layout, placed) = (shown.layout().clone(), shown.layout().clone());
    let decor = decor(&spans, &pieces, base, theme);
    let (mut targets, mut opens, mut start) = (Vec::new(), Vec::new(), 0);
    for (span, piece) in spans.iter().zip(&pieces) {
        if let Face::Image(n) = span.face {
            let path: Option<Arc<Path>> = images
                .iter()
                .find(|(at, _)| *at == n)
                .map(|(_, path)| Arc::from(path.as_path()));
            let range = start..start + piece.len();
            if let Some(path) = &path {
                opens.push((range.clone(), path.clone()));
            }
            targets.push((range, Subject::Image(n, path)));
        }
        start += piece.len();
    }
    let ranges = opens.iter().map(|(range, _)| range.clone()).collect();
    div()
        .relative()
        .child(
            canvas(
                |_, _, _| {},
                move |_, (), window, cx| paint_decor(&layout, &decor, window, cx),
            )
            .absolute()
            .size_full(),
        )
        .child(
            InteractiveText::new(("you-text", key), shown)
                .on_hover(move |index, _, window, cx| {
                    let hovered = index.and_then(|index| targeted(index, &targets, &placed));
                    hover(key, hovered, window, cx);
                })
                .on_click(ranges, move |at, window, cx| {
                    if let Some((_, path)) = opens.get(at) {
                        window.dispatch_action(Box::new(OpenMedia { path: path.clone() }), cx);
                    }
                }),
        )
        .child(link_card(key))
        .into_any_element()
}

pub fn queued(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div().flex().justify_end().mt(px(QUEUED_GAP)).child(
        bubble(theme)
            .shadow(vec![ring(ink(theme, QUEUED_RING))])
            .child(who("You", "queued", false, theme))
            .child(text.into()),
    )
}

type Laid = RefCell<[HashMap<String, Option<Rc<Math>>>; 2]>;

pub fn markdown(source: &str) -> Vec<Block> {
    Reader {
        source,
        painted: true,
        registry: &rich::registry(),
        laid: &Laid::default(),
    }
    .blocks(&md::parse(source, Options::ALL).blocks)
}

pub struct Streaming {
    stream: md::Stream,
    text: String,
    registry: Registry,
    stable: Vec<Vec<Block>>,
    laid: Laid,
    deltas: u32,
    spent: Duration,
    slowest: Duration,
    holds: u32,
    held: Option<(usize, Instant)>,
}

impl Default for Streaming {
    fn default() -> Self {
        Self {
            stream: md::Stream::new(Options::ALL),
            text: String::new(),
            registry: rich::registry(),
            stable: Vec::new(),
            laid: Laid::default(),
            deltas: 0,
            spent: Duration::ZERO,
            slowest: Duration::ZERO,
            holds: 0,
            held: None,
        }
    }
}

impl Drop for Streaming {
    fn drop(&mut self) {
        if self.deltas == 0 {
            return;
        }
        eprintln!(
            "desk: chat stream {} bytes, {} deltas, {:.1} us mean, {} us max per delta, {} holds, ended held {:?}",
            self.text.len(),
            self.deltas,
            self.spent.as_secs_f64() * 1e6 / f64::from(self.deltas),
            self.slowest.as_micros(),
            self.holds,
            self.held.map(|(at, _)| at)
        );
    }
}

impl Streaming {
    pub fn blocks(&mut self, text: &str) -> Vec<Block> {
        let started = Instant::now();
        let out = self.read(text);
        self.watch_hold();
        let took = started.elapsed();
        self.deltas = self.deltas.saturating_add(1);
        self.spent = self.spent.saturating_add(took);
        self.slowest = self.slowest.max(took);
        out
    }

    fn watch_hold(&mut self) {
        let now = self.stream.held();
        if self.held.map(|(at, _)| at) == now {
            return;
        }
        if let Some((at, since)) = self.held {
            eprintln!(
                "desk: chat stream released byte {at} after {} us: {:?}",
                since.elapsed().as_micros(),
                self.text
                    .get(at..)
                    .unwrap_or_default()
                    .chars()
                    .take(24)
                    .collect::<String>()
            );
        }
        if let Some(at) = now {
            eprintln!("desk: chat stream holds from byte {at}");
            self.holds = self.holds.saturating_add(1);
        }
        self.held = now.map(|at| (at, Instant::now()));
    }

    fn read(&mut self, text: &str) -> Vec<Block> {
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
        let (document, changed) = self.stream.snapshot();
        let reader = Reader {
            source: md::visible_prefix(&self.text),
            painted: false,
            registry: &self.registry,
            laid: &self.laid,
        };
        self.stable.truncate(changed.start);
        let settled = document.blocks.get(changed.clone()).unwrap_or_default();
        self.stable.extend(
            settled
                .iter()
                .map(|block| reader.blocks(std::slice::from_ref(block))),
        );
        let tail = reader.blocks(document.blocks.get(changed.end..).unwrap_or_default());
        let mut out =
            Vec::with_capacity(self.stable.iter().map(Vec::len).sum::<usize>() + tail.len());
        for blocks in &self.stable {
            out.extend_from_slice(blocks);
        }
        out.extend(tail);
        out
    }
}

struct Reader<'a> {
    source: &'a str,
    painted: bool,
    registry: &'a Registry,
    laid: &'a Laid,
}

fn kept(inlines: &[Inline]) -> Option<&[Inline]> {
    let edge = |i: &Inline| match &i.kind {
        InlineKind::SoftBreak | InlineKind::HardBreak => true,
        InlineKind::Text(text) => text.trim().is_empty(),
        _ => false,
    };
    let start = inlines.iter().position(|i| !edge(i))?;
    let end = inlines.iter().rposition(|i| !edge(i))?;
    inlines.get(start..=end)
}

impl Reader<'_> {
    fn blocks(&self, blocks: &[md::Block]) -> Vec<Block> {
        blocks
            .iter()
            .flat_map(|block| match &block.kind {
                BlockKind::Paragraph(inlines) => match kept(inlines) {
                    Some(
                        [
                            Inline {
                                kind: InlineKind::Component(component),
                                range,
                            },
                        ],
                    ) => vec![self.component(component, range, || {
                        self.para(&component.children).into_iter().collect()
                    })],
                    _ => self.paragraph(inlines),
                },
                _ => vec![self.block(block)],
            })
            .collect()
    }

    fn component<C>(
        &self,
        component: &Component<C>,
        range: &Range<usize>,
        children: impl FnOnce() -> Vec<Block>,
    ) -> Block {
        let read = rich::read(component, self.registry);
        if self.painted {
            match &read {
                Ok(_) => eprintln!("desk: component {} ok", component.name),
                Err(why) => eprintln!("desk: component {} refused {why}", component.name),
            }
        }
        match read {
            Ok(rich) => Block::Component(rich, children()),
            Err(why) => Block::Source {
                text: self.source(range),
                refused: Some(why.to_string().into()),
            },
        }
    }

    fn paragraph(&self, inlines: &[Inline]) -> Vec<Block> {
        let mut out = Vec::new();
        let mut rest = inlines;
        while let Some(at) = rest
            .iter()
            .position(|i| matches!(i.kind, InlineKind::Math { display: true, .. }))
        {
            let (before, from) = rest.split_at(at);
            let Some((math, after)) = from.split_first() else {
                break;
            };
            out.extend(self.para(before));
            if let InlineKind::Math { tex, .. } = &math.kind {
                out.push(self.display(tex, &math.range));
            }
            rest = after;
        }
        out.extend(self.para(rest));
        out
    }

    fn para(&self, inlines: &[Inline]) -> Option<Block> {
        Some(Block::Para(self.spans(kept(inlines)?, Stress::default())))
    }

    fn display(&self, tex: &str, range: &Range<usize>) -> Block {
        match self.math(tex, true) {
            Some(math) => Block::Math(math),
            None => Block::Code(Code {
                text: self.source(range),
                label: None,
                paints: Vec::new(),
            }),
        }
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
            BlockKind::Heading { level, inlines } => Block::Heading {
                level: *level,
                spans: self.spans(inlines, STRONG),
            },
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
            BlockKind::Code { kind, text } => self
                .flow(kind, text)
                .unwrap_or_else(|| Block::Code(self.code(kind, text))),
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
                scroll: ScrollHandle::new(),
            },
            BlockKind::Math(tex) => self.display(tex, &block.range),
            BlockKind::Html(_) => Block::Source {
                text: self.source(&block.range),
                refused: None,
            },
            BlockKind::Component(component) => {
                self.component(component, &block.range, || self.blocks(&component.children))
            }
        }
    }

    fn math(&self, tex: &str, display: bool) -> Option<Rc<Math>> {
        let [inline, block] = &mut *self.laid.borrow_mut();
        let seen = match display {
            true => block,
            false => inline,
        };
        if let Some(math) = seen.get(tex) {
            return math.clone();
        }
        let math = math::layout(tex, display);
        seen.insert(tex.to_owned(), math.clone());
        math
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
                math: None,
            };
            match &inline.kind {
                InlineKind::Text(text) | InlineKind::Html(text) => out.extend(
                    image_split(text)
                        .into_iter()
                        .map(|(face, text)| span(face, text)),
                ),
                InlineKind::Code(text) => out.push(span(
                    match looks_like_path(text) {
                        true => Face::Path,
                        false => Face::Code,
                    },
                    text.clone().into(),
                )),
                InlineKind::Math { tex, display } => out.push(match self.math(tex, *display) {
                    Some(math) => Span {
                        math: Some(math),
                        ..span(Face::Math, tex.clone().into())
                    },
                    None => span(Face::Code, self.source(&inline.range)),
                }),
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
                            ..span(Face::Plain, url.clone())
                        }),
                        false => self.inlines(&to.children, stress, Some(&url), out),
                    }
                    out.push(Span {
                        link: Some(url),
                        ..span(Face::LinkTail, SharedString::default())
                    });
                }
                InlineKind::Autolink { destination, text } => {
                    let url = SharedString::from(destination.clone());
                    out.push(Span {
                        link: Some(url.clone()),
                        ..span(Face::Plain, text.clone().into())
                    });
                    out.push(Span {
                        link: Some(url),
                        ..span(Face::LinkTail, SharedString::default())
                    });
                }
            }
        }
    }

    fn flow(&self, kind: &CodeKind, text: &str) -> Option<Block> {
        let CodeKind::Fenced { info } = kind else {
            return None;
        };
        if info.split_whitespace().next() != Some(FLOW_FENCE) {
            return None;
        }
        match FlowBlock::read(text) {
            Ok(drawn) => Some(Block::Flow(Rc::new(drawn))),
            Err(why) => {
                if self.painted {
                    eprintln!("desk: chat flow block stays code: {why}");
                }
                None
            }
        }
    }

    fn code(&self, kind: &CodeKind, text: &str) -> Code {
        let text = expand_tabs(text.strip_suffix('\n').unwrap_or(text));
        let text = text.as_str();
        let (language, label) = match kind {
            CodeKind::Fenced { info } => (fence_language(info), fence_label(info)),
            CodeKind::Indented => (None, None),
        };
        Code {
            label,
            paints: language
                .filter(|_| self.painted)
                .map(|language| paints(language, text))
                .unwrap_or_default(),
            text: text.to_owned().into(),
        }
    }
}

fn expand_tabs(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    let mut column = 0;
    for c in text.chars() {
        match c {
            '\t' => {
                let width = TAB_COLUMNS - column % TAB_COLUMNS;
                out.extend(std::iter::repeat_n(' ', width));
                column += width;
            }
            '\n' => {
                out.push(c);
                column = 0;
            }
            _ => {
                out.push(c);
                column += 1;
            }
        }
    }
    out
}

const TAB_COLUMNS: usize = 4;

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

fn fence_label(info: &str) -> Option<SharedString> {
    Some(info.split_whitespace().next()?.to_lowercase().into())
}

fn looks_like_path(text: &str) -> bool {
    if text.is_empty() || text.contains(char::is_whitespace) || text.contains("://") {
        return false;
    }
    let bare = text.trim_end_matches(|c: char| c.is_ascii_digit() || c == ':');
    let name = bare.rsplit(['/', '\\']).next().unwrap_or(bare);
    let extended = name.rsplit_once('.').is_some_and(|(stem, ext)| {
        !stem.is_empty()
            && (1..=5).contains(&ext.len())
            && ext.starts_with(|c: char| c.is_ascii_lowercase())
            && ext
                .chars()
                .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit())
    });
    extended || (bare.contains(['/', '\\']) && !name.is_empty())
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
        Face::Path | Face::Image(_) => format!("{CODE_PAD}{GLYPH_SLOT}{}{CODE_PAD}", span.text),
        Face::LinkTail => LINK_SLOT.to_owned(),
        Face::Math => {
            let em = span
                .math
                .as_ref()
                .map_or(0.0, |math| f32::from(math.width(px(1.0))));
            let slots = (em / MONO_ADVANCE_EM).ceil().max(1.0) as usize;
            std::iter::repeat_n(MATH_SLOT, slots).collect()
        }
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
        Face::Code | Face::Path | Face::Image(_) | Face::LinkTail | Face::Mention | Face::Math => {
            font(mono(theme))
        }
    };
    if span.stress.strong {
        face.weight = FontWeight::SEMIBOLD;
    }
    if span.stress.italic {
        face.style = FontStyle::Italic;
    }
    let color = match (span.face, &span.link) {
        (Face::Mention, _) => rgba(MENTION_TEXT),
        (Face::Code | Face::Math, _) => ink(theme, T1),
        (Face::Path | Face::Image(_) | Face::LinkTail, _) | (Face::Plain, Some(_)) => {
            theme.color(ColorToken::StatusAccent)
        }
        (Face::Plain, None) if span.stress.strong => theme.color(ColorToken::TextStrong),
        (Face::Plain, None) => base,
    };
    TextRun {
        len,
        font: face,
        color: rgb_to_hsla(color),
        underline: span
            .link
            .as_ref()
            .filter(|_| span.face == Face::Plain)
            .map(|_| UnderlineStyle {
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

fn open(url: &str, on_link: Option<&OnLink>, window: &mut Window, cx: &mut App) {
    let lower = url.to_ascii_lowercase();
    match (
        LINK_SCHEMES.iter().any(|scheme| lower.starts_with(scheme)),
        on_link,
    ) {
        (true, _) => {
            eprintln!("desk: chat link opened {url}");
            cx.open_url(url);
        }
        (false, Some(on_link)) => on_link(url, window, cx),
        (false, None) => {
            eprintln!("desk: chat link {url} not opened, only http, https and mailto open")
        }
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
            Block::Para(spans) | Block::Heading { spans, .. } => {
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
            Block::Source { text, .. } => found.push(text.to_string()),
            Block::Component(_, inner) => gather(inner, found),
            Block::Math(math) => found.push(math.tex.clone()),
            Block::Flow(drawn) => found.push(drawn.source.to_string()),
        }
    }
}

struct Draw<'a> {
    marks: &'a [Marks],
    theme: &'a Theme,
    piece: usize,
    tables: usize,
    gap: f32,
    quoted: bool,
    held: Vec<Piece>,
    on_link: Option<OnLink>,
    on_flow: Option<OnLink>,
    cites: Cites<'a>,
}

pub type Cites<'a> = Option<(&'a Window, &'a App)>;

fn swaps(spans: &[Span], pieces: &[String]) -> Vec<(Range<usize>, SharedString)> {
    let mut found = Vec::new();
    let mut start = 0;
    for (span, piece) in spans.iter().zip(pieces) {
        let range = start..start + piece.len();
        start = range.end;
        let hidden = |range: Range<usize>| (range, SharedString::default());
        let tail = hidden(range.end - CODE_PAD.len().min(piece.len())..range.end);
        match span.face {
            Face::Plain => {}
            Face::Code | Face::Mention => {
                found.extend([hidden(range.start..range.start + CODE_PAD.len()), tail]);
            }
            Face::Path => found.extend([
                hidden(range.start..range.start + CODE_PAD.len() + GLYPH_SLOT.len()),
                tail,
            ]),
            Face::LinkTail => found.push(hidden(range)),
            Face::Math => found.push((range, format!("${}$", span.text).into())),
            Face::Image(n) => found.push((range, format!("{IMAGE_OPEN}{n}]").into())),
        }
    }
    found
}

enum Decor {
    Pill(Rgba, Range<usize>),
    Glyph(Glyph, Rgba),
    Math(Rc<Math>, Rgba),
}

fn decor(
    spans: &[Span],
    pieces: &[String],
    base: Rgba,
    theme: &Theme,
) -> Vec<(Range<usize>, Decor)> {
    let accent = theme.color(ColorToken::StatusAccent);
    let mut found = Vec::new();
    let mut start = 0;
    for (span, piece) in spans.iter().zip(pieces) {
        let range = start..start + piece.len();
        start = range.end;
        let slot = |from: usize, slot: &str| from..from + slot.len();
        let first = span.text.chars().next().map_or(0, char::len_utf8);
        let inner = |lead: usize| {
            (range.start + lead + first).min(range.end)..range.end - CODE_PAD.len().min(piece.len())
        };
        let pill = |fill, lead| (range.clone(), Decor::Pill(fill, inner(lead)));
        let marked = CODE_PAD.len() + GLYPH_SLOT.len();
        match span.face {
            Face::Plain => {}
            Face::Code => found.push(pill(ink(theme, CODE_FILL), CODE_PAD.len())),
            Face::Mention => found.push(pill(rgba(MENTION_FILL), CODE_PAD.len())),
            Face::Path => {
                found.push(pill(tint(accent, PATH_FILL), marked));
                found.push((
                    slot(range.start + CODE_PAD.len(), GLYPH_SLOT),
                    Decor::Glyph(Glyph::File, accent),
                ));
            }
            Face::Image(_) => {
                found.push(pill(tint(accent, PATH_FILL), marked));
                found.push((
                    slot(range.start + CODE_PAD.len(), GLYPH_SLOT),
                    Decor::Glyph(Glyph::Image, accent),
                ));
            }
            Face::LinkTail => found.push((
                slot(range.end - GLYPH_SLOT.len(), GLYPH_SLOT),
                Decor::Glyph(Glyph::Globe, accent),
            )),
            Face::Math => found.extend(
                span.math
                    .clone()
                    .map(|math| (range, Decor::Math(math, base))),
            ),
        }
    }
    found
}

fn paint_decor(
    layout: &TextLayout,
    decor: &[(Range<usize>, Decor)],
    window: &mut Window,
    cx: &App,
) {
    let bounds = layout.bounds();
    let line = layout.line_height();
    for (range, kind) in decor {
        let (Some(mut from), Some(mut to)) = (
            layout.position_for_index(range.start),
            layout.position_for_index(range.end),
        ) else {
            continue;
        };
        match kind {
            Decor::Pill(fill, inner) => {
                let (Some(first), Some(last)) = (
                    layout.position_for_index(inner.start),
                    layout.position_for_index(inner.end),
                ) else {
                    continue;
                };
                if first.y != from.y {
                    from = point(bounds.left(), first.y);
                }
                if last.y != to.y {
                    to = last;
                }
                let height = line - px(PILL_INSET * 2.0);
                let pieces = match from.y == to.y {
                    true => vec![(from.x, to.x, from.y)],
                    false => vec![
                        (from.x, bounds.right(), from.y),
                        (bounds.left(), to.x, to.y),
                    ],
                };
                for (left, right, top) in pieces {
                    window.paint_quad(
                        gpui::fill(
                            Bounds::new(
                                point(left, top + px(PILL_INSET)),
                                size(right - left, height),
                            ),
                            rgb_to_hsla(*fill),
                        )
                        .corner_radii(px(RADIUS_CHIP_SMALL)),
                    );
                }
            }
            Decor::Glyph(glyph, color) => {
                if to.y != from.y {
                    from = point(bounds.left(), to.y);
                }
                let side = px(INLINE_GLYPH);
                let origin = point(
                    from.x + (to.x - from.x - side) / 2.0,
                    from.y + (line - side) / 2.0,
                );
                if let Err(error) = window.paint_svg(
                    Bounds::new(origin, size(side, side)),
                    glyph.path().into(),
                    None,
                    TransformationMatrix::unit(),
                    rgb_to_hsla(*color),
                    cx,
                ) {
                    eprintln!("desk: chat inline glyph not drawn: {error}");
                }
            }
            Decor::Math(math, color) => {
                let em = px(FONT_CHAT);
                let left = from.x + (to.x - from.x - math.width(em)) / 2.0;
                let baseline = from.y + line / 2.0 + em * MATH_AXIS_EM;
                math.paint(
                    point(left, baseline - math.ascent(em)),
                    em,
                    rgb_to_hsla(*color),
                    window,
                    cx,
                );
            }
        }
    }
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
            .map(|(at, block)| {
                let gap = self.gap;
                self.block(block).when(at > 0, |drawn| drawn.mt(px(gap)))
            })
            .collect()
    }

    fn prose(&mut self, spans: &[Span]) -> AnyElement {
        let (at, marks) = self.next();
        let (spans, fulls): (Vec<Span>, Vec<_>) = spans
            .iter()
            .map(|span| {
                if span.face != Face::Path || span.link.is_some() {
                    return (span.clone(), None);
                }
                let numbered = |text: &str| {
                    text.rsplit_once(':')
                        .filter(|(_, n)| !n.is_empty() && n.bytes().all(|b| b.is_ascii_digit()))
                        .map(|(file, _)| file.to_owned())
                };
                let mut file = span.text.to_string();
                for _ in 0..2 {
                    file = numbered(&file).unwrap_or(file);
                }
                let path = Path::new(&file);
                match self
                    .cites
                    .and_then(|(window, cx)| confirmed(&Arc::from(path), window, cx))
                {
                    Some(full) => (span.clone(), Some((full, file.len()))),
                    None => (
                        Span {
                            face: Face::Code,
                            ..span.clone()
                        },
                        None,
                    ),
                }
            })
            .unzip();
        let spans = spans.as_slice();
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
        let joined = SharedString::from(pieces.concat());
        let key = link_key(&joined, at);
        let text = StyledText::new(joined.clone()).with_runs(mark_runs(runs, marks));
        let held = Piece::new(joined, text.layout().clone(), swaps(spans, &pieces));
        let decor = decor(spans, &pieces, base, self.theme);
        let layout = text.layout().clone();
        let (mut ranges, mut clicks, mut targets, mut start) =
            (Vec::new(), Vec::new(), Vec::new(), 0);
        for ((span, piece), full) in spans.iter().zip(&pieces).zip(&fulls) {
            let range = start..start + piece.len();
            start = range.end;
            let media = span.face == Face::Path && media_view::shows(Path::new(span.text.as_ref()));
            if let Some(url) = &span.link {
                ranges.push(range.clone());
                clicks.push(Click::Url(url.clone()));
                targets.push((range.clone(), Subject::Link(url.clone())));
            }
            match full {
                Some((full, _)) if media => {
                    ranges.push(range.clone());
                    clicks.push(Click::Media(full.clone()));
                    targets.push((range.clone(), Subject::File(full.clone())));
                }
                Some((full, cited)) if self.on_link.is_some() => {
                    let spot = span.text.get(*cited..).unwrap_or_default();
                    ranges.push(range.clone());
                    clicks.push(Click::Url(format!("{}{spot}", full.display()).into()));
                }
                _ => {}
            }
            if let Face::Image(n) = span.face {
                targets.push((range, Subject::Image(n, None)));
            }
        }
        let on_link = self.on_link.clone();
        let drawn = match targets.is_empty() && ranges.is_empty() {
            true => text.into_any_element(),
            false => {
                let placed = text.layout().clone();
                div()
                    .child(
                        InteractiveText::new(("chat-text", at), text)
                            .on_click(ranges, move |picked, window, cx| match clicks.get(picked) {
                                Some(Click::Url(url)) => open(url, on_link.as_ref(), window, cx),
                                Some(Click::Media(path)) => window.dispatch_action(
                                    Box::new(OpenMedia { path: path.clone() }),
                                    cx,
                                ),
                                None => {}
                            })
                            .on_hover(move |index, _, window, cx| {
                                let hovered =
                                    index.and_then(|index| targeted(index, &targets, &placed));
                                hover(key, hovered, window, cx);
                            }),
                    )
                    .child(link_card(key))
                    .into_any_element()
            }
        };
        let shown = match decor.is_empty() {
            true => drawn,
            false => div()
                .relative()
                .child(
                    canvas(
                        |_, _, _| {},
                        move |_, (), window, cx| paint_decor(&layout, &decor, window, cx),
                    )
                    .absolute()
                    .size_full(),
                )
                .child(drawn)
                .into_any_element(),
        };
        self.hold(held, shown)
    }

    fn hold(&mut self, piece: Piece, element: impl IntoElement) -> AnyElement {
        let tracked = piece.track(element);
        self.held.push(piece);
        tracked
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

    fn block(&mut self, block: &Block) -> Div {
        let theme = self.theme;
        match block {
            Block::Para(spans) => div().child(self.prose(spans)),
            Block::Heading { level, spans } => {
                let scale = match level {
                    1 => H1_SCALE,
                    2 => H2_SCALE,
                    3 => H3_SCALE,
                    _ => 1.0,
                };
                div()
                    .text_size(px(FONT_CHAT * scale))
                    .line_height(px(CHAT_LINE * scale))
                    .child(self.prose(spans))
            }
            Block::List { start, items } => {
                let mut number = start.map(u64::from);
                div().flex().flex_col().children(items.iter().map(|item| {
                    let shown = number;
                    number = number.map(|at| at.saturating_add(1));
                    let was = std::mem::replace(&mut self.gap, ITEM_GAP);
                    let inner = self.blocks(&item.blocks);
                    self.gap = was;
                    div()
                        .flex()
                        .child(self.marker(shown, item.task))
                        .child(div().flex_1().min_w_0().flex().flex_col().children(inner))
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
                let text = StyledText::new(code.text.clone())
                    .with_runs(mark_runs(code_runs(code, theme), marks));
                let piece = Piece::new(code.text.clone(), text.layout().clone(), Vec::new());
                boxed(theme)
                    .relative()
                    .bg(ink(theme, CODE_FILL))
                    .child(self.hold(piece, text))
                    .when_some(code.label.clone(), |boxed, label| {
                        boxed.child(
                            div()
                                .absolute()
                                .top(px(CALLS_PAD_Y))
                                .right(px(CALLS_PAD_X))
                                .font_family(theme.word(WordToken::ShapeFont))
                                .text_size(px(FONT_SMALL))
                                .text_color(ink(theme, T3))
                                .child(label),
                        )
                    })
            }
            Block::Table {
                alignments,
                rows,
                scroll,
            } => {
                let columns = rows.iter().map(Vec::len).max().unwrap_or_default();
                let widths: Vec<f32> = (0..columns)
                    .map(|column| {
                        let widest = rows
                            .iter()
                            .filter_map(|row| row.get(column))
                            .map(|cell| {
                                cell.iter()
                                    .map(|span| span_text(span).chars().count())
                                    .sum::<usize>()
                            })
                            .max()
                            .unwrap_or_default();
                        (widest as f32 * CELL_CHAR + CELL_PAD_X * 2.0).clamp(CELL_MIN, CELL_MAX)
                    })
                    .collect();
                let table = self.tables;
                self.tables += 1;
                let lines: Vec<Div> = rows
                    .iter()
                    .enumerate()
                    .map(|(at, row)| {
                        div()
                            .flex()
                            .when(at == 0, |head| head.bg(ink(theme, CODE_FILL)))
                            .when(at > 0, |line| {
                                line.border_t_1().border_color(ink(theme, RULE_INK))
                            })
                            .children(widths.iter().enumerate().map(|(column, width)| {
                                let cell_box = div()
                                    .flex_basis(px(*width))
                                    .flex_grow(1.0)
                                    .flex_shrink_0()
                                    .min_w_0()
                                    .px(px(CELL_PAD_X))
                                    .py(px(CELL_PAD_Y))
                                    .children(row.get(column).map(|cell| self.prose(cell)));
                                match alignments.get(column) {
                                    Some(Alignment::Center) => cell_box.text_center(),
                                    Some(Alignment::Right) => cell_box.text_right(),
                                    Some(Alignment::Left | Alignment::None) | None => cell_box,
                                }
                            }))
                    })
                    .collect();
                let wheel = scroll.clone();
                div()
                    .relative()
                    .min_w_0()
                    .rounded(px(ROW_RADIUS))
                    .border_1()
                    .border_color(ink(theme, RULE_INK))
                    .child(
                        div()
                            .id(("chat-table", table))
                            .flex()
                            .rounded(px(ROW_RADIUS))
                            .overflow_x_scroll()
                            .restrict_scroll_to_axis()
                            .track_scroll(scroll)
                            .on_scroll_wheel(move |event, window, cx| {
                                let sideways = event.delta.pixel_delta(window.line_height()).x;
                                if sideways == px(0.0) || wheel.max_offset().x <= px(0.0) {
                                    return;
                                }
                                cx.stop_propagation();
                                eprintln!(
                                    "desk: chat table {columns} cols scroll_x {}",
                                    f32::from(-wheel.offset().x)
                                );
                            })
                            .child(
                                div()
                                    .flex_none()
                                    .min_w_full()
                                    .w(px(widths.iter().sum()))
                                    .flex()
                                    .flex_col()
                                    .children(lines),
                            ),
                    )
                    .child(table_rail(scroll.clone(), theme))
                    .child(scrollbar(("chat-table-bar", table), scroll).axis(Axis::Horizontal))
            }
            Block::Rule => div().h(px(RULE_HEIGHT)).bg(ink(theme, RULE_INK)),
            Block::Source { text, refused } => {
                let (_, marks) = self.next();
                let styled = StyledText::new(text.clone()).with_highlights(marks.to_vec());
                let piece = Piece::new(text.clone(), styled.layout().clone(), Vec::new());
                boxed(theme)
                    .shadow(vec![ring(ink(theme, QUEUED_RING))])
                    .text_color(ink(theme, T3))
                    .child(self.hold(piece, styled))
                    .when_some(refused.clone(), |boxed, why| {
                        boxed.child(
                            div()
                                .text_size(px(FONT_SMALL))
                                .text_color(theme.color(ColorToken::StatusDanger))
                                .child(why),
                        )
                    })
            }
            Block::Component(rich, inner) => {
                let key = self.tables;
                self.tables += 1;
                let drawn = self.blocks(inner);
                rich::frame(rich, drawn, key, theme)
            }
            Block::Math(math) => {
                let (at, _) = self.next();
                let base = match self.quoted {
                    true => ink(theme, T2),
                    false => theme.color(ColorToken::TextBase),
                };
                div().child(math::display(
                    ("chat-math", at),
                    math.clone(),
                    px(FONT_CHAT),
                    rgb_to_hsla(base),
                ))
            }
            Block::Flow(drawn) => {
                let (at, _) = self.next();
                let (wide, tall) = drawn.size(FLOW_ZOOM);
                let shown = drawn.clone();
                let picture = canvas(
                    |_, _, _| {},
                    move |bounds, (), window, cx| {
                        let pad = px(FLOW_PAD);
                        let origin = point(bounds.origin.x + pad, bounds.origin.y + pad);
                        shown.paint(origin, FLOW_ZOOM, &ActiveTheme::theme(cx), window, cx);
                    },
                )
                .size_full();
                let open = self.on_flow.clone().map(|on_flow| {
                    let source = drawn.source.clone();
                    button(
                        ("chat-flow-open", at),
                        "Open in Draw",
                        Some(Glyph::Palette),
                        ButtonKind::Text,
                        theme,
                    )
                    .on_click(move |_, window, cx| on_flow(&source, window, cx))
                });
                div()
                    .flex()
                    .flex_col()
                    .gap_1()
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .justify_between()
                            .text_size(px(FONT_SMALL))
                            .text_color(ink(theme, T3))
                            .child(FLOW_FENCE)
                            .children(open),
                    )
                    .child(
                        div()
                            .id(("chat-flow", at))
                            .min_w_0()
                            .rounded(px(ROW_RADIUS))
                            .bg(ink(theme, CODE_FILL))
                            .overflow_x_scroll()
                            .restrict_scroll_to_axis()
                            .child(
                                div()
                                    .flex_none()
                                    .w(px(wide + FLOW_PAD * 2.0))
                                    .h(px(tall + FLOW_PAD * 2.0))
                                    .child(picture),
                            ),
                    )
            }
        }
    }
}

fn table_rail(scroll: ScrollHandle, theme: &Theme) -> impl IntoElement {
    let track = ink(theme, RULE_INK);
    let thumb = theme.color(ColorToken::Scrollbar);
    canvas(
        |_, _, _| {},
        move |_, (), window, _| {
            let max = scroll.max_offset().x;
            if max <= px(0.0) {
                return;
            }
            let view = scroll.bounds();
            let top = view.bottom() - px(RAIL_INSET + RAIL_THICK);
            let left = view.left() + px(RAIL_INSET);
            let length = view.size.width - px(RAIL_INSET * 2.0);
            let bar = (length * (view.size.width / (view.size.width + max)))
                .max(px(RAIL_THUMB_MIN))
                .min(length);
            let travel = (length - bar) * (-scroll.offset().x / max).clamp(0.0, 1.0);
            let radius = px(RAIL_THICK / 2.0);
            window.paint_quad(
                gpui::fill(
                    Bounds::new(point(left, top), size(length, px(RAIL_THICK))),
                    track,
                )
                .corner_radii(radius),
            );
            window.paint_quad(
                gpui::fill(
                    Bounds::new(point(left + travel, top), size(bar, px(RAIL_THICK))),
                    thumb,
                )
                .corner_radii(radius),
            );
        },
    )
    .absolute()
    .top_0()
    .left_0()
    .size_full()
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
    cites: Cites,
    theme: &Theme,
) -> Div {
    flowing_lead(id, time, body, marks, None, cites, theme)
}

pub fn flowing_lead(
    id: impl Into<ElementId>,
    time: impl Into<SharedString>,
    body: &[Block],
    marks: &[Marks],
    on_flow: Option<OnLink>,
    cites: Cites,
    theme: &Theme,
) -> Div {
    linked_lead(id, time, body, marks, None, on_flow, cites, theme)
}

#[expect(
    clippy::too_many_arguments,
    reason = "a lead takes the document's own arguments plus its time, as flowing_lead does"
)]
pub fn linked_lead(
    id: impl Into<ElementId>,
    time: impl Into<SharedString>,
    body: &[Block],
    marks: &[Marks],
    on_link: Option<OnLink>,
    on_flow: Option<OnLink>,
    cites: Cites,
    theme: &Theme,
) -> Div {
    div()
        .mt(px(LEAD_GAP))
        .min_w_0()
        .child(who(agent("Orchestrator", None, theme), time, false, theme))
        .child(linked_document(
            id, body, marks, on_link, on_flow, cites, theme,
        ))
}

pub fn document(
    id: impl Into<ElementId>,
    body: &[Block],
    marks: &[Marks],
    cites: Cites,
    theme: &Theme,
) -> Div {
    linked_document(id, body, marks, None, None, cites, theme)
}

pub fn linked_document(
    id: impl Into<ElementId>,
    body: &[Block],
    marks: &[Marks],
    on_link: Option<OnLink>,
    on_flow: Option<OnLink>,
    cites: Cites,
    theme: &Theme,
) -> Div {
    let mut draw = Draw {
        cites,
        marks,
        theme,
        piece: 0,
        tables: 0,
        gap: GRID_GAP,
        quoted: false,
        held: Vec::new(),
        on_link,
        on_flow,
    };
    let id = id.into();
    let blocks = draw.blocks(body);
    let fill = rgb_to_hsla(tint(theme.color(ColorToken::FocusRing), SELECTION_FILL));
    div()
        .min_w_0()
        .text_size(px(FONT_CHAT))
        .line_height(px(CHAT_LINE))
        .text_color(theme.color(ColorToken::TextBase))
        .child(selectable(
            id.clone(),
            div().id(id).flex().flex_col().children(blocks),
            draw.held,
            fill,
        ))
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
        .when(opened < 1.0, |fold| {
            fold.h(px(natural.get() * opened)).overflow_hidden()
        })
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
        AgentMark::Failed => bang(theme).into_any_element(),
    }
}

fn bang(theme: &Theme) -> Div {
    div()
        .flex_none()
        .w(px(MARK))
        .flex()
        .justify_center()
        .font_family(mono(theme))
        .text_color(theme.color(ColorToken::StatusDanger))
        .child("!")
}

pub fn agent_row(id: impl Into<ElementId>, line_of: &Agent, theme: &Theme) -> Div {
    row(theme)
        .text_color(ink(theme, HOVER_TEXT))
        .child(agent_mark(id, line_of.mark, theme))
        .child(agent(line_of.name.clone(), line_of.tone, theme))
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
    origin_row(Glyph::Cron, job, schedule, prompt, theme)
}

pub fn origin_row(
    mark: Glyph,
    who: impl Into<SharedString>,
    head: impl Into<SharedString>,
    said: impl Into<SharedString>,
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
                .child(glyph(mark, ICON_SMALL, ink(theme, T3))),
        )
        .child(agent(who, None, theme))
        .child(dim(head, theme))
        .child(line(said).text_color(ink(theme, T3)))
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
        .child(bang(theme))
        .child(div().flex_1().min_w_0().child(
            StyledText::new(format!("{text}: {detail}")).with_highlights([(
                text.len() + 2..text.len() + 2 + detail.len(),
                HighlightStyle::from(ink(theme, T3)),
            )]),
        ))
        .child(dim(link, theme).text_size(px(FONT_SMALL)))
}

fn copy_button(id: impl Into<ElementId>, copied: SharedString, theme: &Theme) -> Stateful<Div> {
    div()
        .id(id)
        .aria_label("Copy")
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .size(px(ICON_SMALL + 2.0 * STACK_INSET))
        .rounded(px(RADIUS_CHIP_SMALL))
        .cursor_pointer()
        .hover(|style| style.bg(ink(theme, HOVER)))
        .on_click(move |_, _, cx| {
            cx.stop_propagation();
            cx.write_to_clipboard(ClipboardItem::new_string(copied.to_string()));
        })
        .child(glyph(Glyph::Copy, ICON_TINY, ink(theme, CAPTION_TEXT)))
}

pub fn fork_divider(
    id: impl Into<ElementId>,
    label: SharedString,
    session: SharedString,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Stateful<Div> {
    let id = id.into();
    let rule = || div().flex_1().h(px(HAIRLINE)).bg(ink(theme, FORK_RULE));
    let divider = div()
        .id(id.clone())
        .mt(px(NOTE_GAP))
        .flex()
        .items_center()
        .gap(px(ROW_ITEMS))
        .text_size(px(FONT_SMALL))
        .line_height(px(ROW_LINE))
        .text_color(ink(theme, CAPTION_TEXT))
        .child(rule())
        .child(glyph(Glyph::Fork, ICON_TINY, ink(theme, CAPTION_TEXT)))
        .child(div().flex_none().child(label))
        .child(rule());
    let card = div()
        .flex()
        .items_center()
        .gap(px(ROW_ITEMS))
        .text_size(px(FONT_SMALL))
        .font_family(mono(theme))
        .text_color(ink(theme, T2))
        .child(div().min_w_0().child(session.clone()))
        .child(copy_button((id.clone(), "copy"), session, theme));
    hover_card((id, "card"), divider, Edge::Frame, card, theme, window, cx)
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

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CallState {
    Running,
    Done,
    Failed,
    Skipped,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Ink {
    Plain,
    Muted,
    Ref,
    Added,
    Removed,
    Failed,
    Warn,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Phrase {
    text: SharedString,
    inks: Vec<(Range<usize>, Ink)>,
}

impl Phrase {
    pub fn new<'a>(pieces: impl IntoIterator<Item = (&'a str, Ink)>) -> Self {
        let (mut text, mut inks) = (String::new(), Vec::new());
        for (piece, tone) in pieces {
            let start = text.len();
            text.push_str(piece);
            if tone != Ink::Plain {
                inks.push((start..text.len(), tone));
            }
        }
        Self {
            text: text.into(),
            inks,
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Object {
    File(SharedString),
    Token(SharedString),
    Agent(SharedString),
    Phrase(Phrase),
}

impl Object {
    pub fn text(&self) -> &str {
        match self {
            Object::File(text) | Object::Token(text) | Object::Agent(text) => text,
            Object::Phrase(phrase) => &phrase.text,
        }
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct ToolCard {
    pub glyphs: Vec<Glyph>,
    pub verb: SharedString,
    pub objects: Vec<Object>,
    pub meta: Option<SharedString>,
    pub state: CallState,
}

fn child_id(id: &ElementId, name: &'static str) -> ElementId {
    ElementId::NamedChild(Arc::new(id.clone()), name.into())
}

fn object_chip(object: &Object, theme: &Theme) -> AnyElement {
    match object {
        Object::File(name) => file_chip(
            glyph(Glyph::File, ICON_SMALL, ink(theme, CAPTION_TEXT)),
            name.clone(),
            theme,
        )
        .max_w(px(OBJECT_MAX))
        .into_any_element(),
        Object::Token(text) => token_chip(text.clone(), theme)
            .max_w(px(OBJECT_MAX))
            .truncate()
            .into_any_element(),
        Object::Agent(name) => {
            agent(name.clone(), Some(name_tone(name, theme)), theme).into_any_element()
        }
        Object::Phrase(phrase) => {
            let marks = phrase.inks.iter().map(|(range, tone)| {
                let color = match tone {
                    Ink::Plain => ink(theme, HOVER_TEXT),
                    Ink::Muted => ink(theme, T3),
                    Ink::Ref => theme.color(ColorToken::MentionText),
                    Ink::Added => theme.color(ColorToken::GitAdded),
                    Ink::Removed => theme.color(ColorToken::GitDeleted),
                    Ink::Failed => theme.color(ColorToken::StatusDanger),
                    Ink::Warn => theme.color(ColorToken::StatusWarn),
                };
                (range.clone(), HighlightStyle::from(color))
            });
            div()
                .min_w_0()
                .truncate()
                .child(StyledText::new(phrase.text.clone()).with_highlights(marks))
                .into_any_element()
        }
    }
}

fn lead_glyphs(glyphs: &[Glyph], color: Rgba, theme: &Theme) -> Div {
    let lead = div().flex_none().flex().items_center().justify_center();
    match glyphs {
        [one] => lead.w(px(MARK)).child(glyph(*one, ICON_SMALL, color)),
        many => lead
            .gap(px(STACK_GAP))
            .h(px(FCHIP - 2.0 * STACK_INSET))
            .px(px(STACK_PAD))
            .rounded(px(RADIUS_CHIP_SMALL))
            .bg(ink(theme, HOVER))
            .children(many.iter().map(|each| glyph(*each, ICON_TINY, color))),
    }
}

fn call_status(id: &ElementId, state: CallState, theme: &Theme) -> AnyElement {
    match state {
        CallState::Running => spinner(child_id(id, "spin"), theme).into_any_element(),
        CallState::Failed => bang(theme).into_any_element(),
        CallState::Done => div()
            .flex_none()
            .w(px(MARK))
            .flex()
            .justify_center()
            .child(glyph(
                Glyph::Check,
                ICON_SMALL,
                theme.color(ColorToken::StatusLive),
            ))
            .into_any_element(),
        CallState::Skipped => div().flex_none().w(px(MARK)).into_any_element(),
    }
}

fn card_row(
    id: ElementId,
    card: &ToolCard,
    turned: Option<f32>,
    (bar, trace): (AnyElement, Option<AnyElement>),
    theme: &Theme,
) -> Stateful<Div> {
    let danger = theme.color(ColorToken::StatusDanger);
    let failed = card.state == CallState::Failed;
    let lead = lead_glyphs(
        &card.glyphs,
        if failed { danger } else { ink(theme, T3) },
        theme,
    );
    let status = call_status(&id, card.state, theme);
    let verb = match card.state {
        CallState::Running => Shimmer::new(
            child_id(&id, "shimmer"),
            card.verb.clone(),
            ink(theme, T3),
            ink(theme, HOVER_TEXT),
        )
        .into_any_element(),
        CallState::Done | CallState::Failed => div()
            .flex_none()
            .child(card.verb.clone())
            .into_any_element(),
        CallState::Skipped => div()
            .flex_none()
            .text_color(ink(theme, T3))
            .child(card.verb.clone())
            .into_any_element(),
    };
    let meta = line(card.meta.clone().unwrap_or_default())
        .flex_1()
        .text_color(if failed { danger } else { ink(theme, T3) });
    row(theme)
        .id(id)
        .relative()
        .group(CARD_GROUP)
        .h(px(FCHIP))
        .mx(px(-CARD_PAD))
        .px(px(CARD_PAD))
        .rounded(px(ROW_RADIUS))
        .text_color(ink(theme, HOVER_TEXT))
        .when(failed, |row| {
            row.bg(tint(theme.color(ColorToken::GitDeleted), FAIL_TINT))
        })
        .child(bar)
        .child(lead)
        .when(!card.verb.is_empty(), |row| row.child(verb))
        .children(card.objects.iter().map(|object| object_chip(object, theme)))
        .child(meta)
        .children(trace)
        .child(status)
        .child(match turned {
            Some(turned) => chevron(turned, theme).into_any_element(),
            None => div().flex_none().size(px(ICON_SMALL)).into_any_element(),
        })
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TicketCard {
    pub board: Option<SharedString>,
    pub id: Option<SharedString>,
    pub title: Option<SharedString>,
    pub status: Option<SharedString>,
    pub acceptance: Vec<SharedString>,
}

pub fn ticket_card(ticket: &TicketCard, theme: &Theme) -> Div {
    let head = div()
        .flex()
        .items_center()
        .gap(px(ROW_ITEMS))
        .min_w_0()
        .h(px(FCHIP))
        .children(ticket.board.clone().map(|board| token_chip(board, theme)))
        .children(ticket.id.clone().map(|id| {
            div()
                .flex_none()
                .font_family(mono(theme))
                .text_color(ink(theme, T2))
                .child(id)
        }))
        .children(
            ticket
                .title
                .clone()
                .map(|title| line(title).flex_1().text_color(ink(theme, HOVER_TEXT))),
        )
        .children(ticket.status.clone().map(|status| dim(status, theme)));
    div()
        .mt(px(NOTE_GAP))
        .flex()
        .flex_col()
        .gap(px(ITEM_GAP))
        .min_w_0()
        .text_size(px(FONT_BODY))
        .line_height(px(ROW_LINE))
        .child(head)
        .children(ticket.acceptance.iter().map(|said| {
            div()
                .flex()
                .gap(px(ROW_ITEMS))
                .min_w_0()
                .text_color(ink(theme, T2))
                .child(dim("\u{2022}", theme))
                .child(div().flex_1().min_w_0().child(said.clone()))
        }))
}

pub fn tool_section(
    id: impl Into<ElementId>,
    title: &'static str,
    text: SharedString,
    theme: &Theme,
) -> Div {
    let lines: Vec<&str> = text.lines().collect();
    let hidden = lines.len().saturating_sub(SECTION_LINES);
    let shown = lines.get(hidden..).unwrap_or_default().join("\n");
    div()
        .mt(px(NOTE_GAP))
        .flex()
        .flex_col()
        .gap(px(ITEM_GAP))
        .min_w_0()
        .child(
            div()
                .flex()
                .items_center()
                .text_size(px(FONT_SMALL))
                .line_height(px(ROW_LINE))
                .text_color(ink(theme, T3))
                .child(div().flex_1().child(title))
                .child(copy_button(id, text.clone(), theme)),
        )
        .child(
            div()
                .rounded(px(RADIUS_CHIP_SMALL))
                .px(px(CODE_BLOCK_PAD))
                .py(px(ITEM_GAP))
                .bg(ink(theme, CODE_FILL))
                .font_family(mono(theme))
                .text_size(px(FONT_SMALL))
                .line_height(px(CALL_LINE))
                .text_color(ink(theme, T2))
                .min_w_0()
                .when(hidden > 0, |block| {
                    block.child(
                        div()
                            .text_color(ink(theme, T3))
                            .child(format!("{hidden} lines above")),
                    )
                })
                .child(div().min_w_0().whitespace_normal().child(shown)),
        )
}

fn rail(theme: &Theme) -> Div {
    div()
        .ml(px(MARK / 2.0))
        .pl(px(CALLS_INDENT - MARK / 2.0))
        .border_l(px(RULE_HEIGHT))
        .border_color(ink(theme, RULE_INK))
        .flex()
        .flex_col()
        .min_w_0()
}

#[derive(IntoElement)]
pub struct Shimmer {
    id: ElementId,
    text: SharedString,
    base: Rgba,
    glow: Rgba,
}

impl Shimmer {
    pub fn new(id: impl Into<ElementId>, text: SharedString, base: Rgba, glow: Rgba) -> Self {
        Self {
            id: id.into(),
            text,
            base,
            glow,
        }
    }
}

impl RenderOnce for Shimmer {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let label = div()
            .relative()
            .flex_none()
            .whitespace_nowrap()
            .text_color(self.base)
            .child(self.text.clone());
        if reduced_motion(cx) {
            return label.into_any_element();
        }
        let (text, glow) = (self.text, self.glow);
        let halo = Rgba {
            alpha: (self.base.alpha + glow.alpha) / 2.0,
            ..glow
        };
        label
            .child(div().absolute().inset(px(0.0)).with_animation(
                self.id,
                spin(SHIMMER_PERIOD),
                move |frame, phase| {
                    let center = phase * (1.0 + 2.0 * SHIMMER_BAND) - SHIMMER_BAND;
                    let band = |width: f32, color: Rgba| {
                        let left = center - width / 2.0;
                        div()
                            .absolute()
                            .top_0()
                            .bottom_0()
                            .left(relative(left))
                            .w(relative(width))
                            .overflow_hidden()
                            .child(
                                div()
                                    .absolute()
                                    .top_0()
                                    .left(relative(-left / width))
                                    .w(relative(1.0 / width))
                                    .whitespace_nowrap()
                                    .text_color(color)
                                    .child(text.clone()),
                            )
                    };
                    frame
                        .child(band(SHIMMER_BAND * 2.0, halo))
                        .child(band(SHIMMER_BAND, glow))
                },
            ))
            .into_any_element()
    }
}

struct Fold {
    open: bool,
    failed: bool,
    presence: Presence,
    natural: Rc<Cell<f32>>,
    seen: Seen,
}

type Seen = Rc<Cell<Option<Bounds<Pixels>>>>;

struct Spot {
    glide: Glide,
    hovered: Option<ElementId>,
    painter: Option<ElementId>,
    bounds: Option<Bounds<Pixels>>,
}

impl Spot {
    fn aim(&mut self, id: &ElementId, bounds: Option<Bounds<Pixels>>, now: Instant) {
        self.hovered = Some(id.clone());
        self.painter = Some(id.clone());
        if let Some(bounds) = bounds {
            self.bounds = Some(bounds);
            self.glide.retarget(bounds, now);
        }
    }

    fn leave(&mut self, pointer: Point<Pixels>, now: Instant) -> bool {
        let near = self
            .bounds
            .is_some_and(|bounds| bounds.dilate(px(TOOL_GAP)).contains(&pointer));
        let left = self.hovered.is_some() && !near;
        if left {
            self.hovered = None;
            self.glide.hide(now);
        }
        left
    }
}

fn spot_bar(id: ElementId, spot: Entity<Spot>, seen: Seen, fill: Rgba) -> impl IntoElement {
    let painted = id.clone();
    let painter = spot.clone();
    canvas(
        move |bounds, _, cx| {
            seen.set(Some(bounds));
            spot.update(cx, |spot, _| {
                if spot.hovered.as_ref() == Some(&id) {
                    spot.aim(&id, Some(bounds), Instant::now());
                }
            });
        },
        move |_, (), window, cx| {
            let now = Instant::now();
            let drawn = painter.update(cx, |spot, _| {
                (spot.painter.as_ref() == Some(&painted)).then(|| {
                    let bar = spot.glide.sample(now);
                    let hovered = spot.hovered.as_ref() == Some(&painted);
                    (bar, spot.glide.moving(), hovered)
                })
            });
            let Some((bar, moving, hovered)) = drawn else {
                return;
            };
            if let Some((rect, shade)) = bar {
                window.paint_quad(
                    gpui::fill(rect, tint(fill, fill.alpha * shade))
                        .corner_radii(Corners::all(px(ROW_RADIUS))),
                );
            }
            if moving {
                window.request_animation_frame();
            }
            if hovered {
                let leaver = painter.clone();
                window.on_mouse_event(move |event: &MouseMoveEvent, phase, _, cx| {
                    if phase == DispatchPhase::Bubble {
                        leaver.update(cx, |spot, cx| {
                            if spot.leave(event.position, Instant::now()) {
                                cx.notify();
                            }
                        });
                    }
                });
            }
        },
    )
    .absolute()
    .top_0()
    .left_0()
    .size_full()
}

type Body = Box<dyn FnOnce(&Theme) -> Vec<AnyElement>>;
type OnTrace = Rc<dyn Fn(&mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct ToolFold {
    id: ElementId,
    head: ToolCard,
    opens: bool,
    body: Option<Body>,
    trace: Option<OnTrace>,
}

impl ToolFold {
    pub fn new(
        id: impl Into<ElementId>,
        head: ToolCard,
        opens: bool,
        body: impl FnOnce(&Theme) -> Vec<AnyElement> + 'static,
    ) -> Self {
        Self {
            id: id.into(),
            head,
            opens,
            body: Some(Box::new(body)),
            trace: None,
        }
    }

    pub fn line(id: impl Into<ElementId>, head: ToolCard) -> Self {
        Self {
            id: id.into(),
            head,
            opens: false,
            body: None,
            trace: None,
        }
    }

    pub fn on_trace(mut self, on_trace: impl Fn(&mut Window, &mut App) + 'static) -> Self {
        self.trace = Some(Rc::new(on_trace));
        self
    }
}

impl RenderOnce for ToolFold {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let spot = window.use_keyed_state(CARD_SPOT, cx, |_, _| Spot {
            glide: Glide::new(GlideKind::Eased),
            hovered: None,
            painter: None,
            bounds: None,
        });
        let reduced = reduced_motion(cx);
        spot.update(cx, |spot, _| spot.glide.set_reduced(reduced));
        let trace = self.trace.map(|on_trace| {
            chip::trace(child_id(&self.id, "trace"), MENTION.into(), &theme)
                .opacity(0.0)
                .group_hover(CARD_GROUP, |style| style.opacity(1.0))
                .on_click(move |_, window, cx| {
                    cx.stop_propagation();
                    on_trace(window, cx);
                })
                .into_any_element()
        });
        let Some(body) = self.body else {
            let seen = Rc::default();
            let row = hovered_row(self.id, &self.head, None, (spot, seen, trace), &theme);
            return row.into_any_element();
        };
        let opens = self.opens;
        let fold = window.use_keyed_state(self.id.clone(), cx, |_, _| Fold {
            open: opens,
            failed: opens,
            presence: match opens {
                true => Presence::shown(PANEL_IN_MS, PANEL_OUT_MS),
                false => Presence::new(PANEL_IN_MS, PANEL_OUT_MS),
            },
            natural: Rc::default(),
            seen: Rc::default(),
        });
        let now = Instant::now();
        if opens {
            fold.update(cx, |fold, _| {
                if !fold.failed {
                    fold.failed = true;
                    fold.open = true;
                    fold.presence.set_open(true, now);
                }
            });
        }
        let (open, presence, natural, seen) = {
            let read = fold.read(cx);
            (
                read.open,
                read.presence,
                read.natural.clone(),
                read.seen.clone(),
            )
        };
        let phase = match (reduced, open) {
            (true, true) => Phase::Open,
            (true, false) => Phase::Closed,
            (false, _) => presence.phase(now),
        };
        let turned = match phase {
            Phase::Open => 1.0,
            Phase::Closed => 0.0,
            Phase::Opening | Phase::Closing => {
                window.request_animation_frame();
                presence.progress(now)
            }
        };
        let head = hovered_row(
            self.id,
            &self.head,
            Some(turned),
            (spot, seen, trace),
            &theme,
        )
        .cursor_pointer()
        .on_click(move |_, _, cx| {
            fold.update(cx, |fold, cx| {
                fold.open = !fold.open;
                fold.presence.set_open(fold.open, Instant::now());
                cx.notify();
            });
        });
        let body = (phase != Phase::Closed)
            .then(|| folding(natural, turned, rail(&theme).children(body(&theme))));
        div()
            .flex()
            .flex_col()
            .min_w_0()
            .child(head)
            .children(body)
            .into_any_element()
    }
}

fn hovered_row(
    id: ElementId,
    head: &ToolCard,
    turned: Option<f32>,
    (spot, seen, trace): (Entity<Spot>, Seen, Option<AnyElement>),
    theme: &Theme,
) -> Stateful<Div> {
    let bar =
        spot_bar(id.clone(), spot.clone(), seen.clone(), ink(theme, HOVER)).into_any_element();
    let hovered = id.clone();
    card_row(id, head, turned, (bar, trace), theme).on_mouse_move(move |_, _, cx| {
        spot.update(cx, |spot, cx| {
            if spot.hovered.as_ref() != Some(&hovered) {
                spot.aim(&hovered, seen.get(), Instant::now());
                cx.notify();
            }
        });
    })
}

#[cfg(test)]
mod tests {
    use super::{Block, Face, Streaming, lead, markdown, pieces};
    use crate::theme::{build, parse_layer};

    #[test]
    fn math_reaches_chat_as_math_and_prices_stay_text() {
        let body =
            markdown("inline $x^2$ costs $5 and $6\n\n$$\\frac{a}{b}$$\n\nbroken $\\frac{a$ here");
        let faces: Vec<Vec<(Face, String, bool)>> = body
            .iter()
            .filter_map(|block| match block {
                Block::Para(spans) => Some(
                    spans
                        .iter()
                        .map(|s| (s.face, s.text.to_string(), s.math.is_some()))
                        .collect(),
                ),
                _ => None,
            })
            .collect();
        assert!(
            faces[0].contains(&(Face::Math, "x^2".into(), true)),
            "{:?}",
            faces[0].iter().map(|f| &f.1).collect::<Vec<_>>()
        );
        assert!(faces[0].iter().any(|f| f.1.contains("costs $5 and $6")));
        assert!(matches!(body[1], Block::Math(ref m) if m.tex == "\\frac{a}{b}"));
        assert!(faces[1].contains(&(Face::Code, "$\\frac{a$".into(), false)));
        let streamed = Streaming::default().blocks("so $y$ and");
        assert!(matches!(&streamed[0], Block::Para(spans)
            if spans.iter().any(|s| s.face == Face::Math && s.math.is_some())));
        let open = Streaming::default().blocks("so $\\frac{y");
        assert!(matches!(&open[0], Block::Para(spans)
            if spans.iter().all(|s| !s.text.contains('$'))));
    }

    #[test]
    fn display_math_inside_a_paragraph_gets_its_own_line() {
        let body = markdown("inline $x^2$ and prices $5 and $6.50.\n$$\\frac{a}{b} + \\sqrt{x}$$");
        let shape: Vec<String> = body
            .iter()
            .map(|block| match block {
                Block::Para(spans) => spans
                    .iter()
                    .map(|s| format!("{}:{}", s.math.is_some(), s.text))
                    .collect::<Vec<_>>()
                    .join("|"),
                Block::Math(m) => format!("math:{}", m.tex),
                _ => "other".into(),
            })
            .collect();
        assert_eq!(
            shape,
            [
                "false:inline |true:x^2|false: and prices $5 and $6.50.",
                "math:\\frac{a}{b} + \\sqrt{x}",
            ]
        );
    }

    #[test]
    fn chat_stream_bench() {
        let mut message = String::new();
        for n in 0..50 {
            message.push_str(&match n % 3 {
                0 => format!("<Card title=\"card {n}\">\nSome **text** in card {n}.\n</Card>\n\n"),
                1 => format!("<Callout kind=\"warn\">\nCareful with step {n}.\n</Callout>\n\n"),
                _ => format!("<Nope id=\"{n}\" />\n\n"),
            });
            while message.len() < (n + 1) * 20_000 / 50 {
                message.push_str("A paragraph line with `code` and *emphasis* to fill it.\n");
            }
            message.push('\n');
        }
        let mut streaming = Streaming::default();
        let started = std::time::Instant::now();
        let mut deltas = 0u32;
        for (at, _) in message.char_indices().skip(1) {
            std::hint::black_box(streaming.blocks(&message[..at]));
            deltas += 1;
        }
        let micros = started.elapsed().as_secs_f64() * 1e6 / f64::from(deltas);
        println!(
            "chat stream bench: {} bytes, 50 components, {deltas} deltas of one char, {micros:.1} us per delta",
            message.len()
        );
        let formulas = [
            "x^2 + y^2 = r^2",
            "\\frac{a}{b}",
            "\\sum_{i=1}^{n} i = \\frac{n(n+1)}{2}",
            "\\int_0^1 x\\,dx",
            "e^{i\\pi} + 1 = 0",
        ];
        let mut message = String::new();
        for (n, tex) in formulas.iter().cycle().enumerate() {
            if message.len() >= 20_000 {
                break;
            }
            message.push_str(&format!(
                "Step {n} uses ${tex}$ and costs $5 before the next one.\n\n"
            ));
            if n % 4 == 3 {
                message.push_str(&format!("\n$$\n{tex}\n$$\n\n"));
            }
        }
        let mut streaming = Streaming::default();
        let started = std::time::Instant::now();
        let mut deltas = 0u32;
        for (at, _) in message.char_indices().skip(1) {
            std::hint::black_box(streaming.blocks(&message[..at]));
            deltas += 1;
        }
        let micros = started.elapsed().as_secs_f64() * 1e6 / f64::from(deltas);
        println!(
            "chat math stream bench: {} bytes, {deltas} deltas of one char, {micros:.1} us per delta",
            message.len()
        );
        let mut problems = Vec::new();
        let Ok(layer) = parse_layer(
            "tofu-glass.json",
            include_str!("../../../../themes/tofu-glass.json"),
            &mut problems,
        ) else {
            panic!("built-in theme does not parse: {problems:?}");
        };
        let Ok(theme) = build(&[layer.layer], &mut problems) else {
            panic!("built-in theme does not build: {problems:?}");
        };
        let header: Vec<String> = (1..=12).map(|n| format!("col {n}")).collect();
        let tables = [
            "| Key | Value |\n| --- | --- |\n| mode | dark |".to_owned(),
            format!(
                "| {} |\n|{}\n| {} |",
                header.join(" | "),
                " --- |".repeat(12),
                header.join(" | ")
            ),
            "| Step | What |\n| --- | --- |\n| plan | The lead reads the ticket and every caller before it writes. |".to_owned(),
            "| L | C | R |\n| :--- | :---: | ---: |\n| a | 3 | 1.20 |".to_owned(),
            "| Command | Does |\n| --- | --- |\n| `cargo check -p desk_ui` | checks one crate |".to_owned(),
            "| A | B | C |\n| --- | --- | --- |\n| x |  | z |".to_owned(),
        ];
        let mut message = String::new();
        for (at, table) in tables.iter().cycle().enumerate() {
            if message.len() >= 40_000 {
                break;
            }
            message.push_str(table);
            message.push_str("\n\n");
            while message.len() < (at + 1) * 40_000 / tables.len() {
                message.push_str("A paragraph line with `code` and *emphasis* to fill it.\n");
            }
            message.push('\n');
        }
        let mut runs: Vec<f64> = (0..5)
            .map(|_| {
                let started = std::time::Instant::now();
                let body = markdown(&message);
                std::hint::black_box(lead("bench", "now", &body, &[], None, &theme));
                started.elapsed().as_secs_f64() * 1e3
            })
            .collect();
        runs.sort_by(f64::total_cmp);
        println!(
            "chat table bench: {} bytes, {} tables, lower and build element tree, median of 5 {:.2} ms, runs {runs:.2?}",
            message.len(),
            markdown(&message)
                .iter()
                .filter(|block| matches!(block, Block::Table { .. }))
                .count(),
            runs[2]
        );
    }

    #[test]
    fn go_fence_reaches_paint_indented_by_four_columns() {
        let painted = pieces(&markdown(
            "```go\nfunc main() {\n\tfmt.Println(1)\n\tif x {\n\t\ty()\n\t}\n}\n```\n\n- item\n\n  ```rust\n  fn a() {\n  \tb();\n  }\n  ```",
        ));
        for piece in &painted {
            println!("paint: {piece:?}");
        }
        assert_eq!(
            painted,
            [
                "func main() {\n    fmt.Println(1)\n    if x {\n        y()\n    }\n}",
                "item",
                "fn a() {\n    b();\n}",
            ]
        );
    }
}
