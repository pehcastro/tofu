use std::error::Error;
use std::ops::Range;
use std::path::PathBuf;
use std::time::Instant;

use desk_core::buffer::Buffer;
use desk_core::syntax::{Language, Syntax};

const SHOWN_LINES: usize = 10;
const INSERT_LINE: usize = 100;
const AROUND: usize = 5;
const INSERTED: &str = "fn x() {}\n";

fn ms(since: Instant) -> f64 {
    since.elapsed().as_secs_f64() * 1000.0
}

fn render(
    buffer: &Buffer,
    syntax: &Syntax,
    lines: Range<usize>,
) -> Result<Vec<String>, Box<dyn Error>> {
    let shown = syntax.spans(lines.clone());
    let mut rendered = Vec::new();
    for index in lines.start..lines.end.min(buffer.line_count()) {
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
        rendered.push(format!("{:>3} | {out}", index + 1));
    }
    Ok(rendered)
}

fn sync(label: &str, buffer: &mut Buffer, syntax: &mut Syntax) -> Result<(), Box<dyn Error>> {
    let started = Instant::now();
    syntax.sync(buffer)?;
    println!("{label}: reparse {:.3} ms", ms(started));
    Ok(())
}

fn print(title: &str, lines: &[String]) {
    println!("{title}");
    for line in lines {
        println!("{line}");
    }
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
    let mut buffer = Buffer::load(&path)?;
    println!(
        "{} {language:?} {:?} {} lines",
        path.display(),
        buffer.line_ending(),
        buffer.line_count()
    );

    let started = Instant::now();
    let mut syntax = Syntax::new(language, &buffer)?;
    println!("full parse {:.3} ms", ms(started));
    print(
        "shown:",
        &render(&buffer, &syntax, top..top.saturating_add(SHOWN_LINES))?,
    );

    let line = (INSERT_LINE - 1).min(buffer.line_count().saturating_sub(1));
    let around = line.saturating_sub(AROUND)..line + AROUND + 1;
    let before = render(&buffer, &syntax, around.clone())?;
    print("before insert:", &before);

    let at = buffer.line_to_char(line)?;
    buffer.edit(std::slice::from_ref(&(at..at)), INSERTED)?;
    sync(
        &format!("insert {INSERTED:?} at line {}", line + 1),
        &mut buffer,
        &mut syntax,
    )?;
    print("after insert:", &render(&buffer, &syntax, around.clone())?);
    buffer.undo();
    sync("undo", &mut buffer, &mut syntax)?;
    let undone = render(&buffer, &syntax, around)?;
    print("after undo:", &undone);
    println!("equal {}", before == undone);
    buffer.redo();
    sync("redo", &mut buffer, &mut syntax)?;
    let redone = render(&buffer, &syntax, line..line + 1)?;
    print("after redo:", &redone);
    println!(
        "redo has [function:x] {}",
        redone.iter().any(|l| l.contains("[function:x]"))
    );
    buffer.undo();
    sync("undo again", &mut buffer, &mut syntax)?;

    let typed = (line..buffer.line_count())
        .find(|&index| !buffer.line(index).unwrap_or_default().trim().is_empty())
        .unwrap_or(line);
    let one = typed..typed + 1;
    let before = render(&buffer, &syntax, one.clone())?;
    print("before newline:", &before);
    let middle = buffer.line(typed).unwrap_or_default().chars().count() / 2;
    let at = buffer.line_to_char(typed)? + middle;
    buffer.edit(std::slice::from_ref(&(at..at)), "\n")?;
    sync(
        &format!("newline at char {middle} of line {}", typed + 1),
        &mut buffer,
        &mut syntax,
    )?;
    print(
        "after newline:",
        &render(&buffer, &syntax, typed..typed + 2)?,
    );
    buffer.undo();
    sync("undo newline", &mut buffer, &mut syntax)?;
    let undone = render(&buffer, &syntax, one)?;
    print("after undo:", &undone);
    println!("equal {}", before == undone);
    Ok(())
}
