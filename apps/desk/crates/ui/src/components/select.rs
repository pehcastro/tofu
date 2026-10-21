use std::cell::{Cell, RefCell};
use std::ops::Range;
use std::panic::Location;
use std::rc::Rc;
use std::time::Instant;

use gpui::{
    AnyElement, App, AppContext, Bounds, ClipboardItem, CursorStyle, Element, ElementId,
    FocusHandle, GlobalElementId, Hitbox, HitboxBehavior, Hsla, InspectorElementId, IntoElement,
    KeyDownEvent, LayoutId, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels,
    Point, SharedString, TextLayout, Window, fill, point, px,
};

pub struct Piece {
    text: SharedString,
    layout: TextLayout,
    swaps: Vec<(Range<usize>, SharedString)>,
    seen: Rc<Cell<bool>>,
}

impl Piece {
    pub fn new(
        text: SharedString,
        layout: TextLayout,
        swaps: Vec<(Range<usize>, SharedString)>,
    ) -> Self {
        Self {
            text,
            layout,
            swaps,
            seen: Rc::default(),
        }
    }

    pub fn track(&self, element: impl IntoElement) -> AnyElement {
        Tracked {
            seen: self.seen.clone(),
            child: element.into_any_element(),
        }
        .into_any_element()
    }

    fn index(&self, position: Point<Pixels>) -> usize {
        let bounds = self.layout.bounds();
        let y = position.y.min(bounds.bottom() - px(1.0)).max(bounds.top());
        let mut at = self
            .layout
            .index_for_position(point(position.x, y))
            .unwrap_or_else(|nearest| nearest)
            .min(self.text.len());
        while !self.text.is_char_boundary(at) {
            at -= 1;
        }
        at
    }

    fn copy(&self, range: Range<usize>, out: &mut String) {
        let mut from = range.start;
        for (swap, with) in &self.swaps {
            if swap.end <= range.start || swap.start >= range.end {
                continue;
            }
            out.push_str(
                self.text
                    .get(from..swap.start.max(from))
                    .unwrap_or_default(),
            );
            out.push_str(with);
            from = swap.end.min(range.end).max(from);
        }
        out.push_str(self.text.get(from..range.end).unwrap_or_default());
    }

    fn word(&self, at: usize) -> Range<usize> {
        let kind = |c: char| match c {
            c if c.is_alphanumeric() || c == '_' => 0,
            c if c.is_whitespace() => 1,
            _ => 2,
        };
        let (before, after) = self.text.split_at_checked(at).unwrap_or_default();
        let Some(under) = after.chars().next().or_else(|| before.chars().next_back()) else {
            return at..at;
        };
        let same = |(_, c): &(usize, char)| kind(*c) == kind(under);
        let start = before
            .char_indices()
            .rev()
            .take_while(same)
            .last()
            .map_or(at, |(ix, _)| ix);
        let end = after
            .char_indices()
            .find(|found| !same(found))
            .map_or(self.text.len(), |(ix, _)| at + ix);
        start..end
    }

    fn painted(&self, range: Range<usize>, fill_with: Hsla, window: &mut Window) {
        let (Some(from), Some(to)) = (
            self.layout.position_for_index(range.start),
            self.layout.position_for_index(range.end),
        ) else {
            return;
        };
        let bounds = self.layout.bounds();
        let line = self.layout.line_height();
        let rows = match from.y == to.y {
            true => vec![(from.x, to.x, from.y, line)],
            false => vec![
                (from.x, bounds.right(), from.y, line),
                (
                    bounds.left(),
                    bounds.right(),
                    from.y + line,
                    to.y - from.y - line,
                ),
                (bounds.left(), to.x, to.y, line),
            ],
        };
        for (left, right, top, height) in rows {
            if right > left && height > px(0.0) {
                window.paint_quad(fill(
                    Bounds::from_corners(point(left, top), point(right, top + height)),
                    fill_with,
                ));
            }
        }
    }
}

#[derive(Clone, Copy, Default, PartialEq, Eq, PartialOrd, Ord)]
struct Spot {
    piece: usize,
    at: usize,
}

pub struct Held {
    focus: FocusHandle,
    tail: Spot,
    head: Spot,
    dragging: bool,
    moved: bool,
}

impl Held {
    fn span(&self) -> (Spot, Spot) {
        (self.tail.min(self.head), self.tail.max(self.head))
    }

    fn pick(&mut self, from: Spot, to: Spot) {
        self.tail = from;
        self.head = to;
    }

    fn said(&self, pieces: &[Piece]) {
        if self.tail != self.head {
            let chars = selected(pieces, self.span()).chars().count();
            eprintln!("desk: select: {chars} chars");
        }
    }
}

fn spot(pieces: &[Piece], position: Point<Pixels>) -> Option<Spot> {
    let shown = || {
        pieces
            .iter()
            .enumerate()
            .filter(|(_, piece)| piece.seen.get())
    };
    if let Some((n, piece)) = shown().find(|(_, piece)| piece.layout.bounds().contains(&position)) {
        return Some(Spot {
            piece: n,
            at: piece.index(position),
        });
    }
    let mut before = None;
    for (n, piece) in shown() {
        let bounds = piece.layout.bounds();
        if position.y <= bounds.bottom() {
            return Some(match position.y >= bounds.top() {
                true => Spot {
                    piece: n,
                    at: piece.index(position),
                },
                false => before.unwrap_or(Spot { piece: n, at: 0 }),
            });
        }
        before = Some(Spot {
            piece: n,
            at: piece.text.len(),
        });
    }
    before
}

fn spans(
    pieces: &[Piece],
    (start, end): (Spot, Spot),
) -> impl Iterator<Item = (&Piece, Range<usize>)> {
    pieces
        .iter()
        .enumerate()
        .take(end.piece.saturating_add(1))
        .skip(start.piece)
        .map(move |(n, piece)| {
            let len = piece.text.len();
            let from = if n == start.piece {
                start.at.min(len)
            } else {
                0
            };
            let to = if n == end.piece { end.at.min(len) } else { len };
            (piece, from..to)
        })
}

fn selected(pieces: &[Piece], span: (Spot, Spot)) -> String {
    let mut out = String::new();
    for (n, (piece, range)) in spans(pieces, span).enumerate() {
        if n > 0 {
            out.push('\n');
        }
        piece.copy(range, &mut out);
    }
    out
}

struct Tracked {
    seen: Rc<Cell<bool>>,
    child: AnyElement,
}

impl IntoElement for Tracked {
    type Element = Self;

    fn into_element(self) -> Self {
        self
    }
}

impl Element for Tracked {
    type RequestLayoutState = ();
    type PrepaintState = ();

    fn id(&self) -> Option<ElementId> {
        None
    }

    fn source_location(&self) -> Option<&'static Location<'static>> {
        None
    }

    fn request_layout(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (LayoutId, ()) {
        (self.child.request_layout(window, cx), ())
    }

    fn prepaint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) {
        self.child.prepaint(window, cx);
        self.seen.set(true);
    }

    fn paint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) {
        self.child.paint(window, cx);
    }
}

pub struct Selectable {
    id: ElementId,
    child: AnyElement,
    pieces: Rc<[Piece]>,
    fill: Hsla,
    built: Instant,
}

pub fn selectable(
    id: impl Into<ElementId>,
    child: impl IntoElement,
    pieces: Vec<Piece>,
    fill: Hsla,
) -> Selectable {
    Selectable {
        id: id.into(),
        child: child.into_any_element(),
        pieces: pieces.into(),
        fill,
        built: Instant::now(),
    }
}

impl IntoElement for Selectable {
    type Element = Self;

    fn into_element(self) -> Self {
        self
    }
}

impl Element for Selectable {
    type RequestLayoutState = ();
    type PrepaintState = (Hitbox, Rc<RefCell<Held>>);

    fn id(&self) -> Option<ElementId> {
        Some(self.id.clone())
    }

    fn source_location(&self) -> Option<&'static Location<'static>> {
        None
    }

    fn request_layout(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (LayoutId, ()) {
        (self.child.request_layout(window, cx), ())
    }

    fn prepaint(
        &mut self,
        id: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        bounds: Bounds<Pixels>,
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) -> Self::PrepaintState {
        let hitbox = window.insert_hitbox(bounds, HitboxBehavior::Normal);
        self.child.prepaint(window, cx);
        let held = window.with_optional_element_state(id, |held, _| {
            let held = held.flatten().unwrap_or_else(|| {
                Rc::new(RefCell::new(Held {
                    focus: cx.focus_handle(),
                    tail: Spot::default(),
                    head: Spot::default(),
                    dragging: false,
                    moved: false,
                }))
            });
            (held.clone(), Some(held))
        });
        window.set_focus_handle(&held.borrow().focus, cx);
        (hitbox, held)
    }

    fn paint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        (hitbox, held): &mut Self::PrepaintState,
        window: &mut Window,
        cx: &mut App,
    ) {
        window.set_cursor_style(CursorStyle::IBeam, hitbox);
        let view = window.current_view();
        let (pieces, state) = (self.pieces.clone(), held.clone());
        window.on_key_event(move |event: &KeyDownEvent, phase, _, cx| {
            let keys = &event.keystroke;
            if !phase.bubble() || !keys.modifiers.secondary() {
                return;
            }
            let mut held = state.borrow_mut();
            match keys.key.as_str() {
                "c" if held.tail != held.head => {
                    let text = selected(&pieces, held.span());
                    eprintln!("desk: select: copied {} chars", text.chars().count());
                    cx.write_to_clipboard(ClipboardItem::new_string(text));
                }
                "a" => {
                    let last = pieces.len().saturating_sub(1);
                    let end = pieces.last().map_or(0, |piece| piece.text.len());
                    held.pick(
                        Spot::default(),
                        Spot {
                            piece: last,
                            at: end,
                        },
                    );
                    held.said(&pieces);
                    cx.notify(view);
                }
                _ => return,
            }
            cx.stop_propagation();
        });
        self.child.paint(window, cx);
        let span = held.borrow().span();
        if span.0 != span.1 {
            for (piece, range) in spans(&self.pieces, span) {
                if piece.seen.get() && range.start < range.end {
                    piece.painted(range, self.fill, window);
                }
            }
        }
        let (pieces, state, inside) = (self.pieces.clone(), held.clone(), hitbox.clone());
        window.on_mouse_event(move |event: &MouseDownEvent, phase, window, cx| {
            if event.button != MouseButton::Left {
                return;
            }
            let focus = {
                let mut held = state.borrow_mut();
                match (phase.bubble(), inside.is_hovered(window)) {
                    (true, true) => {
                        let Some(at) = spot(&pieces, event.position) else {
                            return;
                        };
                        eprintln!(
                            "desk: select: press click {} at piece {} byte {}",
                            event.click_count, at.piece, at.at
                        );
                        let whole = pieces.get(at.piece).map_or(0, |piece| piece.text.len());
                        match event.click_count {
                            0 | 1 if event.modifiers.shift => held.head = at,
                            0 | 1 => held.pick(at, at),
                            2 => {
                                let word = pieces
                                    .get(at.piece)
                                    .map_or(at.at..at.at, |piece| piece.word(at.at));
                                held.pick(
                                    Spot {
                                        at: word.start,
                                        ..at
                                    },
                                    Spot { at: word.end, ..at },
                                );
                            }
                            _ => held.pick(Spot { at: 0, ..at }, Spot { at: whole, ..at }),
                        }
                        held.dragging = event.click_count <= 1;
                        if !held.dragging {
                            held.said(&pieces);
                        }
                        held.focus.clone()
                    }
                    (false, false) if held.tail != held.head => {
                        held.head = held.tail;
                        cx.notify(view);
                        return;
                    }
                    _ => return,
                }
            };
            window.focus(&focus, cx);
            window.prevent_default();
            cx.notify(view);
        });
        let (pieces, state) = (self.pieces.clone(), held.clone());
        window.on_mouse_event(move |event: &MouseMoveEvent, phase, _, cx| {
            let mut held = state.borrow_mut();
            if !phase.bubble() || !held.dragging {
                return;
            }
            if let Some(at) = spot(&pieces, event.position)
                && at != held.head
            {
                held.head = at;
                held.moved = true;
                cx.notify(view);
            }
        });
        let (pieces, state) = (self.pieces.clone(), held.clone());
        window.on_mouse_event(move |event: &MouseUpEvent, phase, _, _| {
            let mut held = state.borrow_mut();
            if phase.bubble() && event.button == MouseButton::Left && held.dragging {
                held.dragging = false;
                held.said(&pieces);
            }
        });
        let mut held = held.borrow_mut();
        if held.moved {
            held.moved = false;
            eprintln!(
                "desk: select: frame {:.2}ms, {} pieces, {} bytes",
                self.built.elapsed().as_secs_f64() * 1e3,
                self.pieces.len(),
                self.pieces
                    .iter()
                    .map(|piece| piece.text.len())
                    .sum::<usize>()
            );
        }
    }
}
