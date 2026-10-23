use std::collections::{BTreeMap, BTreeSet};
use std::time::{Duration, Instant};

use super::frame;

use crate::modules::chat::Chat;
use desk_core::model::Store;
use desk_core::protocol::{CarriedFrom, SessionInfo};
use desk_core::query::{Answer, QueryError};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::empty::empty_state;
use desk_ui::components::fork_chain::{Fork, ForkChain, Generation};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyView, App, AppContext, Context, Div, Entity, EntityId, SharedString, Subscription,
    WeakEntity, Window, div, prelude::*, px,
};

use frame::{ellipsis, load_fonts, note, reread, titled, window};

const NO_TOFU: &str =
    "Forks reads session.info from the tofu the work screen runs, and no work screen is open here.";
const NO_SESSION: &str = "Open a session in the work screen. Forks walks its session.info parent back toward the root, ten generations at a time.";
const PAGE: usize = 10;
const OPEN_PATIENCE: Duration = Duration::from_secs(10);

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
            shown: PAGE,
            asked: None,
            queued: None,
        })
        .into())
}

pub struct ForksScreen {
    source: Option<Source>,
    open: Option<String>,
    lineage: Lineage,
    shown: usize,
    asked: Option<Instant>,
    queued: Option<String>,
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

impl<'a> Link<'a> {
    fn info(&self) -> Option<&'a SessionInfo> {
        match self {
            Link::Read(info) => Some(info),
            Link::Waiting(..) => None,
        }
    }
}

struct Chain<'a> {
    links: Vec<Link<'a>>,
    missing: Vec<String>,
    earlier: bool,
}

fn named(id: Option<&str>) -> Option<&str> {
    id.filter(|id| !id.is_empty())
}

fn older(info: &SessionInfo) -> Option<&str> {
    named(info.parent.as_deref()).or_else(|| carried(info).map(|from| from.session.as_str()))
}

fn carried(info: &SessionInfo) -> Option<&CarriedFrom> {
    info.carried_from
        .as_ref()
        .filter(|from| !from.session.is_empty())
}

fn chain<'a>(open: &'a str, lineage: &'a Lineage, shown: usize) -> Chain<'a> {
    let known: BTreeMap<&str, &SessionInfo> = lineage
        .values()
        .filter_map(|answer| answer.read.as_ref())
        .map(|read| (read.value.id.as_str(), &read.value))
        .collect();
    let mut seen = BTreeSet::new();
    let mut links = Vec::new();
    let mut missing = Vec::new();
    let mut at = Some(open);
    while let Some(id) = at.filter(|id| links.len() < shown && seen.insert(*id)) {
        match known.get(id).copied() {
            Some(info) => {
                links.push(Link::Read(info));
                at = older(info);
            }
            None => {
                let answer = lineage.get(id);
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
    let earlier = at.is_some_and(|id| !seen.contains(id));
    Chain {
        links,
        missing,
        earlier,
    }
}

fn earlier_handles(handle: &str, generation: Option<i64>, shown: usize) -> Vec<String> {
    let Some((family, number)) = handle.rsplit_once('.') else {
        return Vec::new();
    };
    let Some(generation) = generation.filter(|generation| number.parse().ok() == Some(*generation))
    else {
        return Vec::new();
    };
    (1..shown)
        .map_while(|back| {
            let older = generation.checked_sub(i64::try_from(back).ok()?)?;
            (older >= 1).then(|| format!("{family}.{older}"))
        })
        .collect()
}

fn first_page(open: &str, chat: &Chat, store: &Store) -> Vec<String> {
    let info = store
        .info
        .read
        .as_ref()
        .map(|read| &read.value)
        .filter(|info| info.id == open)
        .map(|info| ("session.info", info.handle.as_str(), info.generation));
    let row = chat
        .rows()
        .iter()
        .find(|row| row.id == open)
        .map(|row| ("session.list", row.handle.as_str(), row.generation));
    let earlier = [info, row]
        .into_iter()
        .flatten()
        .map(|(from, handle, generation)| (from, earlier_handles(handle, generation, PAGE)))
        .find(|(_, handles)| !handles.is_empty());
    let from = earlier.as_ref().map_or("nothing yet", |(from, _)| from);
    let wanted: Vec<String> = std::iter::once(open.to_owned())
        .chain(earlier.into_iter().flat_map(|(_, handles)| handles))
        .collect();
    eprintln!(
        "desk: forks asks {} session.info at once, handles from {from}",
        wanted.len()
    );
    wanted
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
        let open = store.read(cx).open.clone();
        let lineage = &store.read(cx).lineage;
        let moved = open != self.open;
        if !moved && *lineage == self.lineage {
            return;
        }
        self.lineage = if moved {
            Lineage::new()
        } else {
            lineage.clone()
        };
        self.open = open;
        if moved {
            self.shown = PAGE;
            self.asked = Some(Instant::now());
        }
        self.told();
        let wanted = match (moved, &self.open) {
            (true, Some(open)) => first_page(open, chat.read(cx), store.read(cx)),
            (false, Some(open)) => self.wanted(open),
            (_, None) => Vec::new(),
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
        if moved
            && let Some(open) = self.open.clone()
            && let Some(queued) = self.queued.take()
            && queued != open
        {
            eprintln!("desk: forks: the chat finished opening {open}, so {queued} opens now");
            self.open_fork(&queued, cx);
        }
    }

    fn told(&mut self) {
        let Some(open) = &self.open else {
            return;
        };
        let shown = chain(open, &self.lineage, self.shown);
        if !shown.links.iter().all(|link| matches!(link, Link::Read(_))) {
            return;
        }
        eprintln!("desk: forks shows {}", said(&shown.links));
        if let Some(asked) = self.asked.take() {
            eprintln!(
                "desk: forks first {} in {} ms",
                shown.links.len(),
                asked.elapsed().as_millis()
            );
        }
    }

    fn wanted(&self, open: &str) -> Vec<String> {
        let shown = chain(open, &self.lineage, self.shown);
        let handles: Vec<String> = match shown.links.first() {
            Some(Link::Read(current)) => {
                earlier_handles(&current.handle, current.generation, self.shown)
                    .into_iter()
                    .filter(|handle| !self.lineage.contains_key(handle))
                    .collect()
            }
            Some(Link::Waiting(..)) | None => Vec::new(),
        };
        let asking = self.lineage.values().any(|answer| answer.asking);
        if handles.is_empty() && !asking {
            shown.missing
        } else {
            handles
        }
    }

    fn earlier(&mut self, cx: &mut Context<Self>) {
        let (Some(chat), Some(open)) = (self.chat(), self.open.clone()) else {
            return eprintln!("desk: forks: no chat or no session, so nothing earlier was read");
        };
        self.shown = self.shown.saturating_add(PAGE);
        self.asked = Some(Instant::now());
        eprintln!("desk: forks: load {PAGE} earlier, {} shown", self.shown);
        let wanted = self.wanted(&open);
        chat.update(cx, |chat, cx| {
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

    fn about(&self, open: &str, links: &[Link]) -> String {
        let Some(read) = self
            .lineage
            .get(open)
            .and_then(|answer| answer.read.as_ref())
        else {
            return format!("reading session.info for {open}");
        };
        let info = &read.value;
        let of = info
            .generation
            .map_or_else(String::new, |at| format!(" of {}", grouped(at)));
        format!(
            "{}{of} generation{} of {}, current first, session.info read {}",
            links.len(),
            if links.len() == 1 && of.is_empty() {
                ""
            } else {
                "s"
            },
            info.name.as_deref().unwrap_or(&info.id),
            clock(&read.at)
        )
    }

    fn body(&self, open: &str, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let shown = chain(open, &self.lineage, self.shown);
        let never = match shown.links.as_slice() {
            [Link::Read(info)]
                if older(info).is_none() && named(info.forked_into.as_deref()).is_none() =>
            {
                Some(format!(
                    "{} was never forked: session.info names no parent, no carried_from and no forked_into",
                    info.name.as_deref().unwrap_or(&info.id)
                ))
            }
            _ => None,
        };
        let failures = shown.links.iter().filter_map(|link| match link {
            Link::Waiting(id, Some(error)) => Some(warn(format!("{id}: {error}"), theme)),
            Link::Waiting(..) | Link::Read(_) => None,
        });
        let ids: Vec<String> = shown
            .links
            .iter()
            .map(|link| match link {
                Link::Read(info) => info.id.clone(),
                Link::Waiting(id, _) => (*id).to_owned(),
            })
            .collect();
        let chain = ForkChain::new(theme, generations(open, &shown.links)).on_open(cx.listener(
            move |screen, at: &usize, _, cx| {
                if let Some(id) = ids.get(*at) {
                    screen.open_fork(id, cx);
                }
            },
        ));
        let earlier = shown.earlier.then(|| {
            div().px(px(4.0)).child(
                button(
                    "forks-earlier",
                    format!("Load {PAGE} earlier"),
                    None,
                    ButtonKind::Plain,
                    theme,
                )
                .on_click(cx.listener(|screen, _, _, cx| screen.earlier(cx))),
            )
        });
        div()
            .flex()
            .flex_col()
            .gap(px(8.0))
            .child(ellipsis(note(self.about(open, &shown.links), theme)).px(px(4.0)))
            .child(
                div()
                    .min_w_0()
                    .child(chain)
                    .children(never.map(|never| div().pt(px(10.0)).child(note(never, theme)))),
            )
            .children(failures)
            .children(earlier)
            .child(note(
                "every value is a field of session.info, one read per generation",
                theme,
            ))
    }

    fn open_fork(&mut self, id: &str, cx: &mut Context<Self>) {
        let Some(chat) = self.chat() else {
            return eprintln!("desk: forks: the chat is gone, so {id} was not opened");
        };
        let (open, asking) = {
            let chat = chat.read(cx);
            (
                chat.store().read(cx).open.clone(),
                chat.asked_id().map(str::to_owned),
            )
        };
        match (open.as_deref(), asking.as_deref()) {
            (Some(open), _) if open == id => {
                return eprintln!("desk: forks: {id} is already open");
            }
            (None, Some(asking)) => {
                eprintln!("desk: forks: {id} waits, the chat is still opening {asking}");
                self.queued = Some(id.to_owned());
                return;
            }
            (Some(_) | None, _) => {}
        }
        eprintln!(
            "desk: forks: open {id}, the chat had {}",
            open.as_deref().unwrap_or("none")
        );
        let wanted = id.to_owned();
        chat.update(cx, |chat, cx| chat.open_session(Some(wanted.clone()), cx));
        let asked = chat.read(cx).asked_id().map(str::to_owned);
        if asked.as_deref() != Some(id) {
            eprintln!(
                "desk: forks: {id} did not start opening, the chat asks {}",
                asked.as_deref().unwrap_or("nothing")
            );
        }
        cx.spawn(async move |screen, cx| {
            cx.background_executor().timer(OPEN_PATIENCE).await;
            screen.update(cx, |screen, cx| {
                let Some(chat) = screen.chat() else {
                    return;
                };
                let chat = chat.read(cx);
                let shown = chat.store().read(cx).open.as_deref();
                if shown != Some(wanted.as_str()) {
                    eprintln!(
                        "desk: forks: {wanted} still not open after {} s: the chat shows {} and asks {}",
                        OPEN_PATIENCE.as_secs(),
                        shown.unwrap_or("none"),
                        chat.asked_id().unwrap_or("nothing")
                    );
                }
            })
        })
        .detach();
    }
}

fn generations(open: &str, links: &[Link]) -> Vec<Generation> {
    let mut out = Vec::with_capacity(links.len());
    for (at, link) in links.iter().enumerate() {
        let into = at
            .checked_sub(1)
            .and_then(|newer| links.get(newer))
            .map(|newer| fork(link.info(), newer.info()));
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
    let carry = newer
        .and_then(carried)
        .filter(|from| older.is_some_and(|older| older.id == from.session));
    if let Some(from) = carry {
        return Fork {
            kind: "carried".into(),
            tokens: Some(
                format!(
                    "{} message{} carried from {}",
                    grouped(from.messages),
                    if from.messages == 1 { "" } else { "s" },
                    from.handle
                )
                .into(),
            ),
            fill: None,
        };
    }
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
            (share(after), share(before))
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
                "[{} {} generation {:?} parent {:?} carried_from {:?} forked_into {:?} fork_kind {:?} fork_into_kind {:?} tokens {:?} to {:?} turns {} steps {} outcome {:?} end_reason {:?} head {:?}]",
                info.handle,
                info.id,
                info.generation,
                info.parent,
                info.carried_from,
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
        let asking = self.lineage.values().any(|answer| answer.asking);
        let trailing = self
            .source
            .is_some()
            .then(|| reread("forks", asking, &theme, cx, ForksScreen::reread).into_any_element());
        window(
            titled("forks", trailing),
            &theme,
            div().flex_1().flex().flex_col().child(body),
        )
    }
}
