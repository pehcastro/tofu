use std::sync::Arc;

use gpui::{Div, Image, ObjectFit, StyledImage, div, img, prelude::*, px};

use super::paint::{SANS, SURFACE, hex, white};

pub fn hero(image: &Arc<Image>) -> Div {
    div()
        .flex_1()
        .min_h_0()
        .w_full()
        .relative()
        .overflow_hidden()
        .bg(hex(SURFACE))
        .font_family(SANS)
        .text_size(px(14.0))
        .line_height(px(23.0))
        .text_color(white(0.9))
        .child(
            img(image.clone())
                .absolute()
                .inset_0()
                .size_full()
                .object_fit(ObjectFit::Cover),
        )
}
