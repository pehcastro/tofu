use std::error::Error;
use std::io::{BufWriter, Write};
use std::time::{Duration, Instant};

use desk_markdown::{Options, Stream, parse, visible_prefix};

const CHUNK: usize = 7;

fn main() -> Result<(), Box<dyn Error>> {
    let path = std::env::args().nth(1).ok_or("usage: stream <file>")?;
    let source = std::fs::read_to_string(&path)?;
    let mut out = BufWriter::new(std::io::stdout().lock());
    let mut stream = Stream::new(Options::ALL);
    let (mut streamed, mut reparsed) = (Duration::ZERO, Duration::ZERO);
    let (mut chunks, mut differing) = (0usize, 0usize);
    let mut end = 0;
    while end < source.len() {
        let mut next = (end + CHUNK).min(source.len());
        while !source.is_char_boundary(next) {
            next += 1;
        }
        let chunk = source.get(end..next).ok_or("chunk outside the file")?;
        end = next;
        chunks += 1;
        let full_started = Instant::now();
        let full = parse(
            visible_prefix(source.get(..end).ok_or("prefix outside the file")?),
            Options::ALL,
        );
        reparsed += full_started.elapsed();
        let stream_started = Instant::now();
        stream.push(chunk);
        let snapshot = stream.snapshot();
        streamed += stream_started.elapsed();
        let same = *snapshot == full;
        differing += usize::from(!same);
        writeln!(
            out,
            "chunk {chunks} to byte {end}: {}",
            if same {
                "same as full parse"
            } else {
                "DIFFERS from full parse"
            }
        )?;
    }
    writeln!(
        out,
        "stream matches full parse: {}",
        if differing == 0 { "yes" } else { "no" }
    )?;
    writeln!(
        out,
        "{chunks} chunks of {CHUNK} bytes, {differing} differing; stream {streamed:.2?} total against a full reparse on every chunk {reparsed:.2?}"
    )?;
    out.flush()?;
    Ok(())
}
