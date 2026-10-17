use gpui::{App, FontWeight, Hsla, SharedString, Window, div, prelude::*, px};

use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink, ring};
use crate::components::size::{CAPTION_TEXT, T2};
use crate::live::ActiveTheme;
use crate::metrics::{ICON_TINY, TEXT_TINY};

const DOT_CHIP: f32 = 20.0;
const DOT_CHIP_PAD: f32 = 7.0;
const DOT_CHIP_MAX: f32 = 180.0;
const DOT_CHIP_RING: f32 = 0.12;
const DOT: f32 = 6.0;

enum Mark {
    Dot(Hsla),
    Icon(Glyph),
}

#[derive(IntoElement)]
pub struct DotChip {
    mark: Mark,
    label: SharedString,
}

pub fn dot_chip(label: impl Into<SharedString>, dot: Hsla) -> DotChip {
    DotChip {
        mark: Mark::Dot(dot),
        label: label.into(),
    }
}

pub fn icon_chip(icon: Glyph, label: impl Into<SharedString>) -> DotChip {
    DotChip {
        mark: Mark::Icon(icon),
        label: label.into(),
    }
}

impl RenderOnce for DotChip {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let mark = match self.mark {
            Mark::Dot(color) => div()
                .flex_none()
                .size(px(DOT))
                .rounded_full()
                .bg(color)
                .into_any_element(),
            Mark::Icon(icon) => {
                glyph(icon, ICON_TINY, ink(&theme, CAPTION_TEXT)).into_any_element()
            }
        };
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap_1p5()
            .max_w(px(DOT_CHIP_MAX))
            .h(px(DOT_CHIP))
            .px(px(DOT_CHIP_PAD))
            .rounded_full()
            .shadow(vec![ring(ink(&theme, DOT_CHIP_RING))])
            .text_size(px(TEXT_TINY))
            .font_weight(FontWeight::MEDIUM)
            .text_color(ink(&theme, T2))
            .child(mark)
            .child(div().min_w_0().truncate().child(self.label))
    }
}
