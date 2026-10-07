use std::cell::Cell;
use std::rc::Rc;

use gpui::{
    AnyElement, Div, ElementId, FontWeight, HighlightStyle, SharedString, Stateful, StyledText,
    TextRun, Transformation, div, font, prelude::*, px, radians, rgb_to_hsla, rgba,
};

use crate::components::avatar::spinner;
use crate::components::chip::{file_chip, mono};
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink, ring, tint};
use crate::components::size::{
    CALLS_GAP, CALLS_INDENT, CALLS_PAD_X, CALLS_PAD_Y, CAPTION_TEXT, CODE_FILL, FAIL_RING,
    FAIL_TINT, FONT_BODY, FONT_CHAT, FONT_SMALL, FONT_TAB, FONT_WHO, FOOT_GAP, FOOT_TEXT,
    HOVER_TEXT, LEAD_GAP, NOTE_GAP, RADIUS_BUBBLE, RADIUS_CHIP_SMALL, SHELL_TEXT, T1, T3, TOOL_GAP,
    YOU_GAP, YOU_MAX, YOU_PAD_BOTTOM, YOU_PAD_TOP, YOU_PAD_X,
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

pub struct Call {
    pub tool: SharedString,
    pub arg: SharedString,
    pub out: SharedString,
}

#[derive(Clone)]
pub enum Span {
    Plain(SharedString),
    Code(SharedString),
    Mention(SharedString),
}

pub enum Block {
    Para(Vec<Span>),
    Heading(SharedString),
    Bullets(Vec<SharedString>),
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
                .child(text.into()),
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

fn span_text(span: &Span) -> String {
    match span {
        Span::Plain(text) => text.to_string(),
        Span::Code(text) | Span::Mention(text) => format!("{CODE_PAD}{text}{CODE_PAD}"),
    }
}

pub fn prose(spans: &[Span], theme: &Theme) -> StyledText {
    let body = font(theme.word(WordToken::ShapeFont));
    let code = font(mono(theme));
    let pieces: Vec<String> = spans.iter().map(span_text).collect();
    let runs = spans
        .iter()
        .zip(&pieces)
        .map(|(span, piece)| {
            let (font, color, fill) = match span {
                Span::Plain(_) => (body.clone(), theme.color(ColorToken::TextBase), None),
                Span::Code(_) => (code.clone(), ink(theme, T1), Some(ink(theme, CODE_FILL))),
                Span::Mention(_) => (code.clone(), rgba(MENTION_TEXT), Some(rgba(MENTION_FILL))),
            };
            TextRun {
                len: piece.len(),
                font,
                color: rgb_to_hsla(color),
                background_color: fill.map(rgb_to_hsla),
                ..TextRun::default()
            }
        })
        .collect();
    StyledText::new(pieces.concat()).with_runs(runs)
}

fn block(block: &Block, first: bool, theme: &Theme) -> Div {
    match block {
        Block::Para(spans) => div()
            .when(!first, |para| para.mt(px(PARA_GAP)))
            .child(prose(spans, theme)),
        Block::Heading(text) => div()
            .mt(px(HEADING_GAP))
            .font_weight(FontWeight::SEMIBOLD)
            .child(text.clone()),
        Block::Bullets(items) => {
            div()
                .mt(px(BULLETS_GAP))
                .flex()
                .flex_col()
                .children(items.iter().map(|item| {
                    div()
                        .flex()
                        .child(div().flex_none().w(px(BULLET_INDENT)).child("•"))
                        .child(div().flex_1().min_w_0().child(item.clone()))
                }))
        }
    }
}

pub fn lead(time: impl Into<SharedString>, body: &[Block], theme: &Theme) -> Div {
    div()
        .mt(px(LEAD_GAP))
        .min_w_0()
        .text_size(px(FONT_CHAT))
        .line_height(px(CHAT_LINE))
        .text_color(theme.color(ColorToken::TextBase))
        .child(who(agent("lead", theme), time, false, theme))
        .children(
            body.iter()
                .enumerate()
                .map(|(at, part)| block(part, at == 0, theme)),
        )
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
