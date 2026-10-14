use crate::palette::Palette;
use alacritty_terminal::term::RenderableContent;
use alacritty_terminal::term::cell::Flags;
use alacritty_terminal::term::point_to_viewport;
use alacritty_terminal::term::search::Match;
use alacritty_terminal::vte::ansi::CursorShape;
use gpui::{
    App, BorderStyle, Bounds, Font, FontFeatures, FontStyle, FontWeight, Hsla, PaintQuad, Pixels,
    Point, ShapedLine, SharedString, Size, StrikethroughStyle, TextAlign, TextRun, UnderlineStyle,
    Window, fill, font, outline, point, px, size,
};
use std::mem;
use std::ops::RangeInclusive;

const LINE_HEIGHT: f32 = 1.3;
const FALLBACK_ADVANCE: f32 = 0.6;
const BAR_WIDTH: f32 = 2.;
const DECORATION: f32 = 1.;
const BOX_DRAWING: RangeInclusive<char> = '\u{2500}'..='\u{259F}';
const POWERLINE: RangeInclusive<char> = '\u{E0B0}'..='\u{E0D7}';
pub(crate) const FAMILY: &str = if cfg!(target_os = "windows") {
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
    rows: usize,
    cols: usize,
    font: Pixels,
}

pub(crate) struct Painted {
    backgrounds: Vec<PaintQuad>,
    cursor: Option<PaintQuad>,
    lines: Vec<(Point<Pixels>, ShapedLine)>,
    thumb: Option<PaintQuad>,
    line_height: Pixels,
}

fn mono() -> Font {
    let mut mono = font(FAMILY);
    mono.features = FontFeatures::disable_ligatures();
    mono
}

pub(crate) fn cell_size(window: &Window, font: Pixels) -> Size<Pixels> {
    let text = window.text_system();
    let width = text
        .em_advance(text.resolve_font(&mono()), font)
        .unwrap_or(font * FALLBACK_ADVANCE);
    let scale = window.scale_factor();
    size(
        width,
        px((f32::from(font) * LINE_HEIGHT * scale).round() / scale),
    )
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
        let alone = drawn_alone(ch);
        if alone {
            self.close_span();
        }
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
        if alone || flags.contains(Flags::WIDE_CHAR) {
            self.close_span();
        }
    }
}

fn drawn_alone(ch: char) -> bool {
    BOX_DRAWING.contains(&ch) || POWERLINE.contains(&ch)
}

pub(crate) struct Look<'a> {
    pub palette: &'a Palette,
    pub focused: bool,
    pub cursor_lit: bool,
    pub font: Pixels,
    pub link: Option<(usize, RangeInclusive<usize>)>,
    pub found: &'a [Match],
}

pub(crate) fn frame(content: RenderableContent<'_>, look: Look<'_>) -> Frame {
    let Look {
        palette,
        focused,
        cursor_lit,
        font,
        link,
        found,
    } = look;
    let offset = content.display_offset;
    let cursor = point_to_viewport(offset, content.cursor.point)
        .filter(|_| cursor_lit)
        .map(|at| Cursor {
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
        let matched = found.iter().any(|found| found.contains(&indexed.point));
        let (fg, bg) = palette.cell_colors(indexed.cell);
        let highlight = match (selected, matched) {
            (true, _) => Some(palette.selection),
            (false, true) => Some(palette.found()),
            (false, false) => bg,
        };
        layout.back(col, highlight);
        if indexed.cell.flags.contains(Flags::WIDE_CHAR_SPACER) {
            continue;
        }
        let color = match solid {
            Some(cursor) if cursor.row == at.line && cursor.col == col => palette.background,
            _ => fg,
        };
        let mut flags = indexed.cell.flags;
        if link
            .as_ref()
            .is_some_and(|(row, cols)| *row == at.line && cols.contains(&col))
        {
            flags.insert(Flags::UNDERLINE);
        }
        layout.glyph(col, indexed.cell.c, indexed.cell.zerowidth(), color, flags);
    }
    layout.end_row(end);
    Frame {
        blocks: layout.blocks,
        spans: layout.spans,
        cursor,
        rows: layout.row + 1,
        cols: end,
        font,
    }
}

pub(crate) fn prepare(
    frame: &Frame,
    view: Bounds<Pixels>,
    cell: Size<Pixels>,
    focused: bool,
    palette: &Palette,
    thumb: Option<PaintQuad>,
    window: &Window,
) -> Painted {
    let origin = view.origin;
    let scale = window.scale_factor();
    let at = |row: usize, col: usize| {
        point(
            origin.x + cell.width * col as f32,
            origin.y + cell.height * row as f32,
        )
        .map(|value| px((f32::from(value) * scale).round() / scale))
    };
    let cells = |row: usize, col: usize, rows: usize, cols: usize| {
        Bounds::from_corners(at(row, col), at(row + rows, col + cols))
    };
    let backgrounds = frame
        .blocks
        .iter()
        .map(|block| {
            let mut rect = cells(block.row, block.col, block.rows, block.cols);
            if block.col + block.cols >= frame.cols {
                rect.size.width = view.right() - rect.left();
            }
            if block.row + block.rows >= frame.rows {
                rect.size.height = view.bottom() - rect.top();
            }
            fill(rect, block.color)
        })
        .collect();
    let cursor = frame.cursor.and_then(|cursor| {
        cursor_quad(
            cursor,
            cells(cursor.row, cursor.col, 1, 1),
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
                frame.font,
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
        thumb,
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
        if let Some(thumb) = self.thumb {
            window.paint_quad(thumb);
        }
    }
}
