#[cfg(not(feature = "screen-settings"))]
#[path = "../settings/board.rs"]
pub mod board;
#[cfg(feature = "screen-settings")]
pub use crate::screens::settings::board;
mod catalog;

use board::inner;

use crate::modules::chat::Chat;
use crate::screens::frame;
use desk_core::control::TELL_BADGE;
use desk_core::protocol::{AccountStatus, AccountStatusState, KeyStatus};
use desk_core::query::display_name;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{caption, header_button, outer_card};
use desk_ui::components::form::TextArea;
use desk_ui::components::overlay::{popover, toast};
use desk_ui::components::paint::{ink, tint};
use desk_ui::components::size::T3;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::TOAST_BOTTOM;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Div, Entity, EntityId, Focusable, FontWeight,
    Render, Rgba, SharedString, Subscription, WeakEntity, Window, div, prelude::*, px, relative,
};

use catalog::{ACCOUNTS_NOTE, SIGN_IN_SOURCES};

const SHELL_HEADER: f32 = 28.0;
const DESC: f32 = 13.0;
const SMALL: f32 = 12.0;
const ROW_LINE: f32 = 18.0;
const NOTE_LINE: f32 = 17.0;
const ROW_RADIUS: f32 = 10.0;
const ROW_FILL: f32 = 0.18;
const STATE_TEXT: f32 = 11.5;
const STATE_TINT: f32 = 0.12;
const STATE_WARM_TINT: f32 = 0.14;
const STANDBY_FILL: f32 = 0.06;
const STANDBY_INK: f32 = 0.5;
const EMAIL_INK: f32 = 0.8;
const COLUMN_SHARE: f32 = 0.6;
const POP_WIDTH: f32 = 440.0;
const POP_TOP: f32 = 40.0;
const POP_TEXT: f32 = 13.5;
const POP_TITLE: f32 = 15.0;
const KEY_TAIL: usize = 4;
const NO_TOFU: &str =
    "Accounts reads tofu through the work screen, and no work screen is open here.";
const READING: &str = "Asking tofu for its accounts.";
const EMAIL_HINT: &str = "name@example.com";
const KEY_HINT: &str = "paste the new key";
const NO_KEY: &str = "Paste a key before saving it.";

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    board::load_fonts(cx)?;
    match board {
        None | Some("ISET-2") => Ok(cx
            .new(|_| Accounts {
                source: None,
                adding: false,
                told: None,
                editing: None,
                removing: None,
            })
            .into()),
        Some(other) => Err(format!("the accounts screen draws ISET-2, not {other}")),
    }
}

pub struct Accounts {
    source: Option<Source>,
    adding: bool,
    told: Option<SharedString>,
    editing: Option<Editing>,
    removing: Option<(String, String)>,
}

struct Source {
    chat: WeakEntity<Chat>,
    id: EntityId,
    _watch: Subscription,
}

struct Editing {
    target: Target,
    field: Entity<TextArea>,
}

#[derive(Clone, PartialEq)]
enum Target {
    Email { source: String, id: i64 },
    Key { provider: String },
}

enum Row {
    Subscription {
        source: String,
        account: AccountStatus,
        filled: bool,
    },
    Key(KeyStatus),
}

type Click = Box<dyn Fn(&ClickEvent, &mut Window, &mut App)>;

impl Accounts {
    pub fn read_from(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        if self
            .source
            .as_ref()
            .is_some_and(|source| source.id == chat.entity_id())
        {
            return;
        }
        let store = chat.read(cx).store().clone();
        self.source = Some(Source {
            chat: chat.downgrade(),
            id: chat.entity_id(),
            _watch: cx.observe(&store, |_, _, cx| cx.notify()),
        });
        chat.update(cx, |chat, cx| chat.want_accounts(cx));
        cx.notify();
    }

    fn chat(&self) -> Option<Entity<Chat>> {
        self.source.as_ref()?.chat.upgrade()
    }

    fn rows(&self, cx: &App) -> Result<Vec<Row>, SharedString> {
        let chat = self.chat().ok_or(SharedString::from(NO_TOFU))?;
        let chat = chat.read(cx);
        let answer = chat.accounts();
        let Some(read) = &answer.read else {
            return Err(answer
                .failed
                .as_ref()
                .map_or(READING.into(), |error| error.to_string().into()));
        };
        let subscriptions = read.value.subscriptions.iter().flat_map(|subscription| {
            subscription
                .accounts
                .iter()
                .map(|account| Row::Subscription {
                    filled: chat.filled_email(&subscription.source, account.id)
                        == Some(account.account.as_str()),
                    source: subscription.source.clone(),
                    account: account.clone(),
                })
        });
        Ok(subscriptions
            .chain(read.value.keys.iter().cloned().map(Row::Key))
            .collect())
    }

    fn edit(target: Target, hint: &'static str, cx: &mut Context<Self>) -> Click {
        Box::new(cx.listener(move |this, _: &ClickEvent, window, cx| {
            let accounts = cx.entity().downgrade();
            let field = cx.new(|cx| {
                TextArea::new(hint.into(), window, cx)
                    .max_lines(1)
                    .on_submit(move |_, _, cx| {
                        if let Err(error) = accounts.update(cx, |accounts, cx| accounts.commit(cx))
                        {
                            eprintln!("desk: accounts: {error}");
                        }
                    })
            });
            window.focus(&field.read(cx).focus_handle(cx), cx);
            this.editing = Some(Editing {
                target: target.clone(),
                field,
            });
            this.told = None;
            cx.notify();
        }))
    }

    fn commit(&mut self, cx: &mut Context<Self>) {
        let Some(editing) = self.editing.take() else {
            return;
        };
        let typed = editing.field.read(cx).text();
        let done = match (self.chat(), &editing.target) {
            (None, _) => Err(NO_TOFU.to_owned()),
            (Some(chat), Target::Email { source, id }) => {
                chat.update(cx, |chat, cx| chat.fill_email(source, *id, &typed, cx))
            }
            (Some(_), Target::Key { .. }) if typed.trim().is_empty() => Err(NO_KEY.to_owned()),
            (Some(chat), Target::Key { provider }) => {
                chat.update(cx, |chat, cx| chat.add_key(provider, &typed, cx));
                Ok(())
            }
        };
        if let Err(error) = done {
            self.told = Some(error.into());
            self.editing = Some(editing);
        }
        cx.notify();
    }

    fn with_chat(
        cx: &mut Context<Self>,
        act: impl Fn(&mut Self, &mut Chat, &mut Context<Chat>) -> Result<(), String> + 'static,
    ) -> Click {
        Box::new(cx.listener(move |this, _: &ClickEvent, _, cx| {
            let done = match this.chat() {
                Some(chat) => chat.update(cx, |chat, cx| act(this, chat, cx)),
                None => Err(NO_TOFU.to_owned()),
            };
            if let Err(error) = done {
                this.told = Some(error.into());
            }
            cx.notify();
        }))
    }

    fn sign_in(source: &str, cx: &mut Context<Self>) -> Click {
        let source = source.to_owned();
        Self::with_chat(cx, move |this, chat, cx| {
            this.adding = false;
            chat.sign_in(&source, cx);
            Ok(())
        })
    }

    fn clear(source: &str, id: i64, cx: &mut Context<Self>) -> Click {
        let source = source.to_owned();
        Self::with_chat(cx, move |_, chat, cx| chat.fill_email(&source, id, "", cx))
    }

    fn remove(provider: &str, role: &str, cx: &mut Context<Self>) -> Click {
        let (provider, role) = (provider.to_owned(), role.to_owned());
        Self::with_chat(cx, move |this, chat, cx| {
            this.removing = None;
            chat.remove_key(&provider, &role, cx);
            Ok(())
        })
    }

    fn ask_remove(removing: Option<(String, String)>, cx: &mut Context<Self>) -> Click {
        Box::new(cx.listener(move |this, _: &ClickEvent, _, cx| {
            this.removing = removing.clone();
            cx.notify();
        }))
    }

    fn cancel(cx: &mut Context<Self>) -> Click {
        Box::new(cx.listener(|this, _: &ClickEvent, _, cx| {
            this.editing = None;
            this.told = None;
            cx.notify();
        }))
    }

    fn toggle_add(cx: &mut Context<Self>) -> Click {
        Box::new(cx.listener(|this, _: &ClickEvent, _, cx| {
            this.adding = !this.adding;
            cx.notify();
        }))
    }

    fn editing(&self, target: &Target) -> Option<&Editing> {
        self.editing
            .as_ref()
            .filter(|editing| editing.target == *target)
    }

    fn field_line(editing: &Editing, theme: &Theme, cx: &mut Context<Self>) -> Div {
        div()
            .flex()
            .items_center()
            .gap_1p5()
            .py_1()
            .child(editing.field.clone())
            .child(
                button("save-edit", "Save", None, ButtonKind::Primary, theme)
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.commit(cx))),
            )
            .child(
                button("cancel-edit", "Cancel", None, ButtonKind::Plain, theme)
                    .on_click(Self::cancel(cx)),
            )
    }

    fn subscription(
        &self,
        at: usize,
        (source, account, filled): (&str, &AccountStatus, bool),
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let target = Target::Email {
            source: source.to_owned(),
            id: account.id,
        };
        let plan = account
            .plan
            .as_deref()
            .map_or("subscription".to_owned(), |plan| {
                format!("subscription \u{b7} {}", display_name(plan))
            });
        let email = match (self.editing(&target), account.email()) {
            (Some(editing), _) => Self::field_line(editing, theme, cx),
            (None, Some(email)) => div()
                .flex()
                .items_center()
                .gap_1p5()
                .child(
                    div()
                        .text_color(ink(theme, EMAIL_INK))
                        .child(email.to_owned()),
                )
                .when(filled, |line| {
                    line.child(
                        button(("edit-email", at), "Edit", None, ButtonKind::Text, theme)
                            .on_click(Self::edit(target.clone(), EMAIL_HINT, cx)),
                    )
                    .child(
                        button(("clear-email", at), "Clear", None, ButtonKind::Text, theme)
                            .on_click(Self::clear(source, account.id, cx)),
                    )
                }),
            (None, None) => div().flex().child(
                button(
                    ("add-email", at),
                    "Add email",
                    None,
                    ButtonKind::Text,
                    theme,
                )
                .on_click(Self::edit(target.clone(), EMAIL_HINT, cx)),
            ),
        };
        row(theme)
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .flex_col()
                    .gap_0p5()
                    .line_height(px(ROW_LINE))
                    .child(display_name(source))
                    .child(email)
                    .child(small(plan, theme)),
            )
            .child(state_chip(&account.state, theme))
            .when(!account.state.serving(), |row| {
                row.child(
                    button(
                        ("sign-in", at),
                        "Sign in again",
                        None,
                        ButtonKind::Plain,
                        theme,
                    )
                    .on_click(Self::sign_in(source, cx)),
                )
            })
    }

    fn key(&self, at: usize, key: &KeyStatus, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let role = String::from(key.role.clone());
        let target = Target::Key {
            provider: key.provider.clone(),
        };
        let held = match key.key.as_deref() {
            Some(secret) => {
                let tail: Vec<char> = secret.chars().rev().take(KEY_TAIL).collect();
                let tail: String = tail.into_iter().rev().collect();
                format!("key \u{b7}\u{b7}\u{b7}\u{b7}{tail} from {}", key.variable)
            }
            None => format!("key from {}", key.variable),
        };
        let pair = (key.provider.clone(), role.clone());
        let confirming = self.removing.as_ref() == Some(&pair);
        let detail = match self.editing(&target) {
            Some(editing) => Self::field_line(editing, theme, cx),
            None => small(format!("{held} \u{b7} {role}"), theme),
        };
        row(theme)
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .flex_col()
                    .gap_0p5()
                    .line_height(px(ROW_LINE))
                    .child(display_name(&key.provider))
                    .child(detail),
            )
            .when(confirming, |row| {
                row.child(
                    button(
                        ("remove-key", at),
                        "Remove key",
                        None,
                        ButtonKind::Primary,
                        theme,
                    )
                    .on_click(Self::remove(&key.provider, &role, cx)),
                )
                .child(
                    button(("keep-key", at), "Keep", None, ButtonKind::Plain, theme)
                        .on_click(Self::ask_remove(None, cx)),
                )
            })
            .when(!confirming, |row| {
                row.child(
                    button(
                        ("replace-key", at),
                        "Replace",
                        None,
                        ButtonKind::Text,
                        theme,
                    )
                    .on_click(Self::edit(target.clone(), KEY_HINT, cx)),
                )
                .child(
                    button(("ask-remove", at), "Remove", None, ButtonKind::Text, theme)
                        .on_click(Self::ask_remove(Some(pair.clone()), cx)),
                )
            })
    }

    fn accounts(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let rows: Vec<Div> = match self.rows(cx) {
            Ok(rows) => rows
                .iter()
                .enumerate()
                .map(|(at, row)| match row {
                    Row::Subscription {
                        source,
                        account,
                        filled,
                    } => self.subscription(at, (source, account, *filled), theme, cx),
                    Row::Key(key) => self.key(at, key, theme, cx),
                })
                .collect(),
            Err(said) => vec![small(said, theme).py_2().px_1()],
        };
        outer_card(theme)
            .flex_col()
            .flex_1()
            .min_w_0()
            .max_w(relative(COLUMN_SHARE))
            .child(
                div()
                    .flex()
                    .flex_none()
                    .items_center()
                    .h(px(SHELL_HEADER))
                    .pl(px(9.0))
                    .pr_1p5()
                    .child(caption("Accounts", theme).flex_1())
                    .child(small("from tofu query.accounts", theme)),
            )
            .child(
                inner(theme)
                    .p_2()
                    .gap_1()
                    .text_size(px(DESC))
                    .children(rows)
                    .child(
                        small(ACCOUNTS_NOTE, theme)
                            .py_2()
                            .px_1()
                            .line_height(px(NOTE_LINE)),
                    ),
            )
    }

    fn add(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let sources: Vec<_> = SIGN_IN_SOURCES
            .iter()
            .map(|source| {
                button(
                    *source,
                    display_name(source),
                    None,
                    ButtonKind::Plain,
                    theme,
                )
                .on_click(Self::sign_in(source, cx))
            })
            .collect();
        div()
            .absolute()
            .top(px(POP_TOP))
            .left_0()
            .right_0()
            .flex()
            .justify_center()
            .child(
                popover(theme)
                    .w(px(POP_WIDTH))
                    .py_4()
                    .px(px(18.0))
                    .flex()
                    .flex_col()
                    .gap_3()
                    .text_size(px(POP_TEXT))
                    .child(
                        div()
                            .font_weight(FontWeight::SEMIBOLD)
                            .text_size(px(POP_TITLE))
                            .child("Add an account"),
                    )
                    .child(caption("Subscription, signs in through the browser", theme))
                    .child(div().flex().flex_wrap().gap_1p5().children(sources))
                    .child(small("A key is replaced or removed on its own row.", theme))
                    .child(
                        div().flex().justify_end().child(
                            button("close-add", "Close", None, ButtonKind::Plain, theme)
                                .on_click(Self::toggle_add(cx)),
                        ),
                    ),
            )
    }
}

fn state_chip(state: &AccountStatusState, theme: &Theme) -> Div {
    let (fill, text): (Rgba, Rgba) = match state {
        AccountStatusState::InUse => {
            let live = theme.color(ColorToken::StatusLive);
            (tint(live, STATE_TINT), live)
        }
        AccountStatusState::Standby
        | AccountStatusState::SetAside
        | AccountStatusState::Unread
        | AccountStatusState::Unchecked => (ink(theme, STANDBY_FILL), ink(theme, STANDBY_INK)),
        AccountStatusState::RefreshFailed
        | AccountStatusState::Expired
        | AccountStatusState::Refused
        | AccountStatusState::Spent
        | AccountStatusState::RateLimited
        | AccountStatusState::Unknown(_) => {
            let warn = theme.color(ColorToken::StatusWarn);
            (tint(warn, STATE_WARM_TINT), warn)
        }
    };
    div()
        .flex_none()
        .py(px(1.0))
        .px_2()
        .rounded_full()
        .bg(fill)
        .text_size(px(STATE_TEXT))
        .line_height(relative(1.3))
        .text_color(text)
        .child(state.said().to_owned())
}

fn small(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .text_size(px(SMALL))
        .text_color(ink(theme, T3))
        .child(text.into())
}

fn row(theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap_2p5()
        .py_2p5()
        .px_3()
        .rounded(px(ROW_RADIUS))
        .bg(tint(theme.color(ColorToken::Shadow), ROW_FILL))
}

impl Render for Accounts {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let add = header_button("add-account", "+ Add account", &theme)
            .on_click(Self::toggle_add(cx))
            .into_any_element();
        let body = div()
            .flex_1()
            .min_h_0()
            .flex()
            .flex_col()
            .gap_2p5()
            .overflow_hidden()
            .child(small("what pays for each model", &theme).text_size(px(DESC)))
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .relative()
                    .flex()
                    .gap_2p5()
                    .child(self.accounts(&theme, cx))
                    .when(self.adding, |grid| grid.child(self.add(&theme, cx))),
            );
        let told = self.told.clone().map(|message| {
            div()
                .absolute()
                .left_0()
                .right_0()
                .bottom(px(TOAST_BOTTOM))
                .flex()
                .justify_center()
                .child(toast(
                    message,
                    TELL_BADGE,
                    &theme,
                    cx.listener(|this, _: &ClickEvent, _, cx| {
                        this.told = None;
                        cx.notify();
                    }),
                ))
        });
        frame::window(frame::titled("accounts", Some(add)), &theme, body).children(told)
    }
}
