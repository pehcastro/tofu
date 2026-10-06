mod keys;
mod paint;
mod palette;
mod pty;

use crate::keys::{KeyInput, key_input};
use crate::paint::{Cursor, Frame, Glyph};
use crate::pty::{Output, Pty};
use alacritty_terminal::event::{Event, EventListener};
use alacritty_terminal::grid::Scroll;
use alacritty_terminal::term::test::TermSize;
use alacritty_terminal::term::{Config, Term, TermMode, point_to_viewport};
use alacritty_terminal::vte::ansi::Processor;
use desk_core::limits::TERMINAL_SCROLLBACK_LINES;
use futures::StreamExt;
use gpui::{
    App, Context, FocusHandle, Focusable, IntoElement, KeyDownEvent, Pixels, Render,
    ScrollWheelEvent, Task, Window, canvas, div, prelude::*, px,
};
use std::cell::RefCell;
use std::path::Path;
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

pub struct Terminal {
    term: Term<Replies>,
    parser: Processor,
    replies: Replies,
    pty: Option<Pty>,
    grid: (u16, u16),
    line_height: Pixels,
    scrolled: f32,
    focus: FocusHandle,
    _pump: Option<Task<()>>,
}

impl Terminal {
    pub fn new(cwd: &Path, cx: &mut Context<Self>) -> Self {
        let replies = Replies::default();
        let config = Config {
            scrolling_history: TERMINAL_SCROLLBACK_LINES,
            ..Config::default()
        };
        let size = TermSize::new(INITIAL_COLS.into(), INITIAL_ROWS.into());
        let mut terminal = Terminal {
            term: Term::new(config, &size, replies.clone()),
            parser: Processor::new(),
            replies,
            pty: None,
            grid: (INITIAL_COLS, INITIAL_ROWS),
            line_height: px(1.),
            scrolled: 0.,
            focus: cx.focus_handle(),
            _pump: None,
        };
        match Pty::spawn(cwd, INITIAL_COLS, INITIAL_ROWS) {
            Ok((pty, mut output)) => {
                terminal.pty = Some(pty);
                terminal._pump = Some(cx.spawn(async move |this, cx| {
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
            Err(error) => terminal.feed(format!("{error}\r\n").as_bytes()),
        }
        terminal
    }

    fn receive(&mut self, output: Output, cx: &mut Context<Self>) {
        match output {
            Output::Bytes(bytes) => self.feed(&bytes),
            Output::Exited(code) => {
                self.pty = None;
                let code = code.map_or_else(|| "unknown".to_owned(), |code| code.to_string());
                self.feed(format!("\r\n[shell exited with code {code}]\r\n").as_bytes());
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
                let (fg, bg) = palette::cell_colors(indexed.cell);
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
            Some(KeyInput::Send(bytes)) => {
                self.term.scroll_display(Scroll::Bottom);
                self.send(bytes);
            }
            None => return,
        }
        cx.stop_propagation();
        cx.notify();
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

impl Render for Terminal {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let terminal = cx.entity();
        let focused = self.focus.is_focused(window);
        div()
            .size_full()
            .p(px(PADDING))
            .bg(palette::background())
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
                        paint::prepare(&frame, bounds.origin, cell, focused, window)
                    },
                    |_, painted, window, cx| painted.paint(window, cx),
                )
                .size_full(),
            )
    }
}
