use std::sync::Arc;

use gpui::{Div, Image, div, img, prelude::*, px};

use super::paint::{SURFACE, hex};

const HERO_RADIUS: f32 = 10.0;

pub fn hero(image: &Arc<Image>) -> Div {
    div()
        .absolute()
        .inset_0()
        .overflow_hidden()
        .rounded(px(HERO_RADIUS))
        .bg(hex(SURFACE))
        .child(img(image.clone()).absolute().size_full())
}
