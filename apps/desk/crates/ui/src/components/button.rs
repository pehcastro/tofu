use gpui::{Div, ElementId, FontWeight, SharedString, Stateful, div, prelude::*, px};

use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, pressed};
use crate::components::size::{BUTTON_GAP, FONT_BODY, RADIUS_ROW};
use crate::metrics::{CONTROL, ICON};
use crate::theme::{ColorToken, Theme};

const TEXT_HEIGHT: f32 = 26.0;
const TEXT_FONT: f32 = 12.5;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ButtonKind {
    Plain,
    Primary,
    Opener,
    Text,
}

pub fn button(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    lead: Option<Glyph>,
    kind: ButtonKind,
    theme: &Theme,
) -> Stateful<Div> {
    let label = label.into();
    let (fill, text) = match kind {
        ButtonKind::Plain | ButtonKind::Opener | ButtonKind::Text => {
            (ColorToken::ButtonFill, ColorToken::TextBase)
        }
        ButtonKind::Primary => (ColorToken::ButtonPrimary, ColorToken::ButtonPrimaryText),
    };
    let (height, font) = match kind {
        ButtonKind::Text => (TEXT_HEIGHT, TEXT_FONT),
        ButtonKind::Plain | ButtonKind::Primary | ButtonKind::Opener => (CONTROL, FONT_BODY),
    };
    let hover = theme.color(ColorToken::StateActive);
    let face = div()
        .id(id)
        .aria_label(label.clone())
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .gap(px(BUTTON_GAP))
        .h(px(height))
        .px_3()
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .bg(theme.color(fill))
        .text_size(px(font))
        .font_weight(FontWeight::MEDIUM)
        .text_color(theme.color(text))
        .when(kind != ButtonKind::Primary, |button| {
            button.hover(move |style| style.bg(hover))
        })
        .children(lead.map(|lead| glyph(lead, ICON, theme.color(text))))
        .child(label);
    match kind {
        ButtonKind::Plain | ButtonKind::Primary | ButtonKind::Text => {
            pressed(face, theme.color(ColorToken::CardsInnerFill))
        }
        ButtonKind::Opener => face,
    }
}
