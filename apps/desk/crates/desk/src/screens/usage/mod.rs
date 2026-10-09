use super::frame;

use std::collections::BTreeMap;

use crate::modules::chat::{Chat, Listed};
use desk_core::model::{Session, Store};
use desk_core::protocol::{SessionInfo, SessionTrace};
use desk_ui::components::card::{caption, inner_card, outer_card};
use desk_ui::components::charts::{
    BarLayout, BarLook, Bars, DotColumns, MINT, Said, Series, cached, hues,
};
use desk_ui::components::chip::mono;
use desk_ui::components::empty::empty_state;
use desk_ui::components::paint::ink;
use desk_ui::components::size::T2;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::Theme;
use gpui::{
    AnyElement, AnyView, App, AppContext, Context, Div, Entity, EntityId, FontWeight, SharedString,
    Subscription, WeakEntity, Window, div, prelude::*, px, relative, rgb,
};

use frame::{ellipsis, fraction, load_fonts, note, panel, panes, titled, window};

const NO_TOFU: &str =
    "Usage reads the session the work screen has open, and no work screen is open here.";
const NO_SESSION: &str =
    "Usage shows the session the work screen has open. Open one from the sidebar.";
const NOTHING_YET: &str = "tofu sends usage.updated once per model call. Run a turn in this session and its calls show here as they arrive.";
const NOT_REPLAYED: &str = "Calls before the desk opened this session come from session.trace requests; calls after come from usage.updated as they arrive.";
const LEAD: &str = "lead";
const COUNT_LEAST: f32 = 120.0;
const ACTIVITY_LEAST: f32 = 360.0;
const WENT_LEAST: f32 = 280.0;
const SPLIT_LEAST: f32 = 300.0;
const FIGURE: f32 = 24.0;
const TOTAL: f32 = 40.0;
const VALUE_ROW: f32 = 18.0;
const VALUE_GAP: f32 = 8.0;
const VALUE_WIDTH: f32 = 64.0;
const ACTIVITY_COLUMNS: usize = 24;
const ACTIVITY_CELLS: i64 = 10;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the usage screen draws the open session's usage from tofu, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|_| UsageScreen {
            source: None,
            seen: Seen::default(),
            logged: BTreeMap::new(),
            charts: None,
        })
        .into())
}

pub struct UsageScreen {
    source: Option<Source>,
    seen: Seen,
    logged: BTreeMap<String, usize>,
    charts: Option<Charts>,
}

struct Source {
    chat: WeakEntity<Chat>,
    id: EntityId,
    _watch: Subscription,
    _listed: Subscription,
}

struct Charts {
    activity: Entity<DotColumns>,
    went: Split,
    models: Split,
    agents: Split,
}

struct Split {
    chart: Entity<Bars>,
    rows: Vec<(String, i64)>,
}

#[derive(Default, PartialEq)]
struct Seen {
    session: Option<String>,
    opened: String,
    whole: Option<Whole>,
    calls: Vec<Call>,
    decisions: i64,
}

#[derive(PartialEq)]
struct Whole {
    read_at: String,
    turns: i64,
    steps: i64,
    sub_agents: i64,
    cost_usd: Option<f64>,
    model: Option<String>,
}

#[derive(Clone, PartialEq)]
struct Call {
    turn: String,
    agent: Option<String>,
    model: String,
    fresh: i64,
    out: i64,
    cache_read: i64,
}

impl Call {
    fn tokens(&self) -> i64 {
        self.fresh.saturating_add(self.out)
    }

    fn spender(&self) -> &str {
        self.agent.as_deref().unwrap_or(LEAD)
    }
}

#[derive(Default, Clone, Copy)]
struct Running {
    fresh: i64,
    out: i64,
    cache_read: i64,
    decisions: i64,
}

fn calls(session: &Session) -> (Vec<Call>, i64) {
    let mut turns: BTreeMap<&str, Running> = BTreeMap::new();
    let mut calls = Vec::with_capacity(session.usage.len());
    for usage in &session.usage {
        let before = turns.entry(&usage.turn).or_default();
        let step = |now: i64, was: i64| now.saturating_sub(was).max(0);
        let agent = usage
            .agent
            .as_ref()
            .filter(|agent| !agent.is_empty())
            .map(|agent| {
                session
                    .agents
                    .get(agent)
                    .map_or_else(|| agent.clone(), |known| known.kind.clone())
            });
        calls.push(Call {
            turn: usage.turn.clone(),
            agent,
            model: usage.model.clone(),
            fresh: step(usage.tokens_in, before.fresh),
            out: step(usage.tokens_out, before.out),
            cache_read: step(usage.cache_read, before.cache_read),
        });
        *before = Running {
            fresh: usage.tokens_in.max(before.fresh),
            out: usage.tokens_out.max(before.out),
            cache_read: usage.cache_read.max(before.cache_read),
            decisions: usage.decisions.max(before.decisions),
        };
    }
    let decisions = turns
        .values()
        .fold(0, |sum: i64, turn| sum.saturating_add(turn.decisions));
    (calls, decisions)
}

fn whole(info: &SessionInfo, read_at: &str) -> Whole {
    Whole {
        read_at: read_at.to_owned(),
        turns: info.turns,
        steps: info.steps,
        sub_agents: info.sub_agents,
        cost_usd: info.cost_usd,
        model: info.model.clone(),
    }
}

fn summed<'a>(calls: impl Iterator<Item = (&'a str, i64)>) -> Vec<(String, i64)> {
    let mut sums: BTreeMap<&str, i64> = BTreeMap::new();
    for (key, tokens) in calls {
        let sum = sums.entry(key).or_default();
        *sum = sum.saturating_add(tokens);
    }
    let mut sums: Vec<(String, i64)> = sums
        .into_iter()
        .map(|(key, tokens)| (key.to_owned(), tokens))
        .collect();
    sums.sort_by_key(|row| std::cmp::Reverse(row.1));
    sums
}

fn went(calls: &[Call]) -> Vec<(String, i64)> {
    let spent_by = |sub_agent: bool| {
        calls
            .iter()
            .filter(|call| call.agent.is_some() == sub_agent)
            .fold(0, |sum: i64, call| sum.saturating_add(call.tokens()))
    };
    vec![
        (LEAD.to_owned(), spent_by(false)),
        ("sub-agents".to_owned(), spent_by(true)),
    ]
}

fn split(rows: Vec<(String, i64)>, cx: &mut Context<UsageScreen>) -> Split {
    let total = rows
        .iter()
        .fold(0, |sum: i64, row| sum.saturating_add(row.1))
        .max(1);
    let tips = rows
        .iter()
        .map(|(name, tokens)| {
            Said::titled(name.clone())
                .row("tokens", grouped(*tokens), None)
                .foot(
                    "share",
                    format!("{}%", tokens.saturating_mul(100) / total),
                    None,
                )
        })
        .collect();
    let series = vec![Series {
        name: "tokens".into(),
        color: rgb(MINT),
        values: rows.iter().map(|row| row.1 as f32).collect(),
    }];
    let labels = rows.iter().map(|row| row.0.clone().into()).collect();
    let colors = hues(rows.iter().map(|row| row.0.as_str()));
    Split {
        chart: cx.new(|_| {
            Bars::new(series, labels, tips, BarLook::Gradient, BarLayout::Rows).hued(colors)
        }),
        rows,
    }
}

fn traced(trace: &SessionTrace) -> Vec<Call> {
    trace
        .requests
        .iter()
        .map(|request| Call {
            turn: request.turn.clone(),
            agent: request
                .agent
                .as_ref()
                .filter(|agent| !agent.is_empty())
                .map(|agent| {
                    trace
                        .agents
                        .iter()
                        .find(|run| &run.agent == agent)
                        .and_then(|run| run.definition.clone())
                        .unwrap_or_else(|| agent.clone())
                }),
            model: request
                .model
                .clone()
                .unwrap_or_else(|| "not reported".to_owned()),
            fresh: request.usage.input_tokens,
            out: request.usage.output_tokens,
            cache_read: request.usage.cache_read_tokens,
        })
        .collect()
}

fn activity(calls: &[Call]) -> DotColumns {
    let shown = calls.len().min(ACTIVITY_COLUMNS);
    let first = calls.len() - shown;
    let recent = calls.get(first..).unwrap_or_default();
    let peak = recent.iter().map(Call::tokens).max().unwrap_or(0).max(1);
    let padding = ACTIVITY_COLUMNS - shown;
    let mut heights = vec![0; padding];
    let mut tips = vec![Said::titled("no call yet"); padding];
    for (at, call) in recent.iter().enumerate() {
        let tokens = call.tokens();
        let cells = (tokens.saturating_mul(ACTIVITY_CELLS) + peak - 1) / peak;
        heights
            .push(usize::try_from(cells.clamp(i64::from(tokens > 0), ACTIVITY_CELLS)).unwrap_or(0));
        tips.push(
            Said::titled(format!("call {} · {}", first + at + 1, call.spender()))
                .row("fresh in", grouped(call.fresh), None)
                .row("out", grouped(call.out), None)
                .row("cache read", grouped(call.cache_read), None)
                .note(format!("{} · turn {}", call.model, call.turn)),
        );
    }
    let from = if shown == 0 {
        "no call yet".to_owned()
    } else {
        format!("call {}", first + 1)
    };
    DotColumns::new(heights, tips, (from.into(), "latest".into()))
}

impl UsageScreen {
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
        let listed = cx.subscribe(chat, |screen, chat, _: &Listed, cx| {
            let store = chat.read(cx).store().clone();
            screen.saw(&store, cx);
        });
        self.source = Some(Source {
            chat: chat.downgrade(),
            id: chat.entity_id(),
            _watch: watch,
            _listed: listed,
        });
        chat.update(cx, |chat, cx| chat.want_info(cx));
        self.saw(&store, cx);
        cx.notify();
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let chat = self
            .source
            .as_ref()
            .and_then(|source| source.chat.upgrade());
        let open = chat
            .as_ref()
            .and_then(|chat| chat.read(cx).open_id().map(str::to_owned));
        let name = chat.as_ref().and_then(|chat| {
            let open = open.as_deref()?;
            chat.read(cx)
                .rows()
                .iter()
                .find(|row| row.id == open)
                .map(|row| row.title().to_owned())
        });
        let store = store.read(cx);
        for (id, session) in &store.sessions {
            let logged = self.logged.entry(id.clone()).or_default();
            for usage in session.usage.iter().skip(*logged) {
                eprintln!(
                    "desk: usage.updated session {} turn {} seq {} agent {} model {} tokensIn {} tokensOut {} cacheRead {} decisions {}",
                    usage.session,
                    usage.turn,
                    usage.seq,
                    usage.agent.as_deref().unwrap_or(LEAD),
                    usage.model,
                    usage.tokens_in,
                    usage.tokens_out,
                    usage.cache_read,
                    usage.decisions
                );
            }
            *logged = session.usage.len();
        }
        let session = open.as_ref().and_then(|open| store.sessions.get(open));
        let (live, decisions) = session.map(calls).unwrap_or_default();
        let traced = store
            .trace
            .read
            .as_ref()
            .filter(|read| open.as_deref() == Some(read.value.session.as_str()))
            .map(|read| traced(&read.value))
            .unwrap_or_default();
        let from_trace = traced.len();
        let traced_turns: std::collections::BTreeSet<String> =
            traced.iter().map(|call| call.turn.clone()).collect();
        let mut calls = traced;
        calls.extend(
            live.into_iter()
                .filter(|call| !traced_turns.contains(&call.turn)),
        );
        let whole = store
            .info
            .read
            .as_ref()
            .filter(|read| open.as_deref() == Some(read.value.id.as_str()))
            .map(|read| whole(&read.value, &read.at));
        let seen = Seen {
            session: open.map(|open| {
                name.or_else(|| {
                    session
                        .map(|session| session.name.clone())
                        .filter(|name| !name.is_empty())
                })
                .unwrap_or(open)
            }),
            opened: chrono::DateTime::<chrono::Local>::from(store.opened)
                .format("%H:%M")
                .to_string(),
            whole,
            calls,
            decisions,
        };
        if seen == self.seen {
            return;
        }
        if seen.whole != self.seen.whole
            && let Some(whole) = &seen.whole
        {
            eprintln!(
                "desk: usage shows session.info read {} for {}: turns {}, steps {}, sub_agents {}, cost_usd {:?}, model {:?}",
                whole.read_at,
                seen.session.as_deref().unwrap_or(""),
                whole.turns,
                whole.steps,
                whole.sub_agents,
                whole.cost_usd,
                whole.model
            );
        }
        if seen.calls != self.seen.calls {
            let tokens = seen
                .calls
                .iter()
                .fold(0, |sum: i64, call| sum.saturating_add(call.tokens()));
            eprintln!(
                "desk: usage draws {} calls ({} from session.trace) of {} with {} tokens and {} decisions",
                seen.calls.len(),
                from_trace,
                seen.session.as_deref().unwrap_or("no session"),
                tokens,
                seen.decisions
            );
            self.charts = (!seen.calls.is_empty()).then(|| {
                let calls = &seen.calls;
                Charts {
                    activity: cx.new(|_| activity(calls)),
                    went: split(went(calls), cx),
                    models: split(
                        summed(
                            calls
                                .iter()
                                .map(|call| (call.model.as_str(), call.tokens())),
                        ),
                        cx,
                    ),
                    agents: split(
                        summed(calls.iter().map(|call| (call.spender(), call.tokens()))),
                        cx,
                    ),
                }
            });
        }
        self.seen = seen;
        cx.notify();
    }

    fn about(&self, session: &str, theme: &Theme) -> Div {
        ellipsis(note(
            format!(
                "{session} · totals from session.info, calls from session.trace and usage.updated since {}",
                self.seen.opened
            ),
            theme,
        ))
        .px(px(4.0))
    }

    fn whole_row(&self, theme: &Theme) -> Div {
        let Some(whole) = &self.seen.whole else {
            return note("reading session.info for the whole session", theme);
        };
        let cost = whole.cost_usd.map_or_else(
            || "not reported".to_owned(),
            |cost| format!("{cost:.2} USD"),
        );
        panes().flex_none().children(
            [
                ("Turns", grouped(whole.turns), 1.0),
                ("Sub-agents", grouped(whole.sub_agents), 1.0),
                ("Steps", grouped(whole.steps), 1.0),
                ("Cost", cost, 1.0),
                (
                    "Model",
                    whole
                        .model
                        .clone()
                        .unwrap_or_else(|| "not reported".to_owned()),
                    2.0,
                ),
            ]
            .into_iter()
            .map(|(label, value, grow)| count(label, value, grow, theme)),
        )
    }

    fn activity_panel(&self, theme: &Theme, cx: &App) -> Div {
        let panel = panel(
            "Activity",
            Some(note("tokens per model call", theme).into_any_element()),
            theme,
        )
        .map(|panel| fraction(panel, 2.0, ACTIVITY_LEAST));
        let Some(charts) = &self.charts else {
            return panel.child(empty_state(
                "usage-nothing-yet",
                "No model call in this session yet",
                Some(NOTHING_YET.into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            ));
        };
        let tokens = self
            .seen
            .calls
            .iter()
            .fold(0, |sum: i64, call| sum.saturating_add(call.tokens()));
        let cache = self
            .seen
            .calls
            .iter()
            .fold(0, |sum: i64, call| sum.saturating_add(call.cache_read));
        panel.child(
            inner_card(theme)
                .flex_col()
                .gap(px(14.0))
                .p(px(16.0))
                .child(
                    div()
                        .flex()
                        .flex_wrap()
                        .items_baseline()
                        .gap_x(px(10.0))
                        .child(
                            div()
                                .text_size(px(TOTAL))
                                .line_height(relative(1.0))
                                .font_weight(FontWeight::SEMIBOLD)
                                .child(short(tokens)),
                        )
                        .child(note("tokens", theme))
                        .child(div().flex_1())
                        .child(note(
                            format!(
                                "{} calls · {} cache read",
                                self.seen.calls.len(),
                                short(cache)
                            ),
                            theme,
                        )),
                )
                .child(cached(&charts.activity, cx)),
        )
    }

    fn went_panel(&self, went: &Split, theme: &Theme, cx: &App) -> Div {
        panel(
            "Where it went",
            Some(note("by who spent it", theme).into_any_element()),
            theme,
        )
        .map(|panel| fraction(panel, 1.0, WENT_LEAST))
        .child(
            inner_card(theme)
                .flex_col()
                .gap(px(14.0))
                .p(px(14.0))
                .child(valued(went, theme, cx))
                .child(note(
                    format!(
                        "classifier: {} decisions, its tokens are not on usage.updated",
                        grouped(self.seen.decisions)
                    ),
                    theme,
                )),
        )
    }

    fn body(&self, session: &str, theme: &Theme, cx: &App) -> Div {
        let split_panel = |name: &'static str, split: &Split| {
            panel(name, None, theme)
                .map(|panel| fraction(panel, 1.0, SPLIT_LEAST))
                .child(
                    inner_card(theme)
                        .p(px(14.0))
                        .child(valued(split, theme, cx)),
                )
        };
        let top = panes().child(self.activity_panel(theme, cx)).children(
            self.charts
                .as_ref()
                .map(|charts| self.went_panel(&charts.went, theme, cx)),
        );
        let bottom = self.charts.as_ref().map(|charts| {
            panes()
                .child(split_panel("By model", &charts.models))
                .child(split_panel("By agent", &charts.agents))
        });
        div()
            .flex()
            .flex_col()
            .gap(px(8.0))
            .child(self.about(session, theme))
            .child(self.whole_row(theme))
            .child(top)
            .children(bottom)
            .child(note(NOT_REPLAYED, theme))
    }
}

fn valued(split: &Split, theme: &Theme, cx: &App) -> Div {
    let rows = &split.rows;
    div()
        .flex()
        .gap(px(10.0))
        .child(div().flex_1().min_w_0().child(cached(&split.chart, cx)))
        .child(
            div()
                .w(px(VALUE_WIDTH))
                .flex_none()
                .flex()
                .flex_col()
                .items_end()
                .gap(px(VALUE_GAP))
                .font_family(mono(theme))
                .text_size(px(12.0))
                .line_height(px(VALUE_ROW))
                .text_color(ink(theme, T2))
                .children(
                    rows.iter()
                        .map(|row| div().h(px(VALUE_ROW)).child(short(row.1))),
                ),
        )
}

fn count(label: &'static str, value: String, grow: f32, theme: &Theme) -> Div {
    fraction(outer_card(theme), grow, COUNT_LEAST * grow)
        .flex_col()
        .px(px(3.0))
        .pb(px(3.0))
        .child(
            div()
                .h(px(30.0))
                .flex()
                .items_center()
                .pl(px(9.0))
                .child(caption(label, theme)),
        )
        .child(
            ellipsis(inner_card(theme))
                .px(px(14.0))
                .py(px(10.0))
                .text_size(px(FIGURE))
                .line_height(relative(1.0))
                .font_weight(FontWeight::SEMIBOLD)
                .child(value),
        )
}

fn short(count: i64) -> SharedString {
    let value = count as f64;
    match count.unsigned_abs() {
        0..1_000 => count.to_string().into(),
        1_000..1_000_000 => format!("{:.1}k", value / 1e3).into(),
        _ => format!("{:.1}M", value / 1e6).into(),
    }
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

fn empty(id: &'static str, heading: &'static str, why: &'static str, theme: &Theme) -> AnyElement {
    empty_state(id, heading, Some(why.into()), &[], &[], theme, |_, _, _| {}).into_any_element()
}

impl Render for UsageScreen {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = match (&self.source, &self.seen.session) {
            (None, _) => empty("usage-no-tofu", "No tofu to ask", NO_TOFU, &theme),
            (Some(_), None) => empty("usage-no-session", "No session open", NO_SESSION, &theme),
            (Some(_), Some(session)) => self.body(session, &theme, cx).into_any_element(),
        };
        window(
            titled("usage", None),
            &theme,
            div().flex_1().flex().flex_col().child(body),
        )
    }
}
