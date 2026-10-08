use std::iter;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::chip::kbd;
use desk_ui::components::palette::{
    Palette, PaletteDetail, PaletteEntry, PaletteItem, PaletteMeta,
};
use desk_ui::theme::Theme;
use gpui::{ClickEvent, Context, Div, Entity, KeyDownEvent, SharedString, Window, div, prelude::*};

use super::Book;
use super::kit::label;

const SAMPLES: [(&str, &str, &str, Option<&str>); 16] = [
    ("file.main", "main.rs", "Files", None),
    ("file.resolve", "resolve.go", "Files", None),
    ("file.readme", "README.md", "Files", None),
    ("session.resume", "Resume last session", "Sessions", None),
    ("session.notes", "notes refactor", "Sessions", None),
    ("screen.usage", "Usage", "Screens", None),
    ("screen.limits", "Limits", "Screens", None),
    (
        "screen.reopen",
        "Reopen closed tab",
        "Screens",
        Some("Ctrl Shift T"),
    ),
    ("setting.open", "Open settings", "Settings", Some("Ctrl ,")),
    ("setting.theme", "Theme", "Settings", None),
    (
        "setting.sidebar",
        "Toggle the sidebar",
        "Settings",
        Some("Ctrl B"),
    ),
    ("layout.reset", "Reset layout", "Layout", Some("Ctrl Alt R")),
    ("layout.preset", "Reset to preset", "Layout", None),
    (
        "layout.split",
        "Split the focused tile",
        "Layout",
        Some("Ctrl \\"),
    ),
    ("plugin.reload", "Reload plugins", "Plugins", None),
    ("plugin.browser", "Open the browser", "Plugins", None),
];

struct SampleProject {
    name: &'static str,
    path: &'static str,
    branch: Option<&'static str>,
    sessions: Option<(&'static str, &'static str)>,
    current: bool,
    missing: bool,
}

const PROJECTS: [SampleProject; 3] = [
    SampleProject {
        name: "hono-starter",
        path: "F:\\localhost\\ephem-sh\\tofu\\.local\\desk-app\\sandbox\\hono-starter",
        branch: Some("main"),
        sessions: Some(("16 sessions", "2h ago")),
        current: true,
        missing: false,
    },
    SampleProject {
        name: "tofu",
        path: "F:\\localhost\\ephem-sh\\tofu",
        branch: Some("develop"),
        sessions: Some(("1 session", "3d ago")),
        current: false,
        missing: false,
    },
    SampleProject {
        name: "shop",
        path: "C:\\code\\shop",
        branch: None,
        sessions: None,
        current: false,
        missing: true,
    },
];

pub(super) struct PalettePage {
    palette: Entity<Palette>,
    picker: Entity<Palette>,
    readout: SharedString,
}

fn picker_entries() -> Vec<PaletteEntry> {
    let projects = PROJECTS.iter().enumerate().map(|(at, project)| {
        let meta = project
            .missing
            .then(|| PaletteMeta::Text("missing".into()))
            .into_iter()
            .chain(
                project
                    .branch
                    .map(|branch| PaletteMeta::Branch(branch.into())),
            )
            .chain(project.sessions.into_iter().flat_map(|(count, last)| {
                [
                    PaletteMeta::Text(count.into()),
                    PaletteMeta::Text(last.into()),
                ]
            }))
            .collect();
        let item = PaletteItem {
            id: format!("project.recent.{at}").into(),
            label: project.name.into(),
            group: "Recent projects".into(),
            keys: None,
        };
        let detail = PaletteDetail {
            below: project.path.into(),
            meta,
            checked: project.current,
            dim: project.missing,
        };
        (item, Some(detail))
    });
    let open = PaletteItem {
        id: "project.open".into(),
        label: "Open a folder".into(),
        group: "Open".into(),
        keys: None,
    };
    projects.chain(iter::once((open, None))).collect()
}

impl PalettePage {
    pub(super) fn new(window: &mut Window, cx: &mut Context<Book>) -> Self {
        let items = SAMPLES
            .iter()
            .map(|(id, text, group, keys)| PaletteItem {
                id: (*id).into(),
                label: (*text).into(),
                group: (*group).into(),
                keys: keys.map(SharedString::from),
            })
            .collect();
        let palette = Palette::new(items, window, cx);
        let picker = Palette::detailed(picker_entries(), window, cx);
        for menu in [&palette, &picker] {
            let picked = cx.listener(|book, id: &SharedString, _, cx| {
                book.palette.readout = format!("on_pick({id})").into();
                cx.notify();
            });
            let closed = cx.listener(|book, _: &(), _, cx| {
                book.palette.readout = "on_close".into();
                cx.notify();
            });
            menu.update(cx, |menu, _| {
                menu.on_pick(picked);
                menu.on_close(move |window, cx| closed(&(), window, cx));
            });
        }
        PalettePage {
            palette,
            picker,
            readout: "nothing picked yet".into(),
        }
    }

    pub(super) fn render(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        div()
            .flex()
            .flex_col()
            .items_start()
            .gap_3()
            .child(label(format!("readout: {}", self.readout), theme))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(
                        button(
                            "palette-open",
                            "Open palette",
                            None,
                            ButtonKind::Plain,
                            theme,
                        )
                        .on_click(cx.listener(
                            |book, _: &ClickEvent, window, cx| {
                                book.palette
                                    .palette
                                    .update(cx, |palette, cx| palette.open(window, cx));
                            },
                        )),
                    )
                    .child(kbd("Ctrl K", theme))
                    .child(
                        button(
                            "picker-open",
                            "Open project picker",
                            None,
                            ButtonKind::Plain,
                            theme,
                        )
                        .on_click(cx.listener(
                            |book, _: &ClickEvent, window, cx| {
                                book.palette
                                    .picker
                                    .update(cx, |picker, cx| picker.open(window, cx));
                            },
                        )),
                    ),
            )
            .child(self.palette.clone())
            .child(self.picker.clone())
    }

    pub(super) fn key(
        &self,
        event: &KeyDownEvent,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> bool {
        let keys = &event.keystroke;
        let modifiers = keys.modifiers;
        let ctrl_k = keys.key == "k"
            && modifiers.control
            && !modifiers.alt
            && !modifiers.shift
            && !modifiers.platform;
        if ctrl_k {
            self.palette
                .update(cx, |palette, cx| palette.open(window, cx));
        }
        ctrl_k
    }
}
