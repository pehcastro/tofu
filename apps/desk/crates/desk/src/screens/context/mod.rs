use super::frame;

use crate::modules::chat::Chat;
use desk_core::model::Store;
use desk_core::protocol::{ContextOccupancy, ContextReport};
use desk_core::query::Answer;
use desk_ui::components::avatar::spinner;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::inner_card;
use desk_ui::components::charts::legend;
use desk_ui::components::chip::mono;
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::list::row;
use desk_ui::components::paint::ink;
use desk_ui::components::size::T2;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, Entity, EntityId, FontWeight,
    Rgba, SharedString, Subscription, WeakEntity, Window, div, prelude::*, px, relative,
};

use frame::{ellipsis, load_fonts, note, panel, panes, title, window};

const NO_TOFU: &str = "The context reads query.context from the tofu the work screen runs, and no work screen is open here.";
const STRONG_SWATCH: f32 = 0.55;
const SINCE_SWATCH: f32 = 0.22;
const EMPTY_TRACK: f32 = 0.07;
const WINDOW_LEAST: f32 = 300.0;
const BANDS_LEAST: f32 = 300.0;
const FIGURE: f32 = 34.0;
const TRACK: f32 = 12.0;
const FILL_TRACK: f32 = 4.0;
const PERCENT_WIDTH: f32 = 44.0;

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
            open: None,
            live: None,
        })
        .into())
}

pub struct ContextScreen {
    source: Option<Source>,
    answer: Answer<ContextReport>,
    open: Option<String>,
    live: Option<(i64, i64)>,
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
        if self.answer.asking
            && !store.context.asking
            && let Some(read) = &store.context.read
        {
            eprintln!(
                "desk: context shows query.context at {} for {} {}: {}",
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
        self.answer = store.context.clone();
        self.live = live;
        self.open = open;
        cx.notify();
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

    fn header(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let read = self.answer.read.as_ref().map(|read| {
            format!(
                "{}, query.context read {}",
                read.value.name.as_deref().unwrap_or("unnamed session"),
                read.at
            )
        });
        div()
            .flex()
            .flex_none()
            .flex_wrap()
            .items_center()
            .gap(px(10.0))
            .px(px(4.0))
            .child(title("Context"))
            .children(read.map(|read| ellipsis(note(read, theme))))
            .child(div().flex_1())
            .when(self.answer.asking, |header| {
                header.child(spinner("context-asking", theme))
            })
            .child(
                button(
                    "context-reread",
                    "Read again",
                    None,
                    ButtonKind::Plain,
                    theme,
                )
                .on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.reread(cx))),
            )
    }

    fn elsewhere(&self, report: &ContextReport) -> Option<String> {
        let reported = report.session.as_deref();
        if reported.is_some() && reported == self.open.as_deref() {
            return None;
        }
        let name = report.name.as_deref().unwrap_or("an unnamed session");
        Some(match &self.open {
            Some(open) => format!(
                "query.context reported {name}, read before {open} opened, and is being read again"
            ),
            None => format!(
                "no session is open, so query.context reported {name}, the newest session on disk"
            ),
        })
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

    fn waiting(&self, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let Some(error) = &self.answer.failed else {
            return empty_state(
                "context-reading",
                "Reading the context",
                Some("asked the tofu this project runs for query.context".into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            )
            .into_any_element();
        };
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
        let Some(read) = &self.answer.read else {
            return div()
                .flex()
                .flex_col()
                .gap(px(8.0))
                .child(self.header(theme, cx))
                .child(self.waiting(theme, cx))
                .into_any_element();
        };
        let report = &read.value;
        let shown = match &report.occupancy {
            Some(occupancy) => panes()
                .child(self.window_panel(report, occupancy, theme))
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
        div()
            .flex()
            .flex_col()
            .gap(px(8.0))
            .child(self.header(theme, cx))
            .children(self.elsewhere(report).map(|said| warn(said, theme)))
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
    ) -> Div {
        let total = self.total(report, occupancy);
        let since = (total.used - occupancy.total).max(0);
        let danger = theme.color(ColorToken::StatusDanger);
        let mut parts: Vec<(SharedString, Rgba, i64)> = occupancy
            .bands()
            .into_iter()
            .zip(swatches(theme))
            .map(|((name, band), color)| (name.into(), color, band.tokens))
            .collect();
        if since > 0 {
            parts.push(("since the read".into(), ink(theme, SINCE_SWATCH), since));
        }
        let track = parts
            .iter()
            .fold(track(TRACK, theme), |track, (_, color, tokens)| {
                track.child(
                    div()
                        .h_full()
                        .w(relative(share(*tokens, total.of)))
                        .bg(*color),
                )
            })
            .child(
                div()
                    .absolute()
                    .top_0()
                    .bottom_0()
                    .left(relative(share(occupancy.mark, total.of)))
                    .w(px(2.0))
                    .bg(danger),
            );
        let named = parts
            .iter()
            .map(|(name, color, _)| (name.clone(), *color))
            .chain(std::iter::once((
                format!("mark {}", grouped(occupancy.mark)).into(),
                danger,
            )));
        panel("The window", None, theme)
            .map(|panel| frame::fraction(panel, 1.2, WINDOW_LEAST))
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
                    .child(track)
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
            Some(_) => self.body(&theme, cx),
        };
        window(&theme, div().flex_1().flex().flex_col().child(body))
    }
}
