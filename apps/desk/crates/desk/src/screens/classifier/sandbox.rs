use desk_ui::components::card::inner_card;
use desk_ui::components::paint::{ink, tint};
use desk_ui::theme::Theme;
use gpui::{ClickEvent, Context, Div, Rgba, div, prelude::*, px, relative, rgb};

use super::fixture::{
    CHANGED_COMMANDS, SHIPPED_AGREE, SHIPPED_ASK, SHIPPED_DENY, TELL_APPLY, Verdict, decisions,
};
use super::kit::{
    BASE, GAP, T2, T3, button, cap, faint, figure, headline, mono, primary, shell, shell_head,
};
use super::{ALLOW, ASK, Classifier, DENY, DENY_TEXT};

const TOP_RISK: f32 = 3.0;
const PLOT: f32 = 120.0;
const AXIS: f32 = 22.0;
const DOT: f32 = 8.0;
const UNLABELLED: f32 = 0.35;
const STEP_INK: f32 = 0.45;
const CHANGES_SHOWN: usize = 6;
const NOTE: &str =
    "Mode stays shadow until you choose enforce; nothing here stops a command by itself.";

#[derive(Clone, Copy)]
enum Knob {
    Ask,
    Deny,
}

impl Classifier {
    pub(super) fn sandbox(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let (ask, deny) = (self.ask as f32 / 10.0, self.deny as f32 / 10.0);
        let shipped =
            |risk| Verdict::of(risk, SHIPPED_ASK as f32 / 10.0, SHIPPED_DENY as f32 / 10.0);
        let all = decisions();
        let labelled: Vec<_> = all
            .iter()
            .filter_map(|d| d.label.map(|label| (d.risk, label)))
            .collect();
        let agreeing = labelled
            .iter()
            .filter(|(risk, label)| Verdict::of(*risk, ask, deny) == *label)
            .count();
        let agree = format!(
            "{}%",
            (agreeing * 100 + labelled.len() / 2) / labelled.len().max(1)
        );
        let moved: Vec<_> = all
            .iter()
            .filter(|d| Verdict::of(d.risk, ask, deny) != shipped(d.risk))
            .collect();
        let count = |verdict| {
            all.iter()
                .filter(|d| Verdict::of(d.risk, ask, deny) == verdict)
                .count()
                .to_string()
        };
        let top = headline("Threshold sandbox")
            .h(px(28.0))
            .child(faint(
                "tool_gate · replays the last 88 decisions, no model call",
                13.0,
                T3,
                theme,
            ))
            .child(div().flex_1())
            .child(button("reset", "Reset", 28.0, theme).on_click(cx.listener(
                |this, _: &ClickEvent, _, cx| {
                    this.ask = SHIPPED_ASK;
                    this.deny = SHIPPED_DENY;
                    cx.notify();
                },
            )))
            .child(
                primary("apply", "Apply to this project", 28.0, theme)
                    .on_click(cx.listener(Self::tell(TELL_APPLY))),
            );
        let zone = |from: f32, color: u32, fill: f32| {
            div()
                .absolute()
                .top_0()
                .bottom(px(AXIS))
                .left(relative(from / TOP_RISK))
                .right_0()
                .bg(tint(rgb(color), fill))
                .border_l(px(1.5))
                .border_color(tint(rgb(color), 0.75))
        };
        let dot_color = |label: Option<Verdict>| -> Rgba {
            match label {
                Some(Verdict::Allow) => rgb(ALLOW),
                Some(Verdict::Ask) => rgb(ASK),
                Some(Verdict::Deny) => rgb(DENY),
                None => ink(theme, UNLABELLED),
            }
        };
        let plot = div()
            .relative()
            .h(px(PLOT))
            .child(zone(ask, ASK, 0.06))
            .child(zone(deny, DENY, 0.07))
            .children(all.iter().map(|d| {
                div()
                    .absolute()
                    .left(relative(d.risk / TOP_RISK))
                    .ml(px(-DOT / 2.0))
                    .top(px(d.top))
                    .size(px(DOT))
                    .rounded_full()
                    .bg(dot_color(d.label))
            }))
            .child(
                div()
                    .absolute()
                    .left_0()
                    .right_0()
                    .bottom_0()
                    .flex()
                    .justify_between()
                    .font_family(mono(theme))
                    .text_size(px(11.0))
                    .text_color(ink(theme, T3))
                    .children(["0", "1", "2", "3"]),
            );
        let risk = shell(theme)
            .flex_none()
            .child(
                shell_head(theme)
                    .child(cap("Every decision by risk", 10.0, theme).flex_1())
                    .child(faint(
                        "dot color is your label · grey is unlabelled",
                        12.0,
                        T3,
                        theme,
                    )),
            )
            .child(
                inner_card(theme)
                    .pt(px(18.0))
                    .px(px(20.0))
                    .pb(px(14.0))
                    .child(plot),
            );
        let knob =
            |knob: Knob, label: &'static str, color: u32, value: i32, shipped: &'static str| {
                let step = |sign: i32, glyph: &'static str| {
                    div()
                        .id((label, usize::from(sign > 0)))
                        .flex()
                        .items_center()
                        .justify_center()
                        .size(px(24.0))
                        .cursor_pointer()
                        .text_color(ink(theme, STEP_INK))
                        .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                            let slot = match knob {
                                Knob::Ask => &mut this.ask,
                                Knob::Deny => &mut this.deny,
                            };
                            *slot = slot.saturating_add(sign);
                            cx.notify();
                        }))
                        .child(glyph)
                };
                div()
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap_2()
                            .h(px(24.0))
                            .child(div().flex_1().text_color(rgb(color)).child(label))
                            .child(step(-1, "−"))
                            .child(
                                div()
                                    .w(px(40.0))
                                    .flex()
                                    .justify_center()
                                    .font_family(mono(theme))
                                    .text_size(px(15.0))
                                    .child(format!("{:.1}", value as f32 / 10.0)),
                            )
                            .child(step(1, "+")),
                    )
                    .child(faint(shipped, 12.0, T3, theme).mt_1())
            };
        let thresholds = shell(theme)
            .flex_1()
            .child(shell_head(theme).child(cap("Thresholds", 10.0, theme)))
            .child(
                inner_card(theme)
                    .py(px(14.0))
                    .px(px(16.0))
                    .gap(px(16.0))
                    .text_size(px(13.0))
                    .child(knob(Knob::Ask, "ask at risk", ASK, self.ask, "shipped 1.5"))
                    .child(knob(
                        Knob::Deny,
                        "deny at risk",
                        DENY_TEXT,
                        self.deny,
                        "shipped 2.5",
                    ))
                    .child(div().flex_1())
                    .child(faint(NOTE, 12.0, T3, theme).line_height(px(18.0))),
            );
        let tally = |label: &'static str, value: String, color: Rgba| {
            div()
                .flex_1()
                .child(faint(label, 11.5, T3, theme))
                .child(figure(value, 24.0, 28.8).text_color(color))
        };
        let line = if moved.is_empty() {
            format!("Same as the shipped thresholds: {SHIPPED_AGREE} agreement with your labels.")
        } else {
            format!(
                "{} decisions move against the shipped thresholds. Agreement with your labels is {agree}; it was {SHIPPED_AGREE}.",
                moved.len()
            )
        };
        let result = shell(theme)
            .flex_1()
            .child(shell_head(theme).child(cap("Result", 10.0, theme)))
            .child(
                inner_card(theme)
                    .py(px(14.0))
                    .px(px(16.0))
                    .gap(px(12.0))
                    .child(
                        div()
                            .flex()
                            .gap_2()
                            .child(tally("would ask", count(Verdict::Ask), rgb(ASK)))
                            .child(tally("would deny", count(Verdict::Deny), rgb(DENY_TEXT)))
                            .child(tally("agree with labels", agree.clone(), ink(theme, BASE))),
                    )
                    .child(faint(line, 12.5, T2, theme).line_height(px(19.0))),
            );
        let summary = if moved.is_empty() {
            "none".to_string()
        } else {
            format!("{} decisions", moved.len())
        };
        let changes = shell(theme)
            .flex_1()
            .child(
                shell_head(theme)
                    .child(cap("What changes", 10.0, theme).flex_1())
                    .child(faint(summary, 12.0, T3, theme)),
            )
            .child(
                inner_card(theme)
                    .p_2()
                    .gap(px(2.0))
                    .text_size(px(12.5))
                    .children(
                        moved
                            .iter()
                            .take(CHANGES_SHOWN)
                            .zip(CHANGED_COMMANDS.iter().cycle())
                            .map(|(d, command)| {
                                div()
                                    .flex()
                                    .items_start()
                                    .gap_2()
                                    .py_1p5()
                                    .px_2()
                                    .child(
                                        div()
                                            .flex_1()
                                            .font_family(mono(theme))
                                            .text_size(px(12.0))
                                            .line_height(px(17.0))
                                            .child(*command),
                                    )
                                    .child(
                                        faint(
                                            format!(
                                                "{} to {}",
                                                shipped(d.risk).name(),
                                                Verdict::of(d.risk, ask, deny).name()
                                            ),
                                            12.5,
                                            T3,
                                            theme,
                                        )
                                        .whitespace_nowrap(),
                                    )
                            }),
                    ),
            );
        let panels = div()
            .flex_1()
            .min_h_0()
            .flex()
            .gap(px(GAP))
            .child(thresholds)
            .child(result)
            .child(changes);
        div().child(top).child(risk).child(panels)
    }
}
