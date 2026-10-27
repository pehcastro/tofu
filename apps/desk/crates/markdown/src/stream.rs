use std::ops::Range;

use crate::ast::Document;
use crate::block::{self, Def, RawBlock, Refs};
use crate::{Options, component, resolve};

pub struct Stream {
    options: Options,
    text: String,
    stable_end: usize,
    stable: Vec<RawBlock>,
    stable_defs: Vec<Def>,
    refs: Refs,
    resolved: usize,
    doc: Document,
    held: Option<usize>,
}

pub fn visible_prefix(text: &str) -> &str {
    visible_from(text, 0)
}

fn visible_from(text: &str, from: usize) -> &str {
    let b = text.as_bytes();
    let mut i = from;
    while let Some(at) = b.get(i..).and_then(|r| r.iter().position(|&c| c == b'<')) {
        let at = i + at;
        let name = at + 1 + usize::from(b.get(at + 1) == Some(&b'/'));
        let after_name = name
            + b.get(name..)
                .unwrap_or_default()
                .iter()
                .take_while(|c| c.is_ascii_alphanumeric() || matches!(c, b'_' | b'.'))
                .count();
        let tag_like = b.get(name).is_none_or(u8::is_ascii_uppercase)
            && b.get(after_name)
                .is_none_or(|c| matches!(c, b' ' | b'\t' | b'\n' | b'\r' | b'/' | b'>'));
        if !tag_like {
            i = at + 1;
            continue;
        }
        match component::tag_end(b, after_name) {
            Some(end) => i = end,
            None => return text.get(..at).unwrap_or(text),
        }
    }
    text
}

fn held_from(text: &str, from: usize, math: bool) -> Option<usize> {
    let mut fence: Option<(u8, usize)> = None;
    let mut paragraph = from;
    let mut at = from;
    for line in text.get(from..)?.split_inclusive('\n') {
        let start = at;
        at += line.len();
        let complete = line.ends_with('\n');
        match (fence, fence_marker(line)) {
            (Some((open, len)), Some((mark, run, rest)))
                if mark == open && run >= len && rest.trim().is_empty() =>
            {
                fence = None
            }
            (Some(_), _) => {}
            (None, Some((mark, run, rest))) => {
                let opens = run >= 3 && (mark == b'~' || !rest.contains('`'));
                match (complete, opens) {
                    (false, _) if opens || rest.is_empty() => return Some(start),
                    (true, true) => fence = Some((mark, run)),
                    _ => continue,
                }
            }
            (None, None) if line.trim().is_empty() => {}
            (None, None) => continue,
        }
        paragraph = at;
    }
    match fence {
        None if math => open_math(text.as_bytes(), paragraph),
        _ => None,
    }
}

fn fence_marker(line: &str) -> Option<(u8, usize, &str)> {
    let body = line.trim_start_matches([' ', '\t', '>']);
    let numbered = body.trim_start_matches(|c: char| c.is_ascii_digit());
    let after_marker = match numbered.len() < body.len() {
        true => numbered.strip_prefix(['.', ')']),
        false => body.strip_prefix(['-', '*', '+']),
    };
    let body = after_marker
        .filter(|rest| rest.starts_with([' ', '\t']))
        .map_or(body, |rest| rest.trim_start_matches([' ', '\t']));
    let mark = body.bytes().next().filter(|c| matches!(c, b'`' | b'~'))?;
    let run = run(body.as_bytes(), 0, mark);
    let rest = body.get(run..)?.trim_end_matches(['\n', '\r']);
    Some((mark, run, rest))
}

fn run(b: &[u8], at: usize, mark: u8) -> usize {
    b.get(at..)
        .unwrap_or_default()
        .iter()
        .take_while(|&&c| c == mark)
        .count()
}

fn open_math(b: &[u8], from: usize) -> Option<usize> {
    let mut i = from;
    while let Some(&c) = b.get(i) {
        let n = match c {
            b'`' | b'$' => run(b, i, c),
            _ => 1,
        };
        i = match (c, b.get(i + n)) {
            (b'\\', _) => i + 2,
            (b'`', _) => code_end(b, i + n, n).unwrap_or(i + n),
            (b'$', _) if n > 2 => i + n,
            (b'$', Some(next)) if n == 1 && next.is_ascii_whitespace() => i + 1,
            (b'$', _) => match math_end(b, i + n, n) {
                Some(end) => end,
                None => return Some(i),
            },
            _ => i + 1,
        };
    }
    None
}

fn code_end(b: &[u8], from: usize, n: usize) -> Option<usize> {
    let mut j = from;
    while let Some(at) = b.get(j..)?.iter().position(|&c| c == b'`') {
        let start = j + at;
        let len = run(b, start, b'`');
        if len == n {
            return Some(start + len);
        }
        j = start + len;
    }
    None
}

fn math_end(b: &[u8], from: usize, n: usize) -> Option<usize> {
    let mut j = from;
    while let Some(&c) = b.get(j) {
        let next = b.get(j + 1).copied();
        let before = j.checked_sub(1).and_then(|p| b.get(p));
        j += match (c, next) {
            (b'\\', _) => 2,
            (b'$', Some(b'$')) if n == 2 && j > from => return Some(j + 2),
            (b'$', Some(b'$')) if n == 1 => 2,
            (b'$', Some(d))
                if n == 1
                    && before.is_some_and(|p| !p.is_ascii_whitespace())
                    && !d.is_ascii_digit() =>
            {
                return Some(j + 1);
            }
            _ => 1,
        };
    }
    None
}

impl Stream {
    pub fn new(options: Options) -> Self {
        Self {
            options,
            text: String::new(),
            stable_end: 0,
            stable: Vec::new(),
            stable_defs: Vec::new(),
            refs: Refs::new(),
            resolved: 0,
            doc: Document::default(),
            held: None,
        }
    }

    pub fn push(&mut self, chunk: &str) {
        self.text.push_str(chunk);
        let complete = visible_from(&self.text, self.stable_end)
            .rfind('\n')
            .map_or(0, |i| i + 1);
        if complete <= self.stable_end {
            return;
        }
        let parsed = block::parse(
            self.text.get(..complete).unwrap_or_default(),
            self.stable_end,
            self.options,
        );
        let mut blocks = parsed.blocks;
        let Some(last) = blocks.pop() else {
            return;
        };
        if blocks.is_empty() {
            return;
        }
        let last_line = self
            .text
            .get(..last.range.start)
            .and_then(|t| t.rfind('\n'))
            .map_or(0, |i| i + 1);
        self.stable_defs
            .extend(parsed.defs.into_iter().filter(|d| d.at < last_line));
        self.stable.extend(blocks);
        self.stable_end = last_line;
    }

    pub fn held(&self) -> Option<usize> {
        self.held
    }

    pub fn snapshot(&mut self) -> (&Document, Range<usize>) {
        let shown = visible_from(&self.text, self.stable_end);
        self.held = held_from(shown, self.stable_end, self.options.math);
        let visible = self.held.and_then(|at| shown.get(..at)).unwrap_or(shown);
        let tail = block::parse(visible, self.stable_end.min(visible.len()), self.options);
        let refs = block::refs(self.stable_defs.iter().chain(&tail.defs));
        if refs != self.refs {
            self.refs = refs;
            self.resolved = 0;
        }
        let changed = self.resolved;
        self.doc.blocks.truncate(changed);
        let fresh = self.stable.get(changed..).unwrap_or_default();
        self.doc
            .blocks
            .extend(resolve::blocks(fresh, &self.refs, self.options));
        self.resolved = self.stable.len();
        self.doc
            .blocks
            .extend(resolve::blocks(&tail.blocks, &self.refs, self.options));
        (&self.doc, changed..self.resolved)
    }
}
