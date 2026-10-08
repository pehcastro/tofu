use desk_core::query::{Agents, Definition, Runs};
use desk_ui::components::card::inner_card;
use desk_ui::components::chip::{chip, mono};
use desk_ui::components::empty::empty_state;
use desk_ui::components::paint::{ink, ring};
use desk_ui::components::size::T2;
use desk_ui::theme::Theme;
use gpui::{AnyElement, ClickEvent, Context, Div, FontWeight, Stateful, div, prelude::*, px};

use super::frame::{fraction, note, panel, panes};
use super::{Library, fact, warn};

const LIST_LEAST: f32 = 380.0;
const DETAIL_LEAST: f32 = 340.0;
const CARD_LEAST: f32 = 240.0;
const CARD_ON: f32 = 0.08;
const CARD_OFF: f32 = 0.035;
const CARD_RING: f32 = 0.12;
const NO_AGENTS: &str = "tofu agents found no definition in the library or in this project.";

impl Library {
    pub(super) fn agents_tab(
        &self,
        agents: &Agents,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let Some(chosen) = agents
            .definitions
            .iter()
            .find(|agent| self.agent.as_deref() == Some(agent.name.as_str()))
            .or_else(|| agents.definitions.first())
        else {
            return empty_state(
                "library-no-agents",
                "No agents",
                Some(NO_AGENTS.into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            )
            .into_any_element();
        };
        let cards =
            agents.definitions.iter().enumerate().map(|(at, agent)| {
                self.agent_card(at, agent, agent.name == chosen.name, theme, cx)
            });
        let troubles = agents
            .broken
            .iter()
            .map(|broken| format!("{} is not read: {}", broken.path, broken.reason))
            .chain(agents.notices.iter().cloned())
            .map(|said| warn(said, theme));
        let list = panel(format!("{} agents", agents.definitions.len()), None, theme).child(
            inner_card(theme)
                .p_2()
                .gap_2()
                .child(div().flex().flex_wrap().gap_2().children(cards))
                .children(troubles),
        );
        panes()
            .child(fraction(list, 3.0, LIST_LEAST))
            .child(fraction(agent_detail(chosen, theme), 2.0, DETAIL_LEAST))
            .into_any_element()
    }

    fn agent_card(
        &self,
        at: usize,
        agent: &Definition,
        on: bool,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let name = agent.name.clone();
        fraction(div().id(("agent", at)), 1.0, CARD_LEAST)
            .flex()
            .flex_col()
            .gap_1p5()
            .py(px(12.0))
            .px(px(14.0))
            .rounded(px(11.0))
            .cursor_pointer()
            .bg(ink(theme, if on { CARD_ON } else { CARD_OFF }))
            .when(on, |card| card.shadow(vec![ring(ink(theme, CARD_RING))]))
            .on_click(cx.listener(move |library, _: &ClickEvent, _, cx| {
                library.agent = Some(name.clone());
                cx.notify();
            }))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(
                        div()
                            .font_weight(FontWeight::SEMIBOLD)
                            .child(agent.name.clone()),
                    )
                    .child(div().flex_1())
                    .child(note(agent.origin.clone(), theme)),
            )
            .child(
                div()
                    .text_size(px(12.5))
                    .line_height(px(18.0))
                    .text_color(ink(theme, T2))
                    .child(agent.description.clone()),
            )
            .child(
                div()
                    .flex()
                    .flex_wrap()
                    .gap_1p5()
                    .child(chip(model_said(agent), None, theme))
                    .when(!agent.gate.is_empty(), |chips| {
                        chips.child(chip(
                            format!("gate: {}", agent.gate.join(", ")),
                            None,
                            theme,
                        ))
                    }),
            )
    }
}

fn agent_detail(agent: &Definition, theme: &Theme) -> Div {
    let listed = |key: &'static str, items: &[String]| {
        (!items.is_empty()).then(|| fact(key, items.join(", "), theme))
    };
    let references: Vec<String> = agent
        .references
        .iter()
        .map(|reference| reference.name.clone())
        .collect();
    let shadowed: Vec<String> = agent
        .shadowed
        .iter()
        .map(|under| format!("{} from {}", under.path, under.origin))
        .collect();
    let model = div()
        .flex()
        .flex_col()
        .child(model_said(agent))
        .children(
            agent
                .written_model
                .as_ref()
                .map(|written| note(format!("written as {written}"), theme)),
        )
        .children(
            agent
                .assigned_in
                .as_ref()
                .map(|file| note(format!("assigned in {file}"), theme)),
        )
        .children(
            agent
                .from
                .as_ref()
                .map(|from| note(format!("set by {from}"), theme)),
        );
    let place = [agent.domain.as_deref(), agent.language.as_deref()]
        .into_iter()
        .flatten()
        .collect::<Vec<_>>()
        .join(", ");
    panel(agent.name.clone(), None, theme).child(
        inner_card(theme)
            .py(px(16.0))
            .px(px(18.0))
            .gap(px(10.0))
            .text_size(px(13.0))
            .child(
                div()
                    .text_color(ink(theme, T2))
                    .child(agent.description.clone()),
            )
            .child(fact(
                "file",
                div()
                    .font_family(mono(theme))
                    .text_size(px(12.0))
                    .child(agent.path.clone()),
                theme,
            ))
            .child(fact("model", model, theme))
            .children(
                agent
                    .effort
                    .clone()
                    .map(|effort| fact("effort", effort, theme)),
            )
            .children((!place.is_empty()).then(|| fact("domain", place, theme)))
            .children(listed("tools", &agent.tools))
            .children(listed("gate", &agent.gate))
            .children(listed("skills", &agent.skills))
            .children(listed("references", &references))
            .children(listed("cut references", &agent.cut_references))
            .children(listed("refused", &agent.refused))
            .children(listed("ignored", &agent.ignored))
            .children(listed("ignored tools", &agent.ignored_tools))
            .children(listed("shadows", &shadowed))
            .children(agent.notices.iter().map(|said| warn(said.clone(), theme))),
    )
}

fn model_said(agent: &Definition) -> String {
    match (agent.runs, &agent.model) {
        (Runs::Model, Some(model)) => model.clone(),
        (Runs::Model, None) => "its own model, not named".to_owned(),
        (Runs::Inherit, _) => "the lead's model".to_owned(),
        (Runs::Disabled, _) => "disabled".to_owned(),
        (Runs::Refused, _) => "refused".to_owned(),
    }
}
