use std::rc::Rc;

use gpui::{
    AnyElement, App, Div, FontWeight, ListAlignment, ListState, SharedString, Window, div, list,
    prelude::*, px,
};

use crate::components::avatar::{Agent, AvatarSize, avatar};
use crate::components::chip::{GitStatus, Tone, code, file_chip, git_name, tabular};
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink};
use crate::components::size::{
    CAPTION_TEXT, DIM_TEXT, FONT_BODY, FONT_SMALL, FONT_WHO, RADIUS_ROW, ROW_PAD_X, T1, TREE_GAP,
    TREE_ROW,
};
use crate::live::ActiveTheme;
use crate::metrics::ICON_SMALL;
use crate::theme::{ColorToken, Theme};

const FEED_OVERDRAW: f32 = 400.0;
const COLUMN_NAME: f32 = 112.0;
const COLUMN_DIFF: f32 = 72.0;
const COLUMN_TOKENS: f32 = 48.0;
const COLUMN_ACTION: f32 = 22.0;
const COLUMN_TIME: f32 = 40.0;
const TOKENS_PER_K: f64 = 1000.0;

type AgentAction = Rc<dyn Fn(&Agent, &mut Window, &mut App)>;

#[derive(Clone, Debug, PartialEq)]
pub enum FeedEvent {
    Read {
        path: SharedString,
    },
    CodeSearch {
        query: SharedString,
        hits: u32,
    },
    WebSearch {
        query: SharedString,
        results: u32,
    },
    PageRead {
        url: SharedString,
    },
    Edit {
        path: SharedString,
        added: u32,
        removed: u32,
    },
    Command {
        command: SharedString,
        exit: i32,
    },
    BrowserStep {
        action: SharedString,
    },
    Ask {
        question: SharedString,
    },
    Failure {
        message: SharedString,
    },
    Finished {
        worked_for: SharedString,
    },
    Spawn {
        agent: Agent,
    },
}

impl FeedEvent {
    fn verb(&self) -> &'static str {
        match self {
            FeedEvent::Read { .. } => "read",
            FeedEvent::CodeSearch { .. } => "searched code",
            FeedEvent::WebSearch { .. } => "searched web",
            FeedEvent::PageRead { .. } => "read page",
            FeedEvent::Edit { .. } => "edited",
            FeedEvent::Command { .. } => "ran",
            FeedEvent::BrowserStep { .. } => "browser",
            FeedEvent::Ask { .. } => "asks",
            FeedEvent::Failure { .. } => "failed",
            FeedEvent::Finished { .. } => "finished",
            FeedEvent::Spawn { .. } => "spawned",
        }
    }

    fn object(&self, theme: &Theme) -> Div {
        let file = |path: &SharedString, status: Option<GitStatus>| {
            file_chip(
                glyph(Glyph::File, ICON_SMALL, ink(theme, CAPTION_TEXT)),
                git_name(path.clone(), status, theme),
                theme,
            )
        };
        let text =
            |text: SharedString, color| div().min_w_0().truncate().text_color(color).child(text);
        let strong = ink(theme, T1);
        match self {
            FeedEvent::Read { path } => file(path, None),
            FeedEvent::Edit { path, .. } => file(path, Some(GitStatus::Modified)),
            FeedEvent::CodeSearch { query, hits } => code(format!("{query}  {hits} hits"), theme),
            FeedEvent::WebSearch { query, results } => {
                text(format!("{query}, {results} results").into(), strong)
            }
            FeedEvent::PageRead { url } => code(url.clone(), theme),
            FeedEvent::Command { command, exit } => {
                code(format!("$ {command}  exit {exit}"), theme)
            }
            FeedEvent::BrowserStep { action } => text(action.clone(), strong),
            FeedEvent::Ask { question } => text(question.clone(), Tone::Warn.color(theme)),
            FeedEvent::Failure { message } => text(message.clone(), Tone::Deleted.color(theme)),
            FeedEvent::Finished { worked_for } => {
                text(format!("worked for {worked_for}").into(), strong)
            }
            FeedEvent::Spawn { agent } => text(agent.name(), agent.kind.color(theme)),
        }
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct FeedEntry {
    pub agent: Agent,
    pub event: FeedEvent,
    pub diff: Option<(u32, u32)>,
    pub tokens: u32,
    pub time: SharedString,
}

pub struct Feed {
    entries: Rc<Vec<FeedEntry>>,
    state: ListState,
    expand: Option<AgentAction>,
    mention: Option<AgentAction>,
}

impl Feed {
    pub fn state(&self) -> &ListState {
        &self.state
    }

    pub fn new(entries: Vec<FeedEntry>) -> Self {
        Feed {
            state: ListState::new(entries.len(), ListAlignment::Top, px(FEED_OVERDRAW)),
            entries: Rc::new(entries),
            expand: None,
            mention: None,
        }
    }

    pub fn on_expand(mut self, action: impl Fn(&Agent, &mut Window, &mut App) + 'static) -> Self {
        self.expand = Some(Rc::new(action));
        self
    }

    pub fn on_mention(mut self, action: impl Fn(&Agent, &mut Window, &mut App) + 'static) -> Self {
        self.mention = Some(Rc::new(action));
        self
    }

    pub fn render(&self, _theme: &Theme) -> Div {
        let entries = self.entries.clone();
        let (expand, mention) = (self.expand.clone(), self.mention.clone());
        let feed = list(self.state.clone(), move |ix, _, cx| -> AnyElement {
            let theme = ActiveTheme::theme(cx);
            match entries.get(ix) {
                Some(entry) => feed_row(ix, entry, expand.as_ref(), mention.as_ref(), &theme)
                    .into_any_element(),
                None => div().into_any_element(),
            }
        })
        .size_full();
        div().size_full().child(feed)
    }
}

fn compact(tokens: u32) -> String {
    let tokens = f64::from(tokens);
    if tokens < TOKENS_PER_K {
        format!("{tokens}")
    } else {
        format!("{:.1}k", tokens / TOKENS_PER_K)
    }
}

fn action_button(
    id: (&'static str, usize),
    face: impl IntoElement,
    label: &'static str,
    agent: Agent,
    action: Option<&AgentAction>,
    theme: &Theme,
) -> impl IntoElement {
    let hover = theme.color(ColorToken::StateActive);
    let action = action.cloned();
    div()
        .id(id)
        .aria_label(label)
        .size(px(COLUMN_ACTION))
        .flex_none()
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .text_color(ink(theme, CAPTION_TEXT))
        .hover(move |style| style.bg(hover))
        .on_click(move |_, window, cx| {
            if let Some(action) = &action {
                action(&agent, window, cx);
            }
        })
        .child(face)
}

fn feed_row(
    ix: usize,
    entry: &FeedEntry,
    expand: Option<&AgentAction>,
    mention: Option<&AgentAction>,
    theme: &Theme,
) -> Div {
    let dim = ink(theme, CAPTION_TEXT);
    let agent = entry.agent;
    let numeric = |width: f32| {
        div()
            .w(px(width))
            .flex_none()
            .flex()
            .justify_end()
            .gap_1()
            .text_size(px(FONT_SMALL))
            .font_features(tabular())
            .text_color(dim)
    };
    let diff = entry.diff.map(|(added, removed)| {
        [
            div()
                .text_color(Tone::Added.color(theme))
                .child(format!("+{added}")),
            div()
                .text_color(Tone::Deleted.color(theme))
                .child(format!("\u{2212}{removed}")),
        ]
    });
    div()
        .h(px(TREE_ROW))
        .w_full()
        .flex()
        .items_center()
        .gap(px(TREE_GAP))
        .px(px(ROW_PAD_X))
        .overflow_hidden()
        .whitespace_nowrap()
        .text_size(px(FONT_BODY))
        .text_color(ink(theme, DIM_TEXT))
        .child(avatar(("feed-avatar", ix), &agent, AvatarSize::Feed, theme))
        .child(
            div()
                .w(px(COLUMN_NAME))
                .flex_none()
                .truncate()
                .text_size(px(FONT_WHO))
                .font_weight(FontWeight::MEDIUM)
                .text_color(agent.kind.color(theme))
                .child(agent.name()),
        )
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .items_center()
                .gap_1p5()
                .overflow_hidden()
                .child(div().flex_none().text_color(dim).child(entry.event.verb()))
                .child(entry.event.object(theme)),
        )
        .child(numeric(COLUMN_DIFF).children(diff.into_iter().flatten()))
        .child(numeric(COLUMN_TOKENS).child(compact(entry.tokens)))
        .child(action_button(
            ("feed-expand", ix),
            glyph(Glyph::Agents, ICON_SMALL, dim),
            "expand",
            agent,
            expand,
            theme,
        ))
        .child(action_button(
            ("feed-mention", ix),
            div().text_size(px(FONT_SMALL)).child("@"),
            "mention",
            agent,
            mention,
            theme,
        ))
        .child(numeric(COLUMN_TIME).child(entry.time.clone()))
}
