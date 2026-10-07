use std::ops::Range;
use std::time::{Duration, Instant};

use desk_motion::tokens::{EASE_OUT, HOVER_MS};
use gpui::{
    Animation, AnimationExt, AnyElement, App, ClickEvent, ClipboardItem, Context, ElementId,
    FontWeight, HighlightStyle, Render, Rgba, SharedString, StyledText, Task, Window, div, millis,
    prelude::*, px, rgb_to_hsla,
};

use crate::components::chip::{badge, mono, tabular};
use crate::components::glyph::Glyph;
use crate::components::paint::{Spinner, glyph, ink, pressed, ring, tint};
use crate::components::scroll::ScrollArea;
use crate::components::size::{
    BADGE_PAD_X, CAPTION_TEXT, CODE_LINE, FAIL_RING, FONT_SMALL, FONT_TAB, HEADER, HEADER_PAD_LEFT,
    HEADER_PAD_RIGHT, HOVER, RADIUS_BADGE, RADIUS_LIST, SPINNER, SPINNER_TRACK, T1, T2,
};
use crate::live::ActiveTheme;
use crate::metrics::ICON_SMALL;
use crate::theme::{ColorToken, Theme};

const BODY_LINES: f32 = 14.0;
const BODY_PAD: f32 = 4.0;
const BODY_MAX: f32 = BODY_LINES * CODE_LINE + BODY_PAD;
const EXIT_FILL: f32 = 0.14;
const FAINT: f32 = 0.45;
const COPIED_FOR: Duration = millis(1200);
const STREAM_STEP: Duration = millis(120);
const STREAM_SETTLE: Duration = millis(450);
const STREAM_BURST: usize = 3;
const STREAM_PAUSE: u32 = 3;
const TENTHS_BELOW_SECS: u64 = 10;
const SECS_PER_MINUTE: u64 = 60;

const NORMAL: [ColorToken; 8] = [
    ColorToken::AnsiBlack,
    ColorToken::AnsiRed,
    ColorToken::AnsiGreen,
    ColorToken::AnsiYellow,
    ColorToken::AnsiBlue,
    ColorToken::AnsiMagenta,
    ColorToken::AnsiCyan,
    ColorToken::AnsiWhite,
];
const BRIGHT: [ColorToken; 8] = [
    ColorToken::AnsiBrightBlack,
    ColorToken::AnsiBrightRed,
    ColorToken::AnsiBrightGreen,
    ColorToken::AnsiBrightYellow,
    ColorToken::AnsiBrightBlue,
    ColorToken::AnsiBrightMagenta,
    ColorToken::AnsiBrightCyan,
    ColorToken::AnsiBrightWhite,
];

pub fn exit_color(code: i32, theme: &Theme) -> Rgba {
    theme.color(if code == 0 {
        ColorToken::GitAdded
    } else {
        ColorToken::StatusDanger
    })
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TermStatus {
    Running { since: Instant },
    Exited { code: i32, took: Duration },
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
struct Pen {
    color: Option<ColorToken>,
    bold: bool,
    faint: bool,
}

impl Pen {
    fn apply(self, params: &str) -> Pen {
        let mut codes = params.split(';').map(|code| match code {
            "" => Some(0),
            code => code.parse::<u8>().ok(),
        });
        let mut pen = self;
        while let Some(code) = codes.next() {
            pen = match code {
                Some(38 | 48) => {
                    let skip = match codes.next().flatten() {
                        Some(5) => 1,
                        Some(2) => 3,
                        _ => 0,
                    };
                    codes.by_ref().take(skip).for_each(drop);
                    pen
                }
                Some(code) => pen.code(code),
                None => pen,
            };
        }
        pen
    }

    fn code(self, code: u8) -> Pen {
        match code {
            0 => Pen::default(),
            1 => Pen { bold: true, ..self },
            2 => Pen {
                faint: true,
                ..self
            },
            22 => Pen {
                bold: false,
                faint: false,
                ..self
            },
            39 => Pen {
                color: None,
                ..self
            },
            30..=37 => Pen {
                color: NORMAL.get(usize::from(code - 30)).copied(),
                ..self
            },
            90..=97 => Pen {
                color: BRIGHT.get(usize::from(code - 90)).copied(),
                ..self
            },
            _ => self,
        }
    }

    fn style(self, theme: &Theme) -> HighlightStyle {
        HighlightStyle {
            color: self.color.map(|token| rgb_to_hsla(theme.color(token))),
            font_weight: self.bold.then_some(FontWeight::BOLD),
            fade_out: self.faint.then_some(FAINT),
            ..HighlightStyle::default()
        }
    }
}

fn ansi(raw: &str) -> (String, Vec<(Range<usize>, Pen)>) {
    let mut text = String::with_capacity(raw.len());
    let mut runs = Vec::new();
    let mut pen = Pen::default();
    let mut rest = raw;
    let mut write = |text: &mut String, plain: &str, pen: Pen| {
        if pen != Pen::default() && !plain.is_empty() {
            runs.push((text.len()..text.len().saturating_add(plain.len()), pen));
        }
        text.push_str(plain);
    };
    loop {
        let Some((plain, escape)) = rest.split_once('\x1b') else {
            write(&mut text, rest, pen);
            return (text, runs);
        };
        write(&mut text, plain, pen);
        let Some(sequence) = escape.strip_prefix('[') else {
            rest = escape;
            continue;
        };
        let end = sequence
            .find(|letter: char| ('@'..='~').contains(&letter))
            .unwrap_or(sequence.len());
        let (params, after) = sequence.split_at(end);
        if after.starts_with('m') {
            pen = pen.apply(params);
        }
        rest = after.get(1..).unwrap_or_default();
    }
}

fn plain_text(lines: &[SharedString]) -> String {
    lines
        .iter()
        .map(|line| ansi(line).0)
        .collect::<Vec<_>>()
        .join("\n")
}

fn elapsed(took: Duration) -> String {
    let secs = took.as_secs();
    if secs < TENTHS_BELOW_SECS {
        format!("{:.1}s", took.as_secs_f32())
    } else if secs < SECS_PER_MINUTE {
        format!("{secs}s")
    } else {
        format!("{}m {:02}s", secs / SECS_PER_MINUTE, secs % SECS_PER_MINUTE)
    }
}

type OnClick = Box<dyn Fn(&ClickEvent, &mut Window, &mut App)>;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
struct Copied(bool);

#[derive(IntoElement)]
pub struct TermCard {
    id: ElementId,
    command: SharedString,
    lines: Vec<SharedString>,
    status: TermStatus,
    rerun: Option<OnClick>,
}

impl TermCard {
    pub fn new(
        id: impl Into<ElementId>,
        command: impl Into<SharedString>,
        lines: Vec<SharedString>,
        status: TermStatus,
    ) -> Self {
        TermCard {
            id: id.into(),
            command: command.into(),
            lines,
            status,
            rerun: None,
        }
    }

    pub fn on_rerun(
        mut self,
        rerun: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
    ) -> Self {
        self.rerun = Some(Box::new(rerun));
        self
    }
}

fn small_button(id: &'static str, theme: &Theme) -> gpui::Stateful<gpui::Div> {
    let hover = ink(theme, HOVER);
    pressed(
        div()
            .id(id)
            .flex()
            .flex_none()
            .items_center()
            .gap_1()
            .px(px(BADGE_PAD_X))
            .h(px(HEADER - 2.0 * BADGE_PAD_X))
            .rounded(px(RADIUS_BADGE))
            .cursor_pointer()
            .text_color(ink(theme, CAPTION_TEXT))
            .hover(move |style| style.bg(hover)),
        theme.color(ColorToken::ChatCalls),
    )
}

impl RenderOnce for TermCard {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let copied_state = window.use_keyed_state(self.id.clone(), cx, |_, _| Copied::default());
        let Copied(copied) = *copied_state.read(cx);
        let (mark, took, exit) = match self.status {
            TermStatus::Running { since } => {
                let live = theme.color(ColorToken::StatusLive);
                (
                    Spinner::new(
                        "term-spin",
                        SPINNER,
                        tint(live, SPINNER_TRACK),
                        live,
                        &theme,
                    )
                    .into_any_element(),
                    since.elapsed(),
                    None,
                )
            }
            TermStatus::Exited { code, took } => {
                let color = exit_color(code, &theme);
                let done = if code == 0 {
                    glyph(Glyph::Check, SPINNER, color).into_any_element()
                } else {
                    div()
                        .size(px(SPINNER / 2.0))
                        .rounded_full()
                        .bg(color)
                        .into_any_element()
                };
                (
                    div()
                        .flex()
                        .items_center()
                        .justify_center()
                        .child(done)
                        .with_animation(
                            "term-done",
                            Animation::new(HOVER_MS).with_easing(EASE_OUT),
                            |mark, t| mark.opacity(t),
                        )
                        .into_any_element(),
                    took,
                    Some((code, color)),
                )
            }
        };
        let copy = {
            let text = plain_text(&self.lines);
            small_button("term-copy", &theme)
                .on_click(move |_, window, cx| {
                    cx.write_to_clipboard(ClipboardItem::new_string(text.clone()));
                    flash_copied(&copied_state, window, cx);
                })
                .children(copied.then(|| glyph(Glyph::Check, ICON_SMALL, ink(&theme, T2))))
                .child(if copied { "copied" } else { "copy" })
        };
        let header = div()
            .h(px(HEADER))
            .flex()
            .items_center()
            .gap_2()
            .pl(px(HEADER_PAD_LEFT))
            .pr(px(HEADER_PAD_RIGHT))
            .child(
                div()
                    .size(px(SPINNER))
                    .flex_none()
                    .flex()
                    .items_center()
                    .justify_center()
                    .child(mark),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .text_color(ink(&theme, T1))
                    .child(format!("$ {}", self.command)),
            )
            .child(
                div()
                    .flex_none()
                    .font_features(tabular())
                    .text_color(ink(&theme, CAPTION_TEXT))
                    .child(elapsed(took)),
            )
            .children(exit.map(|(code, color)| {
                badge(format!("exit {code}"), &theme)
                    .bg(tint(color, EXIT_FILL))
                    .text_color(color)
            }))
            .child(copy)
            .children(self.rerun.filter(|_| exit.is_some()).map(|rerun| {
                small_button("term-rerun", &theme)
                    .on_click(rerun)
                    .child("run again")
            }));
        let empty = self.lines.is_empty();
        let lines = self
            .lines
            .iter()
            .enumerate()
            .map(|(ix, line)| term_line(ix, line, &theme));
        let failed = exit.is_some_and(|(code, _)| code != 0);
        let danger = theme.color(ColorToken::StatusDanger);
        div()
            .id(self.id)
            .flex()
            .flex_col()
            .rounded(px(RADIUS_LIST))
            .overflow_hidden()
            .bg(theme.color(ColorToken::ChatCalls))
            .when(failed, |card| {
                card.shadow(vec![ring(tint(danger, FAIL_RING))])
            })
            .font_family(mono(&theme))
            .text_size(px(FONT_SMALL))
            .child(header)
            .when(!empty, |card| {
                card.child(
                    ScrollArea::new("term-body")
                        .max_h(BODY_MAX)
                        .follow_tail(true)
                        .child(div().pb(px(BODY_PAD)).children(lines)),
                )
            })
    }
}

fn term_line(ix: usize, raw: &str, theme: &Theme) -> AnyElement {
    let (text, runs) = ansi(raw);
    let runs = runs
        .into_iter()
        .map(|(range, pen)| (range, pen.style(theme)))
        .collect::<Vec<_>>();
    div()
        .h(px(CODE_LINE))
        .px(px(HEADER_PAD_LEFT))
        .truncate()
        .text_size(px(FONT_TAB))
        .line_height(px(CODE_LINE))
        .text_color(ink(theme, T2))
        .child(StyledText::new(text).with_highlights(runs))
        .with_animation(
            ("term-line", ix),
            Animation::new(HOVER_MS).with_easing(EASE_OUT),
            |line, t| line.opacity(t),
        )
        .into_any_element()
}

fn flash_copied(copied: &gpui::Entity<Copied>, window: &mut Window, cx: &mut App) {
    copied.update(cx, |copied, cx| {
        *copied = Copied(true);
        cx.notify();
    });
    let (copied, timer) = (copied.clone(), cx.background_executor().timer(COPIED_FOR));
    window
        .spawn(cx, async move |cx| {
            timer.await;
            cx.update(|_, cx| {
                copied.update(cx, |copied, cx| {
                    *copied = Copied(false);
                    cx.notify();
                })
            })
        })
        .detach_and_log_err(cx);
}

pub struct TermReplay {
    command: SharedString,
    script: Vec<SharedString>,
    exit: i32,
    lines: Vec<SharedString>,
    status: TermStatus,
    stream: Task<()>,
}

impl TermReplay {
    pub fn new(
        command: impl Into<SharedString>,
        script: Vec<SharedString>,
        exit: i32,
        cx: &mut Context<Self>,
    ) -> Self {
        let steps = script.len();
        TermReplay {
            command: command.into(),
            script,
            exit,
            lines: Vec::new(),
            status: TermStatus::Running {
                since: Instant::now(),
            },
            stream: Self::play(steps, cx),
        }
    }

    pub fn rerun(&mut self, cx: &mut Context<Self>) {
        self.lines.clear();
        self.status = TermStatus::Running {
            since: Instant::now(),
        };
        self.stream = Self::play(self.script.len(), cx);
        cx.notify();
    }

    fn advance(&mut self, at: usize, cx: &mut Context<Self>) {
        match (self.script.get(at), self.status) {
            (Some(line), _) => self.lines.push(line.clone()),
            (None, TermStatus::Running { since }) => {
                self.status = TermStatus::Exited {
                    code: self.exit,
                    took: since.elapsed(),
                }
            }
            (None, TermStatus::Exited { .. }) => {}
        }
        cx.notify();
    }

    fn play(steps: usize, cx: &mut Context<Self>) -> Task<()> {
        cx.spawn(async move |this, cx| {
            for at in 0..=steps {
                let pause = if at == steps {
                    STREAM_SETTLE
                } else if at % STREAM_BURST == STREAM_BURST - 1 {
                    STREAM_STEP * STREAM_PAUSE
                } else {
                    STREAM_STEP
                };
                cx.background_executor().timer(pause).await;
                if this
                    .update(cx, |replay, cx| replay.advance(at, cx))
                    .is_err()
                {
                    return;
                }
            }
        })
    }
}

impl Render for TermReplay {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        TermCard::new(
            ("term-replay", cx.entity_id().as_u64()),
            self.command.clone(),
            self.lines.clone(),
            self.status,
        )
        .on_rerun(cx.listener(|replay, _: &ClickEvent, _, cx| replay.rerun(cx)))
    }
}
