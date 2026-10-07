use std::rc::Rc;

use gpui::{
    App, Div, ElementId, HighlightStyle, ListHorizontalSizingBehavior, SharedString, StyledText,
    UniformListScrollHandle, Window, div, prelude::*, px, rgb_to_hsla, uniform_list,
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

fn code_row(number: usize, line: &CodeLine, theme: &Theme) -> Div {
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
                .w(px(NUMBER_COLUMN))
                .pr(px(NUMBER_PAD))
                .text_right()
                .font_features(tabular())
                .text_color(ink(theme, NUMBER_TEXT))
                .child(number.to_string()),
        )
        .child(
            div()
                .flex_none()
                .text_color(theme.color(ColorToken::SyntaxVariable))
                .child(StyledText::new(line.text.clone()).with_highlights(runs)),
        )
}

impl RenderOnce for CodeView {
    fn render(self, _: &mut Window, _: &mut App) -> impl IntoElement {
        let line = self.line;
        let list = uniform_list(self.id.clone(), self.count, move |range, _, cx| {
            let theme = ActiveTheme::theme(cx);
            range
                .map(|ix| code_row(ix + 1, &line(ix), &theme))
                .collect::<Vec<_>>()
        })
        .with_width_from_item(Some(self.widest))
        .with_horizontal_sizing_behavior(ListHorizontalSizingBehavior::Unconstrained)
        .size_full()
        .pt(px(CODE_PAD_TOP))
        .track_scroll(&self.scroll);
        let base = self.scroll.0.borrow().base_handle.clone();
        let bar = scrollbar(SharedString::from(format!("{}-bar", self.id)), &base);
        div().relative().size_full().child(list).child(bar)
    }
}
