use std::cell::{Cell, RefCell};
use std::rc::Rc;
use std::time::Instant;

use desk_ui::component::control;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::glyph;
use desk_ui::components::tabs::{Tab, TabEvent, TabMark, TabStrip, connected_tabs, new_glyph};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::ICON;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, Axis, Bounds, ClickEvent, Context, CursorStyle, DispatchPhase, Div,
    Entity, Focusable, MouseButton, MouseDownEvent, MouseMoveEvent, Pixels, SharedString, Size,
    Stateful, StyleRefinement, Subscription, WeakEntity, Window, canvas, div, prelude::*, px,
};

use super::{Editor, divider};
use crate::modules::chat::{self, Chat, ChatMirror};
use crate::modules::terminal::{self, TabChanged, TerminalModule};

const BOTTOM_START: f32 = 240.0;
const BOTTOM_LEAST: f32 = 120.0;
const BOTTOM_MOST: f32 = 0.7;
const RIGHT_START: f32 = 380.0;
const RIGHT_LEAST: f32 = 280.0;
const RIGHT_MOST: f32 = 0.6;
const LATE: f64 = 1.5;
const DISABLED: f32 = 0.45;
const TOGGLE_PAD: f32 = 4.0;
const TOGGLE_RADIUS: f32 = 5.0;
const NO_CHAT: &str = "No session running in this project";

type ChatSource = Rc<dyn Fn(&App) -> Option<Entity<Chat>>>;

#[derive(Clone, Copy, PartialEq, Eq)]
enum Dock {
    Terminal,
    Chat,
}

impl Dock {
    fn name(self) -> &'static str {
        match self {
            Dock::Terminal => "terminal",
            Dock::Chat => "chat",
        }
    }
}

pub(super) struct Docks {
    source: Option<ChatSource>,
    terminals: Vec<TerminalTab>,
    shown: usize,
    opened: usize,
    terminal_open: bool,
    mirror: Option<Entity<ChatMirror>>,
    bottom: f32,
    right: f32,
    dragging: Option<Dock>,
    drags: usize,
    held: Held,
    shell_box: Rc<Cell<Bounds<Pixels>>>,
    chat_box: Rc<Cell<Bounds<Pixels>>>,
    timing: Rc<RefCell<DragTiming>>,
    span: Rc<Cell<Bounds<Pixels>>>,
}

struct TerminalTab {
    view: Entity<TerminalModule>,
    label: SharedString,
    _renamed: Subscription,
}

#[derive(Clone, Copy, Default)]
struct Held {
    shell: Option<Size<Pixels>>,
    chat: Option<Size<Pixels>>,
}

#[derive(Default)]
struct DragTiming {
    painted: Vec<Paint>,
    work: Vec<f64>,
    to_row: Vec<f64>,
    row_prepaint: Vec<f64>,
    paint: Vec<f64>,
    ticks: Vec<Instant>,
    moves: usize,
    clamped: usize,
    waiting: Option<Instant>,
    row_reached: Option<Instant>,
}

struct Paint {
    at: Instant,
    waited: Option<Instant>,
}

impl Docks {
    pub(super) fn closed() -> Self {
        Docks {
            source: None,
            terminals: Vec::new(),
            shown: 0,
            opened: 0,
            terminal_open: false,
            mirror: None,
            bottom: BOTTOM_START,
            right: RIGHT_START,
            dragging: None,
            drags: 0,
            held: Held::default(),
            shell_box: Rc::default(),
            chat_box: Rc::default(),
            timing: Rc::default(),
            span: Rc::default(),
        }
    }

    fn hold(&mut self) {
        let laid = |size: &Size<Pixels>| size.width > px(0.0) && size.height > px(0.0);
        self.held = Held {
            shell: self
                .terminal_open
                .then(|| self.shell_box.get().size)
                .filter(laid),
            chat: self
                .mirror
                .as_ref()
                .map(|_| self.chat_box.get().size)
                .filter(laid),
        };
    }
}

fn held_box(content: AnyView, held: Option<Size<Pixels>>, seen: Rc<Cell<Bounds<Pixels>>>) -> Div {
    let inner = match held {
        Some(size) => div().flex_none().w(size.width).h(size.height),
        None => div().flex().flex_1().min_h_0().w_full(),
    };
    div()
        .relative()
        .flex()
        .flex_col()
        .justify_end()
        .overflow_hidden()
        .min_w_0()
        .min_h_0()
        .child(
            canvas(move |bounds, _, _| seen.set(bounds), |_, (), _, _| {})
                .absolute()
                .size_full(),
        )
        .child(
            inner
                .overflow_hidden()
                .child(content.cached(StyleRefinement::default().size_full())),
        )
}

fn tick(editor: WeakEntity<Editor>, drag: usize, window: &mut Window) {
    window.on_next_frame(move |window, cx| {
        let held = editor.update(cx, |editor, _| {
            let held = editor.docks.dragging.is_some() && editor.docks.drags == drag;
            if held {
                editor.docks.timing.borrow_mut().ticks.push(Instant::now());
            }
            held
        });
        if let Ok(true) = held {
            tick(editor, drag, window);
        }
    });
}

fn ms(from: Instant, to: Instant) -> f64 {
    to.saturating_duration_since(from).as_secs_f64() * 1000.0
}

fn gaps(times: &[Instant]) -> Vec<f64> {
    times
        .windows(2)
        .filter_map(|pair| match pair {
            [from, to] => Some(ms(*from, *to)),
            _ => None,
        })
        .collect()
}

fn moving(painted: &[Paint], period: f64) -> Vec<f64> {
    painted
        .windows(2)
        .filter_map(|pair| match pair {
            [from, to] => to
                .waited
                .filter(|waited| ms(from.at, *waited) <= period)
                .map(|_| ms(from.at, to.at)),
            _ => None,
        })
        .collect()
}

fn refresh(ticks: &[f64]) -> Option<f64> {
    let least = ticks.iter().copied().reduce(f64::min)?;
    let single = ticks.iter().copied().filter(|ms| *ms < LATE * least);
    spread(single.collect()).map(|(median, _, _)| median)
}

fn spread(mut values: Vec<f64>) -> Option<(f64, f64, f64)> {
    values.sort_by(f64::total_cmp);
    let median = *values.get(values.len() / 2)?;
    let worst = *values.last()?;
    let p95 = values
        .get(values.len() * 95 / 100)
        .copied()
        .unwrap_or(worst);
    Some((median, p95, worst))
}

fn fit(size: f32, least: f32, share: f32, room: f32) -> f32 {
    match room > 0.0 {
        true => size.min(room * share).max(least),
        false => size.max(least),
    }
}

impl Editor {
    pub fn chat_from(&mut self, source: impl Fn(&App) -> Option<Entity<Chat>> + 'static) {
        self.docks.source = Some(Rc::new(source));
    }

    fn chat(&self, cx: &App) -> Option<Entity<Chat>> {
        self.docks.source.as_ref().and_then(|source| source(cx))
    }

    pub(super) fn close_docks(&mut self) {
        let ended = std::mem::take(&mut self.docks.terminals).len();
        if ended > 0 {
            eprintln!(
                "desk: editor dock {ended} terminal tabs closed, their shells end with the old folder"
            );
        }
        self.docks.shown = 0;
        self.docks.terminal_open = false;
        if self.docks.mirror.take().is_some() {
            eprintln!("desk: editor dock chat closed");
        }
    }

    fn toggle_terminal(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.docks.terminal_open = !self.docks.terminal_open;
        cx.notify();
        if !self.docks.terminal_open {
            return eprintln!("desk: editor dock terminal closed");
        }
        if self.docks.terminals.is_empty() {
            self.open_terminal_tab(window, cx);
        }
        self.focus_terminal_tab(window, cx);
        eprintln!("desk: editor dock terminal open");
    }

    fn open_terminal_tab(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.docks.opened = self.docks.opened.saturating_add(1);
        let number = self.docks.opened;
        let view = terminal::mount(&self.folder.listing.root, number, cx);
        let named = move |view: &Entity<TerminalModule>, cx: &App| {
            view.read(cx)
                .label(cx)
                .unwrap_or_else(|| format!("Terminal {number}").into())
        };
        let label = named(&view, cx);
        let renamed = cx.subscribe(&view, move |editor, view, _: &TabChanged, cx| {
            let label = named(&view, cx);
            let Some(tab) = editor
                .docks
                .terminals
                .iter_mut()
                .find(|tab| tab.view == view)
            else {
                return;
            };
            if tab.label != label {
                tab.label = label;
                cx.notify();
            }
        });
        self.docks.terminals.push(TerminalTab {
            view,
            label,
            _renamed: renamed,
        });
        self.docks.shown = self.docks.terminals.len().saturating_sub(1);
        self.focus_terminal_tab(window, cx);
        cx.notify();
        eprintln!(
            "desk: editor dock terminal tab {} opened, {} open",
            self.docks.opened,
            self.docks.terminals.len()
        );
    }

    fn focus_terminal_tab(&self, window: &mut Window, cx: &mut Context<Self>) {
        if let Some(tab) = self.docks.terminals.get(self.docks.shown) {
            let focus = tab.view.read(cx).focus_handle(cx);
            focus.focus(window, cx);
        }
    }

    fn terminal_tab_event(&mut self, event: TabEvent, window: &mut Window, cx: &mut Context<Self>) {
        match event {
            TabEvent::New => return self.open_terminal_tab(window, cx),
            TabEvent::Select(at) if at < self.docks.terminals.len() => self.docks.shown = at,
            TabEvent::Select(_) => return,
            TabEvent::Close(at) if at < self.docks.terminals.len() => {
                self.docks.terminals.remove(at);
                eprintln!(
                    "desk: editor dock terminal tab closed, its shell ends, {} open",
                    self.docks.terminals.len()
                );
                if at < self.docks.shown || self.docks.shown >= self.docks.terminals.len() {
                    self.docks.shown = self.docks.shown.saturating_sub(1);
                }
                if self.docks.terminals.is_empty() {
                    self.docks.terminal_open = false;
                }
            }
            TabEvent::Close(_) => return,
        }
        self.focus_terminal_tab(window, cx);
        cx.notify();
    }

    fn terminal_strip(&self, theme: &Theme, cx: &mut Context<Self>) -> TabStrip {
        desk_perf::span!("editor-terminal-strip");
        let tabs: Vec<Tab> = self
            .docks
            .terminals
            .iter()
            .map(|tab| Tab {
                label: tab.label.clone(),
                icon: Some(Glyph::Terminal),
                count: None,
                mark: TabMark::Close,
                flag: None,
            })
            .collect();
        let (editor, adder) = (cx.weak_entity(), cx.weak_entity());
        let add = new_glyph("editor-terminal-new", "New terminal", theme).on_click(
            move |_, window, cx| {
                if let Err(error) =
                    adder.update(cx, |editor, cx| editor.open_terminal_tab(window, cx))
                {
                    eprintln!("desk: editor is gone before a terminal tab opened: {error}");
                }
            },
        );
        connected_tabs(
            "editor-terminal-tabs",
            &tabs,
            self.docks.shown,
            tabs.len().max(1),
            theme,
            move |event, window, cx| {
                let event = *event;
                if let Err(error) = editor.update(cx, |editor, cx| {
                    editor.terminal_tab_event(event, window, cx)
                }) {
                    eprintln!("desk: editor is gone before its terminal tab event: {error}");
                }
            },
        )
        .new_button(add)
    }

    fn toggle_chat(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.docks.mirror.take().is_some() {
            cx.notify();
            return eprintln!("desk: editor dock chat closed");
        }
        let Some(owner) = self.chat(cx) else {
            return eprintln!("desk: editor dock chat unavailable");
        };
        self.docks.mirror = Some(chat::mirror(owner, window, cx));
        cx.notify();
        eprintln!("desk: editor dock chat open");
    }

    pub(super) fn dock_buttons(&self, window: &mut Window, cx: &mut Context<Self>) -> AnyElement {
        let chat_ready = self.docks.mirror.is_some() || self.chat(cx).is_some();
        let terminal = self.dock_button(Dock::Terminal, self.docks.terminal_open, true, window, cx);
        let chat = self.dock_button(
            Dock::Chat,
            self.docks.mirror.is_some(),
            chat_ready,
            window,
            cx,
        );
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap_0p5()
            .child(terminal)
            .child(chat)
            .into_any_element()
    }

    fn dock_button(
        &self,
        dock: Dock,
        open: bool,
        ready: bool,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        let theme = ActiveTheme::theme(cx);
        let (id, label, mark) = match dock {
            Dock::Terminal => ("editor-dock-terminal", "Terminal", Glyph::Terminal),
            Dock::Chat => ("editor-dock-chat", "Chat", Glyph::Chat),
        };
        let ink = match open {
            true => theme.color(ColorToken::TextStrong),
            false => theme.color(ColorToken::TextIcon),
        };
        let face = control(id, label, &theme)
            .size(px(ICON + 2.0 * TOGGLE_PAD))
            .rounded(px(TOGGLE_RADIUS))
            .when(open, |face| face.bg(theme.color(ColorToken::StateActive)))
            .when(!ready, |face| {
                face.opacity(DISABLED).cursor(CursorStyle::Arrow)
            })
            .child(glyph(mark, ICON, ink))
            .on_click(
                cx.listener(move |editor, _: &ClickEvent, window, cx| match dock {
                    Dock::Terminal => editor.toggle_terminal(window, cx),
                    Dock::Chat => editor.toggle_chat(window, cx),
                }),
            );
        let tip = match ready {
            true => label,
            false => NO_CHAT,
        };
        tooltip(
            format!("{id}-tip"),
            face,
            Edge::Frame,
            tip,
            &theme,
            window,
            cx,
        )
    }

    pub(super) fn docked(
        &mut self,
        main: Div,
        started: Instant,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        desk_perf::span!("editor-terminal-pane");
        let room = self.docks.span.get().size;
        let bottom = fit(
            self.docks.bottom,
            BOTTOM_LEAST,
            BOTTOM_MOST,
            room.height.as_f32(),
        );
        let right = fit(
            self.docks.right,
            RIGHT_LEAST,
            RIGHT_MOST,
            room.width.as_f32(),
        );
        let terminal = self
            .docks
            .terminals
            .get(self.docks.shown)
            .map(|tab| tab.view.clone())
            .filter(|_| self.docks.terminal_open);
        let column = div()
            .flex()
            .flex_col()
            .flex_1()
            .min_w_0()
            .min_h_0()
            .child(main)
            .when_some(terminal, |column, terminal| {
                column
                    .child(self.dock_divider(Dock::Terminal, theme, cx))
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .flex_none()
                            .min_w_0()
                            .h(px(bottom))
                            .child(self.terminal_strip(theme, cx))
                            .child(
                                held_box(
                                    terminal.into(),
                                    self.docks.held.shell,
                                    self.docks.shell_box.clone(),
                                )
                                .flex_1(),
                            ),
                    )
            });
        let dragging = self.docks.dragging.is_some();
        let reached = self.docks.timing.clone();
        let row_reached = dragging.then(|| {
            canvas(
                move |_, _, _| reached.borrow_mut().row_reached = Some(Instant::now()),
                |_, (), _, _| {},
            )
            .absolute()
            .size_0()
        });
        let timing = self.docks.timing.clone();
        let timed = dragging.then(|| {
            canvas(
                |_, _, _| Instant::now(),
                move |_, prepainted, _, _| {
                    let now = Instant::now();
                    let mut timing = timing.borrow_mut();
                    let waited = timing.waiting.take();
                    let reached = timing.row_reached.take().unwrap_or(prepainted);
                    timing.painted.push(Paint { at: now, waited });
                    timing.work.push(ms(started, now));
                    timing.to_row.push(ms(started, reached));
                    timing.row_prepaint.push(ms(reached, prepainted));
                    timing.paint.push(ms(prepainted, now));
                },
            )
            .absolute()
            .size_0()
        });
        let span = self.docks.span.clone();
        div()
            .flex()
            .flex_row()
            .flex_1()
            .min_w_0()
            .min_h_0()
            .relative()
            .children(row_reached)
            .child(
                canvas(move |bounds, _, _| span.set(bounds), |_, (), _, _| {})
                    .absolute()
                    .size_full(),
            )
            .child(column)
            .when_some(self.docks.mirror.clone(), |row, mirror| {
                row.child(self.dock_divider(Dock::Chat, theme, cx)).child(
                    held_box(
                        mirror.into(),
                        self.docks.held.chat,
                        self.docks.chat_box.clone(),
                    )
                    .items_end()
                    .flex_none()
                    .w(px(right)),
                )
            })
            .children(timed)
            .into_any_element()
    }

    pub(super) fn dock_cover(&self, cx: &mut Context<Self>) -> Option<Div> {
        let cursor = match self.docks.dragging? {
            Dock::Terminal => CursorStyle::ResizeUpDown,
            Dock::Chat => CursorStyle::ResizeLeftRight,
        };
        let (moved, lifted) = (cx.weak_entity(), cx.weak_entity());
        Some(
            div()
                .absolute()
                .top_0()
                .left_0()
                .size_full()
                .occlude()
                .cursor(cursor)
                .on_mouse_move_all(move |event: &MouseMoveEvent, phase, _, _, cx| {
                    if phase != DispatchPhase::Bubble {
                        return;
                    }
                    if let Err(error) = moved.update(cx, |editor, cx| editor.drag_dock(event, cx)) {
                        eprintln!("desk: editor is gone during a dock drag: {error}");
                    }
                })
                .on_mouse_up_all(move |_, phase, _, _, cx| {
                    if phase != DispatchPhase::Bubble {
                        return;
                    }
                    if let Err(error) = lifted.update(cx, |editor, cx| editor.drop_dock(cx)) {
                        eprintln!("desk: editor is gone at the end of a dock drag: {error}");
                    }
                }),
        )
    }

    fn dock_divider(&self, dock: Dock, theme: &Theme, cx: &mut Context<Self>) -> Stateful<Div> {
        let (id, sides) = match dock {
            Dock::Terminal => ("editor-dock-terminal-divider", Axis::Vertical),
            Dock::Chat => ("editor-dock-chat-divider", Axis::Horizontal),
        };
        let dragged = self.docks.dragging == Some(dock);
        divider(id, sides, dragged, theme).on_mouse_down(
            MouseButton::Left,
            cx.listener(move |editor, _: &MouseDownEvent, window, cx| {
                cx.stop_propagation();
                editor.docks.dragging = Some(dock);
                editor.docks.hold();
                editor.docks.drags = editor.docks.drags.wrapping_add(1);
                *editor.docks.timing.borrow_mut() = DragTiming::default();
                tick(cx.weak_entity(), editor.docks.drags, window);
                cx.notify();
            }),
        )
    }

    fn drag_dock(&mut self, event: &MouseMoveEvent, cx: &mut Context<Self>) {
        let Some(dock) = self.docks.dragging else {
            return;
        };
        if event.pressed_button != Some(MouseButton::Left) {
            return self.drop_dock(cx);
        }
        let span = self.docks.span.get();
        let (wanted, size, held) = match dock {
            Dock::Terminal => {
                let wanted = (span.bottom() - event.position.y).as_f32();
                let room = span.size.height.as_f32();
                let size = fit(wanted, BOTTOM_LEAST, BOTTOM_MOST, room);
                (wanted, size, &mut self.docks.bottom)
            }
            Dock::Chat => {
                let wanted = (span.right() - event.position.x).as_f32();
                let room = span.size.width.as_f32();
                let size = fit(wanted, RIGHT_LEAST, RIGHT_MOST, room);
                (wanted, size, &mut self.docks.right)
            }
        };
        let changed = *held != size;
        *held = size;
        let mut timing = self.docks.timing.borrow_mut();
        timing.moves = timing.moves.saturating_add(1);
        if wanted != size {
            timing.clamped = timing.clamped.saturating_add(1);
        }
        if changed {
            timing.waiting.get_or_insert_with(Instant::now);
            cx.notify();
        }
    }

    fn drop_dock(&mut self, cx: &mut Context<Self>) {
        let Some(dock) = self.docks.dragging.take() else {
            return;
        };
        self.docks.held = Held::default();
        cx.notify();
        let size = match dock {
            Dock::Terminal => self.docks.bottom,
            Dock::Chat => self.docks.right,
        };
        eprintln!("desk: editor dock {} resized to {size:.0} px", dock.name());
        let DragTiming {
            painted,
            work,
            to_row,
            row_prepaint,
            paint,
            ticks,
            moves,
            clamped,
            waiting: _,
            row_reached: _,
        } = std::mem::take(&mut *self.docks.timing.borrow_mut());
        let at: Vec<Instant> = painted.iter().map(|paint| paint.at).collect();
        let intervals = gaps(&at);
        let vsyncs = gaps(&ticks);
        let held_ms = match (ticks.first(), ticks.last()) {
            (Some(first), Some(last)) => ms(*first, *last),
            _ => 0.0,
        };
        let (Some(period), Some((tick_median, _, tick_worst))) = (refresh(&vsyncs), spread(vsyncs))
        else {
            return eprintln!("desk: editor dock {} drag saw no vsync", dock.name());
        };
        let late = intervals.iter().filter(|ms| **ms > LATE * period).count();
        let steady = moving(&painted, period);
        let steady_frames = steady.len();
        let lags: Vec<f64> = painted
            .iter()
            .filter_map(|paint| paint.waited.map(|waited| ms(waited, paint.at)))
            .collect();
        let missed = lags.iter().filter(|ms| **ms > LATE * period).count();
        let (
            Some((median, p95, worst)),
            Some((steady_median, steady_p95, steady_worst)),
            Some((lag_median, _, lag_worst)),
            Some((work_median, _, work_worst)),
            Some((to_row_median, _, _)),
            Some((row_median, _, _)),
            Some((paint_median, _, _)),
        ) = (
            spread(intervals),
            spread(steady),
            spread(lags),
            spread(work),
            spread(to_row),
            spread(row_prepaint),
            spread(paint),
        )
        else {
            return eprintln!("desk: editor dock {} drag drew no frames", dock.name());
        };
        eprintln!(
            "desk: editor dock {} drag frames: {} frames, median {median:.2} ms, p95 {p95:.2} ms, worst {worst:.2} ms, {late} over {LATE}x refresh; while moving {} frames, median {steady_median:.2} ms, p95 {steady_p95:.2} ms, worst {steady_worst:.2} ms; move to paint median {lag_median:.2} ms, worst {lag_worst:.2} ms, {missed} over {LATE}x refresh; refresh {period:.2} ms, vsync ticks {} with median {tick_median:.2} ms, worst {tick_worst:.2} ms; {moves} pointer moves in {held_ms:.0} ms, {clamped} at a limit; render to paint median {work_median:.2} ms, worst {work_worst:.2} ms, of it render and layout to the dock row {to_row_median:.2} ms, dock row prepaint {row_median:.2} ms, paint {paint_median:.2} ms",
            dock.name(),
            painted.len(),
            steady_frames,
            ticks.len()
        );
    }
}
