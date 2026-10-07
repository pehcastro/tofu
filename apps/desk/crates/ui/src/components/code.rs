use std::collections::BTreeMap;
use std::ops::Range;
use std::rc::Rc;

use gpui::{
    AnyElement, App, Bounds, ContentMask, Div, ElementId, Font, HighlightStyle,
    ListHorizontalSizingBehavior, PathBuilder, Pixels, Point, ScrollHandle, ShapedLine,
    SharedString, StyledText, TextAlign, TextRun, UniformListScrollHandle, Window, canvas,
    combine_highlights, div, fill, font, point, prelude::*, px, rgb_to_hsla, size, uniform_list,
};

use crate::components::chip::{mono, tabular};
use crate::components::diff::Highlight;
use crate::components::paint::{ink, tint};
use crate::components::scroll::scrollbar;
use crate::components::size::{
    CARET_HEIGHT, CARET_WIDTH, CODE_LINE, FONT_BODY, NUMBER_COLUMN, NUMBER_PAD, NUMBER_TEXT,
    SELECTION_FILL, T1,
};
use crate::live::ActiveTheme;
use crate::theme::{ColorToken, Theme};

const CODE_PAD_TOP: f32 = 2.0;
const MARK_INSET: f32 = 6.0;
const MARK_WIDTH: f32 = 3.0;
const MARK_RADIUS: f32 = 2.0;
const MARK_TOP: f32 = 3.0;
const REMOVED_SIZE: f32 = 6.0;
const EDGE_WIDTH: f32 = 2.0;

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct CodeLine {
    pub text: SharedString,
    pub runs: Highlight,
    pub selected: Vec<Range<usize>>,
    pub carets: Vec<usize>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum GutterMark {
    Changed(ColorToken),
    Removed(ColorToken),
}

pub type Trailing = Rc<dyn Fn(&Theme) -> AnyElement>;

#[derive(Clone, Default)]
pub struct LineMarks {
    pub gutter: Option<GutterMark>,
    pub edge: Option<ColorToken>,
    pub background: Option<ColorToken>,
    pub trailing: Option<Trailing>,
}

pub type Marks = BTreeMap<usize, LineMarks>;

type LineSource = Rc<dyn Fn(Range<usize>, &mut App) -> Vec<CodeLine>>;

#[derive(IntoElement)]
pub struct CodeView {
    id: ElementId,
    count: usize,
    widest: usize,
    scroll: UniformListScrollHandle,
    lines: LineSource,
    marks: Rc<Marks>,
}

pub fn code_view(
    id: impl Into<ElementId>,
    count: usize,
    scroll: &UniformListScrollHandle,
    lines: impl Fn(Range<usize>, &mut App) -> Vec<CodeLine> + 'static,
) -> CodeView {
    CodeView {
        id: id.into(),
        count,
        widest: 0,
        scroll: scroll.clone(),
        lines: Rc::new(lines),
        marks: Rc::default(),
    }
}

impl CodeView {
    pub fn widest(mut self, line: usize) -> Self {
        self.widest = line;
        self
    }

    pub fn marks(mut self, marks: Rc<Marks>) -> Self {
        self.marks = marks;
        self
    }
}

fn shown_rows(bounds: Bounds<Pixels>, top: Pixels, count: usize) -> Range<usize> {
    let first = (-top / px(CODE_LINE)).floor().max(0.0) as usize;
    let shown = (bounds.size.height / px(CODE_LINE)).ceil() as usize + 1;
    first.min(count)..count.min(first + shown)
}

fn row_top(bounds: Bounds<Pixels>, top: Pixels, row: usize) -> Pixels {
    bounds.origin.y + top + px(CODE_LINE) * row as f32
}

fn backgrounds(marks: Rc<Marks>, count: usize, base: ScrollHandle) -> impl IntoElement {
    canvas(
        |_, _, _| {},
        move |bounds, _, window, cx| {
            let theme = ActiveTheme::theme(cx);
            let top = px(CODE_PAD_TOP) + base.offset().y;
            for (row, mark) in marks.range(shown_rows(bounds, top, count)) {
                if let Some(token) = mark.background {
                    let area = Bounds::new(
                        point(bounds.origin.x, row_top(bounds, top, *row)),
                        size(bounds.size.width, px(CODE_LINE)),
                    );
                    window.paint_quad(fill(area, theme.color(token)));
                }
            }
        },
    )
    .absolute()
    .top_0()
    .left_0()
    .size_full()
}

fn paint_marks(mark: &LineMarks, left: Pixels, y: Pixels, theme: &Theme, window: &mut Window) {
    if let Some(token) = mark.edge {
        let edge = Bounds::new(point(left, y), size(px(EDGE_WIDTH), px(CODE_LINE)));
        window.paint_quad(fill(edge, theme.color(token)));
    }
    match mark.gutter {
        Some(GutterMark::Changed(token)) => {
            let x = left + px(NUMBER_COLUMN - MARK_INSET - MARK_WIDTH);
            let bar = Bounds::new(
                point(x, y + px(MARK_TOP)),
                size(px(MARK_WIDTH), px(CODE_LINE - 2.0 * MARK_TOP)),
            );
            window.paint_quad(fill(bar, theme.color(token)).corner_radii(px(MARK_RADIUS)));
        }
        Some(GutterMark::Removed(token)) => {
            let x = left + px(NUMBER_COLUMN - MARK_INSET - REMOVED_SIZE);
            let top = y + px(CODE_LINE - REMOVED_SIZE / 2.0);
            let mut shape = PathBuilder::fill();
            shape.move_to(point(x, top));
            shape.line_to(point(x + px(REMOVED_SIZE), top + px(REMOVED_SIZE / 2.0)));
            shape.line_to(point(x, top + px(REMOVED_SIZE)));
            shape.close();
            if let Ok(path) = shape.build() {
                window.paint_path(path, theme.color(token));
            }
        }
        None => {}
    }
}

pub fn code_origin(
    bounds: Bounds<Pixels>,
    scroll: &UniformListScrollHandle,
    row: usize,
) -> Point<Pixels> {
    let offset = scroll.0.borrow().base_handle.offset();
    point(
        bounds.left() + px(NUMBER_COLUMN) + offset.x,
        bounds.top() + px(CODE_PAD_TOP) + offset.y + px(CODE_LINE) * row as f32,
    )
}

pub fn code_place(
    bounds: Bounds<Pixels>,
    scroll: &UniformListScrollHandle,
    position: Point<Pixels>,
) -> (usize, Pixels) {
    let origin = code_origin(bounds, scroll, 0);
    let row = ((position.y - origin.y) / px(CODE_LINE)).floor().max(0.0) as usize;
    (row, position.x - origin.x)
}

pub fn code_shape(text: SharedString, window: &Window, theme: &Theme) -> ShapedLine {
    let run = TextRun {
        len: text.len(),
        font: font(mono(theme)),
        color: rgb_to_hsla(ink(theme, T1)),
        background_color: None,
        underline: None,
        strikethrough: None,
        letter_spacing: None,
    };
    window
        .text_system()
        .shape_line(text, px(FONT_BODY), &[run], None)
}

fn code_row(line: &CodeLine, trailing: Option<&Trailing>, theme: &Theme) -> Div {
    let runs = line.runs.iter().map(|(range, token)| {
        (
            range.clone(),
            HighlightStyle {
                color: Some(rgb_to_hsla(theme.color(*token))),
                ..HighlightStyle::default()
            },
        )
    });
    let selection = HighlightStyle {
        background_color: Some(rgb_to_hsla(tint(
            theme.color(ColorToken::FocusRing),
            SELECTION_FILL,
        ))),
        ..HighlightStyle::default()
    };
    let selected = line.selected.iter().map(|range| (range.clone(), selection));
    let text =
        StyledText::new(line.text.clone()).with_highlights(combine_highlights(runs, selected));
    let layout = text.layout().clone();
    let carets = line.carets.clone();
    let color = ink(theme, T1);
    div()
        .flex()
        .items_center()
        .h(px(CODE_LINE))
        .whitespace_nowrap()
        .font_family(mono(theme))
        .text_size(px(FONT_BODY))
        .line_height(px(CODE_LINE))
        .child(
            div()
                .flex_none()
                .relative()
                .text_color(theme.color(ColorToken::SyntaxVariable))
                .child(text)
                .when(!carets.is_empty(), |row| {
                    row.child(
                        canvas(
                            |_, _, _| {},
                            move |_, _, window, _| {
                                for at in &carets {
                                    let Some(origin) = layout.position_for_index(*at) else {
                                        continue;
                                    };
                                    let top = origin.y + px((CODE_LINE - CARET_HEIGHT) / 2.0);
                                    let caret = Bounds::new(
                                        point(origin.x, top),
                                        size(px(CARET_WIDTH), px(CARET_HEIGHT)),
                                    );
                                    window.paint_quad(fill(caret, color));
                                }
                            },
                        )
                        .absolute()
                        .size_full(),
                    )
                }),
        )
        .children(trailing.map(|build| build(theme)))
}

impl RenderOnce for CodeView {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let lines = self.lines;
        let marks = self.marks.clone();
        let list = uniform_list(self.id.clone(), self.count, move |range, _, cx| {
            let theme = ActiveTheme::theme(cx);
            lines(range.clone(), cx)
                .iter()
                .zip(range)
                .map(|(line, row)| {
                    let trailing = marks.get(&row).and_then(|mark| mark.trailing.as_ref());
                    code_row(line, trailing, &theme)
                })
                .collect::<Vec<_>>()
        })
        .with_width_from_item(Some(self.widest))
        .with_horizontal_sizing_behavior(ListHorizontalSizingBehavior::Unconstrained)
        .size_full()
        .pt(px(CODE_PAD_TOP))
        .track_scroll(&self.scroll);
        let base = self.scroll.0.borrow().base_handle.clone();
        let bar = scrollbar(SharedString::from(format!("{}-bar", self.id)), &base);
        div()
            .relative()
            .size_full()
            .when(!self.marks.is_empty(), |view| {
                view.child(backgrounds(self.marks.clone(), self.count, base.clone()))
            })
            .child(div().size_full().pl(px(NUMBER_COLUMN)).child(list))
            .child(gutter(self.count, self.marks, base, window, cx))
            .child(bar)
    }
}

fn gutter(
    count: usize,
    marks: Rc<Marks>,
    base: ScrollHandle,
    window: &mut Window,
    cx: &mut App,
) -> Div {
    let theme = ActiveTheme::theme(cx);
    let run = TextRun {
        len: 0,
        font: Font {
            features: tabular(),
            ..font(mono(&theme))
        },
        color: rgb_to_hsla(ink(&theme, NUMBER_TEXT)),
        background_color: None,
        underline: None,
        strikethrough: None,
        letter_spacing: None,
    };
    let view = window.current_view();
    let wheel = base.clone();
    let numbers = canvas(
        move |bounds, window, _| {
            let top = px(CODE_PAD_TOP) + base.offset().y;
            shown_rows(bounds, top, count)
                .map(|row| {
                    let text = SharedString::from((row + 1).to_string());
                    let run = TextRun {
                        len: text.len(),
                        ..run.clone()
                    };
                    let shaped = window
                        .text_system()
                        .shape_line(text, px(FONT_BODY), &[run], None);
                    (row, row_top(bounds, top, row), shaped)
                })
                .collect::<Vec<_>>()
        },
        move |bounds, rows, window, cx| {
            let theme = ActiveTheme::theme(cx);
            window.with_content_mask(
                Some(ContentMask {
                    bounds,
                    ..ContentMask::default()
                }),
                |window| {
                    for (row, y, shaped) in rows {
                        if let Some(mark) = marks.get(&row) {
                            paint_marks(mark, bounds.origin.x, y, &theme, window);
                        }
                        let origin = point(bounds.origin.x, y);
                        let painted = shaped.paint(
                            origin,
                            px(CODE_LINE),
                            TextAlign::Right,
                            Some(px(NUMBER_COLUMN - NUMBER_PAD)),
                            window,
                            cx,
                        );
                        if painted.is_err() {
                            break;
                        }
                    }
                },
            );
        },
    )
    .size_full();
    div()
        .absolute()
        .top_0()
        .left_0()
        .h_full()
        .w(px(NUMBER_COLUMN))
        .on_scroll_wheel(move |event, window, cx| {
            let delta = event.delta.pixel_delta(window.line_height());
            let offset = wheel.offset();
            let y = (offset.y + delta.y).min(px(0.0));
            if y != offset.y {
                wheel.set_offset(point(offset.x, y));
                cx.notify(view);
            }
        })
        .child(numbers)
}
