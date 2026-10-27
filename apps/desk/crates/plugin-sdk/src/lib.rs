use std::collections::VecDeque;
use std::ops::Range;

pub mod bindings {
    wit_bindgen::generate!({
        path: "../../wit/since_v0.3.0",
        world: "plugin",
        pub_export_macro: true,
        default_bindings_module: "desk_plugin_sdk::bindings",
    });
}

pub use bindings::exports::tofu::desk::commands::Command;
pub use bindings::tofu::desk::fs;
pub use bindings::tofu::desk::host::{Denied, notify};
pub use bindings::tofu::desk::types::{Badge, Choice, Editor, Kind, Node, Table, Toggle, Tone};
pub use bindings::{Event, Guest, Tree};

pub const NO_ARGS: &str = "{}";

pub trait Plugin: Default {
    fn view(&self) -> Part;

    fn rows(&self, _range: Range<u64>) -> Part {
        rows([])
    }

    fn handle(&mut self, event: Event);

    fn commands(&self) -> Vec<Command> {
        Vec::new()
    }

    fn run(&mut self, id: &str, _args: &str) -> Result<String, String> {
        Err(format!("this plugin has no command {id}"))
    }
}

pub fn command(id: impl Into<String>, title: impl Into<String>) -> Command {
    Command {
        id: id.into(),
        title: title.into(),
        args_schema: NO_ARGS.to_owned(),
    }
}

pub struct Part {
    key: String,
    kind: Kind,
    children: Vec<Part>,
}

pub fn part(key: impl Into<String>, kind: Kind, children: impl IntoIterator<Item = Part>) -> Part {
    Part {
        key: key.into(),
        kind,
        children: children.into_iter().collect(),
    }
}

pub fn stack(key: impl Into<String>, children: impl IntoIterator<Item = Part>) -> Part {
    part(key, Kind::Stack, children)
}

pub fn text(key: impl Into<String>, text: impl Into<String>) -> Part {
    part(key, Kind::Text(text.into()), [])
}

pub fn button(key: impl Into<String>, label: impl Into<String>) -> Part {
    part(key, Kind::Button(label.into()), [])
}

pub fn editor(key: impl Into<String>, path: impl Into<String>, wrap: bool, syntax: bool) -> Part {
    let path = path.into();
    part(key, Kind::Editor(Editor { path, wrap, syntax }), [])
}

pub fn markdown(key: impl Into<String>, text: impl Into<String>) -> Part {
    part(key, Kind::Markdown(text.into()), [])
}

pub fn table(
    key: impl Into<String>,
    columns: impl IntoIterator<Item = impl Into<String>>,
    row_count: u64,
) -> Part {
    let columns = columns.into_iter().map(Into::into).collect();
    part(key, Kind::Table(Table { columns, row_count }), [])
}

pub fn row(key: impl Into<String>, cells: impl IntoIterator<Item = Part>) -> Part {
    part(key, Kind::Row, cells)
}

pub fn rows(rows: impl IntoIterator<Item = Part>) -> Part {
    part("rows", Kind::Group("rows".into()), rows)
}

pub fn flatten(root: Part) -> Tree {
    let mut tree = Vec::new();
    let mut waiting = VecDeque::from([root]);
    while let Some(part) = waiting.pop_front() {
        let first = tree.len().saturating_add(waiting.len()).saturating_add(1);
        let children = (first..first.saturating_add(part.children.len()))
            .map(|at| u32::try_from(at).unwrap_or(u32::MAX))
            .collect();
        waiting.extend(part.children);
        tree.push(Node {
            key: part.key,
            kind: part.kind,
            children,
        });
    }
    tree
}

#[macro_export]
macro_rules! plugin {
    ($plugin:ty) => {
        struct DeskPluginExport;

        ::std::thread_local! {
            static DESK_PLUGIN: ::core::cell::RefCell<$plugin> =
                ::core::cell::RefCell::new(<$plugin as ::core::default::Default>::default());
        }

        impl $crate::Guest for DeskPluginExport {
            fn view() -> $crate::Tree {
                DESK_PLUGIN.with_borrow(|plugin| $crate::flatten($crate::Plugin::view(plugin)))
            }

            fn rows(start: u64, end: u64) -> $crate::Tree {
                DESK_PLUGIN.with_borrow(|plugin| {
                    $crate::flatten($crate::Plugin::rows(plugin, start..end))
                })
            }

            fn handle(event: $crate::Event) {
                DESK_PLUGIN.with_borrow_mut(|plugin| $crate::Plugin::handle(plugin, event))
            }
        }

        impl $crate::bindings::exports::tofu::desk::commands::Guest for DeskPluginExport {
            fn list() -> ::std::vec::Vec<$crate::Command> {
                DESK_PLUGIN.with_borrow(|plugin| $crate::Plugin::commands(plugin))
            }

            fn run(
                id: ::std::string::String,
                args: ::std::string::String,
            ) -> ::core::result::Result<::std::string::String, ::std::string::String> {
                DESK_PLUGIN.with_borrow_mut(|plugin| $crate::Plugin::run(plugin, &id, &args))
            }
        }

        $crate::bindings::export!(DeskPluginExport with_types_in $crate::bindings);
    };
}
