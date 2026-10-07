use std::borrow::Cow;
use std::fmt;
use std::fs::{self, File};
use std::io::{self, BufWriter, Write};
use std::ops::Range;
use std::path::Path;
use std::time::{Duration, Instant};

use ropey::Rope;

pub const TYPING_GROUP: Duration = Duration::from_millis(500);

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum LineEnding {
    Lf,
    Crlf,
}

#[derive(Debug)]
pub enum BufferError {
    Io(io::Error),
    OutOfRange { index: usize, len: usize },
}

impl fmt::Display for BufferError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Io(error) => write!(f, "{error}"),
            Self::OutOfRange { index, len } => write!(f, "index {index} is past the end {len}"),
        }
    }
}

impl std::error::Error for BufferError {}

impl From<io::Error> for BufferError {
    fn from(error: io::Error) -> Self {
        Self::Io(error)
    }
}

struct Change {
    at: usize,
    removed: String,
    removed_chars: usize,
    inserted: String,
    inserted_chars: usize,
}

struct Step {
    id: u64,
    changes: Vec<Change>,
    before: Vec<Range<usize>>,
    after: Vec<Range<usize>>,
}

pub struct Buffer {
    rope: Rope,
    ending: LineEnding,
    selections: Vec<Range<usize>>,
    undos: Vec<Step>,
    redos: Vec<Step>,
    typed_at: Option<Instant>,
    next_id: u64,
    saved_id: u64,
}

impl Buffer {
    pub fn from_text(text: &str) -> Self {
        let crlf = text
            .split_once('\n')
            .is_some_and(|(first, _)| first.ends_with('\r'));
        Self {
            rope: Rope::from_str(text),
            ending: if crlf {
                LineEnding::Crlf
            } else {
                LineEnding::Lf
            },
            selections: vec![Range { start: 0, end: 0 }],
            undos: Vec::new(),
            redos: Vec::new(),
            typed_at: None,
            next_id: 0,
            saved_id: 0,
        }
    }

    pub fn load(path: &Path) -> Result<Self, BufferError> {
        Ok(Self::from_text(&fs::read_to_string(path)?))
    }

    pub fn save(&mut self, path: &Path) -> Result<(), BufferError> {
        let mut out = BufWriter::new(File::create(path)?);
        self.rope.write_to(&mut out)?;
        out.flush()?;
        self.saved_id = self.top_id();
        self.typed_at = None;
        Ok(())
    }

    pub fn text(&self) -> String {
        self.rope.to_string()
    }

    pub fn line_ending(&self) -> LineEnding {
        self.ending
    }

    pub fn len_chars(&self) -> usize {
        self.rope.len_chars()
    }

    pub fn line_count(&self) -> usize {
        self.rope.len_lines()
    }

    pub fn line(&self, index: usize) -> Option<String> {
        let mut line = self.rope.get_line(index)?.to_string();
        if line.ends_with('\n') {
            line.pop();
            if line.ends_with('\r') {
                line.pop();
            }
        }
        Some(line)
    }

    pub fn char_to_line(&self, char: usize) -> Result<usize, BufferError> {
        self.rope
            .try_char_to_line(char)
            .map_err(|_| BufferError::OutOfRange {
                index: char,
                len: self.len_chars(),
            })
    }

    pub fn line_to_char(&self, line: usize) -> Result<usize, BufferError> {
        self.rope
            .try_line_to_char(line)
            .map_err(|_| BufferError::OutOfRange {
                index: line,
                len: self.line_count(),
            })
    }

    pub fn selections(&self) -> &[Range<usize>] {
        &self.selections
    }

    pub fn is_dirty(&self) -> bool {
        self.top_id() != self.saved_id
    }

    pub fn edit(&mut self, ranges: &[Range<usize>], text: &str) -> Result<(), BufferError> {
        let targets = self.normalize(ranges)?;
        if targets.is_empty() {
            return Ok(());
        }
        let text = self.with_ending(text);
        let inserted_chars = text.chars().count();
        let mut changes = Vec::with_capacity(targets.len());
        for range in targets.iter().rev() {
            let removed = self.rope.slice(range.clone()).to_string();
            self.rope.remove(range.clone());
            self.rope.insert(range.start, &text);
            changes.push(Change {
                at: range.start,
                removed,
                removed_chars: range.len(),
                inserted: text.to_string(),
                inserted_chars,
            });
        }
        let mut after = Vec::with_capacity(targets.len());
        let (mut added, mut removed) = (0, 0);
        for range in &targets {
            let head = range.start + added - removed + inserted_chars;
            after.push(head..head);
            added += inserted_chars;
            removed += range.len();
        }
        let typing = inserted_chars == 1
            && !text.contains('\n')
            && targets.iter().all(|range| range.is_empty());
        let now = Instant::now();
        let grouped = typing
            && self
                .typed_at
                .is_some_and(|at| now.duration_since(at) <= TYPING_GROUP);
        self.typed_at = typing.then_some(now);
        self.next_id += 1;
        self.redos.clear();
        if grouped && let Some(step) = self.undos.last_mut() {
            step.id = self.next_id;
            step.changes.extend(changes);
            step.after.clone_from(&after);
        } else {
            self.undos.push(Step {
                id: self.next_id,
                changes,
                before: targets,
                after: after.clone(),
            });
        }
        self.selections = after;
        Ok(())
    }

    pub fn undo(&mut self) -> bool {
        let Some(step) = self.undos.pop() else {
            return false;
        };
        for change in step.changes.iter().rev() {
            self.rope
                .remove(change.at..change.at + change.inserted_chars);
            self.rope.insert(change.at, &change.removed);
        }
        self.selections.clone_from(&step.before);
        self.redos.push(step);
        self.typed_at = None;
        true
    }

    pub fn redo(&mut self) -> bool {
        let Some(step) = self.redos.pop() else {
            return false;
        };
        for change in &step.changes {
            self.rope
                .remove(change.at..change.at + change.removed_chars);
            self.rope.insert(change.at, &change.inserted);
        }
        self.selections.clone_from(&step.after);
        self.undos.push(step);
        self.typed_at = None;
        true
    }

    fn top_id(&self) -> u64 {
        self.undos.last().map_or(0, |step| step.id)
    }

    fn normalize(&self, ranges: &[Range<usize>]) -> Result<Vec<Range<usize>>, BufferError> {
        let len = self.len_chars();
        let mut sorted = Vec::with_capacity(ranges.len());
        for range in ranges {
            let (start, end) = (range.start.min(range.end), range.start.max(range.end));
            if end > len {
                return Err(BufferError::OutOfRange { index: end, len });
            }
            sorted.push(start..end);
        }
        sorted.sort_by_key(|range| range.start);
        let mut merged: Vec<Range<usize>> = Vec::with_capacity(sorted.len());
        for range in sorted {
            match merged.last_mut() {
                Some(last) if range.start <= last.end => last.end = last.end.max(range.end),
                _ => merged.push(range),
            }
        }
        Ok(merged)
    }

    fn with_ending<'a>(&self, text: &'a str) -> Cow<'a, str> {
        if !text.contains('\n') {
            return Cow::Borrowed(text);
        }
        let lf = text.replace("\r\n", "\n");
        match self.ending {
            LineEnding::Lf => Cow::Owned(lf),
            LineEnding::Crlf => Cow::Owned(lf.replace('\n', "\r\n")),
        }
    }
}
