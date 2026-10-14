use std::ops::Range;
use std::rc::Rc;

use gpui::{
    App, ClickEvent, Context, Div, Entity, FocusHandle, Focusable, HighlightStyle, KeyDownEvent,
    SharedString, Window, div, prelude::*, px, rgb_to_hsla,
};

use crate::components::button::{ButtonKind, button};
use crate::components::card::header_action;
use crate::components::chip::{kbd, tabular};
use crate::components::code_editor::{Class, class};
use crate::components::form::TextArea;
use crate::components::overlay::menu_surface;
use crate::components::paint::{ink, tint};
use crate::components::scroll::ScrollArea;
use crate::components::size::{
    FIELD, FONT_SMALL, HOVER, MENU_GAP, MENU_PAD, RADIUS_POP, ROW_ON, ROW_PAD_X, T1, T2, T3,
};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::theme::{ColorToken, Theme};

const FIND_FIELD: f32 = 180.0;
const COUNT_WIDTH: f32 = 72.0;
const RESULTS_HEIGHT: f32 = 360.0;
const RESULTS_WIDTH: f32 = 640.0;
const MATCH_TINT: f32 = 0.22;
const CURRENT_TINT: f32 = 0.6;
const PLACEHOLDER: &str = "Find";
const NO_MATCHES: &str = "no matches";
const REPLACE_PLACEHOLDER: &str = "Replace";

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Matching {
    pub case: bool,
    pub whole: bool,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Replace {
    Current,
    All,
}

pub fn find_ranges(text: &str, query: &str) -> Vec<Range<usize>> {
    find_matching(text, query, Matching::default())
}

pub fn find_matching(text: &str, query: &str, matching: Matching) -> Vec<Range<usize>> {
    if query.is_empty() {
        return Vec::new();
    }
    let found = match matching.case {
        true => text
            .match_indices(query)
            .map(|(at, hit)| at..at + hit.len())
            .collect(),
        false => folded_ranges(text, query),
    };
    match matching.whole {
        true => found
            .into_iter()
            .filter(|range| whole_word(text, range))
            .collect(),
        false => found,
    }
}

fn whole_word(text: &str, range: &Range<usize>) -> bool {
    let word = |ch: Option<char>| ch.is_some_and(|ch| class(ch) == Class::Word);
    let before = text
        .get(..range.start)
        .and_then(|text| text.chars().next_back());
    let after = text.get(range.end..).and_then(|text| text.chars().next());
    !word(before) && !word(after)
}

fn folded_ranges(text: &str, query: &str) -> Vec<Range<usize>> {
    let needle: Vec<char> = query.chars().flat_map(char::to_lowercase).collect();
    let mut found: Vec<Range<usize>> = Vec::new();
    for (start, _) in text.char_indices() {
        if found.last().is_some_and(|last| start < last.end) {
            continue;
        }
        if let Some(end) = match_end(text, start, &needle) {
            found.push(start..end);
        }
    }
    found
}

fn match_end(text: &str, start: usize, needle: &[char]) -> Option<usize> {
    let mut wanted = needle.iter();
    for (at, c) in text.get(start..)?.char_indices() {
        for lower in c.to_lowercase() {
            if wanted.next() != Some(&lower) {
                return None;
            }
        }
        if wanted.len() == 0 {
            return Some(start + at + c.len_utf8());
        }
    }
    None
}

pub fn match_highlights(
    ranges: &[Range<usize>],
    current: Option<usize>,
    theme: &Theme,
) -> Vec<(Range<usize>, HighlightStyle)> {
    let warn = theme.color(ColorToken::StatusWarn);
    let style = |alpha| HighlightStyle {
        background_color: Some(rgb_to_hsla(tint(warn, alpha))),
        ..HighlightStyle::default()
    };
    ranges
        .iter()
        .enumerate()
        .map(|(at, range)| match Some(at) == current {
            true => (range.clone(), style(CURRENT_TINT)),
            false => (range.clone(), style(MATCH_TINT)),
        })
        .collect()
}

type OnChange = Rc<dyn Fn(&str, usize, &mut Window, &mut App)>;
type OnClose = Rc<dyn Fn(&mut Window, &mut App)>;
type OnReplace = Rc<dyn Fn(Replace, &str, &mut Window, &mut App)>;
pub type OnPick = Rc<dyn Fn(usize, &mut Window, &mut App)>;

#[derive(Clone)]
pub struct FindGroup {
    pub label: SharedString,
    pub hits: Vec<SharedString>,
}

pub fn find_results(
    groups: &[FindGroup],
    current: usize,
    theme: &Theme,
    on_pick: Option<OnPick>,
) -> Div {
    let mut first = 0;
    let lit = ink(theme, ROW_ON);
    let hover = ink(theme, HOVER);
    div()
        .flex()
        .flex_col()
        .gap(px(MENU_GAP))
        .children(groups.iter().map(|group| {
            let start = first;
            first += group.hits.len();
            let rows = group.hits.iter().enumerate().map(|(offset, hit)| {
                let at = start + offset;
                let row = div()
                    .id(("find-hit", at))
                    .flex()
                    .items_center()
                    .h(px(FIELD))
                    .px(px(ROW_PAD_X))
                    .rounded(px(RADIUS_POP))
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(theme, T2))
                    .overflow_hidden()
                    .child(div().flex_1().min_w_0().truncate().child(hit.clone()))
                    .when(at == current, |row| row.bg(lit).text_color(ink(theme, T1)))
                    .hover(move |style| style.bg(hover));
                match on_pick.clone() {
                    Some(pick) => row
                        .cursor_pointer()
                        .on_click(move |_: &ClickEvent, window, cx| pick(at, window, cx)),
                    None => row,
                }
            });
            div()
                .flex()
                .flex_col()
                .child(
                    div()
                        .flex()
                        .justify_between()
                        .px(px(ROW_PAD_X))
                        .text_size(px(FONT_SMALL))
                        .text_color(ink(theme, T3))
                        .child(group.label.clone())
                        .child(
                            div()
                                .font_features(tabular())
                                .child(group.hits.len().to_string()),
                        ),
                )
                .children(rows)
        }))
}

pub struct FindBar {
    field: Entity<TextArea>,
    query: String,
    total: usize,
    current: usize,
    open: bool,
    return_focus: Option<FocusHandle>,
    on_change: Option<OnChange>,
    on_close: Option<OnClose>,
    on_pick: Option<OnPick>,
    groups: Vec<FindGroup>,
    matching: Matching,
    replace: Option<Entity<TextArea>>,
    on_replace: Option<OnReplace>,
}

impl FindBar {
    pub fn new(window: &mut Window, cx: &mut App) -> Entity<FindBar> {
        cx.new(|cx| {
            let field = cx.new(|cx| {
                TextArea::new(PLACEHOLDER.into(), window, cx)
                    .bare()
                    .max_lines(1)
            });
            cx.observe_in(&field, window, |bar: &mut FindBar, field, window, cx| {
                let query = field.read(cx).text();
                if query != bar.query {
                    bar.query = query;
                    bar.select(0, window, cx);
                }
            })
            .detach();
            FindBar {
                field,
                query: String::new(),
                total: 0,
                current: 0,
                open: false,
                return_focus: None,
                on_change: None,
                on_close: None,
                on_pick: None,
                groups: Vec::new(),
                matching: Matching::default(),
                replace: None,
                on_replace: None,
            }
        })
    }

    pub fn on_pick(&mut self, on_pick: impl Fn(usize, &mut Window, &mut App) + 'static) {
        self.on_pick = Some(Rc::new(on_pick));
    }

    pub fn set_groups(&mut self, groups: Vec<FindGroup>, cx: &mut Context<Self>) {
        let total = groups.iter().map(|group| group.hits.len()).sum();
        self.groups = groups;
        self.set_total(total, cx);
    }

    fn pick(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        let Some(on_pick) = self.on_pick.clone().filter(|_| at < self.total) else {
            return;
        };
        self.current = at;
        self.dismiss(window, cx);
        window.defer(cx, move |window, cx| on_pick(at, window, cx));
    }

    pub fn on_replace(
        &mut self,
        on_replace: impl Fn(Replace, &str, &mut Window, &mut App) + 'static,
    ) {
        self.on_replace = Some(Rc::new(on_replace));
    }

    pub fn matching(&self) -> Matching {
        self.matching
    }

    pub fn show_replace(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.on_replace.is_some() && self.replace.is_none() {
            self.replace = Some(cx.new(|cx| {
                TextArea::new(REPLACE_PLACEHOLDER.into(), window, cx)
                    .bare()
                    .max_lines(1)
            }));
        }
        cx.notify();
    }

    fn replace(&mut self, what: Replace, window: &mut Window, cx: &mut Context<Self>) {
        let (Some(on_replace), Some(field)) = (self.on_replace.clone(), &self.replace) else {
            return;
        };
        let with = field.read(cx).text();
        window.defer(cx, move |window, cx| on_replace(what, &with, window, cx));
    }

    fn toggle(&mut self, flip: fn(&mut Matching), window: &mut Window, cx: &mut Context<Self>) {
        flip(&mut self.matching);
        self.select(self.current, window, cx);
    }

    pub fn on_change(&mut self, on_change: impl Fn(&str, usize, &mut Window, &mut App) + 'static) {
        self.on_change = Some(Rc::new(on_change));
    }

    pub fn on_close(&mut self, on_close: impl Fn(&mut Window, &mut App) + 'static) {
        self.on_close = Some(Rc::new(on_close));
    }

    pub fn set_total(&mut self, total: usize, cx: &mut Context<Self>) {
        self.total = total;
        if self.current >= total {
            self.current = 0;
        }
        cx.notify();
    }

    pub fn open(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.open {
            self.open = true;
            self.return_focus = window.focused(cx);
            self.field.update(cx, |field, cx| field.clear(cx));
        }
        let field = self.field.read(cx).focus_handle(cx);
        window.focus(&field, cx);
        cx.notify();
    }

    fn dismiss(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.open = false;
        self.replace = None;
        if let Some(before) = self.return_focus.take() {
            window.focus(&before, cx);
        }
        if let Some(on_close) = self.on_close.clone() {
            window.defer(cx, move |window, cx| on_close(window, cx));
        }
        cx.notify();
    }

    fn select(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        self.current = at;
        if let Some(on_change) = self.on_change.clone() {
            let query = self.query.clone();
            window.defer(cx, move |window, cx| on_change(&query, at, window, cx));
        }
        cx.notify();
    }

    fn next(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.total > 0 {
            self.select((self.current + 1) % self.total, window, cx);
        }
    }

    fn previous(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if let Some(last) = self.total.checked_sub(1) {
            self.select(self.current.checked_sub(1).unwrap_or(last), window, cx);
        }
    }

    fn key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let keys = &event.keystroke;
        let picking = self.on_pick.is_some();
        let held = keys.modifiers;
        let toggles = self.on_replace.is_some();
        let replacing = self
            .replace
            .as_ref()
            .is_some_and(|field| field.read(cx).focus_handle(cx).is_focused(window));
        match (keys.key.as_str(), held.shift) {
            ("enter", _) if replacing && held.secondary() && held.alt => {
                self.replace(Replace::All, window, cx);
            }
            ("enter", false) if replacing => self.replace(Replace::Current, window, cx),
            ("c", _) if toggles && held.alt => self.toggle(|on| on.case = !on.case, window, cx),
            ("w", _) if toggles && held.alt => self.toggle(|on| on.whole = !on.whole, window, cx),
            ("h", _) if toggles && held.secondary() => self.show_replace(window, cx),
            ("enter", false) if picking => self.pick(self.current, window, cx),
            ("enter", false) | ("down", _) => self.next(window, cx),
            ("enter", true) | ("up", _) => self.previous(window, cx),
            ("escape", _) => self.dismiss(window, cx),
            _ => return,
        }
        cx.stop_propagation();
    }

    fn count(&self) -> SharedString {
        match (self.query.is_empty(), self.total) {
            (true, _) => SharedString::default(),
            (false, 0) => NO_MATCHES.into(),
            (false, total) => format!("{} of {total}", self.current + 1).into(),
        }
    }
}

impl Render for FindBar {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if !self.open {
            return div().into_any_element();
        }
        let theme = ActiveTheme::theme(cx);
        let bar = cx.entity().downgrade();
        let pick: OnPick = Rc::new(move |at, window, cx| {
            bar.update(cx, |bar, cx| bar.pick(at, window, cx))
                .unwrap_or_else(|_| eprintln!("find: the bar is gone"));
        });
        let results = (!self.groups.is_empty() && !self.query.is_empty()).then(|| {
            ScrollArea::new("find-results")
                .max_h(RESULTS_HEIGHT)
                .child(find_results(
                    &self.groups,
                    self.current,
                    &theme,
                    self.on_pick.as_ref().map(|_| pick),
                ))
        });
        let row = div()
            .flex()
            .flex_row()
            .items_center()
            .gap(px(MENU_GAP))
            .child(
                div()
                    .flex()
                    .items_center()
                    .w(px(FIND_FIELD))
                    .h(px(FIELD))
                    .px(px(ROW_PAD_X))
                    .child(self.field.clone()),
            )
            .child(
                div()
                    .w(px(COUNT_WIDTH))
                    .flex_none()
                    .text_size(px(FONT_SMALL))
                    .font_features(tabular())
                    .text_color(ink(&theme, T3))
                    .child(self.count()),
            )
            .when(self.on_replace.is_some(), |row| {
                let lit = ink(&theme, ROW_ON);
                let (case, whole) = (self.matching.case, self.matching.whole);
                row.child(
                    button("find-case", "Aa", None, ButtonKind::Text, &theme)
                        .when(case, |toggle| toggle.bg(lit))
                        .on_click(cx.listener(|bar, _: &ClickEvent, window, cx| {
                            bar.toggle(|on| on.case = !on.case, window, cx);
                        })),
                )
                .child(
                    button("find-whole", "Word", None, ButtonKind::Text, &theme)
                        .when(whole, |toggle| toggle.bg(lit))
                        .on_click(cx.listener(|bar, _: &ClickEvent, window, cx| {
                            bar.toggle(|on| on.whole = !on.whole, window, cx);
                        })),
                )
            })
            .child(
                button("find-previous", "Previous", None, ButtonKind::Text, &theme)
                    .child(kbd("Shift Enter", &theme))
                    .on_click(
                        cx.listener(|bar, _: &ClickEvent, window, cx| bar.previous(window, cx)),
                    ),
            )
            .child(
                button("find-next", "Next", None, ButtonKind::Text, &theme)
                    .child(kbd("Enter", &theme))
                    .on_click(cx.listener(|bar, _: &ClickEvent, window, cx| bar.next(window, cx))),
            )
            .child(
                header_action("find-close", Icon::Close, "Close", &theme).on_click(
                    cx.listener(|bar, _: &ClickEvent, window, cx| bar.dismiss(window, cx)),
                ),
            );
        let replacing = self.replace.clone().map(|field| {
            div()
                .flex()
                .flex_row()
                .items_center()
                .gap(px(MENU_GAP))
                .child(
                    div()
                        .flex()
                        .items_center()
                        .w(px(FIND_FIELD))
                        .h(px(FIELD))
                        .px(px(ROW_PAD_X))
                        .child(field),
                )
                .child(
                    button("find-replace", "Replace", None, ButtonKind::Text, &theme)
                        .child(kbd("Enter", &theme))
                        .on_click(cx.listener(|bar, _: &ClickEvent, window, cx| {
                            bar.replace(Replace::Current, window, cx);
                        })),
                )
                .child(
                    button(
                        "find-replace-all",
                        "Replace all",
                        None,
                        ButtonKind::Text,
                        &theme,
                    )
                    .child(kbd("Ctrl Alt Enter", &theme))
                    .on_click(cx.listener(
                        |bar, _: &ClickEvent, window, cx| {
                            bar.replace(Replace::All, window, cx);
                        },
                    )),
                )
        });
        menu_surface(&theme)
            .id("find-bar")
            .absolute()
            .top(px(MENU_PAD))
            .right(px(MENU_PAD))
            .flex_col()
            .gap(px(MENU_GAP))
            .p(px(MENU_PAD))
            .rounded(px(RADIUS_POP))
            .capture_key_down(cx.listener(Self::key))
            .when(results.is_some(), |bar| bar.w(px(RESULTS_WIDTH)))
            .child(row)
            .children(replacing)
            .children(results)
            .into_any_element()
    }
}
