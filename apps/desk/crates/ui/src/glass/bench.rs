use std::sync::Arc;

use gpui::{
    ClickEvent, Context, Image, IntoElement, ObjectFit, ParentElement, Render, SharedString,
    Styled, StyledImage, Window, div, img, prelude::*, px,
};

use crate::components::card::{Header, header_control, inner_card, shell};
use crate::components::composer::picker;
use crate::components::glyph::Glyph;
use crate::components::paint::ink;
use crate::glass::{Glass, GlassMode};
use crate::live::ActiveTheme;
use crate::theme::ColorToken;

const REFERENCE: &str = "Frosted reference";
const PANEL_WIDTH: f32 = 240.0;
const PANEL_HEIGHT: f32 = 150.0;
const PANEL_RADIUS: f32 = 32.0;
const PANEL_GAP: f32 = 28.0;
const PANEL_TEXT: f32 = 0.85;
const REFRACTION: f32 = 40.0;
const EDGE: f32 = 32.0;
const DISPERSION: f32 = 2.0;
const CLEAR_FROST: f32 = 4.0;

pub struct GlassBench {
    image: Arc<Image>,
    mode: GlassMode,
    zeroed: bool,
}

impl GlassBench {
    pub fn new(image: Arc<Image>) -> Self {
        GlassBench {
            image,
            mode: GlassMode::Liquid,
            zeroed: false,
        }
    }
}

impl Render for GlassBench {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let frosted = Glass {
            mode: self.mode,
            ..Glass::frosted(&theme)
        };
        let lens = Glass {
            refraction: REFRACTION,
            edge: EDGE,
            ..frosted
        };
        let panels = match self.zeroed {
            true => [frosted; 3],
            false => [
                lens,
                Glass {
                    frost: CLEAR_FROST,
                    ..lens
                },
                Glass {
                    frost: CLEAR_FROST,
                    dispersion: DISPERSION,
                    ..lens
                },
            ],
        };
        let mode = picker("glass-mode", self.mode.label(), &theme).on_click(cx.listener(
            |bench, _: &ClickEvent, _, cx| {
                bench.mode = bench.mode.other();
                cx.notify();
            },
        ));
        let amount = match self.zeroed {
            true => "lens off",
            false => "lens on",
        };
        let zero = picker("glass-zero", amount, &theme).on_click(cx.listener(
            |bench, _: &ClickEvent, _, cx| {
                bench.zeroed = !bench.zeroed;
                cx.notify();
            },
        ));
        let controls = div()
            .flex()
            .gap_1()
            .child(header_control(mode))
            .child(header_control(zero));
        let fill = theme.color(ColorToken::CardsInnerFill);
        let text = ink(&theme, PANEL_TEXT);
        let panel = |glass: Glass, name: &'static str| {
            glass.apply(
                div()
                    .w(px(PANEL_WIDTH))
                    .h(px(PANEL_HEIGHT))
                    .rounded(px(PANEL_RADIUS))
                    .bg(fill)
                    .flex()
                    .items_center()
                    .justify_center()
                    .text_color(text)
                    .child(SharedString::from(name)),
            )
        };
        let panels = panels
            .into_iter()
            .zip(["Frost", "Refraction", "Dispersion"])
            .map(|(glass, name)| panel(glass, name))
            .chain([panel(Glass::frosted(&theme), REFERENCE)]);
        shell(
            Header::Title(
                Some(Glyph::Image),
                "Liquid glass".into(),
                Some(controls.into_any_element()),
            ),
            &theme,
        )
        .size_full()
        .child(
            inner_card(&theme).child(
                div()
                    .relative()
                    .size_full()
                    .child(
                        img(self.image.clone())
                            .absolute()
                            .size_full()
                            .object_fit(ObjectFit::Cover),
                    )
                    .child(
                        div()
                            .absolute()
                            .size_full()
                            .flex()
                            .items_center()
                            .justify_center()
                            .gap(px(PANEL_GAP))
                            .children(panels),
                    ),
            ),
        )
    }
}
