use alacritty_terminal::index::Side;
use alacritty_terminal::term::TermMode;
use gpui::{Modifiers, MouseButton, Pixels, Point, Size};

const ESCAPE: u8 = 0x1b;
const OFFSET: usize = 33;
const NORMAL_LIMIT: usize = 223;
const UTF8_LIMIT: usize = 2015;
const UTF8_SPLIT: usize = 128;
const RELEASE: u8 = 3;
const MOVE: u8 = 32;
const NO_BUTTON_MOVE: u8 = 35;
const SCROLL_UP: u8 = 64;
const SCROLL_DOWN: u8 = 65;

#[derive(Clone, Copy, PartialEq, Eq)]
pub(crate) struct Cell {
    pub line: usize,
    pub column: usize,
    pub side: Side,
}

pub(crate) fn cell_at(position: Point<Pixels>, cell: Size<Pixels>, grid: (u16, u16)) -> Cell {
    let last_column = usize::from(grid.0.saturating_sub(1));
    let last_line = usize::from(grid.1.saturating_sub(1));
    let column = (position.x / cell.width).floor().max(0.) as usize;
    let line = (position.y / cell.height).floor().max(0.) as usize;
    let within = position.x - cell.width * column as f32;
    let side = if column > last_column || line > last_line || within > cell.width / 2. {
        Side::Right
    } else {
        Side::Left
    };
    Cell {
        line: line.min(last_line),
        column: column.min(last_column),
        side,
    }
}

pub(crate) fn reporting(mode: TermMode, shift: bool) -> bool {
    mode.intersects(TermMode::MOUSE_MODE) && !shift
}

fn button_code(button: MouseButton) -> Option<u8> {
    match button {
        MouseButton::Left => Some(0),
        MouseButton::Middle => Some(1),
        MouseButton::Right => Some(2),
        MouseButton::Navigate(_) => None,
    }
}

pub(crate) fn button_report(
    at: Cell,
    button: MouseButton,
    held: Modifiers,
    pressed: bool,
    mode: TermMode,
) -> Option<Vec<u8>> {
    report(at, button_code(button)?, pressed, held, mode)
}

pub(crate) fn move_report(
    at: Cell,
    button: Option<MouseButton>,
    held: Modifiers,
    mode: TermMode,
) -> Option<Vec<u8>> {
    let code = match button {
        Some(button) => MOVE + button_code(button)?,
        None if mode.contains(TermMode::MOUSE_MOTION) => NO_BUTTON_MOVE,
        None => return None,
    };
    mode.intersects(TermMode::MOUSE_MOTION | TermMode::MOUSE_DRAG)
        .then(|| report(at, code, true, held, mode))
        .flatten()
}

pub(crate) fn scroll_report(at: Cell, lines: i32, held: Modifiers, mode: TermMode) -> Vec<u8> {
    let code = if lines > 0 { SCROLL_UP } else { SCROLL_DOWN };
    let one = report(at, code, true, held, mode).unwrap_or_default();
    one.repeat(lines.unsigned_abs() as usize)
}

pub(crate) fn alternate_scroll(lines: i32) -> Vec<u8> {
    let letter = if lines > 0 { b'A' } else { b'B' };
    [ESCAPE, b'O', letter].repeat(lines.unsigned_abs() as usize)
}

fn report(at: Cell, code: u8, pressed: bool, held: Modifiers, mode: TermMode) -> Option<Vec<u8>> {
    let held = 4 * u8::from(held.shift) + 8 * u8::from(held.alt) + 16 * u8::from(held.control);
    if mode.contains(TermMode::SGR_MOUSE) {
        let end = if pressed { 'M' } else { 'm' };
        let code = code + held;
        return Some(format!("\x1b[<{code};{};{}{end}", at.column + 1, at.line + 1).into_bytes());
    }
    let utf8 = mode.contains(TermMode::UTF8_MOUSE);
    let limit = if utf8 { UTF8_LIMIT } else { NORMAL_LIMIT };
    if at.line >= limit || at.column >= limit {
        return None;
    }
    let code = if pressed { code } else { RELEASE } + held;
    let mut report = vec![ESCAPE, b'[', b'M', MOVE + code];
    for position in [at.column + OFFSET, at.line + OFFSET] {
        match (utf8, u8::try_from(position)) {
            (false, Ok(byte)) => report.push(byte),
            (true, Ok(byte)) if position < UTF8_SPLIT => report.push(byte),
            (true, _) => {
                let wide = char::from_u32(u32::try_from(position).ok()?)?;
                report.extend(wide.to_string().bytes());
            }
            (false, Err(_)) => return None,
        }
    }
    Some(report)
}
