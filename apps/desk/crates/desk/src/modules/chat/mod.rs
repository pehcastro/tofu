mod boards;
mod cassette;
mod chrome;
mod composer;
mod fixture;
mod paint;
mod rows;

use std::borrow::Cow;
use std::sync::Arc;

use gpui::{
    AnyView, App, AppContext, Context, Div, Entity, FocusHandle, Image, ImageFormat,
    InteractiveElement, IntoElement, KeyDownEvent, MouseButton, MouseDownEvent, Render, Window,
};

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

#[derive(Clone, Copy, PartialEq)]
enum Overlay {
    Closed,
    Picker,
    Add,
    Trace,
    Slash,
    Form,
    Kind,
    Tools,
}

struct Chat {
    board: Board,
    overlay: Overlay,
    backdrop: Arc<Image>,
    focus: FocusHandle,
}

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the chat cannot load the Geist fonts: {error}"))?;
    let backdrop = Arc::new(Image::from_bytes(ImageFormat::Jpeg, BACKDROP.to_vec()));
    let (board, overlay) = match board {
        None | Some("ICHAT-1") => (Board::Split, Overlay::Closed),
        Some("ICHAT-2") => (Board::Zoomed, Overlay::Closed),
        Some("ICHAT-3") => (Board::Edits, Overlay::Closed),
        Some("ICHAT-4") => (Board::FourTurns, Overlay::Closed),
        Some("ICHAT-5") => (Board::Running, Overlay::Closed),
        Some("S-CHAT-1") => (Board::Split, Overlay::Picker),
        Some("S-CHAT-2") => (Board::Split, Overlay::Trace),
        Some("S-CHAT-3") => (Board::Zoomed, Overlay::Slash),
        Some("S-CHAT-4") => (Board::Zoomed, Overlay::Form),
        Some("S-CHAT-5") => (Board::Edits, Overlay::Kind),
        Some("S-CHAT-6") => (Board::Running, Overlay::Tools),
        Some("36-agents") => return cassette::open(backdrop, cx),
        Some(other) => {
            return Err(format!(
                "the chat draws ICHAT-1 to ICHAT-5, S-CHAT-1 to S-CHAT-6 and 36-agents, not {other}"
            ));
        }
    };
    Ok(cx
        .new(|cx| {
            let focus = cx.focus_handle();
            focus.focus(window, cx);
            Chat {
                board,
                overlay,
                backdrop,
                focus,
            }
        })
        .into())
}

pub struct Wire {
    chat: Entity<Chat>,
    overlay: Overlay,
}

impl Wire {
    fn on(&self, control: Div, next: Overlay) -> Div {
        let chat = self.chat.clone();
        control.on_mouse_down(MouseButton::Left, move |_, _, cx| {
            cx.stop_propagation();
            chat.update(cx, |chat, cx| {
                chat.overlay = if chat.overlay == next {
                    Overlay::Closed
                } else {
                    next
                };
                cx.notify();
            });
        })
    }
}

impl Render for Chat {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let wire = Wire {
            chat: cx.entity(),
            overlay: self.overlay,
        };
        let main = match self.board {
            Board::Split => boards::split_board(scale, &wire),
            Board::Zoomed => boards::zoomed_board(scale, &wire),
            Board::Edits => boards::edits_board(scale, &wire),
            Board::FourTurns => boards::four_turns_board(scale),
            Board::Running => boards::running_board(scale, &wire),
        };
        chrome::window(&self.backdrop, scale, main)
            .track_focus(&self.focus)
            .on_key_down(cx.listener(|chat, event: &KeyDownEvent, _, cx| {
                if event.keystroke.key == "escape" {
                    chat.close(cx);
                }
            }))
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|chat, _: &MouseDownEvent, _, cx| chat.close(cx)),
            )
    }
}

impl Chat {
    fn close(&mut self, cx: &mut Context<Self>) {
        self.overlay = Overlay::Closed;
        cx.notify();
    }
}
