use super::frame;

use crate::desk::resets;
use crate::modules::chat::Chat;
use desk_core::model::Store;
use desk_core::protocol::{
    AccountStatus, Accounts, QuotaWindow, UsageReport, UsageReportState, WindowStatus,
};
use desk_core::query::{Answer, display_name};
use desk_ui::component::icon;
use desk_ui::components::avatar::spinner;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{caption, inner_card};
use desk_ui::components::charts::{DotMeter, cached};
use desk_ui::components::chip::badge;
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::paint::ink;
use desk_ui::components::size::T3;
use desk_ui::components::status_bar::Reset;
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::{ICON, ICON_SMALL};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, Entity, EntityId, FontWeight,
    SharedString, Subscription, WeakEntity, Window, div, prelude::*, px,
};

use frame::{ellipsis, fraction, load_fonts, note, panel, panes, title, window};

const CARD_LEAST: f32 = 320.0;
const STATE_DOT: f32 = 8.0;
const WINDOW_LEAST: f32 = 120.0;
const READING: &str =
    "tofu asks every signed-in provider for its windows, which takes about ten seconds.";
const NO_TOFU: &str =
    "Limits reads quota from the tofu the work screen runs, and no work screen is open here.";

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the limits screen draws tofu's quota windows, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|_| Limits {
            source: None,
            seen: Seen::default(),
            meters: Vec::new(),
        })
        .into())
}

pub struct Limits {
    source: Option<Source>,
    seen: Seen,
    meters: Vec<Meter>,
}

struct Source {
    chat: WeakEntity<Chat>,
    id: EntityId,
    _watch: Subscription,
}

#[derive(Default, PartialEq)]
struct Seen {
    usage: Answer<UsageReport>,
    accounts: Answer<Accounts>,
    quota: Vec<QuotaWindow>,
}

#[derive(Clone, PartialEq, Eq)]
struct MeterKey {
    source: String,
    account: i64,
    window: String,
}

struct Block {
    source: String,
    account: AccountStatus,
    email: Option<String>,
    windows: Vec<WindowStatus>,
}

struct Meter {
    key: MeterKey,
    percent: Option<u32>,
    view: Entity<DotMeter>,
}

impl Limits {
    pub fn read_from(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        if self
            .source
            .as_ref()
            .is_some_and(|source| source.id == chat.entity_id())
        {
            return;
        }
        let store = chat.read(cx).store().clone();
        let watch = cx.observe(&store, |limits, store, cx| limits.saw(&store, cx));
        self.source = Some(Source {
            chat: chat.downgrade(),
            id: chat.entity_id(),
            _watch: watch,
        });
        chat.update(cx, |chat, cx| {
            chat.want_usage(cx);
            chat.want_accounts(cx);
        });
        self.saw(&store, cx);
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let accounts = self
            .source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
            .map(|chat| chat.read(cx).accounts().clone())
            .unwrap_or_default();
        let store = store.read(cx);
        let seen = Seen {
            usage: store.usage.clone(),
            accounts,
            quota: store.quota.clone(),
        };
        if seen == self.seen {
            return;
        }
        self.seen = seen;
        if let Some(read) = &self.seen.accounts.read {
            let said: Vec<String> = self
                .blocks()
                .iter()
                .map(|block| {
                    let windows: Vec<String> = block
                        .windows
                        .iter()
                        .map(|window| format!("{} {:.0}%", window.id, window.used * 100.0))
                        .collect();
                    format!(
                        "{} #{} {} [{}]",
                        block.source,
                        block.account.id,
                        block.account.account,
                        windows.join(", ")
                    )
                })
                .collect();
            eprintln!(
                "desk: limits shows {} from query.accounts at {} and {} quota.updated windows",
                said.join("; "),
                read.at,
                self.seen.quota.len()
            );
        }
        cx.notify();
    }

    fn blocks(&self) -> Vec<Block> {
        let Some(read) = &self.seen.accounts.read else {
            return Vec::new();
        };
        read.value
            .subscriptions
            .iter()
            .flat_map(|subscription| {
                subscription.accounts.iter().map(|account| Block {
                    source: subscription.source.clone(),
                    account: account.clone(),
                    email: account.email().map(str::to_owned),
                    windows: account.live(&subscription.source, &self.seen.quota),
                })
            })
            .collect()
    }

    fn reread(&mut self, cx: &mut Context<Self>) {
        let Some(chat) = self
            .source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
        else {
            return eprintln!("desk: limits: the chat is gone, so nothing was asked");
        };
        eprintln!("desk: limits: read again");
        chat.update(cx, |chat, cx| chat.reread_usage(cx));
    }

    fn meter(
        &mut self,
        key: MeterKey,
        quota: &WindowStatus,
        cx: &mut Context<Self>,
    ) -> Entity<DotMeter> {
        let percent = Some((quota.used * 100.0).round().clamp(0.0, 100.0) as u32);
        if let Some(kept) = self
            .meters
            .iter()
            .find(|meter| meter.key == key && meter.percent == percent)
        {
            return kept.view.clone();
        }
        let view = cx.new(|_| DotMeter::new(percent, "", false, ""));
        self.meters.retain(|meter| meter.key != key);
        self.meters.push(Meter {
            key,
            percent,
            view: view.clone(),
        });
        view
    }

    fn header(&self, usage: &UsageReport, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let state = match &usage.state {
            UsageReportState::Serving => "serving",
            UsageReportState::NeedsAttention => "needs attention",
            UsageReportState::None => "nothing signed in",
            UsageReportState::Unknown(raw) => raw,
        };
        div()
            .flex()
            .flex_none()
            .flex_wrap()
            .items_center()
            .gap(px(10.0))
            .px(px(4.0))
            .child(title("Limits"))
            .child(badge(state, theme))
            .child(div().flex_1())
            .when(self.seen.usage.asking, |header| {
                header.child(spinner("limits-asking", theme))
            })
            .child(
                button(
                    "limits-reread",
                    "Read again",
                    None,
                    ButtonKind::Plain,
                    theme,
                )
                .on_click(cx.listener(|limits, _: &ClickEvent, _, cx| limits.reread(cx))),
            )
    }

    fn account(
        &mut self,
        block: &Block,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Div {
        let tag = format!("{}-{}", block.source, block.account.id);
        let serving = block.account.state.serving();
        let mark = if serving {
            div()
                .size(px(STATE_DOT))
                .rounded_full()
                .bg(theme.color(ColorToken::StatusLive))
        } else {
            div().child(icon(
                Icon::Warning,
                ICON_SMALL,
                theme.color(ColorToken::StatusWarn),
            ))
        };
        let state = tooltip(
            SharedString::from(format!("limits-state-tip-{tag}")),
            div()
                .id(SharedString::from(format!("limits-state-{tag}")))
                .flex_none()
                .size(px(ICON))
                .flex()
                .items_center()
                .justify_center()
                .child(mark),
            Edge::Frame,
            block.account.state.said().to_owned(),
            theme,
            window,
            cx,
        );
        let source = block.source.clone();
        let again = (!serving).then(|| {
            button(
                SharedString::from(format!("limits-sign-in-{tag}")),
                "Sign in again",
                None,
                ButtonKind::Text,
                theme,
            )
            .on_click(cx.listener(move |limits, _: &ClickEvent, _, cx| {
                if let Some(chat) = limits
                    .source
                    .as_ref()
                    .and_then(|source| source.chat.upgrade())
                {
                    chat.update(cx, |chat, cx| chat.sign_in(&source, cx));
                }
            }))
        });
        let windows: Vec<AnyElement> = block
            .windows
            .iter()
            .map(|quota| {
                let key = MeterKey {
                    source: block.source.clone(),
                    account: block.account.id,
                    window: quota.id.clone(),
                };
                let meter = self.meter(key, quota, cx);
                let reset = quota.resets_at.as_deref().map(|stamp| {
                    let reset = resets(stamp, chrono::Local::now()).unwrap_or_else(|| Reset {
                        left: stamp.to_owned().into(),
                        at: stamp.to_owned().into(),
                    });
                    tooltip(
                        SharedString::from(format!("limits-reset-tip-{tag}-{}", quota.id)),
                        div()
                            .id(SharedString::from(format!(
                                "limits-reset-{tag}-{}",
                                quota.id
                            )))
                            .flex()
                            .items_center()
                            .gap_1()
                            .text_size(px(12.0))
                            .text_color(ink(theme, T3))
                            .child(icon(Icon::Reset, ICON_SMALL, ink(theme, T3)))
                            .child(reset.left),
                        Edge::Frame,
                        reset.at,
                        theme,
                        window,
                        cx,
                    )
                });
                fraction(div(), 1.0, WINDOW_LEAST)
                    .flex()
                    .flex_col()
                    .gap(px(6.0))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .justify_between()
                            .child(caption(quota.id.clone(), theme))
                            .children(reset),
                    )
                    .child(cached(&meter, cx))
                    .into_any_element()
            })
            .collect();
        let plan = block
            .account
            .plan
            .as_deref()
            .map_or("subscription".to_owned(), display_name);
        fraction(panel(plan, None, theme), 1.0, CARD_LEAST).child(
            inner_card(theme)
                .flex_col()
                .gap(px(14.0))
                .p(px(14.0))
                .child(
                    div()
                        .flex()
                        .items_start()
                        .gap(px(8.0))
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .flex()
                                .flex_col()
                                .gap(px(2.0))
                                .child(
                                    div()
                                        .text_size(px(13.5))
                                        .font_weight(FontWeight::SEMIBOLD)
                                        .child(display_name(&block.source)),
                                )
                                .children(block.email.clone().map(|email| {
                                    ellipsis(div())
                                        .text_size(px(12.0))
                                        .text_color(ink(theme, T3))
                                        .child(email)
                                })),
                        )
                        .children(again)
                        .child(state),
                )
                .when(windows.is_empty(), |card| {
                    card.child(note("no window reported for this account", theme))
                })
                .child(div().flex().flex_wrap().gap(px(14.0)).children(windows)),
        )
    }

    fn read(&mut self, theme: &Theme, window: &mut Window, cx: &mut Context<Self>) -> AnyElement {
        let Some(read) = self.seen.usage.read.clone() else {
            return match self.seen.usage.failed.clone() {
                None => empty_state(
                    "limits-reading",
                    "Reading quota",
                    Some(READING.into()),
                    &[],
                    &[],
                    theme,
                    |_, _, _| {},
                )
                .into_any_element(),
                Some(error) => self.failed(error.to_string(), theme, cx),
            };
        };
        let usage = &read.value;
        let header = self.header(usage, theme, cx);
        let failed = self.seen.usage.failed.as_ref().map(|error| {
            note(format!("the last read failed: {error}"), theme)
                .text_color(theme.color(ColorToken::StatusWarn))
        });
        let blocks = self.blocks();
        let body = if self.seen.accounts.read.is_none() {
            note("reading query.accounts", theme).into_any_element()
        } else if blocks.is_empty() {
            let line = usage
                .missing
                .iter()
                .map(|missing| {
                    format!(
                        "{}: {}, run {}",
                        missing.label, missing.what, missing.command
                    )
                })
                .collect::<Vec<_>>()
                .join("\n");
            empty_state(
                "limits-none",
                "No subscription window to show",
                (!line.is_empty()).then(|| line.into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            )
            .into_any_element()
        } else {
            let cards: Vec<Div> = blocks
                .iter()
                .map(|block| self.account(block, theme, window, cx))
                .collect();
            panes().children(cards).into_any_element()
        };
        div()
            .flex()
            .flex_col()
            .gap(px(8.0))
            .child(header)
            .children(failed)
            .child(body)
            .into_any_element()
    }

    fn failed(&self, error: String, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let again = cx.listener(|limits, _: &(), _, cx| limits.reread(cx));
        empty_state(
            "limits-failed",
            "Quota could not be read",
            Some(error.into()),
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
}

impl Render for Limits {
    fn render(&mut self, surface: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = match self.source {
            None => empty_state(
                "limits-no-tofu",
                "No tofu to ask",
                Some(NO_TOFU.into()),
                &[],
                &[],
                &theme,
                |_, _, _| {},
            )
            .into_any_element(),
            Some(_) => self.read(&theme, surface, cx),
        };
        window(&theme, div().flex_1().flex().flex_col().child(body))
    }
}
