use std::time::Instant;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::form::TextInput;
use desk_ui::components::keymap::{Group, Keys, Refusal};
use desk_ui::components::settings::{SettingRow, Source, key_binding, setting_group};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::ColorToken;
use gpui::{
    ClickEvent, Context, Entity, FocusHandle, KeyDownEvent, Render, SharedString, Subscription,
    Window, div, prelude::*, px,
};

const SEARCH: &str = "Search keys";
const RESET_ALL: &str = "Reset all";
const RESET: &str = "Reset";
const PRESS: &str = "Press a chord, Esc cancels";
const OR: &str = "or";
const BAR_GAP: f32 = 12.0;
const CONTROL_GAP: f32 = 8.0;
const PROBLEM_TEXT: f32 = 12.0;
const MODIFIERS: [&str; 5] = ["shift", "control", "alt", "platform", "function"];

pub struct KeysPage {
    search: Entity<TextInput>,
    focus: FocusHandle,
    capturing: Option<String>,
    refused: Option<(String, Refusal)>,
    opened: Option<Instant>,
    _search: Subscription,
    _blur: Subscription,
}

impl KeysPage {
    pub(super) fn new(window: &mut Window, cx: &mut Context<Self>) -> Self {
        let opened = Instant::now();
        let search = TextInput::new(SEARCH.into(), window, cx);
        let focus = cx.focus_handle();
        KeysPage {
            _search: cx.observe(&search, |_, _, cx| cx.notify()),
            _blur: cx.on_blur(&focus, window, |page, _, cx| page.capture(None, cx)),
            search,
            focus,
            capturing: None,
            refused: None,
            opened: Some(opened),
        }
    }

    fn capture(&mut self, id: Option<String>, cx: &mut Context<Self>) {
        Keys::of(cx).set_capturing(id.is_some());
        self.capturing = id;
        cx.notify();
    }

    fn key(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        let Some(id) = self.capturing.clone() else {
            return;
        };
        let pressed = &event.keystroke;
        if MODIFIERS.contains(&pressed.key.as_str()) {
            return;
        }
        cx.stop_propagation();
        self.refused = None;
        if pressed.key != "escape" {
            Keys::of(cx).set_capturing(false);
            if let Err(refusal) = Keys::of(cx).rebind(&id, pressed) {
                eprintln!("desk: keys refused {id} {}: {refusal}", pressed.unparse());
                self.refused = Some((id, refusal));
            }
        }
        self.capture(None, cx);
    }

    fn reset(&mut self, id: Option<String>, cx: &mut Context<Self>) {
        self.refused = None;
        Keys::of(cx).reset(id.as_deref());
        cx.notify();
    }
}

fn shown(chord: &str) -> Vec<SharedString> {
    chord
        .split('+')
        .map(|part| {
            let mut chars = part.chars();
            chars
                .next()
                .map_or_else(
                    || "+".to_owned(),
                    |first| first.to_uppercase().chain(chars).collect(),
                )
                .into()
        })
        .collect()
}

impl Render for KeysPage {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let query = self.search.read(cx).text().trim().to_lowercase();
        let keys = Keys::of(cx);
        let total = keys.shortcuts().len();
        let found: Vec<(Group, Vec<_>)> = Group::ALL
            .into_iter()
            .map(|group| {
                let rows = keys
                    .shortcuts()
                    .iter()
                    .filter(|shortcut| shortcut.group == group)
                    .filter_map(|shortcut| {
                        let chords = keys.chords(shortcut);
                        let found = query.is_empty()
                            || shortcut.label.to_lowercase().contains(&query)
                            || chords.iter().any(|chord| chord.contains(&query));
                        found.then(|| (shortcut.clone(), chords, keys.rebound(&shortcut.id)))
                    })
                    .collect::<Vec<_>>();
                (group, rows)
            })
            .filter(|(_, rows)| !rows.is_empty())
            .collect();
        let groups: Vec<(Group, Vec<SettingRow>)> = found
            .into_iter()
            .map(|(group, rows)| {
                let rows = rows
                    .into_iter()
                    .map(|(shortcut, chords, rebound)| {
                        let id = shortcut.id.clone();
                        let capturing = self.capturing.as_ref() == Some(&id);
                        let refused = self
                            .refused
                            .as_ref()
                            .filter(|(held, _)| *held == id)
                            .map(|(_, refusal)| refusal.clone());
                        let held = match &refused {
                            Some(Refusal::Held(holder)) => Some(holder.clone().into()),
                            _ => None,
                        };
                        let problem = refused
                            .filter(|refusal| !matches!(refusal, Refusal::Held(_)))
                            .map(|refusal| refusal.to_string());
                        let binding = match capturing {
                            true => key_binding(vec![PRESS.into()], held, &theme),
                            false => key_binding(
                                chords
                                    .iter()
                                    .enumerate()
                                    .flat_map(|(at, chord)| {
                                        (at > 0).then(|| OR.into()).into_iter().chain(shown(chord))
                                    })
                                    .collect(),
                                held,
                                &theme,
                            ),
                        };
                        let picked = id.clone();
                        let reset = id.clone();
                        let control = div()
                            .flex()
                            .items_center()
                            .gap(px(CONTROL_GAP))
                            .children(problem.map(|problem| {
                                div()
                                    .text_size(px(PROBLEM_TEXT))
                                    .text_color(theme.color(ColorToken::StatusWarn))
                                    .child(problem)
                            }))
                            .child(
                                div()
                                    .id(SharedString::from(format!("keys-bind-{id}")))
                                    .when(!shortcut.fixed, |bind| bind.cursor_pointer())
                                    .child(binding)
                                    .when(!shortcut.fixed, |bind| {
                                        bind.on_click(cx.listener(
                                            move |page, _: &ClickEvent, window, cx| {
                                                cx.stop_propagation();
                                                page.refused = None;
                                                window.focus(&page.focus, cx);
                                                page.capture(Some(picked.clone()), cx);
                                            },
                                        ))
                                    }),
                            )
                            .when(rebound, |control| {
                                control.child(
                                    button(
                                        SharedString::from(format!("keys-reset-{id}")),
                                        RESET,
                                        None,
                                        ButtonKind::Plain,
                                        &theme,
                                    )
                                    .on_click(cx.listener(
                                        move |page, _: &ClickEvent, _, cx| {
                                            page.reset(Some(reset.clone()), cx)
                                        },
                                    )),
                                )
                            });
                        SettingRow {
                            id: SharedString::from(id),
                            name: shortcut.label.into(),
                            about: SharedString::default(),
                            source: match rebound {
                                true => Source::Global,
                                false => Source::Default,
                            },
                            control: control.into_any_element(),
                        }
                    })
                    .collect();
                (group, rows)
            })
            .collect();
        if let Some(opened) = self.opened.take() {
            window.on_next_frame(move |_, _| {
                eprintln!(
                    "desk: keys page painted in {:.2} ms with {total} actions",
                    opened.elapsed().as_secs_f64() * 1000.0
                )
            });
        }
        div()
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::key))
            .flex()
            .flex_col()
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(BAR_GAP))
                    .child(div().flex_1().child(self.search.clone()))
                    .child(
                        button("keys-reset-all", RESET_ALL, None, ButtonKind::Plain, &theme)
                            .on_click(
                                cx.listener(|page, _: &ClickEvent, _, cx| page.reset(None, cx)),
                            ),
                    ),
            )
            .children(
                groups
                    .into_iter()
                    .map(|(group, rows)| setting_group(group.label(), rows, &theme)),
            )
    }
}
