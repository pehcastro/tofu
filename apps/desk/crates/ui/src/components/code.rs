use std::rc::Rc;

use gpui::{
    App, ContentMask, Div, ElementId, Font, HighlightStyle, ListHorizontalSizingBehavior,
    ScrollHandle, SharedString, StyledText, TextAlign, TextRun, UniformListScrollHandle, Window,
    canvas, div, font, point, prelude::*, px, rgb_to_hsla, uniform_list,
};

use crate::components::chip::{mono, tabular};
use crate::components::diff::Highlight;
use crate::components::paint::ink;
use crate::components::scroll::scrollbar;
use crate::components::size::{CODE_LINE, FONT_BODY, NUMBER_COLUMN, NUMBER_PAD, NUMBER_TEXT};
use crate::live::ActiveTheme;
use crate::theme::{ColorToken, Theme};

const CODE_PAD_TOP: f32 = 2.0;

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct CodeLine {
    pub text: SharedString,
    pub runs: Highlight,
}

type LineSource = Rc<dyn Fn(usize) -> CodeLine>;

#[derive(IntoElement)]
pub struct CodeView {
    id: ElementId,
    count: usize,
    widest: usize,
    scroll: UniformListScrollHandle,
    line: LineSource,
}

pub fn code_view(
    id: impl Into<ElementId>,
    count: usize,
    scroll: &UniformListScrollHandle,
    line: impl Fn(usize) -> CodeLine + 'static,
) -> CodeView {
    CodeView {
        id: id.into(),
        count,
        widest: 0,
        scroll: scroll.clone(),
        line: Rc::new(line),
    }
}

impl CodeView {
    pub fn widest(mut self, line: usize) -> Self {
        self.widest = line;
        self
    }
}

fn code_row(line: &CodeLine, theme: &Theme) -> Div {
    let runs = line.runs.iter().map(|(range, token)| {
        (
            range.clone(),
            HighlightStyle {
                color: Some(rgb_to_hsla(theme.color(*token))),
                ..HighlightStyle::default()
            },
        )
    });
    div()
        .flex()
        .h(px(CODE_LINE))
        .whitespace_nowrap()
        .font_family(mono(theme))
        .text_size(px(FONT_BODY))
        .line_height(px(CODE_LINE))
        .child(
            div()
                .flex_none()
                .text_color(theme.color(ColorToken::SyntaxVariable))
                .child(StyledText::new(line.text.clone()).with_highlights(runs)),
        )
}

impl RenderOnce for CodeView {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let line = self.line;
        let list = uniform_list(self.id.clone(), self.count, move |range, _, cx| {
            let theme = ActiveTheme::theme(cx);
            range
                .map(|ix| code_row(&line(ix), &theme))
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
            .child(div().size_full().pl(px(NUMBER_COLUMN)).child(list))
            .child(gutter(self.count, base, window, cx))
            .child(bar)
    }
}

fn gutter(count: usize, base: ScrollHandle, window: &mut Window, cx: &mut App) -> Div {
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
            let first = (-top / px(CODE_LINE)).floor().max(0.0) as usize;
            let shown = (bounds.size.height / px(CODE_LINE)).ceil() as usize + 1;
            (first..count.min(first + shown))
                .map(|row| {
                    let text = SharedString::from((row + 1).to_string());
                    let run = TextRun {
                        len: text.len(),
                        ..run.clone()
                    };
                    let y = top + px(CODE_LINE) * row as f32;
                    let shaped = window
                        .text_system()
                        .shape_line(text, px(FONT_BODY), &[run], None);
                    (y, shaped)
                })
                .collect::<Vec<_>>()
        },
        move |bounds, rows, window, cx| {
            window.with_content_mask(
                Some(ContentMask {
                    bounds,
                    ..ContentMask::default()
                }),
                |window| {
                    for (y, shaped) in rows {
                        let origin = point(bounds.origin.x, bounds.origin.y + y);
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
