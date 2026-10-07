use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::chip::kbd;
use desk_ui::components::palette::{Palette, PaletteItem};
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

pub(super) struct PalettePage {
    palette: Entity<Palette>,
    readout: SharedString,
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
        let picked = cx.listener(|book, id: &SharedString, _, cx| {
            book.palette.readout = format!("on_pick({id})").into();
            cx.notify();
        });
        let closed = cx.listener(|book, _: &(), _, cx| {
            book.palette.readout = "on_close".into();
            cx.notify();
        });
        palette.update(cx, |palette, _| {
            palette.on_pick(picked);
            palette.on_close(move |window, cx| closed(&(), window, cx));
        });
        PalettePage {
            palette,
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
                    .child(kbd("Ctrl K", theme)),
            )
            .child(self.palette.clone())
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
