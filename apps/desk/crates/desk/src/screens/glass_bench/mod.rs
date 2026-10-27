use std::sync::Arc;

use desk_ui::glass::bench::GlassBench;
use gpui::{AnyView, App, AppContext, Image, ImageFormat, Window};

use super::frame::load_fonts;

const STREET: &[u8] = include_bytes!("../../../../../assets/intro/street.jpg");

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the glass bench draws glass over the intro image, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    let image = Arc::new(Image::from_bytes(ImageFormat::Jpeg, STREET.to_vec()));
    Ok(cx.new(|_| GlassBench::new(image)).into())
}
