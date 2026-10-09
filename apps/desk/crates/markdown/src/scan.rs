use std::ops::Range;

pub(crate) fn is_space_tab(b: Option<u8>) -> bool {
    matches!(b, Some(b' ' | b'\t'))
}

pub(crate) fn is_blank(s: &str) -> bool {
    s.bytes().all(|b| matches!(b, b' ' | b'\t' | b'\n' | b'\r'))
}

pub(crate) fn is_punct(c: char) -> bool {
    c.is_ascii_punctuation()
        || matches!(u32::from(c),
            0xA1..=0xBF | 0xD7 | 0xF7 | 0x2010..=0x2027 | 0x2030..=0x205E | 0x20A0..=0x20C0
            | 0x2100..=0x214F | 0x2190..=0x2BFF | 0x2E00..=0x2E7F | 0x3001..=0x3003 | 0x3008..=0x3020
            | 0xFE10..=0xFE19 | 0xFE30..=0xFE6B | 0xFF01..=0xFF0F | 0xFF1A..=0xFF20 | 0xFF3B..=0xFF40
            | 0xFF5B..=0xFF65)
}

pub(crate) fn entity(s: &str) -> Option<(String, usize)> {
    let body = s.strip_prefix('&')?;
    let semi = body.get(..body.len().min(40))?.find(';')?;
    let name = body.get(..semi)?;
    let len = semi + 2;
    let numeric = |digits: &str, radix: u32, max: usize| {
        (!digits.is_empty() && digits.len() <= max && digits.chars().all(|c| c.is_digit(radix)))
            .then(|| u32::from_str_radix(digits, radix).ok())
            .flatten()
            .map(|code| {
                char::from_u32(code)
                    .filter(|&c| c != '\0')
                    .unwrap_or('\u{FFFD}')
                    .to_string()
            })
    };
    let text = if let Some(hex) = name.strip_prefix("#x").or_else(|| name.strip_prefix("#X")) {
        numeric(hex, 16, 6)?
    } else if let Some(dec) = name.strip_prefix('#') {
        numeric(dec, 10, 7)?
    } else {
        if !name.starts_with(|c: char| c.is_ascii_alphabetic())
            || !name.chars().all(|c| c.is_ascii_alphanumeric())
        {
            return None;
        }
        include_str!("entities.txt")
            .lines()
            .find_map(|line| line.strip_prefix(name)?.strip_prefix(' '))?
            .split(',')
            .filter_map(|h| u32::from_str_radix(h, 16).ok().and_then(char::from_u32))
            .collect()
    };
    Some((text, len))
}

pub(crate) fn unescape(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut rest = s;
    while let Some(i) = rest.find(['\\', '&']) {
        out.push_str(rest.get(..i).unwrap_or_default());
        let tail = rest.get(i..).unwrap_or_default();
        let next = tail.chars().nth(1);
        if tail.starts_with('\\') && next.is_some_and(|c| c.is_ascii_punctuation()) {
            out.push(next.unwrap_or('\\'));
            rest = tail.get(2..).unwrap_or_default();
        } else if let Some((text, len)) = tail.starts_with('&').then(|| entity(tail)).flatten() {
            out.push_str(&text);
            rest = tail.get(len..).unwrap_or_default();
        } else {
            out.push_str(tail.get(..1).unwrap_or_default());
            rest = tail.get(1..).unwrap_or_default();
        }
    }
    out.push_str(rest);
    out
}

pub(crate) fn normalize_uri(s: &str) -> String {
    let b = s.as_bytes();
    let mut out = String::with_capacity(s.len());
    for (i, &c) in b.iter().enumerate() {
        let escaped = c == b'%'
            && b.get(i + 1).is_some_and(u8::is_ascii_hexdigit)
            && b.get(i + 2).is_some_and(u8::is_ascii_hexdigit);
        if c.is_ascii_alphanumeric() || b";/?:@&=+$,-_.!~*'()#".contains(&c) || escaped {
            out.push(char::from(c));
        } else {
            out.push_str(&format!("%{c:02X}"));
        }
    }
    out
}

pub(crate) fn normalize_label(s: &str) -> String {
    let inner = s.get(1..s.len().saturating_sub(1)).unwrap_or_default();
    inner
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .to_lowercase()
        .to_uppercase()
}

pub(crate) fn spnl(b: &[u8], mut i: usize, end: usize) -> usize {
    let skip = |mut i: usize| {
        while i < end && matches!(b.get(i), Some(b' ' | b'\t')) {
            i += 1;
        }
        i
    };
    i = skip(i);
    if i < end && b.get(i) == Some(&b'\n') {
        i = skip(i + 1);
    }
    i
}

pub(crate) fn link_label(b: &[u8], start: usize, end: usize) -> Option<usize> {
    if b.get(start) != Some(&b'[') {
        return None;
    }
    let mut i = start + 1;
    while i < end && i - start <= 1000 {
        match b.get(i)? {
            b']' => return Some(i + 1 - start),
            b'[' => return None,
            b'\\' => i += 2,
            _ => i += 1,
        }
    }
    None
}

pub(crate) fn link_destination(s: &str, start: usize, end: usize) -> Option<(String, usize)> {
    let b = s.as_bytes();
    if b.get(start) == Some(&b'<') {
        let mut i = start + 1;
        while i < end {
            match b.get(i)? {
                b'>' => return Some((normalize_uri(&unescape(s.get(start + 1..i)?)), i + 1)),
                b'<' | b'\n' => return None,
                b'\\' => i += 2,
                _ => i += 1,
            }
        }
        return None;
    }
    let mut i = start;
    let mut parens = 0usize;
    while i < end {
        let c = *b.get(i)?;
        match c {
            b'\\' if b.get(i + 1).is_some_and(u8::is_ascii_punctuation) => i += 2,
            b'(' => {
                parens += 1;
                i += 1;
            }
            b')' if parens == 0 => break,
            b')' => {
                parens -= 1;
                i += 1;
            }
            c if c <= b' ' || c == 0x7f => break,
            _ => i += 1,
        }
    }
    if (i == start && b.get(i) != Some(&b')')) || parens != 0 {
        return None;
    }
    Some((normalize_uri(&unescape(s.get(start..i)?)), i))
}

pub(crate) fn link_title(s: &str, start: usize, end: usize) -> Option<(String, usize)> {
    let b = s.as_bytes();
    let close = match b.get(start)? {
        b'"' => b'"',
        b'\'' => b'\'',
        b'(' => b')',
        _ => return None,
    };
    let mut i = start + 1;
    while i < end {
        match *b.get(i)? {
            c if c == close => return Some((unescape(s.get(start + 1..i)?), i + 1)),
            b'(' if close == b')' => return None,
            b'\\' => i += 2,
            _ => i += 1,
        }
    }
    None
}

pub(crate) struct LinkDef {
    pub label: String,
    pub destination: String,
    pub title: String,
    pub len: usize,
}

fn line_end(b: &[u8], mut i: usize) -> Option<usize> {
    while matches!(b.get(i), Some(b' ' | b'\t')) {
        i += 1;
    }
    match b.get(i) {
        None => Some(i),
        Some(b'\n') => Some(i + 1),
        Some(_) => None,
    }
}

pub(crate) fn link_def(s: &str) -> Option<LinkDef> {
    let b = s.as_bytes();
    let end = b.len();
    let label_len = link_label(b, 0, end)?;
    if b.get(label_len) != Some(&b':') {
        return None;
    }
    let label = normalize_label(s.get(..label_len)?);
    if label.is_empty() {
        return None;
    }
    let (destination, after_dest) = link_destination(s, spnl(b, label_len + 1, end), end)?;
    let before_title = spnl(b, after_dest, end);
    let titled = (before_title > after_dest)
        .then(|| link_title(s, before_title, end))
        .flatten()
        .and_then(|(title, after)| line_end(b, after).map(|len| (title, len)));
    let (title, len) = match titled {
        Some(found) => found,
        None => (String::new(), line_end(b, after_dest)?),
    };
    Some(LinkDef {
        label,
        destination,
        title,
        len,
    })
}

fn tag_name_end(b: &[u8], i: usize) -> Option<usize> {
    b.get(i).filter(|c| c.is_ascii_alphabetic())?;
    Some(
        i + 1
            + b.iter()
                .skip(i + 1)
                .take_while(|c| c.is_ascii_alphanumeric() || **c == b'-')
                .count(),
    )
}

fn html_ws(b: &[u8], mut i: usize) -> usize {
    while matches!(b.get(i), Some(b' ' | b'\t' | b'\n' | b'\r' | b'\x0c')) {
        i += 1;
    }
    i
}

fn attribute_end(b: &[u8], start: usize) -> Option<usize> {
    if !b
        .get(start)
        .is_some_and(|c| c.is_ascii_alphabetic() || matches!(c, b'_' | b':'))
    {
        return None;
    }
    let name_end = start
        + 1
        + b.iter()
            .skip(start + 1)
            .take_while(|c| c.is_ascii_alphanumeric() || matches!(c, b'_' | b'.' | b':' | b'-'))
            .count();
    let eq = html_ws(b, name_end);
    if b.get(eq) != Some(&b'=') {
        return Some(name_end);
    }
    let v = html_ws(b, eq + 1);
    match *b.get(v)? {
        q @ (b'"' | b'\'') => Some(v + 2 + b.get(v + 1..)?.iter().position(|&c| c == q)?),
        _ => {
            let len = b
                .iter()
                .skip(v)
                .take_while(|&&c| {
                    c > b' ' && !matches!(c, b'"' | b'\'' | b'=' | b'<' | b'>' | b'`')
                })
                .count();
            (len > 0).then_some(v + len)
        }
    }
}

pub(crate) fn html_tag(b: &[u8]) -> Option<usize> {
    if b.first() != Some(&b'<') {
        return None;
    }
    if b.get(1) == Some(&b'/') {
        let i = html_ws(b, tag_name_end(b, 2)?);
        return (b.get(i) == Some(&b'>')).then_some(i + 1);
    }
    let mut i = tag_name_end(b, 1)?;
    loop {
        let j = html_ws(b, i);
        match attribute_end(b, j) {
            Some(end) if j > i => i = end,
            _ => {
                i = j;
                break;
            }
        }
    }
    if b.get(i) == Some(&b'/') {
        i += 1;
    }
    (b.get(i) == Some(&b'>')).then_some(i + 1)
}

fn find(b: &[u8], from: usize, needle: &[u8]) -> Option<usize> {
    b.get(from..)?
        .windows(needle.len())
        .position(|w| w == needle)
        .map(|p| from + p + needle.len())
}

pub(crate) fn html_inline(b: &[u8]) -> Option<usize> {
    if b.starts_with(b"<!-->") {
        Some(5)
    } else if b.starts_with(b"<!--->") {
        Some(6)
    } else if b.starts_with(b"<!--") {
        find(b, 4, b"-->")
    } else if b.starts_with(b"<?") {
        find(b, 2, b"?>")
    } else if b.starts_with(b"<![CDATA[") {
        find(b, 9, b"]]>")
    } else if b.starts_with(b"<!") && b.get(2).is_some_and(u8::is_ascii_alphabetic) {
        find(b, 2, b">")
    } else {
        html_tag(b)
    }
}

const BLOCK_TAGS: [&str; 62] = [
    "address",
    "article",
    "aside",
    "base",
    "basefont",
    "blockquote",
    "body",
    "caption",
    "center",
    "col",
    "colgroup",
    "dd",
    "details",
    "dialog",
    "dir",
    "div",
    "dl",
    "dt",
    "fieldset",
    "figcaption",
    "figure",
    "footer",
    "form",
    "frame",
    "frameset",
    "h1",
    "h2",
    "h3",
    "h4",
    "h5",
    "h6",
    "head",
    "header",
    "hr",
    "html",
    "iframe",
    "legend",
    "li",
    "link",
    "main",
    "menu",
    "menuitem",
    "nav",
    "noframes",
    "ol",
    "optgroup",
    "option",
    "p",
    "param",
    "search",
    "section",
    "summary",
    "table",
    "tbody",
    "td",
    "tfoot",
    "th",
    "thead",
    "title",
    "tr",
    "track",
    "ul",
];

fn tag_word(s: &str, start: usize) -> (String, Option<u8>) {
    let b = s.as_bytes();
    let len = b
        .iter()
        .skip(start)
        .take_while(|c| c.is_ascii_alphanumeric())
        .count();
    (
        s.get(start..start + len)
            .unwrap_or_default()
            .to_ascii_lowercase(),
        b.get(start + len).copied(),
    )
}

pub(crate) fn html_block_start(kind: u8, s: &str) -> bool {
    let b = s.as_bytes();
    match kind {
        1 => {
            let (word, after) = tag_word(s, 1);
            s.starts_with('<')
                && ["script", "pre", "textarea", "style"].contains(&word.as_str())
                && matches!(after, None | Some(b' ' | b'\t' | b'>'))
        }
        2 => s.starts_with("<!--"),
        3 => s.starts_with("<?"),
        4 => s.starts_with("<!") && b.get(2).is_some_and(u8::is_ascii_alphabetic),
        5 => s.starts_with("<![CDATA["),
        6 => {
            let start = if s.starts_with("</") { 2 } else { 1 };
            let (word, after) = tag_word(s, start);
            s.starts_with('<')
                && BLOCK_TAGS.contains(&word.as_str())
                && (matches!(after, None | Some(b' ' | b'\t' | b'>'))
                    || (after == Some(b'/') && b.get(start + word.len() + 1) == Some(&b'>')))
        }
        _ => html_tag(b).is_some_and(|len| is_blank(s.get(len..).unwrap_or_default())),
    }
}

pub(crate) fn html_block_end(kind: u8, s: &str) -> bool {
    let lower = s.to_ascii_lowercase();
    match kind {
        1 => ["</script>", "</pre>", "</textarea>", "</style>"]
            .iter()
            .any(|t| lower.contains(t)),
        2 => s.contains("-->"),
        3 => s.contains("?>"),
        4 => s.contains('>'),
        5 => s.contains("]]>"),
        _ => false,
    }
}

pub(crate) fn autolink(s: &str) -> Option<(String, String, usize)> {
    let b = s.as_bytes();
    let close = 1 + b
        .get(1..)?
        .iter()
        .position(|&c| c == b'>' || c == b'<' || c <= b' ')?;
    if b.get(close) != Some(&b'>') {
        return None;
    }
    let body = s.get(1..close)?;
    if let Some(colon) = body.find(':') {
        let scheme = body.get(..colon)?;
        let valid = (2..=32).contains(&scheme.len())
            && scheme.starts_with(|c: char| c.is_ascii_alphabetic())
            && scheme
                .bytes()
                .all(|c| c.is_ascii_alphanumeric() || matches!(c, b'+' | b'.' | b'-'));
        if valid {
            return Some((normalize_uri(body), body.to_string(), close + 1));
        }
    }
    let (local, domain) = body.split_once('@')?;
    let local_ok = !local.is_empty()
        && local
            .bytes()
            .all(|c| c.is_ascii_alphanumeric() || b".!#$%&'*+/=?^_`{|}~-".contains(&c));
    let domain_ok = domain.split('.').all(|label| {
        (1..=63).contains(&label.len())
            && label
                .bytes()
                .all(|c| c.is_ascii_alphanumeric() || c == b'-')
            && !label.starts_with('-')
            && !label.ends_with('-')
    });
    (local_ok && domain_ok).then(|| {
        (
            normalize_uri(&format!("mailto:{body}")),
            body.to_string(),
            close + 1,
        )
    })
}

fn trim_link_end(text: &str, mut end: usize) -> usize {
    loop {
        let link = text.get(..end).unwrap_or_default();
        match link.chars().last() {
            Some('?' | '!' | '.' | ',' | ':' | '*' | '_' | '~' | '\'' | '"') => end -= 1,
            Some(')') if link.matches(')').count() > link.matches('(').count() => end -= 1,
            Some(';') => {
                let stem = link
                    .trim_end_matches(';')
                    .trim_end_matches(|c: char| c.is_ascii_alphanumeric());
                if stem.ends_with('&') && stem.len() + 1 < link.len() {
                    end = stem.len() - 1;
                } else {
                    return end;
                }
            }
            _ => return end,
        }
    }
}

fn domain_ok(domain: &str, need_dot: bool) -> bool {
    let parts: Vec<&str> = domain.split('.').collect();
    !domain.is_empty()
        && (!need_dot || parts.len() > 1)
        && parts.iter().rev().take(2).all(|p| !p.contains('_'))
        && parts.iter().all(|p| !p.is_empty() || parts.len() == 1)
}

fn url_at(text: &str, at: usize) -> Option<(Range<usize>, String)> {
    let rest = text.get(at..)?;
    let (prefix, scheme) = ["www.", "http://", "https://", "ftp://"]
        .iter()
        .find(|p| rest.starts_with(**p))
        .map(|p| (*p, if *p == "www." { "http://" } else { "" }))?;
    let domain_start = if prefix == "www." { 0 } else { prefix.len() };
    let domain_len = rest
        .get(domain_start..)?
        .bytes()
        .take_while(|c| c.is_ascii_alphanumeric() || matches!(c, b'-' | b'_' | b'.'))
        .count();
    let domain = rest
        .get(domain_start..domain_start + domain_len)?
        .trim_end_matches('.');
    if !domain_ok(domain, prefix == "www.") {
        return None;
    }
    let raw_end = rest
        .find(|c: char| c.is_whitespace() || c == '<')
        .unwrap_or(rest.len());
    let end = trim_link_end(rest, raw_end);
    if end <= domain_start {
        return None;
    }
    let link = rest.get(..end)?;
    Some((at..at + end, normalize_uri(&format!("{scheme}{link}"))))
}

fn email_at(text: &str, at: usize, floor: usize) -> Option<(Range<usize>, String)> {
    let b = text.as_bytes();
    let local = |c: &u8| c.is_ascii_alphanumeric() || matches!(c, b'.' | b'+' | b'-' | b'_');
    let start = at
        - b.get(floor..at)?
            .iter()
            .rev()
            .take_while(|c| local(c))
            .count();
    if start == at {
        return None;
    }
    let domain_len = b
        .get(at + 1..)?
        .iter()
        .take_while(|c| c.is_ascii_alphanumeric() || matches!(c, b'-' | b'_' | b'.'))
        .count();
    let domain = text.get(at + 1..at + 1 + domain_len)?.trim_end_matches('.');
    if !domain.contains('.') || domain.ends_with(['-', '_']) {
        return None;
    }
    let end = at + 1 + domain.len();
    let address = text.get(start..end)?;
    Some((start..end, format!("mailto:{address}")))
}

pub(crate) fn extended_autolinks(text: &str) -> Vec<(Range<usize>, String)> {
    let mut found = Vec::new();
    let mut floor = 0;
    let mut i = 0;
    while i < text.len() {
        let before = text.get(..i).and_then(|t| t.chars().next_back());
        let boundary =
            before.is_none_or(|c| c.is_whitespace() || matches!(c, '*' | '_' | '~' | '('));
        let hit = if text.as_bytes().get(i) == Some(&b'@') {
            email_at(text, i, floor)
        } else if boundary {
            url_at(text, i)
        } else {
            None
        };
        match hit {
            Some((range, destination)) => {
                i = range.end;
                floor = range.end;
                found.push((range, destination));
            }
            None => {
                i += text
                    .get(i..)
                    .and_then(|t| t.chars().next())
                    .map_or(1, char::len_utf8)
            }
        }
    }
    found
}
