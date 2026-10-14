use std::error::Error;
use std::io::{BufWriter, Write};
use std::time::Instant;

use desk_markdown::{
    Block, BlockKind, Document, InlineKind, Options, PropKind, PropSpec, Registry, Stream, parse,
    to_html, validate,
};

const CASES: [&str; 2] = [
    "<Card\n  title=\"x\"\n>hi</Card>",
    "<Chart data={\"a\":\">\"} />",
];
const DELTA: usize = 7;

fn components(blocks: &[Block]) -> usize {
    blocks
        .iter()
        .map(|b| match &b.kind {
            BlockKind::Component(c) => 1 + components(&c.children),
            BlockKind::Paragraph(inlines) => inlines
                .iter()
                .filter(|i| matches!(i.kind, InlineKind::Component(_)))
                .count(),
            _ => 0,
        })
        .sum()
}

fn first_component(doc: &Document) -> Option<&desk_markdown::Component<Block>> {
    doc.blocks.iter().find_map(|b| match &b.kind {
        BlockKind::Component(c) => Some(c),
        _ => None,
    })
}

fn main() -> Result<(), Box<dyn Error>> {
    let mut out = BufWriter::new(std::io::stdout().lock());
    for case in CASES {
        writeln!(out, "case {case:?}")?;
        let mut stream = Stream::new(Options::ALL);
        let mut leaked = 0usize;
        for (step, ch) in case.chars().enumerate() {
            stream.push(ch.encode_utf8(&mut [0; 4]));
            let html = to_html(stream.snapshot().0);
            let leak = ["&lt;", "<Card", "<Chart", "</Card", "</Chart"]
                .iter()
                .any(|raw| html.contains(raw));
            leaked += usize::from(leak);
            writeln!(
                out,
                "  step {step:>2} {}: {html:?}",
                if leak { "PARTIAL TAG AS TEXT" } else { "ok" }
            )?;
        }
        writeln!(
            out,
            "  steps with a partial tag as text: {leaked}; components at the end: {}",
            components(&stream.snapshot().0.blocks)
        )?;
    }

    let mut registry = Registry::default();
    registry.define(
        "Card",
        vec![PropSpec {
            name: "title".into(),
            kind: PropKind::Text,
            required: true,
        }],
    );
    let big = format!("<Card title=\"{}\" />\n", "x".repeat(70_000));
    let doc = parse(&big, Options::ALL);
    match first_component(&doc).map(|c| validate(c, &registry)) {
        Some(Err(e)) => writeln!(out, "prop over the byte cap: PropError: {e}")?,
        Some(Ok(())) => writeln!(out, "prop over the byte cap: accepted, no PropError")?,
        None => writeln!(out, "prop over the byte cap: no component parsed")?,
    }

    let mut message = String::new();
    for n in 0..30 {
        message.push_str(&format!(
            "<Chart\n  title=\"chart {n}\"\n  data={{\"points\":[1,2,3],\"label\":\"a > b\"}}\n>\nSome **text** in chart {n}.\n</Chart>\n\n"
        ));
        while message.len() < (n + 1) * 20_000 / 30 {
            message.push_str("A paragraph line with `code` and *emphasis* to fill the message.\n");
        }
        message.push('\n');
    }
    let mut stream = Stream::new(Options::ALL);
    let (mut deltas, mut end) = (0usize, 0usize);
    let started = Instant::now();
    while end < message.len() {
        let mut next = (end + DELTA).min(message.len());
        while !message.is_char_boundary(next) {
            next += 1;
        }
        stream.push(message.get(end..next).ok_or("delta outside the message")?);
        stream.snapshot();
        end = next;
        deltas += 1;
    }
    let micros = started.elapsed().as_secs_f64() * 1e6 / deltas as f64;
    writeln!(
        out,
        "timing: {} bytes, {} components, {deltas} deltas of {DELTA} bytes, {micros:.1} us per delta",
        message.len(),
        components(&stream.snapshot().0.blocks)
    )?;
    out.flush()?;
    Ok(())
}

#[test]
fn stream() -> Result<(), Box<dyn Error>> {
    main()
}
