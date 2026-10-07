use std::path::PathBuf;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{Header, caption, inner_card, shell};
use desk_ui::components::form::switch;
use desk_ui::components::list::separator;
use desk_ui::components::paint::{halo, ink, ring, tint};
use desk_ui::components::size::{T2, T3};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    Bounds, ClickEvent, Context, Div, FontWeight, ObjectFit, Pixels, Render, Rgba, SharedString,
    StyledImage, Window, canvas, div, fill, img, linear_color_stop, linear_gradient, point,
    prelude::*, px, relative, rgba, size,
};

use super::board::root;
use super::fixture::{
    ADD_IMAGE_TELL, EFFORT, Effect, IMAGE_DIR, IMAGES, LATEST_SESSION, MODEL, OPEN_INTRO_TELL,
    PROJECT,
};

const TITLE: f32 = 19.0;
const DESC: f32 = 13.0;
const SMALL: f32 = 12.0;
const LEFT_WIDTH: f32 = 420.0;
const THUMB_WIDTH: f32 = 92.0;
const THUMB_HEIGHT: f32 = 56.0;
const THUMB_RADIUS: f32 = 9.0;
const THUMB_RING: f32 = 0.1;
const ADD_RING: f32 = 0.14;
const PICKED_GAP: f32 = 2.0;
const PICKED_RING: f32 = 3.5;
const ROW_LINE: f32 = 18.0;
const PREVIEW_RADIUS: f32 = 12.0;
const COMPOSER_LEFT: f32 = 0.19;
const COMPOSER_TOP: f32 = 0.46;
const COMPOSER_WIDTH: f32 = 0.62;
const COMPOSER_RADIUS: f32 = 15.0;
const COMPOSER_FILL: u32 = 0x18171e8c;
const COMPOSER_RING: f32 = 0.16;
const COMPOSER_HINT: f32 = 12.5;
const COMPOSER_META: f32 = 11.5;
const SEND: f32 = 22.0;
const SEND_INK: f32 = 0.9;
const RESUME_INK: f32 = 0.7;
const SCAN_STEP: f32 = 3.0;
const SCAN_INK: f32 = 0.05;
const DITHER_STEP: f32 = 3.0;
const DITHER_LAYERS: [(f32, f32, f32); 2] = [(1.05, 0.44, 0.54), (1.45, 0.52, 0.62)];
const DITHER_SOLID: (f32, f32) = (0.60, 0.68);
const HALFTONE_STEP: f32 = 5.0;
const HALFTONE_DOT: f32 = 1.2;
const HALFTONE_INK: u32 = 0x0a08148c;
const HALFTONE_FROM: (f32, f32) = (0.38, 0.68);
const GRAIN_STEP: f32 = 3.0;
const GRAIN_INK: f32 = 0.2;
const FADE: (f32, f32) = (0.46, 0.80);
const FADE_INK: f32 = 0.98;
const GLOW: u32 = 0xff96d24d;
const GLOW_REACH: f32 = 0.4;
const DUOTONE: u32 = 0xf2b48c40;

pub struct Intro {
    image: usize,
    effects: [bool; 7],
    motion: bool,
    resume: bool,
    told: Option<&'static str>,
}

impl Intro {
    pub fn new() -> Self {
        Intro {
            image: 0,
            effects: Effect::ALL.map(Effect::on_at_start),
            motion: true,
            resume: true,
            told: None,
        }
    }

    fn on(&self, effect: Effect) -> bool {
        Effect::ALL
            .iter()
            .position(|known| *known == effect)
            .and_then(|at| self.effects.get(at).copied())
            .unwrap_or(false)
    }

    fn tell(
        message: &'static str,
        cx: &mut Context<Self>,
    ) -> impl Fn(&ClickEvent, &mut Window, &mut gpui::App) + 'static {
        cx.listener(move |this, _: &ClickEvent, _, cx| {
            this.told = Some(message);
            cx.notify();
        })
    }

    fn background(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let thumbs = IMAGES.iter().enumerate().map(|(at, name)| {
            let picked = at == self.image;
            div()
                .id(("image", at))
                .flex_none()
                .w(px(THUMB_WIDTH))
                .h(px(THUMB_HEIGHT))
                .rounded(px(THUMB_RADIUS))
                .cursor_pointer()
                .shadow(if picked {
                    vec![
                        halo(theme.color(ColorToken::AvatarRing), PICKED_GAP),
                        halo(theme.color(ColorToken::TextStrong), PICKED_RING),
                    ]
                } else {
                    vec![ring(ink(theme, THUMB_RING))]
                })
                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    this.image = at;
                    cx.notify();
                }))
                .child(picture(name).size_full().rounded(px(THUMB_RADIUS)))
        });
        let thumbs: Vec<_> = thumbs.collect();
        let effects = Effect::ALL.into_iter().enumerate().map(|(at, effect)| {
            switch_row(
                ("effect", at),
                effect.name(),
                effect.sub(),
                self.on(effect),
                theme,
            )
            .py(px(7.0))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                if let Some(on) = this.effects.get_mut(at) {
                    *on = !*on;
                    cx.notify();
                }
            }))
        });
        let effects: Vec<_> = effects.collect();
        inner_card(theme)
            .p(px(14.0))
            .gap_4()
            .text_size(px(DESC))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .child(caption("Image", theme))
                    .child(
                        div().flex().gap_2().children(thumbs).child(
                            div()
                                .id("add-image")
                                .flex_none()
                                .flex()
                                .items_center()
                                .justify_center()
                                .size(px(THUMB_HEIGHT))
                                .rounded(px(THUMB_RADIUS))
                                .shadow(vec![ring(ink(theme, ADD_RING))])
                                .text_color(ink(theme, T2))
                                .cursor_pointer()
                                .on_click(Self::tell(ADD_IMAGE_TELL, cx))
                                .child("+"),
                        ),
                    ),
            )
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(2.0))
                    .child(caption("Effects, applied in order", theme).pb_1p5())
                    .children(effects),
            )
            .child(separator(theme).flex_none())
            .child(
                switch_row(
                    "motion",
                    "Slow motion",
                    "effects drift; off when the system asks for less motion",
                    self.motion,
                    theme,
                )
                .py_1()
                .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                    this.motion = !this.motion;
                    cx.notify();
                })),
            )
            .child(
                switch_row(
                    "resume",
                    "Resume button",
                    "under the composer, with the project's sessions",
                    self.resume,
                    theme,
                )
                .py_1()
                .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                    this.resume = !this.resume;
                    cx.notify();
                })),
            )
    }

    fn preview(&self, theme: &Theme) -> Div {
        let window = tint(theme.color(ColorToken::SurfaceWindow), 1.0);
        let name = IMAGES.get(self.image).copied().unwrap_or(IMAGES[0]);
        let layers = Layers {
            halftone: self.on(Effect::Halftone),
            grain: self.on(Effect::Grain),
            scan: self.on(Effect::Scan),
            dither: self.on(Effect::Dither),
            window,
        };
        let band = |from: f32, to: f32, ink: f32| {
            div().absolute().inset_0().bg(linear_gradient(
                180.0,
                linear_color_stop(tint(window, 0.0), from),
                linear_color_stop(tint(window, ink), to),
            ))
        };
        let composer = div()
            .absolute()
            .left(relative(COMPOSER_LEFT))
            .top(relative(COMPOSER_TOP))
            .w(relative(COMPOSER_WIDTH))
            .flex()
            .flex_col()
            .gap_2()
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(14.0))
                    .py(px(11.0))
                    .px(px(13.0))
                    .rounded(px(COMPOSER_RADIUS))
                    .bg(rgba(COMPOSER_FILL))
                    .shadow(vec![ring(ink(theme, COMPOSER_RING))])
                    .child(
                        div()
                            .text_size(px(COMPOSER_HINT))
                            .text_color(ink(theme, T3))
                            .child(format!("Do anything in {PROJECT}")),
                    )
                    .child(
                        div()
                            .flex()
                            .justify_end()
                            .items_center()
                            .gap_2()
                            .text_size(px(COMPOSER_META))
                            .child(MODEL)
                            .child(div().text_color(ink(theme, T3)).child(EFFORT))
                            .child(div().size(px(SEND)).rounded_full().bg(ink(theme, SEND_INK))),
                    ),
            )
            .when(self.resume, |composer| {
                composer.child(
                    div()
                        .flex()
                        .justify_end()
                        .text_size(px(COMPOSER_META))
                        .text_color(ink(theme, RESUME_INK))
                        .child(format!("Resume \u{b7} {LATEST_SESSION}")),
                )
            });
        div()
            .flex_1()
            .relative()
            .rounded(px(PREVIEW_RADIUS))
            .overflow_hidden()
            .bg(window)
            .child(
                picture(name)
                    .absolute()
                    .size_full()
                    .rounded(px(PREVIEW_RADIUS))
                    .grayscale(self.on(Effect::Duotone)),
            )
            .when(self.on(Effect::Duotone), |preview| {
                preview.child(div().absolute().inset_0().bg(rgba(DUOTONE)))
            })
            .child(
                canvas(
                    |_, _, _| (),
                    move |bounds, (), window, _| layers.paint(bounds, window),
                )
                .absolute()
                .size_full(),
            )
            .when(self.on(Effect::Glow), |preview| {
                preview.child(div().absolute().inset_0().bg(linear_gradient(
                    0.0,
                    linear_color_stop(rgba(GLOW), 0.0),
                    linear_color_stop(tint(rgba(GLOW), 0.0), GLOW_REACH),
                )))
            })
            .when(self.on(Effect::Fade), |preview| {
                preview.child(band(FADE.0, FADE.1, FADE_INK))
            })
            .when(self.on(Effect::Dither), |preview| {
                preview.child(band(DITHER_SOLID.0, DITHER_SOLID.1, 1.0))
            })
            .child(composer)
    }
}

fn picture(name: &str) -> gpui::Img {
    img(PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join(IMAGE_DIR)
        .join(name))
    .object_fit(ObjectFit::Cover)
}

fn switch_row(
    id: impl Into<gpui::ElementId>,
    name: &'static str,
    sub: &'static str,
    on: bool,
    theme: &Theme,
) -> gpui::Stateful<Div> {
    div()
        .id(id)
        .flex()
        .items_center()
        .gap_2p5()
        .px_1()
        .cursor_pointer()
        .child(
            div()
                .flex_1()
                .flex()
                .flex_col()
                .line_height(px(ROW_LINE))
                .child(name)
                .child(
                    div()
                        .text_size(px(SMALL))
                        .text_color(ink(theme, T3))
                        .child(sub),
                ),
        )
        .child(switch("switch", "", on, theme))
}

fn titled(title: &'static str, note: SharedString, theme: &Theme) -> Div {
    shell(
        Header::Title(None, title.into(), Some(note.into_any_element())),
        theme,
    )
}

#[derive(Clone, Copy)]
struct Layers {
    halftone: bool,
    grain: bool,
    scan: bool,
    dither: bool,
    window: Rgba,
}

fn ramp(share: f32, (from, to): (f32, f32)) -> f32 {
    ((share - from) / (to - from)).clamp(0.0, 1.0)
}

fn dot(window: &mut Window, center: gpui::Point<Pixels>, radius: f32, color: Rgba) {
    window.paint_quad(
        fill(
            Bounds::new(
                center - point(px(radius), px(radius)),
                size(px(radius * 2.0), px(radius * 2.0)),
            ),
            color,
        )
        .corner_radii(px(radius)),
    );
}

impl Layers {
    fn paint(self, bounds: Bounds<Pixels>, window: &mut Window) {
        let (width, height) = (f32::from(bounds.size.width), f32::from(bounds.size.height));
        if height <= 0.0 {
            return;
        }
        let grid = |step: f32| {
            let columns = (0..)
                .map(move |at: u32| at as f32 * step)
                .take_while(move |x| *x < width);
            (0..)
                .map(move |at: u32| at as f32 * step)
                .take_while(move |y| *y < height)
                .flat_map(move |y| columns.clone().map(move |x| (x, y)))
        };
        if self.halftone {
            let ink = rgba(HALFTONE_INK);
            for (x, y) in grid(HALFTONE_STEP) {
                let reach = ramp(y / height, HALFTONE_FROM);
                if reach > 0.0 {
                    let center = bounds.origin
                        + point(px(x + HALFTONE_STEP / 2.0), px(y + HALFTONE_STEP / 2.0));
                    dot(window, center, HALFTONE_DOT, tint(ink, ink.alpha * reach));
                }
            }
        }
        if self.grain {
            for (x, y) in grid(GRAIN_STEP) {
                let light = (x as u32 + y as u32 * 7).is_multiple_of(3);
                let shade = if light { 1.0 } else { 0.0 };
                let color = Rgba::new(shade, shade, shade, GRAIN_INK);
                dot(
                    window,
                    bounds.origin + point(px(x + 1.0), px(y + 1.0)),
                    0.5,
                    color,
                );
            }
        }
        if self.scan {
            let mut y = height - 1.0;
            while y >= 0.0 {
                window.paint_quad(fill(
                    Bounds::new(
                        bounds.origin + point(px(0.0), px(y)),
                        size(px(width), px(1.0)),
                    ),
                    Rgba::new(1.0, 1.0, 1.0, SCAN_INK),
                ));
                y -= SCAN_STEP;
            }
        }
        if self.dither {
            for (radius, from, to) in DITHER_LAYERS {
                for (x, y) in grid(DITHER_STEP) {
                    let share = y / height;
                    let reach = ramp(share, (from, to));
                    if reach > 0.0 && share < DITHER_SOLID.1 {
                        let center = bounds.origin
                            + point(px(x + DITHER_STEP / 2.0), px(y + DITHER_STEP / 2.0));
                        dot(window, center, radius, tint(self.window, reach));
                    }
                }
            }
        }
    }
}

impl Render for Intro {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let name = IMAGES.get(self.image).copied().unwrap_or(IMAGES[0]);
        let title = div()
            .flex()
            .flex_none()
            .items_center()
            .gap_2p5()
            .px_1()
            .child(
                div()
                    .text_size(px(TITLE))
                    .line_height(relative(1.0))
                    .font_weight(FontWeight::SEMIBOLD)
                    .child("Intro screen"),
            )
            .child(
                div()
                    .text_size(px(DESC))
                    .text_color(ink(&theme, T3))
                    .child("the first screen: your image, the effects over it, and what it shows"),
            )
            .child(div().flex_1())
            .child(
                button(
                    "open-intro",
                    "Open the intro",
                    None,
                    ButtonKind::Plain,
                    &theme,
                )
                .on_click(Self::tell(OPEN_INTRO_TELL, cx)),
            );
        let body = div()
            .flex_1()
            .min_h_0()
            .flex()
            .flex_col()
            .gap_2()
            .mb_2()
            .pt_1()
            .px_1()
            .child(title)
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .gap_2()
                    .child(
                        titled("Background", "this device".into(), &theme)
                            .w(px(LEFT_WIDTH))
                            .flex_none()
                            .child(self.background(&theme, cx)),
                    )
                    .child(
                        titled("Preview", format!("{name} \u{b7} live").into(), &theme)
                            .flex_1()
                            .child(inner_card(&theme).p_3().child(self.preview(&theme))),
                    ),
            );
        root(
            &theme,
            body,
            self.told.map(Into::into),
            cx.listener(|this, _: &ClickEvent, _, cx| {
                this.told = None;
                cx.notify();
            }),
        )
    }
}
