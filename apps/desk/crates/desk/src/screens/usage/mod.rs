use super::frame;

use std::collections::BTreeMap;

use crate::modules::chat::{Chat, Listed};
use desk_core::model::Store;
use desk_ui::components::card::{caption, inner_card, outer_card};
use desk_ui::components::chip::mono;
use desk_ui::components::empty::empty_state;
use desk_ui::components::list::bare_row;
use desk_ui::components::paint::ink;
use desk_ui::components::size::T3;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::Theme;
use gpui::{
    AnyView, App, AppContext, Context, Div, Entity, EntityId, FontWeight, Subscription, WeakEntity,
    Window, div, prelude::*, px, relative,
};

use frame::{ellipsis, fraction, load_fonts, note, panel, panes, title, window};

const NO_TOFU: &str = "Usage counts the usage.updated the tofu of the work screen sends, and no work screen is open here.";
const NOTHING_YET: &str = "tofu sends usage.updated once per model call. Run a turn in a session and its calls show here as they arrive.";
const COUNT_LEAST: f32 = 120.0;
const LIST_LEAST: f32 = 340.0;
const FIGURE: f32 = 24.0;
const COLUMN: f32 = 64.0;
const COLUMNS: [&str; 4] = ["calls", "in", "out", "cache read"];

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the usage screen draws the usage.updated tofu sent since the desk opened, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|_| UsageScreen {
            source: None,
            seen: Seen::default(),
            logged: BTreeMap::new(),
        })
        .into())
}

pub struct UsageScreen {
    source: Option<Source>,
    seen: Seen,
    logged: BTreeMap<String, usize>,
}

struct Source {
    chat: WeakEntity<Chat>,
    id: EntityId,
    _watch: Subscription,
    _listed: Subscription,
}

#[derive(Default, PartialEq)]
struct Seen {
    opened: String,
    total: Tally,
    decisions: i64,
    models: Vec<(String, Tally)>,
    sessions: Vec<(String, Tally)>,
}

#[derive(Default, Clone, Copy, PartialEq)]
struct Tally {
    calls: i64,
    tokens_in: i64,
    tokens_out: i64,
    cache_read: i64,
}

impl Tally {
    fn add(&mut self, tokens_in: i64, tokens_out: i64, cache_read: i64) {
        self.calls = self.calls.saturating_add(1);
        self.tokens_in = self.tokens_in.saturating_add(tokens_in);
        self.tokens_out = self.tokens_out.saturating_add(tokens_out);
        self.cache_read = self.cache_read.saturating_add(cache_read);
    }

    fn figures(&self) -> [String; 4] {
        [self.calls, self.tokens_in, self.tokens_out, self.cache_read].map(grouped)
    }
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
        self.saw(&store, cx);
        cx.notify();
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let chat = self
            .source
            .as_ref()
            .and_then(|source| source.chat.upgrade());
        let rows = chat.map(|chat| chat.read(cx).rows().to_vec());
        let store = store.read(cx);
        let mut seen = Seen {
            opened: chrono::DateTime::<chrono::Local>::from(store.opened)
                .format("%H:%M")
                .to_string(),
            ..Seen::default()
        };
        let mut models: BTreeMap<&str, Tally> = BTreeMap::new();
        for (id, session) in &store.sessions {
            let logged = self.logged.entry(id.clone()).or_default();
            for usage in session.usage.iter().skip(*logged) {
                eprintln!(
                    "desk: usage.updated session {} turn {} seq {} model {} tokensIn {} tokensOut {} cacheRead {} decisions {}",
                    usage.session,
                    usage.turn,
                    usage.seq,
                    usage.model,
                    usage.tokens_in,
                    usage.tokens_out,
                    usage.cache_read,
                    usage.decisions
                );
            }
            *logged = session.usage.len();
            if session.usage.is_empty() {
                continue;
            }
            let mut tally = Tally::default();
            for usage in &session.usage {
                tally.add(usage.tokens_in, usage.tokens_out, usage.cache_read);
                seen.total
                    .add(usage.tokens_in, usage.tokens_out, usage.cache_read);
                seen.decisions = seen.decisions.saturating_add(usage.decisions);
                models.entry(&usage.model).or_default().add(
                    usage.tokens_in,
                    usage.tokens_out,
                    usage.cache_read,
                );
            }
            let name = rows
                .iter()
                .flatten()
                .find(|row| row.id == *id)
                .map(|row| row.title().to_owned())
                .or_else(|| Some(session.name.clone()).filter(|name| !name.is_empty()))
                .unwrap_or_else(|| id.clone());
            seen.sessions.push((name, tally));
        }
        seen.models = models
            .into_iter()
            .map(|(model, tally)| (model.to_owned(), tally))
            .collect();
        seen.models
            .sort_by_key(|row| std::cmp::Reverse(row.1.tokens_in));
        seen.sessions
            .sort_by_key(|row| std::cmp::Reverse(row.1.tokens_in));
        if seen == self.seen {
            return;
        }
        if seen.total != self.seen.total {
            eprintln!(
                "desk: usage shows {} calls, {} in, {} out, {} cache read, {} decisions, {} models, {} sessions",
                seen.total.calls,
                seen.total.tokens_in,
                seen.total.tokens_out,
                seen.total.cache_read,
                seen.decisions,
                seen.models.len(),
                seen.sessions.len()
            );
        }
        self.seen = seen;
        cx.notify();
    }

    fn header(&self, theme: &Theme) -> Div {
        div()
            .flex()
            .flex_none()
            .flex_wrap()
            .items_center()
            .gap(px(10.0))
            .px(px(4.0))
            .child(title("Usage"))
            .child(ellipsis(note(
                format!(
                    "the usage.updated tofu sent since the desk opened at {}",
                    self.seen.opened
                ),
                theme,
            )))
    }

    fn body(&self, theme: &Theme) -> Div {
        let shown = div()
            .flex()
            .flex_col()
            .gap(px(8.0))
            .child(self.header(theme));
        if self.seen.total.calls == 0 {
            return shown.child(empty_state(
                "usage-nothing-yet",
                "No model call yet",
                Some(NOTHING_YET.into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            ));
        }
        let total = &self.seen.total;
        shown
            .child(
                panes().flex_none().children(
                    [
                        ("Tokens in", total.tokens_in),
                        ("Tokens out", total.tokens_out),
                        ("Cache read", total.cache_read),
                        ("Model calls", total.calls),
                        ("Decisions", self.seen.decisions),
                    ]
                    .into_iter()
                    .map(|(label, value)| count(label, grouped(value), theme)),
                ),
            )
            .child(
                panes()
                    .child(list("By model", "model", &self.seen.models, theme))
                    .child(list("By session", "session", &self.seen.sessions, theme)),
            )
            .child(note(
                "every value above is a sum of usage.updated, one per model call",
                theme,
            ))
    }
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

fn list(title: &'static str, key: &'static str, rows: &[(String, Tally)], theme: &Theme) -> Div {
    let columns = |row: Div, cells: [String; 4]| {
        row.children(cells.map(|cell| {
            div()
                .w(px(COLUMN))
                .flex_none()
                .flex()
                .justify_end()
                .child(cell)
        }))
    };
    panel(title, None, theme)
        .map(|panel| fraction(panel, 1.0, LIST_LEAST))
        .child(
            inner_card(theme)
                .px(px(8.0))
                .py(px(6.0))
                .text_size(px(13.0))
                .child(columns(
                    div()
                        .flex()
                        .gap(px(8.0))
                        .px(px(10.0))
                        .text_size(px(12.0))
                        .text_color(ink(theme, T3))
                        .child(ellipsis(div().flex_1()).child(key)),
                    COLUMNS.map(str::to_owned),
                ))
                .children(rows.iter().enumerate().map(|(at, (name, tally))| {
                    let row = div()
                        .flex()
                        .flex_1()
                        .min_w_0()
                        .gap(px(8.0))
                        .font_family(mono(theme))
                        .child(ellipsis(div().flex_1()).child(name.clone()));
                    bare_row((key, at), false, false, theme)
                        .cursor_default()
                        .child(columns(row, tally.figures()))
                })),
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

impl Render for UsageScreen {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = match self.source {
            None => empty_state(
                "usage-no-tofu",
                "No tofu to ask",
                Some(NO_TOFU.into()),
                &[],
                &[],
                &theme,
                |_, _, _| {},
            )
            .into_any_element(),
            Some(_) => self.body(&theme).into_any_element(),
        };
        window(&theme, div().flex_1().flex().flex_col().child(body))
    }
}
