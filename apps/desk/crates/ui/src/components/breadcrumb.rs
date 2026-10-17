use std::f32::consts::FRAC_PI_2;

use gpui::{
    App, ClickEvent, FontWeight, SharedString, Transformation, Window, div, prelude::*, px, radians,
};

use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink};
use crate::components::size::{CAPTION_TEXT, DIM_TEXT, FONT_BODY, HOVER, RADIUS_BADGE, T1};
use crate::live::ActiveTheme;
use crate::metrics::ICON_TINY;

const CRUMB: f32 = 24.0;
const CRUMB_PAD: f32 = 6.0;

pub type OnCrumb = Box<dyn Fn(&ClickEvent, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct Breadcrumb {
    crumbs: Vec<(SharedString, Option<OnCrumb>)>,
}

pub fn breadcrumb<L: Into<SharedString>>(
    crumbs: impl IntoIterator<Item = (L, Option<OnCrumb>)>,
) -> Breadcrumb {
    Breadcrumb {
        crumbs: crumbs
            .into_iter()
            .map(|(label, on_click)| (label.into(), on_click))
            .collect(),
    }
}

impl RenderOnce for Breadcrumb {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let hover = ink(&theme, HOVER);
        let last = self.crumbs.len().saturating_sub(1);
        let parts = self
            .crumbs
            .into_iter()
            .enumerate()
            .map(|(at, (label, on_click))| {
                let crumb = div()
                    .id(("crumb", at))
                    .flex()
                    .items_center()
                    .min_w_0()
                    .h(px(CRUMB))
                    .px(px(CRUMB_PAD))
                    .rounded(px(RADIUS_BADGE))
                    .text_color(ink(&theme, if at == last { T1 } else { DIM_TEXT }))
                    .when(at == last, |crumb| crumb.font_weight(FontWeight::MEDIUM))
                    .when_some(on_click, |crumb, on_click| {
                        crumb
                            .cursor_pointer()
                            .hover(move |style| style.bg(hover))
                            .on_click(on_click)
                    })
                    .child(div().min_w_0().truncate().child(label));
                div()
                    .flex()
                    .items_center()
                    .min_w_0()
                    .when(at > 0, |part| {
                        part.child(
                            glyph(Glyph::Chevron, ICON_TINY, ink(&theme, CAPTION_TEXT))
                                .with_transformation(Transformation::rotate(radians(-FRAC_PI_2))),
                        )
                    })
                    .child(crumb)
            });
        div()
            .flex()
            .items_center()
            .min_w_0()
            .text_size(px(FONT_BODY))
            .children(parts)
    }
}
