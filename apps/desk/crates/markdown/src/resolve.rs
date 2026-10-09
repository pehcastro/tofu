use crate::Options;
use crate::ast::{Block, BlockKind, Cell, Component, List, ListItem, Table};
use crate::block::{Mapped, RawBlock, RawKind, Refs, split_row};
use crate::inline;

pub(crate) fn blocks(raw: &[RawBlock], refs: &Refs, options: Options) -> Vec<Block> {
    raw.iter().map(|r| block(r, refs, options)).collect()
}

fn block(raw: &RawBlock, refs: &Refs, options: Options) -> Block {
    let kind = match &raw.kind {
        RawKind::BlockQuote => BlockKind::BlockQuote(blocks(&raw.children, refs, options)),
        RawKind::List { kind, tight, items } => BlockKind::List(List {
            kind: *kind,
            tight: *tight,
            items: items
                .iter()
                .map(|item| ListItem {
                    range: item.range.clone(),
                    task: item.task,
                    blocks: blocks(&item.blocks, refs, options),
                })
                .collect(),
        }),
        RawKind::Paragraph(text) => BlockKind::Paragraph(inline::parse(text, refs, options)),
        RawKind::Heading(level, text) => BlockKind::Heading {
            level: *level,
            inlines: inline::parse(text, refs, options),
        },
        RawKind::ThematicBreak => BlockKind::ThematicBreak,
        RawKind::Code(kind, text) => BlockKind::Code {
            kind: kind.clone(),
            text: text.clone(),
        },
        RawKind::Html(text) => BlockKind::Html(text.clone()),
        RawKind::Table {
            alignments,
            header,
            rows,
        } => {
            let width = alignments.len();
            let row = |(at, line): &(usize, String)| {
                let mut cells = split_row(line).into_iter();
                (0..width)
                    .map(|_| {
                        let range = cells.next().unwrap_or(line.len()..line.len());
                        let source = line.get(range.clone()).unwrap_or_default();
                        let mut text = Mapped::default();
                        let mut from = 0;
                        for (i, _) in source.match_indices("\\|") {
                            text.push(
                                source.get(from..i).unwrap_or_default(),
                                at + range.start + from,
                            );
                            from = i + 1;
                        }
                        text.push(
                            source.get(from..).unwrap_or_default(),
                            at + range.start + from,
                        );
                        Cell {
                            range: at + range.start..at + range.end,
                            inlines: inline::parse(&text, refs, options),
                        }
                    })
                    .collect()
            };
            BlockKind::Table(Table {
                alignments: alignments.clone(),
                header: row(header),
                rows: rows.iter().map(row).collect(),
            })
        }
        RawKind::Component { name, props } => BlockKind::Component(Component {
            name: name.clone(),
            props: props.clone(),
            children: blocks(&raw.children, refs, options),
        }),
    };
    Block {
        range: raw.range.clone(),
        kind,
    }
}
