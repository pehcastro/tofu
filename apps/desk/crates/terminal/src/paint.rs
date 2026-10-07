use crate::palette::Palette;
use alacritty_terminal::term::cell::Flags;
use alacritty_terminal::vte::ansi::CursorShape;
use gpui::{
    App, BorderStyle, Bounds, Font, FontFeatures, FontStyle, FontWeight, Hsla, PaintQuad, Pixels,
    Point, ShapedLine, Size, TextAlign, TextRun, UnderlineStyle, Window, fill, font, outline,
    point, px, size,
};

const FONT_SIZE: f32 = 14.;
const LINE_HEIGHT: f32 = 1.3;
const FALLBACK_ADVANCE: f32 = 0.6;
const BAR_WIDTH: f32 = 2.;
const FAMILY: &str = if cfg!(target_os = "windows") {
    "Consolas"
} else if cfg!(target_os = "macos") {
    "Menlo"
} else {
    "DejaVu Sans Mono"
};

pub(crate) struct Glyph {
    pub ch: char,
    pub fg: Hsla,
    pub bg: Option<Hsla>,
    pub flags: Flags,
}

#[derive(Clone, Copy)]
pub(crate) struct Cursor {
    pub row: usize,
    pub col: usize,
    pub shape: CursorShape,
}

pub(crate) struct Frame {
    pub rows: Vec<Vec<Glyph>>,
    pub cursor: Option<Cursor>,
}

pub(crate) struct Painted {
    backgrounds: Vec<PaintQuad>,
    cursor: Option<PaintQuad>,
    lines: Vec<(Point<Pixels>, ShapedLine)>,
    line_height: Pixels,
}

fn mono() -> Font {
    let mut mono = font(FAMILY);
    mono.features = FontFeatures::disable_ligatures();
    mono
}

pub(crate) fn cell_size(window: &Window) -> Size<Pixels> {
    let text = window.text_system();
    let width = text
        .em_advance(text.resolve_font(&mono()), px(FONT_SIZE))
        .unwrap_or(px(FONT_SIZE * FALLBACK_ADVANCE));
    size(width, px((FONT_SIZE * LINE_HEIGHT).round()))
}

pub(crate) fn prepare(
    frame: &Frame,
    origin: Point<Pixels>,
    cell: Size<Pixels>,
    focused: bool,
    palette: &Palette,
    window: &Window,
) -> Painted {
    let at = |row: usize, col: usize| {
        point(
            origin.x + cell.width * col as f32,
            origin.y + cell.height * row as f32,
        )
    };
    let span = |row: usize, start: usize, end: usize| {
        Bounds::new(
            at(row, start),
            size(cell.width * end.saturating_sub(start) as f32, cell.height),
        )
    };
    let cursor = frame.cursor.and_then(|cursor| {
        cursor_quad(
            cursor,
            Bounds::new(at(cursor.row, cursor.col), cell),
            focused,
            palette.cursor,
        )
    });
    let solid = frame
        .cursor
        .filter(|cursor| focused && cursor.shape == CursorShape::Block);
    let mono = mono();
    let mut backgrounds = Vec::new();
    let mut lines = Vec::new();
    for (row, glyphs) in frame.rows.iter().enumerate() {
        let mut run: Option<(usize, Hsla)> = None;
        for (col, bg) in glyphs
            .iter()
            .map(|glyph| glyph.bg)
            .chain([None])
            .enumerate()
        {
            match run {
                Some((start, color)) if bg != Some(color) => {
                    backgrounds.push(fill(span(row, start, col), color));
                    run = bg.map(|bg| (col, bg));
                }
                None => run = bg.map(|bg| (col, bg)),
                Some(_) => {}
            }
        }
        let mut text = String::new();
        let mut runs: Vec<TextRun> = Vec::new();
        let mut start = 0;
        for (col, glyph) in glyphs.iter().enumerate() {
            if glyph.flags.contains(Flags::WIDE_CHAR_SPACER) {
                continue;
            }
            if text.is_empty() {
                start = col;
            }
            let color = match solid {
                Some(cursor) if cursor.row == row && cursor.col == col => palette.background,
                _ => glyph.fg,
            };
            let weight = if glyph.flags.contains(Flags::BOLD) {
                FontWeight::BOLD
            } else {
                FontWeight::NORMAL
            };
            let style = if glyph.flags.contains(Flags::ITALIC) {
                FontStyle::Italic
            } else {
                FontStyle::Normal
            };
            let underline =
                glyph
                    .flags
                    .intersects(Flags::ALL_UNDERLINES)
                    .then_some(UnderlineStyle {
                        thickness: px(1.),
                        color: None,
                        wavy: false,
                    });
            text.push(glyph.ch);
            match runs.last_mut() {
                Some(last)
                    if last.color == color
                        && last.font.weight == weight
                        && last.font.style == style
                        && last.underline == underline =>
                {
                    last.len += glyph.ch.len_utf8();
                }
                _ => runs.push(TextRun {
                    len: glyph.ch.len_utf8(),
                    font: Font {
                        weight,
                        style,
                        ..mono.clone()
                    },
                    color,
                    background_color: None,
                    underline,
                    strikethrough: None,
                    letter_spacing: None,
                }),
            }
            if glyph.flags.contains(Flags::WIDE_CHAR) {
                lines.push((
                    at(row, start),
                    shape(&mut text, &mut runs, cell.width, window),
                ));
            }
        }
        if !text.is_empty() {
            lines.push((
                at(row, start),
                shape(&mut text, &mut runs, cell.width, window),
            ));
        }
    }
    Painted {
        backgrounds,
        cursor,
        lines,
        line_height: cell.height,
    }
}

fn cursor_quad(
    cursor: Cursor,
    cell: Bounds<Pixels>,
    focused: bool,
    color: Hsla,
) -> Option<PaintQuad> {
    let bar = px(BAR_WIDTH);
    match (cursor.shape, focused) {
        (CursorShape::Hidden, _) => None,
        (CursorShape::Block, true) => Some(fill(cell, color)),
        (CursorShape::Block | CursorShape::HollowBlock, _) => {
            Some(outline(cell, color, BorderStyle::Solid))
        }
        (CursorShape::Beam, _) => Some(fill(
            Bounds::new(cell.origin, size(bar, cell.size.height)),
            color,
        )),
        (CursorShape::Underline, _) => Some(fill(
            Bounds::new(
                point(cell.left(), cell.bottom() - bar),
                size(cell.size.width, bar),
            ),
            color,
        )),
    }
}

fn shape(
    text: &mut String,
    runs: &mut Vec<TextRun>,
    advance: Pixels,
    window: &Window,
) -> ShapedLine {
    window.text_system().shape_line(
        std::mem::take(text).into(),
        px(FONT_SIZE),
        &std::mem::take(runs),
        Some(advance),
    )
}

impl Painted {
    pub(crate) fn paint(self, window: &mut Window, cx: &mut App) {
        for quad in self.backgrounds {
            window.paint_quad(quad);
        }
        if let Some(cursor) = self.cursor {
            window.paint_quad(cursor);
        }
        for (origin, line) in self.lines {
            if let Err(error) =
                line.paint(origin, self.line_height, TextAlign::Left, None, window, cx)
            {
                eprintln!("terminal could not paint a line: {error:#}");
            }
        }
    }
}
