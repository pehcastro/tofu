use alacritty_terminal::term::cell::{Cell, Flags};
use alacritty_terminal::vte::ansi::{Color, NamedColor, Rgb};
use gpui::{ColorExt, Hsla, rgb, rgb_to_hsla};

const ANSI: [u32; 16] = [
    0x1d1f21, 0xcc6666, 0xb5bd68, 0xf0c674, 0x81a2be, 0xb294bb, 0x8abeb7, 0xc5c8c6, 0x666666,
    0xd54e53, 0xb9ca4a, 0xe7c547, 0x7aa6da, 0xc397d8, 0x70c0b1, 0xeaeaea,
];
const FOREGROUND: u32 = 0xd8dadb;
const BACKGROUND: u32 = 0x161719;
const CURSOR: u32 = 0xd8dadb;
const DIM_ALPHA: f32 = 0.66;
const CUBE_FIRST: u8 = 16;
const GREY_FIRST: u8 = 232;

fn hex(hex: u32) -> Hsla {
    rgb_to_hsla(rgb(hex))
}

pub(crate) fn background() -> Hsla {
    hex(BACKGROUND)
}

pub(crate) fn cursor() -> Hsla {
    hex(CURSOR)
}

pub(crate) fn cell_colors(cell: &Cell) -> (Hsla, Option<Hsla>) {
    let (front, back) = if cell.flags.contains(Flags::INVERSE) {
        (cell.bg, cell.fg)
    } else {
        (cell.fg, cell.bg)
    };
    let back = match back {
        Color::Named(NamedColor::Background) => None,
        other => Some(color(other)),
    };
    let front = match (
        cell.flags.contains(Flags::HIDDEN),
        cell.flags.contains(Flags::DIM),
    ) {
        (true, _) => back.unwrap_or_else(background),
        (false, true) => color(front).opacity(DIM_ALPHA),
        (false, false) => color(front),
    };
    (front, back)
}

fn color(color: Color) -> Hsla {
    match color {
        Color::Spec(Rgb { r, g, b }) => hex(u32::from_be_bytes([0, r, g, b])),
        Color::Indexed(index) => indexed(index),
        Color::Named(named) => named_color(named),
    }
}

fn named_color(named: NamedColor) -> Hsla {
    let dim = |base: NamedColor| indexed(base as u8).opacity(DIM_ALPHA);
    match named {
        NamedColor::Foreground | NamedColor::BrightForeground => hex(FOREGROUND),
        NamedColor::DimForeground => hex(FOREGROUND).opacity(DIM_ALPHA),
        NamedColor::Background => background(),
        NamedColor::Cursor => cursor(),
        NamedColor::DimBlack => dim(NamedColor::Black),
        NamedColor::DimRed => dim(NamedColor::Red),
        NamedColor::DimGreen => dim(NamedColor::Green),
        NamedColor::DimYellow => dim(NamedColor::Yellow),
        NamedColor::DimBlue => dim(NamedColor::Blue),
        NamedColor::DimMagenta => dim(NamedColor::Magenta),
        NamedColor::DimCyan => dim(NamedColor::Cyan),
        NamedColor::DimWhite => dim(NamedColor::White),
        NamedColor::Black
        | NamedColor::Red
        | NamedColor::Green
        | NamedColor::Yellow
        | NamedColor::Blue
        | NamedColor::Magenta
        | NamedColor::Cyan
        | NamedColor::White
        | NamedColor::BrightBlack
        | NamedColor::BrightRed
        | NamedColor::BrightGreen
        | NamedColor::BrightYellow
        | NamedColor::BrightBlue
        | NamedColor::BrightMagenta
        | NamedColor::BrightCyan
        | NamedColor::BrightWhite => indexed(named as u8),
    }
}

fn indexed(index: u8) -> Hsla {
    hex(match index {
        0..CUBE_FIRST => ANSI.get(usize::from(index)).copied().unwrap_or(FOREGROUND),
        CUBE_FIRST..GREY_FIRST => {
            let cube = index - CUBE_FIRST;
            let level = |step: u8| {
                if step == 0 {
                    0
                } else {
                    55 + 40 * u32::from(step)
                }
            };
            level(cube / 36) << 16 | level(cube / 6 % 6) << 8 | level(cube % 6)
        }
        GREY_FIRST..=u8::MAX => {
            let grey = 8 + 10 * u32::from(index - GREY_FIRST);
            grey << 16 | grey << 8 | grey
        }
    })
}
