use std::cell::RefCell;
use std::collections::HashMap;
use std::ops::Range;
use std::rc::Rc;

use crate::components::code::{Face, Heading};
use crate::theme::ColorToken;

pub(super) const MARKUP: ColorToken = ColorToken::TextMuted;
const DEEPEST_HEADING: usize = 6;
const MARKS: [(&[char], ColorToken, Option<Face>); 5] = [
    (&['`'], ColorToken::SyntaxTextLiteral, None),
    (
        &['*', '*'],
        ColorToken::SyntaxEmphasisStrong,
        Some(Face::Bold),
    ),
    (
        &['_', '_'],
        ColorToken::SyntaxEmphasisStrong,
        Some(Face::Bold),
    ),
    (&['*'], ColorToken::SyntaxEmphasis, Some(Face::Italic)),
    (&['_'], ColorToken::SyntaxEmphasis, Some(Face::Italic)),
];

#[derive(Default)]
pub(super) struct Markdown {
    pub markup: Vec<Range<usize>>,
    pub runs: Vec<(Range<usize>, ColorToken)>,
    pub faces: Vec<(Range<usize>, Face)>,
    pub heading: Heading,
}

#[derive(Default)]
pub(super) struct Parsed(RefCell<HashMap<Box<str>, Rc<Markdown>>>);

impl Parsed {
    pub fn line(&self, text: &str) -> Rc<Markdown> {
        let known = self.0.borrow().get(text).cloned();
        known.unwrap_or_else(|| {
            let found = Rc::new(parse(text));
            self.0.borrow_mut().insert(text.into(), found.clone());
            found
        })
    }

    pub fn trim(&mut self, most: usize) {
        let known = self.0.get_mut();
        if known.len() > most {
            known.clear();
        }
    }
}

fn hashes(line: impl Iterator<Item = char>) -> Option<usize> {
    let mut hashes = 0;
    for ch in line.take(DEEPEST_HEADING + 1) {
        match ch {
            '#' => hashes += 1,
            ' ' if (1..=DEEPEST_HEADING).contains(&hashes) => return Some(hashes),
            _ => return None,
        }
    }
    None
}

pub(super) fn heading(line: impl Iterator<Item = char>) -> Heading {
    hashes(line).map_or(Heading::Body, Heading::level)
}

fn parse(line: &str) -> Markdown {
    let chars: Vec<char> = line.chars().collect();
    let mut found = Markdown::default();
    if let Some(hashes) = hashes(chars.iter().copied()) {
        found.markup.push(0..hashes + 1);
        found.heading = Heading::level(hashes);
        found
            .runs
            .push((hashes + 1..chars.len(), ColorToken::SyntaxTitle));
        return found;
    }
    let indent = chars.iter().take_while(|ch| ch.is_whitespace()).count();
    let after = |at: usize| chars.get(at).copied();
    if after(indent) == Some('>') {
        let mark = indent + 1 + usize::from(after(indent + 1) == Some(' '));
        found.markup.push(indent..mark);
        found
            .runs
            .push((mark..chars.len(), ColorToken::SyntaxComment));
        return found;
    }
    let digits = chars
        .iter()
        .skip(indent)
        .take_while(|ch| ch.is_ascii_digit())
        .count();
    let body = match (after(indent), after(indent + 1)) {
        (Some('-' | '*' | '+'), Some(' ')) => {
            let bullet = (indent..indent + 1, ColorToken::SyntaxPunctuationListMarker);
            found.runs.push(bullet);
            indent + 2
        }
        _ if digits > 0
            && matches!(after(indent + digits), Some('.' | ')'))
            && after(indent + digits + 1) == Some(' ') =>
        {
            let number = indent..indent + digits + 1;
            found
                .runs
                .push((number, ColorToken::SyntaxPunctuationListMarker));
            indent + digits + 2
        }
        _ => indent,
    };
    inline(&chars, body, &mut found);
    found
}

fn inline(chars: &[char], from: usize, found: &mut Markdown) {
    let mut unclosed = [false; MARKS.len()];
    let mut at = from;
    while let Some(rest) = chars.get(at..).filter(|rest| !rest.is_empty()) {
        let word_before = at
            .checked_sub(1)
            .and_then(|before| chars.get(before))
            .is_some_and(|ch| ch.is_alphanumeric());
        let opening = MARKS.iter().enumerate().find(|(kind, (mark, _, _))| {
            rest.starts_with(mark)
                && !unclosed.get(*kind).copied().unwrap_or(true)
                && !(mark.first() == Some(&'_') && word_before)
                && rest.get(mark.len()).is_some_and(|ch| !ch.is_whitespace())
        });
        let Some((kind, (mark, token, face))) = opening else {
            at += 1;
            continue;
        };
        let open = at + mark.len();
        let close = (open + 1..chars.len()).find(|close| {
            chars
                .get(*close..)
                .is_some_and(|tail| tail.starts_with(mark))
        });
        let Some(close) = close else {
            if let Some(seen) = unclosed.get_mut(kind) {
                *seen = true;
            }
            at = open;
            continue;
        };
        found.markup.push(at..open);
        found.markup.push(close..close + mark.len());
        found.runs.push((open..close, *token));
        found.faces.extend(face.map(|face| (open..close, face)));
        at = close + mark.len();
    }
}
