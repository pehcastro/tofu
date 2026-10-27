use std::collections::{BTreeMap, BTreeSet};

use desk_core::protocol::boardy::EventsAnswer;
use desk_core::protocol::{ForkGeneration, SessionInfo, SessionTurns};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::caption;
use desk_ui::components::chip::{badge, mono, tabular};
use desk_ui::components::fork_chain::facts;
use desk_ui::components::paint::ink;
use desk_ui::components::size::{FONT_BODY, FONT_CHAT, FONT_SMALL, FONT_WHO, T1, T2, T3};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{Context, Div, FontWeight, SharedString, div, prelude::*, px};

use super::{ForksScreen, clock, grouped, place, warn};
use crate::screens::frame::{ellipsis, note};

const FILES_SHOWN: usize = 40;

pub(super) type Index = BTreeMap<String, BTreeSet<String>>;

pub(super) enum Loaded<T> {
    Reading,
    Read(T),
    Failed(String),
}

pub(super) struct Ran {
    name: String,
    status: String,
    said: Option<String>,
}

pub(super) struct Digest {
    first: Option<String>,
    said: Option<String>,
    agents: Vec<Ran>,
    files: Vec<(String, i64, i64)>,
    tokens: (i64, i64, i64),
    cost: f64,
}

pub(super) fn digest(answer: SessionTurns) -> Digest {
    let mut turns = answer.turns;
    turns.sort_by(|a, b| a.at.cmp(&b.at));
    let first = turns
        .iter()
        .map(|turn| turn.asked.trim())
        .find(|asked| !asked.is_empty())
        .map(str::to_owned);
    let said = turns
        .iter()
        .rev()
        .filter_map(|turn| turn.summary.as_deref().map(str::trim))
        .find(|summary| !summary.is_empty())
        .map(str::to_owned);
    let agents = turns
        .iter()
        .flat_map(|turn| &turn.agents)
        .map(|agent| Ran {
            name: agent
                .definition
                .clone()
                .unwrap_or_else(|| agent.agent.clone()),
            status: agent.status.clone(),
            said: agent
                .report
                .as_deref()
                .and_then(|report| report.lines().map(str::trim).find(|line| !line.is_empty()))
                .map(str::to_owned),
        })
        .collect();
    let mut files: BTreeMap<String, (i64, i64)> = BTreeMap::new();
    for file in turns.iter().flat_map(|turn| &turn.files) {
        let (added, removed) = files.entry(file.path.clone()).or_default();
        *added = added.saturating_add(file.added);
        *removed = removed.saturating_add(file.removed);
    }
    let tokens = turns
        .iter()
        .fold((0_i64, 0_i64, 0_i64), |(input, output, cached), turn| {
            (
                input.saturating_add(turn.tokens.input),
                output.saturating_add(turn.tokens.output),
                cached.saturating_add(turn.tokens.cache_read),
            )
        });
    Digest {
        first,
        said,
        agents,
        files: files
            .into_iter()
            .map(|(path, (added, removed))| (path, added, removed))
            .collect(),
        tokens,
        cost: turns.iter().map(|turn| turn.cost_usd).sum(),
    }
}

pub(super) fn sessions_of(answer: EventsAnswer) -> Index {
    let mut index = Index::new();
    for event in answer.events {
        let by = event.author.and_then(|author| author.session);
        for session in [event.session, by]
            .into_iter()
            .flatten()
            .filter(|session| !session.is_empty())
        {
            index
                .entry(session)
                .or_default()
                .insert(event.ticket.clone());
        }
    }
    index
}

pub(super) fn dollars(cost: f64) -> String {
    format!("${cost:.2}")
}

fn section(title: &'static str, body: impl IntoElement, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_col()
        .gap(px(6.0))
        .child(caption(title, theme))
        .child(body)
}

fn prose(text: &str, theme: &Theme) -> Div {
    div()
        .text_size(px(FONT_BODY))
        .text_color(ink(theme, T1))
        .child(text.to_owned())
}

fn pane_facts(
    info: &SessionInfo,
    row: Option<&ForkGeneration>,
    turns: Option<&Digest>,
) -> Vec<(SharedString, SharedString)> {
    let cost = turns
        .map(|turns| turns.cost)
        .or_else(|| row.map(|row| row.cost_usd))
        .or(info.cost_usd);
    let fork = row
        .and_then(|row| row.tokens_before.zip(row.tokens_after))
        .or_else(|| info.fork_tokens_before.zip(info.fork_tokens_after))
        .map(|(before, after)| format!("{} → {}", grouped(before), grouped(after)));
    let kind = row
        .and_then(|row| row.fork_kind.clone())
        .or_else(|| info.fork_into_kind.clone());
    let carried = (info.carried_messages > 0).then(|| {
        let from = info
            .carried_from
            .as_ref()
            .map_or_else(String::new, |from| format!(" from {}", from.handle));
        format!("{} messages{from}", grouped(info.carried_messages))
    });
    [
        ("generation", Some(place(info))),
        ("started", Some(clock(&info.at))),
        ("ended", info.ended_at.as_deref().map(clock)),
        ("turns", Some(grouped(info.turns))),
        ("steps", Some(grouped(info.steps))),
        ("outcome", info.outcome.clone()),
        ("end reason", info.end_reason.clone()),
        ("model", info.model.clone()),
        ("cost", cost.map(dollars)),
        ("tokens in", turns.map(|turns| grouped(turns.tokens.0))),
        ("tokens out", turns.map(|turns| grouped(turns.tokens.1))),
        ("cache read", turns.map(|turns| grouped(turns.tokens.2))),
        ("forked as", kind),
        ("tokens at fork", fork),
        ("carried", carried),
    ]
    .into_iter()
    .filter_map(|(label, value)| Some((label.into(), value?.into())))
    .collect()
}

fn ran(agent: &Ran, theme: &Theme) -> Div {
    div()
        .flex()
        .items_baseline()
        .gap(px(8.0))
        .min_w_0()
        .text_size(px(FONT_BODY))
        .child(
            div()
                .flex_none()
                .font_weight(FontWeight::MEDIUM)
                .text_color(ink(theme, T1))
                .child(agent.name.clone()),
        )
        .child(badge(agent.status.clone(), theme))
        .children(
            agent
                .said
                .clone()
                .map(|said| ellipsis(div().text_color(ink(theme, T2)).child(said))),
        )
}

fn file(path: &str, added: i64, removed: i64, theme: &Theme) -> Div {
    div()
        .flex()
        .gap(px(10.0))
        .min_w_0()
        .text_size(px(FONT_SMALL))
        .font_features(tabular())
        .child(ellipsis(
            div()
                .font_family(mono(theme))
                .text_color(ink(theme, T1))
                .child(path.to_owned()),
        ))
        .child(
            div()
                .flex_none()
                .text_color(theme.color(ColorToken::StatusLive))
                .child(format!("+{}", grouped(added))),
        )
        .child(
            div()
                .flex_none()
                .text_color(theme.color(ColorToken::StatusWarn))
                .child(format!("−{}", grouped(removed))),
        )
}

fn worked(turns: &Digest, theme: &Theme) -> Vec<Div> {
    let first = turns.first.as_deref().map_or_else(
        || note("session.turns reports no task", theme),
        |first| prose(first, theme),
    );
    let said = turns.said.as_deref().map_or_else(
        || note("session.turns reports no summary on any turn", theme),
        |said| prose(said, theme),
    );
    let agents = if turns.agents.is_empty() {
        note("no sub-agent ran", theme)
    } else {
        div()
            .flex()
            .flex_col()
            .gap(px(4.0))
            .children(turns.agents.iter().map(|agent| ran(agent, theme)))
    };
    let hidden = turns.files.len().saturating_sub(FILES_SHOWN);
    let files = if turns.files.is_empty() {
        note("no file changed", theme)
    } else {
        div()
            .flex()
            .flex_col()
            .gap(px(2.0))
            .children(
                turns
                    .files
                    .iter()
                    .take(FILES_SHOWN)
                    .map(|(path, added, removed)| file(path, *added, *removed, theme)),
            )
            .when(hidden > 0, |files| {
                files.child(note(format!("and {hidden} more"), theme))
            })
    };
    vec![
        section("First task", first, theme),
        section("Last reply", said, theme),
        section("Sub-agents", agents, theme),
        section("Files changed", files, theme),
    ]
}

impl ForksScreen {
    pub(super) fn pane(&self, id: &str, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let Some(info) = self
            .lineage
            .get(id)
            .and_then(|answer| answer.read.as_ref())
            .map(|read| &read.value)
        else {
            return div().child(note(format!("reading session.info for {id}"), theme));
        };
        let row = match &self.family {
            Some(Loaded::Read(rows)) => rows.iter().find(|row| row.session == id),
            Some(Loaded::Reading | Loaded::Failed(_)) | None => None,
        };
        let turns = match self.turns.get(id) {
            Some(Loaded::Read(turns)) => Some(turns),
            Some(Loaded::Reading | Loaded::Failed(_)) | None => None,
        };
        let opened = self.open.as_deref() == Some(id);
        let target = id.to_owned();
        let action = if opened {
            note("open in the chat", theme).into_any_element()
        } else {
            button("forks-open", "Open", None, ButtonKind::Plain, theme)
                .on_click(cx.listener(move |screen, _, _, cx| screen.open_fork(&target, cx)))
                .into_any_element()
        };
        let head = div()
            .flex()
            .items_center()
            .gap(px(10.0))
            .min_w_0()
            .child(ellipsis(
                div()
                    .text_size(px(FONT_CHAT))
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(ink(theme, T1))
                    .child(info.name.clone().unwrap_or_else(|| info.id.clone())),
            ))
            .child(
                div()
                    .flex_none()
                    .font_family(mono(theme))
                    .text_size(px(FONT_WHO))
                    .text_color(ink(theme, T3))
                    .child(info.handle.clone()),
            )
            .child(div().flex_1())
            .child(action);
        let tickets = match &self.tickets {
            Some(Loaded::Read(index)) => match index.get(id).or_else(|| index.get(&info.handle)) {
                Some(tickets) => div()
                    .flex()
                    .flex_wrap()
                    .gap(px(6.0))
                    .children(tickets.iter().map(|ticket| badge(ticket.clone(), theme))),
                None => note("no boardy event names this session", theme),
            },
            Some(Loaded::Failed(error)) => warn(format!("boardy.events: {error}"), theme),
            Some(Loaded::Reading) | None => note("reading boardy.events", theme),
        };
        let worked = match self.turns.get(id) {
            Some(Loaded::Read(turns)) => worked(turns, theme),
            Some(Loaded::Failed(error)) => vec![warn(format!("session.turns: {error}"), theme)],
            Some(Loaded::Reading) | None => {
                vec![note(
                    format!("reading session.turns for {}", info.handle),
                    theme,
                )]
            }
        };
        let mut sections = worked;
        sections.insert(sections.len().min(2), section("Tickets", tickets, theme));
        div()
            .flex()
            .flex_col()
            .gap(px(18.0))
            .min_w_0()
            .child(head)
            .child(facts(&pane_facts(info, row, turns), theme))
            .children(sections)
    }
}
