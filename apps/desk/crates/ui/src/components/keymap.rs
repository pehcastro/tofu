use std::fmt;
use std::fs;
use std::io::ErrorKind;
use std::path::Path;
use std::rc::Rc;

use gpui::{App, FocusHandle, Global, KeyDownEvent, Keystroke, Window};
use serde_json::{Map, Value};

use crate::components::vim;

pub mod host {
    gpui::actions!(
        editor,
        [GoToLine, QuickOpen, FindInFiles, CloseTab, NextTab, PrevTab]
    );
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Motion {
    Left,
    Right,
    WordLeft,
    WordRight,
    Up,
    Down,
    LineStart,
    LineEnd,
    Top,
    Bottom,
}

impl Motion {
    const ALL: [Motion; 10] = [
        Motion::Left,
        Motion::Right,
        Motion::WordLeft,
        Motion::WordRight,
        Motion::Up,
        Motion::Down,
        Motion::LineStart,
        Motion::LineEnd,
        Motion::Top,
        Motion::Bottom,
    ];

    fn name(self) -> &'static str {
        match self {
            Motion::Left => "left",
            Motion::Right => "right",
            Motion::WordLeft => "word_left",
            Motion::WordRight => "word_right",
            Motion::Up => "up",
            Motion::Down => "down",
            Motion::LineStart => "line_start",
            Motion::LineEnd => "line_end",
            Motion::Top => "top",
            Motion::Bottom => "bottom",
        }
    }

    fn named(name: &str) -> Option<Motion> {
        Motion::ALL.into_iter().find(|motion| motion.name() == name)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Host {
    GoToLine,
    QuickOpen,
    FindInFiles,
    CloseTab,
    NextTab,
    PrevTab,
}

impl Host {
    pub fn action(self) -> Box<dyn gpui::Action> {
        match self {
            Host::GoToLine => Box::new(host::GoToLine),
            Host::QuickOpen => Box::new(host::QuickOpen),
            Host::FindInFiles => Box::new(host::FindInFiles),
            Host::CloseTab => Box::new(host::CloseTab),
            Host::NextTab => Box::new(host::NextTab),
            Host::PrevTab => Box::new(host::PrevTab),
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Action {
    Move(Motion),
    Select(Motion),
    AddCursorUp,
    AddCursorDown,
    SelectAll,
    SingleCursor,
    DeleteLeft,
    DeleteRight,
    Newline,
    Tab,
    DeleteLine,
    DuplicateLine,
    MoveLineUp,
    MoveLineDown,
    CommentToggle,
    Indent,
    Outdent,
    Find,
    Save,
    Fold,
    Unfold,
    Undo,
    Redo,
    Copy,
    Cut,
    Paste,
    ToggleWrap,
    Host(Host),
}

const PLAIN: [Action; 31] = [
    Action::AddCursorUp,
    Action::AddCursorDown,
    Action::SelectAll,
    Action::SingleCursor,
    Action::DeleteLeft,
    Action::DeleteRight,
    Action::Newline,
    Action::Tab,
    Action::DeleteLine,
    Action::DuplicateLine,
    Action::MoveLineUp,
    Action::MoveLineDown,
    Action::CommentToggle,
    Action::Indent,
    Action::Outdent,
    Action::Find,
    Action::Save,
    Action::Fold,
    Action::Unfold,
    Action::Undo,
    Action::Redo,
    Action::Copy,
    Action::Cut,
    Action::Paste,
    Action::ToggleWrap,
    Action::Host(Host::GoToLine),
    Action::Host(Host::QuickOpen),
    Action::Host(Host::FindInFiles),
    Action::Host(Host::CloseTab),
    Action::Host(Host::NextTab),
    Action::Host(Host::PrevTab),
];

impl Action {
    fn named(name: &str) -> Option<Action> {
        let motion = |prefix: &str| name.strip_prefix(prefix).and_then(Motion::named);
        motion("move_")
            .map(Action::Move)
            .or_else(|| motion("select_").map(Action::Select))
            .or_else(|| PLAIN.into_iter().find(|action| action.to_string() == name))
    }
}

impl fmt::Display for Action {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let name = match self {
            Action::Move(motion) => return write!(f, "move_{}", motion.name()),
            Action::Select(motion) => return write!(f, "select_{}", motion.name()),
            Action::AddCursorUp => "add_cursor_up",
            Action::AddCursorDown => "add_cursor_down",
            Action::SelectAll => "select_all",
            Action::SingleCursor => "single_cursor",
            Action::DeleteLeft => "delete_left",
            Action::DeleteRight => "delete_right",
            Action::Newline => "newline",
            Action::Tab => "tab",
            Action::DeleteLine => "delete_line",
            Action::DuplicateLine => "duplicate_line",
            Action::MoveLineUp => "move_line_up",
            Action::MoveLineDown => "move_line_down",
            Action::CommentToggle => "comment_toggle",
            Action::Indent => "indent",
            Action::Outdent => "outdent",
            Action::Find => "find",
            Action::Save => "save",
            Action::Fold => "fold",
            Action::Unfold => "unfold",
            Action::Undo => "undo",
            Action::Redo => "redo",
            Action::Copy => "copy",
            Action::Cut => "cut",
            Action::Paste => "paste",
            Action::ToggleWrap => "toggle_wrap",
            Action::Host(Host::GoToLine) => "go_to_line",
            Action::Host(Host::QuickOpen) => "quick_open",
            Action::Host(Host::FindInFiles) => "find_in_files",
            Action::Host(Host::CloseTab) => "close_tab",
            Action::Host(Host::NextTab) => "next_tab",
            Action::Host(Host::PrevTab) => "prev_tab",
        };
        f.write_str(name)
    }
}

const NAMED_KEYS: [&str; 27] = [
    "up",
    "down",
    "left",
    "right",
    "home",
    "end",
    "pageup",
    "pagedown",
    "backspace",
    "delete",
    "enter",
    "tab",
    "escape",
    "space",
    "insert",
    "f1",
    "f2",
    "f3",
    "f4",
    "f5",
    "f6",
    "f7",
    "f8",
    "f9",
    "f10",
    "f11",
    "f12",
];

const SHIFTED: [(&str, &str); 21] = [
    ("`", "~"),
    ("1", "!"),
    ("2", "@"),
    ("3", "#"),
    ("4", "$"),
    ("5", "%"),
    ("6", "^"),
    ("7", "&"),
    ("8", "*"),
    ("9", "("),
    ("0", ")"),
    ("-", "_"),
    ("=", "+"),
    ("[", "{"),
    ("]", "}"),
    ("\\", "|"),
    (";", ":"),
    ("'", "\""),
    (",", "<"),
    (".", ">"),
    ("/", "?"),
];

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Stroke {
    ctrl: bool,
    shift: bool,
    alt: bool,
    key: String,
}

impl Stroke {
    pub fn typed(keystroke: &Keystroke) -> Stroke {
        let held = keystroke.modifiers;
        Stroke {
            ctrl: held.secondary(),
            shift: held.shift,
            alt: held.alt,
            key: keystroke.key.to_ascii_lowercase(),
        }
    }

    fn parse(text: &str) -> Result<Stroke, String> {
        let (held, key) = match text.strip_suffix("++") {
            Some(held) => (held, "+"),
            None => text.rsplit_once('+').unwrap_or(("", text)),
        };
        let mut stroke = Stroke {
            ctrl: false,
            shift: false,
            alt: false,
            key: key.to_ascii_lowercase(),
        };
        for modifier in held.split('+').filter(|modifier| !modifier.is_empty()) {
            match modifier.to_ascii_lowercase().as_str() {
                "ctrl" => stroke.ctrl = true,
                "shift" => stroke.shift = true,
                "alt" => stroke.alt = true,
                _ => return Err(format!("unknown modifier {modifier} in {text}")),
            }
        }
        let named = NAMED_KEYS.contains(&stroke.key.as_str());
        if !named && stroke.key.chars().count() != 1 {
            return Err(format!("unknown key {key:?} in {text}"));
        }
        Ok(stroke)
    }

    fn matches(&self, typed: &Stroke) -> bool {
        let shift_in_key = self.shift
            && !typed.shift
            && SHIFTED.contains(&(self.key.as_str(), typed.key.as_str()));
        self.ctrl == typed.ctrl
            && self.alt == typed.alt
            && (shift_in_key || self.shift == typed.shift && self.key == typed.key)
    }
}

impl fmt::Display for Stroke {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let held = [
            (self.ctrl, "ctrl+"),
            (self.shift, "shift+"),
            (self.alt, "alt+"),
        ];
        for (on, name) in held {
            if on {
                f.write_str(name)?;
            }
        }
        f.write_str(&self.key)
    }
}

pub enum Resolved {
    Run(Action),
    Wait,
    Miss,
}

const DEFAULTS: [(&str, Action); 66] = [
    ("left", Action::Move(Motion::Left)),
    ("right", Action::Move(Motion::Right)),
    ("up", Action::Move(Motion::Up)),
    ("down", Action::Move(Motion::Down)),
    ("home", Action::Move(Motion::LineStart)),
    ("end", Action::Move(Motion::LineEnd)),
    ("ctrl+left", Action::Move(Motion::WordLeft)),
    ("ctrl+right", Action::Move(Motion::WordRight)),
    ("ctrl+home", Action::Move(Motion::Top)),
    ("ctrl+end", Action::Move(Motion::Bottom)),
    ("shift+left", Action::Select(Motion::Left)),
    ("shift+right", Action::Select(Motion::Right)),
    ("shift+up", Action::Select(Motion::Up)),
    ("shift+down", Action::Select(Motion::Down)),
    ("shift+home", Action::Select(Motion::LineStart)),
    ("shift+end", Action::Select(Motion::LineEnd)),
    ("ctrl+shift+left", Action::Select(Motion::WordLeft)),
    ("ctrl+shift+right", Action::Select(Motion::WordRight)),
    ("ctrl+shift+home", Action::Select(Motion::Top)),
    ("ctrl+shift+end", Action::Select(Motion::Bottom)),
    ("ctrl+alt+up", Action::AddCursorUp),
    ("ctrl+alt+down", Action::AddCursorDown),
    ("ctrl+a", Action::SelectAll),
    ("escape", Action::SingleCursor),
    ("backspace", Action::DeleteLeft),
    ("shift+backspace", Action::DeleteLeft),
    ("delete", Action::DeleteRight),
    ("enter", Action::Newline),
    ("shift+enter", Action::Newline),
    ("tab", Action::Tab),
    ("shift+tab", Action::Outdent),
    ("ctrl+shift+k", Action::DeleteLine),
    ("shift+alt+down", Action::DuplicateLine),
    ("alt+up", Action::MoveLineUp),
    ("alt+down", Action::MoveLineDown),
    ("ctrl+/", Action::CommentToggle),
    ("ctrl+]", Action::Indent),
    ("ctrl+[", Action::Outdent),
    ("ctrl+f", Action::Find),
    ("ctrl+s", Action::Save),
    ("ctrl+shift+[", Action::Fold),
    ("ctrl+shift+{", Action::Fold),
    ("ctrl+shift+]", Action::Unfold),
    ("ctrl+shift+}", Action::Unfold),
    ("ctrl+z", Action::Undo),
    ("ctrl+y", Action::Redo),
    ("ctrl+shift+z", Action::Redo),
    ("ctrl+c", Action::Copy),
    ("ctrl+insert", Action::Copy),
    ("ctrl+x", Action::Cut),
    ("shift+delete", Action::Cut),
    ("ctrl+v", Action::Paste),
    ("shift+insert", Action::Paste),
    ("alt+z", Action::ToggleWrap),
    ("ctrl+g", Action::Host(Host::GoToLine)),
    ("ctrl+p", Action::Host(Host::QuickOpen)),
    ("ctrl+e", Action::Host(Host::QuickOpen)),
    ("ctrl+shift+f", Action::Host(Host::FindInFiles)),
    ("ctrl+w", Action::Host(Host::CloseTab)),
    ("ctrl+f4", Action::Host(Host::CloseTab)),
    ("ctrl+tab", Action::Host(Host::NextTab)),
    ("ctrl+pagedown", Action::Host(Host::NextTab)),
    ("ctrl+shift+tab", Action::Host(Host::PrevTab)),
    ("ctrl+pageup", Action::Host(Host::PrevTab)),
    ("ctrl+shift+alt+up", Action::AddCursorUp),
    ("ctrl+shift+alt+down", Action::AddCursorDown),
];

pub struct Keymap {
    bindings: Vec<(Vec<Stroke>, Action)>,
}

pub struct FocusedKeys {
    pub(crate) focus: FocusHandle,
    pub(crate) keymap: Rc<Keymap>,
    pub(crate) pending: Vec<Stroke>,
    pub(crate) vim: Option<vim::Mode>,
}

impl Global for FocusedKeys {}

pub fn editor_claims(event: &KeyDownEvent, window: &Window, cx: &App) -> bool {
    let Some(keys) = cx.try_global::<FocusedKeys>() else {
        return false;
    };
    let mut typed = keys.pending.clone();
    typed.push(Stroke::typed(&event.keystroke));
    let modal = keys.vim.is_some_and(vim::Mode::claims) && vim::key(&event.keystroke).is_some();
    keys.focus.is_focused(window)
        && (modal
            || !keys.pending.is_empty()
            || !matches!(keys.keymap.resolve(&typed), Resolved::Miss))
}

fn refuse(file: &str, line: usize, reason: &str) {
    eprintln!("desk: keymap {file}:{line} refused {reason}");
}

fn line_of(text: &str, keys: &str) -> usize {
    let quoted = format!("\"{keys}\"");
    text.match_indices(&quoted)
        .find(|(at, _)| {
            text.get(at + quoted.len()..)
                .is_some_and(|rest| rest.trim_start().starts_with(':'))
        })
        .and_then(|(at, _)| text.get(..at))
        .map_or(1, |before| before.matches('\n').count() + 1)
}

impl Default for Keymap {
    fn default() -> Self {
        let mut keymap = Keymap {
            bindings: Vec::with_capacity(DEFAULTS.len()),
        };
        for (at, (keys, action)) in DEFAULTS.into_iter().enumerate() {
            if let Err(reason) = keymap.bind(keys, action) {
                refuse("defaults", at + 1, &reason);
            }
        }
        keymap
    }
}

impl Keymap {
    pub fn load(file: &Path) -> Keymap {
        let mut keymap = Keymap::default();
        let shown = file.display().to_string();
        let text = match fs::read_to_string(file) {
            Ok(text) => text,
            Err(error) if error.kind() == ErrorKind::NotFound => return keymap,
            Err(error) => {
                eprintln!("desk: keymap {shown} could not be read, the defaults stand: {error}");
                return keymap;
            }
        };
        let entries: Map<String, Value> = match serde_json::from_str(&text) {
            Ok(entries) => entries,
            Err(error) => {
                refuse(&shown, error.line(), &error.to_string());
                return keymap;
            }
        };
        let mut bound = 0;
        for (keys, value) in &entries {
            let action = match value.as_str() {
                Some(name) => Action::named(name).ok_or(format!("{keys}: unknown action {name}")),
                None => Err(format!("{keys}: the action is {value}, not a name")),
            };
            match action.and_then(|action| keymap.bind(keys, action)) {
                Ok(()) => bound += 1,
                Err(reason) => refuse(&shown, line_of(&text, keys), &reason),
            }
        }
        eprintln!(
            "desk: keymap {shown} bound {bound} of {} keys",
            entries.len()
        );
        keymap
    }

    fn bind(&mut self, keys: &str, action: Action) -> Result<(), String> {
        let strokes = keys
            .split_whitespace()
            .map(Stroke::parse)
            .collect::<Result<Vec<_>, _>>()?;
        if strokes.is_empty() {
            return Err("an empty key".to_owned());
        }
        self.bindings.retain(|(bound, _)| *bound != strokes);
        self.bindings.push((strokes, action));
        Ok(())
    }

    pub fn resolve(&self, typed: &[Stroke]) -> Resolved {
        let mut exact = None;
        for (bound, action) in self.bindings.iter().rev() {
            let starts = bound.len() >= typed.len()
                && bound
                    .iter()
                    .zip(typed)
                    .all(|(bound, typed)| bound.matches(typed));
            match (starts, bound.len() == typed.len()) {
                (false, _) => {}
                (true, false) => return Resolved::Wait,
                (true, true) => {
                    exact.get_or_insert(*action);
                }
            }
        }
        exact.map_or(Resolved::Miss, Resolved::Run)
    }
}
