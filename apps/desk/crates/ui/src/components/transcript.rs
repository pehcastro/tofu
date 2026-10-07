use std::ops::Range;

use gpui::{
    AnyElement, App, ElementId, FollowMode, ListAlignment, ListState, Window, div, list,
    prelude::*, px,
};

use crate::components::scroll::list_scrollbar;
use crate::theme::{ColorToken, Theme};

const OVERDRAW: f32 = 200.0;

pub struct Transcript {
    list: ListState,
}

impl Transcript {
    pub fn new(count: usize) -> Self {
        let list = ListState::new(count, ListAlignment::Top, px(OVERDRAW));
        list.set_follow_mode(FollowMode::Tail);
        Transcript { list }
    }

    pub fn reset(&mut self, count: usize) {
        self.list.reset(count);
        self.list.set_follow_mode(FollowMode::Tail);
    }

    pub fn splice(&mut self, old: Range<usize>, count: usize) {
        self.list.splice(old, count);
    }

    pub fn pinned(&self) -> bool {
        self.list.is_following_tail()
    }

    pub fn visible(&self) -> Range<usize> {
        let first = self.list.logical_scroll_top().item_ix;
        let bottom = self.list.viewport_bounds().bottom();
        let shown = (first..self.list.item_count())
            .take_while(|&at| {
                self.list
                    .bounds_for_item(at)
                    .is_some_and(|bounds| bounds.top() < bottom)
            })
            .count();
        first..first + shown
    }
}

pub fn transcript(
    id: impl Into<ElementId>,
    state: &Transcript,
    theme: &Theme,
    row: impl Fn(usize, &mut Window, &mut App) -> AnyElement + 'static,
) -> AnyElement {
    div()
        .relative()
        .flex()
        .flex_col()
        .size_full()
        .min_h_0()
        .text_color(theme.color(ColorToken::TextBase))
        .child(list(state.list.clone(), row).flex_1().w_full())
        .child(list_scrollbar(id, &state.list))
        .into_any_element()
}
