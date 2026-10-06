use alacritty_terminal::grid::Scroll;
use gpui::Keystroke;

const CONTROL_MASK: u8 = 0x1f;

pub(crate) enum KeyInput {
    Send(Vec<u8>),
    Scroll(Scroll),
}

pub(crate) fn key_input(keystroke: &Keystroke, app_cursor: bool) -> Option<KeyInput> {
    let modifiers = keystroke.modifiers;
    let key = keystroke.key.as_str();
    if modifiers.shift {
        let scroll = match key {
            "pageup" => Some(Scroll::PageUp),
            "pagedown" => Some(Scroll::PageDown),
            "home" => Some(Scroll::Top),
            "end" => Some(Scroll::Bottom),
            _ => None,
        };
        if let Some(scroll) = scroll {
            return Some(KeyInput::Scroll(scroll));
        }
    }
    let arrow = |code: char| {
        let lead = if app_cursor { 'O' } else { '[' };
        format!("\x1b{lead}{code}").into_bytes()
    };
    let bytes = match key {
        "enter" => b"\r".to_vec(),
        "backspace" => b"\x7f".to_vec(),
        "tab" if modifiers.shift => b"\x1b[Z".to_vec(),
        "tab" => b"\t".to_vec(),
        "escape" => b"\x1b".to_vec(),
        "up" => arrow('A'),
        "down" => arrow('B'),
        "right" => arrow('C'),
        "left" => arrow('D'),
        "home" => arrow('H'),
        "end" => arrow('F'),
        "insert" => b"\x1b[2~".to_vec(),
        "delete" => b"\x1b[3~".to_vec(),
        "pageup" => b"\x1b[5~".to_vec(),
        "pagedown" => b"\x1b[6~".to_vec(),
        _ => match (modifiers.control, key.as_bytes()) {
            (true, [letter]) if letter.is_ascii_alphabetic() => {
                vec![letter.to_ascii_lowercase() & CONTROL_MASK]
            }
            _ => keystroke.key_char.clone()?.into_bytes(),
        },
    };
    let meta = modifiers.alt && !modifiers.control;
    Some(KeyInput::Send(if meta {
        [b"\x1b".as_slice(), &bytes].concat()
    } else {
        bytes
    }))
}
