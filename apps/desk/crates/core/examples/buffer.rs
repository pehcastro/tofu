use std::error::Error;
use std::fs;
use std::io::{BufWriter, Write};
use std::path::{Path, PathBuf};
use std::thread;
use std::time::{Duration, Instant};

use desk_core::buffer::Buffer;

const INSERTS: usize = 1000;

fn main() -> Result<(), Box<dyn Error>> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match args
        .iter()
        .map(String::as_str)
        .collect::<Vec<_>>()
        .as_slice()
    {
        ["gen", path, lines, ending] => generate(Path::new(path), lines.parse()?, ending),
        ["typing"] => typing(),
        [path] => bench(Path::new(path)),
        _ => Err("usage: buffer <file> | gen <path> <lines> <lf|crlf> | typing".into()),
    }
}

fn generate(path: &Path, lines: usize, ending: &str) -> Result<(), Box<dyn Error>> {
    let newline = match ending {
        "lf" => "\n",
        "crlf" => "\r\n",
        _ => return Err(format!("unknown ending {ending}").into()),
    };
    let mut out = BufWriter::new(fs::File::create(path)?);
    for line in 0..lines {
        write!(
            out,
            "{line:>6} the quick brown fox jumps over the lazy dog{newline}"
        )?;
    }
    out.flush()?;
    println!("wrote {} lines to {}", lines, path.display());
    Ok(())
}

fn bench(path: &Path) -> Result<(), Box<dyn Error>> {
    let original = fs::read_to_string(path)?;
    let started = Instant::now();
    let mut buffer = Buffer::load(path)?;
    println!("load ms {:.2}", ms(started));
    println!(
        "lines {} chars {} ending {:?} dirty {}",
        buffer.line_count(),
        buffer.len_chars(),
        buffer.line_ending(),
        buffer.is_dirty()
    );
    let len = buffer.len_chars();
    let cursors = [len / 4..len / 4, len / 2..len / 2, len * 3 / 4..len * 3 / 4];
    let started = Instant::now();
    let mut at = cursors.to_vec();
    for _ in 0..INSERTS {
        buffer.edit(&at, "x")?;
        at = buffer.selections().to_vec();
    }
    let elapsed = started.elapsed();
    println!(
        "inserts {} at {} cursors total ms {:.2} per edit us {:.2}",
        INSERTS,
        cursors.len(),
        elapsed.as_secs_f64() * 1000.0,
        elapsed.as_secs_f64() * 1_000_000.0 / INSERTS as f64
    );
    println!(
        "after inserts chars {} dirty {} selections {:?}",
        buffer.len_chars(),
        buffer.is_dirty(),
        buffer.selections()
    );
    let started = Instant::now();
    let mut steps = 0;
    while buffer.undo() {
        steps += 1;
    }
    println!(
        "undo steps {} ms {:.2} dirty {}",
        steps,
        ms(started),
        buffer.is_dirty()
    );
    println!("equal {}", buffer.text() == original);
    let saved = PathBuf::from(format!("{}.saved", path.display()));
    let started = Instant::now();
    buffer.save(&saved)?;
    println!("save ms {:.2} to {}", ms(started), saved.display());
    Ok(())
}

fn typing() -> Result<(), Box<dyn Error>> {
    let mut buffer = Buffer::from_text("");
    type_word(&mut buffer, "hello")?;
    show(&buffer, "typed hello");
    thread::sleep(Duration::from_millis(600));
    type_word(&mut buffer, "world")?;
    show(&buffer, "typed world after 600 ms");
    buffer.undo();
    show(&buffer, "undo 1");
    buffer.undo();
    show(&buffer, "undo 2");
    buffer.redo();
    show(&buffer, "redo 1");
    Ok(())
}

fn type_word(buffer: &mut Buffer, word: &str) -> Result<(), Box<dyn Error>> {
    for letter in word.chars() {
        let at = buffer.selections().to_vec();
        buffer.edit(&at, letter.encode_utf8(&mut [0; 4]))?;
    }
    Ok(())
}

fn show(buffer: &Buffer, label: &str) {
    println!(
        "{label}: text {:?} dirty {} selections {:?}",
        buffer.text(),
        buffer.is_dirty(),
        buffer.selections()
    );
}

fn ms(started: Instant) -> f64 {
    started.elapsed().as_secs_f64() * 1000.0
}
