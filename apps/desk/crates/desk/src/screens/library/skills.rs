use desk_ui::components::card::inner_card;
use desk_ui::components::paint::ink;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{ClickEvent, Context, Div, Rgba, Stateful, div, prelude::*, px};

use super::fixture::{
    Dot, EVAL_JUST_RAN, EVAL_NEVER, EVAL_RAN, EVAL_WHEN, Eval, Found, SKILLS, STEPS, Skill,
};
use super::kit::{
    BASE, GAP, ROW_ON, T2, T3, WELL, button, cap, ellipsis, faint, figure, fraction, metric, mono,
    panes, pill, shade, shell, shell_head,
};
use super::{Library, Look, group, segment};

const SUMMARY: &str = "5 found · 3 offered to the model · read again at every task";
const LIST_LEAST: f32 = 340.0;
const DETAIL_LEAST: f32 = 360.0;
const DOT: f32 = 8.0;
const LINE: f32 = 20.0;
const PLACE_LINE: f32 = 16.0;
const SOURCE_INK: f32 = 0.75;
const HEAD_LIGHT: f32 = 0.85;
const NO_DESCRIPTION: &str = "no description, so the model is never told about it";

enum Standing {
    Offered,
    Hidden,
    Shadowed,
    Undescribed,
}

impl Standing {
    fn name(&self) -> &'static str {
        match self {
            Standing::Offered => "offered",
            Standing::Hidden => "hidden by you",
            Standing::Shadowed => "shadowed",
            Standing::Undescribed => "left out: no description",
        }
    }

    fn look(&self) -> Look {
        match self {
            Standing::Offered => Look::Live,
            Standing::Shadowed => Look::Hushed,
            Standing::Hidden | Standing::Undescribed => Look::Warn,
        }
    }

    fn toggle(&self) -> &'static str {
        match self {
            Standing::Offered => "Hide from the model",
            Standing::Hidden => "Offer to the model",
            Standing::Shadowed => "Cannot offer: a closer copy wins",
            Standing::Undescribed => "Cannot offer: it needs a description",
        }
    }
}

impl Library {
    fn standing(&self, index: usize, skill: &Skill) -> Standing {
        match skill.found {
            Found::Offered if self.hidden.get(index).copied().unwrap_or(false) => Standing::Hidden,
            Found::Offered => Standing::Offered,
            Found::Shadowed => Standing::Shadowed,
            Found::Undescribed => Standing::Undescribed,
        }
    }

    pub(super) fn skills(&self, top: Div, theme: &Theme, cx: &mut Context<Self>) -> (Div, Div) {
        let top = top.child(faint(SUMMARY, 12.5, T3, theme));
        let list = shell(theme)
            .child(
                shell_head(theme)
                    .child(cap("Skills", 10.0, theme).flex_1())
                    .child(faint("first found wins a name", 12.0, T3, theme)),
            )
            .child(
                inner_card(theme).p(px(6.0)).gap(px(2.0)).children(
                    SKILLS
                        .iter()
                        .enumerate()
                        .map(|(index, skill)| self.skill_row(index, skill, theme, cx)),
                ),
            );
        let content = panes()
            .child(fraction(list, 1.0, LIST_LEAST))
            .child(fraction(self.skill_detail(theme, cx), 1.0, DETAIL_LEAST));
        (top, content)
    }

    fn skill_row(
        &self,
        index: usize,
        skill: &'static Skill,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let standing = self.standing(index, skill);
        div()
            .id(("skill", index))
            .flex()
            .items_start()
            .gap(px(GAP))
            .py(px(9.0))
            .px(px(10.0))
            .rounded(px(9.0))
            .cursor_pointer()
            .when(index == self.skill, |row| row.bg(ink(theme, ROW_ON)))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.skill = index;
                this.ran = false;
                cx.notify();
            }))
            .child(
                div()
                    .flex_none()
                    .mt(px(6.0))
                    .size(px(DOT))
                    .rounded_full()
                    .bg(dot(skill.dot, theme)),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .line_height(px(LINE))
                    .child(div().text_size(px(13.5)).child(skill.name))
                    .child(faint(skill.desc.unwrap_or(NO_DESCRIPTION), 12.0, T3, theme))
                    .child(
                        ellipsis(faint(skill.place, 11.0, T3, theme))
                            .font_family(mono(theme))
                            .line_height(px(PLACE_LINE)),
                    ),
            )
            .child(pill(standing.name(), standing.look().colors(theme), 11.5, 8.0).py(px(1.0)))
    }

    fn skill_detail(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let index = self.skill;
        let Some(skill) = SKILLS.get(index) else {
            return shell(theme);
        };
        let standing = self.standing(index, skill);
        let views = group(0.05, 7.0, theme).children([("Formatted", false), ("Source", true)].map(
            |(label, source)| {
                segment(label, label, source == self.source, theme)
                    .py(px(2.0))
                    .px(px(9.0))
                    .rounded(px(5.0))
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.source = source;
                        cx.notify();
                    }))
            },
        ));
        let head = shell_head(theme)
            .child(div().text_color(ink(theme, HEAD_LIGHT)).child(skill.name))
            .child(faint("SKILL.md", 12.0, T3, theme).flex_1())
            .child(views);
        let desc = skill.desc.unwrap_or("(no description)");
        let body = if self.source {
            source(skill.name, desc, theme)
        } else {
            div()
                .flex()
                .flex_col()
                .gap(px(12.0))
                .child(figure(skill.name, 18.0, 23.4))
                .child(div().text_color(ink(theme, T2)).child(desc))
                .child(figure("Steps", 14.5, 18.85).mt_1())
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .gap(px(2.0))
                        .text_color(ink(theme, T2))
                        .children(STEPS),
                )
        };
        shell(theme).child(head).child(
            inner_card(theme)
                .py(px(14.0))
                .px(px(18.0))
                .gap(px(12.0))
                .text_size(px(13.5))
                .line_height(px(21.0))
                .text_color(ink(theme, BASE))
                .child(body)
                .child(div().flex_1())
                .child(self.eval(skill, &standing, theme, cx)),
        )
    }

    fn eval(
        &self,
        skill: &Skill,
        standing: &Standing,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Div {
        let blank = Eval {
            followed: "not run",
            cost: "-",
            passed: "-",
        };
        let (shown, when) = match (&skill.eval, self.ran) {
            (_, true) => (&EVAL_RAN, EVAL_JUST_RAN),
            (Some(eval), false) => (eval, EVAL_WHEN),
            (None, false) => (&blank, EVAL_NEVER),
        };
        let index = self.skill;
        let toggles = matches!(standing, Standing::Offered | Standing::Hidden);
        div()
            .flex()
            .flex_col()
            .gap_2()
            .py(px(12.0))
            .px(px(14.0))
            .rounded(px(11.0))
            .bg(shade(theme, WELL))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(cap("Eval", 10.0, theme).flex_1())
                    .child(faint(when, 12.0, T3, theme)),
            )
            .child(
                div()
                    .flex()
                    .gap(px(GAP))
                    .text_size(px(12.5))
                    .child(
                        metric(
                            "followed when offered",
                            figure(shown.followed, 20.0, 26.0),
                            theme,
                        )
                        .flex_1(),
                    )
                    .child(
                        metric("cost per request", figure(shown.cost, 20.0, 26.0), theme).flex_1(),
                    )
                    .child(
                        metric(
                            "tasks passed, with vs without",
                            figure(shown.passed, 20.0, 26.0),
                            theme,
                        )
                        .flex_1(),
                    ),
            )
            .child(
                div()
                    .flex()
                    .flex_wrap()
                    .gap_1p5()
                    .child(
                        button(
                            "run-eval",
                            if self.ran {
                                "Ran on 3 tasks"
                            } else {
                                "Run eval"
                            },
                            26.0,
                            theme,
                        )
                        .on_click(cx.listener(
                            |this, _: &ClickEvent, _, cx| {
                                this.ran = true;
                                cx.notify();
                            },
                        )),
                    )
                    .child(
                        button("offer", standing.toggle(), 26.0, theme).on_click(cx.listener(
                            move |this, _: &ClickEvent, _, cx| {
                                if let (true, Some(hidden)) = (toggles, this.hidden.get_mut(index))
                                {
                                    *hidden = !*hidden;
                                    cx.notify();
                                }
                            },
                        )),
                    ),
            )
    }
}

fn dot(dot: Dot, theme: &Theme) -> Rgba {
    match dot {
        Dot::Live => theme.color(ColorToken::StatusLive),
        Dot::Grey => ink(theme, 0.25),
        Dot::Warn => theme.color(ColorToken::StatusWarn),
    }
}

fn source(name: &'static str, desc: &'static str, theme: &Theme) -> Div {
    let keyword = theme.color(ColorToken::SyntaxKeyword);
    let field = theme.color(ColorToken::SyntaxFunction);
    let line = |parts: Vec<(String, Option<Rgba>)>| {
        div()
            .flex()
            .min_h(px(20.0))
            .children(parts.into_iter().map(|(text, color)| {
                div()
                    .min_w_0()
                    .when_some(color, |part, color| part.text_color(color))
                    .child(text)
            }))
    };
    div()
        .font_family(mono(theme))
        .text_size(px(12.5))
        .line_height(px(20.0))
        .text_color(ink(theme, SOURCE_INK))
        .child(line(vec![("---".into(), Some(keyword))]))
        .child(line(vec![
            ("name".into(), Some(field)),
            (format!(": {name}"), None),
        ]))
        .child(line(vec![
            ("description".into(), Some(field)),
            (format!(": {desc}"), None),
        ]))
        .child(line(vec![("---".into(), Some(keyword))]))
        .child(div().h(px(20.0)))
        .child(line(vec![("## Steps".into(), Some(keyword))]))
        .children(
            STEPS
                .iter()
                .take(2)
                .map(|step| line(vec![((*step).into(), None)])),
        )
}
