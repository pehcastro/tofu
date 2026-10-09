use std::cell::Cell;
use std::fmt::Display;
use std::fs;
use std::ops::Range;
use std::path::Path;
use std::rc::Rc;
use std::time::Instant;

use desk_core::buffer::Buffer;
use desk_core::syntax::{Kind, Span, Syntax};
use gpui::{
    App, Bounds, ClipboardItem, Context, ElementInputHandler, Entity, EntityInputHandler,
    FocusHandle, Focusable, KeyDownEvent, MouseButton, MouseDownEvent, MouseMoveEvent,
    MouseUpEvent, Pixels, Point, ScrollStrategy, SharedString, Task, UTF16Selection,
    UniformListScrollHandle, Window, canvas, div, point, prelude::*, px, size,
};

use crate::components::chat::fail;
use crate::components::code::{
    CodeLine, Marks, Wrap, code_origin, code_place, code_shape, code_view, wrap_width,
};
use crate::components::find::{FindBar, find_ranges};
use crate::components::form::{blinker, caret_shown};
use crate::components::size::{CARET_WIDTH, CODE_LINE, NUMBER_COLUMN};
use crate::live::ActiveTheme;
use crate::theme::ColorToken;

const TAB: &str = "    ";
const FAILURE_INSET: f32 = 12.0;
pub const READ_ONLY_BYTES: u64 = 1024 * 1024;
const REFUSED_BYTES: u64 = 64 * 1024 * 1024;
const LONG_LINE_BYTES: usize = 64 * 1024;
const SNIFF_BYTES: usize = 8192;
const CONTROL_SHARE: f32 = 0.3;
const PROSE: [&str; 4] = ["md", "mdx", "txt", "rst"];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum FileKind {
    Text,
    Large(u64),
    Binary(u64),
}

pub enum Highlight {
    Plain,
    Tree(Syntax),
}

impl Highlight {
    pub fn spans(&self, rows: Range<usize>) -> Vec<Span> {
        match self {
            Highlight::Plain => Vec::new(),
            Highlight::Tree(syntax) => syntax.spans(rows),
        }
    }
}

fn binary(head: &[u8]) -> bool {
    let control = head
        .iter()
        .filter(|byte| byte.is_ascii_control() && !matches!(byte, b'\t' | b'\n' | b'\r' | 0x0c))
        .count();
    head.contains(&0) || !head.is_empty() && control as f32 / head.len() as f32 > CONTROL_SHARE
}

fn cut(text: &str, most: usize) -> &str {
    let end = (0..=most.min(text.len()))
        .rev()
        .find(|at| text.is_char_boundary(*at))
        .unwrap_or(0);
    text.get(..end).unwrap_or_default()
}

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
        Kind::Attribute => ColorToken::SyntaxAttribute,
        Kind::Boolean => ColorToken::SyntaxBoolean,
        Kind::CommentDoc => ColorToken::SyntaxCommentDoc,
        Kind::Constructor => ColorToken::SyntaxConstructor,
        Kind::Embedded => ColorToken::SyntaxEmbedded,
        Kind::Emphasis => ColorToken::SyntaxEmphasis,
        Kind::EmphasisStrong => ColorToken::SyntaxEmphasisStrong,
        Kind::Enum => ColorToken::SyntaxEnum,
        Kind::Label => ColorToken::SyntaxLabel,
        Kind::LinkText => ColorToken::SyntaxLinkText,
        Kind::LinkUri => ColorToken::SyntaxLinkUri,
        Kind::Namespace => ColorToken::SyntaxNamespace,
        Kind::Preproc => ColorToken::SyntaxPreproc,
        Kind::Property => ColorToken::SyntaxProperty,
        Kind::PunctuationBracket => ColorToken::SyntaxPunctuationBracket,
        Kind::PunctuationDelimiter => ColorToken::SyntaxPunctuationDelimiter,
        Kind::PunctuationListMarker => ColorToken::SyntaxPunctuationListMarker,
        Kind::PunctuationMarkup => ColorToken::SyntaxPunctuationMarkup,
        Kind::PunctuationSpecial => ColorToken::SyntaxPunctuationSpecial,
        Kind::Selector => ColorToken::SyntaxSelector,
        Kind::SelectorPseudo => ColorToken::SyntaxSelectorPseudo,
        Kind::StringEscape => ColorToken::SyntaxStringEscape,
        Kind::StringRegex => ColorToken::SyntaxStringRegex,
        Kind::StringSpecial => ColorToken::SyntaxStringSpecial,
        Kind::StringSpecialSymbol => ColorToken::SyntaxStringSpecialSymbol,
        Kind::Tag => ColorToken::SyntaxTag,
        Kind::TextLiteral => ColorToken::SyntaxTextLiteral,
        Kind::Title => ColorToken::SyntaxTitle,
        Kind::VariableParameter => ColorToken::SyntaxVariableParameter,
        Kind::VariableSpecial => ColorToken::SyntaxVariableSpecial,
        Kind::Variant => ColorToken::SyntaxVariant,
    }
}

pub fn code_lines(buffer: &Buffer, syntax: &Syntax, rows: Range<usize>) -> Vec<CodeLine> {
    coloured_lines(buffer, syntax.spans(rows.clone()), rows)
        .into_iter()
        .map(|(line, _)| line)
        .collect()
}

fn prose(path: &Path) -> bool {
    path.extension()
        .and_then(|extension| extension.to_str())
        .is_none_or(|extension| {
            PROSE
                .iter()
                .any(|prose| extension.eq_ignore_ascii_case(prose))
        })
}

struct Drawn {
    text: String,
    starts: Vec<usize>,
}

impl Drawn {
    fn new(line: &str) -> Self {
        let mut text = String::new();
        let mut starts = Vec::new();
        let mut columns = 0;
        for ch in cut(line, LONG_LINE_BYTES).chars() {
            starts.push(text.len());
            let width = match ch {
                '\t' => TAB.len() - columns % TAB.len(),
                _ => 1,
            };
            match ch {
                '\t' => text.extend(std::iter::repeat_n(' ', width)),
                _ => text.push(ch),
            }
            columns += width;
        }
        starts.push(text.len());
        Self { text, starts }
    }

    fn columns(&self) -> usize {
        self.starts.len().saturating_sub(1)
    }

    fn byte(&self, column: usize) -> usize {
        self.starts.get(column).copied().unwrap_or(self.text.len())
    }

    fn column(&self, byte: usize) -> usize {
        let after = self.starts.partition_point(|start| *start < byte);
        match after.checked_sub(1) {
            Some(before) if byte - self.byte(before) < self.byte(after).saturating_sub(byte) => {
                before
            }
            _ => after.min(self.columns()),
        }
    }
}

fn coloured_lines(buffer: &Buffer, spans: Vec<Span>, rows: Range<usize>) -> Vec<(CodeLine, Drawn)> {
    let rope = buffer.rope();
    let mut long = Vec::new();
    let mut lines: Vec<(CodeLine, Drawn)> = rows
        .clone()
        .map(|row| {
            let full = buffer.line(row).unwrap_or_default();
            long.push(full.len() > LONG_LINE_BYTES);
            let drawn = Drawn::new(&full);
            let line = CodeLine {
                text: drawn.text.clone().into(),
                ..CodeLine::default()
            };
            (line, drawn)
        })
        .collect();
    let last_row = rows.end.saturating_sub(1);
    for span in spans {
        let (Ok(first), Ok(last)) = (
            buffer.char_to_line(span.chars.start),
            buffer.char_to_line(span.chars.end),
        ) else {
            continue;
        };
        for row in first.max(rows.start)..=last.min(last_row) {
            let at = row - rows.start;
            let (Some((line, drawn)), Ok(base), Some(false)) =
                (lines.get_mut(at), rope.try_line_to_char(row), long.get(at))
            else {
                continue;
            };
            let bytes = drawn.byte(span.chars.start.saturating_sub(base))
                ..drawn.byte(span.chars.end.saturating_sub(base));
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

fn follow(marks: &Marks, line: usize, line_start: bool, removed: &str, inserted: &str) -> Marks {
    let whole_lines = |text: &str| text.is_empty() || text.ends_with('\n');
    let whole = line_start && whole_lines(removed) && whole_lines(inserted);
    let first = line + usize::from(!whole);
    let gone = removed.matches('\n').count();
    let added = inserted.matches('\n').count();
    marks
        .iter()
        .filter_map(|(row, mark)| {
            let row = match *row {
                row if row < first => row,
                row if row < first + gone => return None,
                row => row - gone + added,
            };
            Some((row, mark.clone()))
        })
        .collect()
}

type Save = Rc<dyn Fn(&mut Buffer, &mut Window, &mut App) -> Result<(), String>>;

pub struct CodeEditor {
    buffer: Buffer,
    syntax: Highlight,
    kind: FileKind,
    carets: Vec<Caret>,
    marked: Option<Range<usize>>,
    widest: usize,
    dragging: bool,
    failure: Option<(&'static str, SharedString)>,
    marks: Rc<Marks>,
    scroll: UniformListScrollHandle,
    bounds: Rc<Cell<Bounds<Pixels>>>,
    focus: FocusHandle,
    on_save: Option<Save>,
    since: Instant,
    blink: Option<Task<()>>,
    bar: Option<Entity<FindBar>>,
    query: String,
    found: Vec<Range<usize>>,
    current: usize,
    edited_since_found: bool,
    wrapping: bool,
    wrap: Rc<Wrap>,
    wrap_stale: bool,
}

impl CodeEditor {
    pub fn new(buffer: Buffer, syntax: Syntax, cx: &mut App) -> Self {
        Self::shown(buffer, Highlight::Tree(syntax), FileKind::Text, cx)
    }

    fn shown(buffer: Buffer, syntax: Highlight, kind: FileKind, cx: &mut App) -> Self {
        let mut editor = CodeEditor {
            buffer,
            syntax,
            kind,
            carets: vec![Caret::at(0)],
            marked: None,
            widest: 0,
            dragging: false,
            failure: None,
            marks: Rc::default(),
            scroll: UniformListScrollHandle::new(),
            bounds: Rc::default(),
            focus: cx.focus_handle(),
            on_save: None,
            since: Instant::now(),
            blink: None,
            bar: None,
            query: String::new(),
            found: Vec::new(),
            current: 0,
            edited_since_found: false,
            wrapping: false,
            wrap: Rc::default(),
            wrap_stale: true,
        };
        editor.widest = editor.widest_line();
        editor
    }

    pub fn open(path: &Path, cx: &mut App) -> Result<Self, String> {
        let shown = path.display();
        let size = fs::metadata(path)
            .map_err(|error| format!("{shown}: {error}"))?
            .len();
        if size > REFUSED_BYTES {
            return Err(format!(
                "{shown} is {} MB, over the {} MB the editor opens",
                size / READ_ONLY_BYTES,
                REFUSED_BYTES / READ_ONLY_BYTES
            ));
        }
        let bytes = fs::read(path).map_err(|error| format!("{shown}: {error}"))?;
        if binary(bytes.get(..SNIFF_BYTES).unwrap_or(&bytes)) {
            let note = format!("Binary file, {size} bytes, not shown.");
            let buffer = Buffer::from_text(&note);
            return Ok(Self::shown(
                buffer,
                Highlight::Plain,
                FileKind::Binary(size),
                cx,
            ));
        }
        let buffer = Buffer::from_text(&String::from_utf8_lossy(&bytes));
        if size > READ_ONLY_BYTES {
            return Ok(Self::shown(
                buffer,
                Highlight::Plain,
                FileKind::Large(size),
                cx,
            ));
        }
        let syntax = match Syntax::for_path(path, &buffer) {
            Some(syntax) => {
                let syntax = syntax.map_err(|error| error.to_string())?;
                eprintln!("desk: syntax {shown} {}", syntax.report());
                Highlight::Tree(syntax)
            }
            None => Highlight::Plain,
        };
        let mut editor = Self::shown(buffer, syntax, FileKind::Text, cx);
        editor.wrapping = prose(path);
        Ok(editor)
    }

    pub fn kind(&self) -> FileKind {
        self.kind
    }

    fn read_only(&self) -> bool {
        self.kind != FileKind::Text
    }

    pub fn on_save(
        mut self,
        save: impl Fn(&mut Buffer, &mut Window, &mut App) -> Result<(), String> + 'static,
    ) -> Self {
        self.on_save = Some(Rc::new(save));
        self
    }

    pub fn marks(&self) -> &Marks {
        &self.marks
    }

    pub fn set_marks(&mut self, marks: Marks) {
        self.marks = Rc::new(marks);
    }

    pub fn buffer(&self) -> &Buffer {
        &self.buffer
    }

    pub fn syntax(&self) -> &Highlight {
        &self.syntax
    }

    pub fn carets(&self) -> impl Iterator<Item = (usize, usize)> + '_ {
        self.carets.iter().map(|caret| (caret.anchor, caret.head))
    }

    pub fn failure(&self) -> Option<(&'static str, &SharedString)> {
        self.failure.as_ref().map(|(what, error)| (*what, error))
    }

    pub fn finding(&self) -> Option<(&str, usize, usize)> {
        let shown = self.current + usize::from(!self.found.is_empty());
        (!self.query.is_empty()).then_some((self.query.as_str(), shown, self.found.len()))
    }

    pub fn find(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let bar = self
            .bar
            .get_or_insert_with(|| {
                let bar = FindBar::new(window, cx);
                let changed = cx.listener(|editor, (query, at): &(String, usize), _, cx| {
                    editor.found(query, *at, cx);
                });
                let closed = cx.listener(|editor, _: &(), _, cx| editor.unfound(cx));
                bar.update(cx, |bar, _| {
                    bar.on_change(move |query, at, window, cx| {
                        changed(&(query.to_owned(), at), window, cx);
                    });
                    bar.on_close(move |window, cx| closed(&(), window, cx));
                });
                bar
            })
            .clone();
        bar.update(cx, |bar, cx| bar.open(window, cx));
        cx.notify();
    }

    fn search(&mut self, cx: &mut Context<Self>) {
        let text = self.buffer.text();
        let rope = self.buffer.rope();
        self.found = find_ranges(&text, &self.query)
            .into_iter()
            .filter_map(|bytes| {
                Some(
                    rope.try_byte_to_char(bytes.start).ok()?
                        ..rope.try_byte_to_char(bytes.end).ok()?,
                )
            })
            .collect();
        if self.current >= self.found.len() {
            self.current = 0;
        }
        self.edited_since_found = false;
        let total = self.found.len();
        if let Some(bar) = &self.bar {
            bar.update(cx, |bar, cx| bar.set_total(total, cx));
        }
    }

    fn found(&mut self, query: &str, at: usize, cx: &mut Context<Self>) {
        query.clone_into(&mut self.query);
        self.current = at;
        self.search(cx);
        if let Some(found) = self.found.get(self.current) {
            self.carets = vec![Caret {
                anchor: found.start,
                head: found.end,
                goal: None,
            }];
            self.reveal();
        }
        cx.notify();
    }

    fn unfound(&mut self, cx: &mut Context<Self>) {
        self.query.clear();
        self.found.clear();
        self.current = 0;
        cx.notify();
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

    fn drawn(&self, line: usize) -> Drawn {
        Drawn::new(&self.buffer.line(line).unwrap_or_default())
    }

    fn wrapped(&self) -> Option<&Wrap> {
        (self.wrapping && self.wrap.rows() > 0).then_some(&*self.wrap)
    }

    fn rewrap(&mut self, cx: &App) {
        let width = wrap_width(self.bounds.get());
        let fresh = !self.wrap_stale && width == self.wrap.width();
        if !self.wrapping || width <= px(0.0) || fresh {
            return;
        }
        let texts = (0..self.buffer.line_count()).map(|line| self.drawn(line).text);
        let theme = ActiveTheme::theme(cx);
        self.wrap = Rc::new(Wrap::new(&self.wrap, texts, width, &theme, cx));
        self.wrap_stale = false;
    }

    fn toggle_wrap(&mut self) {
        self.wrapping = !self.wrapping;
        self.wrap_stale = true;
        for caret in &mut self.carets {
            caret.goal = None;
        }
        let base = self.scroll.0.borrow().base_handle.clone();
        base.set_offset(point(px(0.0), base.offset().y));
        let state = if self.wrapping { "on" } else { "off" };
        eprintln!("desk: editor wrap {state}");
    }

    fn visual(&self, at: usize, wrap: &Wrap) -> Option<(usize, usize)> {
        let line = self.buffer.char_to_line(at).ok()?;
        let drawn = self.drawn(line);
        let byte = drawn.byte(at - self.buffer.line_to_char(line).ok()?);
        let row = wrap.row_of(line, byte);
        let within = wrap.piece(line, wrap.locate(row).1, drawn.text.len());
        Some((row, drawn.column(byte) - drawn.column(within.start)))
    }

    fn visual_step(&self, caret: Caret, up: bool, wrap: &Wrap) -> Option<(usize, Option<usize>)> {
        let (row, column) = self.visual(caret.head, wrap)?;
        let goal = caret.goal.unwrap_or(column);
        let next = match up {
            true => row.checked_sub(1),
            false => Some(row + 1).filter(|next| *next < wrap.rows()),
        };
        let Some(next) = next else {
            let edge = if up { 0 } else { self.buffer.len_chars() };
            return Some((edge, Some(goal)));
        };
        let (line, piece) = wrap.locate(next);
        let drawn = self.drawn(line);
        let within = wrap.piece(line, piece, drawn.text.len());
        let before = drawn.column(within.start);
        let chars = drawn.column(within.end) - before;
        let most = match piece + 1 == wrap.pieces(line) {
            true => chars,
            false => chars.saturating_sub(1),
        };
        let start = self.buffer.line_to_char(line).ok()?;
        Some((start + before + goal.min(most), Some(goal)))
    }

    fn moved(&self, caret: Caret, key: &str, word: bool) -> Option<(usize, Option<usize>)> {
        let head = caret.head;
        let row = self.buffer.char_to_line(head).ok()?;
        let start = self.buffer.line_to_char(row).ok()?;
        if let ("up" | "down", Some(wrap)) = (key, self.wrapped()) {
            return self.visual_step(caret, key == "up", wrap);
        }
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

    fn report(&mut self, what: &'static str, result: Result<(), impl Display>) {
        if let Err(error) = result {
            self.failure = Some((what, error.to_string().into()));
        }
    }

    fn settle(&mut self, lines: usize) {
        self.wrap_stale = true;
        if let Highlight::Tree(syntax) = &mut self.syntax {
            let synced = syntax.sync(&mut self.buffer);
            self.report("Could not colour", synced);
        }
        self.edited_since_found = !self.query.is_empty();
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
        if ranges.is_empty() || self.read_only() {
            return;
        }
        let lines = self.buffer.line_count();
        let marks = self.followed(&ranges, text);
        match self.buffer.edit(&ranges, text) {
            Ok(()) => {
                self.marks = marks;
                self.settle(lines);
            }
            Err(error) => self.report("Could not edit", Err(error)),
        }
    }

    fn followed(&self, ranges: &[Range<usize>], text: &str) -> Rc<Marks> {
        if self.marks.is_empty() {
            return self.marks.clone();
        }
        let mut ranges = ranges.to_vec();
        ranges.sort_by_key(|range| std::cmp::Reverse(range.start));
        let mut marks = (*self.marks).clone();
        for range in ranges {
            let rope = self.buffer.rope();
            let (Ok(line), Some(removed)) = (
                rope.try_char_to_line(range.start),
                rope.get_slice(range.clone()),
            ) else {
                continue;
            };
            let line_start = rope.try_line_to_char(line).ok() == Some(range.start);
            marks = follow(&marks, line, line_start, &removed.to_string(), text);
        }
        Rc::new(marks)
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
        if self.read_only() {
            return;
        }
        let lines = self.buffer.line_count();
        let before = self.buffer.rope().clone();
        let moved = if back {
            self.buffer.undo()
        } else {
            self.buffer.redo()
        };
        if !moved {
            return;
        }
        let after = self.buffer.rope();
        if !self.marks.is_empty() {
            let same = |pair: &(char, char)| pair.0 == pair.1;
            let start = before.chars().zip(after.chars()).take_while(same).count();
            let (old_len, new_len) = (before.len_chars(), after.len_chars());
            let tail = before
                .chars_at(old_len)
                .reversed()
                .zip(after.chars_at(new_len).reversed())
                .take_while(same)
                .count()
                .min(old_len.min(new_len) - start);
            let line = before.char_to_line(start);
            let line_start = before.line_to_char(line) == start;
            let removed = before.slice(start..old_len - tail).to_string();
            let inserted = after.slice(start..new_len - tail).to_string();
            self.marks = Rc::new(follow(&self.marks, line, line_start, &removed, &inserted));
        }
        self.settle(lines);
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
        if self.edited_since_found {
            self.search(cx);
        }
        self.since = Instant::now();
        self.blink = None;
        cx.notify();
    }

    fn reveal(&self) {
        if let Some(caret) = self.carets.last()
            && let Ok(row) = self.buffer.char_to_line(caret.head)
        {
            let row = match self.wrapped() {
                Some(wrap) => self.visual(caret.head, wrap).map_or(row, |(row, _)| row),
                None => row,
            };
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
            (false, "z") if held.alt && !held.shift => self.toggle_wrap(),
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
            (true, "f") => self.find(window, cx),
            (true, "s") => {
                if let Some(save) = self.on_save.clone().filter(|_| !self.read_only()) {
                    let saved = save(&mut self.buffer, window, cx);
                    self.failure = saved.err().map(|error| ("Could not save", error.into()));
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
        let wrap = self.wrapped();
        let (line, piece) = match wrap {
            Some(wrap) => wrap.locate(row.min(wrap.rows().saturating_sub(1))),
            None => (row.min(self.buffer.line_count().saturating_sub(1)), 0),
        };
        let drawn = self.drawn(line);
        let text = &drawn.text;
        let (within, last) = match wrap {
            Some(wrap) => (
                wrap.piece(line, piece, text.len()),
                piece + 1 == wrap.pieces(line),
            ),
            None => (0..text.len(), true),
        };
        let shown: SharedString = text.get(within.clone())?.to_owned().into();
        let theme = ActiveTheme::theme(cx);
        let mut byte = within.start + code_shape(shown, window, &theme).closest_index_for_x(x);
        if !last && byte >= within.end {
            byte = text
                .get(..within.end)?
                .char_indices()
                .last()
                .map_or(within.start, |(at, _)| at.max(within.start));
        }
        Some(self.buffer.line_to_char(line).ok()? + drawn.column(byte))
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
        let mut lines = coloured_lines(&self.buffer, self.syntax.spans(rows.clone()), rows.clone());
        let current = self.found.get(self.current);
        for (row, (line, drawn)) in rows.zip(lines.iter_mut()) {
            let Ok(start) = self.buffer.line_to_char(row) else {
                continue;
            };
            let end = start + drawn.columns();
            let byte_of = |at: usize| drawn.byte(at - start);
            let first = self.found.partition_point(|found| found.end <= start);
            for found in self.found.iter().skip(first) {
                let (from, to) = (found.start.max(start), found.end.min(end));
                if from >= to {
                    break;
                }
                if Some(found) == current {
                    line.current_match = Some(line.matches.len());
                }
                line.matches.push(byte_of(from)..byte_of(to));
            }
            for caret in &self.carets {
                if caret_on && (start..=end).contains(&caret.head) {
                    line.carets.push(byte_of(caret.head));
                }
                let range = caret.range();
                let (from, to) = (range.start.max(start), range.end.min(end));
                if from < to && Some(&range) != current {
                    line.selected.push(byte_of(from)..byte_of(to));
                }
            }
        }
        lines.into_iter().map(|(line, _)| line).collect()
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
        let line = self.buffer.char_to_line(at).ok()?;
        let column = at - self.buffer.line_to_char(line).ok()?;
        let drawn = self.drawn(line);
        let text = drawn.text.as_str();
        let byte = drawn.byte(column);
        let (row, within) = match self.wrapped() {
            Some(wrap) => {
                let row = wrap.row_of(line, byte);
                (row, wrap.piece(line, wrap.locate(row).1, text.len()))
            }
            None => (line, 0..text.len()),
        };
        let piece: SharedString = text.get(within.clone())?.to_owned().into();
        let x = code_shape(piece, window, &ActiveTheme::theme(cx))
            .x_for_index(byte.saturating_sub(within.start));
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
        self.rewrap(cx);
        let entity = cx.entity();
        let source = entity.clone();
        let bounds = self.bounds.clone();
        let focus = self.focus.clone();
        let wrapped_at = self.wrapping.then(|| self.wrap.width());
        let rows = code_view(
            "code-editor-rows",
            self.buffer.line_count(),
            &self.scroll,
            move |rows, cx| source.update(cx, |editor, _| editor.rows(rows, shown)),
        )
        .widest(self.widest)
        .wrap(self.wrapped().map(|_| self.wrap.clone()))
        .marks(self.marks.clone());
        let failure = self.failure.clone().map(|(what, error)| {
            div()
                .absolute()
                .left(px(NUMBER_COLUMN))
                .right(px(FAILURE_INSET))
                .bottom(px(FAILURE_INSET))
                .child(fail(what, error, "", &ActiveTheme::theme(cx)))
        });
        let editing = div()
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
                        if wrapped_at.is_some_and(|width| width != wrap_width(area)) {
                            cx.notify(entity.entity_id());
                        }
                        window.handle_input(&focus, ElementInputHandler::new(area, entity), cx);
                    },
                )
                .absolute()
                .top_0()
                .left_0()
                .size_full(),
            )
            .children(failure);
        div()
            .relative()
            .size_full()
            .child(editing)
            .children(self.bar.clone())
    }
}
