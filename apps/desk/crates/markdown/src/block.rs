use std::collections::HashMap;
use std::ops::Range;

use crate::ast::{Alignment, CodeKind, ListKind};
use crate::component::{self, PropValue};
use crate::scan::{self, is_blank, is_space_tab};
use crate::{MAX_NESTING, Options};

#[derive(Debug, Clone, Default, PartialEq)]
pub(crate) struct Mapped {
    pub text: String,
    segs: Vec<(usize, usize)>,
}

impl Mapped {
    pub fn push(&mut self, s: &str, at: usize) {
        self.segs.push((self.text.len(), at));
        self.text.push_str(s);
    }

    pub fn source(&self, p: usize) -> usize {
        let i = self.segs.partition_point(|&(c, _)| c <= p);
        match i.checked_sub(1).and_then(|i| self.segs.get(i)) {
            Some(&(c, s)) => s + (p - c),
            None => p,
        }
    }

    pub fn source_range(&self, r: &Range<usize>) -> Range<usize> {
        let start = self.source(r.start);
        let end = if r.end > r.start {
            self.source(r.end - 1) + 1
        } else {
            start
        };
        start..end
    }

    fn drop_front(&mut self, n: usize) {
        let at = self.source(n);
        self.text.drain(..n);
        self.segs.retain(|&(c, _)| c > n);
        for seg in &mut self.segs {
            seg.0 -= n;
        }
        self.segs.insert(0, (0, at));
    }
}

#[derive(Debug, Clone, PartialEq)]
pub(crate) struct LinkRef {
    pub destination: String,
    pub title: String,
}

pub(crate) type Refs = HashMap<String, LinkRef>;

#[derive(Debug, Clone)]
pub(crate) struct Def {
    pub label: String,
    pub link: LinkRef,
    pub at: usize,
}

pub(crate) fn refs<'a>(defs: impl IntoIterator<Item = &'a Def>) -> Refs {
    let mut map = Refs::new();
    for def in defs {
        map.entry(def.label.clone())
            .or_insert_with(|| def.link.clone());
    }
    map
}

#[derive(Debug, Clone)]
pub(crate) struct RawItem {
    pub range: Range<usize>,
    pub task: Option<bool>,
    pub blocks: Vec<RawBlock>,
}

#[derive(Debug, Clone)]
pub(crate) enum RawKind {
    BlockQuote,
    List {
        kind: ListKind,
        tight: bool,
        items: Vec<RawItem>,
    },
    Paragraph(Mapped),
    Heading(u8, Mapped),
    ThematicBreak,
    Code(CodeKind, String),
    Html(String),
    Table {
        alignments: Vec<Alignment>,
        header: (usize, String),
        rows: Vec<(usize, String)>,
    },
    Component {
        name: String,
        props: Vec<(String, PropValue)>,
    },
}

#[derive(Debug, Clone)]
pub(crate) struct RawBlock {
    pub kind: RawKind,
    pub range: Range<usize>,
    pub children: Vec<RawBlock>,
}

pub(crate) struct Parsed {
    pub blocks: Vec<RawBlock>,
    pub defs: Vec<Def>,
}

#[derive(Debug, Clone, Copy)]
struct Marker {
    kind: ListKind,
    offset: usize,
    padding: usize,
    tight: bool,
}

#[derive(Debug, Clone, Copy)]
struct Fence {
    ch: u8,
    len: usize,
    offset: usize,
}

#[derive(Debug, Default)]
enum Kind {
    #[default]
    Document,
    BlockQuote,
    List(Marker),
    Item(Marker),
    Paragraph,
    Heading(u8),
    ThematicBreak,
    Code(Option<Fence>),
    Html(u8),
    Table(Vec<Alignment>, (usize, String)),
    Component(String, Vec<(String, PropValue)>),
}

#[derive(Debug, Default)]
struct Node {
    kind: Kind,
    parent: usize,
    children: Vec<usize>,
    open: bool,
    start: usize,
    end: usize,
    start_line: usize,
    end_line: usize,
    depth: usize,
    text: Mapped,
    rows: Vec<(usize, String)>,
}

enum Cont {
    Yes,
    No,
    Done,
}

enum Start {
    None,
    Container,
    Leaf,
}

struct Parser<'a> {
    src: &'a str,
    options: Options,
    nodes: Vec<Node>,
    tip: usize,
    old_tip: usize,
    last_matched: usize,
    all_closed: bool,
    line: &'a str,
    line_start: usize,
    line_no: usize,
    prev_line_end: usize,
    offset: usize,
    column: usize,
    next_nonspace: usize,
    next_nonspace_column: usize,
    indent: usize,
    indented: bool,
    blank: bool,
    partial_tab: bool,
    defs: Vec<Def>,
}

pub(crate) fn parse(src: &str, from: usize, options: Options) -> Parsed {
    let root = Node {
        open: true,
        start: from,
        ..Node::default()
    };
    let mut p = Parser {
        src,
        options,
        nodes: vec![root],
        tip: 0,
        old_tip: 0,
        last_matched: 0,
        all_closed: true,
        line: "",
        line_start: from,
        line_no: 0,
        prev_line_end: from,
        offset: 0,
        column: 0,
        next_nonspace: 0,
        next_nonspace_column: 0,
        indent: 0,
        indented: false,
        blank: false,
        partial_tab: false,
        defs: Vec::new(),
    };
    let mut pos = from;
    while let Some(rest) = src.get(pos..).filter(|r| !r.is_empty()) {
        let len = rest.find(['\n', '\r']).unwrap_or(rest.len());
        let newline = if rest.get(len..).is_some_and(|r| r.starts_with("\r\n")) {
            2
        } else {
            usize::from(len < rest.len())
        };
        p.incorporate(pos, len);
        pos += len + newline;
    }
    p.finish()
}

pub(crate) fn split_row(s: &str) -> Vec<Range<usize>> {
    let b = s.as_bytes();
    let ws = |c: &&u8| matches!(c, b' ' | b'\t');
    let mut start = b.iter().take_while(ws).count();
    let mut end = b.len() - b.iter().rev().take_while(ws).count();
    if start < end && b.get(start) == Some(&b'|') {
        start += 1;
    }
    if end > start && b.get(end - 1) == Some(&b'|') && b.get(end.wrapping_sub(2)) != Some(&b'\\') {
        end -= 1;
    }
    let mut cells = Vec::new();
    let mut cell = start;
    let mut i = start;
    while i < end {
        match b.get(i) {
            Some(b'\\') => i += 1,
            Some(b'|') => {
                cells.push(cell..i);
                cell = i + 1;
            }
            _ => {}
        }
        i += 1;
    }
    cells.push(cell..end);
    cells
        .into_iter()
        .map(|r| {
            let lead = b
                .get(r.clone())
                .map_or(0, |c| c.iter().take_while(ws).count());
            let trail = b
                .get(r.clone())
                .map_or(0, |c| c.iter().rev().take_while(ws).count());
            (r.start + lead)..(r.end - trail).max(r.start + lead)
        })
        .collect()
}

fn delimiter_row(s: &str) -> Option<Vec<Alignment>> {
    split_row(s)
        .into_iter()
        .map(|r| {
            let cell = s.get(r)?;
            let dashes = cell.trim_start_matches(':').trim_end_matches(':');
            if dashes.is_empty()
                || !dashes.bytes().all(|c| c == b'-')
                || cell.len() > dashes.len() + 2
            {
                return None;
            }
            Some(match (cell.starts_with(':'), cell.ends_with(':')) {
                (true, true) => Alignment::Center,
                (true, false) => Alignment::Left,
                (false, true) => Alignment::Right,
                (false, false) => Alignment::None,
            })
        })
        .collect()
}

fn atx_level(rest: &str) -> Option<u8> {
    let hashes = rest.bytes().take_while(|&c| c == b'#').count();
    let followed = matches!(rest.as_bytes().get(hashes), None | Some(b' ' | b'\t'));
    ((1..=6).contains(&hashes) && followed)
        .then(|| u8::try_from(hashes).ok())
        .flatten()
}

fn atx_content(raw: &str) -> &str {
    let trimmed = raw.trim_end_matches([' ', '\t']);
    let without = trimmed.trim_end_matches('#');
    if without.len() < trimmed.len() && (without.is_empty() || without.ends_with([' ', '\t'])) {
        without
    } else {
        trimmed
    }
}

fn setext_level(rest: &str) -> Option<u8> {
    let body = rest.trim_end_matches([' ', '\t']);
    match body.as_bytes().first() {
        Some(b'=') if body.bytes().all(|c| c == b'=') => Some(1),
        Some(b'-') if body.bytes().all(|c| c == b'-') => Some(2),
        _ => None,
    }
}

fn is_thematic(rest: &str) -> bool {
    let Some(&ch) = rest
        .as_bytes()
        .first()
        .filter(|c| matches!(c, b'*' | b'_' | b'-'))
    else {
        return false;
    };
    rest.bytes().all(|c| c == ch || c == b' ' || c == b'\t')
        && rest.bytes().filter(|&c| c == ch).count() >= 3
}

impl Parser<'_> {
    fn peek(&self, i: usize) -> Option<u8> {
        self.line.as_bytes().get(i).copied()
    }

    fn find_next_nonspace(&mut self) {
        let mut i = self.offset;
        let mut cols = self.column;
        while let Some(c) = self.peek(i) {
            match c {
                b' ' => cols += 1,
                b'\t' => cols += 4 - cols % 4,
                _ => break,
            }
            i += 1;
        }
        self.blank = i >= self.line.len();
        self.next_nonspace = i;
        self.next_nonspace_column = cols;
        self.indent = cols - self.column;
        self.indented = self.indent >= 4;
    }

    fn advance_next_nonspace(&mut self) {
        self.offset = self.next_nonspace;
        self.column = self.next_nonspace_column;
        self.partial_tab = false;
    }

    fn advance_offset(&mut self, mut count: usize, columns: bool) {
        while count > 0
            && let Some(c) = self.peek(self.offset)
        {
            if c == b'\t' {
                let to_tab = 4 - self.column % 4;
                if columns {
                    self.partial_tab = to_tab > count;
                    let step = to_tab.min(count);
                    self.column += step;
                    self.offset += usize::from(!self.partial_tab);
                    count -= step;
                } else {
                    self.partial_tab = false;
                    self.column += to_tab;
                    self.offset += 1;
                    count -= 1;
                }
            } else {
                self.partial_tab = false;
                self.offset += 1;
                self.column += 1;
                count -= 1;
            }
        }
    }

    fn accepts_lines(&self, n: usize) -> bool {
        matches!(
            self.nodes[n].kind,
            Kind::Paragraph | Kind::Code(_) | Kind::Html(_) | Kind::Table(..)
        )
    }

    fn add_line(&mut self) {
        let n = self.tip;
        if let Kind::Table(..) = self.nodes[n].kind {
            if let Some(row) = self.line.get(self.offset..).filter(|r| !r.is_empty()) {
                self.nodes[n]
                    .rows
                    .push((self.line_start + self.offset, row.to_string()));
            }
            return;
        }
        if self.partial_tab {
            self.offset += 1;
            let spaces = "    ".get(..4 - self.column % 4).unwrap_or_default();
            let at = self.line_start + self.offset - 1;
            self.nodes[n].text.push(spaces, at);
        }
        let rest = self.line.get(self.offset..).unwrap_or_default();
        let at = self.line_start + self.offset;
        let text = &mut self.nodes[n].text;
        text.push(rest, at);
        text.text.push('\n');
    }

    fn add_child(&mut self, kind: Kind, offset: usize) -> usize {
        while !self.can_contain(self.tip, &kind) {
            self.close_prev(self.tip);
        }
        let parent = self.tip;
        let id = self.nodes.len();
        self.nodes.push(Node {
            kind,
            parent,
            open: true,
            start: self.line_start + offset,
            start_line: self.line_no,
            depth: self.nodes[parent].depth + 1,
            ..Node::default()
        });
        self.nodes[parent].children.push(id);
        self.tip = id;
        id
    }

    fn can_contain(&self, parent: usize, child: &Kind) -> bool {
        match self.nodes[parent].kind {
            Kind::Document | Kind::BlockQuote | Kind::Item(_) | Kind::Component(..) => {
                !matches!(child, Kind::Item(_))
            }
            Kind::List(_) => matches!(child, Kind::Item(_)),
            _ => false,
        }
    }

    fn close_prev(&mut self, n: usize) {
        self.finalize(n, self.line_no.saturating_sub(1), self.prev_line_end);
    }

    fn close_here(&mut self, n: usize) {
        self.finalize(n, self.line_no, self.line_start + self.line.len());
    }

    fn close_unmatched(&mut self) {
        if !self.all_closed {
            while self.old_tip != self.last_matched {
                let parent = self.nodes[self.old_tip].parent;
                self.close_prev(self.old_tip);
                self.old_tip = parent;
            }
            self.all_closed = true;
        }
    }

    fn take_defs(&mut self, n: usize) {
        while self.nodes[n].text.text.starts_with('[') {
            let Some(def) = scan::link_def(&self.nodes[n].text.text) else {
                break;
            };
            let at = self.nodes[n].text.source(0);
            self.defs.push(Def {
                label: def.label,
                link: LinkRef {
                    destination: def.destination,
                    title: def.title,
                },
                at,
            });
            self.nodes[n].text.drop_front(def.len);
        }
    }

    fn trim_trailing_blank_lines(&mut self, n: usize) {
        let node = &mut self.nodes[n];
        let mut keep = 0;
        let mut at = 0;
        let mut kept_lines = 0usize;
        for (i, line) in node.text.text.split_inclusive('\n').enumerate() {
            at += line.len();
            if !is_blank(line) {
                kept_lines = i + 1;
                keep = at;
            }
        }
        if keep > 0 {
            node.text.text.truncate(keep);
            node.end_line = node.start_line + kept_lines.saturating_sub(1);
            node.end = node.text.source(keep - 1);
        }
    }

    fn finalize(&mut self, n: usize, line: usize, end: usize) {
        let node = &mut self.nodes[n];
        node.open = false;
        node.end_line = line;
        node.end = end.max(node.start);
        let parent = node.parent;
        match node.kind {
            Kind::Paragraph => {
                self.take_defs(n);
                if is_blank(&self.nodes[n].text.text) {
                    self.nodes[parent].children.retain(|&c| c != n);
                }
            }
            Kind::Code(None) | Kind::Html(_) => self.trim_trailing_blank_lines(n),
            Kind::Item(_) => {
                if let Some(&last) = self.nodes[n].children.last() {
                    let (end, end_line) = (self.nodes[last].end, self.nodes[last].end_line);
                    let node = &mut self.nodes[n];
                    node.end = end;
                    node.end_line = end_line;
                } else {
                    let node = &mut self.nodes[n];
                    node.end_line = node.start_line;
                }
            }
            Kind::List(_) => self.finalize_list(n),
            _ => {}
        }
        self.tip = parent;
    }

    fn finalize_list(&mut self, n: usize) {
        let nodes = &self.nodes;
        let gap = |a: usize, b: usize| nodes[b].start_line > nodes[a].end_line + 1;
        let siblings_gap =
            |list: &[usize]| list.windows(2).any(|w| matches!(w, [a, b] if gap(*a, *b)));
        let items = &nodes[n].children;
        let loose = siblings_gap(items) || items.iter().any(|&i| siblings_gap(&nodes[i].children));
        let last = items.last().map(|&l| (nodes[l].end, nodes[l].end_line));
        let node = &mut self.nodes[n];
        if let Kind::List(marker) = &mut node.kind {
            marker.tight = !loose;
        }
        if let Some((end, end_line)) = last {
            node.end = end;
            node.end_line = end_line;
        }
    }

    fn continues(&mut self, n: usize) -> Cont {
        let nns = self.next_nonspace;
        let rest = self.line.get(nns..).unwrap_or_default();
        match &self.nodes[n].kind {
            Kind::Document | Kind::List(_) => Cont::Yes,
            Kind::BlockQuote => {
                if self.indented || self.peek(nns) != Some(b'>') {
                    return Cont::No;
                }
                self.advance_next_nonspace();
                self.advance_offset(1, false);
                if is_space_tab(self.peek(self.offset)) {
                    self.advance_offset(1, true);
                }
                Cont::Yes
            }
            Kind::Item(marker) => {
                let width = marker.offset + marker.padding;
                if self.blank {
                    if self.nodes[n].children.is_empty() {
                        return Cont::No;
                    }
                    self.advance_next_nonspace();
                } else if self.indent >= width {
                    self.advance_offset(width, true);
                } else {
                    return Cont::No;
                }
                Cont::Yes
            }
            Kind::Heading(_) | Kind::ThematicBreak => Cont::No,
            Kind::Code(Some(fence)) => {
                let fence = *fence;
                let run = rest.bytes().take_while(|&c| c == fence.ch).count();
                if self.indent <= 3
                    && run >= fence.len
                    && is_blank(rest.get(run..).unwrap_or_default())
                {
                    self.close_here(n);
                    return Cont::Done;
                }
                let mut i = fence.offset;
                while i > 0 && is_space_tab(self.peek(self.offset)) {
                    self.advance_offset(1, true);
                    i -= 1;
                }
                Cont::Yes
            }
            Kind::Code(None) => {
                if self.indent >= 4 {
                    self.advance_offset(4, true);
                } else if self.blank {
                    self.advance_next_nonspace();
                } else {
                    return Cont::No;
                }
                Cont::Yes
            }
            Kind::Html(kind) => {
                if self.blank && *kind >= 6 {
                    Cont::No
                } else {
                    Cont::Yes
                }
            }
            Kind::Paragraph | Kind::Table(..) => {
                if self.blank {
                    Cont::No
                } else {
                    Cont::Yes
                }
            }
            Kind::Component(name, _) => {
                if self.indented
                    || component::close_tag(rest, name)
                        .is_none_or(|len| !is_blank(rest.get(len..).unwrap_or_default()))
                {
                    return Cont::Yes;
                }
                while self.tip != n {
                    self.close_here(self.tip);
                }
                self.close_here(n);
                Cont::Done
            }
        }
    }

    fn incorporate(&mut self, start: usize, len: usize) {
        self.line = self.src.get(start..start + len).unwrap_or_default();
        self.line_start = start;
        self.line_no += 1;
        self.offset = 0;
        self.column = 0;
        self.blank = false;
        self.partial_tab = false;
        self.old_tip = self.tip;
        let mut container = 0;
        while let Some(&last) = self.nodes[container].children.last()
            && self.nodes[last].open
        {
            self.find_next_nonspace();
            match self.continues(last) {
                Cont::Yes => container = last,
                Cont::No => break,
                Cont::Done => {
                    self.prev_line_end = start + len;
                    return;
                }
            }
        }
        self.all_closed = container == self.old_tip;
        self.last_matched = container;
        let mut matched_leaf = self.accepts_lines(container)
            && !matches!(
                self.nodes[container].kind,
                Kind::Paragraph | Kind::Table(..)
            );
        while !matched_leaf {
            self.find_next_nonspace();
            let special = matches!(
                self.peek(self.next_nonspace),
                Some(
                    b'#' | b'`'
                        | b'~'
                        | b'*'
                        | b'+'
                        | b'_'
                        | b'='
                        | b'<'
                        | b'>'
                        | b'-'
                        | b'|'
                        | b':'
                        | b'0'..=b'9'
                )
            );
            if !self.indented && !special {
                self.advance_next_nonspace();
                break;
            }
            match self.start(container) {
                Start::None => {
                    self.advance_next_nonspace();
                    break;
                }
                Start::Container => container = self.tip,
                Start::Leaf => {
                    container = self.tip;
                    matched_leaf = true;
                }
            }
        }
        if !self.all_closed && !self.blank && matches!(self.nodes[self.tip].kind, Kind::Paragraph) {
            self.add_line();
        } else {
            self.close_unmatched();
            if self.accepts_lines(container) {
                self.add_line();
                if let Kind::Html(kind @ 1..=5) = self.nodes[container].kind
                    && scan::html_block_end(kind, self.line.get(self.offset..).unwrap_or_default())
                {
                    self.close_here(container);
                }
            } else if self.offset < len && !self.blank {
                self.add_child(Kind::Paragraph, self.offset);
                self.advance_next_nonspace();
                self.add_line();
            }
        }
        self.prev_line_end = start + len;
    }

    fn start(&mut self, container: usize) -> Start {
        let nns = self.next_nonspace;
        let line = self.line;
        let rest = line.get(nns..).unwrap_or_default();
        let first = rest.as_bytes().first().copied();
        let depth = self.nodes[container].depth;
        let is_para = matches!(self.nodes[container].kind, Kind::Paragraph);
        if !self.indented {
            if first == Some(b'>') && depth < MAX_NESTING {
                self.advance_next_nonspace();
                self.advance_offset(1, false);
                if is_space_tab(self.peek(self.offset)) {
                    self.advance_offset(1, true);
                }
                self.close_unmatched();
                self.add_child(Kind::BlockQuote, nns);
                return Start::Container;
            }
            if let Some(level) = atx_level(rest) {
                self.advance_next_nonspace();
                self.close_unmatched();
                let n = self.add_child(Kind::Heading(level), nns);
                let content_at = nns + usize::from(level);
                let content = atx_content(line.get(content_at..).unwrap_or_default());
                self.nodes[n]
                    .text
                    .push(content, self.line_start + content_at);
                self.offset = line.len();
                return Start::Leaf;
            }
            if let Some(ch) = first.filter(|c| matches!(c, b'`' | b'~')) {
                let run = rest.bytes().take_while(|&c| c == ch).count();
                if run >= 3 && (ch == b'~' || !rest.get(run..).unwrap_or_default().contains('`')) {
                    self.close_unmatched();
                    self.add_child(
                        Kind::Code(Some(Fence {
                            ch,
                            len: run,
                            offset: self.indent,
                        })),
                        nns,
                    );
                    self.advance_next_nonspace();
                    self.advance_offset(run, false);
                    return Start::Leaf;
                }
            }
            if first == Some(b'<') {
                if self.options.components
                    && depth < MAX_NESTING
                    && let Some(tag) = component::open_tag(rest)
                    && is_blank(rest.get(tag.len..).unwrap_or_default())
                {
                    self.close_unmatched();
                    let n = self.add_child(Kind::Component(tag.name, tag.props), nns);
                    self.offset = line.len();
                    if tag.self_closing {
                        self.close_here(n);
                        return Start::Leaf;
                    }
                    return Start::Container;
                }
                let lazy = !self.all_closed
                    && !self.blank
                    && matches!(self.nodes[self.tip].kind, Kind::Paragraph);
                if let Some(kind) = (1..=7).find(|&k| scan::html_block_start(k, rest))
                    && (kind < 7 || (!is_para && !lazy))
                {
                    self.close_unmatched();
                    self.add_child(Kind::Html(kind), self.offset);
                    return Start::Leaf;
                }
            }
            if is_para && let Some(level) = setext_level(rest) {
                self.close_unmatched();
                self.take_defs(container);
                if !is_blank(&self.nodes[container].text.text) {
                    self.nodes[container].kind = Kind::Heading(level);
                    self.offset = line.len();
                    return Start::Leaf;
                }
            }
            if self.options.gfm
                && is_para
                && let Some(alignments) = delimiter_row(rest)
                && self.table(container, alignments)
            {
                return Start::Leaf;
            }
            if is_thematic(rest) {
                self.close_unmatched();
                self.add_child(Kind::ThematicBreak, nns);
                self.offset = line.len();
                return Start::Leaf;
            }
        }
        let in_list = matches!(self.nodes[container].kind, Kind::List(_));
        if (!self.indented || in_list)
            && depth + 2 <= MAX_NESTING
            && let Some(marker) = self.list_marker(is_para)
        {
            self.close_unmatched();
            let continues_list = match (&self.nodes[self.tip].kind, marker.kind) {
                (
                    Kind::List(Marker {
                        kind: ListKind::Bullet(x),
                        ..
                    }),
                    ListKind::Bullet(y),
                ) => *x == y,
                (
                    Kind::List(Marker {
                        kind: ListKind::Ordered { delimiter: x, .. },
                        ..
                    }),
                    ListKind::Ordered { delimiter: y, .. },
                ) => *x == y,
                _ => false,
            };
            if !continues_list {
                self.add_child(Kind::List(marker), nns);
            }
            self.add_child(Kind::Item(marker), nns);
            return Start::Container;
        }
        if self.indented && !matches!(self.nodes[self.tip].kind, Kind::Paragraph) && !self.blank {
            self.advance_offset(4, true);
            self.close_unmatched();
            self.add_child(Kind::Code(None), self.offset);
            return Start::Leaf;
        }
        Start::None
    }

    fn table(&mut self, para: usize, alignments: Vec<Alignment>) -> bool {
        let text = &self.nodes[para].text;
        let body = text.text.strip_suffix('\n').unwrap_or(&text.text);
        let header_at = body.rfind('\n').map_or(0, |i| i + 1);
        let header = body.get(header_at..).unwrap_or_default().to_string();
        if split_row(&header).len() != alignments.len() {
            return false;
        }
        let header_start = text.source(header_at);
        let before_end = header_at.checked_sub(1).map(|i| text.source(i));
        self.close_unmatched();
        let table = Kind::Table(alignments, (header_start, header));
        let n = match before_end {
            None => {
                let node = &mut self.nodes[para];
                node.kind = table;
                node.text = Mapped::default();
                para
            }
            Some(end) => {
                self.nodes[para].text.text.truncate(header_at);
                self.finalize(para, self.line_no.saturating_sub(2), end);
                let n = self.add_child(table, 0);
                self.nodes[n].start_line = self.line_no - 1;
                n
            }
        };
        self.nodes[n].start = header_start;
        self.offset = self.line.len();
        true
    }

    fn list_marker(&mut self, is_para: bool) -> Option<Marker> {
        if self.indent >= 4 {
            return None;
        }
        let rest = self.line.get(self.next_nonspace..)?;
        let b = rest.as_bytes();
        let (kind, len) = match b.first()? {
            c @ (b'*' | b'+' | b'-') => (ListKind::Bullet(char::from(*c)), 1),
            _ => {
                let digits = b.iter().take_while(|c| c.is_ascii_digit()).count();
                let delimiter = *b.get(digits).filter(|c| matches!(c, b'.' | b')'))?;
                let start: u32 = rest.get(..digits)?.parse().ok()?;
                if !(1..=9).contains(&digits) || (is_para && start != 1) {
                    return None;
                }
                (
                    ListKind::Ordered {
                        start,
                        delimiter: char::from(delimiter),
                    },
                    digits + 1,
                )
            }
        };
        if !matches!(b.get(len), None | Some(b' ' | b'\t'))
            || (is_para && is_blank(rest.get(len..)?))
        {
            return None;
        }
        let offset = self.indent;
        self.advance_next_nonspace();
        self.advance_offset(len, true);
        let spaces_col = self.column;
        let spaces_off = self.offset;
        loop {
            self.advance_offset(1, true);
            if !(self.column - spaces_col < 5 && is_space_tab(self.peek(self.offset))) {
                break;
            }
        }
        let spaces = self.column - spaces_col;
        let padding = if !(1..5).contains(&spaces) || self.peek(self.offset).is_none() {
            self.column = spaces_col;
            self.offset = spaces_off;
            self.partial_tab = false;
            if is_space_tab(self.peek(self.offset)) {
                self.advance_offset(1, true);
            }
            len + 1
        } else {
            len + spaces
        };
        Some(Marker {
            kind,
            offset,
            padding,
            tight: true,
        })
    }

    fn finish(mut self) -> Parsed {
        while self.tip != 0 {
            self.finalize(self.tip, self.line_no, self.prev_line_end);
        }
        let children = std::mem::take(&mut self.nodes[0].children);
        let blocks = children.into_iter().map(|c| self.raw(c)).collect();
        Parsed {
            blocks,
            defs: self.defs,
        }
    }

    fn raw(&mut self, n: usize) -> RawBlock {
        let node = std::mem::take(&mut self.nodes[n]);
        let range = node.start..node.end;
        let ids = node.children;
        let kind = match node.kind {
            Kind::Document | Kind::BlockQuote => RawKind::BlockQuote,
            Kind::List(marker) => RawKind::List {
                kind: marker.kind,
                tight: marker.tight,
                items: ids
                    .iter()
                    .map(|&c| {
                        let item = std::mem::take(&mut self.nodes[c]);
                        self.item(item.start..item.end, &item.children)
                    })
                    .collect(),
            },
            Kind::Item(marker) => RawKind::List {
                kind: marker.kind,
                tight: true,
                items: vec![self.item(range.clone(), &ids)],
            },
            Kind::Paragraph => RawKind::Paragraph(node.text),
            Kind::Heading(level) => RawKind::Heading(level, node.text),
            Kind::ThematicBreak => RawKind::ThematicBreak,
            Kind::Code(Some(_)) => {
                let (info, text) = node
                    .text
                    .text
                    .split_once('\n')
                    .unwrap_or((&node.text.text, ""));
                RawKind::Code(
                    CodeKind::Fenced {
                        info: scan::unescape(info.trim()),
                    },
                    text.to_string(),
                )
            }
            Kind::Code(None) => RawKind::Code(CodeKind::Indented, node.text.text),
            Kind::Html(_) => RawKind::Html(
                node.text
                    .text
                    .strip_suffix('\n')
                    .unwrap_or(&node.text.text)
                    .to_string(),
            ),
            Kind::Table(alignments, header) => RawKind::Table {
                alignments,
                header,
                rows: node.rows,
            },
            Kind::Component(name, props) => RawKind::Component { name, props },
        };
        let children = match kind {
            RawKind::List { .. } => Vec::new(),
            _ => ids.iter().map(|&c| self.raw(c)).collect(),
        };
        RawBlock {
            kind,
            range,
            children,
        }
    }

    fn item(&mut self, range: Range<usize>, ids: &[usize]) -> RawItem {
        let mut blocks: Vec<RawBlock> = ids.iter().map(|&c| self.raw(c)).collect();
        let mut task = None;
        if self.options.gfm
            && let Some(RawBlock {
                kind: RawKind::Paragraph(text),
                ..
            }) = blocks.first_mut()
        {
            let b = text.text.as_bytes();
            if b.first() == Some(&b'[')
                && b.get(2) == Some(&b']')
                && matches!(b.get(3), Some(b' ' | b'\t'))
            {
                task = match b.get(1) {
                    Some(b' ') => Some(false),
                    Some(b'x' | b'X') => Some(true),
                    _ => None,
                };
                if task.is_some() {
                    text.drop_front(3);
                }
            }
        }
        RawItem {
            range,
            task,
            blocks,
        }
    }
}
