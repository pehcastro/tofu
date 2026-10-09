use super::frame;

use crate::modules::chat::Chat;
use desk_core::model::Store;
use desk_core::protocol::{
    AccountStatus, AccountStatusState, Accounts, QuotaWindow, UsageReport, UsageReportState,
    WindowStatus,
};
use desk_core::query::Answer;
use desk_ui::components::avatar::spinner;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{caption, inner_card};
use desk_ui::components::charts::{DotMeter, cached};
use desk_ui::components::chip::badge;
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::paint::ink;
use desk_ui::components::size::T3;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, Entity, EntityId, SharedString,
    Subscription, WeakEntity, Window, div, prelude::*, px,
};

use frame::{Mark, ellipsis, fraction, load_fonts, note, panel, rich, title, window};

const ACCOUNT_LEAST: f32 = 150.0;
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
    windows: Vec<WindowStatus>,
}

struct Meter {
    key: MeterKey,
    percent: Option<u32>,
    reset: SharedString,
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
        read_at: &str,
        cx: &mut Context<Self>,
    ) -> Entity<DotMeter> {
        let percent = Some((quota.used * 100.0).round().clamp(0.0, 100.0) as u32);
        let reset: SharedString = match &quota.resets_at {
            None => "no reset reported".into(),
            Some(stamp) => format!("resets {}", clock(stamp, read_at)).into(),
        };
        if let Some(kept) = self
            .meters
            .iter()
            .find(|meter| meter.key == key && meter.percent == percent && meter.reset == reset)
        {
            return kept.view.clone();
        }
        let view = cx.new(|_| DotMeter::new(percent, "", false, reset.clone()));
        self.meters.retain(|meter| meter.key != key);
        self.meters.push(Meter {
            key,
            percent,
            reset,
            view: view.clone(),
        });
        view
    }

    fn header(
        &self,
        read_at: &str,
        usage: &UsageReport,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let read = if self.seen.usage.stale {
            format!(
                "read {}, percentages since then from quota.updated",
                clock(read_at, read_at)
            )
        } else {
            format!("read {}", clock(read_at, read_at))
        };
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
            .child(ellipsis(
                div()
                    .text_size(px(12.0))
                    .child(rich(&[(&read, Mark::Dim)], theme)),
            ))
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
        read_at: &str,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let (said, state) = match &block.account.state {
            AccountStatusState::InUse => ("in use", Mark::Dim),
            AccountStatusState::Standby => ("standby", Mark::Dim),
            AccountStatusState::SetAside => ("set aside", Mark::Dim),
            AccountStatusState::Unread => ("not read yet", Mark::Dim),
            AccountStatusState::Unchecked => ("not checked", Mark::Dim),
            AccountStatusState::RefreshFailed => ("refresh failed", Mark::Warn),
            AccountStatusState::Expired => ("expired", Mark::Warn),
            AccountStatusState::Refused => ("refused", Mark::Warn),
            AccountStatusState::Spent => ("spent", Mark::Warn),
            AccountStatusState::RateLimited => ("rate limited", Mark::Warn),
            AccountStatusState::Unknown(raw) => (raw.as_str(), Mark::Warn),
        };
        let label = fraction(div(), 1.0, ACCOUNT_LEAST)
            .flex()
            .flex_col()
            .items_start()
            .gap(px(4.0))
            .child(
                div()
                    .flex()
                    .flex_wrap()
                    .items_center()
                    .gap(px(6.0))
                    .text_size(px(13.5))
                    .w_full()
                    .child(rich(&[(&block.source, Mark::Strong)], theme))
                    .child(
                        ellipsis(div())
                            .text_color(ink(theme, T3))
                            .child(block.account.account.clone()),
                    )
                    .children(block.account.plan.clone().map(|plan| badge(plan, theme))),
            )
            .child(
                div()
                    .text_size(px(12.0))
                    .child(rich(&[(said, state)], theme)),
            );
        let windows: Vec<AnyElement> = block
            .windows
            .iter()
            .map(|quota| {
                let key = MeterKey {
                    source: block.source.clone(),
                    account: block.account.id,
                    window: quota.id.clone(),
                };
                let meter = self.meter(key, quota, read_at, cx);
                fraction(div(), 1.0, WINDOW_LEAST)
                    .flex()
                    .flex_col()
                    .gap(px(4.0))
                    .child(caption(quota.id.clone(), theme))
                    .child(cached(&meter, cx))
                    .into_any_element()
            })
            .collect();
        div()
            .flex()
            .flex_wrap()
            .items_start()
            .gap(px(14.0))
            .p(px(10.0))
            .child(label)
            .when(windows.is_empty(), |row| {
                row.child(fraction(
                    note("no window reported for this account", theme),
                    1.0,
                    WINDOW_LEAST,
                ))
            })
            .children(windows)
    }

    fn read(&mut self, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
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
        let header = self.header(&read.at, usage, theme, cx);
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
            let rows: Vec<Div> = blocks
                .iter()
                .map(|block| self.account(block, &read.at, theme, cx))
                .collect();
            let keys: Vec<String> = self
                .seen
                .accounts
                .read
                .iter()
                .flat_map(|read| &read.value.keys)
                .map(|key| format!("{} {}", key.provider, String::from(key.role.clone())))
                .collect();
            let trailing = usage
                .fullest_window
                .as_ref()
                .map(|fullest| note(format!("fullest {fullest}"), theme).into_any_element());
            panel("Windows", trailing, theme)
                .child(
                    inner_card(theme)
                        .py(px(4.0))
                        .px(px(6.0))
                        .children(rows)
                        .child(
                            div()
                                .flex()
                                .flex_wrap()
                                .gap_x(px(14.0))
                                .p(px(10.0))
                                .text_size(px(12.0))
                                .text_color(ink(theme, T3))
                                .when(!keys.is_empty(), |line| {
                                    line.child(caption("Keys", theme)).child(keys.join(" · "))
                                })
                                .child(usage.spend_limit.clone()),
                        ),
                )
                .into_any_element()
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

fn clock(stamp: &str, read_at: &str) -> String {
    let local = |at: &str| {
        chrono::DateTime::parse_from_rfc3339(at).map(|at| at.with_timezone(&chrono::Local))
    };
    let Ok(at) = local(stamp) else {
        return stamp.to_owned();
    };
    let today = local(read_at).is_ok_and(|read| read.date_naive() == at.date_naive());
    at.format(if today { "%H:%M" } else { "%m-%d %H:%M" })
        .to_string()
}

impl Render for Limits {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
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
            Some(_) => self.read(&theme, cx),
        };
        window(&theme, div().flex_1().flex().flex_col().child(body))
    }
}
