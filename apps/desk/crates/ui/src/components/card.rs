use gpui::{
    AnyElement, Bounds, Div, ElementId, FontWeight, Pixels, Rgba, SharedString, Stateful, canvas,
    div, fill, linear_color_stop, linear_gradient, point, prelude::*, px, relative, size,
};

use crate::component::{control, icon};
use crate::components::glyph::Glyph;
use crate::components::paint::{drop, glyph, ink, ring, tint, top_light};
use crate::components::size::{
    CAP2_TRACKING, CAPTION_TEXT, DOT, DOT_SPACING, DOTS_SHARE, FONT_CAP2, FONT_SMALL, HEADER,
    HEADER_PAD_LEFT, HEADER_PAD_LEFT_TABBED, HEADER_PAD_RIGHT, INNER_SHADOW_BLUR,
    RADIUS_CHIP_SMALL, SHELL_TEXT, TAB_IN_TILE,
};
use crate::icon::Icon;
use crate::metrics::ICON_SMALL;
use crate::theme::{BorderToken, ColorToken, FlagToken, NumberToken, Theme};

pub fn outer_card(theme: &Theme) -> Div {
    let gap = px(theme.number(NumberToken::CardsOuterGap));
    div()
        .flex()
        .min_w_0()
        .min_h_0()
        .bg(theme.color(ColorToken::CardsOuterFill))
        .rounded(px(theme.number(NumberToken::CardsOuterRadius)))
        .px(gap)
        .pb(gap)
        .shadow(
            theme
                .border(BorderToken::CardsOuter)
                .map(ring)
                .into_iter()
                .collect(),
        )
}

pub fn inner_card(theme: &Theme) -> Div {
    let mut shadows = Vec::new();
    if theme.flag(FlagToken::CardsInnerHighlight) {
        shadows.push(top_light(theme.color(ColorToken::CardsInnerSheen)));
        shadows.push(drop(
            theme.color(ColorToken::CardsInnerShadow),
            1.0,
            INNER_SHADOW_BLUR,
        ));
    }
    shadows.extend(theme.border(BorderToken::CardsInner).map(ring));
    div()
        .relative()
        .flex_1()
        .min_w_0()
        .min_h_0()
        .flex()
        .flex_col()
        .overflow_hidden()
        .rounded(px(theme.number(NumberToken::CardsInnerRadius)))
        .bg(linear_gradient(
            180.0,
            linear_color_stop(theme.color(ColorToken::CardsInnerFill), 0.0),
            linear_color_stop(theme.color(ColorToken::CardsInnerFillEnd), 1.0),
        ))
        .shadow(shadows)
}

pub enum Header {
    Title(Option<Glyph>, SharedString, Option<AnyElement>),
    Tabs(AnyElement, Option<AnyElement>),
}

pub fn shell(header: Header, theme: &Theme) -> Div {
    let top = div()
        .flex()
        .flex_none()
        .gap_1p5()
        .pr(px(HEADER_PAD_RIGHT))
        .text_size(px(FONT_SMALL))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, SHELL_TEXT));
    let top = match header {
        Header::Title(icon, title, trailing) => top
            .h(px(HEADER))
            .items_center()
            .pl(px(HEADER_PAD_LEFT))
            .children(icon.map(|icon| glyph(icon, ICON_SMALL, ink(theme, SHELL_TEXT))))
            .child(div().flex_1().min_w_0().truncate().child(title))
            .children(trailing),
        Header::Tabs(tabs, trailing) => top
            .h(px(HEADER))
            .items_end()
            .pl(px(HEADER_PAD_LEFT_TABBED))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .h(px(TAB_IN_TILE))
                    .flex()
                    .flex_col()
                    .child(tabs),
            )
            .children(trailing.map(|trailing| div().self_center().child(trailing))),
    };
    outer_card(theme).flex_col().child(top)
}

const HEADER_ACTION_INSET: f32 = 4.0;

pub fn header_action(
    id: impl Into<ElementId>,
    glyph: Icon,
    label: &'static str,
    theme: &Theme,
) -> Stateful<Div> {
    control(id, label, theme)
        .size(px(HEADER - 2.0 * HEADER_ACTION_INSET))
        .rounded(px(RADIUS_CHIP_SMALL))
        .child(icon(glyph, ICON_SMALL, theme.color(ColorToken::TextIcon)))
}

pub fn caption(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .text_size(px(FONT_CAP2))
        .font_weight(FontWeight::SEMIBOLD)
        .line_height(relative(1.0))
        .letter_spacing(px(CAP2_TRACKING))
        .text_color(ink(theme, CAPTION_TEXT))
        .child(text.into().to_uppercase())
}

pub fn dots(theme: &Theme) -> impl IntoElement {
    let color = theme.color(ColorToken::CardsDots);
    canvas(
        |_, _, _| (),
        move |bounds: Bounds<Pixels>, (), window, _| paint_dots(bounds, color, window),
    )
    .absolute()
    .top_0()
    .right_0()
    .bottom_0()
    .w(relative(DOTS_SHARE))
}

fn paint_dots(bounds: Bounds<Pixels>, color: Rgba, window: &mut gpui::Window) {
    let (width, height) = (f32::from(bounds.size.width), f32::from(bounds.size.height));
    let mut x = 0.0;
    while x < width {
        let dot = tint(color, color.alpha * x / width);
        let mut y = 0.0;
        while y < height {
            let origin =
                bounds.origin + point(px(x + DOT_SPACING / 2.0), px(y + DOT_SPACING / 2.0));
            window.paint_quad(
                fill(Bounds::new(origin, size(px(DOT * 2.0), px(DOT * 2.0))), dot)
                    .corner_radii(px(DOT)),
            );
            y += DOT_SPACING;
        }
        x += DOT_SPACING;
    }
}
