use std::fs;
use std::io::ErrorKind;
use std::mem;
use std::ops::Range;
use std::path::Path;

use desk_core::buffer::Buffer;
use gpui::Keystroke;
use serde_json::{Map, Value};

use crate::components::code_editor::{Class, class};

const SETTING: &str = "editor.vim";
const MOST_COUNT: usize = 9999;
const PAIRS: [(char, char, char); 4] = [
    ('(', ')', 'b'),
    ('[', ']', '['),
    ('{', '}', 'B'),
    ('<', '>', '<'),
];

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Mode {
    #[default]
    Normal,
    Insert,
    Visual,
    VisualLine,
    Pending,
    Command,
}

impl Mode {
    fn name(self) -> &'static str {
        match self {
            Mode::Normal => "normal",
            Mode::Insert => "insert",
            Mode::Visual => "visual",
            Mode::VisualLine => "visual line",
            Mode::Pending => "operator pending",
            Mode::Command => "command",
        }
    }

    fn pill(self) -> &'static str {
        match self {
            Mode::Normal => "NORMAL",
            Mode::Insert => "INSERT",
            Mode::Visual => "VISUAL",
            Mode::VisualLine => "VISUAL LINE",
            Mode::Pending => "PENDING",
            Mode::Command => ":",
        }
    }

    pub fn claims(self) -> bool {
        self != Mode::Insert
    }

    pub fn visual(self) -> bool {
        matches!(self, Mode::Visual | Mode::VisualLine)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Key {
    Char(char),
    Escape,
    Enter,
    Backspace,
    Redo,
}

fn single(text: &str) -> Option<char> {
    let mut chars = text.chars();
    let ch = chars.next()?;
    (chars.next().is_none() && !ch.is_control()).then_some(ch)
}

pub fn key(keystroke: &Keystroke) -> Option<Key> {
    let held = keystroke.modifiers;
    let typed = keystroke.key_char.as_deref().and_then(single);
    match (
        held.control,
        held.alt || held.platform,
        keystroke.key.as_str(),
    ) {
        (true, true, _) => typed.map(Key::Char),
        (true, false, "r") => Some(Key::Redo),
        (true, false, _) | (false, true, _) => None,
        (false, false, "escape") => Some(Key::Escape),
        (false, false, "enter") => Some(Key::Enter),
        (false, false, "backspace") => Some(Key::Backspace),
        (false, false, key) => typed.or_else(|| single(key)).map(Key::Char),
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Find {
    target: char,
    till: bool,
    back: bool,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Motion {
    Left,
    Right,
    Down,
    Up,
    WordStart,
    WordBack,
    WordEnd,
    LineStart,
    LineEnd,
    Goto(Option<usize>),
    Find(Find),
    Match,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Object {
    Word {
        around: bool,
    },
    Pair {
        open: char,
        close: char,
        around: bool,
    },
    Quote {
        quote: char,
        around: bool,
    },
}

impl Object {
    fn named(ch: char, around: bool) -> Option<Object> {
        match ch {
            'w' => Some(Object::Word { around }),
            '"' | '\'' | '`' => Some(Object::Quote { quote: ch, around }),
            _ => PAIRS
                .iter()
                .find(|(open, close, alias)| ch == *open || ch == *close || ch == *alias)
                .map(|(open, close, _)| Object::Pair {
                    open: *open,
                    close: *close,
                    around,
                }),
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Target {
    Motion(Motion),
    Object(Object),
    Lines,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Operator {
    Delete,
    Change,
    Yank,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Entry {
    Before,
    After,
    LineStart,
    LineEnd,
    Below,
    Above,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Ex {
    Write,
    Quit,
    WriteQuit,
    Line(usize),
    Unknown(String),
}

impl Ex {
    fn parsed(line: &str) -> Ex {
        match line {
            "w" => Ex::Write,
            "q" | "q!" => Ex::Quit,
            "wq" | "x" => Ex::WriteQuit,
            _ => line
                .parse()
                .map_or_else(|_| Ex::Unknown(line.to_owned()), Ex::Line),
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Command {
    Pass,
    Held,
    Move(Motion, usize),
    Operate(Operator, Target, usize),
    Selected(Operator, bool),
    Grab(Object),
    Insert(Entry),
    Paste { before: bool, count: usize },
    Undo(usize),
    Redo(usize),
    Repeat(usize),
    Select,
    Unselect,
    Normal,
    Ex(Ex),
}

impl Command {
    fn changes(&self) -> bool {
        matches!(
            self,
            Command::Operate(Operator::Delete | Operator::Change, _, _)
                | Command::Insert(_)
                | Command::Paste { .. }
        )
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Prefix {
    Go,
    Find { till: bool, back: bool },
    Object { around: bool },
}

#[derive(Default)]
pub struct Vim {
    mode: Mode,
    count: Option<usize>,
    operator: Option<(Operator, usize)>,
    prefix: Option<Prefix>,
    line: String,
    keys: Vec<Key>,
    last: Vec<Key>,
    inserted: String,
    find: Option<Find>,
    register: Option<(String, bool)>,
    insert_from: usize,
}

impl Vim {
    pub fn enabled(file: &Path) -> bool {
        let shown = file.display();
        let read = match fs::read_to_string(file) {
            Ok(text) => serde_json::from_str::<Map<String, Value>>(&text)
                .map(|settings| settings.get(SETTING).cloned())
                .map_err(|error| error.to_string()),
            Err(error) if error.kind() == ErrorKind::NotFound => Ok(None),
            Err(error) => Err(error.to_string()),
        };
        let on = match read {
            Ok(None) => false,
            Ok(Some(Value::Bool(on))) => on,
            Ok(Some(other)) => {
                eprintln!("desk: settings {shown} refused {SETTING} {other}, not true or false");
                false
            }
            Err(error) => {
                eprintln!("desk: settings {shown} could not be read, vim stays off: {error}");
                false
            }
        };
        eprintln!("desk: editor vim {}", if on { "on" } else { "off" });
        on
    }

    pub fn mode(&self) -> Mode {
        self.mode
    }

    pub fn pill(&self) -> String {
        match self.mode {
            Mode::Command => format!(":{}", self.line),
            mode => mode.pill().to_owned(),
        }
    }

    pub fn enter(&mut self, mode: Mode) {
        if mode != self.mode {
            eprintln!("desk: vim {}", mode.name());
            self.mode = mode;
        }
    }

    pub fn yank(&mut self, text: String, linewise: bool) {
        self.register = Some((text, linewise));
    }

    pub fn register(&self) -> Option<(&str, bool)> {
        self.register
            .as_ref()
            .map(|(text, linewise)| (text.as_str(), *linewise))
    }

    pub fn began_insert(&mut self, at: usize) {
        self.insert_from = at;
    }

    pub fn insert_from(&self) -> usize {
        self.insert_from
    }

    pub fn finished_insert(&mut self, text: String) {
        self.inserted = text;
    }

    pub fn last_change(&self) -> (Vec<Key>, String) {
        (self.last.clone(), self.inserted.clone())
    }

    pub fn key(&mut self, key: Key) -> Command {
        match self.mode {
            Mode::Insert if key == Key::Escape => {
                self.enter(Mode::Normal);
                Command::Normal
            }
            Mode::Insert => Command::Pass,
            Mode::Command => self.command_line(key),
            Mode::Normal | Mode::Pending | Mode::Visual | Mode::VisualLine => {
                self.keys.push(key);
                let command = self.normal(key);
                let waiting =
                    self.count.is_some() || self.operator.is_some() || self.prefix.is_some();
                if !waiting {
                    let keys = mem::take(&mut self.keys);
                    if command.changes() {
                        self.last = keys;
                        self.inserted.clear();
                    }
                }
                command
            }
        }
    }

    fn take_count(&mut self) -> usize {
        self.count.take().unwrap_or(1).clamp(1, MOST_COUNT)
    }

    fn reset(&mut self) -> Command {
        self.count = None;
        self.operator = None;
        self.prefix = None;
        if self.mode == Mode::Pending {
            self.enter(Mode::Normal);
        }
        Command::Held
    }

    fn normal(&mut self, key: Key) -> Command {
        let visual = self.mode.visual();
        if let Some(prefix) = self.prefix.take() {
            return match key {
                Key::Char(ch) => self.after(prefix, ch),
                Key::Escape | Key::Enter | Key::Backspace | Key::Redo => self.reset(),
            };
        }
        let ch = match key {
            Key::Char(ch) => ch,
            Key::Escape => {
                self.reset();
                self.enter(Mode::Normal);
                return Command::Unselect;
            }
            Key::Enter => return self.motion(Motion::Down),
            Key::Backspace => return self.motion(Motion::Left),
            Key::Redo if !visual && self.operator.is_none() => {
                return Command::Redo(self.take_count());
            }
            Key::Redo => return self.reset(),
        };
        match ch {
            '0' if self.count.is_none() => self.motion(Motion::LineStart),
            '0'..='9' => {
                let digit = ch.to_digit(10).map_or(0, |digit| digit as usize);
                let count = self.count.unwrap_or(0).saturating_mul(10);
                self.count = Some(count.saturating_add(digit).min(MOST_COUNT));
                Command::Held
            }
            'h' => self.motion(Motion::Left),
            'l' | ' ' => self.motion(Motion::Right),
            'j' => self.motion(Motion::Down),
            'k' => self.motion(Motion::Up),
            'w' => self.motion(Motion::WordStart),
            'b' => self.motion(Motion::WordBack),
            'e' => self.motion(Motion::WordEnd),
            '$' => self.motion(Motion::LineEnd),
            '%' => self.motion(Motion::Match),
            'G' => {
                let line = self.count.take();
                self.motion(Motion::Goto(line))
            }
            'g' | 'f' | 't' | 'F' | 'T' => {
                self.prefix = Some(match ch {
                    'g' => Prefix::Go,
                    _ => Prefix::Find {
                        till: matches!(ch, 't' | 'T'),
                        back: ch.is_ascii_uppercase(),
                    },
                });
                Command::Held
            }
            ';' => match self.find {
                Some(find) => self.motion(Motion::Find(find)),
                None => self.reset(),
            },
            'i' | 'a' if visual || self.operator.is_some() => {
                self.prefix = Some(Prefix::Object { around: ch == 'a' });
                Command::Held
            }
            'd' => self.operator(Operator::Delete),
            'c' => self.operator(Operator::Change),
            'y' => self.operator(Operator::Yank),
            'x' if visual => self.operator(Operator::Delete),
            'v' | 'V' if self.operator.is_none() => self.visual(ch == 'V'),
            _ if self.operator.is_some() || visual => self.reset(),
            'x' => self.operate(Operator::Delete, Target::Motion(Motion::Right)),
            's' => self.operate(Operator::Change, Target::Motion(Motion::Right)),
            'D' => self.operate(Operator::Delete, Target::Motion(Motion::LineEnd)),
            'C' => self.operate(Operator::Change, Target::Motion(Motion::LineEnd)),
            'Y' => self.operate(Operator::Yank, Target::Lines),
            'i' | 'a' | 'I' | 'A' | 'o' | 'O' => {
                self.reset();
                self.enter(Mode::Insert);
                Command::Insert(match ch {
                    'i' => Entry::Before,
                    'a' => Entry::After,
                    'I' => Entry::LineStart,
                    'A' => Entry::LineEnd,
                    'o' => Entry::Below,
                    _ => Entry::Above,
                })
            }
            'p' | 'P' => Command::Paste {
                before: ch == 'P',
                count: self.take_count(),
            },
            'u' => Command::Undo(self.take_count()),
            '.' => Command::Repeat(self.take_count()),
            ':' => {
                self.reset();
                self.enter(Mode::Command);
                Command::Held
            }
            _ => self.reset(),
        }
    }

    fn after(&mut self, prefix: Prefix, ch: char) -> Command {
        match prefix {
            Prefix::Go if ch == 'g' => {
                let line = self.count.take().unwrap_or(1);
                self.motion(Motion::Goto(Some(line)))
            }
            Prefix::Go => self.reset(),
            Prefix::Find { till, back } => {
                let find = Find {
                    target: ch,
                    till,
                    back,
                };
                self.find = Some(find);
                self.motion(Motion::Find(find))
            }
            Prefix::Object { around } => match (Object::named(ch, around), self.operator) {
                (Some(object), Some((operator, _))) => {
                    self.operate(operator, Target::Object(object))
                }
                (Some(object), None) if self.mode.visual() => {
                    self.count = None;
                    Command::Grab(object)
                }
                _ => self.reset(),
            },
        }
    }

    fn motion(&mut self, motion: Motion) -> Command {
        match self.operator {
            Some((operator, _)) => self.operate(operator, Target::Motion(motion)),
            None => Command::Move(motion, self.take_count()),
        }
    }

    fn operate(&mut self, operator: Operator, target: Target) -> Command {
        let count = self.take_count();
        let before = self.operator.take().map_or(1, |(_, before)| before);
        self.prefix = None;
        self.enter(match operator {
            Operator::Change => Mode::Insert,
            Operator::Delete | Operator::Yank => Mode::Normal,
        });
        Command::Operate(
            operator,
            target,
            before.saturating_mul(count).min(MOST_COUNT),
        )
    }

    fn operator(&mut self, operator: Operator) -> Command {
        let mode = self.mode;
        if mode.visual() {
            self.reset();
            self.enter(match operator {
                Operator::Change => Mode::Insert,
                Operator::Delete | Operator::Yank => Mode::Normal,
            });
            return Command::Selected(operator, mode == Mode::VisualLine);
        }
        match self.operator {
            Some((pending, _)) if pending == operator => self.operate(operator, Target::Lines),
            Some(_) => self.reset(),
            None => {
                let count = self.take_count();
                self.operator = Some((operator, count));
                self.enter(Mode::Pending);
                Command::Held
            }
        }
    }

    fn visual(&mut self, line: bool) -> Command {
        self.reset();
        let wanted = if line { Mode::VisualLine } else { Mode::Visual };
        match self.mode {
            mode if mode == wanted => {
                self.enter(Mode::Normal);
                Command::Unselect
            }
            Mode::Visual | Mode::VisualLine => {
                self.enter(wanted);
                Command::Held
            }
            Mode::Normal | Mode::Insert | Mode::Pending | Mode::Command => {
                self.enter(wanted);
                Command::Select
            }
        }
    }

    fn command_line(&mut self, key: Key) -> Command {
        match key {
            Key::Char(ch) => self.line.push(ch),
            Key::Backspace => {
                if self.line.pop().is_none() {
                    self.enter(Mode::Normal);
                }
            }
            Key::Escape => {
                self.line.clear();
                self.enter(Mode::Normal);
            }
            Key::Enter => {
                let line = mem::take(&mut self.line);
                eprintln!("desk: vim :{line}");
                self.enter(Mode::Normal);
                return Command::Ex(Ex::parsed(line.trim()));
            }
            Key::Redo => {}
        }
        Command::Held
    }
}

pub struct Reach {
    pub to: usize,
    pub inclusive: bool,
    pub linewise: bool,
}

pub struct Span {
    pub chars: Range<usize>,
    pub linewise: bool,
}

fn char_at(buffer: &Buffer, at: usize) -> Option<char> {
    buffer.rope().get_char(at)
}

fn kind(buffer: &Buffer, at: usize) -> Option<Class> {
    char_at(buffer, at).map(class)
}

pub fn line_span(buffer: &Buffer, line: usize) -> Option<Range<usize>> {
    let start = buffer.line_to_char(line).ok()?;
    Some(start..start + buffer.line(line)?.chars().count())
}

pub fn first_word(buffer: &Buffer, line: usize) -> Option<usize> {
    let span = line_span(buffer, line)?;
    Some(
        (span.start..span.end)
            .find(|at| !char_at(buffer, *at).is_some_and(char::is_whitespace))
            .unwrap_or(span.end),
    )
}

fn word_start(buffer: &Buffer, mut at: usize) -> usize {
    let len = buffer.len_chars();
    let first = kind(buffer, at);
    if first != Some(Class::Space) {
        while at < len && kind(buffer, at) == first {
            at += 1;
        }
    }
    while at < len && kind(buffer, at) == Some(Class::Space) {
        at += 1;
    }
    at
}

fn word_end(buffer: &Buffer, at: usize) -> usize {
    let len = buffer.len_chars();
    let mut at = at + 1;
    while at < len && kind(buffer, at) == Some(Class::Space) {
        at += 1;
    }
    let word = kind(buffer, at);
    while at + 1 < len && kind(buffer, at + 1) == word {
        at += 1;
    }
    at.min(len.saturating_sub(1))
}

fn word_back(buffer: &Buffer, mut at: usize) -> usize {
    while at > 0 && kind(buffer, at - 1) == Some(Class::Space) {
        at -= 1;
    }
    let word = at.checked_sub(1).and_then(|before| kind(buffer, before));
    while at > 0 && kind(buffer, at - 1) == word {
        at -= 1;
    }
    at
}

fn find_in(
    buffer: &Buffer,
    at: usize,
    line: &Range<usize>,
    find: Find,
    count: usize,
) -> Option<usize> {
    let hit = |spot: &usize| char_at(buffer, *spot) == Some(find.target);
    let skip = count.saturating_sub(1);
    let found = match find.back {
        false => (at + 1..line.end).filter(hit).nth(skip)?,
        true => (line.start..at).rev().filter(hit).nth(skip)?,
    };
    Some(match (find.till, find.back) {
        (false, _) => found,
        (true, false) => found - 1,
        (true, true) => found + 1,
    })
}

fn opening(buffer: &Buffer, before: usize, open: char, close: char) -> Option<usize> {
    let mut depth = 0usize;
    for (back, ch) in buffer.rope().get_chars_at(before)?.reversed().enumerate() {
        match ch {
            ch if ch == close => depth += 1,
            ch if ch == open => match depth.checked_sub(1) {
                Some(less) => depth = less,
                None => return Some(before - 1 - back),
            },
            _ => {}
        }
    }
    None
}

fn closing(buffer: &Buffer, from: usize, open: char, close: char) -> Option<usize> {
    let mut depth = 0usize;
    for (ahead, ch) in buffer.rope().get_chars_at(from)?.enumerate() {
        match ch {
            ch if ch == open => depth += 1,
            ch if ch == close => match depth.checked_sub(1) {
                Some(less) => depth = less,
                None => return Some(from + ahead),
            },
            _ => {}
        }
    }
    None
}

fn partner(buffer: &Buffer, at: usize, line: &Range<usize>) -> Option<usize> {
    let (from, open, close, forward) = (at..line.end).find_map(|spot| {
        let ch = char_at(buffer, spot)?;
        PAIRS.iter().find_map(|(open, close, _)| {
            (ch == *open || ch == *close).then_some((spot, *open, *close, ch == *open))
        })
    })?;
    match forward {
        true => closing(buffer, from + 1, open, close),
        false => opening(buffer, from, open, close),
    }
}

pub fn reach(buffer: &Buffer, at: usize, motion: Motion, count: usize) -> Option<Reach> {
    let line = buffer.char_to_line(at).ok()?;
    let span = line_span(buffer, line)?;
    let last = buffer.line_count().saturating_sub(1);
    let repeat = |step: fn(&Buffer, usize) -> usize| (0..count).fold(at, |at, _| step(buffer, at));
    let (to, inclusive, linewise) = match motion {
        Motion::Left => (at.saturating_sub(count).max(span.start), false, false),
        Motion::Right => (at.saturating_add(count).min(span.end), false, false),
        Motion::Down | Motion::Up => {
            let target = match motion {
                Motion::Down => line.saturating_add(count).min(last),
                _ => line.saturating_sub(count),
            };
            if target == line {
                return None;
            }
            let goal = line_span(buffer, target)?;
            ((goal.start + at - span.start).min(goal.end), false, true)
        }
        Motion::WordStart => (repeat(word_start), false, false),
        Motion::WordBack => (repeat(word_back), false, false),
        Motion::WordEnd => (repeat(word_end), true, false),
        Motion::LineStart => (span.start, false, false),
        Motion::LineEnd => {
            let goal = line_span(
                buffer,
                line.saturating_add(count.saturating_sub(1)).min(last),
            )?;
            (goal.end.saturating_sub(1).max(goal.start), true, false)
        }
        Motion::Goto(target) => {
            let target = target.map_or(last, |target| target.saturating_sub(1).min(last));
            (first_word(buffer, target)?, false, true)
        }
        Motion::Find(find) => (find_in(buffer, at, &span, find, count)?, !find.back, false),
        Motion::Match => (partner(buffer, at, &span)?, true, false),
    };
    Some(Reach {
        to,
        inclusive,
        linewise,
    })
}

fn object_range(buffer: &Buffer, at: usize, object: Object) -> Option<Range<usize>> {
    let line = line_span(buffer, buffer.char_to_line(at).ok()?)?;
    match object {
        Object::Word { around } => {
            let word = kind(buffer, at).filter(|_| at < line.end)?;
            let same = |spot: usize, wanted: Class| kind(buffer, spot) == Some(wanted);
            let (mut start, mut end) = (at, at + 1);
            while start > line.start && same(start - 1, word) {
                start -= 1;
            }
            while end < line.end && same(end, word) {
                end += 1;
            }
            if around && word != Class::Space {
                let mut tail = end;
                while tail < line.end && same(tail, Class::Space) {
                    tail += 1;
                }
                if tail > end {
                    end = tail;
                } else {
                    while start > line.start && same(start - 1, Class::Space) {
                        start -= 1;
                    }
                }
            }
            Some(start..end)
        }
        Object::Pair {
            open,
            close,
            around,
        } => {
            let opener = match char_at(buffer, at) {
                Some(ch) if ch == open => at,
                _ => opening(buffer, at, open, close)?,
            };
            let closer = closing(buffer, opener + 1, open, close)?;
            Some(match around {
                true => opener..closer + 1,
                false => opener + 1..closer,
            })
        }
        Object::Quote { quote, around } => {
            let escaped =
                |spot: usize| spot > line.start && char_at(buffer, spot - 1) == Some('\\');
            let marks: Vec<usize> = line
                .clone()
                .filter(|spot| char_at(buffer, *spot) == Some(quote) && !escaped(*spot))
                .collect();
            let [opener, closer] = *marks
                .as_chunks::<2>()
                .0
                .iter()
                .find(|[_, closer]| at <= *closer)?;
            if !around {
                return Some(opener + 1..closer);
            }
            let mut end = closer + 1;
            while end < line.end && kind(buffer, end) == Some(Class::Space) {
                end += 1;
            }
            Some(opener..end)
        }
    }
}

pub fn span(
    buffer: &Buffer,
    at: usize,
    operator: Operator,
    target: Target,
    count: usize,
) -> Option<Span> {
    let line = buffer.char_to_line(at).ok()?;
    let last = buffer.line_count().saturating_sub(1);
    let lines = |first: usize, last: usize| -> Option<Span> {
        let start = buffer.line_to_char(first).ok()?;
        let end = buffer.line_to_char(last + 1).unwrap_or(buffer.len_chars());
        Some(Span {
            chars: start..end,
            linewise: true,
        })
    };
    let motion = match target {
        Target::Lines => {
            return lines(line, line.saturating_add(count.saturating_sub(1)).min(last));
        }
        Target::Object(object) => {
            return Some(Span {
                chars: object_range(buffer, at, object)?,
                linewise: false,
            });
        }
        Target::Motion(motion) => motion,
    };
    let motion = match (operator, motion, kind(buffer, at)) {
        (Operator::Change, Motion::WordStart, Some(Class::Word | Class::Mark)) => Motion::WordEnd,
        _ => motion,
    };
    let reach = reach(buffer, at, motion, count)?;
    let (from, to) = (at.min(reach.to), at.max(reach.to));
    if reach.linewise {
        return lines(
            buffer.char_to_line(from).ok()?,
            buffer.char_to_line(to).ok()?,
        );
    }
    let to_line = buffer.char_to_line(to).ok()?;
    let end = match (reach.inclusive, motion) {
        (true, _) => (to + 1).min(line_span(buffer, to_line)?.end).max(to),
        (false, Motion::WordStart) if to_line > line => {
            line_span(buffer, to_line - 1)?.end.max(from)
        }
        (false, _) => to,
    };
    Some(Span {
        chars: from..end,
        linewise: false,
    })
}
