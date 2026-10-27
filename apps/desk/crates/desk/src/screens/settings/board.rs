use std::borrow::Cow;

use desk_ui::components::card::inner_card;
use desk_ui::components::paint::tint;
use desk_ui::theme::Theme;
use gpui::{App, Div, linear_color_stop, linear_gradient, prelude::*, rgb};

const INNER_TINT: u32 = 0xe6e0ff;
const INNER_FILL: (f32, f32) = (0.07, 0.05);
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

pub fn load_fonts(cx: &App) -> Result<(), String> {
    let text = cx.text_system();
    if text.all_font_names().iter().any(|name| name == "Geist") {
        return Ok(());
    }
    text.add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the screen cannot load Geist: {error}"))
}

pub fn inner(theme: &Theme) -> Div {
    let tinted = rgb(INNER_TINT);
    inner_card(theme).bg(linear_gradient(
        180.0,
        linear_color_stop(tint(tinted, INNER_FILL.0), 0.0),
        linear_color_stop(tint(tinted, INNER_FILL.1), 1.0),
    ))
}
