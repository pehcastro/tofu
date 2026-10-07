use desk_ui::components::empty::{EmptyAction, EmptyHint, empty_state};
use desk_ui::components::glyph::Glyph;
use desk_ui::theme::Theme;
use gpui::{App, Context, Div, SharedString, Window, div, prelude::*, px};

use super::Book;
use super::kit::block;

const BARE_HEIGHT: f32 = 120.0;
const ACTIONS_HEIGHT: f32 = 200.0;
const TILING_HEIGHT: f32 = 460.0;

const CHAT_ACTIONS: [(&str, Option<Glyph>, Option<&str>); 3] = [
    ("New chat", Some(Glyph::Chat), Some("ctrl n")),
    ("Attach a file", Some(Glyph::Attach), None),
    ("Pick a model", None, Some("ctrl m")),
];

const TILING_ACTIONS: [(&str, Option<Glyph>, Option<&str>); 4] = [
    ("Open chat", Some(Glyph::Chat), Some("ctrl 1")),
    ("Open a file", Some(Glyph::File), Some("ctrl p")),
    ("New shell", Some(Glyph::Terminal), Some("ctrl `")),
    ("Agents", Some(Glyph::Agents), Some("ctrl 4")),
];

const TILING_HINTS: [(&str, &str); 8] = [
    ("ctrl k", "Command palette"),
    ("ctrl \\", "Split right"),
    ("ctrl -", "Split down"),
    ("ctrl w", "Close tile"),
    ("ctrl tab", "Next tile"),
    ("alt arrows", "Move focus"),
    ("ctrl shift t", "Reopen closed tile"),
    ("ctrl ,", "Settings"),
];

fn actions(rows: &[(&'static str, Option<Glyph>, Option<&'static str>)]) -> Vec<EmptyAction> {
    rows.iter()
        .map(|&(label, glyph, keys)| EmptyAction {
            label: label.into(),
            glyph,
            keys: keys.map(SharedString::from),
        })
        .collect()
}

pub(super) fn empty_page(theme: &Theme, _window: &mut Window, cx: &mut Context<Book>) -> Div {
    let chat = actions(&CHAT_ACTIONS);
    let tiling = actions(&TILING_ACTIONS);
    let hints: Vec<EmptyHint> = TILING_HINTS
        .iter()
        .map(|&(keys, label)| EmptyHint {
            keys: keys.into(),
            label: label.into(),
        })
        .collect();
    fn picked(
        labels: Vec<SharedString>,
        cx: &mut Context<Book>,
    ) -> impl Fn(&usize, &mut Window, &mut App) + 'static {
        cx.listener(move |this: &mut Book, at: &usize, _, cx| {
            let label = labels.get(*at).cloned().unwrap_or_default();
            this.tell(format!("Empty state: {label}"), cx);
        })
    }
    let labels = |rows: &[EmptyAction]| rows.iter().map(|row| row.label.clone()).collect();
    let chat_on = picked(labels(&chat), cx);
    let tiling_on = picked(labels(&tiling), cx);
    div()
        .flex()
        .flex_col()
        .gap_3()
        .child(block(
            "Title only",
            theme,
            div().h(px(BARE_HEIGHT)).child(empty_state(
                "empty-bare",
                "No files open",
                None,
                &[],
                &[],
                theme,
                |_, _, _| {},
            )),
        ))
        .child(block(
            "Title, line and three actions",
            theme,
            div().h(px(ACTIONS_HEIGHT)).child(empty_state(
                "empty-chat",
                "No messages yet",
                Some("Ask the lead for something, or bring a file in first.".into()),
                &chat,
                &[],
                theme,
                move |at, window, cx| chat_on(&at, window, cx),
            )),
        ))
        .child(block(
            "Empty workspace, four actions and eight hints",
            theme,
            div().h(px(TILING_HEIGHT)).child(empty_state(
                "empty-tiling",
                "This workspace is empty",
                Some("Open a tile to start. Every tile can split, move and close.".into()),
                &tiling,
                &hints,
                theme,
                move |at, window, cx| tiling_on(&at, window, cx),
            )),
        ))
}
