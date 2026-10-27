use desk_ui::components::logo::{Brand, Paint, logo};
use desk_ui::theme::Theme;
use gpui::{Div, ParentElement, Styled, div, px};

use super::kit::{block, label};

const SIZES: [f32; 3] = [12.0, 16.0, 24.0];
const CELL_WIDTH: f32 = 148.0;
const SWATCH: f32 = 8.0;

pub fn logos_page(theme: &Theme) -> Div {
    let cells = Brand::ALL.map(|brand| {
        div()
            .w(px(CELL_WIDTH))
            .flex()
            .flex_col()
            .gap_1()
            .child(
                div()
                    .flex()
                    .items_end()
                    .gap_3()
                    .children(SIZES.map(|size| logo(brand, px(size), theme))),
            )
            .child(label(brand.slug(), theme))
            .child(paint(brand.paint(), theme))
    });
    block(
        "Logos",
        theme,
        div().flex().flex_wrap().gap_4().children(cells),
    )
}

fn paint(paint: Paint, theme: &Theme) -> Div {
    let row = div().flex().items_center().gap_1();
    match paint {
        Paint::Own => row.child(label("own", theme)),
        Paint::Tinted { brand: None } => row.child(label("tinted, ink", theme)),
        Paint::Tinted { brand: Some(color) } => row
            .child(label("tinted, brand", theme))
            .child(div().size(px(SWATCH)).rounded_full().bg(color)),
    }
}
