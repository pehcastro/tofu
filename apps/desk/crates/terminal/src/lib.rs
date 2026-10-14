mod font;
mod keys;
mod links;
mod mouse;
mod paint;
mod palette;
mod pty;
mod scrollbar;
pub mod shells;

use crate::keys::{KeyInput, key_input};
use crate::links::Link;
use crate::mouse::Cell;
use crate::paint::{Frame, Look};
pub use crate::palette::Palette;
use crate::pty::{Output, Pty};
use crate::scrollbar::Scrollback;
use alacritty_terminal::event::{Event, EventListener, WindowSize};
use alacritty_terminal::grid::{Dimensions, Scroll};
use alacritty_terminal::index::{Column, Line, Point as GridPoint};
use alacritty_terminal::selection::{Selection, SelectionType};
use alacritty_terminal::term::color::COUNT;
use alacritty_terminal::term::test::TermSize;
use alacritty_terminal::term::{Config, Term, TermMode, point_to_viewport};
use alacritty_terminal::vte::ansi::Processor;
use desk_core::limits::{TERMINAL_CURSOR_BLINK, TERMINAL_SCROLLBACK_LINES};
use flume::Receiver;
use gpui::{
    App, Bounds, ClipboardItem, Context, DispatchPhase, ElementInputHandler, EntityInputHandler,
    EventEmitter, FocusHandle, Focusable, IntoElement, KeyDownEvent, ModifiersChangedEvent,
    MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, PaintQuad, Pixels, Point, Render,
    Result, ScrollWheelEvent, SharedString, Size, Task, UTF16Selection, Window, canvas, div,
    prelude::*, px, size,
};
use std::cell::RefCell;
use std::ops::Range;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::time::{Duration, Instant};

const START_SETTLE: Duration = Duration::from_millis(150);
const INITIAL_COLS: u16 = 80;
const INITIAL_ROWS: u16 = 24;
const MIN_COLS: f32 = 2.;
const MIN_ROWS: f32 = 1.;
const GUTTER: f32 = 8.;
const DRAG_THRESHOLD: f32 = 2.;
const FOCUS_IN: &[u8] = b"\x1b[I";
const FOCUS_OUT: &[u8] = b"\x1b[O";

#[derive(Clone, Default)]
struct Events(Rc<RefCell<Vec<Event>>>);

impl EventListener for Events {
    fn send_event(&self, event: Event) {
        self.0.borrow_mut().push(event);
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TerminalEvent {
    Started,
    Exited(Option<u32>),
    Retitled,
    Bell,
}

pub struct Terminal {
    term: Term<Events>,
    parser: Processor,
    events: Events,
    pty: Option<Pty>,
    shell: PathBuf,
    cwd: PathBuf,
    title: Option<SharedString>,
    palette: Palette,
    grid: (u16, u16),
    started: bool,
    cell: Size<Pixels>,
    bounds: Bounds<Pixels>,
    drawn: Option<Rc<Frame>>,
    frame_owed: bool,
    focused: bool,
    cursor_lit: bool,
    pressed_at: Option<Point<Pixels>>,
    grabbed: Option<Pixels>,
    hovered: Option<Cell>,
    pointer: Option<Point<Pixels>>,
    control: bool,
    link: Option<Link>,
    font: u8,
    scrolled: f32,
    last_output: Option<Instant>,
    sync_at: Option<Instant>,
    focus: FocusHandle,
    _pump: Option<Task<()>>,
    _settle: Option<Task<Result<()>>>,
    _sync: Option<Task<Result<()>>>,
    _blink: Option<Task<Result<()>>>,
}

impl EventEmitter<TerminalEvent> for Terminal {}

fn blank_term(grid: (u16, u16), events: Events) -> Term<Events> {
    let config = Config {
        scrolling_history: TERMINAL_SCROLLBACK_LINES,
        ..Config::default()
    };
    Term::new(config, &TermSize::new(grid.0.into(), grid.1.into()), events)
}

async fn next_batch(output: &Receiver<Output>) -> Option<Vec<Output>> {
    let mut batch = vec![output.recv_async().await.ok()?];
    batch.extend(output.drain());
    Some(batch)
}

impl Terminal {
    pub fn new(cwd: &Path, cx: &mut Context<Self>) -> Self {
        let events = Events::default();
        let grid = (INITIAL_COLS, INITIAL_ROWS);
        Terminal {
            term: blank_term(grid, events.clone()),
            parser: Processor::new(),
            events,
            pty: None,
            shell: shells::current(),
            cwd: cwd.to_path_buf(),
            title: None,
            palette: Palette::default(),
            grid,
            started: false,
            cell: size(px(1.), px(1.)),
            bounds: Bounds::default(),
            drawn: None,
            frame_owed: false,
            focused: false,
            cursor_lit: true,
            pressed_at: None,
            grabbed: None,
            hovered: None,
            pointer: None,
            control: false,
            link: None,
            font: font::saved(),
            scrolled: 0.,
            last_output: None,
            sync_at: None,
            focus: cx.focus_handle(),
            _pump: None,
            _settle: None,
            _sync: None,
            _blink: None,
        }
    }

    pub fn set_palette(&mut self, palette: Palette, cx: &mut Context<Self>) {
        if self.palette != palette {
            self.palette = palette;
            self.redraw(cx);
        }
    }

    pub fn pid(&self) -> Option<u32> {
        self.pty.as_ref().and_then(Pty::pid)
    }

    pub fn shell(&self) -> &Path {
        &self.shell
    }

    pub fn title(&self) -> Option<SharedString> {
        self.title.clone()
    }

    pub fn last_output(&self) -> Option<Instant> {
        self.last_output
    }

    pub fn restart(&mut self, cx: &mut Context<Self>) {
        self.term = blank_term(self.grid, self.events.clone());
        self.parser = Processor::new();
        self.scrolled = 0.;
        self.sync_at = None;
        self._sync = None;
        self.retitle(None, cx);
        self.start(cx);
        self.redraw(cx);
    }

    fn redraw(&mut self, cx: &mut Context<Self>) {
        self.drawn = None;
        cx.notify();
    }

    fn start(&mut self, cx: &mut Context<Self>) {
        self.started = true;
        let (cols, rows) = self.grid;
        match Pty::spawn(&self.shell, &self.cwd, cols, rows) {
            Ok((pty, output)) => {
                eprintln!("terminal: pty opened at {cols}x{rows}");
                self.pty = Some(pty);
                cx.emit(TerminalEvent::Started);
                self._pump = Some(cx.spawn(async move |this, cx| {
                    while let Some(batch) = next_batch(&output).await {
                        if this
                            .update(cx, |terminal, cx| terminal.receive(batch, cx))
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
                self.feed(format!("{error}\r\n").as_bytes(), cx);
            }
        }
    }

    fn receive(&mut self, batch: Vec<Output>, cx: &mut Context<Self>) {
        self.end_sync_when_due(cx);
        for output in batch {
            match output {
                Output::Bytes(bytes) => self.parser.advance(&mut self.term, &bytes),
                Output::Exited(code) => {
                    self.pty = None;
                    let shown = code.map_or_else(|| "unknown".to_owned(), |code| code.to_string());
                    self.feed(
                        format!(
                            "\r\n[shell exited with code {shown}, press Enter for a new shell]"
                        )
                        .as_bytes(),
                        cx,
                    );
                    cx.emit(TerminalEvent::Exited(code));
                }
            }
        }
        self.answer(cx);
        self.watch_sync(cx);
        self.last_output = Some(Instant::now());
        self.drawn = None;
        if !self.frame_owed {
            self.frame_owed = true;
            cx.notify();
        }
    }

    fn watch_sync(&mut self, cx: &mut Context<Self>) {
        let due = self.parser.sync_timeout().sync_timeout();
        if due == self.sync_at {
            return;
        }
        self.sync_at = due;
        self._sync = due.map(|due| {
            cx.spawn(async move |this, cx| {
                let wait = due.saturating_duration_since(Instant::now());
                cx.background_executor().timer(wait).await;
                this.update(cx, |terminal, cx| {
                    terminal.sync_at = None;
                    terminal.end_sync_when_due(cx);
                    terminal.watch_sync(cx);
                })
            })
        });
    }

    fn end_sync_when_due(&mut self, cx: &mut Context<Self>) {
        let now = Instant::now();
        if self
            .parser
            .sync_timeout()
            .sync_timeout()
            .is_some_and(|due| due <= now)
        {
            self.parser.stop_sync(&mut self.term);
            self.answer(cx);
            self.redraw(cx);
        }
    }

    fn feed(&mut self, bytes: &[u8], cx: &mut Context<Self>) {
        self.parser.advance(&mut self.term, bytes);
        self.answer(cx);
    }

    fn answer(&mut self, cx: &mut Context<Self>) {
        let events = std::mem::take(&mut *self.events.0.borrow_mut());
        let mut replies = Vec::new();
        for event in events {
            match event {
                Event::PtyWrite(text) => replies.extend_from_slice(text.as_bytes()),
                Event::ColorRequest(index, format) => {
                    let set = (index < COUNT).then(|| self.term.colors()[index]).flatten();
                    let color = set.unwrap_or_else(|| self.palette.rgb(index));
                    replies.extend_from_slice(format(color).as_bytes());
                }
                Event::TextAreaSizeRequest(format) => {
                    replies.extend_from_slice(format(self.window_size()).as_bytes());
                }
                Event::ClipboardStore(_, text) => {
                    cx.write_to_clipboard(ClipboardItem::new_string(text));
                }
                Event::ClipboardLoad(..) => {
                    eprintln!("terminal: a program asked to read the clipboard, refused");
                }
                Event::Title(title) => self.retitle(Some(title), cx),
                Event::ResetTitle => self.retitle(None, cx),
                Event::Bell => cx.emit(TerminalEvent::Bell),
                Event::MouseCursorDirty
                | Event::CursorBlinkingChange
                | Event::Wakeup
                | Event::Exit
                | Event::ChildExit(_) => {}
            }
        }
        if !replies.is_empty() {
            self.send(replies);
        }
    }

    fn retitle(&mut self, title: Option<String>, cx: &mut Context<Self>) {
        let shell_name = self.shell.file_name();
        let title = title
            .filter(|title| !title.trim().is_empty())
            .filter(|title| {
                let named = Path::new(title.trim()).file_name();
                !named
                    .zip(shell_name)
                    .is_some_and(|(named, shell)| named.eq_ignore_ascii_case(shell))
            })
            .map(SharedString::from);
        if self.title != title {
            self.title = title;
            cx.emit(TerminalEvent::Retitled);
        }
    }

    fn window_size(&self) -> WindowSize {
        WindowSize {
            num_lines: self.grid.1,
            num_cols: self.grid.0,
            cell_width: f32::from(self.cell.width).round() as u16,
            cell_height: f32::from(self.cell.height).round() as u16,
        }
    }

    fn send(&mut self, bytes: Vec<u8>) {
        if let Some(pty) = &self.pty
            && !pty.send(bytes)
        {
            self.pty = None;
        }
    }

    fn frame(
        &mut self,
        bounds: Bounds<Pixels>,
        cell: Size<Pixels>,
        cx: &mut Context<Self>,
    ) -> Rc<Frame> {
        let cols = (bounds.size.width / cell.width).floor().max(MIN_COLS) as u16;
        let rows = (bounds.size.height / cell.height).floor().max(MIN_ROWS) as u16;
        self.bounds = bounds;
        self.cell = cell;
        self.frame_owed = false;
        let resized = self.grid != (cols, rows);
        if resized {
            self.grid = (cols, rows);
            self.drawn = None;
            self.term.resize(TermSize::new(cols.into(), rows.into()));
            if let Some(pty) = &self.pty {
                match pty.resize(cols, rows) {
                    Ok(()) => eprintln!("terminal: grid and pty resized to {cols}x{rows}"),
                    Err(error) => eprintln!("{error}"),
                }
            }
        }
        if !self.started && (resized || self._settle.is_none()) {
            self._settle = Some(cx.spawn(async move |this, cx| {
                cx.background_executor().timer(START_SETTLE).await;
                this.update(cx, |terminal, cx| terminal.start(cx))
            }));
        }
        if let Some(frame) = &self.drawn {
            return frame.clone();
        }
        let offset = self.term.grid().display_offset();
        let link = self.link.as_ref().and_then(|link| {
            point_to_viewport(offset, GridPoint::new(link.line, Column(0)))
                .map(|at| (at.line, link.columns.clone()))
        });
        let frame = Rc::new(paint::frame(
            self.term.renderable_content(),
            Look {
                palette: &self.palette,
                focused: self.focused,
                cursor_lit: self.cursor_lit,
                font: px(f32::from(self.font)),
                link,
            },
        ));
        self.drawn = Some(frame.clone());
        frame
    }

    fn scrollback(&self) -> Scrollback {
        let grid = self.term.grid();
        Scrollback {
            offset: grid.display_offset(),
            history: grid.history_size(),
            rows: grid.screen_lines(),
        }
    }

    fn inside_gutter(&self) -> Bounds<Pixels> {
        Bounds::new(
            self.bounds.origin,
            size(self.bounds.size.width - px(GUTTER), self.bounds.size.height),
        )
    }

    fn thumb(&self) -> Option<PaintQuad> {
        scrollbar::thumb(self.inside_gutter(), px(GUTTER), self.scrollback())
            .map(|thumb| scrollbar::quad(thumb, self.grabbed.is_some(), self.palette.foreground))
    }

    fn selected(&self) -> bool {
        self.term
            .selection
            .as_ref()
            .is_some_and(|selection| !selection.is_empty())
    }

    fn copy(&mut self, cx: &mut Context<Self>) {
        let text = self.term.selection_to_string();
        if let Some(text) = text.filter(|text| !text.is_empty()) {
            cx.write_to_clipboard(ClipboardItem::new_string(text));
        }
        self.term.selection = None;
    }

    fn paste(&mut self, cx: &mut Context<Self>) {
        let Some(text) = cx.read_from_clipboard().and_then(|item| item.text()) else {
            return;
        };
        let text = text.replace("\r\n", "\r").replace('\n', "\r");
        let text = if self.term.mode().contains(TermMode::BRACKETED_PASTE) {
            format!("\x1b[200~{}\x1b[201~", text.replace('\x1b', ""))
        } else {
            text
        };
        self.type_bytes(text.into_bytes(), cx);
    }

    fn key_down(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        if self.started && self.pty.is_none() && event.keystroke.key == "enter" {
            self.restart(cx);
            return cx.stop_propagation();
        }
        let held = event.keystroke.modifiers;
        if held.control && !held.alt && self.zoom(&event.keystroke.key, cx) {
            return cx.stop_propagation();
        }
        match key_input(&event.keystroke, *self.term.mode(), self.selected()) {
            Some(KeyInput::Scroll(scroll)) => self.scroll(scroll, cx),
            Some(KeyInput::Send(bytes)) => self.type_bytes(bytes, cx),
            Some(KeyInput::Paste) => self.paste(cx),
            Some(KeyInput::Copy) => self.copy(cx),
            None => return,
        }
        cx.stop_propagation();
        self.redraw(cx);
    }

    fn zoom(&mut self, key: &str, cx: &mut Context<Self>) -> bool {
        let font = match key {
            "=" | "+" => self.font.saturating_add(1),
            "-" => self.font.saturating_sub(1),
            "0" => font::DEFAULT,
            _ => return false,
        };
        let font = font::clamped(font);
        if font != self.font {
            self.font = font;
            match font::save(font) {
                Ok(()) => eprintln!("desk: terminal font {font}"),
                Err(error) => eprintln!("terminal: font {font} not kept: {error}"),
            }
            self.redraw(cx);
        }
        true
    }

    fn point_link(&mut self, cx: &mut Context<Self>) {
        let link = self
            .pointer
            .filter(|_| self.control)
            .and_then(|at| links::link_at(&self.term, self.grid_point(self.cell_at(at))));
        if link != self.link {
            self.link = link;
            self.redraw(cx);
        }
    }

    fn modifiers_changed(
        &mut self,
        event: &ModifiersChangedEvent,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.control = event.modifiers.control;
        self.point_link(cx);
    }

    fn type_bytes(&mut self, bytes: Vec<u8>, cx: &mut Context<Self>) {
        self.term.selection = None;
        self.term.scroll_display(Scroll::Bottom);
        self.send(bytes);
        if self.focused {
            self.blink(cx);
        }
    }

    fn blink(&mut self, cx: &mut Context<Self>) {
        self.cursor_lit = true;
        self._blink = Some(cx.spawn(async move |this, cx| {
            loop {
                cx.background_executor().timer(TERMINAL_CURSOR_BLINK).await;
                this.update(cx, |terminal, cx| {
                    terminal.cursor_lit = !terminal.cursor_lit;
                    terminal.redraw(cx);
                })?;
            }
        }));
    }

    fn scroll(&mut self, scroll: Scroll, cx: &mut Context<Self>) {
        self.term.scroll_display(scroll);
        self.answer(cx);
    }

    fn cell_at(&self, position: Point<Pixels>) -> Cell {
        mouse::cell_at(position - self.bounds.origin, self.cell, self.grid)
    }

    fn grid_point(&self, at: Cell) -> GridPoint {
        let line = i32::try_from(at.line).unwrap_or(i32::MAX);
        let offset = i32::try_from(self.term.grid().display_offset()).unwrap_or(i32::MAX);
        let last = self.term.bottommost_line();
        let line = Line(line.saturating_sub(offset)).min(last);
        GridPoint::new(line, Column(at.column))
    }

    fn mouse_down(&mut self, event: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        window.focus(&self.focus, cx);
        let mode = *self.term.mode();
        let at = self.cell_at(event.position);
        if event.button == MouseButton::Left
            && event.modifiers.control
            && let Some(link) = links::link_at(&self.term, self.grid_point(at))
        {
            eprintln!("desk: terminal opened {}", link.url);
            cx.open_url(&link.url);
            return;
        }
        if mouse::reporting(mode, event.modifiers.shift) {
            if let Some(report) =
                mouse::button_report(at, event.button, event.modifiers, true, mode)
            {
                self.send(report);
            }
            return;
        }
        match event.button {
            MouseButton::Left
                if self.scrollback().history > 0
                    && scrollbar::rail(self.inside_gutter(), px(GUTTER))
                        .contains(&event.position) =>
            {
                self.press_rail(event.position.y, cx)
            }
            MouseButton::Left => self.press_text(event, at),
            MouseButton::Right if self.selected() => self.copy(cx),
            MouseButton::Right | MouseButton::Middle => self.paste(cx),
            MouseButton::Navigate(_) => return,
        }
        self.redraw(cx);
    }

    fn press_rail(&mut self, y: Pixels, cx: &mut Context<Self>) {
        let Some(thumb) = scrollbar::thumb(self.inside_gutter(), px(GUTTER), self.scrollback())
        else {
            return;
        };
        if y < thumb.top() {
            self.scroll(Scroll::PageUp, cx);
        } else if y > thumb.bottom() {
            self.scroll(Scroll::PageDown, cx);
        } else {
            self.grabbed = Some(y - thumb.top());
        }
    }

    fn press_text(&mut self, event: &MouseDownEvent, at: Cell) {
        self.pressed_at = Some(event.position);
        let kind = match event.click_count {
            0 | 1 => SelectionType::Simple,
            2 => SelectionType::Semantic,
            _ => SelectionType::Lines,
        };
        let point = self.grid_point(at);
        match (kind, event.modifiers.shift, self.term.selection.as_mut()) {
            (SelectionType::Simple, true, Some(selection)) => selection.update(point, at.side),
            _ => self.term.selection = Some(Selection::new(kind, point, at.side)),
        }
    }

    fn mouse_move(&mut self, event: &MouseMoveEvent, _: &mut Window, cx: &mut Context<Self>) {
        self.pointer = Some(event.position);
        self.control = event.modifiers.control;
        self.point_link(cx);
        let mode = *self.term.mode();
        let at = self.cell_at(event.position);
        if mouse::reporting(mode, event.modifiers.shift)
            && self.hovered.replace(at) != Some(at)
            && let Some(report) =
                mouse::move_report(at, event.pressed_button, event.modifiers, mode)
        {
            self.send(report);
        }
    }

    fn drag(&mut self, event: &MouseMoveEvent, cx: &mut Context<Self>) {
        if let Some(grabbed) = self.grabbed {
            let back = self.scrollback();
            let target = scrollbar::offset_at(self.bounds, back, event.position.y - grabbed);
            let delta = i32::try_from(target)
                .unwrap_or(i32::MAX)
                .saturating_sub(i32::try_from(back.offset).unwrap_or(i32::MAX));
            self.scroll(Scroll::Delta(delta), cx);
            return self.redraw(cx);
        }
        if mouse::reporting(*self.term.mode(), event.modifiers.shift) {
            return;
        }
        let Some(pressed_at) = self.pressed_at else {
            return;
        };
        let moved = event.position - pressed_at;
        if moved.x.abs().max(moved.y.abs()) <= px(DRAG_THRESHOLD) && !self.selected() {
            return;
        }
        let y = event.position.y;
        let above = ((self.bounds.top() - y) / self.cell.height).ceil();
        let below = ((y - self.bounds.bottom()) / self.cell.height).ceil();
        if above > 0. {
            self.scroll(Scroll::Delta(above as i32), cx);
        } else if below > 0. {
            self.scroll(Scroll::Delta(-(below as i32)), cx);
        }
        let at = self.cell_at(event.position);
        let point = self.grid_point(at);
        if let Some(selection) = self.term.selection.as_mut() {
            selection.update(point, at.side);
            self.redraw(cx);
        }
    }

    fn mouse_up(&mut self, event: &MouseUpEvent, _: &mut Window, cx: &mut Context<Self>) {
        self.pressed_at = None;
        if self.grabbed.take().is_some() {
            self.redraw(cx);
        }
        let mode = *self.term.mode();
        if mouse::reporting(mode, event.modifiers.shift)
            && let Some(report) = mouse::button_report(
                self.cell_at(event.position),
                event.button,
                event.modifiers,
                false,
                mode,
            )
        {
            self.send(report);
        }
    }

    fn scroll_wheel(&mut self, event: &ScrollWheelEvent, _: &mut Window, cx: &mut Context<Self>) {
        let height = self.cell.height;
        let lines = event.delta.pixel_delta(height).y / height + self.scrolled;
        let whole = lines.trunc();
        self.scrolled = lines - whole;
        let whole = whole as i32;
        if whole == 0 {
            return;
        }
        let mode = *self.term.mode();
        let shift = event.modifiers.shift;
        if mouse::reporting(mode, shift) {
            let at = self.cell_at(event.position);
            self.send(mouse::scroll_report(at, whole, event.modifiers, mode));
        } else if mode.contains(TermMode::ALT_SCREEN | TermMode::ALTERNATE_SCROLL) && !shift {
            self.send(mouse::alternate_scroll(whole));
        } else {
            self.scroll(Scroll::Delta(whole), cx);
            self.redraw(cx);
        }
    }

    fn track_focus(&mut self, focused: bool, cx: &mut Context<Self>) {
        if self.focused == focused {
            return;
        }
        self.focused = focused;
        self.drawn = None;
        if focused {
            self.blink(cx);
        } else {
            self._blink = None;
            self.cursor_lit = true;
        }
        if self.term.mode().contains(TermMode::FOCUS_IN_OUT) {
            self.send(if focused { FOCUS_IN } else { FOCUS_OUT }.to_vec());
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
        if !text.is_empty() && self.pty.is_some() {
            self.type_bytes(text.as_bytes().to_vec(), cx);
            self.redraw(cx);
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
        self.frame_owed = false;
        let focused = self.focus.is_focused(window);
        self.track_focus(focused, cx);
        let terminal = cx.entity();
        let typed = terminal.clone();
        let dragged = terminal.clone();
        let focus = self.focus.clone();
        let palette = self.palette;
        let font = px(f32::from(self.font));
        div()
            .size_full()
            .bg(palette.background)
            .when(self.link.is_some(), |div| div.cursor_pointer())
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::key_down))
            .on_modifiers_changed(cx.listener(Self::modifiers_changed))
            .on_mouse_down(MouseButton::Left, cx.listener(Self::mouse_down))
            .on_mouse_down(MouseButton::Middle, cx.listener(Self::mouse_down))
            .on_mouse_down(MouseButton::Right, cx.listener(Self::mouse_down))
            .on_mouse_move(cx.listener(Self::mouse_move))
            .on_mouse_up(MouseButton::Left, cx.listener(Self::mouse_up))
            .on_mouse_up(MouseButton::Middle, cx.listener(Self::mouse_up))
            .on_mouse_up(MouseButton::Right, cx.listener(Self::mouse_up))
            .on_mouse_up_out(MouseButton::Left, cx.listener(Self::mouse_up))
            .on_scroll_wheel(cx.listener(Self::scroll_wheel))
            .child(
                canvas(
                    move |bounds, window, cx| {
                        let cell = paint::cell_size(window, font);
                        let (frame, thumb) = terminal.update(cx, |terminal, cx| {
                            (terminal.frame(bounds, cell, cx), terminal.thumb())
                        });
                        paint::prepare(&frame, bounds, cell, focused, &palette, thumb, window)
                    },
                    move |bounds, painted, window, cx| {
                        window.handle_input(&focus, ElementInputHandler::new(bounds, typed), cx);
                        window.on_mouse_event(move |event: &MouseMoveEvent, phase, _, cx| {
                            if phase == DispatchPhase::Bubble && event.dragging() {
                                dragged.update(cx, |terminal, cx| terminal.drag(event, cx));
                            }
                        });
                        painted.paint(window, cx);
                    },
                )
                .size_full(),
            )
    }
}
