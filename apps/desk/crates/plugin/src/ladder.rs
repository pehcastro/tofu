pub mod since_v0_1_0 {
    wasmtime::component::bindgen!({ path: "../../wit/since_v0.1.0", world: "plugin" });
}

pub mod since_v0_2_0 {
    wasmtime::component::bindgen!({ path: "../../wit/since_v0.2.0", world: "plugin" });
}

pub mod since_v0_3_0 {
    wasmtime::component::bindgen!({ path: "../../wit/since_v0.3.0", world: "plugin" });
}

use since_v0_1_0::tofu::desk::types as old;
use since_v0_2_0::tofu::desk::types as new;
use since_v0_3_0::tofu::desk::types as next;

pub fn from_next(nodes: Vec<next::Node>) -> Vec<new::Node> {
    nodes
        .into_iter()
        .map(|node| new::Node {
            key: node.key,
            kind: next_kind(node.kind),
            children: node.children,
        })
        .collect()
}

pub fn to_next(event: &new::Event) -> next::Event {
    match event.clone() {
        new::Event::Click(key) => next::Event::Click(key),
        new::Event::Change(pair) => next::Event::Change(pair),
        new::Event::Submit(pair) => next::Event::Submit(pair),
        new::Event::Changed(path) => next::Event::Changed(path),
        new::Event::Saved(path) => next::Event::Saved(path),
    }
}

fn next_choice(choice: next::Choice) -> new::Choice {
    new::Choice {
        options: choice.options,
        selected: choice.selected,
    }
}

fn next_kind(kind: next::Kind) -> new::Kind {
    match kind {
        next::Kind::Shell => new::Kind::Shell,
        next::Kind::Inner => new::Kind::Inner,
        next::Kind::Tabs(c) => new::Kind::Tabs(next_choice(c)),
        next::Kind::Row => new::Kind::Row,
        next::Kind::Group(s) => new::Kind::Group(s),
        next::Kind::Avatar(s) => new::Kind::Avatar(s),
        next::Kind::Chip(s) => new::Kind::Chip(s),
        next::Kind::Badge(b) => new::Kind::Badge(new::Badge {
            label: b.label,
            tone: match b.tone {
                next::Tone::Neutral => new::Tone::Neutral,
                next::Tone::Accent => new::Tone::Accent,
                next::Tone::Success => new::Tone::Success,
                next::Tone::Warning => new::Tone::Warning,
                next::Tone::Danger => new::Tone::Danger,
            },
        }),
        next::Kind::Button(s) => new::Kind::Button(s),
        next::Kind::Segmented(c) => new::Kind::Segmented(next_choice(c)),
        next::Kind::Switch(t) => new::Kind::Switch(new::Toggle {
            label: t.label,
            on: t.on,
        }),
        next::Kind::Input(s) => new::Kind::Input(s),
        next::Kind::Table(t) => new::Kind::Table(new::Table {
            columns: t.columns,
            row_count: t.row_count,
        }),
        next::Kind::VirtualList(n) => new::Kind::VirtualList(n),
        next::Kind::Diff(s) => new::Kind::Diff(s),
        next::Kind::Term(s) => new::Kind::Term(s),
        next::Kind::FeedItem(s) => new::Kind::FeedItem(s),
        next::Kind::Sheet => new::Kind::Sheet,
        next::Kind::Popover => new::Kind::Popover,
        next::Kind::Text(s) => new::Kind::Text(s),
        next::Kind::Stack => new::Kind::Stack,
        next::Kind::Grid(n) => new::Kind::Grid(n),
        next::Kind::Editor(e) => new::Kind::Editor(new::Editor {
            path: e.path,
            wrap: e.wrap,
            syntax: e.syntax,
        }),
        next::Kind::Markdown(s) => new::Kind::Markdown(s),
    }
}

pub fn tree(nodes: Vec<old::Node>) -> Vec<new::Node> {
    nodes
        .into_iter()
        .map(|node| new::Node {
            key: node.key,
            kind: kind(node.kind),
            children: node.children,
        })
        .collect()
}

pub fn event(event: &new::Event) -> Option<old::Event> {
    match event {
        new::Event::Click(key) => Some(old::Event::Click(key.clone())),
        new::Event::Change(pair) => Some(old::Event::Change(pair.clone())),
        new::Event::Submit(pair) => Some(old::Event::Submit(pair.clone())),
        new::Event::Changed(_) | new::Event::Saved(_) => None,
    }
}

fn choice(choice: old::Choice) -> new::Choice {
    new::Choice {
        options: choice.options,
        selected: choice.selected,
    }
}

fn kind(kind: old::Kind) -> new::Kind {
    match kind {
        old::Kind::Shell => new::Kind::Shell,
        old::Kind::Inner => new::Kind::Inner,
        old::Kind::Tabs(c) => new::Kind::Tabs(choice(c)),
        old::Kind::Row => new::Kind::Row,
        old::Kind::Group(s) => new::Kind::Group(s),
        old::Kind::Avatar(s) => new::Kind::Avatar(s),
        old::Kind::Chip(s) => new::Kind::Chip(s),
        old::Kind::Badge(b) => new::Kind::Badge(new::Badge {
            label: b.label,
            tone: match b.tone {
                old::Tone::Neutral => new::Tone::Neutral,
                old::Tone::Accent => new::Tone::Accent,
                old::Tone::Success => new::Tone::Success,
                old::Tone::Warning => new::Tone::Warning,
                old::Tone::Danger => new::Tone::Danger,
            },
        }),
        old::Kind::Button(s) => new::Kind::Button(s),
        old::Kind::Segmented(c) => new::Kind::Segmented(choice(c)),
        old::Kind::Switch(t) => new::Kind::Switch(new::Toggle {
            label: t.label,
            on: t.on,
        }),
        old::Kind::Input(s) => new::Kind::Input(s),
        old::Kind::Table(t) => new::Kind::Table(new::Table {
            columns: t.columns,
            row_count: t.row_count,
        }),
        old::Kind::VirtualList(n) => new::Kind::VirtualList(n),
        old::Kind::Diff(s) => new::Kind::Diff(s),
        old::Kind::Term(s) => new::Kind::Term(s),
        old::Kind::FeedItem(s) => new::Kind::FeedItem(s),
        old::Kind::Sheet => new::Kind::Sheet,
        old::Kind::Popover => new::Kind::Popover,
        old::Kind::Text(s) => new::Kind::Text(s),
        old::Kind::Stack => new::Kind::Stack,
        old::Kind::Grid(n) => new::Kind::Grid(n),
    }
}
