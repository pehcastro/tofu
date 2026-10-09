use std::collections::HashMap;
use std::fmt;
use std::ops::Range;

use crate::ast::Component;
use crate::json::{Json, JsonError, parse_json};

#[derive(Debug, Clone, PartialEq)]
pub enum PropValue {
    Text(String),
    Json(Json),
    Invalid { raw: String, error: JsonError },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PropKind {
    Text,
    Number,
    Bool,
    Array,
    Object,
    Any,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PropSpec {
    pub name: String,
    pub kind: PropKind,
    pub required: bool,
}

#[derive(Debug, Clone, Default)]
pub struct Registry {
    components: Vec<(String, Vec<PropSpec>)>,
}

impl Registry {
    pub fn define(&mut self, name: &str, props: Vec<PropSpec>) {
        self.components.retain(|(n, _)| n != name);
        self.components.push((name.to_string(), props));
    }
}

#[derive(Debug, Clone, PartialEq)]
pub enum PropError {
    UnknownComponent(String),
    UnknownProp(String),
    MissingProp(String),
    WrongKind { prop: String, expected: PropKind },
    InvalidJson { prop: String, error: JsonError },
}

impl fmt::Display for PropError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::UnknownComponent(name) => write!(f, "no component named {name}"),
            Self::UnknownProp(prop) => write!(f, "no prop named {prop}"),
            Self::MissingProp(prop) => write!(f, "required prop {prop} is missing"),
            Self::WrongKind { prop, expected } => write!(f, "prop {prop} is not {expected:?}"),
            Self::InvalidJson { prop, error } => write!(f, "prop {prop} holds bad json: {error}"),
        }
    }
}

impl std::error::Error for PropError {}

pub fn validate<C>(component: &Component<C>, registry: &Registry) -> Result<(), PropError> {
    let (_, specs) = registry
        .components
        .iter()
        .find(|(name, _)| *name == component.name)
        .ok_or_else(|| PropError::UnknownComponent(component.name.clone()))?;
    for (name, value) in &component.props {
        let spec = specs
            .iter()
            .find(|s| s.name == *name)
            .ok_or_else(|| PropError::UnknownProp(name.clone()))?;
        let fits = match (value, spec.kind) {
            (PropValue::Invalid { error, .. }, _) => {
                return Err(PropError::InvalidJson {
                    prop: name.clone(),
                    error: *error,
                });
            }
            (_, PropKind::Any)
            | (PropValue::Text(_) | PropValue::Json(Json::String(_)), PropKind::Text)
            | (PropValue::Json(Json::Number(_)), PropKind::Number)
            | (PropValue::Json(Json::Bool(_)), PropKind::Bool)
            | (PropValue::Json(Json::Array(_)), PropKind::Array)
            | (PropValue::Json(Json::Object(_)), PropKind::Object) => true,
            _ => false,
        };
        if !fits {
            return Err(PropError::WrongKind {
                prop: name.clone(),
                expected: spec.kind,
            });
        }
    }
    match specs
        .iter()
        .find(|s| s.required && !component.props.iter().any(|(n, _)| *n == s.name))
    {
        Some(missing) => Err(PropError::MissingProp(missing.name.clone())),
        None => Ok(()),
    }
}

pub(crate) struct Tag {
    pub name: String,
    pub props: Vec<(String, PropValue)>,
    pub self_closing: bool,
    pub len: usize,
}

fn skip_ws(b: &[u8], mut i: usize) -> usize {
    while matches!(b.get(i), Some(b' ' | b'\t' | b'\n' | b'\r')) {
        i += 1;
    }
    i
}

fn word(b: &[u8], i: usize, first: fn(u8) -> bool, rest: fn(u8) -> bool) -> Option<usize> {
    if !b.get(i).copied().is_some_and(first) {
        return None;
    }
    Some(i + 1 + b.iter().skip(i + 1).take_while(|&&c| rest(c)).count())
}

fn name_end(b: &[u8], i: usize) -> Option<usize> {
    word(
        b,
        i,
        |c| c.is_ascii_uppercase(),
        |c| c.is_ascii_alphanumeric() || c == b'_' || c == b'.',
    )
}

fn braced_end(b: &[u8], start: usize) -> Option<usize> {
    let mut depth = 0usize;
    let mut i = start;
    let mut in_string = false;
    while let Some(&c) = b.get(i) {
        match (in_string, c) {
            (true, b'\\') => i += 1,
            (true, b'"') | (false, b'"') => in_string = !in_string,
            (false, b'{' | b'[') => depth += 1,
            (false, b'}' | b']') => {
                depth = depth.checked_sub(1)?;
                if depth == 0 {
                    return Some(i + 1);
                }
            }
            _ => {}
        }
        i += 1;
    }
    None
}

fn prop_value(s: &str, b: &[u8], i: usize) -> Option<(PropValue, usize)> {
    match b.get(i)? {
        q @ (b'"' | b'\'') => {
            let close = i + 1 + b.get(i + 1..)?.iter().position(|c| c == q)?;
            Some((PropValue::Text(s.get(i + 1..close)?.to_string()), close + 1))
        }
        b'{' => {
            let end = braced_end(b, i)?;
            let raw = s.get(i + 1..end - 1)?;
            let value = match parse_json(raw) {
                Ok(json) => PropValue::Json(json),
                Err(error) => PropValue::Invalid {
                    raw: raw.to_string(),
                    error,
                },
            };
            Some((value, end))
        }
        _ => None,
    }
}

pub(crate) fn open_tag(s: &str) -> Option<Tag> {
    let b = s.as_bytes();
    if b.first() != Some(&b'<') {
        return None;
    }
    let mut i = name_end(b, 1)?;
    let name = s.get(1..i)?.to_string();
    let mut props = Vec::new();
    loop {
        let j = skip_ws(b, i);
        match b.get(j)? {
            b'>' => {
                return Some(Tag {
                    name,
                    props,
                    self_closing: false,
                    len: j + 1,
                });
            }
            b'/' if b.get(j + 1) == Some(&b'>') => {
                return Some(Tag {
                    name,
                    props,
                    self_closing: true,
                    len: j + 2,
                });
            }
            _ if j == i => return None,
            _ => {}
        }
        let key_end = word(
            b,
            j,
            |c| c.is_ascii_alphabetic() || c == b'_' || c == b':',
            |c| c.is_ascii_alphanumeric() || matches!(c, b'_' | b'.' | b':' | b'-'),
        )?;
        let key = s.get(j..key_end)?.to_string();
        let eq = skip_ws(b, key_end);
        let (value, end) = if b.get(eq) == Some(&b'=') {
            prop_value(s, b, skip_ws(b, eq + 1))?
        } else {
            (PropValue::Json(Json::Bool(true)), key_end)
        };
        props.push((key, value));
        i = end;
    }
}

pub(crate) fn close_tag(s: &str, name: &str) -> Option<usize> {
    let rest = s.strip_prefix("</")?.strip_prefix(name)?;
    let b = rest.as_bytes();
    let gap = skip_ws(b, 0);
    (b.get(gap) == Some(&b'>')).then_some(2 + name.len() + gap + 1)
}

pub(crate) type Pairs = HashMap<usize, Range<usize>>;

pub(crate) fn pairs(s: &str, start: usize, end: usize) -> Pairs {
    let mut found = Pairs::new();
    let mut open: HashMap<&str, Vec<usize>> = HashMap::new();
    let mut i = start;
    while let Some(off) = s.get(i..end).and_then(|r| r.find('<')) {
        let at = i + off;
        let rest = s.get(at..end).unwrap_or_default();
        let closing = name_end(rest.as_bytes(), 2)
            .filter(|_| rest.starts_with("</"))
            .and_then(|name_to| rest.get(2..name_to))
            .and_then(|name| close_tag(rest, name).map(|len| (name, len)));
        if let Some((name, len)) = closing {
            if let Some(opened) = open.get_mut(name).and_then(Vec::pop) {
                found.insert(opened, at..at + len);
            }
            i = at + len;
        } else if let Some(tag) = open_tag(rest) {
            if !tag.self_closing {
                let name = rest.get(1..=tag.name.len()).unwrap_or_default();
                open.entry(name).or_default().push(at);
            }
            i = at + tag.len;
        } else {
            i = at + 1;
        }
    }
    found
}
