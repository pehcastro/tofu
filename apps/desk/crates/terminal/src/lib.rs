mod keys;
mod paint;
mod palette;
mod pty;

use crate::keys::{KeyInput, key_input};
use crate::paint::{Cursor, Frame, Glyph};
pub use crate::palette::Palette;
use crate::pty::{Output, Pty};
use alacritty_terminal::event::{Event, EventListener};
use alacritty_terminal::grid::Scroll;
use alacritty_terminal::term::test::TermSize;
use alacritty_terminal::term::{Config, Term, TermMode, point_to_viewport};
use alacritty_terminal::vte::ansi::Processor;
use desk_core::limits::TERMINAL_SCROLLBACK_LINES;
use futures::StreamExt;
use gpui::{
    App, Bounds, Context, ElementInputHandler, EntityInputHandler, EventEmitter, FocusHandle,
    Focusable, IntoElement, KeyDownEvent, Pixels, Point, Render, ScrollWheelEvent, Task,
    UTF16Selection, Window, canvas, div, prelude::*, px,
};
use std::cell::RefCell;
use std::ops::Range;
use std::path::{Path, PathBuf};
use std::rc::Rc;

const INITIAL_COLS: u16 = 80;
const INITIAL_ROWS: u16 = 24;
const MIN_COLS: f32 = 2.;
const MIN_ROWS: f32 = 1.;
const PADDING: f32 = 8.;

#[derive(Clone, Default)]
struct Replies(Rc<RefCell<Vec<u8>>>);

impl EventListener for Replies {
    fn send_event(&self, event: Event) {
        if let Event::PtyWrite(text) = event {
            self.0.borrow_mut().extend_from_slice(text.as_bytes());
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TerminalEvent {
    Exited(Option<u32>),
}

pub struct Terminal {
    term: Term<Replies>,
    parser: Processor,
    replies: Replies,
    pty: Option<Pty>,
    cwd: PathBuf,
    palette: Palette,
    grid: (u16, u16),
    line_height: Pixels,
    scrolled: f32,
    focus: FocusHandle,
    _pump: Option<Task<()>>,
}

impl EventEmitter<TerminalEvent> for Terminal {}

fn blank_term(grid: (u16, u16), replies: Replies) -> Term<Replies> {
    let config = Config {
        scrolling_history: TERMINAL_SCROLLBACK_LINES,
        ..Config::default()
    };
    Term::new(
        config,
        &TermSize::new(grid.0.into(), grid.1.into()),
        replies,
    )
}

impl Terminal {
    pub fn new(cwd: &Path, cx: &mut Context<Self>) -> Self {
        let replies = Replies::default();
        let grid = (INITIAL_COLS, INITIAL_ROWS);
        let mut terminal = Terminal {
            term: blank_term(grid, replies.clone()),
            parser: Processor::new(),
            replies,
            pty: None,
            cwd: cwd.to_path_buf(),
            palette: Palette::default(),
            grid,
            line_height: px(1.),
            scrolled: 0.,
            focus: cx.focus_handle(),
            _pump: None,
        };
        terminal.start(cx);
        terminal
    }

    pub fn set_palette(&mut self, palette: Palette, cx: &mut Context<Self>) {
        if self.palette != palette {
            self.palette = palette;
            cx.notify();
        }
    }

    pub fn pid(&self) -> Option<u32> {
        self.pty.as_ref().and_then(Pty::pid)
    }

    pub fn restart(&mut self, cx: &mut Context<Self>) {
        self.term = blank_term(self.grid, self.replies.clone());
        self.parser = Processor::new();
        self.scrolled = 0.;
        self.start(cx);
        cx.notify();
    }

    fn start(&mut self, cx: &mut Context<Self>) {
        match Pty::spawn(&self.cwd, self.grid.0, self.grid.1) {
            Ok((pty, mut output)) => {
                self.pty = Some(pty);
                self._pump = Some(cx.spawn(async move |this, cx| {
                    while let Some(output) = output.next().await {
                        if this
                            .update(cx, |terminal, cx| terminal.receive(output, cx))
                            .is_err()
                        {
                            return;
                        }
                    }
                }));
            }
            Err(error) => {
                self.pty = None;
                self._pump = None;
                self.feed(format!("{error}\r\n").as_bytes());
            }
        }
    }

    fn receive(&mut self, output: Output, cx: &mut Context<Self>) {
        match output {
            Output::Bytes(bytes) => self.feed(&bytes),
            Output::Exited(code) => {
                self.pty = None;
                let shown = code.map_or_else(|| "unknown".to_owned(), |code| code.to_string());
                self.feed(format!("\r\n[shell exited with code {shown}]\r\n").as_bytes());
                cx.emit(TerminalEvent::Exited(code));
            }
        }
        cx.notify();
    }

    fn feed(&mut self, bytes: &[u8]) {
        self.parser.advance(&mut self.term, bytes);
        let replies = std::mem::take(&mut *self.replies.0.borrow_mut());
        if !replies.is_empty() {
            self.send(replies);
        }
    }

    fn send(&mut self, bytes: Vec<u8>) {
        if let Some(pty) = &self.pty
            && !pty.send(bytes)
        {
            self.pty = None;
        }
    }

    fn frame(&mut self, cols: u16, rows: u16, line_height: Pixels) -> Frame {
        self.line_height = line_height;
        if self.grid != (cols, rows) {
            self.grid = (cols, rows);
            self.term.resize(TermSize::new(cols.into(), rows.into()));
            if let Some(pty) = &self.pty
                && let Err(error) = pty.resize(cols, rows)
            {
                eprintln!("{error}");
            }
        }
        let content = self.term.renderable_content();
        let mut lines: Vec<Vec<Glyph>> =
            (0..rows).map(|_| Vec::with_capacity(cols.into())).collect();
        for indexed in content.display_iter {
            let Some(at) = point_to_viewport(content.display_offset, indexed.point) else {
                continue;
            };
            if let Some(line) = lines.get_mut(at.line) {
                let (fg, bg) = self.palette.cell_colors(indexed.cell);
                line.push(Glyph {
                    ch: if indexed.cell.c == '\t' {
                        ' '
                    } else {
                        indexed.cell.c
                    },
                    fg,
                    bg,
                    flags: indexed.cell.flags,
                });
            }
        }
        let cursor = point_to_viewport(content.display_offset, content.cursor.point)
            .filter(|at| at.line < lines.len())
            .map(|at| Cursor {
                row: at.line,
                col: at.column.0,
                shape: content.cursor.shape,
            });
        Frame {
            rows: lines,
            cursor,
        }
    }

    fn key_down(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        let app_cursor = self.term.mode().contains(TermMode::APP_CURSOR);
        match key_input(&event.keystroke, app_cursor) {
            Some(KeyInput::Scroll(scroll)) => self.term.scroll_display(scroll),
            Some(KeyInput::Send(bytes)) => self.type_bytes(bytes),
            Some(KeyInput::Paste) => {
                let Some(text) = cx.read_from_clipboard().and_then(|item| item.text()) else {
                    return;
                };
                let text = text.replace("\r\n", "\r").replace('\n', "\r");
                let text = if self.term.mode().contains(TermMode::BRACKETED_PASTE) {
                    format!("\x1b[200~{}\x1b[201~", text.replace('\x1b', ""))
                } else {
                    text
                };
                self.type_bytes(text.into_bytes());
            }
            None => return,
        }
        cx.stop_propagation();
        cx.notify();
    }

    fn type_bytes(&mut self, bytes: Vec<u8>) {
        self.term.scroll_display(Scroll::Bottom);
        self.send(bytes);
    }

    fn scroll_wheel(&mut self, event: &ScrollWheelEvent, _: &mut Window, cx: &mut Context<Self>) {
        let lines = event.delta.pixel_delta(self.line_height).y / self.line_height + self.scrolled;
        let whole = lines.trunc();
        self.scrolled = lines - whole;
        if whole.abs() >= 1. {
            self.term.scroll_display(Scroll::Delta(whole as i32));
            cx.notify();
        }
    }
}

impl Focusable for Terminal {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

impl EntityInputHandler for Terminal {
    fn text_for_range(
        &mut self,
        _: Range<usize>,
        _: &mut Option<Range<usize>>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<String> {
        None
    }

    fn selected_text_range(
        &mut self,
        _: bool,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<UTF16Selection> {
        None
    }

    fn marked_text_range(&self, _: &mut Window, _: &mut Context<Self>) -> Option<Range<usize>> {
        None
    }

    fn unmark_text(&mut self, _: &mut Window, _: &mut Context<Self>) {}

    fn replace_text_in_range(
        &mut self,
        _: Option<Range<usize>>,
        text: &str,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !text.is_empty() {
            self.type_bytes(text.as_bytes().to_vec());
            cx.notify();
        }
    }

    fn replace_and_mark_text_in_range(
        &mut self,
        _: Option<Range<usize>>,
        _: &str,
        _: Option<Range<usize>>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) {
    }

    fn bounds_for_range(
        &mut self,
        _: Range<usize>,
        element_bounds: Bounds<Pixels>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<Bounds<Pixels>> {
        Some(element_bounds)
    }

    fn character_index_for_point(
        &mut self,
        _: Point<Pixels>,
        _: &mut Window,
        _: &mut Context<Self>,
    ) -> Option<usize> {
        None
    }
}

impl Render for Terminal {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let terminal = cx.entity();
        let typed = terminal.clone();
        let focus = self.focus.clone();
        let focused = self.focus.is_focused(window);
        let palette = self.palette;
        div()
            .size_full()
            .p(px(PADDING))
            .bg(palette.background)
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::key_down))
            .on_scroll_wheel(cx.listener(Self::scroll_wheel))
            .child(
                canvas(
                    move |bounds, window, cx| {
                        let cell = paint::cell_size(window);
                        let cols = (bounds.size.width / cell.width).floor().max(MIN_COLS) as u16;
                        let rows = (bounds.size.height / cell.height).floor().max(MIN_ROWS) as u16;
                        let frame = terminal
                            .update(cx, |terminal, _| terminal.frame(cols, rows, cell.height));
                        paint::prepare(&frame, bounds.origin, cell, focused, &palette, window)
                    },
                    move |bounds, painted, window, cx| {
                        window.handle_input(&focus, ElementInputHandler::new(bounds, typed), cx);
                        painted.paint(window, cx);
                    },
                )
                .size_full(),
            )
    }
}
