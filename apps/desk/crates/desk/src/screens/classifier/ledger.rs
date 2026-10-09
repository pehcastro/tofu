use desk_core::protocol::{DecisionMade, GateAnswer, Verdict};
use desk_ui::components::card::inner_card;
use desk_ui::components::chip::{badge, mono};
use desk_ui::components::list::separator;
use desk_ui::theme::Theme;
use gpui::{AnyElement, Div, div, prelude::*, px};

use super::frame::{Mark, fraction, note, panel, panes, rich};
use super::{Classifier, Seen};

const POINT_LEAST: f32 = 150.0;
const COUNT_SIZE: f32 = 22.0;

impl Classifier {
    pub(super) fn points(&self, theme: &Theme) -> Div {
        let mut points: Vec<(&str, usize, bool)> = Vec::new();
        for seen in &self.seen {
            let decision = &seen.decision;
            match points
                .iter_mut()
                .find(|(point, _, _)| *point == decision.point)
            {
                Some((_, count, _)) => *count = count.saturating_add(1),
                None => points.push((&decision.point, 1, decision.enforced)),
            }
        }
        panes().children(points.into_iter().map(|(point, count, enforced)| {
            fraction(
                panel(
                    point.to_owned(),
                    Some(badge(mode(enforced), theme).into_any_element()),
                    theme,
                ),
                1.0,
                POINT_LEAST,
            )
            .child(
                inner_card(theme)
                    .py(px(10.0))
                    .px(px(12.0))
                    .gap(px(2.0))
                    .child(
                        div()
                            .text_size(px(COUNT_SIZE))
                            .font_family(mono(theme))
                            .child(count.to_string()),
                    )
                    .child(note("decisions in this session", theme)),
            )
        }))
    }

    pub(super) fn ledger(&self, theme: &Theme) -> Div {
        let mut rows: Vec<AnyElement> = Vec::new();
        for (at, seen) in self.seen.iter().enumerate() {
            if at > 0 {
                rows.push(separator(theme).into_any_element());
            }
            rows.push(row(seen, theme).into_any_element());
        }
        panel(
            "Ledger",
            Some(note("newest first", theme).into_any_element()),
            theme,
        )
        .child(inner_card(theme).py(px(4.0)).px(px(6.0)).children(rows))
    }
}

fn row(seen: &Seen, theme: &Theme) -> Div {
    let decision = &seen.decision;
    let (verdict, look) = match &decision.verdict {
        Verdict::Allow => ("allow", Mark::Strong),
        Verdict::Ask => ("ask", Mark::Warn),
        Verdict::Deny => ("deny", Mark::Warn),
        Verdict::Unknown(raw) => (raw.as_str(), Mark::Warn),
    };
    let turn = seen.when.said();
    div()
        .flex()
        .flex_col()
        .gap(px(4.0))
        .p(px(10.0))
        .child(
            div()
                .flex()
                .flex_wrap()
                .items_center()
                .gap(px(8.0))
                .child(rich(&[(&turn, Mark::Dim)], theme))
                .child(rich(&[(verdict, look)], theme))
                .child(badge(mode(decision.enforced), theme))
                .child(badge(decision.point.clone(), theme))
                .child(rich(&[(&decision.tool, Mark::Mono)], theme)),
        )
        .child(note(value_said(decision), theme))
        .children(
            (!decision.answers.is_empty()).then(|| note(answers_said(&decision.answers), theme)),
        )
}

pub(super) fn mode(enforced: bool) -> &'static str {
    if enforced { "enforced" } else { "shadow" }
}

pub(super) fn value_said(decision: &DecisionMade) -> String {
    let mut said = match &decision.reason {
        None => "no value sent".to_owned(),
        Some(reason) => {
            let mut said = format!(
                "{} {:.2} against {} {:.2}",
                reason.question, reason.value, reason.limit, reason.threshold
            );
            if reason.dead_band {
                said.push_str(", in the dead band");
            }
            if let Some(relaxed) = &reason.relaxed_by {
                said.push_str(&format!(", relaxed by {relaxed}"));
            }
            if reason.blocked {
                said.push_str(", blocked");
            }
            said
        }
    };
    if let Some(failure) = &decision.failure {
        said.push_str(&format!(", failed: {failure}"));
    }
    said
}

fn answers_said(answers: &[GateAnswer]) -> String {
    answers
        .iter()
        .map(|answer| match &answer.choice {
            Some(choice) => format!("{} {choice}", answer.question),
            None => format!(
                "{} {:.2} of {:.2}",
                answer.question, answer.value, answer.max
            ),
        })
        .collect::<Vec<_>>()
        .join(", ")
}
