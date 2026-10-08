use super::frame;

mod agents;
mod rules;

use crate::modules::chat::Chat;
use desk_core::model::Store;
use desk_core::protocol::{RuleListReport, subagent};
use desk_core::query::{Answer, QueryError, Read};
use desk_ui::components::avatar::spinner;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::paint::ink;
use desk_ui::components::size::T3;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, Entity, EntityId, SharedString,
    Subscription, WeakEntity, Window, div, prelude::*, px,
};

use frame::{HEADER_PILLS, ellipsis, load_fonts, note, pills, title, window};

const NO_TOFU: &str = "The library reads agents and rules from the tofu the work screen runs, and no work screen is open here.";
const FACT_KEY: f32 = 96.0;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the library screen draws the agents and rules tofu reports, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|_| Library {
            source: None,
            tab: Tab::Rules,
            agents: Answer::default(),
            rules: Answer::default(),
            agent: None,
            rule: None,
            kind: None,
        })
        .into())
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Tab {
    Rules,
    Agents,
}

pub struct Library {
    source: Option<Source>,
    tab: Tab,
    agents: Answer<subagent::Found>,
    rules: Answer<RuleListReport>,
    agent: Option<String>,
    rule: Option<String>,
    kind: Option<&'static str>,
}

struct Source {
    chat: WeakEntity<Chat>,
    id: EntityId,
    _watch: Subscription,
}

impl Library {
    pub fn read_from(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        if self
            .source
            .as_ref()
            .is_some_and(|source| source.id == chat.entity_id())
        {
            return;
        }
        let store = chat.read(cx).store().clone();
        let watch = cx.observe(&store, |library, store, cx| library.saw(&store, cx));
        self.source = Some(Source {
            chat: chat.downgrade(),
            id: chat.entity_id(),
            _watch: watch,
        });
        chat.update(cx, |chat, cx| chat.want_library(cx));
        self.saw(&store, cx);
        cx.notify();
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let store = store.read(cx);
        if !moved(&self.agents, &store.agents) && !moved(&self.rules, &store.rules) {
            return;
        }
        if let Some(read) = landed(&self.agents, &store.agents) {
            eprintln!(
                "desk: library shows {} agents from query.agents at {}: {}",
                read.value.definitions.len(),
                read.at,
                read.value
                    .definitions
                    .iter()
                    .map(|agent| agent.name.as_str())
                    .collect::<Vec<_>>()
                    .join(", ")
            );
        }
        if let Some(read) = landed(&self.rules, &store.rules) {
            eprintln!(
                "desk: library shows {} rules from query.rules at {}",
                read.value.rules.len(),
                read.at
            );
        }
        self.agents = store.agents.clone();
        self.rules = store.rules.clone();
        cx.notify();
    }

    fn reread(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self
            .source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
        else {
            return eprintln!("desk: library: the chat is gone, so nothing was asked");
        };
        eprintln!("desk: library: read again");
        chat.update(cx, |chat, cx| chat.reread_library(cx));
    }

    fn header(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let (asking, read, verb) = match self.tab {
            Tab::Rules => (
                self.rules.asking,
                self.rules.read.as_ref().map(|read| read.at.as_str()),
                "tofu rules list",
            ),
            Tab::Agents => (
                self.agents.asking,
                self.agents.read.as_ref().map(|read| read.at.as_str()),
                "tofu agents",
            ),
        };
        div()
            .flex()
            .flex_none()
            .flex_wrap()
            .items_center()
            .gap(px(10.0))
            .px(px(4.0))
            .child(title("Library"))
            .child(pills(
                "library-tab",
                &[(Tab::Rules, "Rules"), (Tab::Agents, "Agents")],
                self.tab,
                &HEADER_PILLS,
                theme,
                cx,
                |library, tab| library.tab = tab,
            ))
            .children(read.map(|at| ellipsis(note(format!("{verb}, read {}", clock(at)), theme))))
            .child(div().flex_1())
            .when(asking, |header| {
                header.child(spinner("library-asking", theme))
            })
            .child(
                button(
                    "library-reread",
                    "Read again",
                    None,
                    ButtonKind::Plain,
                    theme,
                )
                .on_click(cx.listener(|library, _: &ClickEvent, _, cx| library.reread(cx))),
            )
    }

    fn waiting(
        &self,
        failed: Option<&QueryError>,
        what: &'static str,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let Some(error) = failed else {
            return empty_state(
                "library-reading",
                format!("Reading the {what}"),
                Some(format!("asked the tofu this project runs for its {what}").into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            )
            .into_any_element();
        };
        let again = cx.listener(|library, _: &(), _, cx| library.reread(cx));
        empty_state(
            "library-failed",
            format!("The {what} could not be read"),
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
        let (failed, shown) = match (self.tab, &self.rules.read, &self.agents.read) {
            (Tab::Rules, Some(read), _) => (
                self.rules.failed.as_ref(),
                self.rules_tab(&read.value, theme, cx),
            ),
            (Tab::Rules, None, _) => (
                None,
                self.waiting(self.rules.failed.as_ref(), "rules", theme, cx),
            ),
            (Tab::Agents, _, Some(read)) => (
                self.agents.failed.as_ref(),
                self.agents_tab(&read.value, theme, cx),
            ),
            (Tab::Agents, _, None) => (
                None,
                self.waiting(self.agents.failed.as_ref(), "agents", theme, cx),
            ),
        };
        div()
            .flex()
            .flex_col()
            .gap(px(8.0))
            .child(self.header(theme, cx))
            .children(failed.map(|error| warn(format!("the last read failed: {error}"), theme)))
            .child(shown)
            .into_any_element()
    }
}

fn moved<T>(had: &Answer<T>, now: &Answer<T>) -> bool {
    had.asking != now.asking
        || had.failed != now.failed
        || had.read.as_ref().map(|read| &read.at) != now.read.as_ref().map(|read| &read.at)
}

fn landed<'a, T>(had: &Answer<T>, now: &'a Answer<T>) -> Option<&'a Read<T>> {
    (had.asking && !now.asking)
        .then_some(now.read.as_ref())
        .flatten()
}

fn clock(at: &str) -> String {
    at.get(11..16)
        .map_or_else(|| at.to_owned(), |time| format!("{time} UTC"))
}

fn warn(text: impl Into<SharedString>, theme: &Theme) -> Div {
    note(text, theme).text_color(theme.color(ColorToken::StatusWarn))
}

fn fact(key: &'static str, value: impl IntoElement, theme: &Theme) -> Div {
    div()
        .flex()
        .items_start()
        .gap_3()
        .child(
            div()
                .flex_none()
                .w(px(FACT_KEY))
                .text_color(ink(theme, T3))
                .child(key),
        )
        .child(div().flex_1().min_w_0().child(value))
}

impl Render for Library {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = match self.source {
            None => empty_state(
                "library-no-tofu",
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
