use std::ops::Range;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::inner_card;
use desk_ui::components::chip::{kbd, mono};
use desk_ui::components::find::{FindBar, find_ranges, match_highlights};
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::size::{FONT_SMALL, LINE_BODY};
use desk_ui::theme::Theme;
use gpui::{
    ClickEvent, Context, Div, Entity, KeyDownEvent, ScrollHandle, SharedString, StyledText, Window,
    div, point, prelude::*, px,
};

use super::Book;
use super::kit::label;

const TRANSCRIPT: [&str; 30] = [
    "$ cargo test -p desk_book",
    "   Compiling desk_ui v0.1.0",
    "   Compiling desk_book v0.1.0",
    "warning: unused import: `Range`",
    "  --> crates/book/src/book/find.rs:3:5",
    "    Finished `test` profile in 14.2s",
    "     Running unittests src/main.rs",
    "running 12 tests",
    "test catalog::parses_every_slug ... ok",
    "test themes::applies_dark ... ok",
    "test themes::rejects_unknown ... ok",
    "test drive::reads_script ... ok",
    "test drive::refuses_bad_step ... FAILED",
    "test drive::shot_name ... ok",
    "failures:",
    "---- drive::refuses_bad_step stdout ----",
    "thread 'main' panicked at examples/drive.rs:142:9:",
    "expected an error for step [\"jump\"], got Ok",
    "stderr:",
    "  Error: unknown step \"jump\"",
    "note: run with RUST_BACKTRACE=1 for a backtrace",
    "failures:",
    "    drive::refuses_bad_step",
    "test result: FAILED. 11 passed; 1 failed; 0 ignored",
    "error: test failed, to rerun pass `-p desk_book --bin book`",
    "$ cargo test -p desk_book -- refuses",
    "running 1 test",
    "test drive::refuses_bad_step ... ok",
    "test result: ok. 1 passed; 0 failed",
    "$ echo $?",
];
const TRANSCRIPT_HEIGHT: f32 = 320.0;
const TRANSCRIPT_PAD: f32 = 12.0;

struct Hit {
    line: usize,
    range: Range<usize>,
}

pub(super) struct FindPage {
    bar: Entity<FindBar>,
    query: String,
    hits: Vec<Hit>,
    current: usize,
    event: SharedString,
    scroll: ScrollHandle,
}

impl FindPage {
    pub(super) fn new(window: &mut Window, cx: &mut Context<Book>) -> Self {
        let bar = FindBar::new(window, cx);
        let changed = cx.listener(|book, (query, current): &(String, usize), _, cx| {
            book.find.change(query, *current, cx);
            cx.notify();
        });
        let closed = cx.listener(|book, _: &(), _, cx| {
            book.find.hits.clear();
            book.find.event = "on_close".into();
            cx.notify();
        });
        bar.update(cx, |bar, _| {
            bar.on_change(move |query, current, window, cx| {
                changed(&(query.to_owned(), current), window, cx)
            });
            bar.on_close(move |window, cx| closed(&(), window, cx));
        });
        FindPage {
            bar,
            query: String::new(),
            hits: Vec::new(),
            current: 0,
            event: "nothing yet".into(),
            scroll: ScrollHandle::new(),
        }
    }

    fn change(&mut self, query: &str, current: usize, cx: &mut Context<Book>) {
        query.clone_into(&mut self.query);
        self.hits = TRANSCRIPT
            .iter()
            .enumerate()
            .flat_map(|(line, text)| {
                find_ranges(text, query)
                    .into_iter()
                    .map(move |range| Hit { line, range })
            })
            .collect();
        self.current = current;
        self.event = format!("on_change({query:?}, {current})").into();
        let total = self.hits.len();
        self.bar.update(cx, |bar, cx| bar.set_total(total, cx));
        self.reveal();
    }

    fn reveal(&self) {
        let Some(hit) = self.hits.get(self.current) else {
            return;
        };
        let top = px(TRANSCRIPT_PAD + hit.line as f32 * LINE_BODY);
        let bottom = top + px(LINE_BODY);
        let view = self.scroll.bounds().size.height;
        let offset = self.scroll.offset();
        let shown = -offset.y;
        if top >= shown && bottom <= shown + view {
            return;
        }
        let y = view / 2.0 - (top + bottom) / 2.0;
        let max = self.scroll.max_offset().y;
        self.scroll
            .set_offset(point(offset.x, y.clamp(-max, px(0.0))));
    }

    fn open(&mut self, window: &mut Window, cx: &mut Context<Book>) {
        self.event = "opened".into();
        self.bar.update(cx, |bar, cx| bar.open(window, cx));
    }

    fn readout(&self) -> String {
        let count = match (self.query.is_empty(), self.hits.get(self.current)) {
            (true, _) => "0 of 0".to_owned(),
            (false, None) => "no matches".to_owned(),
            (false, Some(hit)) => format!(
                "{} of {}, line {}",
                self.current + 1,
                self.hits.len(),
                hit.line + 1
            ),
        };
        format!("readout: query {:?} | {count} | {}", self.query, self.event)
    }

    pub(super) fn render(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let current = self.hits.get(self.current);
        let rows = TRANSCRIPT.iter().enumerate().map(|(line, text)| {
            let ranges: Vec<Range<usize>> = self
                .hits
                .iter()
                .filter(|hit| hit.line == line)
                .map(|hit| hit.range.clone())
                .collect();
            let here = current
                .filter(|hit| hit.line == line)
                .and_then(|hit| ranges.iter().position(|range| *range == hit.range));
            div().h(px(LINE_BODY)).whitespace_nowrap().child(
                StyledText::new(*text).with_highlights(match_highlights(&ranges, here, theme)),
            )
        });
        div()
            .flex()
            .flex_col()
            .items_start()
            .gap_3()
            .child(label(self.readout(), theme))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(
                        button("find-open", "Find", None, ButtonKind::Plain, theme).on_click(
                            cx.listener(|book, _: &ClickEvent, window, cx| {
                                book.find.open(window, cx)
                            }),
                        ),
                    )
                    .child(kbd("Ctrl F", theme)),
            )
            .child(
                div()
                    .relative()
                    .w_full()
                    .child(
                        inner_card(theme).child(
                            ScrollArea::new("find-transcript")
                                .max_h(TRANSCRIPT_HEIGHT)
                                .track(&self.scroll)
                                .child(
                                    div()
                                        .p(px(TRANSCRIPT_PAD))
                                        .font_family(mono(theme))
                                        .text_size(px(FONT_SMALL))
                                        .line_height(px(LINE_BODY))
                                        .children(rows),
                                ),
                        ),
                    )
                    .child(self.bar.clone()),
            )
    }

    pub(super) fn key(
        &mut self,
        event: &KeyDownEvent,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> bool {
        let keys = &event.keystroke;
        let modifiers = keys.modifiers;
        let ctrl_f = keys.key == "f"
            && modifiers.control
            && !modifiers.alt
            && !modifiers.shift
            && !modifiers.platform;
        if ctrl_f {
            self.open(window, cx);
        }
        ctrl_f
    }
}
