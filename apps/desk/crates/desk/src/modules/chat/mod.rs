mod boards;
mod cassette;
mod chrome;
mod composer;
mod fixture;
mod paint;
mod rows;

use std::borrow::Cow;
use std::sync::Arc;

use gpui::{AnyView, App, AppContext, Context, Image, ImageFormat, IntoElement, Render, Window};

const BACKDROP: &[u8] = include_bytes!("assets/backdrop.jpg");
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

#[derive(Clone, Copy)]
enum Board {
    Split,
    Zoomed,
    Edits,
    FourTurns,
    Running,
}

struct Chat {
    board: Board,
    backdrop: Arc<Image>,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the chat cannot load the Geist fonts: {error}"))?;
    let backdrop = Arc::new(Image::from_bytes(ImageFormat::Jpeg, BACKDROP.to_vec()));
    let board = match board {
        None | Some("ICHAT-1") => Board::Split,
        Some("ICHAT-2") => Board::Zoomed,
        Some("ICHAT-3") => Board::Edits,
        Some("ICHAT-4") => Board::FourTurns,
        Some("ICHAT-5") => Board::Running,
        Some("36-agents") => return cassette::open(backdrop, cx),
        Some(other) => {
            return Err(format!(
                "the chat draws ICHAT-1 to ICHAT-5 and 36-agents, not {other}"
            ));
        }
    };
    Ok(cx.new(|_| Chat { board, backdrop }).into())
}

impl Render for Chat {
    fn render(&mut self, window: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let main = match self.board {
            Board::Split => boards::split_board(scale),
            Board::Zoomed => boards::zoomed_board(scale),
            Board::Edits => boards::edits_board(scale),
            Board::FourTurns => boards::four_turns_board(scale),
            Board::Running => boards::running_board(scale),
        };
        chrome::window(&self.backdrop, scale, main)
    }
}
