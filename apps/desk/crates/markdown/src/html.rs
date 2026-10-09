use crate::ast::{
    Alignment, Block, BlockKind, Cell, CodeKind, Document, Inline, InlineKind, ListKind,
};

pub fn to_html(doc: &Document) -> String {
    let mut out = String::new();
    blocks(&mut out, &doc.blocks, false);
    out
}

fn cr(out: &mut String) {
    if !out.is_empty() && !out.ends_with('\n') {
        out.push('\n');
    }
}

fn escape(out: &mut String, s: &str) {
    for c in s.chars() {
        match c {
            '&' => out.push_str("&amp;"),
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '"' => out.push_str("&quot;"),
            c => out.push(c),
        }
    }
}

fn blocks(out: &mut String, list: &[Block], tight: bool) {
    for b in list {
        block(out, b, tight);
    }
}

fn block(out: &mut String, b: &Block, tight: bool) {
    match &b.kind {
        BlockKind::Paragraph(i) if tight => inlines(out, i),
        BlockKind::Paragraph(i) => {
            cr(out);
            out.push_str("<p>");
            inlines(out, i);
            out.push_str("</p>\n");
        }
        BlockKind::Heading { level, inlines: i } => {
            cr(out);
            out.push_str(&format!("<h{level}>"));
            inlines(out, i);
            out.push_str(&format!("</h{level}>\n"));
        }
        BlockKind::ThematicBreak => {
            cr(out);
            out.push_str("<hr />\n");
        }
        BlockKind::BlockQuote(children) => {
            cr(out);
            out.push_str("<blockquote>\n");
            blocks(out, children, false);
            cr(out);
            out.push_str("</blockquote>\n");
        }
        BlockKind::List(list) => {
            cr(out);
            let (open, tag) = match list.kind {
                ListKind::Bullet(_) => ("<ul>\n".to_string(), "ul"),
                ListKind::Ordered { start: 1, .. } => ("<ol>\n".to_string(), "ol"),
                ListKind::Ordered { start, .. } => (format!("<ol start=\"{start}\">\n"), "ol"),
            };
            out.push_str(&open);
            for item in &list.items {
                out.push_str("<li>");
                match item.task {
                    Some(true) => {
                        out.push_str("<input checked=\"\" disabled=\"\" type=\"checkbox\"> ")
                    }
                    Some(false) => out.push_str("<input disabled=\"\" type=\"checkbox\"> "),
                    None => {}
                }
                blocks(out, &item.blocks, list.tight);
                out.push_str("</li>\n");
            }
            cr(out);
            out.push_str(&format!("</{tag}>\n"));
        }
        BlockKind::Code { kind, text } => {
            cr(out);
            out.push_str("<pre><code");
            if let CodeKind::Fenced { info } = kind
                && let Some(lang) = info.split_whitespace().next()
            {
                out.push_str(" class=\"language-");
                escape(out, lang);
                out.push('"');
            }
            out.push('>');
            escape(out, text);
            out.push_str("</code></pre>\n");
        }
        BlockKind::Html(text) => {
            cr(out);
            out.push_str(text);
            out.push('\n');
        }
        BlockKind::Table(table) => {
            cr(out);
            out.push_str("<table>\n<thead>\n");
            row(out, &table.header, &table.alignments, "th");
            out.push_str("</thead>\n");
            if !table.rows.is_empty() {
                out.push_str("<tbody>\n");
                for cells in &table.rows {
                    row(out, cells, &table.alignments, "td");
                }
                out.push_str("</tbody>\n");
            }
            out.push_str("</table>\n");
        }
        BlockKind::Component(c) => {
            cr(out);
            out.push_str("<div data-component=\"");
            escape(out, &c.name);
            out.push_str("\">\n");
            blocks(out, &c.children, false);
            cr(out);
            out.push_str("</div>\n");
        }
    }
}

fn row(out: &mut String, cells: &[Cell], alignments: &[Alignment], tag: &str) {
    out.push_str("<tr>\n");
    for (cell, alignment) in cells.iter().zip(alignments) {
        let align = match alignment {
            Alignment::None => "",
            Alignment::Left => " align=\"left\"",
            Alignment::Center => " align=\"center\"",
            Alignment::Right => " align=\"right\"",
        };
        out.push_str(&format!("<{tag}{align}>"));
        inlines(out, &cell.inlines);
        out.push_str(&format!("</{tag}>\n"));
    }
    out.push_str("</tr>\n");
}

fn inlines(out: &mut String, list: &[Inline]) {
    for inline in list {
        match &inline.kind {
            InlineKind::Text(t) => escape(out, t),
            InlineKind::Code(t) => {
                out.push_str("<code>");
                escape(out, t);
                out.push_str("</code>");
            }
            InlineKind::Emphasis(c) => wrap(out, "em", c),
            InlineKind::Strong(c) => wrap(out, "strong", c),
            InlineKind::Strikethrough(c) => wrap(out, "del", c),
            InlineKind::Link(link) => {
                out.push_str("<a href=\"");
                escape(out, &link.destination);
                out.push('"');
                title(out, &link.title);
                out.push('>');
                inlines(out, &link.children);
                out.push_str("</a>");
            }
            InlineKind::Image(link) => {
                out.push_str("<img src=\"");
                escape(out, &link.destination);
                out.push_str("\" alt=\"");
                plain(out, &link.children);
                out.push('"');
                title(out, &link.title);
                out.push_str(" />");
            }
            InlineKind::Autolink { destination, text } => {
                out.push_str("<a href=\"");
                escape(out, destination);
                out.push_str("\">");
                escape(out, text);
                out.push_str("</a>");
            }
            InlineKind::SoftBreak => out.push('\n'),
            InlineKind::HardBreak => out.push_str("<br />\n"),
            InlineKind::Html(t) => out.push_str(t),
            InlineKind::Component(c) => {
                out.push_str("<span data-component=\"");
                escape(out, &c.name);
                out.push_str("\">");
                inlines(out, &c.children);
                out.push_str("</span>");
            }
        }
    }
}

fn wrap(out: &mut String, tag: &str, children: &[Inline]) {
    out.push_str(&format!("<{tag}>"));
    inlines(out, children);
    out.push_str(&format!("</{tag}>"));
}

fn title(out: &mut String, title: &str) {
    if !title.is_empty() {
        out.push_str(" title=\"");
        escape(out, title);
        out.push('"');
    }
}

fn plain(out: &mut String, list: &[Inline]) {
    for inline in list {
        match &inline.kind {
            InlineKind::Text(t) | InlineKind::Code(t) | InlineKind::Html(t) => escape(out, t),
            InlineKind::Autolink { text, .. } => escape(out, text),
            InlineKind::SoftBreak | InlineKind::HardBreak => out.push('\n'),
            InlineKind::Emphasis(c) | InlineKind::Strong(c) | InlineKind::Strikethrough(c) => {
                plain(out, c)
            }
            InlineKind::Link(l) | InlineKind::Image(l) => plain(out, &l.children),
            InlineKind::Component(c) => plain(out, &c.children),
        }
    }
}
