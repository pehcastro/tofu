use std::rc::Rc;

use desk_motion::tokens::{EASE_OUT, TOGGLE_MS};
use gpui::{
    Animation, AnimationExt, AnyElement, App, ClickEvent, Context, Div, ElementId, FocusHandle,
    FontWeight, KeyDownEvent, MouseDownEvent, Pixels, Rgba, SharedString, Stateful, Transformation,
    Window, canvas, div, prelude::*, px, radians, relative,
};

use crate::component::icon;
use crate::components::avatar::{Agent, AgentStatus, AvatarSize, avatar};
use crate::components::button::{ButtonKind, button};
use crate::components::card::{Header, caption, inner_card, shell};
use crate::components::chip::{Tone, mono, tabular, trace};
use crate::components::empty::empty_state;
use crate::components::glyph::Glyph;
use crate::components::list::{HoverVariant, RowGlide};
use crate::components::paint::{glyph, ink, ring, solid, tint};
use crate::components::scroll::ScrollArea;
use crate::components::sheet::Drawer;
use crate::components::size::{
    AVATAR_TINT_FEED, CAPTION_TEXT, CHIP, DIM_TEXT, FAIL_TINT, FCHIP_PAD, FONT_BODY, FONT_CHAT,
    FONT_KBD, FONT_SMALL, FONT_TAB, FONT_TREE, FONT_WHO, GRID_GAP, GRID_ON, GRID_PAD_X, GRID_ROW,
    GRID_WHEN, GRID_WHO, GROUP_PAD_BOTTOM, GROUP_PAD_TOP, HOVER, ICON_BUTTON, LINE_BODY, LINE_WHO,
    RADIUS_CHIP, RADIUS_CHIP_SMALL, RADIUS_LIST, RADIUS_TAB, ROW_GAP, ROW_PAD_X, SHELL_TEXT, T1,
    T2, T3,
};
use crate::components::width::Width;
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{HAIRLINE, ICON_SMALL, ICON_TINY};
use crate::theme::{ColorToken, Theme};

const OPEN_AT_START: [bool; 4] = [true, true, true, false];
const COUNT_FADE: f32 = 0.7;
const TRACE_FADE: f32 = 0.7;
const DONE_INK: f32 = 0.4;
const FEED_LIMIT: usize = 70;
const JUST_NOW: f32 = 3.0;
const FEW_MINUTES: f32 = 8.0;
const TWENTY_MINUTES: f32 = 20.0;
const MINUTES_PER_HOUR: f32 = 60.0;

const TILE_PAD_TOP: f32 = 2.0;
const TILE_PAD_BOTTOM: f32 = 8.0;
const COLUMNS_PAD_TOP: f32 = 8.0;
const COLUMNS_PAD_BOTTOM: f32 = 4.0;
const TILE_ROW_PAD_Y: f32 = 3.0;
const TILE_AVATAR_GAP: f32 = 8.0;
const LINE_TASK: f32 = 17.0;
const DOING_LEAST: f32 = 48.0;
const LINES_FROM: f32 = 560.0;
const FILES_FROM: f32 = 640.0;
const TOOLS_FROM: f32 = 760.0;
const GRID_LINES: f32 = 64.0;
const GRID_COUNT: f32 = 40.0;
const LINES_GAP: f32 = 6.0;

const DRAWER_PAD_X: f32 = 16.0;
const DRAWER_HEAD_PAD_BOTTOM: f32 = 10.0;
const DRAWER_HEAD_GAP: f32 = 12.0;
const DRAWER_HEAD_LINE: f32 = 19.0;
const DRAWER_NAME: f32 = 14.0;
const DRAWER_META_GAP: f32 = 8.0;
const DRAWER_PAD_BOTTOM: f32 = 14.0;
const THINK_PAD_X: f32 = 12.0;
const THINK_PAD_Y: f32 = 10.0;
const THINK_FILL: f32 = 0.035;
const THINK_CAPTION_GAP: f32 = 4.0;

const EVENT_GAP: f32 = 12.0;
const EVENT_PAD_Y: f32 = 9.0;
const EVENT_HEAD_GAP: f32 = 8.0;
const EVENT_AVATAR: f32 = 24.0;
const EVENT_AVATAR_FONT: f32 = 12.0;
const BODY_GAP_TIGHT: f32 = 5.0;
const BODY_GAP: f32 = 6.0;
const SPAWN_GAP: f32 = 3.0;
const WORD_GAP: f32 = 4.0;
const PILL_FILL: f32 = 0.05;
const PILL_RING: f32 = 0.05;
const TAG_PAD_X: f32 = 7.0;
const TAG_PAD_Y: f32 = 2.0;
const TAG_FILL: f32 = 0.04;
const TAG_GAP: f32 = 4.0;
const RESULT_GAP: f32 = 2.0;
const RESULT_DOT: f32 = 5.0;
const RESULT_DOT_FADE: f32 = 0.7;
const RESULT_ITEMS_GAP: f32 = 7.0;
const RESULTS_TOP: f32 = 4.0;
const BOX_FILL: f32 = 0.26;
const BOX_RING: f32 = 0.06;
const BOX_HEAD_FILL: f32 = 0.025;
const BOX_PAD_X: f32 = 12.0;
const BOX_HEAD_PAD_Y: f32 = 7.0;
const FETCH_PAD_Y: f32 = 9.0;
const FETCH_GAP: f32 = 10.0;
const HUNK_PAD_Y: f32 = 4.0;
const NUMBER_WIDTH: f32 = 42.0;
const NUMBER_GAP: f32 = 12.0;
const NUMBER_INK: f32 = 0.22;
const SIGN_WIDTH: f32 = 14.0;
const SIGN_FADE: f32 = 0.7;
const LINE_TINT: f32 = 0.1;
const ADDED_TEXT_MIX: f32 = 0.33;
const REMOVED_TEXT_MIX: f32 = 0.45;
const EXIT_PAD_X: f32 = 7.0;
const EXIT_PAD_Y: f32 = 2.0;
const EXIT_OK_FILL: f32 = 0.12;
const EXIT_FAIL_FILL: f32 = 0.14;
const OUTPUT_PAD_LEFT: f32 = 26.0;
const OUTPUT_PAD_BOTTOM: f32 = 8.0;
const NOTE_PAD_Y: f32 = 9.0;
const REPORT_PAD_Y: f32 = 10.0;
const LINE_NOTE: f32 = 21.0;
const ASK_FILL: f32 = 0.07;
const ASK_RING: f32 = 0.2;
const FAIL_RING: f32 = 0.22;
const REPORT_FILL: f32 = 0.04;
const REPORT_RING: f32 = 0.07;
const NOTE_AFTER_GAP: f32 = 2.0;
const REPORT_HEAD_GAP: f32 = 7.0;
const REPORT_HEAD_AFTER: f32 = 3.0;

const LIST_WIDTH: f32 = 340.0;
const LIST_LEAST: f32 = 132.0;
const FEED_LEAST: f32 = 320.0;
const LIST_PAD_X: f32 = 6.0;
const LIST_PAD_Y: f32 = 8.0;
const ALL_ROW: f32 = 32.0;
const SCREEN_GROUP_PAD_TOP: f32 = 14.0;
const SCREEN_GROUP_PAD_BOTTOM: f32 = 6.0;
const SCREEN_ROW_PAD_Y: f32 = 6.0;
const DIVIDER_SHADE: f32 = 0.35;
const FEED_PAD_X: f32 = 28.0;
const FEED_PAD_BOTTOM: f32 = 20.0;
const FEED_HEAD_PAD_TOP: f32 = 18.0;
const ALL_HEAD_PAD_BOTTOM: f32 = 2.0;
const AGENT_HEAD_PAD_BOTTOM: f32 = 6.0;
const ALL_HEAD_GAP: f32 = 10.0;
const AGENT_HEAD_GAP: f32 = 14.0;
const TITLE_ALL: f32 = 16.0;
const TITLE_AGENT: f32 = 17.0;
const TITLE_AGENT_LINE: f32 = 1.2;
const BUCKET_PAD_TOP: f32 = 18.0;
const BUCKET_PAD_BOTTOM: f32 = 4.0;

type AgentAction = Rc<dyn Fn(&Agent, &mut Window, &mut App)>;

#[derive(Clone)]
pub struct AgentLine {
    pub agent: Agent,
    pub task: SharedString,
    pub now: SharedString,
    pub time: SharedString,
    pub owns: SharedString,
    pub thinking: SharedString,
    pub added: usize,
    pub removed: usize,
    pub files: usize,
    pub tools: usize,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum DiffSign {
    Context,
    Added,
    Removed,
}

#[derive(Clone)]
pub struct DiffLine {
    pub number: u32,
    pub sign: DiffSign,
    pub text: SharedString,
}

#[derive(Clone)]
pub enum AgentStep {
    Spawn {
        task: SharedString,
        owns: SharedString,
    },
    Read {
        path: SharedString,
        lines: SharedString,
    },
    Grep {
        query: SharedString,
        scope: SharedString,
        hits: SharedString,
        files: Vec<SharedString>,
    },
    Web {
        query: SharedString,
        results: Vec<SharedString>,
    },
    Fetch {
        url: SharedString,
        title: SharedString,
        size: SharedString,
    },
    Edit {
        path: SharedString,
        added: u32,
        removed: u32,
        hunk: Vec<DiffLine>,
    },
    Bash {
        command: SharedString,
        exit: i32,
        output: Vec<SharedString>,
    },
    Browse {
        action: SharedString,
        target: SharedString,
        result: SharedString,
    },
    Ask {
        question: SharedString,
    },
    Fail {
        text: SharedString,
    },
    Report {
        text: SharedString,
        worked: SharedString,
    },
}

impl AgentStep {
    fn verb(&self) -> SharedString {
        match self {
            AgentStep::Spawn { .. } => "started".into(),
            AgentStep::Read { .. } => "read".into(),
            AgentStep::Grep { .. } => "searched the code".into(),
            AgentStep::Web { .. } => "searched the web".into(),
            AgentStep::Fetch { .. } => "read a page".into(),
            AgentStep::Edit { .. } => "edited".into(),
            AgentStep::Bash { .. } => "ran".into(),
            AgentStep::Browse { action, .. } => action.clone(),
            AgentStep::Ask { .. } => "asked the lead".into(),
            AgentStep::Fail { .. } => "failed".into(),
            AgentStep::Report { .. } => "finished".into(),
        }
    }
}

#[derive(Clone)]
pub struct AgentEvent {
    pub agent: Agent,
    pub minutes: f32,
    pub step: AgentStep,
}

impl AgentEvent {
    fn by(&self, agent: Agent) -> bool {
        self.agent == agent && !matches!(self.step, AgentStep::Spawn { .. })
    }
}

pub struct AgentBoard {
    pub lines: Vec<AgentLine>,
    pub events: Vec<AgentEvent>,
    pub attributed: bool,
}

impl AgentBoard {
    fn count(&self, status: AgentStatus) -> usize {
        self.lines
            .iter()
            .filter(|line| line.agent.status == status)
            .count()
    }

    fn find(&self, agent: Agent) -> Option<usize> {
        self.lines
            .iter()
            .position(|line| line.agent.kind == agent.kind && line.agent.instance == agent.instance)
    }

    pub fn live(&self) -> usize {
        self.lines.len() - self.count(AgentStatus::Finished)
    }

    fn groups(&self) -> impl Iterator<Item = (usize, AgentStatus, Vec<usize>)> + '_ {
        AgentStatus::ALL
            .into_iter()
            .enumerate()
            .map(|(at, status)| {
                let members = self
                    .lines
                    .iter()
                    .enumerate()
                    .filter(|(_, line)| line.agent.status == status)
                    .map(|(ix, _)| ix)
                    .collect::<Vec<_>>();
                (at, status, members)
            })
            .filter(|(_, _, members)| !members.is_empty())
    }

    fn feed(&self, agent: Option<Agent>) -> impl Iterator<Item = (usize, &AgentEvent)> {
        self.events
            .iter()
            .enumerate()
            .filter(move |(_, event)| agent.is_none_or(|agent| event.by(agent)))
            .take(FEED_LIMIT)
    }
}

pub fn ago(minutes: f32) -> SharedString {
    if minutes < 1.0 {
        "just now".into()
    } else if minutes < MINUTES_PER_HOUR {
        format!("{}m ago", minutes.round()).into()
    } else {
        format!("{}h ago", (minutes / MINUTES_PER_HOUR).round()).into()
    }
}

fn bucket(minutes: f32) -> &'static str {
    if minutes < JUST_NOW {
        "Just now"
    } else if minutes < FEW_MINUTES {
        "A few minutes ago"
    } else if minutes < TWENTY_MINUTES {
        "In the last twenty minutes"
    } else {
        "Earlier"
    }
}

fn summary(board: &AgentBoard, theme: &Theme) -> Div {
    let words = [
        (AgentStatus::Working, "working"),
        (AgentStatus::Asking, "asking"),
        (AgentStatus::Failed, "failed"),
        (AgentStatus::Finished, "finished"),
    ]
    .map(|(status, word)| format!("{} {word}", board.count(status)));
    div()
        .flex_none()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T3))
        .child(words.join(" · "))
}

fn status_word(status: AgentStatus) -> &'static str {
    match status {
        AgentStatus::Working => "working",
        AgentStatus::Asking => "asking the lead",
        AgentStatus::Failed => "failed",
        AgentStatus::Finished => "finished",
    }
}

fn status_color(status: AgentStatus, theme: &Theme) -> Rgba {
    match status {
        AgentStatus::Working => theme.color(ColorToken::StatusLive),
        AgentStatus::Asking => theme.color(ColorToken::StatusWarn),
        AgentStatus::Failed => Tone::Deleted.color(theme),
        AgentStatus::Finished => ink(theme, DONE_INK),
    }
}

fn mention_button(
    id: impl Into<ElementId>,
    agent: Agent,
    mention: &AgentAction,
    theme: &Theme,
) -> Stateful<Div> {
    let mention = mention.clone();
    trace(id, "Mention in chat".into(), theme)
        .opacity(TRACE_FADE)
        .on_click(move |_, window, cx| mention(&agent, window, cx))
}

fn group_head(
    id: impl Into<ElementId>,
    open: bool,
    label: Div,
    count: Div,
    theme: &Theme,
) -> Stateful<Div> {
    let turn = if open {
        std::f32::consts::FRAC_PI_2
    } else {
        0.0
    };
    div()
        .id(id)
        .w_full()
        .flex()
        .items_center()
        .gap_1p5()
        .cursor_pointer()
        .child(
            glyph(Glyph::Chevron, ICON_TINY, ink(theme, CAPTION_TEXT))
                .with_transformation(Transformation::rotate(radians(turn))),
        )
        .child(label)
        .child(count)
}

fn count_caption(count: usize, theme: &Theme) -> Div {
    caption(count.to_string(), theme).font_family(mono(theme))
}

fn disc(initial: SharedString, color: Rgba) -> Div {
    div()
        .flex_none()
        .size(px(EVENT_AVATAR))
        .flex()
        .items_center()
        .justify_center()
        .rounded_full()
        .bg(tint(color, AVATAR_TINT_FEED))
        .text_color(color)
        .font_weight(FontWeight::SEMIBOLD)
        .text_size(px(EVENT_AVATAR_FONT))
        .line_height(px(EVENT_AVATAR))
        .child(initial)
}

fn pill(theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap_1p5()
        .h(px(CHIP))
        .px(px(FCHIP_PAD))
        .min_w_0()
        .max_w_full()
        .rounded(px(RADIUS_CHIP))
        .bg(ink(theme, PILL_FILL))
        .shadow(vec![ring(ink(theme, PILL_RING))])
        .text_size(px(FONT_TAB))
}

fn mono_text(text: SharedString, size: f32, theme: &Theme) -> Div {
    div()
        .min_w_0()
        .truncate()
        .font_family(mono(theme))
        .text_size(px(size))
        .child(text)
}

fn framed(fill: Rgba, edge: Rgba) -> Div {
    div()
        .mt(px(BODY_GAP))
        .rounded(px(RADIUS_LIST))
        .overflow_hidden()
        .bg(fill)
        .shadow(vec![ring(edge)])
}

fn code_box(theme: &Theme) -> Div {
    framed(
        tint(theme.color(ColorToken::Shadow), BOX_FILL),
        ink(theme, BOX_RING),
    )
}

fn box_head(theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(EVENT_HEAD_GAP))
        .px(px(BOX_PAD_X))
        .py(px(BOX_HEAD_PAD_Y))
        .text_size(px(FONT_TAB))
        .child(
            div()
                .font_family(mono(theme))
                .text_color(ink(theme, T3))
                .child("$"),
        )
}

fn diff_line(line: &DiffLine, theme: &Theme) -> Div {
    let strong = ink(theme, 1.0);
    let (sign, fill, text) = match line.sign {
        DiffSign::Context => (" ", None, ink(theme, DIM_TEXT)),
        DiffSign::Added => {
            let added = theme.color(ColorToken::GitAdded);
            let text = solid(tint(added, ADDED_TEXT_MIX), strong);
            ("+", Some(tint(added, LINE_TINT)), text)
        }
        DiffSign::Removed => {
            let removed = theme.color(ColorToken::GitDeleted);
            let text = solid(tint(removed, REMOVED_TEXT_MIX), strong);
            ("-", Some(tint(removed, LINE_TINT)), text)
        }
    };
    div()
        .flex()
        .whitespace_nowrap()
        .text_color(text)
        .when_some(fill, |row, fill| row.bg(fill))
        .child(
            div()
                .w(px(NUMBER_WIDTH))
                .flex_none()
                .flex()
                .justify_end()
                .pr(px(NUMBER_GAP))
                .text_color(ink(theme, NUMBER_INK))
                .child(line.number.to_string()),
        )
        .child(
            div()
                .w(px(SIGN_WIDTH))
                .flex_none()
                .opacity(SIGN_FADE)
                .child(sign),
        )
        .child(div().min_w_0().truncate().child(line.text.clone()))
}

fn step_body(step: &AgentStep, theme: &Theme) -> Option<Div> {
    let t2 = ink(theme, T2);
    let t3 = ink(theme, T3);
    let body =
        match step {
            AgentStep::Spawn { .. } => return None,
            AgentStep::Read { path, lines } => div().mt(px(BODY_GAP_TIGHT)).flex().child(
                pill(theme)
                    .child(glyph(Glyph::File, ICON_SMALL, t3))
                    .child(mono_text(path.clone(), FONT_SMALL, theme))
                    .child(
                        div()
                            .flex_none()
                            .text_size(px(FONT_WHO))
                            .text_color(t3)
                            .child(lines.clone()),
                    ),
            ),
            AgentStep::Grep {
                query,
                scope,
                hits,
                files,
            } => div()
                .mt(px(BODY_GAP_TIGHT))
                .flex()
                .flex_col()
                .gap(px(BODY_GAP))
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(EVENT_HEAD_GAP))
                        .text_size(px(FONT_TAB))
                        .child(
                            pill(theme)
                                .child(icon(Icon::Search, ICON_SMALL, t3))
                                .child(mono_text(query.clone(), FONT_SMALL, theme)),
                        )
                        .child(
                            div()
                                .flex()
                                .items_baseline()
                                .min_w_0()
                                .text_color(t3)
                                .child("in\u{a0}")
                                .child(mono_text(scope.clone(), FONT_WHO, theme))
                                .child(format!("\u{a0}· {hits}")),
                        ),
                )
                .when(!files.is_empty(), |body| {
                    body.child(div().flex().flex_wrap().gap(px(TAG_GAP)).children(
                        files.iter().map(|file| {
                            div()
                                .px(px(TAG_PAD_X))
                                .py(px(TAG_PAD_Y))
                                .rounded(px(RADIUS_CHIP_SMALL))
                                .bg(ink(theme, TAG_FILL))
                                .font_family(mono(theme))
                                .text_size(px(FONT_WHO))
                                .text_color(t2)
                                .child(file.clone())
                        }),
                    ))
                }),
            AgentStep::Web { query, results } => {
                let link = theme.color(ColorToken::StatusAccent);
                div()
                    .mt(px(BODY_GAP_TIGHT))
                    .text_size(px(FONT_BODY))
                    .line_height(px(LINE_BODY))
                    .child(div().text_color(t2).child(format!("\"{query}\"")))
                    .when(!results.is_empty(), |body| {
                        body.child(
                            div()
                                .mt(px(RESULTS_TOP))
                                .flex()
                                .flex_col()
                                .gap(px(RESULT_GAP))
                                .children(results.iter().map(|result| {
                                    div()
                                        .flex()
                                        .items_center()
                                        .gap(px(RESULT_ITEMS_GAP))
                                        .text_size(px(FONT_TAB))
                                        .child(
                                            div()
                                                .flex_none()
                                                .size(px(RESULT_DOT))
                                                .rounded_full()
                                                .bg(tint(link, RESULT_DOT_FADE)),
                                        )
                                        .child(
                                            div()
                                                .min_w_0()
                                                .truncate()
                                                .text_color(link)
                                                .child(result.clone()),
                                        )
                                })),
                        )
                    })
            }
            AgentStep::Fetch { url, title, size } => code_box(theme)
                .flex()
                .items_center()
                .gap(px(FETCH_GAP))
                .px(px(BOX_PAD_X))
                .py(px(FETCH_PAD_Y))
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .line_height(px(LINE_WHO))
                        .when(!title.is_empty(), |text| {
                            text.child(div().text_size(px(FONT_BODY)).child(title.clone()))
                        })
                        .child(mono_text(url.clone(), FONT_WHO, theme).text_color(t3)),
                )
                .child(
                    div()
                        .flex_none()
                        .text_size(px(FONT_WHO))
                        .text_color(t3)
                        .child(size.clone()),
                ),
            AgentStep::Edit {
                path,
                added,
                removed,
                hunk,
            } => code_box(theme)
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(EVENT_HEAD_GAP))
                        .px(px(BOX_PAD_X))
                        .py(px(BOX_HEAD_PAD_Y))
                        .text_size(px(FONT_TAB))
                        .bg(ink(theme, BOX_HEAD_FILL))
                        .child(glyph(Glyph::File, ICON_SMALL, t3))
                        .child(mono_text(path.clone(), FONT_SMALL, theme).flex_1())
                        .when(*added > 0, |head| {
                            head.child(
                                mono_text(format!("+{added}").into(), FONT_WHO, theme)
                                    .text_color(Tone::Added.color(theme)),
                            )
                        })
                        .when(*removed > 0, |head| {
                            head.child(
                                mono_text(format!("-{removed}").into(), FONT_WHO, theme)
                                    .text_color(Tone::Deleted.color(theme)),
                            )
                        }),
                )
                .child(
                    div()
                        .py(px(HUNK_PAD_Y))
                        .font_family(mono(theme))
                        .text_size(px(FONT_SMALL))
                        .line_height(px(LINE_BODY))
                        .children(hunk.iter().map(|line| diff_line(line, theme))),
                ),
            AgentStep::Bash {
                command,
                exit,
                output,
            } => {
                let (fill, text) = if *exit == 0 {
                    let live = theme.color(ColorToken::StatusLive);
                    (tint(live, EXIT_OK_FILL), live)
                } else {
                    let failed = Tone::Deleted.color(theme);
                    (tint(failed, EXIT_FAIL_FILL), failed)
                };
                code_box(theme)
                    .child(
                        box_head(theme)
                            .child(mono_text(command.clone(), FONT_SMALL, theme).flex_1())
                            .child(
                                div()
                                    .flex_none()
                                    .px(px(EXIT_PAD_X))
                                    .py(px(EXIT_PAD_Y))
                                    .rounded(px(RADIUS_CHIP_SMALL))
                                    .bg(fill)
                                    .text_color(text)
                                    .font_family(mono(theme))
                                    .text_size(px(FONT_KBD))
                                    .line_height(relative(1.0))
                                    .child(format!("exit {exit}")),
                            ),
                    )
                    .child(
                        div()
                            .pl(px(OUTPUT_PAD_LEFT))
                            .pr(px(BOX_PAD_X))
                            .pb(px(OUTPUT_PAD_BOTTOM))
                            .font_family(mono(theme))
                            .text_size(px(FONT_WHO))
                            .line_height(px(LINE_WHO))
                            .text_color(ink(theme, SHELL_TEXT))
                            .children(output.iter().map(|line| {
                                div().whitespace_nowrap().truncate().child(line.clone())
                            })),
                    )
            }
            AgentStep::Browse { target, result, .. } => div()
                .mt(px(BODY_GAP_TIGHT))
                .flex()
                .flex_wrap()
                .items_center()
                .gap(px(EVENT_HEAD_GAP))
                .text_size(px(FONT_TAB))
                .child(pill(theme).child(mono_text(target.clone(), FONT_SMALL, theme)))
                .child(div().text_color(t3).child("\u{2192}"))
                .child(div().text_color(t2).child(result.clone())),
            AgentStep::Ask { question } => {
                let warn = theme.color(ColorToken::StatusWarn);
                framed(tint(warn, ASK_FILL), tint(warn, ASK_RING))
                    .px(px(BOX_PAD_X))
                    .py(px(NOTE_PAD_Y))
                    .text_size(px(FONT_TREE))
                    .line_height(px(LINE_NOTE))
                    .child(question.clone())
                    .child(
                        div()
                            .mt(px(NOTE_AFTER_GAP))
                            .text_size(px(FONT_SMALL))
                            .text_color(t3)
                            .child("the lead has not answered yet"),
                    )
            }
            AgentStep::Fail { text } => {
                let failed = Tone::Deleted.color(theme);
                framed(tint(failed, FAIL_TINT), tint(failed, FAIL_RING))
                    .px(px(BOX_PAD_X))
                    .py(px(NOTE_PAD_Y))
                    .font_family(mono(theme))
                    .text_size(px(FONT_BODY))
                    .line_height(px(LINE_BODY))
                    .child(text.clone())
            }
            AgentStep::Report { text, worked } => {
                let added = Tone::Added.color(theme);
                framed(ink(theme, REPORT_FILL), ink(theme, REPORT_RING))
                    .px(px(BOX_PAD_X))
                    .py(px(REPORT_PAD_Y))
                    .text_size(px(FONT_TREE))
                    .line_height(px(LINE_NOTE))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap(px(REPORT_HEAD_GAP))
                            .mb(px(REPORT_HEAD_AFTER))
                            .text_size(px(FONT_SMALL))
                            .child(glyph(Glyph::Check, ICON_SMALL, added))
                            .child(div().text_color(added).child("done"))
                            .child(div().text_color(t3).child(format!("· {worked}"))),
                    )
                    .child(text.clone())
            }
        };
    Some(body)
}

fn event_row(ix: usize, event: &AgentEvent, mention: &AgentAction, theme: &Theme) -> Div {
    let agent = event.agent;
    let spawned = match &event.step {
        AgentStep::Spawn { task, owns } => Some((task.clone(), owns.clone())),
        _ => None,
    };
    let (name, color) = if spawned.is_some() {
        (
            SharedString::from("lead"),
            theme.color(ColorToken::TextBase),
        )
    } else {
        (agent.name(), agent.kind.color(theme))
    };
    let initial: SharedString = name
        .chars()
        .next()
        .map(|first| first.to_uppercase().to_string())
        .unwrap_or_default()
        .into();
    let t3 = ink(theme, T3);
    div()
        .flex()
        .gap(px(EVENT_GAP))
        .py(px(EVENT_PAD_Y))
        .child(disc(initial, color))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .child(
                    div()
                        .flex()
                        .items_baseline()
                        .gap(px(EVENT_HEAD_GAP))
                        .text_size(px(FONT_BODY))
                        .line_height(px(LINE_BODY))
                        .child(
                            div()
                                .flex_none()
                                .font_weight(FontWeight::SEMIBOLD)
                                .text_color(color)
                                .child(name),
                        )
                        .child(
                            div()
                                .min_w_0()
                                .truncate()
                                .text_color(t3)
                                .child(event.step.verb()),
                        )
                        .child(div().flex_1())
                        .child(
                            div()
                                .flex_none()
                                .text_size(px(FONT_WHO))
                                .text_color(t3)
                                .child(ago(event.minutes)),
                        )
                        .child(
                            mention_button(("event-mention", ix), agent, mention, theme)
                                .self_center(),
                        ),
                )
                .when_some(spawned, |body, (task, owns)| {
                    body.child(
                        div()
                            .mt(px(SPAWN_GAP))
                            .text_size(px(FONT_TREE))
                            .line_height(px(LINE_NOTE))
                            .child(
                                div()
                                    .flex()
                                    .flex_wrap()
                                    .items_baseline()
                                    .gap_x(px(WORD_GAP))
                                    .child(
                                        div()
                                            .font_weight(FontWeight::SEMIBOLD)
                                            .text_color(agent.kind.color(theme))
                                            .child(agent.name()),
                                    )
                                    .child(div().text_color(ink(theme, T2)).child(task))
                                    .when(!owns.is_empty(), |words| {
                                        words.child(
                                            div()
                                                .flex()
                                                .items_baseline()
                                                .text_size(px(FONT_SMALL))
                                                .text_color(t3)
                                                .child("·\u{a0}owns\u{a0}")
                                                .child(mono_text(owns, FONT_WHO, theme)),
                                        )
                                    }),
                            ),
                    )
                })
                .children(step_body(&event.step, theme)),
        )
}

fn grid(id: impl Into<ElementId>) -> Stateful<Div> {
    div()
        .id(id)
        .w_full()
        .flex()
        .items_center()
        .gap(px(GRID_GAP))
        .px(px(GRID_PAD_X))
}

fn who(fit: Fit) -> Div {
    if fit.doing {
        div().w(px(GRID_WHO)).flex_shrink_1().min_w_0()
    } else {
        div().flex_1().min_w_0()
    }
}

fn doing() -> Div {
    div().flex_1().min_w_0()
}

fn when(fit: Fit) -> Div {
    if fit.doing {
        div().w(px(GRID_WHEN)).flex_none().min_w_0().truncate()
    } else {
        div().flex_none().flex().justify_end().whitespace_nowrap()
    }
}

fn figure(width: f32) -> Div {
    div()
        .w(px(width))
        .flex_none()
        .flex()
        .justify_end()
        .gap(px(LINES_GAP))
        .whitespace_nowrap()
}

fn first_line(text: &SharedString) -> SharedString {
    text.lines().next().unwrap_or_default().to_owned().into()
}

fn tile_row(
    ix: usize,
    line: &AgentLine,
    (picked, fit): (bool, Fit),
    theme: &Theme,
) -> Stateful<Div> {
    let agent = &line.agent;
    let t3 = ink(theme, T3);
    let count = |value: usize| {
        figure(GRID_COUNT)
            .text_size(px(FONT_WHO))
            .font_features(tabular())
            .text_color(t3)
            .child(value.to_string())
    };
    grid(("tile-row", ix))
        .min_h(px(GRID_ROW))
        .py(px(TILE_ROW_PAD_Y))
        .cursor_pointer()
        .text_size(px(FONT_TAB))
        .when(picked, |row| row.bg(ink(theme, GRID_ON)))
        .child(
            who(fit)
                .flex()
                .items_center()
                .gap(px(TILE_AVATAR_GAP))
                .whitespace_nowrap()
                .child(avatar(("tile-avatar", ix), agent, AvatarSize::Feed, theme))
                .child(
                    div()
                        .min_w_0()
                        .truncate()
                        .text_color(agent.kind.color(theme))
                        .child(agent.name()),
                ),
        )
        .when(fit.doing, |row| {
            row.child(
                doing()
                    .line_height(px(LINE_TASK))
                    .child(div().truncate().child(first_line(&line.task)))
                    .child(
                        div()
                            .truncate()
                            .text_size(px(FONT_WHO))
                            .text_color(t3)
                            .child(first_line(&line.now)),
                    ),
            )
        })
        .when(fit.lines, |row| {
            row.child(
                figure(GRID_LINES)
                    .text_size(px(FONT_WHO))
                    .font_features(tabular())
                    .child(
                        div()
                            .text_color(Tone::Added.color(theme))
                            .child(format!("+{}", line.added)),
                    )
                    .child(
                        div()
                            .text_color(Tone::Deleted.color(theme))
                            .child(format!("-{}", line.removed)),
                    ),
            )
        })
        .when(fit.files, |row| row.child(count(line.files)))
        .when(fit.tools, |row| row.child(count(line.tools)))
        .child(
            when(fit)
                .text_size(px(FONT_WHO))
                .font_features(tabular())
                .text_color(t3)
                .child(line.time.clone()),
        )
}

fn columns(fit: Fit, theme: &Theme) -> Stateful<Div> {
    grid("tile-columns")
        .pt(px(COLUMNS_PAD_TOP))
        .pb(px(COLUMNS_PAD_BOTTOM))
        .whitespace_nowrap()
        .child(who(fit).child(caption("agent", theme)))
        .when(fit.doing, |row| {
            row.child(doing().child(caption("doing", theme).truncate()))
        })
        .when(fit.lines, |row| {
            row.child(figure(GRID_LINES).child(caption("lines", theme)))
        })
        .when(fit.files, |row| {
            row.child(figure(GRID_COUNT).child(caption("files", theme)))
        })
        .when(fit.tools, |row| {
            row.child(figure(GRID_COUNT).child(caption("tools", theme)))
        })
        .child(when(fit).child(caption("time", theme)))
}

#[derive(Clone, Copy)]
struct Fit {
    doing: bool,
    lines: bool,
    files: bool,
    tools: bool,
}

impl Fit {
    fn of(tile: Option<Pixels>, attributed: bool) -> Self {
        let board = tile.map_or(f32::INFINITY, |width| width.as_f32());
        Fit {
            doing: board - 2.0 * GRID_PAD_X - 2.0 * GRID_GAP - GRID_WHO - GRID_WHEN >= DOING_LEAST,
            lines: attributed && board >= LINES_FROM,
            files: attributed && board >= FILES_FROM,
            tools: attributed && board >= TOOLS_FROM,
        }
    }
}

pub struct AgentTile {
    board: Rc<AgentBoard>,
    open: bool,
    shown: usize,
    open_groups: [bool; 4],
    focus: FocusHandle,
    mention: AgentAction,
    expand: AgentAction,
}

impl AgentTile {
    pub fn new(
        board: Rc<AgentBoard>,
        mention: impl Fn(&Agent, &mut Window, &mut App) + 'static,
        expand: impl Fn(&Agent, &mut Window, &mut App) + 'static,
        cx: &mut Context<Self>,
    ) -> Self {
        AgentTile {
            board,
            open: false,
            shown: 0,
            open_groups: OPEN_AT_START,
            focus: cx.focus_handle(),
            mention: Rc::new(mention),
            expand: Rc::new(expand),
        }
    }

    pub fn set_board(&mut self, board: impl Into<Rc<AgentBoard>>, cx: &mut Context<Self>) {
        let board = board.into();
        let shown = self.board.lines.get(self.shown).map(|line| line.agent);
        match shown.and_then(|agent| board.find(agent)) {
            Some(at) => self.shown = at,
            None => self.open = false,
        }
        self.board = board;
        cx.notify();
    }

    fn show(&mut self, ix: usize, window: &mut Window, cx: &mut Context<Self>) {
        self.shown = ix;
        self.open = true;
        self.focus.focus(window, cx);
        cx.notify();
    }

    fn close(&mut self, cx: &mut Context<Self>) {
        if self.open {
            self.open = false;
            cx.notify();
        }
    }

    fn toggle(&mut self, at: usize, cx: &mut Context<Self>) {
        if let Some(open) = self.open_groups.get_mut(at) {
            *open = !*open;
            cx.notify();
        }
    }

    fn key(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        if event.keystroke.key == "escape" && self.open {
            self.close(cx);
            cx.stop_propagation();
        }
    }

    fn drawer_head(&self, line: &AgentLine, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let agent = line.agent;
        let expand = self.expand.clone();
        let t3 = ink(theme, T3);
        let hover = ink(theme, HOVER);
        div()
            .flex_none()
            .flex()
            .items_center()
            .gap(px(DRAWER_HEAD_GAP))
            .px(px(DRAWER_PAD_X))
            .pb(px(DRAWER_HEAD_PAD_BOTTOM))
            .child(avatar("drawer-avatar", &agent, AvatarSize::Large, theme))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .line_height(px(DRAWER_HEAD_LINE))
                    .child(
                        div()
                            .flex()
                            .items_baseline()
                            .gap(px(DRAWER_META_GAP))
                            .text_size(px(FONT_SMALL))
                            .child(
                                div()
                                    .text_size(px(DRAWER_NAME))
                                    .font_weight(FontWeight::SEMIBOLD)
                                    .text_color(agent.kind.color(theme))
                                    .child(agent.name()),
                            )
                            .child(
                                div()
                                    .text_color(status_color(agent.status, theme))
                                    .child(status_word(agent.status)),
                            )
                            .child(
                                div()
                                    .min_w_0()
                                    .truncate()
                                    .text_color(t3)
                                    .child(format!("· {}", line.time)),
                            ),
                    )
                    .child(
                        div()
                            .truncate()
                            .text_size(px(FONT_BODY))
                            .child(line.task.clone()),
                    ),
            )
            .child(mention_button(
                "drawer-mention",
                agent,
                &self.mention,
                theme,
            ))
            .child(
                button("drawer-expand", "Expand", None, ButtonKind::Text, theme)
                    .text_size(px(FONT_SMALL))
                    .on_click(cx.listener(move |tile, _: &ClickEvent, window, cx| {
                        tile.close(cx);
                        expand(&agent, window, cx);
                    })),
            )
            .child(
                div()
                    .id("drawer-close")
                    .aria_label("Close")
                    .flex()
                    .flex_none()
                    .items_center()
                    .justify_center()
                    .size(px(ICON_BUTTON))
                    .rounded(px(RADIUS_CHIP))
                    .cursor_pointer()
                    .hover(move |style| style.bg(hover))
                    .on_click(cx.listener(|tile, _: &ClickEvent, _, cx| tile.close(cx)))
                    .child(icon(Icon::Close, ICON_TINY, ink(theme, CAPTION_TEXT))),
            )
    }

    fn drawer_body(&self, line: &AgentLine, theme: &Theme) -> impl IntoElement {
        let events = self
            .board
            .feed(Some(line.agent))
            .map(|(ix, event)| event_row(ix, event, &self.mention, theme))
            .collect::<Vec<_>>();
        ScrollArea::new(("drawer-scroll", self.shown)).child(
            div()
                .px(px(DRAWER_PAD_X))
                .pb(px(DRAWER_PAD_BOTTOM))
                .child(
                    div()
                        .px(px(THINK_PAD_X))
                        .py(px(THINK_PAD_Y))
                        .rounded(px(RADIUS_LIST))
                        .bg(ink(theme, THINK_FILL))
                        .text_size(px(FONT_BODY))
                        .line_height(px(LINE_BODY))
                        .text_color(ink(theme, T2))
                        .child(caption("thinking", theme).mb(px(THINK_CAPTION_GAP)))
                        .child(div().italic().child(line.thinking.clone())),
                )
                .children(events),
        )
    }
}

impl Render for AgentTile {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let glide = RowGlide::new("tile-glide", HoverVariant::Glide, &theme, window, cx);
        let width = Width::of(format!("tile-width-{}", cx.entity_id()), window, cx);
        let fit = Fit::of(width.get(cx), self.board.attributed);
        let mut table = div()
            .flex()
            .flex_col()
            .min_w_0()
            .pt(px(TILE_PAD_TOP))
            .pb(px(TILE_PAD_BOTTOM))
            .child(columns(fit, &theme));
        let mut slot = 0;
        for (at, status, members) in self.board.groups() {
            let open = self.open_groups.get(at).copied().unwrap_or(false);
            let head = group_head(
                ("tile-group", at),
                open,
                caption(status.group(), &theme),
                count_caption(members.len(), &theme).opacity(COUNT_FADE),
                &theme,
            )
            .px(px(GRID_PAD_X))
            .pt(px(GROUP_PAD_TOP))
            .pb(px(GROUP_PAD_BOTTOM));
            table = table.child(
                glide
                    .row(slot, head)
                    .on_click(cx.listener(move |tile, _: &ClickEvent, _, cx| tile.toggle(at, cx))),
            );
            slot += 1;
            if !open {
                continue;
            }
            for ix in members {
                let Some(line) = self.board.lines.get(ix) else {
                    continue;
                };
                let row = tile_row(ix, line, (self.open && self.shown == ix, fit), &theme)
                    .on_click(cx.listener(move |tile, _: &ClickEvent, window, cx| {
                        tile.show(ix, window, cx)
                    }));
                table = table.child(glide.row(slot, row));
                slot += 1;
            }
        }
        let close = cx.listener(|tile, _: &(), _, cx| tile.close(cx));
        let shown = self.board.lines.get(self.shown).cloned();
        let drawer = Drawer::new("tile-drawer")
            .open(self.open && shown.is_some())
            .on_close(move |window, cx| close(&(), window, cx))
            .when_some(shown, |drawer, line| {
                drawer
                    .child(self.drawer_head(&line, &theme, cx))
                    .child(self.drawer_body(&line, &theme))
            });
        div()
            .id("agents-tile")
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::key))
            .on_mouse_down_out(cx.listener(|tile, _: &MouseDownEvent, _, cx| tile.close(cx)))
            .relative()
            .size_full()
            .min_h_0()
            .flex()
            .flex_col()
            .text_color(ink(&theme, T1))
            .child(
                canvas(
                    move |bounds, window, cx| {
                        if width.get(cx) != Some(bounds.size.width) {
                            width.record(bounds.size.width, cx);
                            window.request_animation_frame();
                        }
                    },
                    |_, _, _, _| {},
                )
                .absolute()
                .top_0()
                .left_0()
                .size_full(),
            )
            .child(ScrollArea::new("tile-scroll").child(glide.frame("tile-rows", table)))
            .child(drawer)
    }
}

pub struct AgentScreen {
    board: Rc<AgentBoard>,
    picked: Option<usize>,
    open_groups: [bool; 4],
    mention: AgentAction,
}

impl AgentScreen {
    pub fn new(
        board: Rc<AgentBoard>,
        mention: impl Fn(&Agent, &mut Window, &mut App) + 'static,
    ) -> Self {
        AgentScreen {
            board,
            picked: None,
            open_groups: OPEN_AT_START,
            mention: Rc::new(mention),
        }
    }

    pub fn set_board(&mut self, board: impl Into<Rc<AgentBoard>>, cx: &mut Context<Self>) {
        let board = board.into();
        self.picked = self
            .picked
            .and_then(|ix| self.board.lines.get(ix))
            .and_then(|line| board.find(line.agent));
        self.board = board;
        cx.notify();
    }

    pub fn show(&mut self, agent: Option<Agent>, cx: &mut Context<Self>) {
        let picked = agent.and_then(|agent| self.board.find(agent));
        let group = picked
            .and_then(|ix| self.board.lines.get(ix))
            .and_then(|line| {
                AgentStatus::ALL
                    .iter()
                    .position(|status| *status == line.agent.status)
            })
            .and_then(|at| self.open_groups.get_mut(at));
        if let Some(open) = group {
            *open = true;
        }
        self.pick(picked, cx);
    }

    fn pick(&mut self, picked: Option<usize>, cx: &mut Context<Self>) {
        if self.picked != picked {
            self.picked = picked;
            cx.notify();
        }
    }

    fn toggle(&mut self, at: usize, cx: &mut Context<Self>) {
        if let Some(open) = self.open_groups.get_mut(at) {
            *open = !*open;
            cx.notify();
        }
    }

    fn list(&self, theme: &Theme, window: &mut Window, cx: &mut Context<Self>) -> Div {
        let glide = RowGlide::new("screen-glide", HoverVariant::Glide, theme, window, cx);
        let all = div()
            .id("screen-all")
            .flex()
            .items_center()
            .gap(px(ROW_GAP))
            .h(px(ALL_ROW))
            .px(px(ROW_PAD_X))
            .rounded(px(RADIUS_TAB))
            .cursor_pointer()
            .text_size(px(FONT_BODY))
            .when(self.picked.is_none(), |row| row.bg(ink(theme, GRID_ON)))
            .child(
                div()
                    .flex_1()
                    .font_weight(FontWeight::SEMIBOLD)
                    .child("All activity"),
            )
            .child(
                div()
                    .font_family(mono(theme))
                    .text_size(px(FONT_WHO))
                    .text_color(ink(theme, T3))
                    .child(self.board.events.len().to_string()),
            );
        let mut rows = div().flex().flex_col().min_w_0().child(
            glide
                .row(0, all)
                .on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.pick(None, cx))),
        );
        let mut slot = 1;
        for (at, status, members) in self.board.groups() {
            let open = self.open_groups.get(at).copied().unwrap_or(false);
            let head = group_head(
                ("screen-group", at),
                open,
                caption(status.group(), theme).flex_1(),
                count_caption(members.len(), theme),
                theme,
            )
            .px(px(ROW_PAD_X))
            .pt(px(SCREEN_GROUP_PAD_TOP))
            .pb(px(SCREEN_GROUP_PAD_BOTTOM));
            rows =
                rows.child(glide.row(slot, head).on_click(
                    cx.listener(move |screen, _: &ClickEvent, _, cx| screen.toggle(at, cx)),
                ));
            slot += 1;
            if !open {
                continue;
            }
            for ix in members {
                let Some(line) = self.board.lines.get(ix) else {
                    continue;
                };
                let row = screen_row(ix, line, self.picked == Some(ix), theme).on_click(
                    cx.listener(move |screen, _: &ClickEvent, _, cx| screen.pick(Some(ix), cx)),
                );
                rows = rows.child(glide.row(slot, row));
                slot += 1;
            }
        }
        div()
            .flex_basis(px(LIST_WIDTH))
            .flex_shrink_1()
            .min_w(px(LIST_LEAST))
            .h_full()
            .flex()
            .flex_col()
            .child(
                ScrollArea::new("screen-list").child(
                    div()
                        .px(px(LIST_PAD_X))
                        .py(px(LIST_PAD_Y))
                        .child(glide.frame("screen-rows", rows)),
                ),
            )
    }

    fn head(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let t3 = ink(theme, T3);
        let Some(line) = self.picked.and_then(|ix| self.board.lines.get(ix)) else {
            return div()
                .flex()
                .items_baseline()
                .gap(px(ALL_HEAD_GAP))
                .pt(px(FEED_HEAD_PAD_TOP))
                .pb(px(ALL_HEAD_PAD_BOTTOM))
                .child(
                    div()
                        .text_size(px(TITLE_ALL))
                        .line_height(relative(1.0))
                        .font_weight(FontWeight::SEMIBOLD)
                        .child("All activity"),
                )
                .child(
                    div()
                        .min_w_0()
                        .truncate()
                        .text_size(px(FONT_TAB))
                        .text_color(t3)
                        .child("every sub-agent, newest first"),
                );
        };
        let agent = line.agent;
        div()
            .flex()
            .items_center()
            .gap(px(AGENT_HEAD_GAP))
            .pt(px(FEED_HEAD_PAD_TOP))
            .pb(px(AGENT_HEAD_PAD_BOTTOM))
            .child(avatar(
                "screen-head-avatar",
                &agent,
                AvatarSize::Large,
                theme,
            ))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .line_height(px(LINE_BODY))
                    .child(
                        div()
                            .flex()
                            .flex_wrap()
                            .items_baseline()
                            .gap_x(px(ALL_HEAD_GAP))
                            .text_size(px(FONT_TAB))
                            .child(
                                div()
                                    .text_size(px(TITLE_AGENT))
                                    .line_height(relative(TITLE_AGENT_LINE))
                                    .font_weight(FontWeight::SEMIBOLD)
                                    .text_color(agent.kind.color(theme))
                                    .child(agent.name()),
                            )
                            .child(
                                div()
                                    .text_color(status_color(agent.status, theme))
                                    .child(status_word(agent.status)),
                            )
                            .child(
                                div()
                                    .flex()
                                    .items_baseline()
                                    .min_w_0()
                                    .text_color(t3)
                                    .child(line.time.clone())
                                    .when(!line.owns.is_empty(), |time| {
                                        time.child("\u{a0}· owns\u{a0}").child(mono_text(
                                            line.owns.clone(),
                                            FONT_SMALL,
                                            theme,
                                        ))
                                    }),
                            ),
                    )
                    .child(
                        div()
                            .truncate()
                            .text_size(px(FONT_CHAT))
                            .child(line.task.clone()),
                    ),
            )
            .child(mention_button(
                "screen-head-mention",
                agent,
                &self.mention,
                theme,
            ))
            .child(
                button(
                    "screen-head-all",
                    "All activity",
                    None,
                    ButtonKind::Text,
                    theme,
                )
                .text_size(px(FONT_SMALL))
                .on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.pick(None, cx))),
            )
    }

    fn feed(&self, theme: &Theme) -> Vec<AnyElement> {
        let agent = self
            .picked
            .and_then(|ix| self.board.lines.get(ix))
            .map(|line| line.agent);
        let mut shown = Vec::new();
        let mut last = None;
        for (ix, event) in self.board.feed(agent) {
            let label = bucket(event.minutes);
            if last != Some(label) {
                last = Some(label);
                shown.push(
                    caption(label, theme)
                        .pt(px(BUCKET_PAD_TOP))
                        .pb(px(BUCKET_PAD_BOTTOM))
                        .into_any_element(),
                );
            }
            shown.push(event_row(ix, event, &self.mention, theme).into_any_element());
        }
        shown
    }
}

fn screen_row(ix: usize, line: &AgentLine, picked: bool, theme: &Theme) -> Stateful<Div> {
    let agent = &line.agent;
    let t3 = ink(theme, T3);
    div()
        .id(("screen-row", ix))
        .flex()
        .items_center()
        .gap(px(ROW_GAP))
        .px(px(ROW_PAD_X))
        .py(px(SCREEN_ROW_PAD_Y))
        .rounded(px(RADIUS_TAB))
        .cursor_pointer()
        .when(picked, |row| row.bg(ink(theme, GRID_ON)))
        .child(avatar(("screen-avatar", ix), agent, AvatarSize::Row, theme))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .line_height(px(LINE_TASK))
                .child(
                    div()
                        .flex()
                        .gap(px(EVENT_HEAD_GAP))
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .truncate()
                                .text_size(px(FONT_BODY))
                                .text_color(agent.kind.color(theme))
                                .child(agent.name()),
                        )
                        .child(
                            div()
                                .flex_none()
                                .text_size(px(FONT_KBD))
                                .text_color(t3)
                                .child(line.time.clone()),
                        ),
                )
                .child(
                    div()
                        .truncate()
                        .text_size(px(FONT_WHO))
                        .text_color(t3)
                        .child(line.now.clone()),
                ),
        )
}

impl Render for AgentScreen {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let key = self.picked.unwrap_or(usize::MAX);
        let summed = (!self.board.lines.is_empty()).then(|| summary(&self.board, &theme));
        let card = shell(
            Header::Title(
                Some(Glyph::Agents),
                "Sub-agents".into(),
                summed.map(IntoElement::into_any_element),
            ),
            &theme,
        )
        .size_full();
        if self.board.lines.is_empty() {
            return card.child(inner_card(&theme).child(empty_state(
                "agents-empty",
                "No sub-agents",
                Some("Agents the lead starts appear here.".into()),
                &[],
                &[],
                &theme,
                |_, _, _| {},
            )));
        }
        let pane = div()
            .flex_1()
            .min_w(px(FEED_LEAST))
            .h_full()
            .flex()
            .flex_col()
            .child(
                ScrollArea::new(("screen-feed", key)).child(
                    div()
                        .px(px(FEED_PAD_X))
                        .pb(px(FEED_PAD_BOTTOM))
                        .child(self.head(&theme, cx))
                        .children(self.feed(&theme)),
                ),
            )
            .with_animation(
                ("screen-pane", key),
                Animation::new(TOGGLE_MS).with_easing(EASE_OUT),
                |pane, shown| pane.opacity(shown),
            );
        card.child(
            inner_card(&theme).child(
                div()
                    .size_full()
                    .min_h_0()
                    .flex()
                    .text_color(ink(&theme, T1))
                    .child(self.list(&theme, window, cx))
                    .child(
                        div()
                            .flex_none()
                            .w(px(HAIRLINE))
                            .h_full()
                            .bg(tint(theme.color(ColorToken::Shadow), DIVIDER_SHADE)),
                    )
                    .child(pane),
            ),
        )
    }
}
