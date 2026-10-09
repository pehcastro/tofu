use std::ops::Range;

use crate::ast::{Component, Inline, InlineKind, Link};
use crate::block::{LinkRef, Mapped, Refs};
use crate::component;
use crate::scan::{self, is_punct};
use crate::{MAX_NESTING, Options};

pub(crate) fn parse(text: &Mapped, refs: &Refs, options: Options) -> Vec<Inline> {
    let s = text.text.as_str();
    let ws = [' ', '\t', '\n'];
    let start = s.len() - s.trim_start_matches(ws).len();
    let end = s.trim_end_matches(ws).len().max(start);
    let pairs = if options.components && s.contains("</") {
        component::pairs(s, start, end)
    } else {
        component::Pairs::new()
    };
    let cx = Context {
        refs,
        pairs,
        options,
    };
    let (inlines, _) = Parser::new(s, start, end, &cx, 0).run();
    finish(inlines, text, options.gfm, false)
}

struct Context<'a> {
    refs: &'a Refs,
    pairs: component::Pairs,
    options: Options,
}

fn finish(list: Vec<Inline>, text: &Mapped, gfm: bool, in_link: bool) -> Vec<Inline> {
    let mut merged: Vec<Inline> = Vec::with_capacity(list.len());
    for mut inline in list {
        let nested = |c: Vec<Inline>, link: bool| finish(c, text, gfm, in_link || link);
        inline.kind = match inline.kind {
            InlineKind::Emphasis(c) => InlineKind::Emphasis(nested(c, false)),
            InlineKind::Strong(c) => InlineKind::Strong(nested(c, false)),
            InlineKind::Strikethrough(c) => InlineKind::Strikethrough(nested(c, false)),
            InlineKind::Link(l) => InlineKind::Link(Link {
                children: nested(l.children, true),
                ..l
            }),
            InlineKind::Image(l) => InlineKind::Image(Link {
                children: nested(l.children, true),
                ..l
            }),
            InlineKind::Component(c) => InlineKind::Component(Component {
                children: nested(c.children, false),
                ..c
            }),
            other => other,
        };
        match (merged.last_mut(), &inline.kind) {
            (_, InlineKind::Text(t)) if t.is_empty() => {}
            (
                Some(Inline {
                    kind: InlineKind::Text(prev),
                    range,
                }),
                InlineKind::Text(t),
            ) => {
                prev.push_str(t);
                range.end = inline.range.end;
            }
            _ => merged.push(inline),
        }
    }
    let mut out = Vec::with_capacity(merged.len());
    for inline in merged {
        match inline.kind {
            InlineKind::Text(t) if gfm && !in_link => split_autolinks(t, inline.range, &mut out),
            kind => out.push(Inline {
                range: inline.range,
                kind,
            }),
        }
    }
    for inline in &mut out {
        inline.range = text.source_range(&inline.range);
    }
    out
}

fn split_autolinks(t: String, range: Range<usize>, out: &mut Vec<Inline>) {
    let links = scan::extended_autolinks(&t);
    if links.is_empty() {
        out.push(Inline {
            range,
            kind: InlineKind::Text(t),
        });
        return;
    }
    let exact = t.len() == range.len();
    let at = |r: Range<usize>| {
        if exact {
            range.start + r.start..range.start + r.end
        } else {
            range.clone()
        }
    };
    let mut from = 0;
    for (r, destination) in links {
        if r.start > from {
            out.push(Inline {
                range: at(from..r.start),
                kind: InlineKind::Text(t.get(from..r.start).unwrap_or_default().into()),
            });
        }
        let text = t.get(r.clone()).unwrap_or_default().to_string();
        from = r.end;
        out.push(Inline {
            range: at(r),
            kind: InlineKind::Autolink { destination, text },
        });
    }
    if from < t.len() {
        out.push(Inline {
            range: at(from..t.len()),
            kind: InlineKind::Text(t.get(from..).unwrap_or_default().into()),
        });
    }
}

struct Slot {
    inline: Option<Inline>,
    depth: usize,
    prev: Option<usize>,
    next: Option<usize>,
}

#[derive(Clone, Copy)]
struct Delim {
    slot: usize,
    ch: u8,
    num: usize,
    orig: usize,
    open: bool,
    close: bool,
    prev: Option<usize>,
    next: Option<usize>,
}

#[derive(Clone, Copy)]
struct Bracket {
    slot: usize,
    prev_delim: Option<usize>,
    index: usize,
    image: bool,
    active: bool,
    bracket_after: bool,
}

struct Parser<'a> {
    s: &'a str,
    b: &'a [u8],
    pos: usize,
    end: usize,
    cx: &'a Context<'a>,
    depth: usize,
    slots: Vec<Slot>,
    head: Option<usize>,
    tail: Option<usize>,
    delims: Vec<Delim>,
    top: Option<usize>,
    brackets: Vec<Bracket>,
}

impl<'a> Parser<'a> {
    fn new(s: &'a str, pos: usize, end: usize, cx: &'a Context<'a>, depth: usize) -> Self {
        Self {
            s,
            b: s.as_bytes(),
            pos,
            end,
            cx,
            depth,
            slots: Vec::new(),
            head: None,
            tail: None,
            delims: Vec::new(),
            top: None,
            brackets: Vec::new(),
        }
    }

    fn run(mut self) -> (Vec<Inline>, usize) {
        while self.pos < self.end {
            self.step();
        }
        self.process_emphasis(None);
        let mut out = Vec::new();
        let mut depth = 0;
        let mut cur = self.head;
        while let Some(i) = cur {
            let slot = &mut self.slots[i];
            depth = depth.max(slot.depth);
            out.extend(slot.inline.take());
            cur = slot.next;
        }
        (out, depth)
    }

    fn peek(&self, i: usize) -> Option<u8> {
        if i < self.end {
            self.b.get(i).copied()
        } else {
            None
        }
    }

    fn step(&mut self) {
        let Some(c) = self.peek(self.pos) else {
            self.pos = self.end;
            return;
        };
        match c {
            b'\n' => self.newline(),
            b'\\' => self.backslash(),
            b'`' => self.code_span(),
            b'*' | b'_' => self.delim(c),
            b'~' if self.cx.options.gfm => self.delim(c),
            b'[' => {
                let slot = self.text(self.pos, self.pos + 1);
                self.open_bracket(slot, self.pos, false);
                self.pos += 1;
            }
            b'!' if self.peek(self.pos + 1) == Some(b'[') => {
                let slot = self.text(self.pos, self.pos + 2);
                self.open_bracket(slot, self.pos + 1, true);
                self.pos += 2;
            }
            b']' => self.close_bracket(),
            b'<' => self.angle(),
            b'&' => match scan::entity(self.s.get(self.pos..self.end).unwrap_or_default()) {
                Some((text, len)) => {
                    self.push(InlineKind::Text(text), self.pos..self.pos + len, 1);
                    self.pos += len;
                }
                None => {
                    self.text(self.pos, self.pos + 1);
                    self.pos += 1;
                }
            },
            _ => self.string(),
        }
    }

    fn string(&mut self) {
        let start = self.pos;
        let first = self
            .s
            .get(start..)
            .and_then(|r| r.chars().next())
            .map_or(1, char::len_utf8);
        let gfm = self.cx.options.gfm;
        let mut i = start + first;
        while let Some(c) = self.peek(i) {
            if matches!(
                c,
                b'\n' | b'\\' | b'`' | b'*' | b'_' | b'[' | b']' | b'!' | b'<' | b'&'
            ) || (gfm && c == b'~')
            {
                break;
            }
            i += 1;
        }
        self.text(start, i);
        self.pos = i;
    }

    fn push(&mut self, kind: InlineKind, range: Range<usize>, depth: usize) -> usize {
        let id = self.slots.len();
        self.slots.push(Slot {
            inline: Some(Inline { range, kind }),
            depth,
            prev: self.tail,
            next: None,
        });
        match self.tail {
            Some(t) => self.slots[t].next = Some(id),
            None => self.head = Some(id),
        }
        self.tail = Some(id);
        id
    }

    fn text(&mut self, a: usize, b: usize) -> usize {
        let t = self.s.get(a..b).unwrap_or_default().to_string();
        self.push(InlineKind::Text(t), a..b, 1)
    }

    fn unlink(&mut self, i: usize) {
        let (prev, next) = (self.slots[i].prev, self.slots[i].next);
        match prev {
            Some(p) => self.slots[p].next = next,
            None => self.head = next,
        }
        match next {
            Some(n) => self.slots[n].prev = prev,
            None => self.tail = prev,
        }
    }

    fn depth_between(&self, a: usize, stop: Option<usize>) -> usize {
        let mut depth = 0;
        let mut cur = self.slots[a].next;
        while cur != stop
            && let Some(i) = cur
        {
            depth = depth.max(self.slots[i].depth);
            cur = self.slots[i].next;
        }
        depth
    }

    fn take_between(&mut self, a: usize, stop: Option<usize>) -> Vec<Inline> {
        let mut out = Vec::new();
        let mut cur = self.slots[a].next;
        while cur != stop
            && let Some(i) = cur
        {
            out.extend(self.slots[i].inline.take());
            cur = self.slots[i].next;
        }
        self.slots[a].next = stop;
        match stop {
            Some(b) => self.slots[b].prev = Some(a),
            None => self.tail = Some(a),
        }
        out
    }

    fn slot_range(&self, i: usize) -> Range<usize> {
        self.slots[i]
            .inline
            .as_ref()
            .map_or(0..0, |x| x.range.clone())
    }

    fn newline(&mut self) {
        let at = self.pos;
        self.pos += 1;
        let mut hard = false;
        if let Some(t) = self.tail
            && let Some(Inline {
                kind: InlineKind::Text(s),
                range,
            }) = &mut self.slots[t].inline
            && s.ends_with(' ')
        {
            hard = s.ends_with("  ");
            let kept = s.trim_end_matches(' ').len();
            range.end -= s.len() - kept;
            s.truncate(kept);
        }
        self.push(
            if hard {
                InlineKind::HardBreak
            } else {
                InlineKind::SoftBreak
            },
            at..at + 1,
            1,
        );
        while matches!(self.peek(self.pos), Some(b' ' | b'\t')) {
            self.pos += 1;
        }
    }

    fn backslash(&mut self) {
        let at = self.pos;
        match self.peek(at + 1) {
            Some(b'\n') => {
                self.push(InlineKind::HardBreak, at..at + 2, 1);
                self.pos = at + 2;
                while matches!(self.peek(self.pos), Some(b' ' | b'\t')) {
                    self.pos += 1;
                }
            }
            Some(c) if c.is_ascii_punctuation() => {
                self.push(InlineKind::Text(char::from(c).to_string()), at..at + 2, 1);
                self.pos = at + 2;
            }
            _ => {
                self.text(at, at + 1);
                self.pos = at + 1;
            }
        }
    }

    fn run_len(&self, at: usize, c: u8) -> usize {
        (at..self.end)
            .take_while(|&i| self.b.get(i) == Some(&c))
            .count()
    }

    fn code_span(&mut self) {
        let start = self.pos;
        let n = self.run_len(start, b'`');
        let after = start + n;
        let mut i = after;
        while let Some(off) = self
            .b
            .get(i..self.end)
            .and_then(|r| r.iter().position(|&c| c == b'`'))
        {
            let j = i + off;
            let m = self.run_len(j, b'`');
            if m == n {
                let raw = self.s.get(after..j).unwrap_or_default().replace('\n', " ");
                let strip = raw.len() >= 2
                    && raw.starts_with(' ')
                    && raw.ends_with(' ')
                    && raw.bytes().any(|c| c != b' ');
                let code = if strip {
                    raw.get(1..raw.len() - 1).unwrap_or_default().to_string()
                } else {
                    raw
                };
                self.push(InlineKind::Code(code), start..j + n, 1);
                self.pos = j + n;
                return;
            }
            i = j + m;
        }
        self.text(start, after);
        self.pos = after;
    }

    fn delim(&mut self, c: u8) {
        let start = self.pos;
        let n = self.run_len(start, c);
        let before = self
            .s
            .get(..start)
            .and_then(|r| r.chars().next_back())
            .unwrap_or('\n');
        let after = self
            .s
            .get(start + n..self.end)
            .and_then(|r| r.chars().next())
            .unwrap_or('\n');
        let (after_ws, after_p) = (after.is_whitespace(), is_punct(after));
        let (before_ws, before_p) = (before.is_whitespace(), is_punct(before));
        let left = !after_ws && (!after_p || before_ws || before_p);
        let right = !before_ws && (!before_p || after_ws || after_p);
        let (open, close) = match c {
            b'_' => (left && (!right || before_p), right && (!left || after_p)),
            b'~' if n > 2 => (false, false),
            _ => (left, right),
        };
        self.pos = start + n;
        let slot = self.text(start, start + n);
        if open || close {
            let id = self.delims.len();
            self.delims.push(Delim {
                slot,
                ch: c,
                num: n,
                orig: n,
                open,
                close,
                prev: self.top,
                next: None,
            });
            if let Some(t) = self.top {
                self.delims[t].next = Some(id);
            }
            self.top = Some(id);
        }
    }

    fn remove_delim(&mut self, d: usize) {
        let (prev, next) = (self.delims[d].prev, self.delims[d].next);
        if let Some(p) = prev {
            self.delims[p].next = next;
        }
        match next {
            Some(n) => self.delims[n].prev = prev,
            None => self.top = prev,
        }
    }

    fn shrink(&mut self, slot: usize, by: usize, from_end: bool) {
        if let Some(Inline {
            kind: InlineKind::Text(t),
            range,
        }) = &mut self.slots[slot].inline
        {
            t.truncate(t.len().saturating_sub(by));
            if from_end {
                range.end -= by;
            } else {
                range.start += by;
            }
        }
    }

    fn process_emphasis(&mut self, bottom: Option<usize>) {
        let mut openers_bottom = [bottom; 14];
        let mut closer = self.top;
        while let Some(c) = closer
            && self.delims[c].prev != bottom
        {
            closer = self.delims[c].prev;
        }
        while let Some(c) = closer {
            let d = self.delims[c];
            if !d.close {
                closer = d.next;
                continue;
            }
            let index = match d.ch {
                b'~' => 12 + usize::from(d.orig == 2),
                ch => usize::from(ch == b'_') * 6 + usize::from(d.open) * 3 + d.orig % 3,
            };
            let mut opener = d.prev;
            let mut found = None;
            while let Some(o) = opener
                && opener != bottom
                && opener != openers_bottom[index]
            {
                let od = &self.delims[o];
                let odd = d.ch != b'~'
                    && (d.open || od.close)
                    && !d.orig.is_multiple_of(3)
                    && (od.orig + d.orig).is_multiple_of(3);
                let sized = d.ch != b'~' || od.num == d.num;
                if od.ch == d.ch && od.open && !odd && sized {
                    found = Some(o);
                    break;
                }
                opener = od.prev;
            }
            let found = found
                .filter(|&o| self.depth_between(self.delims[o].slot, Some(d.slot)) < MAX_NESTING);
            let Some(o) = found else {
                openers_bottom[index] = d.prev;
                closer = d.next;
                if !d.open {
                    self.remove_delim(c);
                }
                continue;
            };
            let (os, cs) = (self.delims[o].slot, d.slot);
            let used = match d.ch {
                b'~' => d.num,
                _ if d.num >= 2 && self.delims[o].num >= 2 => 2,
                _ => 1,
            };
            let ch = d.ch;
            self.delims[o].num -= used;
            self.delims[c].num -= used;
            self.shrink(os, used, true);
            self.shrink(cs, used, false);
            let depth = self.depth_between(os, Some(cs)) + 1;
            let children = self.take_between(os, Some(cs));
            let range = self.slot_range(os).end..self.slot_range(cs).start;
            let kind = match (ch, used) {
                (b'~', _) => InlineKind::Strikethrough(children),
                (_, 1) => InlineKind::Emphasis(children),
                _ => InlineKind::Strong(children),
            };
            let id = self.slots.len();
            self.slots.push(Slot {
                inline: Some(Inline { range, kind }),
                depth,
                prev: Some(os),
                next: Some(cs),
            });
            self.slots[os].next = Some(id);
            self.slots[cs].prev = Some(id);
            self.delims[o].next = Some(c);
            self.delims[c].prev = Some(o);
            if self.delims[o].num == 0 {
                self.unlink(os);
                self.remove_delim(o);
            }
            if self.delims[c].num == 0 {
                self.unlink(cs);
                closer = self.delims[c].next;
                self.remove_delim(c);
            }
        }
        while let Some(t) = self.top
            && Some(t) != bottom
        {
            self.remove_delim(t);
        }
    }

    fn open_bracket(&mut self, slot: usize, index: usize, image: bool) {
        if let Some(last) = self.brackets.last_mut() {
            last.bracket_after = true;
        }
        self.brackets.push(Bracket {
            slot,
            prev_delim: self.top,
            index,
            image,
            active: true,
            bracket_after: false,
        });
    }

    fn inline_target(&self, at: usize) -> Option<(LinkRef, usize)> {
        let (b, end) = (self.b, self.end);
        if self.peek(at) != Some(b'(') {
            return None;
        }
        let (destination, after_dest) =
            scan::link_destination(self.s, scan::spnl(b, at + 1, end), end)?;
        let gap = scan::spnl(b, after_dest, end);
        let (title, after_title) = (gap > after_dest)
            .then(|| scan::link_title(self.s, gap, end))
            .flatten()
            .unwrap_or((String::new(), gap));
        let close = scan::spnl(b, after_title, end);
        (self.peek(close) == Some(b')')).then_some((LinkRef { destination, title }, close + 1))
    }

    fn close_bracket(&mut self) {
        let start = self.pos;
        let after = start + 1;
        self.pos = after;
        let Some(opener) = self.brackets.last().copied() else {
            self.text(start, after);
            return;
        };
        if !opener.active {
            self.text(start, after);
            self.brackets.pop();
            return;
        }
        let mut target = self.inline_target(after);
        if target.is_none() {
            let n = scan::link_label(self.b, after, self.end).unwrap_or(0);
            let label = if n > 2 {
                self.s.get(after..after + n)
            } else if !opener.bracket_after {
                self.s.get(opener.index..after)
            } else {
                None
            };
            target = label
                .and_then(|l| self.cx.refs.get(&scan::normalize_label(l)))
                .map(|link| (link.clone(), after + n));
        }
        let too_deep = target.is_some() && self.depth_between(opener.slot, None) >= MAX_NESTING;
        let Some((link, end)) = target.filter(|_| !too_deep) else {
            self.brackets.pop();
            if too_deep {
                self.brackets.iter_mut().for_each(|b| b.active = false);
            }
            self.text(start, after);
            return;
        };
        self.pos = end;
        self.process_emphasis(opener.prev_delim);
        let depth = self.depth_between(opener.slot, None) + 1;
        let children = self.take_between(opener.slot, None);
        let link = Link {
            destination: link.destination,
            title: link.title,
            children,
        };
        let range = self.slot_range(opener.slot).start..end;
        let kind = if opener.image {
            InlineKind::Image(link)
        } else {
            InlineKind::Link(link)
        };
        let slot = &mut self.slots[opener.slot];
        slot.inline = Some(Inline { range, kind });
        slot.depth = depth;
        self.brackets.pop();
        if !opener.image {
            for b in &mut self.brackets {
                if !b.image {
                    b.active = false;
                }
            }
        }
    }

    fn angle(&mut self) {
        let at = self.pos;
        let rest = self.s.get(at..self.end).unwrap_or_default();
        if let Some((destination, text, len)) = scan::autolink(rest) {
            self.push(InlineKind::Autolink { destination, text }, at..at + len, 1);
            self.pos = at + len;
        } else if self.cx.options.components
            && let Some(len) = self.component(rest)
        {
            self.pos = at + len;
        } else if let Some(len) = scan::html_inline(rest.as_bytes()) {
            self.push(
                InlineKind::Html(rest.get(..len).unwrap_or_default().to_string()),
                at..at + len,
                1,
            );
            self.pos = at + len;
        } else {
            self.text(at, at + 1);
            self.pos = at + 1;
        }
    }

    fn component(&mut self, rest: &str) -> Option<usize> {
        let at = self.pos;
        let tag = component::open_tag(rest)?;
        let (children, depth, len) = if tag.self_closing {
            (Vec::new(), 1, tag.len)
        } else {
            if self.depth + 1 >= MAX_NESTING {
                return None;
            }
            let close = self.cx.pairs.get(&at).filter(|c| c.end <= self.end)?;
            let (children, depth) =
                Parser::new(self.s, at + tag.len, close.start, self.cx, self.depth + 1).run();
            (children, depth + 1, close.end - at)
        };
        let kind = InlineKind::Component(Component {
            name: tag.name,
            props: tag.props,
            children,
        });
        self.push(kind, at..at + len, depth);
        Some(len)
    }
}
