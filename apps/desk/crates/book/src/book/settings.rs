use desk_ui::components::chip::{chip, mono};
use desk_ui::components::form::{segmented, switch_bare};
use desk_ui::components::settings::{SettingRow, Source, key_binding, page_title, setting_group};
use desk_ui::theme::Theme;
use gpui::{ClickEvent, Context, Div, SharedString, div, prelude::*, px};

use super::Book;
use super::kit::block;

const COLUMN: f32 = 588.0;
const VALUE_TEXT: f32 = 12.0;
const SCOPES: [&str; 2] = ["Everywhere", "This project"];
const DENSITIES: [&str; 3] = ["compact", "comfortable", "roomy"];

const LOOK: [(&str, &str, &str, &str, Source); 4] = [
    ("theme", "Theme", "", "tofu", Source::Default),
    ("density", "Density", "", "comfortable", Source::Default),
    ("animations", "Animations", "", "subtle", Source::Global),
    (
        "glass",
        "Glass",
        "Frost everywhere; refraction where the GPU path allows it.",
        "frost",
        Source::Default,
    ),
];

const SWITCHES: [(&str, &str, &str, Source); 3] = [
    (
        "gatePrompt",
        "Ask before risky commands",
        "The lead stops for your answer when the classifier says ask. Off: it only logs.",
        Source::Default,
    ),
    (
        "turnMaySpawn",
        "Lead may spawn sub-agents",
        "Off keeps every change in the lead.",
        Source::Global,
    ),
    (
        "verifySubAgents",
        "Lead checks sub-agent work",
        "Re-runs checks and a browser check after a report. Costs requests.",
        Source::Project,
    ),
];

const KEYS: [(&str, &str, &str, Option<&str>, Source); 3] = [
    (
        "palette",
        "Command palette",
        "Ctrl K",
        None,
        Source::Default,
    ),
    ("split", "Split right", "Ctrl \\", None, Source::Global),
    (
        "closeTab",
        "Close tab",
        "Ctrl W",
        Some("Close tile"),
        Source::Project,
    ),
];

pub(super) struct SettingsPage {
    on: [bool; 3],
    scope: usize,
    density: usize,
}

impl SettingsPage {
    pub(super) fn new() -> Self {
        SettingsPage {
            on: [false, true, false],
            scope: 1,
            density: 1,
        }
    }

    pub(super) fn render(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let row = |id: &'static str, name: &'static str, about: &'static str, source, control| {
            SettingRow {
                id: id.into(),
                name: name.into(),
                about: about.into(),
                source,
                control,
            }
        };
        let look = LOOK
            .iter()
            .map(|&(id, name, about, value, source)| {
                let value = chip(value, None, theme)
                    .font_family(mono(theme))
                    .text_size(px(VALUE_TEXT));
                row(id, name, about, source, value.into_any_element())
            })
            .collect();
        let switches = SWITCHES
            .iter()
            .enumerate()
            .map(|(at, &(id, name, about, source))| {
                let on = self.on.get(at).copied().unwrap_or_default();
                let control = switch_bare(id, name, on, theme).on_click(cx.listener(
                    move |this, _: &ClickEvent, _, cx| {
                        if let Some(on) = this.settings.on.get_mut(at) {
                            *on = !*on;
                        }
                        let state = if on { "off" } else { "on" };
                        this.tell(format!("{name}: {state}"), cx);
                        cx.notify();
                    },
                ));
                row(id, name, about, source, control.into_any_element())
            })
            .collect();
        let density = segmented(
            "density-choice",
            &DENSITIES,
            self.density,
            theme,
            cx.listener(|this, at: &usize, _, cx| {
                this.settings.density = *at;
                cx.notify();
            }),
        );
        let scope = segmented(
            "scope-choice",
            &SCOPES,
            self.scope,
            theme,
            cx.listener(|this, at: &usize, _, cx| {
                this.settings.scope = *at;
                cx.notify();
            }),
        );
        let segmented_rows = vec![
            row(
                "density-row",
                "Density",
                "How much room rows and tiles take.",
                Source::Project,
                density.into_any_element(),
            ),
            row(
                "scope-row",
                "Scope",
                "Where a change is written.",
                Source::Default,
                scope.into_any_element(),
            ),
        ];
        let keys = KEYS
            .iter()
            .map(|&(id, name, keys, conflict, source)| {
                let binding = key_binding(
                    keys.split_whitespace().map(SharedString::from).collect(),
                    conflict.map(SharedString::from),
                    theme,
                );
                row(id, name, "", source, binding.into_any_element())
            })
            .collect();
        block(
            "Appearance, then one group per row kind",
            theme,
            div()
                .w(px(COLUMN))
                .flex()
                .flex_col()
                .gap_1p5()
                .child(page_title("Appearance", None, theme))
                .child(setting_group("Look", look, theme))
                .child(setting_group("Switches", switches, theme))
                .child(setting_group("Segmented", segmented_rows, theme))
                .child(setting_group("Key bindings", keys, theme)),
        )
    }
}
