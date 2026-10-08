use std::ops::Range;
use std::rc::Rc;
use std::time::{Duration, Instant};

use desk_motion::tokens::TOGGLE;
use gpui::{
    App, Bounds, ClipboardItem, Context, Div, ElementId, ElementInputHandler, Entity,
    EntityInputHandler, FocusHandle, Focusable, FontWeight, HighlightStyle, KeyDownEvent,
    Modifiers, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels, Point,
    ScrollHandle, SharedString, Stateful, StyledText, Subscription, Task, TextLayout,
    UTF16Selection, UnderlineStyle, Window, canvas, combine_highlights, div, fill, point,
    prelude::*, px, rgb_to_hsla, size,
};

use crate::components::overlay::{ContextMenu, actions, context_menu};
use crate::components::paint::{arrowed, focus_ring, ink, pressed, tint};
use crate::components::size::{
    CARET_BLINK_MS, CARET_HEIGHT, CARET_WIDTH, DIM_TEXT, FIELD, FIELD_PAD, FIELD_WIDTH, FONT_BODY,
    FONT_SMALL, RADIUS_CHIP, RADIUS_ROW, SEGMENT_PAD_X, SEGMENT_PAD_Y, SEGMENTED_PAD,
    SELECTION_FILL, SWITCH_HEIGHT, SWITCH_INSET, SWITCH_THUMB, SWITCH_WIDTH, T1, T3,
};
use crate::live::ActiveTheme;
use crate::theme::{ColorToken, Theme};

const CARET_SCALE: f32 = 0.85;
const TEXT_AREA_LINES: usize = 8;
const TEXT_AREA_LINE: f32 = 20.0;
const TEXT_AREA_PAD_Y: f32 = 5.0;
const MARKED_UNDERLINE: f32 = 1.0;

pub fn input(
    id: impl Into<ElementId>,
    label: &'static str,
    value: SharedString,
    focused: bool,
    theme: &Theme,
) -> Stateful<Div> {
    let empty = value.is_empty();
    div()
        .id(id)
        .aria_label(label)
        .flex()
        .items_center()
        .w(px(FIELD_WIDTH))
        .h(px(FIELD))
        .px(px(FIELD_PAD))
        .rounded(px(RADIUS_ROW))
        .bg(theme.color(ColorToken::FieldFill))
        .text_size(px(FONT_BODY))
        .text_color(ink(theme, if empty { T3 } else { T1 }))
        .cursor_text()
        .child(if empty {
            SharedString::from(label)
        } else {
            value
        })
        .when(focused, |field| {
            field.child(
                div()
                    .w(px(CARET_WIDTH))
                    .h(px(CARET_HEIGHT * CARET_SCALE))
                    .bg(ink(theme, T1)),
            )
        })
}

pub fn segmented(
    id: &'static str,
    options: &[&'static str],
    selected: usize,
    theme: &Theme,
    on_change: impl Fn(&usize, &mut Window, &mut App) + 'static,
) -> Div {
    div()
        .flex()
        .flex_none()
        .child(segment_group(id, options, selected, theme, on_change))
}

pub fn segmented_with_focus(
    focus: &FocusHandle,
    id: &'static str,
    options: &[&'static str],
    selected: usize,
    theme: &Theme,
    on_change: impl Fn(&usize, &mut Window, &mut App) + 'static,
) -> Div {
    let group = segment_group(id, options, selected, theme, on_change);
    div()
        .flex()
        .flex_none()
        .child(group.track_focus(&focus.clone().tab_stop(true)))
}

fn segment_group(
    id: &'static str,
    options: &[&'static str],
    selected: usize,
    theme: &Theme,
    on_change: impl Fn(&usize, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let on_change = Rc::new(on_change);
    let count = options.len();
    let stepper = on_change.clone();
    let backdrop = theme.color(ColorToken::SegmentedFill);
    let bright = ink(theme, T1);
    let segments = options.iter().enumerate().map(|(ix, option)| {
        let on_change = on_change.clone();
        let chosen = ix == selected;
        let segment = div()
            .id((id, ix))
            .aria_label(*option)
            .px(px(SEGMENT_PAD_X))
            .py(px(SEGMENT_PAD_Y))
            .rounded(px(RADIUS_CHIP - SEGMENTED_PAD))
            .cursor_pointer()
            .text_size(px(FONT_SMALL))
            .font_weight(FontWeight::MEDIUM)
            .text_color(ink(theme, if chosen { T1 } else { DIM_TEXT }))
            .map(|segment| {
                if chosen {
                    segment.bg(theme.color(ColorToken::SegmentedOn))
                } else {
                    segment.hover(move |style| style.text_color(bright))
                }
            })
            .on_click(move |_, window, cx| on_change(&ix, window, cx))
            .child(*option);
        pressed(segment, backdrop)
    });
    focus_ring(div().id(id), theme)
        .flex()
        .flex_none()
        .items_center()
        .p(px(SEGMENTED_PAD))
        .rounded(px(RADIUS_ROW))
        .bg(backdrop)
        .on_key_down(move |event, window, cx| {
            if let Some(to) = arrowed(event, selected, count) {
                cx.stop_propagation();
                if to != selected {
                    stepper(&to, window, cx);
                }
            }
        })
        .children(segments)
}

pub fn switch(
    id: impl Into<ElementId>,
    label: &'static str,
    on: bool,
    theme: &Theme,
) -> Stateful<Div> {
    switch_bare(id, label, on, theme).gap_2().child(label)
}

pub fn switch_bare(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    on: bool,
    theme: &Theme,
) -> Stateful<Div> {
    let travel = if on {
        SWITCH_WIDTH - SWITCH_THUMB - 2.0 * SWITCH_INSET
    } else {
        0.0
    };
    focus_ring(div().id(id), theme)
        .aria_label(label)
        .flex()
        .items_center()
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .text_size(px(FONT_BODY))
        .text_color(ink(theme, T1))
        .child(
            pressed(
                div()
                    .id("track")
                    .flex()
                    .flex_none()
                    .items_center()
                    .w(px(SWITCH_WIDTH))
                    .h(px(SWITCH_HEIGHT))
                    .px(px(SWITCH_INSET))
                    .rounded_full()
                    .bg(theme.color(if on {
                        ColorToken::SwitchOn
                    } else {
                        ColorToken::SwitchOff
                    })),
                theme.color(ColorToken::CardsInnerFill),
            )
            .child(
                div()
                    .id("thumb")
                    .ml(px(travel))
                    .size(px(SWITCH_THUMB))
                    .rounded_full()
                    .bg(theme.color(ColorToken::SwitchThumb))
                    .transitions(|transitions| transitions.ml(TOGGLE)),
            ),
        )
}

struct Span {
    text: String,
    range: Range<usize>,
    reversed: bool,
    layout: TextLayout,
    dragging: bool,
}

impl Span {
    fn new(text: String) -> Self {
        Span {
            text,
            range: 0..0,
            reversed: false,
            layout: TextLayout::default(),
            dragging: false,
        }
    }

    fn head(&self) -> usize {
        if self.reversed {
            self.range.start
        } else {
            self.range.end
        }
    }

    fn select(&mut self, to: usize, extend: bool) {
        let tail = match (extend, self.reversed) {
            (false, _) => to,
            (true, true) => self.range.end,
            (true, false) => self.range.start,
        };
        self.reversed = to < tail;
        self.range = to.min(tail)..to.max(tail);
    }

    fn selected(&self) -> &str {
        self.text.get(self.range.clone()).unwrap_or_default()
    }

    fn previous(&self, at: usize) -> usize {
        self.text
            .get(..at)
            .and_then(|before| before.char_indices().next_back())
            .map_or(0, |(ix, _)| ix)
    }

    fn next(&self, at: usize) -> usize {
        self.text
            .get(at..)
            .and_then(|after| after.chars().next())
            .map_or(at, |ch| at + ch.len_utf8())
    }

    fn index_at(&self, position: Point<Pixels>) -> usize {
        let bounds = self.layout.bounds();
        let y = position.y.min(bounds.bottom() - px(1.0)).max(bounds.top());
        let mut at = self
            .layout
            .index_for_position(point(position.x, y))
            .unwrap_or_else(|nearest| nearest)
            .min(self.text.len());
        while !self.text.is_char_boundary(at) {
            at -= 1;
        }
        at
    }

    fn navigate(&mut self, key: &str, modifiers: &Modifiers) -> bool {
        let collapse = !modifiers.shift && !self.range.is_empty();
        let to = match (modifiers.secondary(), key) {
            (false, "left") if collapse => self.range.start,
            (false, "right") if collapse => self.range.end,
            (false, "left") => self.previous(self.head()),
            (false, "right") => self.next(self.head()),
            (false, "home") => 0,
            (false, "end") => self.text.len(),
            (true, "a") => {
                self.select(0, false);
                self.text.len()
            }
            _ => return false,
        };
        self.select(to, modifiers.shift || key == "a");
        true
    }

    fn copy(&self, cx: &mut App) {
        if !self.range.is_empty() {
            cx.write_to_clipboard(ClipboardItem::new_string(self.selected().to_owned()));
        }
    }

    fn select_all(&mut self) {
        self.select(0, false);
        self.select(self.text.len(), true);
    }

    fn select_word(&mut self, at: usize) {
        let word = |(_, ch): &(usize, char)| ch.is_alphanumeric() || *ch == '_';
        let (before, after) = self.text.split_at_checked(at).unwrap_or_default();
        let start = before
            .char_indices()
            .rev()
            .take_while(word)
            .last()
            .map_or(at, |(ix, _)| ix);
        let end = after
            .char_indices()
            .find(|found| !word(found))
            .map_or(self.text.len(), |(ix, _)| at + ix);
        self.select(start, false);
        self.select(end, true);
    }

    fn styled(
        &mut self,
        shown: SharedString,
        marked: Option<Range<usize>>,
        theme: &Theme,
    ) -> StyledText {
        let highlight = HighlightStyle {
            background_color: Some(rgb_to_hsla(tint(
                theme.color(ColorToken::FocusRing),
                SELECTION_FILL,
            ))),
            ..HighlightStyle::default()
        };
        let underline = HighlightStyle {
            underline: Some(UnderlineStyle {
                thickness: px(MARKED_UNDERLINE),
                color: Some(rgb_to_hsla(ink(theme, T1))),
                wavy: false,
            }),
            ..HighlightStyle::default()
        };
        let selection = (!self.range.is_empty()).then(|| (self.range.clone(), highlight));
        let composing = marked.map(|range| (range, underline));
        let text = StyledText::new(shown).with_highlights(combine_highlights(selection, composing));
        self.layout = text.layout().clone();
        text
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Edit {
    Cut,
    Copy,
    Paste,
    SelectAll,
}

impl Edit {
    fn label(self) -> &'static str {
        match self {
            Edit::Cut => "Cut",
            Edit::Copy => "Copy",
            Edit::Paste => "Paste",
            Edit::SelectAll => "Select all",
        }
    }
}

trait Spanned: Focusable + Sized + 'static {
    const EDITS: &'static [Edit];

    fn span(&mut self) -> &mut Span;

    fn edit(&mut self, edit: Edit, cx: &mut Context<Self>);
}

fn edit_menu<T: Spanned>(
    id: &'static str,
    body: Stateful<Div>,
    cx: &mut Context<T>,
) -> ContextMenu {
    let entity = cx.entity();
    context_menu(actions(T::EDITS.iter().map(|edit| edit.label())))
        .id(ElementId::NamedInteger(
            id.into(),
            entity.entity_id().as_u64(),
        ))
        .on_pick(move |ix, window, cx| {
            let Some(edit) = T::EDITS.get(*ix).copied() else {
                return;
            };
            entity.update(cx, |this, cx| {
                window.focus(&this.focus_handle(cx), cx);
                this.edit(edit, cx);
                cx.notify();
            });
        })
        .child(selectable(body, cx))
}

fn selectable<T: Spanned>(element: Stateful<Div>, cx: &mut Context<T>) -> Stateful<Div> {
    let release = |this: &mut T, _: &MouseUpEvent, _: &mut Window, _: &mut Context<T>| {
        this.span().dragging = false;
    };
    element
        .cursor_text()
        .on_mouse_down(
            MouseButton::Left,
            cx.listener(|this: &mut T, event: &MouseDownEvent, window, cx| {
                let focus = this.focus_handle(cx);
                window.focus(&focus, cx);
                let span = this.span();
                let at = span.index_at(event.position);
                match event.click_count {
                    0 | 1 => span.select(at, event.modifiers.shift),
                    2 => span.select_word(at),
                    _ => span.select_all(),
                }
                span.dragging = event.click_count <= 1;
                cx.notify();
            }),
        )
        .on_mouse_move(cx.listener(|this: &mut T, event: &MouseMoveEvent, _, cx| {
            let span = this.span();
            if span.dragging {
                let at = span.index_at(event.position);
                span.select(at, true);
                cx.notify();
            }
        }))
        .on_mouse_up(MouseButton::Left, cx.listener(release))
        .on_mouse_up_out(MouseButton::Left, cx.listener(release))
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Lines {
    One,
    Many,
}

enum Keyed {
    Ignored,
    Handled,
    Submit,
}

trait Editable: Sized + 'static {
    fn editor(&mut self) -> &mut Editor;
}

struct Editor {
    placeholder: SharedString,
    span: Span,
    lines: Lines,
    marked: Option<Range<usize>>,
    undo: Option<(String, Range<usize>)>,
    since: Instant,
    reveal: bool,
    blink: Option<Task<()>>,
    focus: FocusHandle,
    _focus_changes: [Subscription; 2],
}

pub fn caret_shown(since: Instant) -> bool {
    (since.elapsed().as_millis() / u128::from(CARET_BLINK_MS)).is_multiple_of(2)
}

pub fn blinker<T: 'static>(cx: &mut Context<T>) -> Task<()> {
    cx.spawn(async move |this, cx| {
        loop {
            cx.background_executor()
                .timer(Duration::from_millis(CARET_BLINK_MS))
                .await;
            if this.update(cx, |_, cx| cx.notify()).is_err() {
                break;
            }
        }
    })
}

fn to_utf16(text: &str, offset: usize) -> usize {
    text.get(..offset)
        .map_or(0, |before| before.encode_utf16().count())
}

fn from_utf16(text: &str, offset: usize) -> usize {
    let mut units = 0;
    for (at, ch) in text.char_indices() {
        if units >= offset {
            return at;
        }
        units += ch.len_utf16();
    }
    text.len()
}

impl Editor {
    fn new<T: Editable>(
        placeholder: SharedString,
        lines: Lines,
        window: &mut Window,
        cx: &mut Context<T>,
    ) -> Self {
        let focus = cx.focus_handle();
        let focused = cx.on_focus(&focus, window, |this: &mut T, _, cx| {
            let editor = this.editor();
            editor.blink = Some(blinker(cx));
            editor.touch(cx);
        });
        let blurred = cx.on_blur(&focus, window, |this: &mut T, _, cx| {
            let editor = this.editor();
            editor.blink = None;
            editor.span.dragging = false;
            cx.notify();
        });
        Editor {
            placeholder,
            span: Span::new(String::new()),
            lines,
            marked: None,
            undo: None,
            since: Instant::now(),
            reveal: false,
            blink: None,
            focus,
            _focus_changes: [focused, blurred],
        }
    }

    fn touch<T: 'static>(&mut self, cx: &mut Context<T>) {
        self.since = Instant::now();
        self.reveal = true;
        if self.blink.is_some() {
            self.blink = Some(blinker(cx));
        }
        cx.notify();
    }

    fn caret_on(&self) -> bool {
        caret_shown(self.since)
    }

    fn replace(&mut self, range: Range<usize>, with: &str) {
        if range.is_empty() && with.is_empty() {
            return;
        }
        let typed = match self.lines {
            Lines::One => with.replace(['\r', '\n'], " "),
            Lines::Many => with.replace("\r\n", "\n").replace('\r', "\n"),
        };
        self.undo = Some((self.span.text.clone(), self.span.range.clone()));
        self.span.text.replace_range(range.clone(), &typed);
        self.span.select(range.start + typed.len(), false);
        self.marked = None;
    }

    fn undo(&mut self) {
        if let Some((text, range)) = self.undo.take() {
            self.marked = None;
            let current = std::mem::replace(&mut self.span.text, text);
            self.undo = Some((current, self.span.range.clone()));
            self.span.range = range;
            self.span.reversed = false;
        }
    }

    fn edit<T: 'static>(&mut self, edit: Edit, cx: &mut Context<T>) {
        let range = self.span.range.clone();
        match edit {
            Edit::Cut => {
                self.span.copy(cx);
                self.replace(range, "");
            }
            Edit::Copy => self.span.copy(cx),
            Edit::Paste => {
                if let Some(pasted) = cx.read_from_clipboard().and_then(|item| item.text()) {
                    self.replace(range, &pasted);
                }
            }
            Edit::SelectAll => self.span.select_all(),
        }
        self.touch(cx);
    }

    fn key<T: 'static>(&mut self, event: &KeyDownEvent, cx: &mut Context<T>) -> Keyed {
        let keys = &event.keystroke;
        if !self.span.navigate(&keys.key, &keys.modifiers) {
            let range = self.span.range.clone();
            let collapsed = range.is_empty();
            let many = self.lines == Lines::Many;
            match (keys.modifiers.secondary(), keys.key.as_str()) {
                (false, "enter") if many && keys.modifiers.shift => self.replace(range, "\n"),
                (false, "enter") if many => return Keyed::Submit,
                (false, "backspace") if collapsed => {
                    self.replace(self.span.previous(range.start)..range.end, "")
                }
                (false, "delete") if collapsed => {
                    self.replace(range.start..self.span.next(range.end), "")
                }
                (false, "backspace" | "delete") => self.replace(range, ""),
                (true, "c") => self.edit(Edit::Copy, cx),
                (true, "x") => self.edit(Edit::Cut, cx),
                (true, "v") => self.edit(Edit::Paste, cx),
                (true, "z") => self.undo(),
                _ => return Keyed::Ignored,
            }
        }
        self.touch(cx);
        Keyed::Handled
    }

    fn text_for(&self, range: Range<usize>, actual: &mut Option<Range<usize>>) -> Option<String> {
        let text = &self.span.text;
        let range = from_utf16(text, range.start)..from_utf16(text, range.end);
        actual.replace(to_utf16(text, range.start)..to_utf16(text, range.end));
        text.get(range).map(str::to_owned)
    }

    fn selection(&self) -> UTF16Selection {
        let text = &self.span.text;
        UTF16Selection {
            range: to_utf16(text, self.span.range.start)..to_utf16(text, self.span.range.end),
            reversed: self.span.reversed,
        }
    }

    fn target(&self, units: Option<Range<usize>>) -> Range<usize> {
        let Some(units) = units else {
            return self.marked.clone().unwrap_or(self.span.range.clone());
        };
        let start = from_utf16(&self.span.text, units.start);
        let end = from_utf16(&self.span.text, units.end);
        start.min(end)..start.max(end)
    }

    fn type_in<T: 'static>(
        &mut self,
        units: Option<Range<usize>>,
        text: &str,
        cx: &mut Context<T>,
    ) {
        self.replace(self.target(units), text);
        self.touch(cx);
    }

    fn compose<T: 'static>(
        &mut self,
        units: Option<Range<usize>>,
        text: &str,
        caret: Option<Range<usize>>,
        cx: &mut Context<T>,
    ) {
        let range = self.target(units);
        let start = range.start;
        self.replace(range, text);
        let marked = start..self.span.head();
        if let Some(caret) = caret {
            let within = self.span.text.get(marked.clone()).unwrap_or_default();
            let anchor = from_utf16(within, caret.start);
            let head = from_utf16(within, caret.end);
            self.span.select(start + anchor, false);
            self.span.select(start + head, true);
        }
        self.marked = (!marked.is_empty()).then_some(marked);
        self.touch(cx);
    }

    fn marked_units(&self) -> Option<Range<usize>> {
        let text = &self.span.text;
        let marked = self.marked.as_ref()?;
        Some(to_utf16(text, marked.start)..to_utf16(text, marked.end))
    }

    fn bounds_for(&self, units: Range<usize>) -> Option<Bounds<Pixels>> {
        let at = from_utf16(&self.span.text, units.start);
        let origin = self.span.layout.position_for_index(at)?;
        Some(Bounds::new(
            origin,
            size(px(CARET_WIDTH), self.span.layout.line_height()),
        ))
    }

    fn index_for(&self, point: Point<Pixels>) -> usize {
        to_utf16(&self.span.text, self.span.index_at(point))
    }
}

macro_rules! editable {
    ($field:ty) => {
        impl Editable for $field {
            fn editor(&mut self) -> &mut Editor {
                &mut self.editor
            }
        }

        impl Focusable for $field {
            fn focus_handle(&self, _: &App) -> FocusHandle {
                self.editor.focus.clone()
            }
        }

        impl Spanned for $field {
            const EDITS: &'static [Edit] = &[Edit::Cut, Edit::Copy, Edit::Paste, Edit::SelectAll];

            fn span(&mut self) -> &mut Span {
                &mut self.editor.span
            }

            fn edit(&mut self, edit: Edit, cx: &mut Context<Self>) {
                self.editor.edit(edit, cx);
            }
        }

        impl EntityInputHandler for $field {
            fn text_for_range(
                &mut self,
                range: Range<usize>,
                actual: &mut Option<Range<usize>>,
                _: &mut Window,
                _: &mut Context<Self>,
            ) -> Option<String> {
                self.editor.text_for(range, actual)
            }

            fn selected_text_range(
                &mut self,
                _: bool,
                _: &mut Window,
                _: &mut Context<Self>,
            ) -> Option<UTF16Selection> {
                Some(self.editor.selection())
            }

            fn marked_text_range(
                &self,
                _: &mut Window,
                _: &mut Context<Self>,
            ) -> Option<Range<usize>> {
                self.editor.marked_units()
            }

            fn unmark_text(&mut self, _: &mut Window, cx: &mut Context<Self>) {
                self.editor.marked = None;
                cx.notify();
            }

            fn replace_text_in_range(
                &mut self,
                range: Option<Range<usize>>,
                text: &str,
                _: &mut Window,
                cx: &mut Context<Self>,
            ) {
                self.editor.type_in(range, text, cx);
            }

            fn replace_and_mark_text_in_range(
                &mut self,
                range: Option<Range<usize>>,
                text: &str,
                caret: Option<Range<usize>>,
                _: &mut Window,
                cx: &mut Context<Self>,
            ) {
                self.editor.compose(range, text, caret, cx);
            }

            fn bounds_for_range(
                &mut self,
                range: Range<usize>,
                _: Bounds<Pixels>,
                _: &mut Window,
                _: &mut Context<Self>,
            ) -> Option<Bounds<Pixels>> {
                self.editor.bounds_for(range)
            }

            fn character_index_for_point(
                &mut self,
                point: Point<Pixels>,
                _: &mut Window,
                _: &mut Context<Self>,
            ) -> Option<usize> {
                Some(self.editor.index_for(point))
            }
        }
    };
}

fn reveal(scroll: &ScrollHandle, line: Bounds<Pixels>) {
    let view = scroll.bounds();
    let offset = scroll.offset();
    let y = if line.bottom() > view.bottom() {
        offset.y - (line.bottom() - view.bottom())
    } else if line.top() < view.top() {
        offset.y + (view.top() - line.top())
    } else {
        return;
    };
    scroll.set_offset(point(offset.x, y.min(px(0.0))));
}

fn typed<T: Editable + EntityInputHandler>(
    this: &mut T,
    scroll: Option<ScrollHandle>,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<T>,
) -> Div {
    let entity = cx.entity();
    let editor = this.editor();
    let focused = editor.focus.is_focused(window);
    let empty = editor.span.text.is_empty();
    let shown = if empty {
        editor.placeholder.clone()
    } else {
        SharedString::from(editor.span.text.clone())
    };
    let text = editor.span.styled(shown, editor.marked.clone(), theme);
    let lit = focused && editor.span.range.is_empty() && editor.caret_on();
    let revealing = std::mem::take(&mut editor.reveal);
    let layout = editor.span.layout.clone();
    let head = editor.span.head();
    let focus = editor.focus.clone();
    let color = ink(theme, T1);
    div()
        .relative()
        .flex_1()
        .text_color(ink(theme, if empty { T3 } else { T1 }))
        .child(text)
        .child(
            canvas(
                move |_, _, _| {
                    let at = layout.position_for_index(head)?;
                    Some(Bounds::new(at, size(px(CARET_WIDTH), layout.line_height())))
                },
                move |bounds, line, window, cx| {
                    window.handle_input(&focus, ElementInputHandler::new(bounds, entity), cx);
                    let Some(line) = line else {
                        return;
                    };
                    if lit {
                        let height = line.size.height * CARET_SCALE;
                        let top = line.origin.y + (line.size.height - height) / 2.0;
                        let caret =
                            Bounds::new(point(line.origin.x, top), size(line.size.width, height));
                        window.paint_quad(fill(caret, color));
                    }
                    if let Some(scroll) = scroll.filter(|_| revealing) {
                        reveal(&scroll, line);
                        window.request_animation_frame();
                    }
                },
            )
            .absolute()
            .size_full(),
        )
}

pub struct TextInput {
    editor: Editor,
}

editable!(TextInput);

impl TextInput {
    pub fn new(placeholder: SharedString, window: &mut Window, cx: &mut App) -> Entity<TextInput> {
        cx.new(|cx| TextInput {
            editor: Editor::new(placeholder, Lines::One, window, cx),
        })
    }

    pub fn text(&self) -> &str {
        &self.editor.span.text
    }

    fn key_down(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        match self.editor.key(event, cx) {
            Keyed::Handled => cx.stop_propagation(),
            Keyed::Ignored | Keyed::Submit => {}
        }
    }
}

impl Render for TextInput {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = typed(self, None, &theme, window, cx);
        let field = div()
            .id("text-input")
            .track_focus(&self.editor.focus)
            .flex()
            .items_center()
            .w(px(FIELD_WIDTH))
            .h(px(FIELD))
            .px(px(FIELD_PAD))
            .overflow_hidden()
            .whitespace_nowrap()
            .rounded(px(RADIUS_ROW))
            .bg(theme.color(ColorToken::FieldFill))
            .text_size(px(FONT_BODY))
            .on_key_down(cx.listener(Self::key_down))
            .child(body);
        edit_menu("text-input-menu", field, cx)
    }
}

type Submit = Rc<dyn Fn(&str, &mut Window, &mut App)>;

pub struct TextArea {
    editor: Editor,
    max_lines: usize,
    on_submit: Option<Submit>,
    scroll: ScrollHandle,
    bare: bool,
}

editable!(TextArea);

impl TextArea {
    pub fn new(placeholder: SharedString, window: &mut Window, cx: &mut Context<TextArea>) -> Self {
        TextArea {
            editor: Editor::new(placeholder, Lines::Many, window, cx),
            max_lines: TEXT_AREA_LINES,
            on_submit: None,
            scroll: ScrollHandle::new(),
            bare: false,
        }
    }

    pub fn bare(mut self) -> Self {
        self.bare = true;
        self
    }

    pub fn max_lines(mut self, lines: usize) -> Self {
        self.max_lines = lines.max(1);
        self
    }

    pub fn on_submit(mut self, on_submit: impl Fn(&str, &mut Window, &mut App) + 'static) -> Self {
        self.on_submit = Some(Rc::new(on_submit));
        self
    }

    pub fn text(&self) -> String {
        self.editor.span.text.clone()
    }

    pub fn clear(&mut self, cx: &mut Context<Self>) {
        let all = 0..self.editor.span.text.len();
        self.editor.replace(all, "");
        self.editor.touch(cx);
    }

    fn key_down(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        match self.editor.key(event, cx) {
            Keyed::Ignored => return,
            Keyed::Handled => {}
            Keyed::Submit => {
                if let Some(on_submit) = self.on_submit.clone() {
                    let text = self.text();
                    window.defer(cx, move |window, cx| on_submit(&text, window, cx));
                }
            }
        }
        cx.stop_propagation();
    }
}

impl Render for TextArea {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let scroll = self.scroll.clone();
        let body = typed(self, Some(scroll), &theme, window, cx);
        let tallest = TEXT_AREA_LINE * self.max_lines as f32;
        let field = div()
            .id("text-area")
            .track_focus(&self.editor.focus)
            .when(self.bare, |field| field.w_full())
            .when(!self.bare, |field| {
                field
                    .w(px(FIELD_WIDTH))
                    .px(px(FIELD_PAD))
                    .py(px(TEXT_AREA_PAD_Y))
                    .rounded(px(RADIUS_ROW))
                    .bg(theme.color(ColorToken::FieldFill))
            })
            .text_size(px(FONT_BODY))
            .line_height(px(TEXT_AREA_LINE))
            .on_key_down(cx.listener(Self::key_down))
            .child(
                div()
                    .id("text-area-scroll")
                    .track_scroll(&self.scroll)
                    .overflow_y_scroll()
                    .max_h(px(tallest))
                    .child(body),
            );
        edit_menu("text-area-menu", field, cx)
    }
}

pub struct SelectableText {
    span: Span,
    focus: FocusHandle,
}

impl SelectableText {
    pub fn new(text: SharedString, cx: &mut App) -> Entity<SelectableText> {
        cx.new(|cx| SelectableText {
            span: Span::new(text.to_string()),
            focus: cx.focus_handle(),
        })
    }

    fn key_down(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        let keys = &event.keystroke;
        let copy = keys.modifiers.secondary() && keys.key == "c";
        if copy {
            self.span.copy(cx);
        } else if !self.span.navigate(&keys.key, &keys.modifiers) {
            return;
        }
        cx.stop_propagation();
        cx.notify();
    }
}

impl Spanned for SelectableText {
    const EDITS: &'static [Edit] = &[Edit::Copy, Edit::SelectAll];

    fn span(&mut self) -> &mut Span {
        &mut self.span
    }

    fn edit(&mut self, edit: Edit, cx: &mut Context<Self>) {
        match edit {
            Edit::Copy => self.span.copy(cx),
            Edit::SelectAll => self.span.select_all(),
            Edit::Cut | Edit::Paste => {}
        }
    }
}

impl Focusable for SelectableText {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

impl Render for SelectableText {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let shown = SharedString::from(self.span.text.clone());
        let text = self.span.styled(shown, None, &theme);
        let body = div()
            .id("selectable-text")
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::key_down))
            .child(text);
        edit_menu("selectable-text-menu", body, cx)
    }
}
