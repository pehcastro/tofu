use super::frame;

use crate::modules::chat::Chat;
use desk_core::model::Store;
use desk_core::protocol::{ContextOccupancy, ContextReport};
use desk_core::query::{Answer, QueryError, Read};
use desk_ui::components::card::inner_card;
use desk_ui::components::charts::{ContextLine, DotGrid, GridPart, Said, cached, legend};
use desk_ui::components::chip::mono;
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::list::row;
use desk_ui::components::paint::ink;
use desk_ui::components::size::T2;
use desk_ui::components::skeleton::{pulse, skeleton_bar, skeleton_block, skeleton_lines};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, Context, Div, Entity, EntityId, FontWeight, Rgba,
    SharedString, Subscription, WeakEntity, Window, div, prelude::*, px, relative,
};
use std::collections::BTreeMap;

use frame::{Gate, ellipsis, load_fonts, note, panel, panes, reread, titled, window};

const NO_TOFU: &str = "The context reads query.context from the tofu the work screen runs, and no work screen is open here.";
const STRONG_SWATCH: f32 = 0.55;
const SINCE_SWATCH: f32 = 0.22;
const EMPTY_TRACK: f32 = 0.07;
const WINDOW_LEAST: f32 = 300.0;
const BANDS_LEAST: f32 = 300.0;
const FIGURE: f32 = 34.0;
const PANEL_GAP: f32 = 10.0;
const PER_DOT: i64 = 1000;
const LINE_WIDE: f32 = 700.0;
const LINE_FLOOR: f32 = 148.0;
const LINE_RISE: f32 = 140.0;
const NO_COMPACTION: f32 = -1.0;
const FILL_TRACK: f32 = 4.0;
const PERCENT_WIDTH: f32 = 44.0;
const SKELETON_GRID: f32 = 150.0;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the context screen draws what tofu query.context reports, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|_| ContextScreen {
            source: None,
            answer: Answer::default(),
            kept: BTreeMap::new(),
            open: None,
            switching: false,
            live: None,
            trail: Vec::new(),
            trail_of: None,
            charts: None,
            said: String::new(),
            gate: Gate::default(),
        })
        .into())
}

pub struct ContextScreen {
    source: Option<Source>,
    answer: Answer<ContextReport>,
    kept: BTreeMap<String, Read<ContextReport>>,
    open: Option<String>,
    switching: bool,
    live: Option<(i64, i64)>,
    trail: Vec<i64>,
    trail_of: Option<(Option<String>, String)>,
    charts: Option<Charts>,
    said: String,
    gate: Gate,
}

enum Shown<'a> {
    Loading,
    Failed(&'a QueryError),
    Read(&'a Read<ContextReport>),
}

struct Charts {
    grid: Entity<DotGrid>,
    line: Entity<ContextLine>,
}

struct Source {
    chat: WeakEntity<Chat>,
    id: EntityId,
    _watch: Subscription,
}

struct Total {
    used: i64,
    of: i64,
    source: &'static str,
}

impl ContextScreen {
    pub fn read_from(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        if self
            .source
            .as_ref()
            .is_some_and(|source| source.id == chat.entity_id())
        {
            return;
        }
        let store = chat.read(cx).store().clone();
        let watch = cx.observe(&store, |screen, store, cx| screen.saw(&store, cx));
        self.source = Some(Source {
            chat: chat.downgrade(),
            id: chat.entity_id(),
            _watch: watch,
        });
        chat.update(cx, |chat, cx| chat.want_context(cx));
        self.saw(&store, cx);
        cx.notify();
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let open = self
            .source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
            .and_then(|chat| chat.read(cx).open_id().map(str::to_owned));
        let store = store.read(cx);
        let live = open
            .as_deref()
            .and_then(|id| store.sessions.get(id))
            .and_then(|session| session.context);
        if store.context == self.answer && live == self.live && open == self.open {
            return;
        }
        if !self.answer.asking && store.context.asking {
            eprintln!("desk: context: asked query.context at {}", frame::stamp());
        }
        if self.answer.asking
            && !store.context.asking
            && let Some(read) = &store.context.read
        {
            eprintln!(
                "desk: context: answer at {}, query.context at {} for {} {}: {}",
                frame::stamp(),
                read.at,
                read.value.name.as_deref().unwrap_or("unnamed"),
                read.value.session.as_deref().unwrap_or("none"),
                read.value.occupancy.as_ref().map_or_else(
                    || "no occupancy".to_owned(),
                    |occupancy| format!("total {}", occupancy.total)
                )
            );
        }
        if let (Some((used, budget)), true) = (live, live != self.live) {
            eprintln!(
                "desk: context shows {used} of {budget} live from context.updated for session {}",
                open.as_deref().unwrap_or("none")
            );
        }
        let moved = live != self.live;
        self.switching = match open {
            Some(_) => false,
            None => self.switching || self.open.is_some(),
        };
        self.answer = store.context.clone();
        self.live = live;
        self.open = open;
        if let Some(read) = &self.answer.read
            && let Some(session) = &read.value.session
        {
            self.kept.insert(session.clone(), read.clone());
        }
        if matches!(self.shown(), Shown::Loading) {
            self.gate.wait();
        }
        let shown = self.shown_read();
        let shown_of = shown.map(|read| (read.value.session.clone(), read.at.clone()));
        let current = shown.is_some_and(|read| self.elsewhere(&read.value).is_none());
        let first = shown
            .and_then(|read| read.value.occupancy.as_ref())
            .map(|occupancy| occupancy.total);
        if shown_of != self.trail_of {
            self.trail = first.into_iter().collect();
            self.trail_of = shown_of;
        } else if let (Some((reading, _)), true, true) = (live, moved, current) {
            self.trail.push(reading);
        }
        self.charts = self.draw(cx);
        let said = match self.shown() {
            Shown::Loading => "loading".to_owned(),
            Shown::Failed(error) => format!("failed: {error}"),
            Shown::Read(read) => format!(
                "{} {}",
                if read.value.occupancy.is_some() {
                    "filled"
                } else {
                    "empty"
                },
                read.value.session.as_deref().unwrap_or("none")
            ),
        };
        if said != self.said {
            eprintln!(
                "desk: context state {said} for open {} asking {}",
                self.open.as_deref().unwrap_or("none"),
                self.answer.asking
            );
            self.said = said;
        }
        cx.notify();
    }

    fn shown(&self) -> Shown<'_> {
        let kept = match &self.open {
            Some(open) => self.kept.get(open),
            None if self.switching => None,
            None => self.answer.read.as_ref(),
        };
        match (kept, &self.answer.failed) {
            (Some(read), _) => Shown::Read(read),
            (None, Some(error)) if !self.answer.asking && !self.switching => Shown::Failed(error),
            (None, _) => Shown::Loading,
        }
    }

    fn shown_read(&self) -> Option<&Read<ContextReport>> {
        match self.shown() {
            Shown::Read(read) => Some(read),
            Shown::Loading | Shown::Failed(_) => None,
        }
    }

    fn draw(&self, cx: &mut Context<Self>) -> Option<Charts> {
        let report = &self.shown_read()?.value;
        let occupancy = report.occupancy.as_ref()?;
        let theme = ActiveTheme::theme(cx);
        let total = self.total(report, occupancy);
        let parts = parts(occupancy, total.used, &theme)
            .into_iter()
            .map(|(name, color, tokens)| GridPart {
                name,
                tokens: dots(tokens.saturating_add(PER_DOT / 2)),
                color,
            })
            .collect();
        let capacity = dots(total.of.saturating_add(PER_DOT - 1)).max(1);
        let fork_at = dots(occupancy.mark.saturating_add(PER_DOT / 2));
        let height = |used: i64| LINE_FLOOR - share(used, total.of) * LINE_RISE;
        let mut readings: Vec<(i64, &'static str)> = self
            .trail
            .iter()
            .enumerate()
            .map(|(at, &used)| {
                (
                    used,
                    if at == 0 {
                        "query.context read"
                    } else {
                        "context.updated"
                    },
                )
            })
            .collect();
        if let [(used, _)] = readings[..] {
            readings.push((used, "now"));
        }
        let last = readings.len().saturating_sub(1).max(1) as f32;
        let points = readings
            .iter()
            .enumerate()
            .map(|(at, &(used, _))| (at as f32 / last * LINE_WIDE, height(used)))
            .collect();
        let tips = readings
            .iter()
            .map(|&(used, said)| Said::new(format!("{}k", dots(used)), said))
            .collect();
        let threshold = height(occupancy.mark);
        Some(Charts {
            grid: cx.new(|_| DotGrid::new(parts, capacity, fork_at, None)),
            line: cx.new(|_| ContextLine::new(points, tips, threshold, NO_COMPACTION, Vec::new())),
        })
    }

    fn reread(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self
            .source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
        else {
            return eprintln!("desk: context: the chat is gone, so nothing was asked");
        };
        eprintln!("desk: context: read again");
        chat.update(cx, |chat, cx| chat.reread_context(cx));
    }

    fn elsewhere(&self, report: &ContextReport) -> Option<String> {
        let reported = report.session.as_deref();
        if reported.is_some() && reported == self.open.as_deref() {
            return None;
        }
        let name = report.name.as_deref().unwrap_or("an unnamed session");
        Some(format!(
            "no session is open, so query.context reported {name}, the newest session on disk"
        ))
    }

    fn total(&self, report: &ContextReport, occupancy: &ContextOccupancy) -> Total {
        match self.live {
            Some((used, of)) if self.elsewhere(report).is_none() => Total {
                used,
                of,
                source: "total and ceiling live from context.updated",
            },
            _ => Total {
                used: occupancy.total,
                of: report.ceiling.unwrap_or_default(),
                source: "total and ceiling from query.context",
            },
        }
    }

    fn failed(&self, error: &QueryError, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let again = cx.listener(|screen, _: &(), _, cx| screen.reread(cx));
        empty_state(
            "context-failed",
            "The context could not be read",
            Some(error.to_string().into()),
            &[EmptyAction {
                label: "Try again".into(),
                glyph: None,
                keys: None,
            }],
            &[],
            theme,
            move |_, window, cx| again(&(), window, cx),
        )
        .into_any_element()
    }

    fn body(&self, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let page = div().flex().flex_col().gap(px(8.0));
        let read = match self.shown() {
            Shown::Read(read) => read,
            Shown::Loading => return page.child(loading(theme, cx)).into_any_element(),
            Shown::Failed(error) => {
                return page.child(self.failed(error, theme, cx)).into_any_element();
            }
        };
        let report = &read.value;
        let about = format!(
            "{}, query.context read {}",
            report.name.as_deref().unwrap_or("unnamed session"),
            read.at
        );
        let page = page.child(ellipsis(note(about, theme)).px(px(4.0)));
        let shown = match &report.occupancy {
            Some(occupancy) => panes()
                .child(
                    frame::fraction(div(), 1.2, WINDOW_LEAST)
                        .flex()
                        .flex_col()
                        .gap(px(PANEL_GAP))
                        .child(self.window_panel(report, occupancy, theme, cx))
                        .child(self.growth_panel(theme, cx)),
                )
                .child(bands_panel(report, occupancy, theme))
                .into_any_element(),
            None => empty_state(
                "context-unmeasured",
                "Nothing measured yet",
                Some(
                    report
                        .unmeasured
                        .clone()
                        .unwrap_or_else(|| "query.context reported no occupancy".to_owned())
                        .into(),
                ),
                &[],
                &[],
                theme,
                |_, _, _| {},
            )
            .into_any_element(),
        };
        page.children(self.elsewhere(report).map(|said| note(said, theme)))
            .children(
                self.answer
                    .failed
                    .as_ref()
                    .map(|error| warn(format!("the last read failed: {error}"), theme)),
            )
            .child(shown)
            .into_any_element()
    }

    fn window_panel(
        &self,
        report: &ContextReport,
        occupancy: &ContextOccupancy,
        theme: &Theme,
        cx: &App,
    ) -> Div {
        let total = self.total(report, occupancy);
        let danger = theme.color(ColorToken::StatusDanger);
        let named = parts(occupancy, total.used, theme)
            .into_iter()
            .map(|(name, color, _)| (name, color))
            .chain(std::iter::once((
                format!("mark {}", grouped(occupancy.mark)).into(),
                danger,
            )));
        panel(
            "The window",
            Some(note("one dot = 1k tokens", theme).into_any_element()),
            theme,
        )
        .child(
            inner_card(theme)
                .px(px(16.0))
                .py(px(14.0))
                .gap(px(12.0))
                .child(
                    div()
                        .flex()
                        .flex_wrap()
                        .items_baseline()
                        .gap(px(10.0))
                        .child(
                            div()
                                .text_size(px(FIGURE))
                                .line_height(relative(1.0))
                                .font_weight(FontWeight::SEMIBOLD)
                                .child(grouped(total.used)),
                        )
                        .child(
                            div()
                                .text_color(ink(theme, T2))
                                .child(format!("of {} tokens", grouped(total.of))),
                        ),
                )
                .children(self.charts.as_ref().map(|charts| cached(&charts.grid, cx)))
                .child(legend(named, theme))
                .child(note(
                    format!(
                        "{}, mark from query.context occupancy, {} bytes per thousand tokens",
                        total.source,
                        grouped(report.bytes_per_thousand_tokens.unwrap_or_default())
                    ),
                    theme,
                )),
        )
    }

    fn growth_panel(&self, theme: &Theme, cx: &App) -> Div {
        let since = self.trail.len().saturating_sub(1);
        panel(
            "How it grew",
            Some(note("per reading", theme).into_any_element()),
            theme,
        )
        .child(
            inner_card(theme)
                .px(px(16.0))
                .py(px(14.0))
                .gap(px(10.0))
                .children(self.charts.as_ref().map(|charts| cached(&charts.line, cx)))
                .child(
                    div()
                        .flex()
                        .flex_wrap()
                        .justify_between()
                        .gap(px(10.0))
                        .child(note("query.context read", theme))
                        .child(note(
                            format!("{since} context.updated since, dashed at the mark"),
                            theme,
                        )),
                ),
        )
    }
}

fn parts(occupancy: &ContextOccupancy, used: i64, theme: &Theme) -> Vec<(SharedString, Rgba, i64)> {
    let since = used.saturating_sub(occupancy.total).max(0);
    let mut parts: Vec<(SharedString, Rgba, i64)> = occupancy
        .bands()
        .into_iter()
        .zip(swatches(theme))
        .map(|((name, band), color)| (name.into(), color, band.tokens))
        .collect();
    if since > 0 {
        parts.push(("since the read".into(), ink(theme, SINCE_SWATCH), since));
    }
    parts
}

fn dots(tokens: i64) -> usize {
    usize::try_from(tokens.max(0) / PER_DOT).unwrap_or(usize::MAX)
}

fn bands_panel(report: &ContextReport, occupancy: &ContextOccupancy, theme: &Theme) -> Div {
    let mut notes = vec![note(
        format!(
            "query.context occupancy, step {} of {}",
            occupancy.step,
            report.steps.unwrap_or_default()
        ),
        theme,
    )];
    if let Some(task) = &report.task {
        notes.push(note(format!("task: {task}"), theme));
    }
    if !occupancy.caps_recorded {
        notes.push(warn("the caps were not recorded for this session", theme));
    }
    if let Some(fork) = &report.fork {
        let counts = fork.counts.as_ref().map_or_else(String::new, |counts| {
            format!(
                ", {} to {} tokens",
                grouped(counts.tokens_before),
                grouped(counts.tokens_after)
            )
        });
        notes.push(note(
            format!(
                "forked into {}{}{counts}",
                fork.into,
                fork.kind
                    .as_deref()
                    .map_or_else(String::new, |kind| format!(" as {kind}"))
            ),
            theme,
        ));
    }
    if !report.skipped.is_empty() {
        notes.push(note(
            format!(
                "{} older sessions tofu could not read were skipped",
                report.skipped.len()
            ),
            theme,
        ));
    }
    panel("The four bands", None, theme)
        .map(|panel| frame::fraction(panel, 1.0, BANDS_LEAST))
        .child(
            inner_card(theme)
                .p(px(8.0))
                .gap(px(2.0))
                .text_size(px(13.5))
                .children(
                    occupancy
                        .bands()
                        .into_iter()
                        .zip(swatches(theme))
                        .enumerate()
                        .map(|(at, ((name, band), color))| {
                            row(("band", at), false, false, theme)
                                .flex_col()
                                .items_stretch()
                                .gap(px(6.0))
                                .child(
                                    div()
                                        .flex()
                                        .items_center()
                                        .gap(px(8.0))
                                        .child(
                                            div()
                                                .flex_none()
                                                .size(px(9.0))
                                                .rounded(px(2.0))
                                                .bg(color),
                                        )
                                        .child(ellipsis(div().flex_1()).child(name))
                                        .child(div().flex_none().font_family(mono(theme)).child(
                                            format!(
                                                "{} / {}",
                                                grouped(band.tokens),
                                                grouped(band.cap)
                                            ),
                                        ))
                                        .child(
                                            div()
                                                .flex_none()
                                                .min_w(px(PERCENT_WIDTH))
                                                .flex()
                                                .justify_end()
                                                .font_family(mono(theme))
                                                .text_color(ink(theme, T2))
                                                .child(format!("{}%", band.fill_percent)),
                                        ),
                                )
                                .child(
                                    track(FILL_TRACK, theme).child(
                                        div()
                                            .h_full()
                                            .w(relative(share(band.fill_percent, 100)))
                                            .bg(color),
                                    ),
                                )
                        }),
                )
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .gap(px(2.0))
                        .px(px(10.0))
                        .py(px(6.0))
                        .children(notes),
                ),
        )
}

fn swatches(theme: &Theme) -> [Rgba; 4] {
    [
        ink(theme, STRONG_SWATCH),
        theme.color(ColorToken::StatusAccent),
        theme.color(ColorToken::StatusLive),
        theme.color(ColorToken::Trace),
    ]
}

fn track(height: f32, theme: &Theme) -> Div {
    div()
        .relative()
        .flex()
        .w_full()
        .h(px(height))
        .rounded(px(height / 2.0))
        .overflow_hidden()
        .bg(ink(theme, EMPTY_TRACK))
}

fn share(part: i64, whole: i64) -> f32 {
    if whole <= 0 {
        return 0.0;
    }
    (part as f32 / whole as f32).clamp(0.0, 1.0)
}

fn grouped(count: i64) -> String {
    let digits = count.unsigned_abs().to_string();
    let mut said = String::with_capacity(digits.len() + digits.len() / 3 + 1);
    if count < 0 {
        said.push('-');
    }
    for (at, digit) in digits.chars().enumerate() {
        if at > 0 && (digits.len() - at).is_multiple_of(3) {
            said.push(',');
        }
        said.push(digit);
    }
    said
}

fn loading(theme: &Theme, cx: &App) -> Div {
    let card = |lines: usize, key: &'static str| {
        inner_card(theme)
            .px(px(16.0))
            .py(px(14.0))
            .child(skeleton_lines(key, lines, theme, cx))
    };
    panes()
        .child(
            frame::fraction(div(), 1.2, WINDOW_LEAST)
                .flex()
                .flex_col()
                .gap(px(PANEL_GAP))
                .child(
                    inner_card(theme)
                        .px(px(16.0))
                        .py(px(14.0))
                        .gap(px(14.0))
                        .child(pulse(
                            "context-figure",
                            0,
                            skeleton_bar(0.3, FIGURE, theme),
                            cx,
                        ))
                        .child(pulse(
                            "context-grid",
                            1,
                            skeleton_block(SKELETON_GRID, theme),
                            cx,
                        ))
                        .child(skeleton_lines("context-legend", 2, theme, cx)),
                )
                .child(card(3, "context-growth")),
        )
        .child(frame::fraction(card(8, "context-bands"), 1.0, BANDS_LEAST))
}

fn warn(text: impl Into<SharedString>, theme: &Theme) -> Div {
    note(text, theme).text_color(theme.color(ColorToken::StatusWarn))
}

impl Render for ContextScreen {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = match self.source {
            None => empty_state(
                "context-no-tofu",
                "No tofu to ask",
                Some(NO_TOFU.into()),
                &[],
                &[],
                &theme,
                |_, _, _| {},
            )
            .into_any_element(),
            Some(_) => {
                let mut gate = std::mem::take(&mut self.gate);
                let ready = !matches!(self.shown(), Shown::Loading);
                let shown = gate.show(
                    "context",
                    ready,
                    |cx| loading(&theme, cx).into_any_element(),
                    |cx| self.body(&theme, cx),
                    cx,
                );
                self.gate = gate;
                shown
            }
        };
        let trailing = self.source.is_some().then(|| {
            reread(
                "context",
                self.answer.asking,
                &theme,
                cx,
                ContextScreen::reread,
            )
            .into_any_element()
        });
        window(
            titled("context", trailing),
            &theme,
            div().flex_1().flex().flex_col().child(body),
        )
    }
}
