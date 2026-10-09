use std::error::Error;
use std::fs;
use std::path::Path;

use desk_markdown::{Json, Options, parse, parse_json, to_html};

struct Case {
    number: usize,
    section: String,
    markdown: String,
    html: String,
}

fn field<'a>(fields: &'a [(String, Json)], name: &str) -> Option<&'a Json> {
    fields.iter().find(|(k, _)| k == name).map(|(_, v)| v)
}

fn commonmark_cases(text: &str) -> Result<Vec<Case>, Box<dyn Error>> {
    let Json::Array(items) = parse_json(text)? else {
        return Err("spec.json is not an array".into());
    };
    items
        .iter()
        .map(|item| {
            let Json::Object(fields) = item else {
                return None;
            };
            let text = |name| match field(fields, name) {
                Some(Json::String(s)) => Some(s.clone()),
                _ => None,
            };
            let number = match field(fields, "example") {
                Some(Json::Number(n)) => format!("{n}").parse().ok()?,
                _ => return None,
            };
            Some(Case {
                number,
                section: text("section")?,
                markdown: text("markdown")?,
                html: text("html")?,
            })
        })
        .collect::<Option<_>>()
        .ok_or_else(|| "an example lacks a field".into())
}

fn gfm_cases(text: &str) -> Vec<Case> {
    let fence = "`".repeat(32);
    let mut cases = Vec::new();
    let mut section = String::new();
    let mut lines = text.lines();
    let mut number = 0;
    while let Some(line) = lines.next() {
        if let Some(title) = line.strip_prefix("## ").or_else(|| line.strip_prefix("# ")) {
            section = title.trim_end_matches(" (extension)").to_string();
        }
        let Some(kind) = line
            .strip_prefix(&fence)
            .and_then(|r| r.strip_prefix(" example"))
        else {
            continue;
        };
        number += 1;
        let mut markdown = String::new();
        let mut html = String::new();
        for l in lines.by_ref().take_while(|l| *l != ".") {
            markdown.push_str(l);
            markdown.push('\n');
        }
        for l in lines.by_ref().take_while(|l| *l != fence) {
            html.push_str(l);
            html.push('\n');
        }
        if [" table", " strikethrough", " autolink", " disabled"].contains(&kind) {
            cases.push(Case {
                number,
                section: section.clone(),
                markdown: markdown.replace('→', "\t"),
                html: html.replace('→', "\t"),
            });
        }
    }
    cases
}

const BLOCK_TAGS: [&str; 20] = [
    "p",
    "li",
    "ul",
    "ol",
    "h1",
    "h2",
    "h3",
    "h4",
    "h5",
    "h6",
    "blockquote",
    "pre",
    "hr",
    "table",
    "thead",
    "tbody",
    "tr",
    "th",
    "td",
    "div",
];

fn normalize(html: &str) -> String {
    let mut tokens: Vec<String> = Vec::new();
    let mut rest = html;
    while !rest.is_empty() {
        let len = if rest.starts_with('<') {
            rest.find('>').map_or(rest.len(), |i| i + 1)
        } else {
            rest.find('<').unwrap_or(rest.len())
        };
        let (token, tail) = rest.split_at(len);
        tokens.push(token.replace(" />", ">").replace("/>", ">"));
        rest = tail;
    }
    let is_block = |t: &str| {
        let name = t.trim_start_matches('<').trim_start_matches('/');
        let name: String = name
            .chars()
            .take_while(char::is_ascii_alphanumeric)
            .collect();
        t.starts_with('<') && BLOCK_TAGS.contains(&name.as_str())
    };
    let mut out = String::new();
    let mut in_pre = false;
    for (i, token) in tokens.iter().enumerate() {
        if token.starts_with("<pre") {
            in_pre = true;
        } else if token.starts_with("</pre") {
            in_pre = false;
        }
        if token.starts_with('<') || in_pre {
            out.push_str(token);
            continue;
        }
        let mut text = token.split_whitespace().collect::<Vec<_>>().join(" ");
        if token.starts_with(char::is_whitespace) && !text.is_empty() {
            text.insert(0, ' ');
        }
        if token.ends_with(char::is_whitespace) && !text.is_empty() {
            text.push(' ');
        }
        if text.is_empty() && !token.is_empty() {
            text.push(' ');
        }
        if i > 0 && tokens.get(i - 1).is_some_and(|t| is_block(t)) {
            text = text.trim_start().to_string();
        }
        if tokens.get(i + 1).is_some_and(|t| is_block(t)) {
            text = text.trim_end().to_string();
        }
        out.push_str(&text);
    }
    out
}

fn report(label: &str, cases: &[Case], options: Options, show: Option<usize>) {
    let mut failed: Vec<(String, usize)> = Vec::new();
    let mut pass = 0;
    for case in cases {
        let got = to_html(&parse(&case.markdown, options));
        if got == case.html || normalize(&got) == normalize(&case.html) {
            pass += 1;
        } else {
            match failed.iter_mut().find(|(s, _)| *s == case.section) {
                Some((_, n)) => *n += 1,
                None => failed.push((case.section.clone(), 1)),
            }
            if show.is_some_and(|n| n == 0 || n == case.number) {
                println!(
                    "--- {label} example {} ({})\n{:?}\nwant {:?}\ngot  {:?}",
                    case.number, case.section, case.markdown, case.html, got
                );
            }
        }
    }
    println!("{label}: {pass}/{} pass", cases.len());
    for (section, n) in failed {
        println!("  failing in {section}: {n}");
    }
}

fn main() -> Result<(), Box<dyn Error>> {
    let dir = Path::new(env!("CARGO_MANIFEST_DIR")).join("spec");
    let show = std::env::args().nth(1).and_then(|a| a.parse().ok());
    let commonmark = commonmark_cases(&fs::read_to_string(dir.join("spec.json"))?)?;
    report("commonmark", &commonmark, Options::COMMONMARK, show);
    report(
        "commonmark with gfm and components on",
        &commonmark,
        Options::ALL,
        show,
    );
    let gfm = gfm_cases(&fs::read_to_string(dir.join("gfm-spec.txt"))?);
    report(
        "gfm extensions (table, task list, strikethrough, autolink)",
        &gfm,
        Options::GFM,
        show.map(|_| 0),
    );
    Ok(())
}
