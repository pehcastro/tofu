use super::frame;

mod ledger;
mod sandbox;

use std::collections::BTreeMap;

use crate::modules::chat::Chat;
use desk_core::model::Store;
use desk_core::protocol::DecisionMade;
use desk_ui::components::empty::empty_state;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, Context, Entity, EntityId, Subscription, Window, div, prelude::*, px,
};

use frame::{load_fonts, note, title, window};

const NO_TOFU: &str = "The classifier screen shows the decisions tofu sends to the work screen, and no work screen is open here.";
const NONE_YET: &str = "tofu sends a decision each time its classifier judges a tool call. None has arrived since the desk opened.";

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the classifier screen draws the decisions tofu sends, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|_| Classifier {
            source: None,
            known: 0,
            seen: Vec::new(),
            nudges: BTreeMap::new(),
        })
        .into())
}

pub struct Classifier {
    source: Option<Source>,
    known: usize,
    seen: Vec<Seen>,
    nudges: BTreeMap<String, i32>,
}

struct Source {
    id: EntityId,
    _watch: Subscription,
}

#[derive(Clone, PartialEq)]
struct Seen {
    turn_at: Option<String>,
    decision: DecisionMade,
}

impl Classifier {
    pub fn read_from(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        if self
            .source
            .as_ref()
            .is_some_and(|source| source.id == chat.entity_id())
        {
            return;
        }
        let store = chat.read(cx).store().clone();
        let watch = cx.observe(&store, |classifier, store, cx| classifier.saw(&store, cx));
        self.source = Some(Source {
            id: chat.entity_id(),
            _watch: watch,
        });
        self.saw(&store, cx);
        cx.notify();
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let store = store.read(cx);
        let known = store.sessions.values().fold(0_usize, |all, session| {
            all.saturating_add(session.decisions.len())
        });
        if known == self.known {
            return;
        }
        self.known = known;
        let mut seen: Vec<Seen> = store
            .sessions
            .values()
            .flat_map(|session| {
                session.decisions.iter().map(|decision| Seen {
                    turn_at: session
                        .turns
                        .get(&decision.turn)
                        .map(|turn| turn.started_at.clone()),
                    decision: decision.clone(),
                })
            })
            .collect();
        seen.sort_by(|a, b| {
            b.turn_at
                .cmp(&a.turn_at)
                .then(b.decision.seq.cmp(&a.decision.seq))
        });
        for row in seen.iter().filter(|row| !self.seen.contains(row)) {
            eprintln!(
                "desk: classifier row seq {} turn {} started {} {:?} {} {} on {}: {}",
                row.decision.seq,
                row.decision.turn,
                row.turn_at.as_deref().unwrap_or("unknown"),
                row.decision.verdict,
                ledger::mode(row.decision.enforced),
                row.decision.point,
                row.decision.tool,
                ledger::value_said(&row.decision)
            );
        }
        self.seen = seen;
        cx.notify();
    }
}

impl Render for Classifier {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = match (&self.source, self.seen.is_empty()) {
            (None, _) => empty_state(
                "classifier-no-tofu",
                "No tofu to ask",
                Some(NO_TOFU.into()),
                &[],
                &[],
                &theme,
                |_, _, _| {},
            )
            .into_any_element(),
            (Some(_), true) => empty_state(
                "classifier-none",
                "No decision yet",
                Some(NONE_YET.into()),
                &[],
                &[],
                &theme,
                |_, _, _| {},
            )
            .into_any_element(),
            (Some(_), false) => div()
                .flex()
                .flex_col()
                .gap(px(8.0))
                .child(
                    div()
                        .flex()
                        .flex_wrap()
                        .items_center()
                        .gap(px(10.0))
                        .px(px(4.0))
                        .child(title("Classifier"))
                        .child(note(
                            "the decisions tofu sent for this project since the desk opened",
                            &theme,
                        )),
                )
                .child(self.points(&theme))
                .child(self.ledger(&theme))
                .child(self.sandbox(&theme, cx))
                .into_any_element(),
        };
        window(&theme, div().flex_1().flex().flex_col().child(body))
    }
}
