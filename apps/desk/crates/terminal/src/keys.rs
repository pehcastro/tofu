use alacritty_terminal::grid::Scroll;
use alacritty_terminal::term::TermMode;
use gpui::Keystroke;

const CONTROL_MASK: u8 = 0x1f;
const ESCAPE: u8 = 0x1b;

pub(crate) enum KeyInput {
    Send(Vec<u8>),
    Scroll(Scroll),
    Paste,
    Copy,
}

pub(crate) fn key_input(keystroke: &Keystroke, mode: TermMode, selected: bool) -> Option<KeyInput> {
    let held = keystroke.modifiers;
    let key = keystroke.key.to_ascii_lowercase();
    let only_shift = held.shift && !held.control && !held.alt;
    match (key.as_str(), held.control, held.alt, held.shift) {
        ("c", true, false, true) => return Some(KeyInput::Copy),
        ("c", true, false, false) if selected => return Some(KeyInput::Copy),
        ("v", true, false, _) => return Some(KeyInput::Paste),
        ("insert", false, false, true) => return Some(KeyInput::Paste),
        _ => {}
    }
    if only_shift && !mode.contains(TermMode::ALT_SCREEN) {
        let scroll = match key.as_str() {
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
    sequence(keystroke, &key, mode).map(KeyInput::Send)
}

fn sequence(keystroke: &Keystroke, key: &str, mode: TermMode) -> Option<Vec<u8>> {
    let held = keystroke.modifiers;
    let code = 1 + u8::from(held.shift) + 2 * u8::from(held.alt) + 4 * u8::from(held.control);
    let modified = code > 1;
    let app_cursor = mode.contains(TermMode::APP_CURSOR);
    let cursor = |letter: char| match (modified, app_cursor) {
        (true, _) => format!("\x1b[1;{code}{letter}"),
        (false, true) => format!("\x1bO{letter}"),
        (false, false) => format!("\x1b[{letter}"),
    };
    let tilde = |number: u8| match modified {
        true => format!("\x1b[{number};{code}~"),
        false => format!("\x1b[{number}~"),
    };
    let function = |letter: char| match modified {
        true => format!("\x1b[1;{code}{letter}"),
        false => format!("\x1bO{letter}"),
    };
    let text = match key {
        "up" => cursor('A'),
        "down" => cursor('B'),
        "right" => cursor('C'),
        "left" => cursor('D'),
        "home" => cursor('H'),
        "end" => cursor('F'),
        "insert" => tilde(2),
        "delete" => tilde(3),
        "pageup" => tilde(5),
        "pagedown" => tilde(6),
        "f1" => function('P'),
        "f2" => function('Q'),
        "f3" => function('R'),
        "f4" => function('S'),
        "f5" => tilde(15),
        "f6" => tilde(17),
        "f7" => tilde(18),
        "f8" => tilde(19),
        "f9" => tilde(20),
        "f10" => tilde(21),
        "f11" => tilde(23),
        "f12" => tilde(24),
        "enter" if held.shift => "\n".to_owned(),
        "enter" if held.alt => "\x1b\r".to_owned(),
        "enter" => "\r".to_owned(),
        "backspace" if held.control => "\x08".to_owned(),
        "backspace" if held.alt => "\x1b\x7f".to_owned(),
        "backspace" => "\x7f".to_owned(),
        "tab" if held.control => return None,
        "tab" if held.shift => "\x1b[Z".to_owned(),
        "tab" => "\t".to_owned(),
        "escape" => "\x1b".to_owned(),
        "space" if held.control && !held.alt => "\0".to_owned(),
        _ => return control_or_meta(keystroke, key),
    };
    Some(text.into_bytes())
}

fn control_or_meta(keystroke: &Keystroke, key: &str) -> Option<Vec<u8>> {
    let held = keystroke.modifiers;
    match (held.control, held.alt, key.as_bytes()) {
        (true, false, [byte]) => {
            let code = match byte {
                b'a'..=b'z' => byte & CONTROL_MASK,
                b'@' => 0,
                b'[' => ESCAPE,
                b'\\' => 0x1c,
                b']' => 0x1d,
                b'^' => 0x1e,
                b'_' | b'/' => CONTROL_MASK,
                b'?' => 0x7f,
                _ => return None,
            };
            Some(vec![code])
        }
        (false, true, _) => {
            let typed = keystroke.key_char.as_deref()?;
            Some([&[ESCAPE], typed.as_bytes()].concat())
        }
        _ => None,
    }
}
