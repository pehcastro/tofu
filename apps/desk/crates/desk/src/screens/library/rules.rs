use desk_ui::components::card::inner_card;
use desk_ui::components::paint::{ink, ring, tint};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{ClickEvent, Context, Div, Stateful, div, prelude::*, px};

use super::fixture::{
    BUILT_IN, Mode, RECENT, RULES, Rule, TELL_EDIT_RULE, TELL_NEW_RULE, TELL_RUN_CHECK,
    TELL_SAVE_OVERRIDE, TELL_WHICH_RULES,
};
use super::kit::{
    BASE, GAP, ROW_ON, Spark, T2, T3, WELL, button, cap, faint, mono, pill, primary, shade, shell,
    shell_head, spark,
};
use super::{Filter, Library, group, segment};

const SUMMARY: &str = "142 rules · 3 overridden · 1 stale";
const DETAIL_WIDTH: f32 = 470.0;
const MODE_COLUMN: f32 = 92.0;
const LAYER_COLUMN: f32 = 90.0;
const FIRED_COLUMN: f32 = 110.0;
const KEY_COLUMN: f32 = 90.0;
const ROW_LINE: f32 = 18.0;
const DETAIL_LINE: f32 = 21.0;
const HEAD_LIGHT: f32 = 0.85;

impl Library {
    fn mode_of(&self, index: usize, rule: &Rule) -> Mode {
        self.modes
            .get(index)
            .copied()
            .flatten()
            .unwrap_or(rule.mode)
    }

    fn shown(&self, index: usize, rule: &Rule) -> bool {
        match self.filter {
            Filter::All => true,
            Filter::Fired => rule.fires > 0,
            Filter::Overridden => {
                rule.layer != BUILT_IN || self.modes.get(index).copied().flatten().is_some()
            }
        }
    }

    pub(super) fn rules(&self, top: Div, theme: &Theme, cx: &mut Context<Self>) -> (Div, Div) {
        let top = top.child(faint(SUMMARY, 12.5, T3, theme)).child(
            button("new-rule", "+ New rule", 28.0, theme)
                .on_click(cx.listener(Self::tell(TELL_NEW_RULE))),
        );
        let content = div()
            .flex_1()
            .min_h_0()
            .flex()
            .gap(px(GAP))
            .child(self.rule_list(theme, cx).flex_1())
            .child(self.rule_detail(theme, cx).w(px(DETAIL_WIDTH)).flex_none());
        (top, content)
    }

    fn rule_list(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let filters = [
            (Filter::All, "All"),
            (Filter::Fired, "Fired here"),
            (Filter::Overridden, "Overridden"),
        ];
        let head = shell_head(theme)
            .child(
                group(0.04, 8.0, theme).children(filters.map(|(filter, label)| {
                    segment(label, label, filter == self.filter, theme)
                        .py(px(3.0))
                        .px(px(10.0))
                        .rounded(px(6.0))
                        .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                            this.filter = filter;
                            cx.notify();
                        }))
                })),
            )
            .child(div().flex_1())
            .child(faint(
                "layers: built in · global · this project",
                12.0,
                T3,
                theme,
            ));
        let columns = div()
            .flex()
            .pt(px(6.0))
            .pb(px(7.0))
            .px(px(10.0))
            .child(cap("Rule", 11.5, theme).flex_1())
            .child(cap("Mode", 11.5, theme).w(px(MODE_COLUMN)))
            .child(cap("Layer", 11.5, theme).w(px(LAYER_COLUMN)))
            .child(
                cap("Fired, 7 days", 11.5, theme)
                    .w(px(FIRED_COLUMN))
                    .flex()
                    .justify_end(),
            );
        let rows = RULES
            .iter()
            .enumerate()
            .filter(|(index, rule)| self.shown(*index, rule))
            .map(|(index, rule)| self.rule_row(index, rule, theme, cx));
        shell(theme)
            .child(head)
            .child(inner_card(theme).p(px(6.0)).child(columns).children(rows))
    }

    fn rule_row(
        &self,
        index: usize,
        rule: &'static Rule,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let mode = self.mode_of(index, rule);
        let fired = rule.fires > 0;
        div()
            .id(rule.id)
            .flex()
            .items_center()
            .py(px(8.0))
            .px(px(10.0))
            .rounded(px(8.0))
            .cursor_pointer()
            .text_size(px(13.0))
            .when(index == self.rule, |row| row.bg(ink(theme, ROW_ON)))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.rule = index;
                cx.notify();
            }))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .line_height(px(ROW_LINE))
                    .child(
                        div()
                            .font_family(mono(theme))
                            .text_size(px(12.5))
                            .child(rule.id),
                    )
                    .child(faint(rule.text, 12.0, T3, theme).truncate()),
            )
            .child(
                div().w(px(MODE_COLUMN)).flex().child(
                    pill(mode.name(), mode.look().colors(theme), 11.5, 8.0)
                        .py(px(1.0))
                        .line_height(px(15.0)),
                ),
            )
            .child(faint(rule.layer, 12.5, T2, theme).w(px(LAYER_COLUMN)))
            .child(
                div()
                    .w(px(FIRED_COLUMN))
                    .flex()
                    .items_center()
                    .justify_end()
                    .gap_2()
                    .child(spark(Spark {
                        values: &rule.week,
                        width: 64.0,
                        height: 18.0,
                        floor: 16.0,
                        rise: 14.0,
                        stroke: ink(theme, if fired { 0.7 } else { 0.18 }),
                        area: fired.then(|| ink(theme, 0.07)),
                        dashed: !fired,
                        dot: None,
                    }))
                    .child(
                        faint(rule.fires.to_string(), 12.0, T2, theme)
                            .font_family(mono(theme))
                            .w(px(20.0))
                            .flex()
                            .justify_end(),
                    ),
            )
    }

    fn rule_detail(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let index = self.rule;
        let Some(rule) = RULES.get(index) else {
            return shell(theme);
        };
        let mode = self.mode_of(index, rule);
        let head = shell_head(theme)
            .child(
                div()
                    .font_family(mono(theme))
                    .text_color(ink(theme, HEAD_LIGHT))
                    .child(rule.id),
            )
            .child(div().flex_1())
            .child(faint(rule.file, 12.0, T3, theme));
        let modes = group(0.05, 7.0, theme).children(Mode::ALL.map(|choice| {
            segment(choice.name(), choice.name(), choice == mode, theme)
                .py(px(2.0))
                .px(px(9.0))
                .rounded(px(5.0))
                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    if let Some(slot) = this.modes.get_mut(index) {
                        *slot = Some(choice);
                    }
                    cx.notify();
                }))
        }));
        let facts = div()
            .flex()
            .flex_col()
            .gap(px(6.0))
            .text_size(px(12.5))
            .child(fact("kind", div().child(rule.kind), theme))
            .child(fact("fires when", div().child(rule.trigger), theme))
            .child(fact("mode", modes, theme));
        let reason = (mode == Mode::Off && rule.mode != Mode::Off).then(|| {
            div()
                .flex()
                .flex_col()
                .gap_2()
                .py(px(10.0))
                .px(px(12.0))
                .rounded(px(10.0))
                .bg(shade(theme, WELL))
                .text_size(px(13.0))
                .child(faint("Turning a rule off needs a reason; it is written as an override in this project and shown in tofu doctor.", 13.0, T2, theme))
                .child(
                    div()
                        .py(px(6.0))
                        .px(px(10.0))
                        .rounded(px(7.0))
                        .bg(ink(theme, 0.05))
                        .text_color(ink(theme, T3))
                        .child("we test blank titles elsewhere"),
                )
                .child(
                    div()
                        .flex()
                        .gap_1p5()
                        .items_center()
                        .child(primary("save-override", "Save override", 26.0, theme).on_click(cx.listener(Self::tell(TELL_SAVE_OVERRIDE))))
                        .child(faint("undo: tofu rules restore", 11.5, T3, theme).font_family(mono(theme))),
                )
        });
        let stale = rule.stale.then(|| {
            let warn = theme.color(ColorToken::StatusWarn);
            div()
                .py(px(10.0))
                .px(px(12.0))
                .rounded(px(10.0))
                .bg(tint(warn, 0.07))
                .shadow(vec![ring(tint(warn, 0.22))])
                .text_size(px(13.0))
                .text_color(warn)
                .child("Your override was written for @1; tofu now ships @2, so the shipped rule runs unchanged until you review it.")
        });
        let recent = div()
            .flex()
            .flex_col()
            .gap_1()
            .text_size(px(12.5))
            .children(RECENT.map(|(at, file, found)| {
                div()
                    .flex()
                    .gap(px(GAP))
                    .child(
                        faint(at, 12.5, T3, theme)
                            .font_family(mono(theme))
                            .w(px(44.0)),
                    )
                    .child(div().flex_1().font_family(mono(theme)).child(file))
                    .child(faint(found, 12.5, T2, theme))
            }));
        let actions = div()
            .flex()
            .gap_1p5()
            .child(
                button("edit-rule", "Edit in this project", 28.0, theme)
                    .on_click(cx.listener(Self::tell(TELL_EDIT_RULE))),
            )
            .child(
                button("run-check", "Run check now", 28.0, theme)
                    .on_click(cx.listener(Self::tell(TELL_RUN_CHECK))),
            )
            .child(
                button("which-rules", "Which rules fire for a task", 28.0, theme)
                    .on_click(cx.listener(Self::tell(TELL_WHICH_RULES))),
            );
        shell(theme).child(head).child(
            inner_card(theme)
                .py(px(16.0))
                .px(px(18.0))
                .gap(px(14.0))
                .text_size(px(13.5))
                .line_height(px(DETAIL_LINE))
                .text_color(ink(theme, BASE))
                .child(div().child(rule.text))
                .child(facts)
                .children(reason)
                .children(stale)
                .child(cap("Recent fires", 10.0, theme))
                .child(recent)
                .child(div().flex_1())
                .child(actions),
        )
    }
}

fn fact(key: &'static str, value: Div, theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap_3()
        .child(faint(key, 12.5, T3, theme).w(px(KEY_COLUMN)))
        .child(value)
}
