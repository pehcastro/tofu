use std::cell::RefCell;
use std::rc::Rc;

use desk_motion::GlideKind;
use gpui::{
    AnchoredPositionMode, AnyElement, App, Bounds, ClickEvent, Context, Corners, Div, Entity,
    FocusHandle, Focusable, KeyDownEvent, MouseButton, MouseDownEvent, MouseMoveEvent, Pixels,
    ScrollHandle, SharedString, Stateful, Window, anchored, deferred, div, point, prelude::*, px,
};

use crate::component::icon;
use crate::components::card::caption;
use crate::components::chip::kbd;
use crate::components::form::TextArea;
use crate::components::glyph::Glyph;
use crate::components::list::{HoverList, HoverVariant, Marker, bare_row, separator};
use crate::components::overlay::menu_surface;
use crate::components::paint::{glyph, ink};
use crate::components::scroll::ScrollArea;
use crate::components::size::{
    FIELD, FONT_SMALL, GROUP_PAD_BOTTOM, GROUP_PAD_TOP, HOVER, LINE_CAP, MENU_PAD, RADIUS_POP,
    RADIUS_ROW, ROW_GAP, ROW_PAD_X, ROW_PAD_Y, T2, T3,
};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{ICON_SMALL, ICON_TINY};
use crate::theme::Theme;

const META_GAP: f32 = 6.0;
const META_DOT: &str = "·";
const PALETTE_WIDTH: f32 = 520.0;
const PALETTE_TOP: f32 = 96.0;
const PALETTE_LIST: f32 = 400.0;
const PLACEHOLDER: &str = "Type a command or search";
const NOTHING_FOUND: &str = "No matching commands";

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct PaletteItem {
    pub id: SharedString,
    pub label: SharedString,
    pub group: SharedString,
    pub keys: Option<SharedString>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum PaletteMeta {
    Branch(SharedString),
    Text(SharedString),
}

impl PaletteMeta {
    fn text(&self) -> &SharedString {
        match self {
            PaletteMeta::Branch(text) | PaletteMeta::Text(text) => text,
        }
    }
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct PaletteDetail {
    pub below: SharedString,
    pub meta: Vec<PaletteMeta>,
    pub checked: bool,
    pub dim: bool,
}

pub type PaletteEntry = (PaletteItem, Option<PaletteDetail>);

type OnPick = Rc<dyn Fn(&SharedString, &mut Window, &mut App)>;
type OnClose = Rc<dyn Fn(&mut Window, &mut App)>;

pub struct Palette {
    items: Vec<PaletteEntry>,
    field: Entity<TextArea>,
    query: String,
    highlighted: usize,
    open: bool,
    return_focus: Option<FocusHandle>,
    on_pick: Option<OnPick>,
    on_close: Option<OnClose>,
    scroll: ScrollHandle,
    row_spans: Rc<RefCell<Vec<Bounds<Pixels>>>>,
}

impl Palette {
    pub fn new(items: Vec<PaletteItem>, window: &mut Window, cx: &mut App) -> Entity<Palette> {
        Self::detailed(
            items.into_iter().map(|item| (item, None)).collect(),
            window,
            cx,
        )
    }

    pub fn detailed(
        items: Vec<PaletteEntry>,
        window: &mut Window,
        cx: &mut App,
    ) -> Entity<Palette> {
        cx.new(|cx| {
            let field = cx.new(|cx| {
                TextArea::new(PLACEHOLDER.into(), window, cx)
                    .bare()
                    .max_lines(1)
            });
            cx.observe(&field, |palette: &mut Palette, field, cx| {
                let query = field.read(cx).text();
                if query != palette.query {
                    palette.query = query;
                    palette.highlighted = 0;
                    palette.scroll.set_offset(point(px(0.0), px(0.0)));
                    cx.notify();
                }
            })
            .detach();
            Palette {
                items,
                field,
                query: String::new(),
                highlighted: 0,
                open: false,
                return_focus: None,
                on_pick: None,
                on_close: None,
                scroll: ScrollHandle::new(),
                row_spans: Rc::default(),
            }
        })
    }

    pub fn replace(&mut self, items: Vec<PaletteEntry>, cx: &mut Context<Self>) {
        self.items = items;
        self.highlighted = self.highlighted.min(self.matched().len().saturating_sub(1));
        cx.notify();
    }

    pub fn on_pick(&mut self, on_pick: impl Fn(&SharedString, &mut Window, &mut App) + 'static) {
        self.on_pick = Some(Rc::new(on_pick));
    }

    pub fn on_close(&mut self, on_close: impl Fn(&mut Window, &mut App) + 'static) {
        self.on_close = Some(Rc::new(on_close));
    }

    pub fn open(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if !self.open {
            self.open = true;
            self.highlighted = 0;
            self.scroll.set_offset(point(px(0.0), px(0.0)));
            self.query.clear();
            self.field.update(cx, |field, cx| field.clear(cx));
            self.return_focus = window.focused(cx);
        }
        let field = self.field.read(cx).focus_handle(cx);
        window.focus(&field, cx);
        cx.notify();
    }

    fn close(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.open = false;
        if let Some(before) = self.return_focus.take() {
            window.focus(&before, cx);
        }
        cx.notify();
    }

    fn dismiss(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.close(window, cx);
        if let Some(on_close) = self.on_close.clone() {
            window.defer(cx, move |window, cx| on_close(window, cx));
        }
    }

    fn pick(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        let Some(id) = self.matched().get(at).map(|(item, _)| item.id.clone()) else {
            return;
        };
        self.close(window, cx);
        if let Some(on_pick) = self.on_pick.clone() {
            window.defer(cx, move |window, cx| on_pick(&id, window, cx));
        }
    }

    fn matched(&self) -> Vec<&PaletteEntry> {
        let query = self.query.trim().to_lowercase();
        let found = |text: &SharedString| text.to_lowercase().contains(&query);
        let mut groups: Vec<&SharedString> = Vec::new();
        for (item, _) in &self.items {
            if !groups.contains(&&item.group) {
                groups.push(&item.group);
            }
        }
        groups
            .into_iter()
            .flat_map(|group| {
                self.items
                    .iter()
                    .filter(|(item, _)| item.group == *group)
                    .filter(|(item, detail)| {
                        found(&item.label)
                            || detail.as_ref().is_some_and(|detail| {
                                found(&detail.below)
                                    || detail.meta.iter().any(|meta| found(meta.text()))
                            })
                    })
            })
            .collect()
    }

    fn key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let count = self.matched().len();
        match event.keystroke.key.as_str() {
            "down" if count > 0 => self.highlighted = (self.highlighted + 1) % count,
            "up" if count > 0 => {
                self.highlighted = self.highlighted.checked_sub(1).unwrap_or(count - 1)
            }
            "enter" => self.pick(self.highlighted, window, cx),
            "escape" => self.dismiss(window, cx),
            _ => return,
        }
        self.reveal_highlighted();
        cx.stop_propagation();
        cx.notify();
    }

    fn reveal_highlighted(&self) {
        let Some(span) = self.row_spans.borrow().get(self.highlighted).copied() else {
            return;
        };
        let view = self.scroll.bounds();
        let offset = self.scroll.offset();
        let max = self.scroll.max_offset().y;
        let y = if self.highlighted == 0 {
            px(0.0)
        } else if span.bottom() + offset.y > view.bottom() {
            view.bottom() - span.bottom()
        } else if span.top() + offset.y < view.top() {
            view.top() - span.top()
        } else {
            return;
        };
        self.scroll
            .set_offset(point(offset.x, y.clamp(-max, px(0.0))));
    }

    fn row(
        &self,
        at: usize,
        (item, detail): &PaletteEntry,
        theme: &Theme,
        cx: &Context<Self>,
    ) -> Stateful<Div> {
        let spans = self.row_spans.clone();
        let scroll = self.scroll.clone();
        bare_row(("palette-row", at), false, false, theme)
            .on_prepaint(move |prepaint, _, _| {
                let mut spans = spans.borrow_mut();
                if spans.len() <= at {
                    spans.resize(at + 1, Bounds::default());
                }
                if let Some(span) = spans.get_mut(at) {
                    *span = prepaint.bounds - point(px(0.0), scroll.offset().y);
                }
            })
            .on_mouse_move(cx.listener(move |palette, _: &MouseMoveEvent, _, cx| {
                if palette.highlighted != at {
                    palette.highlighted = at;
                    cx.notify();
                }
            }))
            .on_click(cx.listener(move |palette, _: &ClickEvent, window, cx| {
                palette.pick(at, window, cx);
            }))
            .map(|row| match detail {
                None => row.child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .truncate()
                        .child(item.label.clone()),
                ),
                Some(detail) => row.child(detailed(&item.label, detail, theme)),
            })
            .children(item.keys.clone().map(|keys| kbd(keys, theme)))
    }

    fn rows(&self, theme: &Theme, cx: &Context<Self>) -> AnyElement {
        let matched = self.matched();
        if matched.is_empty() {
            return div()
                .px(px(ROW_PAD_X))
                .py(px(ROW_PAD_Y))
                .text_color(ink(theme, T3))
                .child(NOTHING_FOUND)
                .into_any_element();
        }
        let fill = ink(theme, HOVER);
        let corners = Corners::all(px(RADIUS_ROW));
        let mut list = HoverList::within(
            "palette-rows",
            div().flex().flex_col(),
            fill,
            corners,
            theme,
        )
        .variant(HoverVariant::Snap);
        let mut group = None;
        let mut marker = None;
        let mut entries = 0;
        for (at, entry) in matched.into_iter().enumerate() {
            let item = &entry.0;
            if group != Some(&item.group) {
                group = Some(&item.group);
                list = list.inert(
                    caption(item.group.clone(), theme)
                        .px(px(ROW_PAD_X))
                        .pt(px(GROUP_PAD_TOP))
                        .pb(px(GROUP_PAD_BOTTOM)),
                );
                entries += 1;
            }
            if at == self.highlighted {
                marker = Some(Marker {
                    at: entries,
                    kind: GlideKind::Eased,
                    fill,
                    corners,
                });
            }
            list = list.item(self.row(at, entry, theme, cx));
            entries += 1;
        }
        list.keyed(marker).into_any_element()
    }
}

impl Render for Palette {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        if !self.open {
            return div().into_any_element();
        }
        let theme = ActiveTheme::theme(cx);
        let panel = menu_surface(&theme)
            .id("palette")
            .w(px(PALETTE_WIDTH))
            .rounded(px(RADIUS_POP))
            .overflow_hidden()
            .capture_key_down(cx.listener(Self::key))
            .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .child(
                div()
                    .flex()
                    .items_center()
                    .h(px(FIELD))
                    .mx(px(MENU_PAD))
                    .mt(px(MENU_PAD))
                    .px(px(ROW_PAD_X))
                    .child(self.field.clone()),
            )
            .child(separator(&theme).my(px(MENU_PAD)))
            .child(
                ScrollArea::new("palette-scroll")
                    .max_h(PALETTE_LIST)
                    .track(&self.scroll)
                    .child(
                        div()
                            .px(px(MENU_PAD))
                            .pb(px(MENU_PAD))
                            .child(self.rows(&theme, cx)),
                    ),
            );
        let viewport = window.viewport_size();
        let layer = div()
            .id("palette-layer")
            .occlude()
            .w(viewport.width)
            .h(viewport.height)
            .flex()
            .flex_col()
            .items_center()
            .pt(px(PALETTE_TOP))
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|palette, _: &MouseDownEvent, window, cx| palette.dismiss(window, cx)),
            )
            .child(panel);
        deferred(
            anchored()
                .position_mode(AnchoredPositionMode::Window)
                .position(point(px(0.0), px(0.0)))
                .child(layer),
        )
        .into_any_element()
    }
}

fn detailed(label: &SharedString, detail: &PaletteDetail, theme: &Theme) -> Div {
    let dim = ink(theme, T3);
    let meta = detail.meta.iter().enumerate().map(|(at, meta)| {
        div()
            .flex()
            .items_center()
            .gap(px(META_GAP))
            .when(at > 0, |part| part.child(META_DOT))
            .when(matches!(meta, PaletteMeta::Branch(_)), |part| {
                part.child(icon(Icon::Branch, ICON_TINY, dim))
            })
            .child(meta.text().clone())
    });
    div()
        .flex_1()
        .min_w_0()
        .flex()
        .flex_col()
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(ROW_GAP))
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .truncate()
                        .when(detail.dim, |name| name.text_color(dim))
                        .child(label.clone()),
                )
                .child(
                    div()
                        .flex()
                        .flex_none()
                        .items_center()
                        .gap(px(META_GAP))
                        .text_size(px(FONT_SMALL))
                        .text_color(dim)
                        .children(meta),
                )
                .child(
                    div()
                        .flex_none()
                        .size(px(ICON_SMALL))
                        .when(detail.checked, |mark| {
                            mark.child(glyph(Glyph::Check, ICON_SMALL, ink(theme, T2)))
                        }),
                ),
        )
        .child(
            div()
                .min_w_0()
                .overflow_hidden()
                .whitespace_nowrap()
                .text_ellipsis_middle()
                .text_size(px(FONT_SMALL))
                .line_height(px(LINE_CAP))
                .text_color(dim)
                .child(detail.below.clone()),
        )
}
