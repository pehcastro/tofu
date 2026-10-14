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

    pub fn snapshot(&mut self) -> (&Document, Range<usize>) {
        let visible = visible_from(&self.text, self.stable_end);
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
