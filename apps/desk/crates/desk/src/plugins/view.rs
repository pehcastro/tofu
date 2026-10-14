use std::cell::RefCell;
use std::collections::HashMap;
use std::ops::Range;
use std::rc::Rc;
use std::sync::mpsc;
use std::thread;
use std::time::{Duration, Instant};

use desk_plugin::{Error, Event, Kind, Node, Pending};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::chip::{badge, chip};
use desk_ui::components::empty::empty_state;
use desk_ui::components::form::{TextArea, segmented, switch_bare};
use desk_ui::components::list::{CellKind, Column, Width, row, table};
use desk_ui::components::scroll::scrollbar;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, ElementId, Entity, FocusHandle,
    IntoElement, KeyDownEvent, MouseButton, Render, ScrollStrategy, SharedString, Subscription,
    Task, UniformListScrollHandle, WeakEntity, Window, canvas, div, prelude::*, uniform_list,
};

use super::Plugin;
use super::wasm::Guest;

const FIRST_WINDOW: u64 = 30;
const MIN_PAGE: u64 = 16;
const MAX_CACHED: usize = 4096;
const MAX_DEPTH: usize = 32;

enum Ask {
    View(Pending<Vec<Node>>),
    Rows(Range<u64>, Instant, Pending<Vec<Node>>),
    Handle(Pending<()>),
}

enum Reply {
    View(Result<Vec<Node>, Error>),
    Rows(Range<u64>, Duration, Result<Vec<Node>, Error>),
    Handle(Result<(), Error>),
}

type Cells = Rc<RefCell<HashMap<u64, Rc<[SharedString]>>>>;

struct Parts {
    entry: &'static Plugin,
    this: WeakEntity<GuestView>,
    cells: Cells,
    scroll: UniformListScrollHandle,
    inputs: HashMap<String, (Entity<TextArea>, Subscription)>,
    columns: Vec<(Vec<String>, &'static [Column])>,
    words: HashMap<String, &'static str>,
}

pub struct GuestView {
    guest: &'static Guest,
    tree: Option<Vec<Node>>,
    fault: Option<String>,
    parts: Parts,
    focus: FocusHandle,
    asking: Vec<Range<u64>>,
    shown: Range<u64>,
    guest_time: Duration,
    opened: Option<Instant>,
    asks: Option<mpsc::Sender<Ask>>,
    _replies: Task<()>,
}

pub fn mount(entry: &'static Plugin, guest: &'static Guest, cx: &mut App) -> AnyView {
    cx.new(|cx| {
        let (asks, waiting) = mpsc::channel();
        let (answers, replies) = flume::unbounded();
        let waiter = thread::Builder::new()
            .name(format!("plugin {} waiter", entry.id))
            .spawn(move || wait(&waiting, &answers));
        let (asks, fault) = match waiter {
            Ok(_) => (Some(asks), None),
            Err(error) => (
                None,
                Some(format!("the desk could not start a thread: {error}")),
            ),
        };
        let mut view = GuestView {
            guest,
            tree: None,
            fault,
            parts: Parts {
                entry,
                this: cx.weak_entity(),
                cells: Cells::default(),
                scroll: UniformListScrollHandle::new(),
                inputs: HashMap::new(),
                columns: Vec::new(),
                words: HashMap::new(),
            },
            focus: cx.focus_handle(),
            asking: Vec::new(),
            shown: 0..0,
            guest_time: Duration::ZERO,
            opened: Some(Instant::now()),
            asks,
            _replies: cx.spawn(async move |this, cx| {
                while let Ok(reply) = replies.recv_async().await {
                    if this.update(cx, |view, cx| view.apply(reply, cx)).is_err() {
                        break;
                    }
                }
            }),
        };
        view.ask(|plugin| Ask::View(plugin.view()));
        view
    })
    .into()
}

fn wait(asks: &mpsc::Receiver<Ask>, replies: &flume::Sender<Reply>) {
    for ask in asks {
        let reply = match ask {
            Ask::View(pending) => Reply::View(pending.wait()),
            Ask::Rows(range, asked, pending) => {
                let rows = pending.wait();
                Reply::Rows(range, asked.elapsed(), rows)
            }
            Ask::Handle(pending) => Reply::Handle(pending.wait()),
        };
        if replies.send(reply).is_err() {
            return;
        }
    }
}

impl GuestView {
    fn ask(&mut self, ask: impl FnOnce(&desk_plugin::Plugin) -> Ask) {
        let (Some(plugin), Some(asks)) = (self.guest.running(), &self.asks) else {
            return;
        };
        if asks.send(ask(plugin)).is_err() {
            self.fault = Some("the plugin waiter stopped".to_owned());
        }
    }

    fn send(&mut self, event: Event, cx: &mut Context<Self>) {
        self.ask(|plugin| Ask::Handle(plugin.handle(event)));
        self.ask(|plugin| Ask::View(plugin.view()));
        self.parts.cells.borrow_mut().clear();
        self.asking.clear();
        cx.notify();
    }

    fn apply(&mut self, reply: Reply, cx: &mut Context<Self>) {
        match reply {
            Reply::View(Ok(tree)) => {
                if let Some(count) = windowed(&tree) {
                    eprintln!("desk: plugin {} table of {count} rows", self.parts.entry.id);
                }
                self.tree = Some(tree);
                self.fault = None;
            }
            Reply::Rows(range, guest, Ok(tree)) => {
                self.asking.retain(|asked| *asked != range);
                self.guest_time += guest;
                let mut cells = self.parts.cells.borrow_mut();
                let rows = tree.first().map(|root| root.children.as_slice());
                for (at, &line) in (range.start..).zip(rows.unwrap_or_default()) {
                    let texts: Rc<[SharedString]> = node(&tree, line)
                        .map(|line| line.children.as_slice())
                        .unwrap_or_default()
                        .iter()
                        .map(|&cell| node(&tree, cell).and_then(cell_text).unwrap_or_default())
                        .collect();
                    cells.insert(at, texts);
                }
                if cells.len() > MAX_CACHED {
                    let page = span(&self.shown).max(MIN_PAGE);
                    let keep = self.shown.start.saturating_sub(page.saturating_mul(4))
                        ..self.shown.end.saturating_add(page.saturating_mul(4));
                    cells.retain(|at, _| keep.contains(at));
                }
            }
            Reply::Handle(Ok(())) => {}
            Reply::View(Err(error)) | Reply::Rows(_, _, Err(error)) | Reply::Handle(Err(error)) => {
                self.asking.clear();
                self.failed(error);
            }
        }
        cx.notify();
    }

    fn failed(&mut self, error: Error) {
        let reason = error.to_string();
        match error {
            Error::Deadline { .. } | Error::Trap { .. } | Error::Stopped { .. } => {
                self.guest.trap(&self.parts.entry.id, reason);
            }
            _ => {
                eprintln!("desk: plugin {}: {reason}", self.parts.entry.id);
                self.fault = Some(reason);
            }
        }
    }

    fn fetch(&mut self, wanted: Range<u64>) {
        let missing = |at: &u64| {
            !self.parts.cells.borrow().contains_key(at)
                && !self.asking.iter().any(|asked| asked.contains(at))
        };
        let Some(first) = wanted.clone().find(&missing) else {
            return;
        };
        let last = wanted.rev().find(&missing).unwrap_or(first);
        let range = first..last.saturating_add(1);
        self.asking.push(range.clone());
        self.ask(|plugin| Ask::Rows(range.clone(), Instant::now(), plugin.rows(range)));
    }

    fn around(&mut self, shown: &Range<u64>, count: u64) {
        let page = span(shown).max(MIN_PAGE);
        let start = shown.start.saturating_sub(page) / page * page;
        let end = shown
            .end
            .saturating_add(page)
            .div_ceil(page)
            .saturating_mul(page);
        self.fetch(start..end.min(count));
    }

    fn key(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        let Some(count) = self.tree.as_deref().and_then(windowed) else {
            return;
        };
        let shown = visible(&self.parts.scroll, count);
        let page = span(&shown).max(1);
        let bottom = count.saturating_sub(page);
        let top = match event.keystroke.key.as_str() {
            "home" => 0,
            "end" => bottom,
            "pageup" => shown.start.saturating_sub(page),
            "pagedown" => shown.start.saturating_add(page).min(bottom),
            _ => return,
        };
        cx.stop_propagation();
        let to = usize::try_from(top).unwrap_or(usize::MAX);
        self.parts
            .scroll
            .scroll_to_item_strict(to, ScrollStrategy::Top);
        eprintln!("desk: plugin {} scrolled to row {top}", self.parts.entry.id);
        self.around(&(top..top.saturating_add(page).min(count)), count);
        cx.notify();
    }

    fn frame_log(
        &mut self,
        shown: Range<u64>,
        began: Instant,
        build: Duration,
    ) -> Option<AnyElement> {
        let cells = self.parts.cells.borrow();
        let ready = shown.clone().all(|at| cells.contains_key(&at));
        drop(cells);
        let arrived = self.guest_time > Duration::ZERO;
        if !ready || (shown == self.shown && !arrived) {
            return None;
        }
        let page = span(&self.shown).max(1);
        if shown.start.abs_diff(self.shown.start) > page {
            eprintln!(
                "desk: plugin {} scrolled to row {}",
                self.parts.entry.id, shown.start
            );
        }
        let id = self.parts.entry.id.as_str();
        let rows = span(&shown);
        let guest = std::mem::take(&mut self.guest_time);
        let opened = self.opened.take();
        self.shown = shown;
        Some(
            canvas(
                |_, _, _| {},
                move |_, _, _, _| {
                    eprintln!(
                        "desk: plugin {id} view+paint {rows} rows in {:.3} ms (guest {:.3} ms, build {:.3} ms)",
                        ms(began.elapsed()),
                        ms(guest),
                        ms(build)
                    );
                    if let Some(opened) = opened {
                        eprintln!(
                            "desk: plugin {id} first rows on screen {:.3} ms after open",
                            ms(opened.elapsed())
                        );
                    }
                },
            )
            .absolute()
            .size_full()
            .into_any_element(),
        )
    }
}

impl Render for GuestView {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let began = Instant::now();
        let theme = ActiveTheme::theme(cx);
        let entry = self.parts.entry;
        let stopped = self
            .guest
            .stopped()
            .map(|(state, reason)| (state, reason.to_owned()))
            .or_else(|| self.fault.clone().map(|reason| ("failed", reason)));
        let mut logged = None;
        let body = match (stopped, self.tree.take()) {
            (Some((state, reason)), tree) => {
                self.tree = tree;
                error_part(
                    "plugin-stopped",
                    format!("{} {state}", entry.name),
                    reason,
                    &theme,
                )
            }
            (None, None) => div()
                .text_color(theme.color(ColorToken::TextBase))
                .child(format!("{} is starting", entry.name))
                .into_any_element(),
            (None, Some(tree)) => {
                let count = windowed(&tree);
                let shown = count.map(|count| {
                    let shown = visible(&self.parts.scroll, count);
                    self.around(&shown, count);
                    shown
                });
                let drawn = self.parts.draw(&tree, &theme, window, cx);
                self.tree = Some(tree);
                logged = shown.and_then(|shown| self.frame_log(shown, began, began.elapsed()));
                drawn
            }
        };
        div()
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::key))
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|view, _, window, cx| view.focus.focus(window, cx)),
            )
            .relative()
            .size_full()
            .flex()
            .flex_col()
            .min_h_0()
            .p_2()
            .child(body)
            .children(logged)
    }
}

fn ms(duration: Duration) -> f64 {
    duration.as_secs_f64() * 1000.0
}

fn span(range: &Range<u64>) -> u64 {
    range.end.saturating_sub(range.start)
}

fn node(tree: &[Node], at: u32) -> Option<&Node> {
    tree.get(usize::try_from(at).ok()?)
}

fn row_at(at: usize) -> u64 {
    u64::try_from(at).unwrap_or(u64::MAX)
}

fn windowed(tree: &[Node]) -> Option<u64> {
    tree.iter().find_map(|node| match &node.kind {
        Kind::Table(table) => Some(table.row_count),
        Kind::VirtualList(count) => Some(*count),
        _ => None,
    })
}

fn visible(scroll: &UniformListScrollHandle, count: u64) -> Range<u64> {
    let state = scroll.0.borrow();
    let first = 0..FIRST_WINDOW.min(count);
    let Some(size) = state.last_item_size else {
        return first;
    };
    let total = f32::from(size.contents.height);
    if count == 0 || total <= 0.0 {
        return first;
    }
    let line = total / count as f32;
    let top = (-f32::from(state.base_handle.offset().y) / line).floor() as u64;
    let shown = (f32::from(size.item.height) / line).ceil() as u64;
    top.min(count)..top.saturating_add(shown).saturating_add(1).min(count)
}

fn error_part(
    id: impl Into<ElementId>,
    title: impl Into<SharedString>,
    reason: impl Into<SharedString>,
    theme: &Theme,
) -> AnyElement {
    empty_state(
        id,
        title,
        Some(reason.into()),
        &[],
        &[],
        theme,
        |_, _, _| {},
    )
    .into_any_element()
}

fn deliver(this: &WeakEntity<GuestView>, event: Event, cx: &mut App) {
    if let Err(error) = this.update(cx, |view, cx| view.send(event, cx)) {
        eprintln!("desk: plugin view is gone, the event is dropped: {error}");
    }
}

fn on_click(
    this: &WeakEntity<GuestView>,
    event: Event,
) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
    let this = this.clone();
    move |_, _, cx| deliver(&this, event.clone(), cx)
}

fn cell_text(node: &Node) -> Option<SharedString> {
    match &node.kind {
        Kind::Text(text)
        | Kind::Chip(text)
        | Kind::Button(text)
        | Kind::Input(text)
        | Kind::Group(text)
        | Kind::Avatar(text)
        | Kind::FeedItem(text) => Some(text.clone().into()),
        Kind::Badge(badge) => Some(badge.label.clone().into()),
        _ => None,
    }
}

fn kind_name(kind: &Kind) -> &'static str {
    match kind {
        Kind::Shell => "shell",
        Kind::Inner => "inner",
        Kind::Tabs(_) => "tabs",
        Kind::Row => "row",
        Kind::Group(_) => "group",
        Kind::Avatar(_) => "avatar",
        Kind::Chip(_) => "chip",
        Kind::Badge(_) => "badge",
        Kind::Button(_) => "button",
        Kind::Segmented(_) => "segmented",
        Kind::Switch(_) => "switch",
        Kind::Input(_) => "input",
        Kind::Table(_) => "table",
        Kind::VirtualList(_) => "virtual-list",
        Kind::Diff(_) => "diff",
        Kind::Term(_) => "term",
        Kind::FeedItem(_) => "feed-item",
        Kind::Sheet => "sheet",
        Kind::Popover => "popover",
        Kind::Text(_) => "text",
        Kind::Stack => "stack",
        Kind::Grid(_) => "grid",
    }
}

impl Parts {
    fn draw(
        &mut self,
        tree: &[Node],
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<GuestView>,
    ) -> AnyElement {
        let mut depth = vec![0usize; tree.len()];
        for (at, node) in tree.iter().enumerate() {
            let below = depth
                .get(at)
                .copied()
                .unwrap_or(MAX_DEPTH)
                .saturating_add(1);
            for &child in &node.children {
                if let Some(slot) = usize::try_from(child).ok().and_then(|c| depth.get_mut(c)) {
                    *slot = below;
                }
            }
        }
        let mut built: Vec<Option<AnyElement>> = tree.iter().map(|_| None).collect();
        let mut windowed = false;
        for (at, node) in tree.iter().enumerate().rev() {
            let children: Vec<AnyElement> = node
                .children
                .iter()
                .filter_map(|&child| built.get_mut(usize::try_from(child).ok()?)?.take())
                .collect();
            let element = match depth.get(at) {
                Some(&deep) if deep <= MAX_DEPTH => {
                    self.part(node, children, &mut windowed, theme, window, cx)
                }
                _ => error_part(
                    SharedString::from(node.key.clone()),
                    "nested too deep",
                    format!("the desk draws {MAX_DEPTH} levels of parts"),
                    theme,
                ),
            };
            if let Some(slot) = built.get_mut(at) {
                *slot = Some(element);
            }
        }
        built
            .into_iter()
            .next()
            .flatten()
            .unwrap_or_else(|| div().into_any_element())
    }

    fn part(
        &mut self,
        node: &Node,
        children: Vec<AnyElement>,
        windowed: &mut bool,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<GuestView>,
    ) -> AnyElement {
        let key = node.key.clone();
        let id = SharedString::from(key.clone());
        match &node.kind {
            Kind::Stack => div()
                .flex()
                .flex_col()
                .gap_2()
                .flex_1()
                .min_h_0()
                .min_w_0()
                .children(children)
                .into_any_element(),
            Kind::Text(text) => div()
                .text_color(theme.color(ColorToken::TextBase))
                .child(text.clone())
                .into_any_element(),
            Kind::Button(label) => button(id, label.clone(), None, ButtonKind::Plain, theme)
                .on_click(on_click(&self.this, Event::Click(key)))
                .into_any_element(),
            Kind::Badge(mark) => badge(mark.label.clone(), theme).into_any_element(),
            Kind::Chip(text) => chip(text.clone(), None, theme).into_any_element(),
            Kind::Switch(toggle) => {
                let flipped = Event::Change((key, (!toggle.on).to_string()));
                switch_bare(id, toggle.label.clone(), toggle.on, theme)
                    .gap_2()
                    .child(toggle.label.clone())
                    .on_click(on_click(&self.this, flipped))
                    .into_any_element()
            }
            Kind::Segmented(choice) | Kind::Tabs(choice) => {
                let options: Vec<&'static str> = choice
                    .options
                    .iter()
                    .map(|option| self.word(option))
                    .collect();
                let picked = options.clone();
                let this = self.this.clone();
                let selected = usize::try_from(choice.selected).unwrap_or(usize::MAX);
                div()
                    .id(id)
                    .child(segmented(
                        self.entry.id.as_str(),
                        &options,
                        selected,
                        theme,
                        move |at, _, cx| {
                            if let Some(option) = picked.get(*at) {
                                deliver(
                                    &this,
                                    Event::Change((key.clone(), (*option).to_owned())),
                                    cx,
                                );
                            }
                        },
                    ))
                    .into_any_element()
            }
            Kind::Input(placeholder) => self.input(key, placeholder, window, cx).into_any_element(),
            Kind::Table(shape) if !*windowed => {
                *windowed = true;
                let columns = self.columns(&shape.columns);
                let cells = self.cells.clone();
                let count = usize::try_from(shape.row_count).unwrap_or(usize::MAX);
                div()
                    .id(id)
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_h_0()
                    .child(table(
                        self.entry.id.as_str(),
                        columns,
                        count,
                        None,
                        &self.scroll,
                        theme,
                        move |at, column| {
                            cells
                                .borrow()
                                .get(&row_at(at))
                                .and_then(|line| line.get(column).cloned())
                                .unwrap_or_default()
                        },
                    ))
                    .into_any_element()
            }
            Kind::VirtualList(count) if !*windowed => {
                *windowed = true;
                let cells = self.cells.clone();
                let count = usize::try_from(*count).unwrap_or(usize::MAX);
                let base = self.scroll.0.borrow().base_handle.clone();
                div()
                    .id(id.clone())
                    .relative()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_h_0()
                    .child(
                        uniform_list(id.clone(), count, move |range, _, cx| {
                            let theme = ActiveTheme::theme(cx);
                            let cells = cells.borrow();
                            range
                                .map(|at| {
                                    let line = cells.get(&row_at(at)).cloned();
                                    row(("plugin-row", at), false, false, &theme)
                                        .children(line.iter().flat_map(|line| line.iter().cloned()))
                                })
                                .collect::<Vec<_>>()
                        })
                        .track_scroll(&self.scroll)
                        .flex_1()
                        .min_h_0(),
                    )
                    .child(scrollbar(SharedString::from(format!("{id}-bar")), &base))
                    .into_any_element()
            }
            Kind::Table(_) | Kind::VirtualList(_) => error_part(
                id,
                "a second windowed list",
                "a plugin shows one table or virtual-list, its rows come from rows(range)",
                theme,
            ),
            other => error_part(
                id,
                format!("{} is not a part", kind_name(other)),
                format!("node {} asks for a part this desk does not draw", node.key),
                theme,
            ),
        }
    }

    fn input(
        &mut self,
        key: String,
        placeholder: &str,
        window: &mut Window,
        cx: &mut Context<GuestView>,
    ) -> Entity<TextArea> {
        if let Some((field, _)) = self.inputs.get(&key) {
            return field.clone();
        }
        let (this, submitted) = (self.this.clone(), key.clone());
        let field = cx.new(|cx| {
            TextArea::new(placeholder.to_owned().into(), window, cx)
                .max_lines(1)
                .on_submit(move |text, _, cx| {
                    deliver(
                        &this,
                        Event::Submit((submitted.clone(), text.to_owned())),
                        cx,
                    );
                })
        });
        let (changed, mut last) = (key.clone(), String::new());
        let watch = cx.observe(&field, move |view, field, cx| {
            let text = field.read(cx).text();
            if text != last {
                last.clone_from(&text);
                view.send(Event::Change((changed.clone(), text)), cx);
            }
        });
        self.inputs.insert(key, (field.clone(), watch));
        field
    }

    fn word(&mut self, word: &str) -> &'static str {
        if let Some(kept) = self.words.get(word) {
            return kept;
        }
        let kept: &'static str = Box::leak(word.to_owned().into_boxed_str());
        self.words.insert(word.to_owned(), kept);
        kept
    }

    fn columns(&mut self, titles: &[String]) -> &'static [Column] {
        if let Some((_, kept)) = self.columns.iter().find(|(known, _)| known == titles) {
            return kept;
        }
        let columns: Vec<Column> = titles
            .iter()
            .map(|title| Column {
                title: self.word(title),
                width: Width::Flex(1.0),
                kind: CellKind::Text,
            })
            .collect();
        let kept: &'static [Column] = Box::leak(columns.into_boxed_slice());
        self.columns.push((titles.to_vec(), kept));
        kept
    }
}
