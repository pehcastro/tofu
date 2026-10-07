use std::rc::Rc;

use desk_motion::GlideKind;
use gpui::{
    AnchoredPositionMode, AnyElement, App, ClickEvent, Context, Corners, Div, Entity, FocusHandle,
    Focusable, KeyDownEvent, MouseButton, MouseDownEvent, MouseMoveEvent, SharedString, Stateful,
    Window, anchored, deferred, div, point, prelude::*, px,
};

use crate::components::card::caption;
use crate::components::chip::kbd;
use crate::components::form::TextArea;
use crate::components::list::{HoverList, HoverVariant, Marker, bare_row, separator};
use crate::components::overlay::menu_surface;
use crate::components::paint::ink;
use crate::components::scroll::ScrollArea;
use crate::components::size::{
    FIELD, GROUP_PAD_BOTTOM, GROUP_PAD_TOP, HOVER, MENU_PAD, RADIUS_POP, RADIUS_ROW, ROW_PAD_X,
    ROW_PAD_Y, T3,
};
use crate::live::ActiveTheme;
use crate::theme::Theme;

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

type OnPick = Rc<dyn Fn(&SharedString, &mut Window, &mut App)>;
type OnClose = Rc<dyn Fn(&mut Window, &mut App)>;

pub struct Palette {
    items: Vec<PaletteItem>,
    field: Entity<TextArea>,
    query: String,
    highlighted: usize,
    open: bool,
    return_focus: Option<FocusHandle>,
    on_pick: Option<OnPick>,
    on_close: Option<OnClose>,
}

impl Palette {
    pub fn new(items: Vec<PaletteItem>, window: &mut Window, cx: &mut App) -> Entity<Palette> {
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
            }
        })
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
        let Some(id) = self.matched().get(at).map(|item| item.id.clone()) else {
            return;
        };
        self.close(window, cx);
        if let Some(on_pick) = self.on_pick.clone() {
            window.defer(cx, move |window, cx| on_pick(&id, window, cx));
        }
    }

    fn matched(&self) -> Vec<&PaletteItem> {
        let query = self.query.trim().to_lowercase();
        let mut groups: Vec<&SharedString> = Vec::new();
        for item in &self.items {
            if !groups.contains(&&item.group) {
                groups.push(&item.group);
            }
        }
        groups
            .into_iter()
            .flat_map(|group| {
                self.items
                    .iter()
                    .filter(|item| item.group == *group)
                    .filter(|item| item.label.to_lowercase().contains(&query))
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
        cx.stop_propagation();
        cx.notify();
    }

    fn row(at: usize, item: &PaletteItem, theme: &Theme, cx: &Context<Self>) -> Stateful<Div> {
        bare_row(("palette-row", at), false, false, theme)
            .on_mouse_move(cx.listener(move |palette, _: &MouseMoveEvent, _, cx| {
                if palette.highlighted != at {
                    palette.highlighted = at;
                    cx.notify();
                }
            }))
            .on_click(cx.listener(move |palette, _: &ClickEvent, window, cx| {
                palette.pick(at, window, cx);
            }))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .child(item.label.clone()),
            )
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
        for (at, item) in matched.into_iter().enumerate() {
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
            list = list.item(Self::row(at, item, theme, cx));
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
                ScrollArea::new("palette-scroll").max_h(PALETTE_LIST).child(
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
