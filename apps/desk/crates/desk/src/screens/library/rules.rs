use std::iter;

use desk_core::protocol::{OverrideListing, RuleListReport, RuleListing};
use desk_ui::components::card::caption;
use desk_ui::components::chip::{badge, mono};
use desk_ui::components::empty::empty_state;
use desk_ui::components::list::row;
use desk_ui::components::paint::ink;
use desk_ui::components::size::T2;
use desk_ui::theme::Theme;
use gpui::{AnyElement, ClickEvent, Context, Div, div, prelude::*, px};

use super::frame::{SHELL_PILLS, fraction, note, pills};
use super::{Library, fact, warn};

const ID_LEAST: f32 = 200.0;
const KIND_COLUMN: f32 = 84.0;
const MODE_COLUMN: f32 = 76.0;
const ORIGIN_COLUMN: f32 = 76.0;
const KINDS: [(&str, &str); 4] = [
    ("human", "Human"),
    ("structural", "Structural"),
    ("decision", "Decision"),
    ("measured", "Measured"),
];

impl Library {
    pub(super) fn rules_tab(
        &self,
        rules: &RuleListReport,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        if rules.rules.is_empty() {
            return empty_state(
                "library-no-rules",
                "No rules",
                Some(format!("tofu rules list found none from {}", rules.origin).into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            )
            .into_any_element();
        }
        let overridden: Vec<&OverrideListing> = rules
            .rules
            .iter()
            .filter_map(|rule| rule.r#override.as_ref())
            .collect();
        let stale = overridden.iter().filter(|over| over.stale).count();
        let kinds: Vec<(Option<&'static str>, &'static str)> = iter::once((None, "All"))
            .chain(
                KINDS
                    .into_iter()
                    .filter(|(kind, _)| rules.rules.iter().any(|rule| rule.kind == *kind))
                    .map(|(kind, name)| (Some(kind), name)),
            )
            .collect();
        let filter = div()
            .flex()
            .flex_wrap()
            .items_center()
            .gap(px(10.0))
            .px(px(6.0))
            .pt(px(4.0))
            .pb(px(6.0))
            .child(pills(
                "library-kind",
                &kinds,
                self.kind,
                &SHELL_PILLS,
                theme,
                cx,
                |library, kind| library.kind = kind,
            ))
            .child(note(
                format!(
                    "{} rules from {}, {} overridden, {stale} stale",
                    rules.rules.len(),
                    rules.origin,
                    overridden.len()
                ),
                theme,
            ));
        let columns = line(
            caption("Rule", theme),
            caption("Kind", theme),
            caption("Mode", theme),
            caption("Layer", theme),
        )
        .px(px(10.0))
        .pb(px(6.0));
        let rows = rules
            .rules
            .iter()
            .filter(|rule| self.kind.is_none_or(|kind| kind == rule.kind))
            .enumerate()
            .map(|(at, rule)| self.rule_row(at, rule, theme, cx));
        div()
            .flex()
            .flex_col()
            .child(filter)
            .child(columns)
            .children(rows)
            .into_any_element()
    }

    fn rule_row(
        &self,
        at: usize,
        rule: &RuleListing,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let more = rule.file.is_some() || rule.switch.is_some() || rule.r#override.is_some();
        let open = more && self.rule.as_deref() == Some(rule.id.as_str());
        let id = rule.id.clone();
        let cells = line(
            div().font_family(mono(theme)).child(rule.id.clone()),
            div()
                .text_color(ink(theme, T2))
                .child(kind_name(&rule.kind).to_owned()),
            div()
                .flex()
                .children(rule.mode.clone().map(|mode| badge(mode, theme))),
            div().text_color(ink(theme, T2)).child(rule.origin.clone()),
        );
        let face = row(("rule", at), open, false, theme).child(cells);
        let face = if more {
            face.on_click(cx.listener(move |library, _: &ClickEvent, _, cx| {
                library.rule = (library.rule.as_deref() != Some(id.as_str())).then(|| id.clone());
                cx.notify();
            }))
        } else {
            face.cursor_default()
        };
        div()
            .child(face)
            .children(open.then(|| rule_detail(rule, theme)))
    }
}

fn line(id: Div, kind: Div, mode: Div, origin: Div) -> Div {
    div()
        .w_full()
        .flex()
        .flex_wrap()
        .items_center()
        .child(fraction(id, 1.0, ID_LEAST))
        .child(
            div()
                .flex()
                .flex_none()
                .items_center()
                .child(kind.w(px(KIND_COLUMN)))
                .child(mode.w(px(MODE_COLUMN)))
                .child(origin.w(px(ORIGIN_COLUMN))),
        )
}

fn rule_detail(rule: &RuleListing, theme: &Theme) -> Div {
    let file = |path: &str| div().font_family(mono(theme)).child(path.to_owned());
    div()
        .flex()
        .flex_col()
        .gap(px(6.0))
        .py(px(8.0))
        .px(px(14.0))
        .text_size(px(12.5))
        .children(rule.file.as_deref().map(|path| fact("file", file(path), theme)))
        .children(rule.switch.clone().map(|switch| fact("switch", switch, theme)))
        .children(rule.r#override.as_ref().map(|over| {
            div()
                .flex()
                .flex_col()
                .gap(px(6.0))
                .child(fact(
                    "override",
                    format!("{} in {}", over.change, over.layer),
                    theme,
                ))
                .children(over.text.clone().map(|text| fact("text", text, theme)))
                .children(over.reason.clone().map(|reason| fact("reason", reason, theme)))
                .children(over.by.clone().map(|by| fact("by", by, theme)))
                .children(over.at.clone().map(|at| fact("at", at, theme)))
                .child(fact("written in", file(&over.file), theme))
                .when(over.stale, |facts| {
                    facts.child(warn(
                        match (over.version, over.current) {
                            (Some(version), Some(current)) => format!(
                                "written for @{version}; tofu now ships @{current}, so the shipped rule runs until this override is reviewed"
                            ),
                            _ => "this override is stale, so the shipped rule runs until it is reviewed".to_owned(),
                        },
                        theme,
                    ))
                })
        }))
}

fn kind_name(kind: &str) -> &str {
    KINDS
        .iter()
        .find(|(wire, _)| *wire == kind)
        .map_or(kind, |(_, name)| name)
}
