use std::fmt;

use crate::MAX_JSON_DEPTH;

#[derive(Debug, Clone, PartialEq)]
pub enum Json {
    Null,
    Bool(bool),
    Number(f64),
    String(String),
    Array(Vec<Json>),
    Object(Vec<(String, Json)>),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum JsonErrorKind {
    UnexpectedEnd,
    Unexpected(char),
    BadNumber,
    BadEscape,
    TooDeep,
    TrailingData,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct JsonError {
    pub at: usize,
    pub kind: JsonErrorKind,
}

impl fmt::Display for JsonError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.kind {
            JsonErrorKind::UnexpectedEnd => write!(f, "json ends early at byte {}", self.at),
            JsonErrorKind::Unexpected(c) => write!(f, "unexpected {c:?} at byte {}", self.at),
            JsonErrorKind::BadNumber => write!(f, "bad number at byte {}", self.at),
            JsonErrorKind::BadEscape => write!(f, "bad escape at byte {}", self.at),
            JsonErrorKind::TooDeep => {
                write!(f, "nested deeper than {MAX_JSON_DEPTH} at byte {}", self.at)
            }
            JsonErrorKind::TrailingData => write!(f, "data after the value at byte {}", self.at),
        }
    }
}

impl std::error::Error for JsonError {}

pub fn parse_json(source: &str) -> Result<Json, JsonError> {
    let mut reader = Reader { s: source, pos: 0 };
    let value = reader.value(0)?;
    reader.skip_ws();
    if reader.pos < source.len() {
        return Err(reader.fail(JsonErrorKind::TrailingData));
    }
    Ok(value)
}

struct Reader<'a> {
    s: &'a str,
    pos: usize,
}

impl Reader<'_> {
    fn peek(&self) -> Option<u8> {
        self.s.as_bytes().get(self.pos).copied()
    }

    fn fail(&self, kind: JsonErrorKind) -> JsonError {
        JsonError { at: self.pos, kind }
    }

    fn unexpected(&self) -> JsonError {
        match self.s.get(self.pos..).and_then(|r| r.chars().next()) {
            Some(c) => self.fail(JsonErrorKind::Unexpected(c)),
            None => self.fail(JsonErrorKind::UnexpectedEnd),
        }
    }

    fn skip_ws(&mut self) {
        while matches!(self.peek(), Some(b' ' | b'\t' | b'\n' | b'\r')) {
            self.pos += 1;
        }
    }

    fn eat(&mut self, word: &str) -> bool {
        let found = self.s.get(self.pos..).is_some_and(|r| r.starts_with(word));
        if found {
            self.pos += word.len();
        }
        found
    }

    fn value(&mut self, depth: usize) -> Result<Json, JsonError> {
        if depth >= MAX_JSON_DEPTH {
            return Err(self.fail(JsonErrorKind::TooDeep));
        }
        self.skip_ws();
        match self.peek() {
            Some(b'{') => self.object(depth),
            Some(b'[') => self.array(depth),
            Some(b'"') => self.string().map(Json::String),
            Some(b'-' | b'0'..=b'9') => self.number(),
            _ if self.eat("true") => Ok(Json::Bool(true)),
            _ if self.eat("false") => Ok(Json::Bool(false)),
            _ if self.eat("null") => Ok(Json::Null),
            _ => Err(self.unexpected()),
        }
    }

    fn array(&mut self, depth: usize) -> Result<Json, JsonError> {
        self.pos += 1;
        let mut items = Vec::new();
        self.skip_ws();
        if self.eat("]") {
            return Ok(Json::Array(items));
        }
        loop {
            items.push(self.value(depth + 1)?);
            self.skip_ws();
            if self.eat("]") {
                return Ok(Json::Array(items));
            }
            if !self.eat(",") {
                return Err(self.unexpected());
            }
        }
    }

    fn object(&mut self, depth: usize) -> Result<Json, JsonError> {
        self.pos += 1;
        let mut fields = Vec::new();
        self.skip_ws();
        if self.eat("}") {
            return Ok(Json::Object(fields));
        }
        loop {
            self.skip_ws();
            if self.peek() != Some(b'"') {
                return Err(self.unexpected());
            }
            let key = self.string()?;
            self.skip_ws();
            if !self.eat(":") {
                return Err(self.unexpected());
            }
            fields.push((key, self.value(depth + 1)?));
            self.skip_ws();
            if self.eat("}") {
                return Ok(Json::Object(fields));
            }
            if !self.eat(",") {
                return Err(self.unexpected());
            }
        }
    }

    fn digits(&mut self) -> usize {
        let start = self.pos;
        while matches!(self.peek(), Some(b'0'..=b'9')) {
            self.pos += 1;
        }
        self.pos - start
    }

    fn number(&mut self) -> Result<Json, JsonError> {
        let start = self.pos;
        self.eat("-");
        if self.eat("0") {
            if matches!(self.peek(), Some(b'0'..=b'9')) {
                return Err(self.fail(JsonErrorKind::BadNumber));
            }
        } else if self.digits() == 0 {
            return Err(self.fail(JsonErrorKind::BadNumber));
        }
        if self.eat(".") && self.digits() == 0 {
            return Err(self.fail(JsonErrorKind::BadNumber));
        }
        if matches!(self.peek(), Some(b'e' | b'E')) {
            self.pos += 1;
            if matches!(self.peek(), Some(b'+' | b'-')) {
                self.pos += 1;
            }
            if self.digits() == 0 {
                return Err(self.fail(JsonErrorKind::BadNumber));
            }
        }
        self.s
            .get(start..self.pos)
            .and_then(|t| t.parse().ok())
            .map(Json::Number)
            .ok_or(JsonError {
                at: start,
                kind: JsonErrorKind::BadNumber,
            })
    }

    fn hex4(&mut self) -> Result<u32, JsonError> {
        let code = self
            .s
            .get(self.pos..self.pos + 4)
            .filter(|h| h.bytes().all(|b| b.is_ascii_hexdigit()))
            .and_then(|h| u32::from_str_radix(h, 16).ok())
            .ok_or(self.fail(JsonErrorKind::BadEscape))?;
        self.pos += 4;
        Ok(code)
    }

    fn string(&mut self) -> Result<String, JsonError> {
        self.pos += 1;
        let mut out = String::new();
        loop {
            let rest = self.s.get(self.pos..).unwrap_or_default();
            let Some(c) = rest.chars().next() else {
                return Err(self.fail(JsonErrorKind::UnexpectedEnd));
            };
            match c {
                '"' => {
                    self.pos += 1;
                    return Ok(out);
                }
                '\\' => {
                    self.pos += 1;
                    let escaped = match self.peek() {
                        Some(b'"') => '"',
                        Some(b'\\') => '\\',
                        Some(b'/') => '/',
                        Some(b'b') => '\u{8}',
                        Some(b'f') => '\u{c}',
                        Some(b'n') => '\n',
                        Some(b'r') => '\r',
                        Some(b't') => '\t',
                        Some(b'u') => {
                            self.pos += 1;
                            let high = self.hex4()?;
                            let code = if (0xD800..0xDC00).contains(&high) && self.eat("\\u") {
                                let low = self.hex4()?;
                                if !(0xDC00..0xE000).contains(&low) {
                                    return Err(self.fail(JsonErrorKind::BadEscape));
                                }
                                0x10000 + ((high - 0xD800) << 10) + (low - 0xDC00)
                            } else {
                                high
                            };
                            out.push(
                                char::from_u32(code).ok_or(self.fail(JsonErrorKind::BadEscape))?,
                            );
                            continue;
                        }
                        _ => return Err(self.fail(JsonErrorKind::BadEscape)),
                    };
                    self.pos += 1;
                    out.push(escaped);
                }
                c if u32::from(c) < 0x20 => return Err(self.unexpected()),
                c => {
                    self.pos += c.len_utf8();
                    out.push(c);
                }
            }
        }
    }
}
