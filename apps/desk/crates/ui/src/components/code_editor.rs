use std::cell::Cell;
use std::fmt::Display;
use std::ops::Range;
use std::rc::Rc;
use std::time::Instant;

use desk_core::buffer::Buffer;
use desk_core::syntax::{Kind, Syntax};
use gpui::{
    App, Bounds, ClipboardItem, Context, ElementInputHandler, EntityInputHandler, FocusHandle,
    Focusable, KeyDownEvent, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels,
    Point, ScrollStrategy, SharedString, Task, UTF16Selection, UniformListScrollHandle, Window,
    canvas, div, point, prelude::*, px, size,
};

use crate::components::code::{CodeLine, code_origin, code_place, code_shape, code_view};
use crate::components::form::{blinker, caret_shown};
use crate::components::size::{CARET_WIDTH, CODE_LINE};
use crate::live::ActiveTheme;
use crate::theme::ColorToken;

const TAB: &str = "    ";

pub fn syntax_token(kind: Kind) -> ColorToken {
    match kind {
        Kind::Keyword => ColorToken::SyntaxKeyword,
        Kind::String => ColorToken::SyntaxString,
        Kind::Number => ColorToken::SyntaxNumber,
        Kind::Comment => ColorToken::SyntaxComment,
        Kind::Function => ColorToken::SyntaxFunction,
        Kind::Type => ColorToken::SyntaxType,
        Kind::Variable => ColorToken::SyntaxVariable,
        Kind::Constant => ColorToken::SyntaxConstant,
        Kind::Operator => ColorToken::SyntaxOperator,
        Kind::Punctuation => ColorToken::SyntaxPunctuation,
    }
}

pub fn code_lines(buffer: &Buffer, syntax: &Syntax, rows: Range<usize>) -> Vec<CodeLine> {
    let rope = buffer.rope();
    let mut lines: Vec<CodeLine> = rows
        .clone()
        .map(|row| CodeLine {
            text: buffer.line(row).unwrap_or_default().into(),
            ..CodeLine::default()
        })
        .collect();
    let last_row = rows.end.saturating_sub(1);
    for span in syntax.spans(rows.clone()) {
        let (Ok(start), Ok(end), Ok(first), Ok(last)) = (
            rope.try_char_to_byte(span.chars.start),
            rope.try_char_to_byte(span.chars.end),
            buffer.char_to_line(span.chars.start),
            buffer.char_to_line(span.chars.end),
        ) else {
            continue;
        };
        for row in first.max(rows.start)..=last.min(last_row) {
            let (Some(line), Ok(base)) =
                (lines.get_mut(row - rows.start), rope.try_line_to_byte(row))
            else {
                continue;
            };
            let bytes = start.saturating_sub(base)..end.saturating_sub(base).min(line.text.len());
            if !bytes.is_empty() {
                line.runs.push((bytes, syntax_token(span.kind)));
            }
        }
    }
    lines
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Caret {
    anchor: usize,
    head: usize,
    goal: Option<usize>,
}

impl Caret {
    fn at(char: usize) -> Self {
        Caret {
            anchor: char,
            head: char,
            goal: None,
        }
    }

    fn range(self) -> Range<usize> {
        self.anchor.min(self.head)..self.anchor.max(self.head)
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Class {
    Space,
    Word,
    Mark,
}

fn class(ch: char) -> Class {
    if ch.is_whitespace() {
        Class::Space
    } else if ch.is_alphanumeric() || ch == '_' {
        Class::Word
    } else {
        Class::Mark
    }
}

fn chars_within(text: &str, units: usize) -> usize {
    let mut seen = 0;
    text.chars()
        .take_while(|ch| {
            let before = seen;
            seen += ch.len_utf16();
            before < units
        })
        .count()
}

fn byte_of(text: &str, column: usize) -> usize {
    text.char_indices()
        .nth(column)
        .map_or(text.len(), |(byte, _)| byte)
}

type Save = Rc<dyn Fn(&mut Buffer, &mut Window, &mut App)>;

pub struct CodeEditor {
    buffer: Buffer,
    syntax: Syntax,
    carets: Vec<Caret>,
    marked: Option<Range<usize>>,
    widest: usize,
    dragging: bool,
    failure: Option<SharedString>,
    scroll: UniformListScrollHandle,
    bounds: Rc<Cell<Bounds<Pixels>>>,
    focus: FocusHandle,
    on_save: Option<Save>,
    since: Instant,
    blink: Option<Task<()>>,
}

impl CodeEditor {
    pub fn new(buffer: Buffer, syntax: Syntax, cx: &mut Context<Self>) -> Self {
        let mut editor = CodeEditor {
            buffer,
            syntax,
            carets: vec![Caret::at(0)],
            marked: None,
            widest: 0,
            dragging: false,
            failure: None,
            scroll: UniformListScrollHandle::new(),
            bounds: Rc::default(),
            focus: cx.focus_handle(),
            on_save: None,
            since: Instant::now(),
            blink: None,
        };
        editor.widest = editor.widest_line();
        editor
    }

    pub fn on_save(mut self, save: impl Fn(&mut Buffer, &mut Window, &mut App) + 'static) -> Self {
        self.on_save = Some(Rc::new(save));
        self
    }

    pub fn buffer(&self) -> &Buffer {
        &self.buffer
    }

    pub fn syntax(&self) -> &Syntax {
        &self.syntax
    }

    pub fn carets(&self) -> impl Iterator<Item = (usize, usize)> + '_ {
        self.carets.iter().map(|caret| (caret.anchor, caret.head))
    }

    pub fn failure(&self) -> Option<&SharedString> {
        self.failure.as_ref()
    }

    fn char_at(&self, at: usize) -> Option<char> {
        self.buffer.rope().get_char(at)
    }

    fn before(&self, at: usize) -> usize {
        let crlf =
            at >= 2 && self.char_at(at - 1) == Some('\n') && self.char_at(at - 2) == Some('\r');
        if crlf { at - 2 } else { at.saturating_sub(1) }
    }

    fn after(&self, at: usize) -> usize {
        let crlf = self.char_at(at) == Some('\r') && self.char_at(at + 1) == Some('\n');
        (at + if crlf { 2 } else { 1 }).min(self.buffer.len_chars())
    }

    fn snap(&self, at: usize) -> usize {
        let at = at.min(self.buffer.len_chars());
        let inside = at > 0 && self.char_at(at - 1) == Some('\r') && self.char_at(at) == Some('\n');
        if inside { at - 1 } else { at }
    }

    fn word_right(&self, mut at: usize) -> usize {
        while self.char_at(at).is_some_and(char::is_whitespace) {
            at += 1;
        }
        let kind = self.char_at(at).map(class);
        while kind.is_some() && self.char_at(at).map(class) == kind {
            at += 1;
        }
        at
    }

    fn word_left(&self, mut at: usize) -> usize {
        while at > 0 && self.char_at(at - 1).is_some_and(char::is_whitespace) {
            at -= 1;
        }
        let kind = at
            .checked_sub(1)
            .and_then(|before| self.char_at(before))
            .map(class);
        while at > 0 && kind.is_some() && self.char_at(at - 1).map(class) == kind {
            at -= 1;
        }
        at
    }

    fn line_len(&self, row: usize) -> usize {
        self.buffer.line(row).map_or(0, |line| line.chars().count())
    }

    fn widest_line(&self) -> usize {
        let rope = self.buffer.rope();
        (0..self.buffer.line_count())
            .max_by_key(|row| rope.get_line(*row).map_or(0, |line| line.len_chars()))
            .unwrap_or(0)
    }

    fn moved(&self, caret: Caret, key: &str, word: bool) -> Option<(usize, Option<usize>)> {
        let head = caret.head;
        let row = self.buffer.char_to_line(head).ok()?;
        let start = self.buffer.line_to_char(row).ok()?;
        Some(match (key, word) {
            ("left", false) => (self.before(head), None),
            ("right", false) => (self.after(head), None),
            ("left", true) => (self.word_left(head), None),
            ("right", true) => (self.word_right(head), None),
            ("home", false) => (start, None),
            ("end", false) => (start + self.line_len(row), None),
            ("home", true) => (0, None),
            ("end", true) => (self.buffer.len_chars(), None),
            ("up" | "down", _) => {
                let goal = caret.goal.unwrap_or(head - start);
                let next = match key {
                    "up" => row.checked_sub(1),
                    _ => Some(row + 1).filter(|next| *next < self.buffer.line_count()),
                };
                let to = match next {
                    Some(next) => {
                        self.buffer.line_to_char(next).ok()? + goal.min(self.line_len(next))
                    }
                    None if key == "up" => 0,
                    None => self.buffer.len_chars(),
                };
                (to, Some(goal))
            }
            _ => return None,
        })
    }

    fn merge(&mut self) {
        let mut kept: Vec<Caret> = Vec::with_capacity(self.carets.len());
        for caret in self.carets.drain(..).rev() {
            let range = caret.range();
            let clash = kept.iter().any(|other| {
                let taken = other.range();
                other.head == caret.head || (range.start < taken.end && taken.start < range.end)
            });
            if !clash {
                kept.push(caret);
            }
        }
        kept.reverse();
        self.carets = kept;
    }

    fn travel(&mut self, key: &str, word: bool, extend: bool) {
        let moved: Option<Vec<Caret>> = self
            .carets
            .iter()
            .map(|caret| {
                let range = caret.range();
                let collapse = !extend && !word && !range.is_empty();
                let (head, goal) = match key {
                    "left" if collapse => (range.start, None),
                    "right" if collapse => (range.end, None),
                    _ => self.moved(*caret, key, word)?,
                };
                let anchor = if extend { caret.anchor } else { head };
                Some(Caret { anchor, head, goal })
            })
            .collect();
        if let Some(moved) = moved {
            self.carets = moved;
            self.merge();
        }
    }

    fn stack(&mut self, key: &str) {
        let Some(last) = self.carets.last().copied() else {
            return;
        };
        let Some((head, goal)) = self.moved(last, key, false) else {
            return;
        };
        if self.buffer.char_to_line(head).ok() == self.buffer.char_to_line(last.head).ok() {
            return;
        }
        self.carets.push(Caret {
            goal,
            ..Caret::at(head)
        });
        self.merge();
    }

    fn report(&mut self, result: Result<(), impl Display>) {
        if let Err(error) = result {
            self.failure = Some(error.to_string().into());
        }
    }

    fn settle(&mut self, lines: usize) {
        let synced = self.syntax.sync(&mut self.buffer);
        self.report(synced);
        self.carets = self
            .buffer
            .selections()
            .iter()
            .map(|range| Caret {
                anchor: range.start,
                head: range.end,
                goal: None,
            })
            .collect();
        let rope = self.buffer.rope();
        let width = |row: &usize| rope.get_line(*row).map_or(0, |line| line.len_chars());
        self.widest = if self.buffer.line_count() == lines {
            self.carets
                .iter()
                .filter_map(|caret| self.buffer.char_to_line(caret.head).ok())
                .chain([self.widest])
                .max_by_key(width)
                .unwrap_or(self.widest)
        } else {
            self.widest_line()
        };
    }

    fn apply(&mut self, ranges: &[Range<usize>], text: &str) {
        let ranges: Vec<Range<usize>> = ranges
            .iter()
            .filter(|range| !range.is_empty() || !text.is_empty())
            .cloned()
            .collect();
        if ranges.is_empty() {
            return;
        }
        let lines = self.buffer.line_count();
        match self.buffer.edit(&ranges, text) {
            Ok(()) => self.settle(lines),
            Err(error) => self.report(Err(error)),
        }
    }

    fn selections(&self) -> Vec<Range<usize>> {
        self.carets.iter().map(|caret| caret.range()).collect()
    }

    fn insert(&mut self, text: &str) {
        self.marked = None;
        self.apply(&self.selections(), text);
    }

    fn erase(&mut self, back: bool) {
        let ranges: Vec<Range<usize>> = self
            .carets
            .iter()
            .map(|caret| match (caret.range(), back) {
                (range, _) if !range.is_empty() => range,
                (_, true) => self.before(caret.head)..caret.head,
                (_, false) => caret.head..self.after(caret.head),
            })
            .collect();
        self.apply(&ranges, "");
    }

    fn history(&mut self, back: bool) {
        let lines = self.buffer.line_count();
        let moved = if back {
            self.buffer.undo()
        } else {
            self.buffer.redo()
        };
        if moved {
            self.settle(lines);
        }
    }

    fn copy(&self, cx: &mut App) {
        let rope = self.buffer.rope();
        let parts: Vec<String> = self
            .carets
            .iter()
            .filter_map(|caret| rope.get_slice(caret.range()))
            .map(|slice| slice.to_string())
            .filter(|part| !part.is_empty())
            .collect();
        if !parts.is_empty() {
            cx.write_to_clipboard(ClipboardItem::new_string(parts.join("\n")));
        }
    }

    fn paste(&mut self, cx: &mut App) {
        if let Some(text) = cx.read_from_clipboard().and_then(|item| item.text()) {
            self.insert(&text);
        }
    }

    fn touch(&mut self, cx: &mut Context<Self>) {
        self.since = Instant::now();
        self.blink = None;
        cx.notify();
    }

    fn reveal(&self) {
        if let Some(caret) = self.carets.last()
            && let Ok(row) = self.buffer.char_to_line(caret.head)
        {
            self.scroll.scroll_to_item(row, ScrollStrategy::Nearest);
        }
    }

    fn key_down(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let keys = &event.keystroke;
        let held = keys.modifiers;
        let key = keys.key.as_str();
        let command = held.secondary();
        match (command, key) {
            (true, "up" | "down") if held.alt => self.stack(key),
            (_, "left" | "right" | "up" | "down" | "home" | "end") => {
                self.travel(key, command, held.shift)
            }
            (false, "backspace") => self.erase(true),
            (false, "delete") => self.erase(false),
            (false, "enter") => self.insert("\n"),
            (false, "tab") => self.insert(TAB),
            (false, "escape") => {
                self.carets = self
                    .carets
                    .last()
                    .map(|caret| Caret::at(caret.head))
                    .into_iter()
                    .collect();
            }
            (true, "z") => self.history(!held.shift),
            (true, "y") => self.history(false),
            (true, "a") => {
                self.carets = vec![Caret {
                    head: self.buffer.len_chars(),
                    ..Caret::at(0)
                }]
            }
            (true, "c") => self.copy(cx),
            (true, "x") => {
                self.copy(cx);
                self.insert("");
            }
            (true, "v") => self.paste(cx),
            (true, "s") => {
                if let Some(save) = self.on_save.clone() {
                    save(&mut self.buffer, window, cx);
                }
            }
            _ => return,
        }
        cx.stop_propagation();
        self.reveal();
        self.touch(cx);
    }

    fn char_at_point(&self, position: Point<Pixels>, window: &Window, cx: &App) -> Option<usize> {
        let (row, x) = code_place(self.bounds.get(), &self.scroll, position);
        let row = row.min(self.buffer.line_count().saturating_sub(1));
        let text: SharedString = self.buffer.line(row)?.into();
        let byte = code_shape(text.clone(), window, &ActiveTheme::theme(cx)).closest_index_for_x(x);
        let column = text.get(..byte).map_or(0, |before| before.chars().count());
        Some(self.buffer.line_to_char(row).ok()? + column)
    }

    fn mouse_down(&mut self, event: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        window.focus(&self.focus, cx);
        let Some(at) = self.char_at_point(event.position, window, cx) else {
            return;
        };
        let held = event.modifiers;
        match self.carets.last_mut() {
            Some(last) if held.shift && !held.alt => {
                last.head = at;
                last.goal = None;
            }
            _ if held.alt => self.carets.push(Caret::at(at)),
            _ => self.carets = vec![Caret::at(at)],
        }
        self.marked = None;
        self.dragging = true;
        self.merge();
        self.touch(cx);
    }

    fn mouse_move(&mut self, event: &MouseMoveEvent, window: &mut Window, cx: &mut Context<Self>) {
        if !self.dragging || !event.dragging() {
            return;
        }
        let Some(at) = self.char_at_point(event.position, window, cx) else {
            return;
        };
        if let Some(last) = self.carets.last_mut()
            && last.head != at
        {
            last.head = at;
            last.goal = None;
            self.merge();
            self.touch(cx);
        }
    }

    fn release(&mut self, _: &MouseUpEvent, _: &mut Window, _: &mut Context<Self>) {
        self.dragging = false;
    }

    fn rows(&self, rows: Range<usize>, caret_on: bool) -> Vec<CodeLine> {
        let mut lines = code_lines(&self.buffer, &self.syntax, rows.clone());
        for (row, line) in rows.zip(lines.iter_mut()) {
            let Ok(start) = self.buffer.line_to_char(row) else {
                continue;
            };
            let text = line.text.clone();
            let end = start + text.chars().count();
            for caret in &self.carets {
                if caret_on && (start..=end).contains(&caret.head) {
                    line.carets.push(byte_of(&text, caret.head - start));
                }
                let range = caret.range();
                let (from, to) = (range.start.max(start), range.end.min(end));
                if from < to {
                    line.selected
                        .push(byte_of(&text, from - start)..byte_of(&text, to - start));
                }
            }
        }
        lines
    }

    fn chars(&self, units: Range<usize>) -> Range<usize> {
        let rope = self.buffer.rope();
        let char_of = |unit: usize| self.snap(rope.utf16_cu_to_char(unit.min(rope.len_utf16_cu())));
        let (start, end) = (char_of(units.start), char_of(units.end));
        start.min(end)..start.max(end)
    }

    fn units(&self, at: usize) -> usize {
        let rope = self.buffer.rope();
        rope.char_to_utf16_cu(at.min(rope.len_chars()))
    }
}

impl Focusable for CodeEditor {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

impl EntityInputHandler for CodeEditor {
    fn text_for_range(
        &mut self,
        units: Range<usize>,
        actual: &mut Option<Range<usize>>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<String> {
        let chars = self.chars(units);
        actual.replace(self.units(chars.start)..self.units(chars.end));
        self.buffer
            .rope()
            .get_slice(chars)
            .map(|slice| slice.to_string())
    }

    fn selected_text_range(
        &mut self,
        _: bool,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<UTF16Selection> {
        let caret = self.carets.last()?;
        let range = caret.range();
        Some(UTF16Selection {
            range: self.units(range.start)..self.units(range.end),
            reversed: caret.head < caret.anchor,
        })
    }

    fn marked_text_range(&self, _: &mut Window, _: &mut Context<Self>) -> Option<Range<usize>> {
        let marked = self.marked.as_ref()?;
        Some(self.units(marked.start)..self.units(marked.end))
    }

    fn unmark_text(&mut self, _: &mut Window, cx: &mut Context<Self>) {
        self.marked = None;
        cx.notify();
    }

    fn replace_text_in_range(
        &mut self,
        units: Option<Range<usize>>,
        text: &str,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let targets = match (units, self.marked.take()) {
            (Some(units), _) => vec![self.chars(units)],
            (None, Some(marked)) => vec![marked],
            (None, None) => self.selections(),
        };
        self.apply(&targets, text);
        self.reveal();
        self.touch(cx);
    }

    fn replace_and_mark_text_in_range(
        &mut self,
        units: Option<Range<usize>>,
        text: &str,
        caret: Option<Range<usize>>,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let target = match (units, self.marked.take()) {
            (Some(units), _) => self.chars(units),
            (None, Some(marked)) => marked,
            (None, None) => self.carets.last().map_or(0..0, |caret| caret.range()),
        };
        self.apply(std::slice::from_ref(&target), text);
        let start = target.start;
        let typed = text.chars().count();
        self.marked = (typed > 0).then_some(start..start + typed);
        if let Some(caret) = caret {
            self.carets = vec![Caret {
                anchor: start + chars_within(text, caret.start),
                head: start + chars_within(text, caret.end),
                goal: None,
            }];
        }
        self.reveal();
        self.touch(cx);
    }

    fn bounds_for_range(
        &mut self,
        units: Range<usize>,
        element: Bounds<Pixels>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Option<Bounds<Pixels>> {
        let at = self.chars(units).start;
        let row = self.buffer.char_to_line(at).ok()?;
        let column = at - self.buffer.line_to_char(row).ok()?;
        let text: SharedString = self.buffer.line(row)?.into();
        let byte = byte_of(&text, column);
        let x = code_shape(text, window, &ActiveTheme::theme(cx)).x_for_index(byte);
        let origin = code_origin(element, &self.scroll, row);
        Some(Bounds::new(
            point(origin.x + x, origin.y),
            size(px(CARET_WIDTH), px(CODE_LINE)),
        ))
    }

    fn character_index_for_point(
        &mut self,
        position: Point<Pixels>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Option<usize> {
        let at = self.char_at_point(position, window, cx)?;
        Some(self.units(at))
    }
}

impl Render for CodeEditor {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let focused = self.focus.is_focused(window);
        match (focused, self.blink.is_some()) {
            (true, false) => self.blink = Some(blinker(cx)),
            (false, true) => self.blink = None,
            _ => {}
        }
        let shown = focused && caret_shown(self.since);
        let entity = cx.entity();
        let source = entity.clone();
        let bounds = self.bounds.clone();
        let focus = self.focus.clone();
        let rows = code_view(
            "code-editor-rows",
            self.buffer.line_count(),
            &self.scroll,
            move |rows, cx| source.update(cx, |editor, _| editor.rows(rows, shown)),
        )
        .widest(self.widest);
        div()
            .id("code-editor")
            .relative()
            .size_full()
            .track_focus(&self.focus)
            .cursor_text()
            .on_key_down(cx.listener(Self::key_down))
            .on_mouse_down(MouseButton::Left, cx.listener(Self::mouse_down))
            .on_mouse_move(cx.listener(Self::mouse_move))
            .on_mouse_up(MouseButton::Left, cx.listener(Self::release))
            .on_mouse_up_out(MouseButton::Left, cx.listener(Self::release))
            .child(rows)
            .child(
                canvas(
                    |_, _, _| {},
                    move |area, _, window, cx| {
                        bounds.set(area);
                        window.handle_input(&focus, ElementInputHandler::new(area, entity), cx);
                    },
                )
                .absolute()
                .top_0()
                .left_0()
                .size_full(),
            )
    }
}
