use crate::ast::Document;
use crate::block::{self, Def, RawBlock, Refs};
use crate::{Options, resolve};

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
    let line = text.rfind('\n').map_or(0, |i| i + 1);
    let tail = text.get(line..).unwrap_or_default();
    let open = tail.match_indices('<').map(|(i, _)| i).find(|&i| {
        tail.as_bytes()
            .get(i + 1)
            .is_some_and(u8::is_ascii_uppercase)
            && !tail.get(i..).unwrap_or_default().contains('>')
    });
    match open {
        Some(i) => text.get(..line + i).unwrap_or(text),
        None => text,
    }
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
        let complete = self.text.rfind('\n').map_or(0, |i| i + 1);
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

    pub fn snapshot(&mut self) -> &Document {
        let visible = visible_prefix(&self.text);
        let tail = block::parse(visible, self.stable_end.min(visible.len()), self.options);
        let refs = block::refs(self.stable_defs.iter().chain(&tail.defs));
        if refs != self.refs {
            self.refs = refs;
            self.resolved = 0;
        }
        self.doc.blocks.truncate(self.resolved);
        let fresh = self.stable.get(self.resolved..).unwrap_or_default();
        self.doc
            .blocks
            .extend(resolve::blocks(fresh, &self.refs, self.options));
        self.resolved = self.stable.len();
        self.doc
            .blocks
            .extend(resolve::blocks(&tail.blocks, &self.refs, self.options));
        &self.doc
    }
}
