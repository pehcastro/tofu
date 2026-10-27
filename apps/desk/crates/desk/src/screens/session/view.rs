use std::ops::Range;

use super::digest::{Digest, Line, TurnRow, TurnState};
use super::frame::{ellipsis, fraction, note, panes, reread, titled, window};
use super::{Fork, Loaded, SessionScreen, Shown};
use desk_core::protocol::SessionInfo;
use desk_core::query::QueryError;
use desk_ui::components::card::caption;
use desk_ui::components::chip::mono;
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::list::row;
use desk_ui::components::paint::ink;
use desk_ui::components::size::{CHIP_FILL, T2, T3};
use desk_ui::components::skeleton::{pulse, skeleton_bar, skeleton_lines};
use desk_ui::components::waterfall::ROW;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, App, ClickEvent, Context, Div, FontWeight, Rgba, SharedString, StyleRefinement,
    Window, div, prelude::*, px, relative, uniform_list,
};

const NO_TOFU: &str = "The session reads session.info, query.session and session.trace from the tofu the work screen runs, and no work screen is open here.";
const FIGURE: f32 = 22.0;
const FIGURE_LEAST: f32 = 130.0;
const TURNS_LEAST: f32 = 380.0;
const DETAIL_LEAST: f32 = 320.0;
const TURN_ROW: f32 = 30.0;
const TURNS_MOST: f32 = 390.0;
const TIME_WIDTH: f32 = 44.0;
const TIME_DATED: f32 = 84.0;
const TRACE_MOST: f32 = 420.0;
const TRACE_TEXT: f32 = 12.5;
const SECTION_GAP: f32 = 16.0;
const DIVIDER_INK: f32 = 0.08;
const LISTED_MOST: usize = 6;
const FILES_MOST: usize = 8;
const SKELETON_FIGURE: f32 = 22.0;

impl SessionScreen {
    fn about(&self, info: &SessionInfo, theme: &Theme) -> Div {
        let mut said = vec![format!("started {}", clock(&info.at))];
        said.extend(info.model.clone());
        said.extend(info.outcome.clone());
        if let (Some(at), Some(of)) = (info.generation, info.generations) {
            said.push(format!("generation {at} of {of}"));
        }
        let scoped = self.scoped();
        if scoped.len() > 1 {
            said.push(format!("{} forks shown", scoped.len()));
        }
        let unread = scoped
            .iter()
            .filter(|(id, _)| self.trace_of(id).is_none())
            .count();
        if unread > 0 {
            said.push(format!(
                "reading session.trace for {unread} of {}",
                scoped.len()
            ));
        }
        let failed = scoped.iter().find_map(|(id, _)| match self.traces.get(id) {
            Some(Loaded::Failed(error)) => Some(format!("{id}: {error}")),
            Some(Loaded::Read(_) | Loaded::Reading) | None => None,
        });
        let family = match &self.family {
            Some((_, Loaded::Failed(error))) => Some(format!("query.session: {error}")),
            Some((_, Loaded::Read(_) | Loaded::Reading)) | None => None,
        };
        div()
            .flex()
            .flex_col()
            .gap(px(2.0))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(10.0))
                    .min_w_0()
                    .child(
                        div()
                            .flex_none()
                            .font_weight(FontWeight::SEMIBOLD)
                            .child(info.name.clone().unwrap_or_else(|| info.handle.clone())),
                    )
                    .child(ellipsis(note(said.join(" · "), theme))),
            )
            .children(
                failed
                    .into_iter()
                    .chain(family)
                    .map(|error| warn(error, theme)),
            )
    }

    fn figures(&self, info: &SessionInfo, theme: &Theme) -> Div {
        let scoped = self.scoped();
        let forks: Vec<&Fork> = self
            .forks()
            .iter()
            .filter(|fork| scoped.iter().any(|(id, _)| *id == fork.id))
            .collect();
        let (turns, cost) = match forks.is_empty() {
            true => (info.turns, info.cost_usd),
            false => (
                forks.iter().map(|fork| fork.turns).sum(),
                Some(forks.iter().map(|fork| fork.cost_usd).sum()),
            ),
        };
        let mut shown = vec![(
            "Turns",
            grouped(turns),
            Some(match forks.len() > 1 {
                true => format!("{} forks", forks.len()),
                false => format!("{} steps", grouped(info.steps)),
            }),
        )];
        if let Some(digest) = &self.digest {
            let totals = &digest.totals;
            let agents_aside = [
                digest.partial.then(|| "so far".to_owned()),
                (totals.running > 0).then(|| format!("{} running", totals.running)),
            ]
            .into_iter()
            .flatten()
            .collect::<Vec<_>>()
            .join(" · ");
            shown.extend([
                (
                    "Sub-agents",
                    grouped_count(totals.agents),
                    (!agents_aside.is_empty()).then_some(agents_aside),
                ),
                (
                    "Files changed",
                    grouped_count(totals.files),
                    Some(format!("{} edits", grouped_count(totals.edits))),
                ),
                (
                    "Tool calls",
                    grouped_count(totals.calls),
                    Some(format!("{} LLM calls", grouped_count(totals.requests))),
                ),
                (
                    "Tokens",
                    compact(totals.tokens),
                    cost.map(|cost| format!("${cost:.2}")),
                ),
            ]);
        }
        panes().children(
            shown
                .into_iter()
                .map(|(label, value, aside)| figure(label, value, aside, theme)),
        )
    }

    fn body(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let read = match self.shown() {
            Shown::Read(read) => read,
            Shown::Loading => return loading(theme, cx),
            Shown::Failed(error) => {
                return div().flex().flex_col().child(self.failed(error, theme, cx));
            }
        };
        let info = &read.value;
        let page = div()
            .flex()
            .flex_col()
            .gap(px(SECTION_GAP))
            .child(self.about(info, theme))
            .children(
                self.answer
                    .failed
                    .as_ref()
                    .filter(|_| !self.answer.asking && self.asked_for.as_ref() == Some(&info.id))
                    .map(|error| warn(format!("the last read failed: {error}"), theme)),
            )
            .child(self.figures(info, theme));
        let Some(digest) = &self.digest else {
            return page.child(loading_turns(theme, cx));
        };
        if digest.lines.is_empty() && digest.trace.is_empty() {
            return page.child(empty_state(
                "session-no-turns",
                "No turns yet",
                Some("session.trace reported no turn for what the picker shows".into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            ));
        }
        let picked = picked_turn(self.picked.as_deref(), digest);
        page.when(!digest.trace.is_empty(), |page| {
            let height = (digest.trace.len() as f32 * ROW).min(TRACE_MOST) + ROW;
            page.child(divider(theme)).child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(6.0))
                    .child(head(
                        "Trace",
                        "wheel scrolls rows · shift or sideways pans time · ctrl zooms",
                        theme,
                    ))
                    .child(
                        div().text_size(px(TRACE_TEXT)).child(
                            self.trace
                                .clone()
                                .cached(StyleRefinement::default().w_full().h(px(height))),
                        ),
                    ),
            )
        })
        .child(divider(theme))
        .child(
            panes()
                .items_start()
                .child(self.turns(digest, picked.map(|turn| turn.id.as_str()), theme, cx))
                .children(picked.map(|turn| detail(turn, theme))),
        )
    }

    fn turns(
        &self,
        digest: &Digest,
        picked: Option<&str>,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let height = (digest.lines.len() as f32 * TURN_ROW).min(TURNS_MOST);
        let picked = picked.map(str::to_owned);
        fraction(div(), 1.4, TURNS_LEAST)
            .flex()
            .flex_col()
            .gap(px(6.0))
            .child(head(
                "Turns",
                "newest first · what was typed · quiet turns folded",
                theme,
            ))
            .child(
                div().id("session-turns").occlude().h(px(height)).child(
                    uniform_list(
                        "session-turn-rows",
                        digest.lines.len(),
                        cx.processor(move |screen: &mut Self, range: Range<usize>, _, cx| {
                            let theme = ActiveTheme::theme(cx);
                            let Some(digest) = &screen.digest else {
                                return Vec::new();
                            };
                            let dated = digest.dated;
                            range
                                .filter_map(|at| {
                                    let line = digest.lines.get(at)?;
                                    Some(turn_line(at, line, picked.as_deref(), dated, &theme, cx))
                                })
                                .collect::<Vec<_>>()
                        }),
                    )
                    .track_scroll(&self.turns_scroll)
                    .size_full(),
                ),
            )
    }

    fn failed(&self, error: &QueryError, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let again = cx.listener(|screen, _: &(), _, cx| screen.reread(cx));
        empty_state(
            "session-failed",
            "The session could not be read",
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
}

fn picked_turn<'a>(picked: Option<&str>, digest: &'a Digest) -> Option<&'a TurnRow> {
    let turns = || {
        digest.lines.iter().filter_map(|line| match line {
            Line::Turn(turn) => Some(turn),
            Line::Quiet { .. } => None,
        })
    };
    picked
        .and_then(|id| turns().find(|turn| turn.id == id))
        .or_else(|| turns().next())
}

fn turn_line(
    at: usize,
    line: &Line,
    picked: Option<&str>,
    dated: bool,
    theme: &Theme,
    cx: &mut Context<SessionScreen>,
) -> AnyElement {
    let time = |said: String| {
        div()
            .flex_none()
            .w(px(if dated { TIME_DATED } else { TIME_WIDTH }))
            .font_family(mono(theme))
            .text_color(ink(theme, T3))
            .child(said)
    };
    match line {
        Line::Quiet { count, from, to } => div()
            .h(px(TURN_ROW))
            .flex()
            .items_center()
            .gap(px(12.0))
            .px(px(10.0))
            .text_size(px(12.0))
            .text_color(ink(theme, T3))
            .child(time(to.clone()))
            .child(ellipsis(div().flex_1()).child(match count {
                1 => format!("1 quiet turn, nothing typed · {from}"),
                many => format!("{many} quiet turns, nothing typed · {from} to {to}"),
            }))
            .into_any_element(),
        Line::Turn(turn) => {
            let id = turn.id.clone();
            row(
                ("session-turn", at),
                picked == Some(turn.id.as_str()),
                false,
                theme,
            )
            .h(px(TURN_ROW))
            .gap(px(12.0))
            .px(px(10.0))
            .text_size(px(13.5))
            .child(time(turn.at.clone()))
            .child(ellipsis(div().flex_1()).child(turn.asked.clone()))
            .child(
                div()
                    .flex_none()
                    .text_size(px(12.0))
                    .text_color(ink(theme, T3))
                    .child(did(turn)),
            )
            .child(
                div()
                    .flex_none()
                    .text_size(px(12.0))
                    .text_color(state_ink(turn.state, theme))
                    .child(state_said(turn.state)),
            )
            .on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| {
                screen.picked = Some(id.clone());
                cx.notify();
            }))
            .into_any_element()
        }
    }
}

fn detail(turn: &TurnRow, theme: &Theme) -> Div {
    let mut about = vec![turn.at.clone()];
    if turn.worked_ms > 0 {
        about.push(format!("worked {}", lasted(turn.worked_ms)));
    }
    if turn.tokens > 0 {
        about.push(format!("{} tokens", compact(turn.tokens)));
    }
    about.push(did(turn));
    let more =
        |left: usize, theme: &Theme| (left > 0).then(|| note(format!("and {left} more"), theme));
    fraction(div(), 1.0, DETAIL_LEAST)
        .flex()
        .flex_col()
        .gap(px(8.0))
        .min_w_0()
        .text_size(px(13.5))
        .child(head("Turn", &about.join(" · "), theme))
        .child(
            div()
                .font_weight(FontWeight::SEMIBOLD)
                .child(turn.asked.clone()),
        )
        .child(match turn.said.clone() {
            Some(said) => div().text_color(ink(theme, T2)).child(said),
            None => note("the lead has not answered in this turn yet", theme),
        })
        .when(!turn.agents.is_empty(), |pane| {
            pane.child(caption("Sub-agents", theme))
                .children(turn.agents.iter().take(LISTED_MOST).map(|agent| {
                    div()
                        .flex()
                        .items_center()
                        .gap(px(8.0))
                        .min_w_0()
                        .child(
                            div()
                                .flex_none()
                                .font_weight(FontWeight::MEDIUM)
                                .child(agent.name.clone()),
                        )
                        .child(div().flex_none().child(note(agent.outcome.clone(), theme)))
                        .children(agent.did.clone().map(|did| ellipsis(note(did, theme))))
                }))
                .children(more(turn.agents.len().saturating_sub(LISTED_MOST), theme))
        })
        .when(!turn.files.is_empty(), |pane| {
            pane.child(caption("Files", theme)).child(
                div()
                    .flex()
                    .flex_wrap()
                    .gap(px(6.0))
                    .children(turn.files.iter().take(FILES_MOST).map(|path| {
                        div()
                            .px(px(8.0))
                            .py(px(2.0))
                            .rounded(px(6.0))
                            .bg(ink(theme, CHIP_FILL))
                            .font_family(mono(theme))
                            .text_size(px(12.0))
                            .child(file_name(path))
                    }))
                    .children(more(turn.files.len().saturating_sub(FILES_MOST), theme)),
            )
        })
}

fn figure(label: &'static str, value: String, aside: Option<String>, theme: &Theme) -> Div {
    fraction(div(), 1.0, FIGURE_LEAST)
        .flex()
        .flex_col()
        .gap(px(6.0))
        .child(caption(label, theme))
        .child(
            div()
                .flex()
                .items_baseline()
                .gap(px(8.0))
                .min_w_0()
                .child(
                    div()
                        .text_size(px(FIGURE))
                        .line_height(relative(1.0))
                        .font_weight(FontWeight::SEMIBOLD)
                        .child(value),
                )
                .children(aside.map(|aside| ellipsis(note(aside, theme)))),
        )
}

fn head(title: &'static str, hint: &str, theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(10.0))
        .min_w_0()
        .child(div().flex_none().child(caption(title, theme)))
        .child(ellipsis(note(hint.to_owned(), theme)))
}

fn divider(theme: &Theme) -> Div {
    div().h(px(1.0)).w_full().bg(ink(theme, DIVIDER_INK))
}

fn did(turn: &TurnRow) -> String {
    let counted = |count: usize, one: &str, many: &str| match count {
        0 => None,
        1 => Some(format!("1 {one}")),
        count => Some(format!("{count} {many}")),
    };
    let said: Vec<String> = [
        counted(turn.agents.len(), "sub-agent", "sub-agents"),
        counted(turn.files.len(), "file", "files"),
        counted(turn.calls, "call", "calls"),
    ]
    .into_iter()
    .flatten()
    .collect();
    match said.is_empty() {
        true => "no calls".to_owned(),
        false => said.join(" · "),
    }
}

fn state_said(state: TurnState) -> &'static str {
    match state {
        TurnState::Running => "running",
        TurnState::Finished => "finished",
        TurnState::Stopped => "stopped",
        TurnState::Failed => "failed",
        TurnState::Unreported => "",
    }
}

fn state_ink(state: TurnState, theme: &Theme) -> Rgba {
    match state {
        TurnState::Running => theme.color(ColorToken::StatusLive),
        TurnState::Failed => theme.color(ColorToken::StatusDanger),
        TurnState::Finished | TurnState::Stopped | TurnState::Unreported => ink(theme, T3),
    }
}

fn file_name(path: &str) -> String {
    path.rsplit(['/', '\\']).next().unwrap_or(path).to_owned()
}

fn clock(at: &str) -> String {
    chrono::DateTime::parse_from_rfc3339(at).map_or_else(
        |_| at.to_owned(),
        |at| {
            at.with_timezone(&chrono::Local)
                .format("%m-%d %H:%M")
                .to_string()
        },
    )
}

fn lasted(ms: i64) -> String {
    let seconds = ms.max(0) / 1000;
    match seconds {
        0..60 => format!("{seconds}s"),
        60..3600 => format!("{}m", seconds / 60),
        _ => format!("{}h {}m", seconds / 3600, seconds % 3600 / 60),
    }
}

fn compact(count: i64) -> String {
    match count {
        ..1_000 => count.to_string(),
        1_000..1_000_000 => format!("{:.1}k", count as f64 / 1e3),
        _ => format!("{:.1}M", count as f64 / 1e6),
    }
}

fn grouped_count(count: usize) -> String {
    grouped(i64::try_from(count).unwrap_or(i64::MAX))
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
    div()
        .flex()
        .flex_col()
        .gap(px(SECTION_GAP))
        .child(panes().children((0..4).map(|at| {
            fraction(div(), 1.0, FIGURE_LEAST)
                .flex()
                .flex_col()
                .gap(px(10.0))
                .child(pulse(
                    "session-count-label",
                    at,
                    skeleton_bar(0.4, 8.0, theme),
                    cx,
                ))
                .child(pulse(
                    "session-count",
                    at,
                    skeleton_bar(0.3, SKELETON_FIGURE, theme),
                    cx,
                ))
        })))
        .child(loading_turns(theme, cx))
}

fn loading_turns(theme: &Theme, cx: &App) -> Div {
    panes()
        .child(fraction(div(), 1.4, TURNS_LEAST).child(skeleton_lines(
            "session-turns",
            6,
            theme,
            cx,
        )))
        .child(fraction(div(), 1.0, DETAIL_LEAST).child(skeleton_lines(
            "session-detail",
            5,
            theme,
            cx,
        )))
}

fn warn(text: impl Into<SharedString>, theme: &Theme) -> Div {
    note(text, theme).text_color(theme.color(ColorToken::StatusWarn))
}

impl Render for SessionScreen {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = match self.source {
            None => empty_state(
                "session-no-tofu",
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
                    "session",
                    ready,
                    |cx| loading(&theme, cx).into_any_element(),
                    |cx| self.body(&theme, cx).into_any_element(),
                    cx,
                );
                self.gate = gate;
                shown
            }
        };
        let trailing = self.source.is_some().then(|| {
            div()
                .flex()
                .flex_none()
                .items_center()
                .gap(px(10.0))
                .child(self.scope_menu.clone())
                .child(reread(
                    "session",
                    self.answer.asking,
                    &theme,
                    cx,
                    SessionScreen::reread,
                ))
                .into_any_element()
        });
        window(
            titled("session", trailing),
            &theme,
            div().flex_1().flex().flex_col().child(body),
        )
    }
}
