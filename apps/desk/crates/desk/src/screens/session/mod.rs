use super::frame;

use crate::modules::chat::Chat;
use desk_core::model::Store;
use desk_core::protocol::SessionInfo;
use desk_core::query::Answer;
use desk_ui::components::avatar::spinner;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{caption, inner_card, outer_card};
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::paint::ink;
use desk_ui::components::size::T3;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, Entity, EntityId, FontWeight,
    SharedString, Subscription, WeakEntity, Window, div, prelude::*, px, relative,
};

use frame::{ellipsis, fraction, load_fonts, note, panel, panes, title, window};

const NO_TOFU: &str = "The session reads session.info from the tofu the work screen runs, and no work screen is open here.";
const NO_FORK: &str = "session.info names no fork for this session";
const COUNT_LEAST: f32 = 120.0;
const FACTS_LEAST: f32 = 300.0;
const LABEL_WIDTH: f32 = 120.0;
const FIGURE: f32 = 24.0;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the session screen draws what tofu session.info reports, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|_| SessionScreen {
            source: None,
            answer: Answer::default(),
            open: None,
        })
        .into())
}

pub struct SessionScreen {
    source: Option<Source>,
    answer: Answer<SessionInfo>,
    open: Option<String>,
}

struct Source {
    chat: WeakEntity<Chat>,
    id: EntityId,
    _watch: Subscription,
}

impl SessionScreen {
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
        chat.update(cx, |chat, cx| chat.want_info(cx));
        self.saw(&store, cx);
        cx.notify();
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let open = self
            .source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
            .and_then(|chat| chat.read(cx).open_id().map(str::to_owned));
        let info = &store.read(cx).info;
        if *info == self.answer && open == self.open {
            return;
        }
        if self.answer.asking
            && !info.asking
            && let Some(read) = &info.read
        {
            eprintln!(
                "desk: session shows session.info at {} for {} {}: {} turns, {} steps, {} sub-agents, {} reads",
                read.at,
                read.value.name.as_deref().unwrap_or("unnamed"),
                read.value.id,
                read.value.turns,
                read.value.steps,
                read.value.sub_agents,
                read.value.reads
            );
        }
        self.answer = info.clone();
        self.open = open;
        cx.notify();
    }

    fn reread(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self
            .source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
        else {
            return eprintln!("desk: session: the chat is gone, so nothing was asked");
        };
        eprintln!("desk: session: read again");
        chat.update(cx, |chat, cx| chat.reread_info(cx));
    }

    fn header(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let read = self.answer.read.as_ref().map(|read| {
            format!(
                "{}, session.info read {}",
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
            .child(title("Session"))
            .children(read.map(|read| ellipsis(note(read, theme))))
            .child(div().flex_1())
            .when(self.answer.asking, |header| {
                header.child(spinner("session-asking", theme))
            })
            .child(
                button(
                    "session-reread",
                    "Read again",
                    None,
                    ButtonKind::Plain,
                    theme,
                )
                .on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.reread(cx))),
            )
    }

    fn elsewhere(&self, info: &SessionInfo) -> Option<String> {
        if self.open.as_deref() == Some(info.id.as_str()) {
            return None;
        }
        let name = info.name.as_deref().unwrap_or(&info.id);
        Some(match &self.open {
            Some(open) => format!(
                "session.info reported {name}, read before {open} opened, and is being read again"
            ),
            None => format!("no session is open, so session.info reported {name}"),
        })
    }

    fn waiting(&self, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let Some(error) = &self.answer.failed else {
            return empty_state(
                "session-reading",
                "Reading the session",
                Some("asked the tofu this project runs for session.info".into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            )
            .into_any_element();
        };
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

    fn body(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let shown = div()
            .flex()
            .flex_col()
            .gap(px(8.0))
            .child(self.header(theme, cx));
        let Some(read) = &self.answer.read else {
            return shown.child(self.waiting(theme, cx));
        };
        let info = &read.value;
        shown
            .children(self.elsewhere(info).map(|said| warn(said, theme)))
            .children(
                self.answer
                    .failed
                    .as_ref()
                    .map(|error| warn(format!("the last read failed: {error}"), theme)),
            )
            .child(
                panes().flex_none().children(
                    counts(info)
                        .into_iter()
                        .map(|(label, value)| count(label, value, theme)),
                ),
            )
            .child(
                panes()
                    .child(facts("The session", about(info), None, theme))
                    .child(facts("Forks", forks(info), Some(NO_FORK), theme)),
            )
            .child(note("every value above is a field of session.info", theme))
    }
}

fn counts(info: &SessionInfo) -> Vec<(&'static str, String)> {
    let mut counts = vec![
        ("Turns", grouped(info.turns)),
        ("Steps", grouped(info.steps)),
        ("Sub-agents", grouped(info.sub_agents)),
        ("Reads", grouped(info.reads)),
        ("Carried messages", grouped(info.carried_messages)),
    ];
    if let Some(cost) = info.cost_usd {
        counts.push(("Cost", format!("${cost:.2}")));
    }
    counts
}

fn about(info: &SessionInfo) -> Vec<(&'static str, String)> {
    let said = |flag: Option<bool>| flag.map(|on| if on { "yes" } else { "no" }.to_owned());
    [
        ("name", info.name.clone()),
        ("id", Some(info.id.clone())),
        ("handle", Some(info.handle.clone())),
        ("task", info.task.clone()),
        ("model", info.model.clone()),
        ("wire", info.wire.clone()),
        ("started", Some(info.at.clone())),
        ("last turn", info.last_at.clone()),
        ("ended", info.ended_at.clone()),
        ("outcome", info.outcome.clone()),
        ("end reason", info.end_reason.clone()),
        ("error", info.error.clone()),
        ("head", said(info.head)),
        ("expired", said(info.expired)),
        ("auto compaction", info.auto_compaction.clone()),
        ("context target", info.context_target.map(grouped)),
        ("context ceiling", info.context_ceiling.map(grouped)),
        ("unrecorded reads", info.unrecorded_reads.map(grouped)),
    ]
    .into_iter()
    .filter_map(|(label, value)| Some((label, value?)))
    .collect()
}

fn forks(info: &SessionInfo) -> Vec<(&'static str, String)> {
    let generation = match (info.generation, info.generations) {
        (Some(at), Some(of)) => Some(format!("{at} of {of}")),
        (at, _) => at.map(grouped),
    };
    let tokens = match (info.fork_tokens_before, info.fork_tokens_after) {
        (Some(before), Some(after)) => Some(format!("{} to {}", grouped(before), grouped(after))),
        (before, after) => before.or(after).map(grouped),
    };
    [
        ("forked into", info.forked_into.clone()),
        ("as", info.fork_into_kind.clone()),
        ("made by", info.fork_kind.clone()),
        ("fork tokens", tokens),
        ("parent", info.parent.clone()),
        ("root", info.root.clone()),
        ("family", info.family.clone()),
        ("generation", generation),
    ]
    .into_iter()
    .filter_map(|(label, value)| Some((label, value?)))
    .collect()
}

fn count(label: &'static str, value: String, theme: &Theme) -> Div {
    fraction(outer_card(theme), 1.0, COUNT_LEAST)
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

fn facts(
    title: &'static str,
    rows: Vec<(&'static str, String)>,
    none: Option<&'static str>,
    theme: &Theme,
) -> Div {
    let empty = rows.is_empty();
    panel(title, None, theme)
        .map(|panel| fraction(panel, 1.0, FACTS_LEAST))
        .child(
            inner_card(theme)
                .px(px(16.0))
                .py(px(12.0))
                .gap(px(6.0))
                .text_size(px(13.0))
                .children(rows.into_iter().map(|(label, value)| {
                    div()
                        .flex()
                        .gap(px(14.0))
                        .child(
                            div()
                                .w(px(LABEL_WIDTH))
                                .flex_none()
                                .text_color(ink(theme, T3))
                                .child(label),
                        )
                        .child(div().flex_1().min_w_0().child(value))
                }))
                .children(none.filter(|_| empty).map(|none| note(none, theme))),
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
            Some(_) => self.body(&theme, cx).into_any_element(),
        };
        window(&theme, div().flex_1().flex().flex_col().child(body))
    }
}
