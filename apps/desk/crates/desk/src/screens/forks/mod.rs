use std::collections::{BTreeMap, BTreeSet};

use super::frame;

use crate::modules::chat::Chat;
use desk_core::model::Store;
use desk_core::protocol::SessionInfo;
use desk_core::query::{Answer, QueryError};
use desk_ui::components::avatar::spinner;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::empty::empty_state;
use desk_ui::components::fork_chain::{Fork, ForkChain, Generation};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Div, Entity, EntityId, SharedString,
    Subscription, WeakEntity, Window, div, prelude::*, px,
};

use frame::{ellipsis, load_fonts, note, panel, title, window};

const NO_TOFU: &str =
    "Forks reads session.info from the tofu the work screen runs, and no work screen is open here.";
const NO_SESSION: &str = "Open a session in the work screen. Forks walks its session.info parent back to the root and forked_into forward to the newest generation.";

type Lineage = BTreeMap<String, Answer<SessionInfo>>;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the forks screen draws what tofu session.info reports, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|_| ForksScreen {
            source: None,
            open: None,
            lineage: Lineage::new(),
        })
        .into())
}

pub struct ForksScreen {
    source: Option<Source>,
    open: Option<String>,
    lineage: Lineage,
}

struct Source {
    chat: WeakEntity<Chat>,
    id: EntityId,
    _watch: Subscription,
}

enum Link<'a> {
    Read(&'a SessionInfo),
    Waiting(&'a str, Option<&'a QueryError>),
}

struct Chain<'a> {
    links: Vec<Link<'a>>,
    missing: Vec<String>,
}

fn named(id: Option<&str>) -> Option<&str> {
    id.filter(|id| !id.is_empty())
}

fn chain<'a>(open: &'a str, lineage: &'a Lineage) -> Chain<'a> {
    let mut seen = BTreeSet::new();
    let mut missing = Vec::new();
    let mut walk = |from: Option<&'a str>, next: fn(&SessionInfo) -> Option<&str>| {
        let mut links = Vec::new();
        let mut at = from;
        while let Some(id) = at.filter(|id| seen.insert(*id)) {
            let answer = lineage.get(id);
            match answer.and_then(|answer| answer.read.as_ref()) {
                Some(read) => {
                    links.push(Link::Read(&read.value));
                    at = named(next(&read.value));
                }
                None => {
                    if answer.is_none() {
                        missing.push(id.to_owned());
                    }
                    links.push(Link::Waiting(
                        id,
                        answer.and_then(|answer| answer.failed.as_ref()),
                    ));
                    at = None;
                }
            }
        }
        links
    };
    let mut links = walk(Some(open), |info| info.parent.as_deref());
    links.reverse();
    let newer = match links.last() {
        Some(Link::Read(info)) if info.id == open => named(info.forked_into.as_deref()),
        _ => None,
    };
    links.extend(walk(newer, |info| info.forked_into.as_deref()));
    Chain { links, missing }
}

impl ForksScreen {
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
        self.saw(&store, cx);
        cx.notify();
    }

    fn chat(&self) -> Option<Entity<Chat>> {
        self.source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let Some(chat) = self.chat() else {
            return;
        };
        let open = chat.read(cx).open_id().map(str::to_owned);
        let lineage = &store.read(cx).lineage;
        let moved = open != self.open;
        let missing = open
            .as_deref()
            .map(|open| chain(open, lineage).missing)
            .unwrap_or_default();
        if !moved && missing.is_empty() && *lineage == self.lineage {
            return;
        }
        self.lineage = lineage.clone();
        self.open = open;
        if let Some(open) = &self.open {
            let shown = chain(open, &self.lineage);
            if shown.missing.is_empty()
                && shown.links.iter().all(|link| matches!(link, Link::Read(_)))
            {
                eprintln!("desk: forks shows {}", said(&shown.links));
            }
        }
        let wanted = match (moved, &self.open) {
            (true, Some(open)) => vec![open.clone()],
            (true, None) => Vec::new(),
            (false, _) => missing,
        };
        chat.update(cx, |chat, cx| {
            if moved {
                chat.forget_lineage(cx);
            }
            for session in &wanted {
                chat.want_lineage(session, cx);
            }
        });
        cx.notify();
    }

    fn reread(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self.chat() else {
            return eprintln!("desk: forks: the chat is gone, so nothing was asked");
        };
        eprintln!("desk: forks: read again");
        chat.update(cx, |chat, cx| chat.reread_lineage(cx));
    }

    fn header(&self, links: &[Link], theme: &Theme, cx: &mut Context<Self>) -> Div {
        let asking = self.lineage.values().any(|answer| answer.asking);
        let read = links.iter().rev().find_map(|link| match link {
            Link::Read(info) => self
                .lineage
                .get(&info.id)
                .and_then(|answer| answer.read.as_ref())
                .map(|read| (info, clock(&read.at))),
            Link::Waiting(..) => None,
        });
        let read = read.map(|(info, at)| {
            let generations = links.len();
            format!(
                "{} generation{} of {}, session.info read {at}",
                generations,
                if generations == 1 { "" } else { "s" },
                info.name.as_deref().unwrap_or(&info.id)
            )
        });
        div()
            .flex()
            .flex_none()
            .flex_wrap()
            .items_center()
            .gap(px(10.0))
            .px(px(4.0))
            .child(title("Forks"))
            .children(read.map(|read| ellipsis(note(read, theme))))
            .child(div().flex_1())
            .when(asking, |header| {
                header.child(spinner("forks-asking", theme))
            })
            .child(
                button("forks-reread", "Read again", None, ButtonKind::Plain, theme)
                    .on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.reread(cx))),
            )
    }

    fn body(&self, open: &str, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let shown = chain(open, &self.lineage);
        let never = match shown.links.as_slice() {
            [Link::Read(info)]
                if named(info.parent.as_deref()).is_none()
                    && named(info.forked_into.as_deref()).is_none() =>
            {
                Some(format!(
                    "{} was never forked: session.info names no parent and no forked_into",
                    info.name.as_deref().unwrap_or(&info.id)
                ))
            }
            _ => None,
        };
        let failures = shown.links.iter().filter_map(|link| match link {
            Link::Waiting(id, Some(error)) => Some(warn(format!("{id}: {error}"), theme)),
            Link::Waiting(..) | Link::Read(_) => None,
        });
        div()
            .flex()
            .flex_col()
            .gap(px(8.0))
            .child(self.header(&shown.links, theme, cx))
            .children(failures)
            .child(
                panel("Generations, root first", None, theme).child(
                    div()
                        .px(px(6.0))
                        .pt(px(4.0))
                        .min_w_0()
                        .child(ForkChain::new(theme, generations(open, &shown.links)))
                        .children(never.map(|never| div().pt(px(10.0)).child(note(never, theme)))),
                ),
            )
            .child(note(
                "every value is a field of session.info, one read per generation",
                theme,
            ))
    }
}

fn generations(open: &str, links: &[Link]) -> Vec<Generation> {
    let mut older: Option<&SessionInfo> = None;
    let mut out = Vec::with_capacity(links.len());
    for (at, link) in links.iter().enumerate() {
        let info = match link {
            Link::Read(info) => Some(*info),
            Link::Waiting(..) => None,
        };
        let into = (at > 0).then(|| fork(older, info));
        out.push(match link {
            Link::Read(info) => Generation {
                name: info.name.clone().unwrap_or_else(|| info.id.clone()).into(),
                handle: Some(info.handle.clone().into()),
                place: place(info).into(),
                current: info.id == open,
                head: info.head == Some(true),
                facts: facts(info),
                into,
            },
            Link::Waiting(id, failed) => Generation {
                name: SharedString::from((*id).to_owned()),
                handle: None,
                place: if failed.is_some() {
                    "session.info failed".into()
                } else {
                    "reading session.info".into()
                },
                current: *id == open,
                head: false,
                facts: Vec::new(),
                into,
            },
        });
        older = info;
    }
    out
}

fn place(info: &SessionInfo) -> String {
    match (info.generation, info.generations) {
        (Some(at), Some(of)) => format!("generation {at} of {of}"),
        (Some(at), None) => format!("generation {at}"),
        (None, _) => "no generation recorded".to_owned(),
    }
}

fn fork(older: Option<&SessionInfo>, newer: Option<&SessionInfo>) -> Fork {
    let kind = newer
        .and_then(|info| info.fork_kind.clone())
        .or_else(|| older.and_then(|info| info.fork_into_kind.clone()))
        .unwrap_or_else(|| "fork".to_owned());
    let counts = older.and_then(|info| info.fork_tokens_before.zip(info.fork_tokens_after));
    let ceiling = older
        .and_then(|info| info.context_ceiling)
        .filter(|ceiling| *ceiling > 0);
    Fork {
        kind: kind.into(),
        tokens: counts.map(|(before, after)| {
            format!(
                "{} → {} tokens, {} dropped",
                grouped(before),
                grouped(after),
                grouped(before.saturating_sub(after))
            )
            .into()
        }),
        fill: counts.zip(ceiling).map(|((before, after), ceiling)| {
            let share = |tokens: i64| (tokens as f64 / ceiling as f64) as f32;
            (share(before), share(after))
        }),
    }
}

fn facts(info: &SessionInfo) -> Vec<(SharedString, SharedString)> {
    [
        ("turns", Some(grouped(info.turns))),
        ("steps", Some(grouped(info.steps))),
        ("outcome", info.outcome.clone()),
        ("end reason", info.end_reason.clone()),
        ("started", Some(clock(&info.at))),
        ("ended", info.ended_at.as_deref().map(clock)),
    ]
    .into_iter()
    .filter_map(|(label, value)| Some((label.into(), value?.into())))
    .collect()
}

fn said(links: &[Link]) -> String {
    links
        .iter()
        .filter_map(|link| match link {
            Link::Read(info) => Some(format!(
                "[{} {} generation {:?} parent {:?} forked_into {:?} fork_kind {:?} fork_into_kind {:?} tokens {:?} to {:?} turns {} steps {} outcome {:?} end_reason {:?} head {:?}]",
                info.handle,
                info.id,
                info.generation,
                info.parent,
                info.forked_into,
                info.fork_kind,
                info.fork_into_kind,
                info.fork_tokens_before,
                info.fork_tokens_after,
                info.turns,
                info.steps,
                info.outcome,
                info.end_reason,
                info.head
            )),
            Link::Waiting(..) => None,
        })
        .collect::<Vec<_>>()
        .join(" ")
}

fn clock(stamp: &str) -> String {
    chrono::DateTime::parse_from_rfc3339(stamp).map_or_else(
        |_| stamp.to_owned(),
        |at| {
            at.with_timezone(&chrono::Local)
                .format("%Y-%m-%d %H:%M:%S")
                .to_string()
        },
    )
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

impl Render for ForksScreen {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let empty = |id: &'static str, head: &'static str, text: &'static str| {
            empty_state(id, head, Some(text.into()), &[], &[], &theme, |_, _, _| {})
                .into_any_element()
        };
        let body = match (&self.source, self.open.clone()) {
            (None, _) => empty("forks-no-tofu", "No tofu to ask", NO_TOFU),
            (Some(_), None) => empty("forks-no-session", "No session open", NO_SESSION),
            (Some(_), Some(open)) => self.body(&open, &theme, cx).into_any_element(),
        };
        window(&theme, div().flex_1().flex().flex_col().child(body))
    }
}
