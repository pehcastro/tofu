use desk_ui::components::card::inner_card;
use desk_ui::components::paint::{ink, ring};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{ClickEvent, Context, Div, FontWeight, Stateful, div, prelude::*, px, relative};

use super::fixture::{AGENTS, Agent, TELL_DISABLE, TELL_NEW_AGENT, TELL_OPEN_FILE, Tier};
use super::kit::{
    BASE, GAP, T2, T3, WELL, button, cap, faint, figure, metric, mono, shade, shell, shell_head,
};
use super::{Library, group, segment};

const DETAIL_WIDTH: f32 = 480.0;
const KEY_COLUMN: f32 = 100.0;
const CARD_ON: f32 = 0.08;
const CARD_OFF: f32 = 0.035;
const CARD_RING: f32 = 0.12;
const CHIP_FILL: f32 = 0.07;
const CHIP_INK: f32 = 0.85;
const HEAD_LIGHT: f32 = 0.85;
const ICON: f32 = 13.0;
const NOTE: &str = "Changing the model writes .tofu/agent-models.yaml for this project; tiers are set in Settings.";

impl Library {
    fn tier_of(&self, index: usize, agent: &Agent) -> Tier {
        self.tiers
            .get(index)
            .copied()
            .flatten()
            .unwrap_or(agent.tier)
    }

    pub(super) fn agents(&self, top: Div, theme: &Theme, cx: &mut Context<Self>) -> (Div, Div) {
        let top = top.child(
            button("new-agent", "+ New agent", 28.0, theme)
                .on_click(cx.listener(Self::tell(TELL_NEW_AGENT))),
        );
        let cards = AGENTS.chunks(2).enumerate().map(|(pair, agents)| {
            div().flex().gap_2().children(
                agents
                    .iter()
                    .enumerate()
                    .map(|(offset, agent)| self.agent_card(pair * 2 + offset, agent, theme, cx)),
            )
        });
        let list = shell(theme)
            .flex_1()
            .child(
                shell_head(theme)
                    .child(cap("Agents", 10.0, theme).flex_1())
                    .child(faint("the lead picks one when it spawns", 12.0, T3, theme)),
            )
            .child(inner_card(theme).p_2().gap_2().children(cards));
        let content = div()
            .flex_1()
            .min_h_0()
            .flex()
            .gap(px(GAP))
            .child(list)
            .child(self.agent_detail(theme, cx).w(px(DETAIL_WIDTH)).flex_none());
        (top, content)
    }

    fn agent_card(
        &self,
        index: usize,
        agent: &'static Agent,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let on = index == self.agent;
        let chip = |text: String| {
            div()
                .flex()
                .items_center()
                .h(px(20.0))
                .px(px(9.0))
                .rounded(px(7.0))
                .bg(ink(theme, CHIP_FILL))
                .text_size(px(11.5))
                .line_height(relative(1.0))
                .font_weight(FontWeight::MEDIUM)
                .text_color(ink(theme, CHIP_INK))
                .child(text)
        };
        div()
            .id(("agent", index))
            .flex_1()
            .min_w_0()
            .flex()
            .flex_col()
            .gap_1p5()
            .py(px(12.0))
            .px(px(14.0))
            .rounded(px(11.0))
            .cursor_pointer()
            .bg(ink(theme, if on { CARD_ON } else { CARD_OFF }))
            .when(on, |card| card.shadow(vec![ring(ink(theme, CARD_RING))]))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.agent = index;
                cx.notify();
            }))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(robot(theme))
                    .child(div().font_weight(FontWeight::SEMIBOLD).child(agent.name))
                    .child(div().flex_1())
                    .child(faint(agent.origin, 11.5, T3, theme)),
            )
            .child(faint(agent.desc, 12.5, T2, theme).line_height(px(18.0)))
            .child(
                div()
                    .flex()
                    .gap_1p5()
                    .child(chip(self.tier_of(index, agent).name().into()))
                    .child(chip(format!("gate: {}", agent.gate))),
            )
    }

    fn agent_detail(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let index = self.agent;
        let Some(agent) = AGENTS.get(index) else {
            return shell(theme);
        };
        let tier = self.tier_of(index, agent);
        let head = shell_head(theme)
            .child(div().text_color(ink(theme, HEAD_LIGHT)).child(agent.name))
            .child(
                faint(agent.path, 11.5, T3, theme)
                    .font_family(mono(theme))
                    .flex_1()
                    .flex()
                    .justify_end(),
            );
        let tiers = group(0.05, 7.0, theme)
            .text_size(px(12.0))
            .children(Tier::ALL.map(|choice| {
                segment(choice.name(), choice.name(), choice == tier, theme)
                    .py(px(2.0))
                    .px(px(9.0))
                    .rounded(px(5.0))
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        if let Some(slot) = this.tiers.get_mut(index) {
                            *slot = Some(choice);
                        }
                        cx.notify();
                    }))
            }));
        let facts = div()
            .flex()
            .flex_col()
            .gap(px(9.0))
            .child(fact("model", tiers, theme))
            .child(fact(
                "runs on",
                div()
                    .font_family(mono(theme))
                    .text_size(px(12.5))
                    .child(tier.runs_on()),
                theme,
            ))
            .child(fact("tools", div().child(agent.tools), theme))
            .child(fact(
                "gate",
                div()
                    .flex()
                    .child(agent.gate)
                    .child(faint(" · up to 3 tries after the last edit", 13.0, T3, theme).ml_1()),
                theme,
            ))
            .child(fact("references", div().child(agent.refs), theme));
        let stats = div()
            .flex()
            .gap(px(GAP))
            .py(px(12.0))
            .px(px(14.0))
            .rounded(px(11.0))
            .bg(shade(theme, WELL))
            .child(metric("runs this week", figure(agent.runs, 20.0, 26.0), theme).flex_1())
            .child(
                metric(
                    "gate passed first try",
                    figure(agent.first, 20.0, 26.0),
                    theme,
                )
                .flex_1(),
            )
            .child(metric("steps, median", figure(agent.steps, 20.0, 26.0), theme).flex_1());
        let actions = div()
            .flex()
            .gap_1p5()
            .child(
                button("open-file", "Open the file", 28.0, theme)
                    .on_click(cx.listener(Self::tell(TELL_OPEN_FILE))),
            )
            .child(
                button("disable", "Disable here", 28.0, theme)
                    .on_click(cx.listener(Self::tell(TELL_DISABLE))),
            );
        shell(theme).child(head).child(
            inner_card(theme)
                .py(px(16.0))
                .px(px(18.0))
                .gap(px(14.0))
                .text_size(px(13.0))
                .text_color(ink(theme, BASE))
                .child(faint(agent.desc, 13.0, T2, theme).line_height(px(20.0)))
                .child(facts)
                .child(stats)
                .child(div().flex_1())
                .child(faint(NOTE, 12.0, T3, theme))
                .child(actions),
        )
    }
}

fn fact(key: &'static str, value: Div, theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap_3()
        .child(faint(key, 13.0, T3, theme).w(px(KEY_COLUMN)))
        .child(value)
}

fn robot(theme: &Theme) -> Div {
    let color = theme.color(ColorToken::Trace);
    let scale = ICON / 16.0;
    div()
        .relative()
        .flex_none()
        .size(px(ICON))
        .child(
            div()
                .absolute()
                .left(px(3.0 * scale))
                .top(px(5.0 * scale))
                .w(px(10.0 * scale))
                .h(px(8.0 * scale))
                .rounded(px(2.0 * scale))
                .border_1()
                .border_color(color),
        )
        .child(
            div()
                .absolute()
                .left(px(8.0 * scale - 0.5))
                .top(px(2.5 * scale))
                .w(px(1.0))
                .h(px(2.5 * scale))
                .bg(color),
        )
}
