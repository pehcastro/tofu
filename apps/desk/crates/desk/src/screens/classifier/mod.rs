use super::frame;

mod ledger;
mod sandbox;

use std::collections::BTreeMap;

use crate::modules::chat::Chat;
use desk_core::protocol::{DecisionMade, LedgerParams, SessionParams, request};
use desk_core::query::{LEDGER_READ_LAST, session_ledger};
use desk_ui::components::empty::empty_state;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, Context, Entity, Subscription, WeakEntity, Window, div, prelude::*,
    px,
};

use frame::{load_fonts, note, titled, window};

const NO_TOFU: &str = "The classifier screen shows the decisions tofu sends to the work screen, and no work screen is open here.";
const NONE: &str =
    "tofu judged no tool call in this session, by its ledger and since the desk opened.";
const READING: &str = "Reading this session's decisions from tofu's ledger";

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
            past: None,
            seen: Vec::new(),
            nudges: BTreeMap::new(),
        })
        .into())
}

pub struct Classifier {
    source: Option<Source>,
    past: Option<(String, Past)>,
    seen: Vec<Seen>,
    nudges: BTreeMap<String, i32>,
}

struct Source {
    chat: Entity<Chat>,
    _watch: [Subscription; 2],
}

enum Past {
    Reading,
    Read(Vec<(String, DecisionMade)>),
    Failed(String),
}

#[derive(Clone, PartialEq)]
enum When {
    TurnStarted(String),
    Decided(String),
    Unknown,
}

#[derive(Clone, PartialEq)]
struct Seen {
    when: When,
    decision: DecisionMade,
}

impl Classifier {
    pub fn read_from(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        if self
            .source
            .as_ref()
            .is_some_and(|source| source.chat == *chat)
        {
            return;
        }
        let store = chat.read(cx).store().clone();
        self.source = Some(Source {
            chat: chat.clone(),
            _watch: [
                cx.observe(&store, |classifier, _, cx| classifier.saw(cx)),
                cx.observe(chat, |classifier, _, cx| classifier.saw(cx)),
            ],
        });
        self.saw(cx);
    }

    fn ask_past(&mut self, chat: &Entity<Chat>, session: &str, cx: &mut Context<Self>) {
        self.past = Some((session.to_owned(), Past::Reading));
        let params = LedgerParams {
            last: Some(LEDGER_READ_LAST),
            ..LedgerParams::default()
        };
        eprintln!("desk: query.ledger asked for session {session}, last {LEDGER_READ_LAST}");
        let screen = cx.entity().downgrade();
        let session = session.to_owned();
        let traced = SessionParams {
            session: Some(session.to_owned()),
        };
        chat.update(cx, |chat, cx| {
            chat.request::<request::QueryLedger>(&params, cx, move |chat, ledger, cx| {
                let ledger = match ledger {
                    Ok(ledger) => ledger,
                    Err(reason) => return Self::answer(screen, session, Past::Failed(reason), cx),
                };
                chat.request::<request::SessionTrace>(&traced, cx, move |_, trace, cx| {
                    let past = match trace {
                        Ok(trace) => {
                            let mut turns: Vec<&str> = trace
                                .calls
                                .iter()
                                .map(|call| call.turn.as_str())
                                .chain(trace.requests.iter().map(|asked| asked.turn.as_str()))
                                .chain(trace.messages.iter().map(|message| message.turn.as_str()))
                                .collect();
                            turns.sort_unstable();
                            turns.dedup();
                            let rows = session_ledger(&ledger.rows, &session, &turns);
                            eprintln!(
                                "desk: query.ledger answered {} rows, {} on the {} turns session.trace names for {session}",
                                ledger.rows.len(),
                                rows.len(),
                                turns.len()
                            );
                            Past::Read(rows)
                        }
                        Err(reason) => Past::Failed(reason),
                    };
                    Self::answer(screen, session, past, cx);
                });
            });
        });
    }

    fn answer(screen: WeakEntity<Self>, session: String, past: Past, cx: &mut App) {
        cx.defer(move |cx| {
            if let Err(error) = screen.update(cx, |classifier, cx| {
                if classifier
                    .past
                    .as_ref()
                    .is_some_and(|(asked, _)| *asked == session)
                {
                    classifier.past = Some((session, past));
                    classifier.saw(cx);
                }
            }) {
                eprintln!("desk: the ledger answered a closed classifier: {error}");
            }
        });
    }

    fn saw(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self.source.as_ref().map(|source| source.chat.clone()) else {
            return;
        };
        let open = chat.read(cx).open_id().map(str::to_owned);
        if let Some(session) = &open
            && self.past.as_ref().is_none_or(|(asked, _)| asked != session)
        {
            self.ask_past(&chat, session, cx);
        }
        let store = chat.read(cx).store().read(cx);
        let mut live: Vec<Seen> = store
            .sessions
            .iter()
            .filter(|(id, _)| open.as_ref().is_none_or(|open| open == *id))
            .flat_map(|(_, session)| {
                session.decisions.iter().map(|decision| Seen {
                    when: session
                        .turns
                        .get(&decision.turn)
                        .map_or(When::Unknown, |turn| {
                            When::TurnStarted(turn.started_at.clone())
                        }),
                    decision: decision.clone(),
                })
            })
            .collect();
        live.sort_by(|a, b| {
            b.when
                .stamp()
                .cmp(&a.when.stamp())
                .then(b.decision.seq.cmp(&a.decision.seq))
        });
        let past: Vec<Seen> = match (&open, &self.past) {
            (Some(open), Some((asked, Past::Read(rows)))) if open == asked => rows
                .iter()
                .map(|(at, decision)| Seen {
                    when: When::Decided(at.clone()),
                    decision: decision.clone(),
                })
                .collect(),
            _ => Vec::new(),
        };
        let seen: Vec<Seen> = live.into_iter().chain(past).collect();
        if seen == self.seen {
            return cx.notify();
        }
        for row in seen.iter().filter(|row| !self.seen.contains(row)) {
            eprintln!(
                "desk: classifier row seq {} turn {} {} {:?} {} {} on {}: {}",
                row.decision.seq,
                row.decision.turn,
                row.when.said(),
                row.decision.verdict,
                ledger::mode(row.decision.enforced),
                row.decision.point,
                row.decision.tool,
                ledger::value_said(&row.decision)
            );
        }
        eprintln!("desk: classifier shows {} decisions", seen.len());
        self.seen = seen;
        cx.notify();
    }
}

impl When {
    fn stamp(&self) -> Option<&str> {
        match self {
            When::TurnStarted(at) | When::Decided(at) => Some(at),
            When::Unknown => None,
        }
    }

    fn said(&self) -> String {
        let clock = |at: &str| at.get(11..19).unwrap_or(at).to_owned();
        match self {
            When::TurnStarted(at) => format!("turn {}", clock(at)),
            When::Decided(at) => format!("decided {}", clock(at)),
            When::Unknown => "time unknown".to_owned(),
        }
    }
}

impl Render for Classifier {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let quiet = |id: &'static str, heading: &'static str, said: String| {
            empty_state(
                id,
                heading,
                Some(said.into()),
                &[],
                &[],
                &theme,
                |_, _, _| {},
            )
            .into_any_element()
        };
        let body = match (&self.source, &self.past, self.seen.is_empty()) {
            (None, _, _) => quiet("classifier-no-tofu", "No tofu to ask", NO_TOFU.to_owned()),
            (Some(_), Some((_, Past::Reading)), true) => div()
                .p(px(16.0))
                .child(note(READING, &theme))
                .into_any_element(),
            (Some(_), Some((_, Past::Failed(reason))), true) => quiet(
                "classifier-failed",
                "The ledger did not answer",
                format!("query.ledger failed: {reason}"),
            ),
            (Some(_), _, true) => quiet(
                "classifier-none",
                "No classifier decisions in this session",
                NONE.to_owned(),
            ),
            (Some(_), past, false) => div()
                .flex()
                .flex_col()
                .gap(px(8.0))
                .child(
                    note(
                        match past {
                            Some((_, Past::Reading)) => {
                                "this session's live decisions, its ledger still reading"
                            }
                            Some((_, Past::Failed(_))) => {
                                "this session's live decisions, its ledger did not answer"
                            }
                            Some((_, Past::Read(_))) | None => {
                                "this session's decisions, from tofu's ledger and live"
                            }
                        },
                        &theme,
                    )
                    .px(px(4.0)),
                )
                .child(self.points(&theme))
                .child(self.ledger(&theme))
                .child(self.sandbox(&theme, cx))
                .into_any_element(),
        };
        window(
            titled("classifier", None),
            &theme,
            div().flex_1().flex().flex_col().child(body),
        )
    }
}
