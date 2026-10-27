use desk_ui::components::color_picker::{ColorPicked, ColorPicker};
use desk_ui::components::overlay::Dismissed;
use desk_ui::effects::Rgb;
use desk_ui::effects::controls::{self, Dial, Settle, from_hsla, to_rgba};
use desk_ui::live::ActiveTheme;
use gpui::{
    AppContext, ClickEvent, Context, Entity, EventEmitter, IntoElement, Render, Subscription,
    Window, prelude::*, rgb_to_hsla,
};

use crate::screens::intro::remembered::Effect;
use crate::screens::intro::tuning::{Hue, Knob, Tuning, hues, knobs};

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Card {
    Effect(Effect),
    Motion,
}

pub enum Tuned {
    Knob(Effect, Knob, f32, Settle),
    Hue(Hue, Rgb, Settle),
    Speed(f32, Settle),
}

struct Editing {
    hue: Hue,
    picker: Entity<ColorPicker>,
    _picked: Subscription,
}

pub struct TunePanel {
    tuning: Tuning,
    card: Card,
    speed: Dial,
    editing: Option<Editing>,
}

impl EventEmitter<Tuned> for TunePanel {}

impl TunePanel {
    pub fn new(tuning: Tuning, card: Card, speed: Dial) -> TunePanel {
        TunePanel {
            tuning,
            card,
            speed,
            editing: None,
        }
    }

    pub fn card(&self) -> Card {
        self.card
    }

    pub fn speed(&mut self, speed: Dial, cx: &mut Context<Self>) {
        if self.speed != speed {
            self.speed = speed;
            cx.notify();
        }
    }

    fn edit(&mut self, hue: Hue, window: &mut Window, cx: &mut Context<Self>) {
        if self
            .editing
            .as_ref()
            .is_some_and(|editing| editing.hue == hue)
        {
            return self.close(cx);
        }
        let start = rgb_to_hsla(to_rgba(self.tuning.hue(hue)));
        let picker = cx.new(|cx| ColorPicker::new(start, window, cx));
        let picked = cx.subscribe(&picker, move |panel, _, picked: &ColorPicked, cx| {
            let (color, settle) = match picked {
                ColorPicked::Moving(color) => (from_hsla(*color), Settle::Moving),
                ColorPicked::Settled(color) => (from_hsla(*color), Settle::Settled),
            };
            panel.tuning.set_hue(hue, color);
            cx.emit(Tuned::Hue(hue, color, settle));
            cx.notify();
        });
        self.editing = Some(Editing {
            hue,
            picker,
            _picked: picked,
        });
        cx.notify();
    }

    fn close(&mut self, cx: &mut Context<Self>) {
        if self.editing.take().is_some() {
            cx.notify();
        }
    }
}

impl Render for TunePanel {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        desk_perf::span!("intro-setup-tune");
        let theme = ActiveTheme::theme(cx);
        let weak = cx.weak_entity();
        let tiles = controls::grid();
        let tiles = match self.card {
            Card::Motion => tiles.child(controls::slider_tile(
                "tune-speed",
                self.speed,
                &theme,
                move |value, settle, _, cx| {
                    let moved = weak.update(cx, |panel, cx| {
                        panel.speed.value = value;
                        cx.emit(Tuned::Speed(value, settle));
                        cx.notify();
                    });
                    if let Err(error) = moved {
                        eprintln!("desk: intro options closed under a slider: {error}");
                    }
                },
            )),
            Card::Effect(effect) => {
                let swatches: Vec<_> = hues(effect)
                    .iter()
                    .enumerate()
                    .map(|(at, hue)| {
                        let hue = *hue;
                        controls::swatch_editor(
                            at,
                            self.tuning.hue(hue),
                            self.editing
                                .as_ref()
                                .filter(|editing| editing.hue == hue)
                                .map(|editing| editing.picker.clone()),
                            &theme,
                            cx.listener(move |panel, _: &ClickEvent, window, cx| {
                                panel.edit(hue, window, cx)
                            }),
                            cx.listener(|panel, _: &Dismissed, _, cx| panel.close(cx)),
                        )
                    })
                    .collect();
                let title = match hues(effect) {
                    [one] => one.name(),
                    _ => "Colours",
                };
                let sliders = knobs(effect).iter().enumerate().map(|(at, knob)| {
                    let (knob, weak) = (*knob, weak.clone());
                    controls::slider_tile(
                        ("tune-knob", at),
                        self.tuning.dial(effect, knob),
                        &theme,
                        move |value, settle, _, cx| {
                            let moved = weak.update(cx, |panel, cx| {
                                let same = panel.tuning.knob(effect, knob) == value;
                                if settle == Settle::Moving && same {
                                    return;
                                }
                                panel.tuning.set_knob(effect, knob, value);
                                cx.emit(Tuned::Knob(effect, knob, value, settle));
                                cx.notify();
                            });
                            if let Err(error) = moved {
                                eprintln!("desk: intro options closed under a slider: {error}");
                            }
                        },
                    )
                });
                tiles
                    .when(!swatches.is_empty(), |grid| {
                        grid.child(controls::colors_tile(title, None, swatches, &theme))
                    })
                    .children(sliders)
            }
        };
        controls::section(tiles)
    }
}
