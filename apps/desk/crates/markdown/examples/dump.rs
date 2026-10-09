use std::error::Error;
use std::fmt::{self, Write};
use std::time::Instant;

use desk_markdown::{
    Block, BlockKind, Component, Inline, InlineKind, Json, Options, PropKind, PropSpec, PropValue,
    Registry, parse, validate,
};

const RUNS: u32 = 100;

fn chart_registry() -> Registry {
    let prop = |name: &str, kind, required| PropSpec {
        name: name.to_string(),
        kind,
        required,
    };
    let mut registry = Registry::default();
    registry.define(
        "Chart",
        vec![
            prop("kind", PropKind::Text, true),
            prop("data", PropKind::Array, true),
            prop("title", PropKind::Text, false),
        ],
    );
    registry
}

fn json(value: &Json) -> String {
    match value {
        Json::Null => "null".into(),
        Json::Bool(b) => b.to_string(),
        Json::Number(n) => n.to_string(),
        Json::String(s) => format!("{s:?}"),
        Json::Array(items) => format!("[{}]", items.iter().map(json).collect::<Vec<_>>().join(",")),
        Json::Object(fields) => {
            format!(
                "{{{}}}",
                fields
                    .iter()
                    .map(|(k, v)| format!("{k:?}:{}", json(v)))
                    .collect::<Vec<_>>()
                    .join(",")
            )
        }
    }
}

fn component<C>(out: &mut String, pad: &str, c: &Component<C>, registry: &Registry) -> fmt::Result {
    let props: Vec<String> = c
        .props
        .iter()
        .map(|(k, v)| match v {
            PropValue::Text(t) => format!("{k}={t:?}"),
            PropValue::Json(j) => format!("{k}={{{}}}", json(j)),
            PropValue::Invalid { raw, error } => format!("{k}=invalid json {raw:?} ({error})"),
        })
        .collect();
    writeln!(out, "{pad}  props: {}", props.join(" "))?;
    match validate(c, registry) {
        Ok(()) => writeln!(out, "{pad}  validate: ok"),
        Err(e) => writeln!(out, "{pad}  validate: error: {e}"),
    }
}

fn block(out: &mut String, depth: usize, b: &Block, registry: &Registry) -> fmt::Result {
    let pad = "  ".repeat(depth);
    let head = match &b.kind {
        BlockKind::Heading { level, .. } => format!("Heading {level}"),
        BlockKind::Paragraph(_) => "Paragraph".into(),
        BlockKind::ThematicBreak => "ThematicBreak".into(),
        BlockKind::BlockQuote(_) => "BlockQuote".into(),
        BlockKind::List(l) => format!("List {:?} tight={}", l.kind, l.tight),
        BlockKind::Code { kind, text } => format!("Code {kind:?} {} bytes", text.len()),
        BlockKind::Html(text) => format!("Html {text:?}"),
        BlockKind::Table(t) => format!(
            "Table {} columns, {} rows",
            t.alignments.len(),
            t.rows.len()
        ),
        BlockKind::Component(c) => format!("Component {}", c.name),
    };
    writeln!(out, "{pad}{head} {}..{}", b.range.start, b.range.end)?;
    match &b.kind {
        BlockKind::Heading { inlines: list, .. } | BlockKind::Paragraph(list) => {
            inlines(out, depth + 1, list, registry)
        }
        BlockKind::BlockQuote(children) => children
            .iter()
            .try_for_each(|c| block(out, depth + 1, c, registry)),
        BlockKind::List(l) => l.items.iter().try_for_each(|item| {
            writeln!(
                out,
                "{pad}  Item task={:?} {}..{}",
                item.task, item.range.start, item.range.end
            )?;
            item.blocks
                .iter()
                .try_for_each(|c| block(out, depth + 2, c, registry))
        }),
        BlockKind::Table(t) => {
            t.header
                .iter()
                .chain(t.rows.iter().flatten())
                .try_for_each(|cell| {
                    writeln!(out, "{pad}  Cell {}..{}", cell.range.start, cell.range.end)?;
                    inlines(out, depth + 2, &cell.inlines, registry)
                })
        }
        BlockKind::Component(c) => {
            component(out, &pad, c, registry)?;
            c.children
                .iter()
                .try_for_each(|child| block(out, depth + 1, child, registry))
        }
        BlockKind::ThematicBreak | BlockKind::Code { .. } | BlockKind::Html(_) => Ok(()),
    }
}

fn inlines(out: &mut String, depth: usize, list: &[Inline], registry: &Registry) -> fmt::Result {
    let pad = "  ".repeat(depth);
    for inline in list {
        let (head, children): (String, &[Inline]) = match &inline.kind {
            InlineKind::Text(t) => (format!("Text {t:?}"), &[]),
            InlineKind::Code(t) => (format!("Code {t:?}"), &[]),
            InlineKind::Emphasis(c) => ("Emphasis".into(), c),
            InlineKind::Strong(c) => ("Strong".into(), c),
            InlineKind::Strikethrough(c) => ("Strikethrough".into(), c),
            InlineKind::Link(l) => (format!("Link {:?}", l.destination), &l.children),
            InlineKind::Image(l) => (format!("Image {:?}", l.destination), &l.children),
            InlineKind::Autolink { destination, .. } => (format!("Autolink {destination:?}"), &[]),
            InlineKind::SoftBreak => ("SoftBreak".into(), &[]),
            InlineKind::HardBreak => ("HardBreak".into(), &[]),
            InlineKind::Html(t) => (format!("Html {t:?}"), &[]),
            InlineKind::Component(c) => (format!("Component {}", c.name), &c.children),
        };
        writeln!(
            out,
            "{pad}{head} {}..{}",
            inline.range.start, inline.range.end
        )?;
        if let InlineKind::Component(c) = &inline.kind {
            component(out, &pad, c, registry)?;
        }
        inlines(out, depth + 1, children, registry)?;
    }
    Ok(())
}

fn main() -> Result<(), Box<dyn Error>> {
    let path = std::env::args().nth(1).ok_or("usage: dump <file>")?;
    let source = std::fs::read_to_string(&path)?;
    let started = Instant::now();
    for _ in 1..RUNS {
        std::hint::black_box(parse(std::hint::black_box(&source), Options::ALL));
    }
    let doc = parse(&source, Options::ALL);
    let elapsed = started.elapsed();
    let registry = chart_registry();
    let mut out = String::new();
    for b in &doc.blocks {
        block(&mut out, 0, b, &registry)?;
    }
    print!("{out}");
    println!(
        "parsed {} bytes into {} blocks; {RUNS} runs took {:.2?}, {:.2?} per parse",
        source.len(),
        doc.blocks.len(),
        elapsed,
        elapsed / RUNS
    );
    Ok(())
}
