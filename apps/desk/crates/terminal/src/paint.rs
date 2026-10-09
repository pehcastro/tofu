use crate::palette::Palette;
use alacritty_terminal::term::RenderableContent;
use alacritty_terminal::term::cell::Flags;
use alacritty_terminal::term::point_to_viewport;
use alacritty_terminal::vte::ansi::CursorShape;
use gpui::{
    App, BorderStyle, Bounds, Font, FontFeatures, FontStyle, FontWeight, Hsla, PaintQuad, Pixels,
    Point, ShapedLine, SharedString, Size, StrikethroughStyle, TextAlign, TextRun, UnderlineStyle,
    Window, fill, font, outline, point, px, size,
};
use std::mem;

const FONT_SIZE: f32 = 14.;
const LINE_HEIGHT: f32 = 1.3;
const FALLBACK_ADVANCE: f32 = 0.6;
const BAR_WIDTH: f32 = 2.;
const DECORATION: f32 = 1.;
const FAMILY: &str = if cfg!(target_os = "windows") {
    "Consolas"
} else if cfg!(target_os = "macos") {
    "Menlo"
} else {
    "DejaVu Sans Mono"
};

#[derive(Clone, Copy)]
pub(crate) struct Cursor {
    pub row: usize,
    pub col: usize,
    pub shape: CursorShape,
}

struct Block {
    row: usize,
    col: usize,
    rows: usize,
    cols: usize,
    color: Hsla,
}

struct Span {
    row: usize,
    col: usize,
    text: SharedString,
    runs: Vec<TextRun>,
}

pub(crate) struct Frame {
    blocks: Vec<Block>,
    spans: Vec<Span>,
    cursor: Option<Cursor>,
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

struct Layout {
    mono: Font,
    blocks: Vec<Block>,
    above: Vec<usize>,
    here: Vec<usize>,
    spans: Vec<Span>,
    row: usize,
    back: Option<(usize, Hsla)>,
    text: String,
    runs: Vec<TextRun>,
    start: usize,
    ink: usize,
}

impl Layout {
    fn close_back(&mut self, end: usize) {
        let Some((col, color)) = self.back.take() else {
            return;
        };
        let cols = end.saturating_sub(col);
        let row = self.row;
        let merged = self.above.iter().copied().find(|at| {
            self.blocks.get(*at).is_some_and(|block| {
                block.col == col
                    && block.cols == cols
                    && block.color == color
                    && block.row + block.rows == row
            })
        });
        match merged.and_then(|at| self.blocks.get_mut(at).map(|block| (at, block))) {
            Some((at, block)) => {
                block.rows += 1;
                self.here.push(at);
            }
            None => {
                self.here.push(self.blocks.len());
                self.blocks.push(Block {
                    row,
                    col,
                    rows: 1,
                    cols,
                    color,
                });
            }
        }
    }

    fn back(&mut self, col: usize, color: Option<Hsla>) {
        if self.back.map(|(_, open)| open) == color {
            return;
        }
        self.close_back(col);
        self.back = color.map(|color| (col, color));
    }

    fn close_span(&mut self) {
        let mut text = mem::take(&mut self.text);
        let mut runs = mem::take(&mut self.runs);
        if self.ink == 0 {
            return;
        }
        text.truncate(self.ink);
        let mut left = self.ink;
        runs.retain_mut(|run| {
            run.len = run.len.min(left);
            left -= run.len;
            run.len > 0
        });
        self.ink = 0;
        self.spans.push(Span {
            row: self.row,
            col: self.start,
            text: text.into(),
            runs,
        });
    }

    fn end_row(&mut self, end: usize) {
        self.close_back(end);
        self.close_span();
        self.above = mem::take(&mut self.here);
    }

    fn glyph(&mut self, col: usize, ch: char, extra: Option<&[char]>, color: Hsla, flags: Flags) {
        if self.text.is_empty() {
            self.start = col;
        }
        let weight = if flags.contains(Flags::BOLD) {
            FontWeight::BOLD
        } else {
            FontWeight::NORMAL
        };
        let style = if flags.contains(Flags::ITALIC) {
            FontStyle::Italic
        } else {
            FontStyle::Normal
        };
        let underline = flags
            .intersects(Flags::ALL_UNDERLINES)
            .then_some(UnderlineStyle {
                thickness: px(DECORATION),
                color: None,
                wavy: flags.contains(Flags::UNDERCURL),
            });
        let strikethrough = flags
            .contains(Flags::STRIKEOUT)
            .then_some(StrikethroughStyle {
                thickness: px(DECORATION),
                color: None,
            });
        let before = self.text.len();
        self.text.push(if ch == '\t' { ' ' } else { ch });
        self.text.extend(extra.into_iter().flatten());
        let len = self.text.len() - before;
        if ch != ' ' || underline.is_some() || strikethrough.is_some() {
            self.ink = self.text.len();
        }
        match self.runs.last_mut() {
            Some(last)
                if last.color == color
                    && last.font.weight == weight
                    && last.font.style == style
                    && last.underline == underline
                    && last.strikethrough == strikethrough =>
            {
                last.len += len;
            }
            _ => self.runs.push(TextRun {
                len,
                font: Font {
                    weight,
                    style,
                    ..self.mono.clone()
                },
                color,
                background_color: None,
                underline,
                strikethrough,
                letter_spacing: None,
            }),
        }
        if flags.contains(Flags::WIDE_CHAR) {
            self.close_span();
        }
    }
}

pub(crate) fn frame(content: RenderableContent<'_>, palette: &Palette, focused: bool) -> Frame {
    let offset = content.display_offset;
    let cursor = point_to_viewport(offset, content.cursor.point).map(|at| Cursor {
        row: at.line,
        col: at.column.0,
        shape: content.cursor.shape,
    });
    let solid = cursor.filter(|cursor| focused && cursor.shape == CursorShape::Block);
    let mut layout = Layout {
        mono: mono(),
        blocks: Vec::new(),
        above: Vec::new(),
        here: Vec::new(),
        spans: Vec::new(),
        row: 0,
        back: None,
        text: String::new(),
        runs: Vec::new(),
        start: 0,
        ink: 0,
    };
    let mut end = 0;
    for indexed in content.display_iter {
        let Some(at) = point_to_viewport(offset, indexed.point) else {
            continue;
        };
        let col = at.column.0;
        if at.line != layout.row {
            layout.end_row(end);
            layout.row = at.line;
        }
        end = col + 1;
        let selected = content.selection.is_some_and(|selection| {
            selection.contains_cell(&indexed, content.cursor.point, content.cursor.shape)
        });
        let (fg, bg) = palette.cell_colors(indexed.cell);
        layout.back(col, selected.then_some(palette.selection).or(bg));
        if indexed.cell.flags.contains(Flags::WIDE_CHAR_SPACER) {
            continue;
        }
        let color = match solid {
            Some(cursor) if cursor.row == at.line && cursor.col == col => palette.background,
            _ => fg,
        };
        layout.glyph(
            col,
            indexed.cell.c,
            indexed.cell.zerowidth(),
            color,
            indexed.cell.flags,
        );
    }
    layout.end_row(end);
    Frame {
        blocks: layout.blocks,
        spans: layout.spans,
        cursor,
    }
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
    let backgrounds = frame
        .blocks
        .iter()
        .map(|block| {
            fill(
                Bounds::new(
                    at(block.row, block.col),
                    size(
                        cell.width * block.cols as f32,
                        cell.height * block.rows as f32,
                    ),
                ),
                block.color,
            )
        })
        .collect();
    let cursor = frame.cursor.and_then(|cursor| {
        cursor_quad(
            cursor,
            Bounds::new(at(cursor.row, cursor.col), cell),
            focused,
            palette.cursor,
        )
    });
    let lines = frame
        .spans
        .iter()
        .map(|span| {
            let shaped = window.text_system().shape_line(
                span.text.clone(),
                px(FONT_SIZE),
                &span.runs,
                Some(cell.width),
            );
            (at(span.row, span.col), shaped)
        })
        .collect();
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
