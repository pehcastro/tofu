use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::inner_card;
use desk_ui::components::list::separator;
use desk_ui::theme::Theme;
use gpui::{AnyElement, ClickEvent, Context, Div, div, prelude::*, px};

use super::frame::{Mark, note, panel, rich};
use super::{Classifier, Seen};

const STEP: f64 = 0.1;

struct Limit<'a> {
    name: &'a str,
    question: &'a str,
    reported: f64,
    values: Vec<(&'a Seen, f64)>,
}

impl Classifier {
    pub(super) fn sandbox(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let mut limits: Vec<Limit> = Vec::new();
        for seen in &self.seen {
            let Some(reason) = &seen.decision.reason else {
                continue;
            };
            match limits.iter_mut().find(|limit| limit.name == reason.limit) {
                Some(limit) => limit.values.push((seen, reason.value)),
                None => limits.push(Limit {
                    name: &reason.limit,
                    question: &reason.question,
                    reported: reason.threshold,
                    values: vec![(seen, reason.value)],
                }),
            }
        }
        let mut parts: Vec<AnyElement> = Vec::new();
        for (at, limit) in limits.iter().enumerate() {
            if at > 0 {
                parts.push(separator(theme).into_any_element());
            }
            parts.push(self.limit(at, limit, theme, cx).into_any_element());
        }
        if parts.is_empty() {
            parts.push(
                div()
                    .p(px(10.0))
                    .child(note(
                        "none of the decisions carried a value to replay",
                        theme,
                    ))
                    .into_any_element(),
            );
        }
        panel(
            "Threshold sandbox",
            Some(note("replays the decisions above, no model call", theme).into_any_element()),
            theme,
        )
        .child(inner_card(theme).py(px(4.0)).px(px(6.0)).children(parts))
    }

    fn limit(&self, at: usize, limit: &Limit, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let nudge = self.nudges.get(limit.name).copied().unwrap_or(0);
        let tried = limit.reported + f64::from(nudge) * STEP;
        let over = |threshold: f64| {
            limit
                .values
                .iter()
                .filter(|(_, value)| *value >= threshold)
                .count()
        };
        let step = |id: &'static str, label: &'static str, by: i32| {
            let name = limit.name.to_owned();
            button((id, at), label, None, ButtonKind::Plain, theme).on_click(cx.listener(
                move |classifier, _: &ClickEvent, _, cx| {
                    let nudge = classifier.nudges.entry(name.clone()).or_insert(0);
                    *nudge = if by == 0 { 0 } else { nudge.saturating_add(by) };
                    cx.notify();
                },
            ))
        };
        let tried_said = format!("{tried:.2}");
        let moved = limit
            .values
            .iter()
            .filter(|(_, value)| (*value >= tried) != (*value >= limit.reported));
        div()
            .flex()
            .flex_col()
            .gap(px(6.0))
            .p(px(10.0))
            .child(
                div()
                    .flex()
                    .flex_wrap()
                    .items_center()
                    .gap(px(8.0))
                    .child(rich(&[(limit.name, Mark::Mono)], theme))
                    .child(step("sandbox-lower", "-0.1", -1))
                    .child(rich(&[(&tried_said, Mark::Strong)], theme))
                    .child(step("sandbox-raise", "+0.1", 1))
                    .when(nudge != 0, |row| {
                        row.child(step("sandbox-reset", "Reset", 0))
                    })
                    .child(note(format!("tofu reported {:.2}", limit.reported), theme)),
            )
            .child(note(
                format!(
                    "{} of {} {} values at or over {tried:.2}, {} at or over {:.2}",
                    over(tried),
                    limit.values.len(),
                    limit.question,
                    over(limit.reported),
                    limit.reported
                ),
                theme,
            ))
            .children(moved.map(|(seen, value)| {
                let subject = format!("{} seq {}", seen.decision.tool, seen.decision.seq);
                let crossing = if *value >= tried {
                    "under to at or over"
                } else {
                    "at or over to under"
                };
                div()
                    .flex()
                    .flex_wrap()
                    .items_center()
                    .gap(px(8.0))
                    .child(rich(&[(&subject, Mark::Mono)], theme))
                    .child(note(format!("{value:.2}, {crossing}"), theme))
            }))
    }
}
