use std::error::Error;
use std::path::PathBuf;
use std::time::Instant;

use desk_core::buffer::Buffer;
use desk_core::syntax::{Language, Syntax};

const SHOWN_LINES: usize = 10;
const INSERT_LINE: usize = 100;
const INSERTED: &str = "fn x() {}\n";

fn ms(since: Instant) -> f64 {
    since.elapsed().as_secs_f64() * 1000.0
}

fn main() -> Result<(), Box<dyn Error>> {
    let mut path = None;
    let mut extension = None;
    let mut first: usize = 1;
    let mut args = std::env::args().skip(1);
    while let Some(arg) = args.next() {
        match arg.as_str() {
            "--as" => extension = args.next(),
            "--show" => first = args.next().ok_or("--show needs a line")?.parse()?,
            _ => path = Some(PathBuf::from(arg)),
        }
    }
    let path = path.ok_or("usage: syntax <file> [--as extension] [--show first line]")?;
    let named = extension.map_or_else(|| path.clone(), |ext| path.with_extension(ext));
    let language = Language::from_path(&named).ok_or("no grammar for this extension")?;
    let top = first.saturating_sub(1);
    let shown_lines = top..top.saturating_add(SHOWN_LINES);
    let mut buffer = Buffer::load(&path)?;
    println!(
        "{} {language:?} {} lines",
        path.display(),
        buffer.line_count()
    );

    let started = Instant::now();
    let mut syntax = Syntax::new(language, &buffer)?;
    println!("full parse {:.3} ms", ms(started));

    let started = Instant::now();
    let spans = syntax.spans(0..60);
    println!("spans lines 1-60: {} in {:.3} ms", spans.len(), ms(started));

    let shown = syntax.spans(shown_lines.clone());
    for index in shown_lines.start..shown_lines.end.min(buffer.line_count()) {
        let start = buffer.line_to_char(index)?;
        let line: Vec<char> = buffer.line(index).unwrap_or_default().chars().collect();
        let mut out = String::new();
        let mut at = 0;
        for span in &shown {
            let from = span.chars.start.saturating_sub(start);
            let to = span.chars.end.saturating_sub(start).min(line.len());
            if span.chars.end <= start || from >= line.len() || from < at {
                continue;
            }
            out.extend(line.get(at..from).unwrap_or_default());
            let text: String = line.get(from..to).unwrap_or_default().iter().collect();
            out.push_str(&format!("[{}:{text}]", span.kind.name()));
            at = to;
        }
        out.extend(line.get(at..).unwrap_or_default());
        println!("{:>3} | {out}", index + 1);
    }

    let line = (INSERT_LINE - 1).min(buffer.line_count() - 1);
    let at = buffer.line_to_char(line)?;
    let point = at..at;
    buffer.edit(std::slice::from_ref(&point), INSERTED)?;
    let started = Instant::now();
    syntax.edit(point, INSERTED)?;
    println!(
        "insert {INSERTED:?} at line {}: incremental reparse {:.3} ms",
        line + 1,
        ms(started)
    );
    let started = Instant::now();
    let spans = syntax.spans(89..110);
    println!(
        "spans lines 90-110: {} in {:.3} ms",
        spans.len(),
        ms(started)
    );
    let text = buffer.text();
    let inserted = spans
        .iter()
        .filter(|span| span.chars.start >= at && span.chars.end <= at + INSERTED.len())
        .map(|span| {
            let word: String = text
                .chars()
                .skip(span.chars.start)
                .take(span.chars.len())
                .collect();
            format!("[{}:{word}]", span.kind.name())
        })
        .collect::<String>();
    println!("inserted line spans: {inserted}");
    Ok(())
}
