use super::frame;

use crate::modules::chat::{Chat, windows_said};
use desk_core::model::Store;
use desk_core::protocol::QuotaWindow;
use desk_core::query::{Answer, Provider, SERVING, Usage, UsageState, Window as Quota};
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
    usage: Answer<Usage>,
    quota: Vec<QuotaWindow>,
}

struct Meter {
    key: (usize, String),
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
        chat.update(cx, |chat, cx| chat.want_usage(cx));
        self.saw(&store, cx);
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let store = store.read(cx);
        let seen = Seen {
            usage: store.usage.clone(),
            quota: store.quota.clone(),
        };
        if seen == self.seen {
            return;
        }
        self.seen = seen;
        if let Some(read) = &self.seen.usage.read {
            eprintln!(
                "desk: limits shows {} from query.usage at {}{}",
                windows_said(&self.providers()),
                read.at,
                if self.seen.usage.stale {
                    " and quota.updated after it"
                } else {
                    ""
                }
            );
        }
        cx.notify();
    }

    fn providers(&self) -> Vec<Provider> {
        match &self.seen.usage.read {
            None => Vec::new(),
            Some(read) if self.seen.usage.stale => read.value.live(&self.seen.quota),
            Some(read) => read.value.providers.clone(),
        }
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
        key: (usize, String),
        quota: &Quota,
        read_at: &str,
        cx: &mut Context<Self>,
    ) -> Entity<DotMeter> {
        let percent = quota
            .used_reported
            .then(|| (quota.used_fraction * 100.0).round().clamp(0.0, 100.0) as u32);
        let reset: SharedString = match &quota.resets_at {
            None if quota.used_reported => "no reset reported".into(),
            None => "not reported".into(),
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

    fn header(&self, read_at: &str, usage: &Usage, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let read = if self.seen.usage.stale {
            format!(
                "read {}, percentages since then from quota.updated",
                clock(read_at, read_at)
            )
        } else {
            format!("read {}", clock(read_at, read_at))
        };
        let state = match usage.state {
            UsageState::Serving => "serving",
            UsageState::NeedsAttention => "needs attention",
            UsageState::Nothing => "nothing signed in",
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
        at: usize,
        provider: &Provider,
        read_at: &str,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let state = if provider.state == SERVING {
            Mark::Dim
        } else {
            Mark::Warn
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
                    .child(rich(&[(&provider.provider, Mark::Strong)], theme))
                    .children(provider.plan.clone().map(|plan| badge(plan, theme))),
            )
            .child(
                div()
                    .text_size(px(12.0))
                    .child(rich(&[(&provider.state, state)], theme)),
            );
        let windows: Vec<AnyElement> = provider
            .windows
            .iter()
            .map(|quota| {
                let meter = self.meter((at, quota.id.clone()), quota, read_at, cx);
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
        let providers = self.providers();
        let body = if providers.is_empty() {
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
            let rows: Vec<Div> = providers
                .iter()
                .enumerate()
                .map(|(at, provider)| self.account(at, provider, &read.at, theme, cx))
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
                                .p(px(10.0))
                                .text_size(px(12.0))
                                .text_color(ink(theme, T3))
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
    let (Some(day), Some(time)) = (stamp.get(..10), stamp.get(11..16)) else {
        return stamp.to_owned();
    };
    match (read_at.get(..10) == Some(day), stamp.get(5..10)) {
        (false, Some(date)) => format!("{date} {time} UTC"),
        _ => format!("{time} UTC"),
    }
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
