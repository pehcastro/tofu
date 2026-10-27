use std::rc::Rc;

use gpui::{
    App, AppContext, ClickEvent, Context, Entity, EntityInputHandler, EventEmitter, IntoElement,
    Render, SharedString, Subscription, Window, prelude::*, rgb_to_hsla,
};

use super::{
    Ascii, Charset, Click, Knob, LEAST_COLORS, MOST_COLORS, MOST_CUSTOM, Preset, Shape, Toggle,
};
use crate::components::color_picker::{ColorPicked, ColorPicker};
use crate::components::form::{FieldWidth, TextInput};
use crate::components::overlay::{Dismissed, MenuButton, MenuItem};
use crate::effects::controls::{self, Act, Settle, Stops, from_hsla, to_rgba};
use crate::live::ActiveTheme;

const CUSTOM_HINT: &str = "dimmest first, a space for empty";

pub enum AsciiChanged {
    Moving(Ascii),
    Settled(Ascii),
}

#[derive(Clone, Copy)]
enum Row {
    Knob(Knob),
    Toggle(Toggle),
}

const ROWS: [Row; 22] = [
    Row::Knob(Knob::Cell),
    Row::Knob(Knob::Density),
    Row::Knob(Knob::GlyphOpacity),
    Row::Knob(Knob::GradientOpacity),
    Row::Knob(Knob::Glow),
    Row::Knob(Knob::Depth),
    Row::Knob(Knob::Flicker),
    Row::Knob(Knob::Softness),
    Row::Knob(Knob::OriginX),
    Row::Knob(Knob::OriginY),
    Row::Knob(Knob::Angle),
    Row::Knob(Knob::Scale),
    Row::Knob(Knob::Curve),
    Row::Knob(Knob::Flow),
    Row::Knob(Knob::Speed),
    Row::Knob(Knob::Grain),
    Row::Knob(Knob::Variation),
    Row::Knob(Knob::Scramble),
    Row::Toggle(Toggle::Mouse),
    Row::Knob(Knob::Intro),
    Row::Toggle(Toggle::Light),
    Row::Toggle(Toggle::Underneath),
];

#[derive(Clone, Copy)]
enum Pick {
    Preset,
    Characters,
    Shape,
    Click,
}

impl Pick {
    const ALL: [Pick; 4] = [Pick::Preset, Pick::Characters, Pick::Shape, Pick::Click];

    fn id(self) -> &'static str {
        match self {
            Pick::Preset => "ascii-preset",
            Pick::Characters => "ascii-characters",
            Pick::Shape => "ascii-shape",
            Pick::Click => "ascii-click",
        }
    }

    fn name(self) -> &'static str {
        match self {
            Pick::Preset => "Preset",
            Pick::Characters => "Characters",
            Pick::Shape => "Shape",
            Pick::Click => "Click Effect",
        }
    }

    fn items(self) -> Vec<MenuItem> {
        let names: Vec<&'static str> = match self {
            Pick::Preset => Preset::ALL.map(Preset::name).to_vec(),
            Pick::Characters => Charset::ALL.map(Charset::name).to_vec(),
            Pick::Shape => Shape::ALL.map(Shape::name).to_vec(),
            Pick::Click => Click::ALL.map(Click::name).to_vec(),
        };
        names.into_iter().map(MenuItem::action).collect()
    }

    fn shown(self, ascii: &Ascii) -> &'static str {
        match self {
            Pick::Preset => ascii.preset().map_or("Custom", Preset::name),
            Pick::Characters => ascii.charset.name(),
            Pick::Shape => ascii.shape.name(),
            Pick::Click => ascii.click.name(),
        }
    }

    fn choose(self, at: usize, ascii: &mut Ascii) {
        match self {
            Pick::Preset => {
                if let Some(preset) = Preset::ALL.get(at) {
                    preset.apply(ascii);
                }
            }
            Pick::Characters => {
                if let Some(charset) = Charset::ALL.get(at) {
                    ascii.charset = *charset;
                }
            }
            Pick::Shape => {
                if let Some(shape) = Shape::ALL.get(at) {
                    ascii.shape = *shape;
                }
            }
            Pick::Click => {
                if let Some(click) = Click::ALL.get(at) {
                    ascii.click = *click;
                }
            }
        }
    }
}

struct Editing {
    at: usize,
    picker: Entity<ColorPicker>,
    _picked: Subscription,
}

pub struct AsciiPanel {
    ascii: Ascii,
    menus: [Entity<MenuButton>; 4],
    custom: Entity<TextInput>,
    editing: Option<Editing>,
    _typed: Subscription,
}

impl EventEmitter<AsciiChanged> for AsciiPanel {}

impl AsciiPanel {
    pub fn new(ascii: Ascii, window: &mut Window, cx: &mut Context<Self>) -> AsciiPanel {
        let menus = Pick::ALL.map(|pick| {
            let button = MenuButton::new(SharedString::default(), pick.items(), cx);
            let weak = cx.weak_entity();
            button.update(cx, |button, _| {
                button.on_pick(move |at, _, cx| {
                    if let Err(error) = weak.update(cx, |panel, cx| panel.picked(pick, *at, cx)) {
                        eprintln!("desk: ascii options closed before a pick landed: {error}");
                    }
                });
            });
            button
        });
        let custom = TextInput::new(CUSTOM_HINT.into(), window, cx);
        custom.update(cx, |input, cx| {
            input.sized(FieldWidth::Fill);
            let all = input.text().encode_utf16().count();
            input.replace_text_in_range(Some(0..all), &ascii.custom, window, cx);
        });
        let typed = cx.observe(&custom, |panel, input, cx| panel.typed(&input, cx));
        let panel = AsciiPanel {
            ascii,
            menus,
            custom,
            editing: None,
            _typed: typed,
        };
        panel.dress(cx);
        panel
    }

    pub fn ascii(&self) -> &Ascii {
        &self.ascii
    }

    fn dress(&self, cx: &mut App) {
        for (pick, menu) in Pick::ALL.into_iter().zip(&self.menus) {
            let value = pick.shown(&self.ascii);
            menu.update(cx, |button, cx| {
                button.trigger(move |_, theme| {
                    controls::picker_tile(pick.id(), pick.name(), value, theme)
                });
                cx.notify();
            });
        }
    }

    fn changed(&mut self, settle: Settle, cx: &mut Context<Self>) {
        self.dress(cx);
        let ascii = self.ascii.clone();
        cx.emit(match settle {
            Settle::Moving => AsciiChanged::Moving(ascii),
            Settle::Settled => AsciiChanged::Settled(ascii),
        });
        cx.notify();
    }

    fn picked(&mut self, pick: Pick, at: usize, cx: &mut Context<Self>) {
        pick.choose(at, &mut self.ascii);
        self.changed(Settle::Settled, cx);
    }

    fn typed(&mut self, input: &Entity<TextInput>, cx: &mut Context<Self>) {
        let custom: String = input.read(cx).text().chars().take(MOST_CUSTOM).collect();
        if custom == self.ascii.custom {
            return;
        }
        self.ascii.custom = custom;
        self.changed(Settle::Settled, cx);
    }

    pub fn turned(&mut self, knob: Knob, value: f32, settle: Settle, cx: &mut Context<Self>) {
        if settle == Settle::Moving && self.ascii.knob(knob) == value {
            return;
        }
        self.ascii.set_knob(knob, value);
        self.changed(settle, cx);
    }

    fn flipped(&mut self, toggle: Toggle, cx: &mut Context<Self>) {
        let on = !self.ascii.toggle(toggle);
        self.ascii.set_toggle(toggle, on);
        self.changed(Settle::Settled, cx);
    }

    fn restop(&mut self, more: bool, cx: &mut Context<Self>) {
        let colors = &mut self.ascii.colors;
        match more {
            true if colors.len() < MOST_COLORS => {
                let last = colors.last().copied().unwrap_or([255; 3]);
                colors.push(last);
            }
            false if colors.len() > LEAST_COLORS => {
                colors.pop();
            }
            true | false => return,
        }
        if self
            .editing
            .as_ref()
            .is_some_and(|editing| editing.at >= self.ascii.colors.len())
        {
            self.editing = None;
        }
        self.changed(Settle::Settled, cx);
    }

    fn edit(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        if self
            .editing
            .as_ref()
            .is_some_and(|editing| editing.at == at)
        {
            return self.close(cx);
        }
        let Some(color) = self.ascii.colors.get(at).copied() else {
            return;
        };
        let picker = cx.new(|cx| ColorPicker::new(rgb_to_hsla(to_rgba(color)), window, cx));
        let picked = cx.subscribe(&picker, move |panel, _, picked: &ColorPicked, cx| {
            let (color, settle) = match picked {
                ColorPicked::Moving(color) => (*color, Settle::Moving),
                ColorPicked::Settled(color) => (*color, Settle::Settled),
            };
            if let Some(stop) = panel.ascii.colors.get_mut(at) {
                *stop = from_hsla(color);
                panel.changed(settle, cx);
            }
        });
        self.editing = Some(Editing {
            at,
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

impl Render for AsciiPanel {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let swatches = self
            .ascii
            .colors
            .iter()
            .enumerate()
            .map(|(at, color)| {
                controls::swatch_editor(
                    at,
                    *color,
                    self.editing
                        .as_ref()
                        .filter(|editing| editing.at == at)
                        .map(|editing| editing.picker.clone()),
                    &theme,
                    cx.listener(move |panel, _: &ClickEvent, window, cx| {
                        panel.edit(at, window, cx)
                    }),
                    cx.listener(|panel, _: &Dismissed, _, cx| panel.close(cx)),
                )
            })
            .collect();
        let count = self.ascii.colors.len();
        let step = |more: bool, live: bool, cx: &mut Context<Self>| {
            live.then(|| {
                let act = cx.listener(move |panel, _: &ClickEvent, _, cx| panel.restop(more, cx));
                Rc::new(act) as Act
            })
        };
        let stops = Stops {
            fewer: step(false, count > LEAST_COLORS, cx),
            more: step(true, count < MOST_COLORS, cx),
        };
        let weak = cx.weak_entity();
        let rows: Vec<_> = ROWS
            .into_iter()
            .map(|row| match row {
                Row::Knob(knob) => {
                    let weak = weak.clone();
                    controls::slider_tile(
                        ("ascii-knob", knob as usize),
                        knob.dial(&self.ascii),
                        &theme,
                        move |value, settle, _, cx| {
                            if let Err(error) =
                                weak.update(cx, |panel, cx| panel.turned(knob, value, settle, cx))
                            {
                                eprintln!("desk: ascii options closed under a slider: {error}");
                            }
                        },
                    )
                    .into_any_element()
                }
                Row::Toggle(toggle) => controls::switch_tile(
                    ("ascii-toggle", toggle as usize),
                    toggle.name(),
                    self.ascii.toggle(toggle),
                    &theme,
                )
                .on_click(
                    cx.listener(move |panel, _: &ClickEvent, _, cx| panel.flipped(toggle, cx)),
                )
                .into_any_element(),
            })
            .collect();
        let tiles = controls::grid()
            .child(controls::colors_tile(
                "Colors",
                Some(stops),
                swatches,
                &theme,
            ))
            .children(self.menus.iter().cloned())
            .when(self.ascii.charset == Charset::Custom, |grid| {
                grid.child(controls::field_tile(
                    "Custom characters",
                    self.custom.clone(),
                    &theme,
                ))
            })
            .children(rows);
        controls::section(tiles)
    }
}
