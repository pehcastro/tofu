use std::f32::consts::FRAC_PI_2;

use gpui::{
    App, ClickEvent, Div, ElementId, Entity, FontWeight, SharedString, Stateful, Transformation,
    Window, div, prelude::*, px, radians,
};

use crate::components::chip::tabular;
use crate::components::glyph::Glyph;
use crate::components::list::HoverList;
use crate::components::paint::{glyph, ink};
use crate::components::size::{
    CAPTION_TEXT, FONT_SMALL, HOVER_TEXT, RADIUS_ROW, ROW_ON, ROW_PAD_X, T1, T2,
};
use crate::components::status_icon::{TicketStatus, status_icon};
use crate::live::ActiveTheme;
use crate::metrics::{CONTROL, ICON, ICON_SMALL, ICON_TINY, SIDEBAR_WIDTH, TEXT};
use crate::theme::{ColorToken, Theme};

pub const NAV_INDENT: f32 = 16.0;
const NAV_DOT: f32 = 6.0;
const GROUP_GAP: f32 = 10.0;

type OnNav = Box<dyn Fn(&ClickEvent, &mut Window, &mut App)>;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum NavIcon {
    Glyph(Glyph),
    Status(TicketStatus),
}

impl From<Glyph> for NavIcon {
    fn from(glyph: Glyph) -> Self {
        NavIcon::Glyph(glyph)
    }
}

impl From<TicketStatus> for NavIcon {
    fn from(status: TicketStatus) -> Self {
        NavIcon::Status(status)
    }
}

struct NavItem {
    icon: NavIcon,
    label: SharedString,
    count: Option<usize>,
    dot: bool,
    selected: bool,
    on_click: OnNav,
}

enum NavEntry {
    Item(NavItem),
    Group {
        title: SharedString,
        collapsed: bool,
        children: NavSidebar,
    },
}

#[derive(IntoElement)]
pub struct NavSidebar {
    id: ElementId,
    entries: Vec<NavEntry>,
}

impl NavSidebar {
    pub fn new(id: impl Into<ElementId>) -> Self {
        NavSidebar {
            id: id.into(),
            entries: Vec::new(),
        }
    }

    pub fn item(
        mut self,
        icon: impl Into<NavIcon>,
        label: impl Into<SharedString>,
        count: Option<usize>,
        dot: bool,
        selected: bool,
        on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
    ) -> Self {
        self.entries.push(NavEntry::Item(NavItem {
            icon: icon.into(),
            label: label.into(),
            count,
            dot,
            selected,
            on_click: Box::new(on_click),
        }));
        self
    }

    pub fn group(
        mut self,
        title: impl Into<SharedString>,
        collapsed: bool,
        children: NavSidebar,
    ) -> Self {
        self.entries.push(NavEntry::Group {
            title: title.into(),
            collapsed,
            children,
        });
        self
    }
}

impl RenderOnce for NavSidebar {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let mut rows = Vec::new();
        let mut selected = None;
        let scope = self.id.clone();
        let mut pending: Vec<(usize, ElementId, NavEntry)> = self
            .entries
            .into_iter()
            .rev()
            .map(|entry| (0, scope.clone(), entry))
            .collect();
        while let Some((depth, scope, entry)) = pending.pop() {
            match entry {
                NavEntry::Item(item) => {
                    if item.selected {
                        selected = Some(rows.len());
                    }
                    let id = ElementId::from((scope, item.label.clone()));
                    rows.push(item_row(id, depth, item, &theme));
                }
                NavEntry::Group {
                    title,
                    collapsed,
                    children,
                } => {
                    let id = ElementId::from((scope, title.clone()));
                    let fold = window.use_keyed_state(ElementId::from((id.clone(), "fold")), cx, {
                        move |_, _| collapsed
                    });
                    let shut = *fold.read(cx);
                    rows.push(group_row(id.clone(), depth, title, shut, fold, &theme));
                    if !shut {
                        let inner = depth.saturating_add(1);
                        pending.extend(
                            children
                                .entries
                                .into_iter()
                                .rev()
                                .map(|entry| (inner, id.clone(), entry)),
                        );
                    }
                }
            }
        }
        div()
            .w(px(SIDEBAR_WIDTH))
            .flex_none()
            .flex()
            .flex_col()
            .text_size(px(TEXT))
            .child(
                HoverList::new(self.id, &theme)
                    .items(rows)
                    .selected(selected),
            )
    }
}

fn indent(depth: usize) -> f32 {
    ROW_PAD_X + NAV_INDENT * depth as f32
}

fn nav_row(id: ElementId, depth: usize) -> Stateful<Div> {
    div()
        .id(id)
        .flex()
        .flex_none()
        .items_center()
        .h(px(CONTROL))
        .pl(px(indent(depth)))
        .pr(px(ROW_PAD_X))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
}

fn item_row(id: ElementId, depth: usize, item: NavItem, theme: &Theme) -> Stateful<Div> {
    let mark = match item.icon {
        NavIcon::Glyph(shown) => glyph(shown, ICON, ink(theme, T2)).into_any_element(),
        NavIcon::Status(status) => status_icon(status, px(ICON_SMALL + 1.0)).into_any_element(),
    };
    nav_row(id, depth)
        .gap_2()
        .text_color(ink(theme, if item.selected { T1 } else { HOVER_TEXT }))
        .when(item.selected, |row| row.bg(ink(theme, ROW_ON)))
        .on_click(item.on_click)
        .child(
            div()
                .flex()
                .flex_none()
                .w(px(ICON))
                .justify_center()
                .child(mark),
        )
        .child(div().flex_1().min_w_0().truncate().child(item.label))
        .when(item.dot, |row| {
            row.child(
                div()
                    .flex_none()
                    .size(px(NAV_DOT))
                    .rounded_full()
                    .bg(theme.color(ColorToken::StatusLive)),
            )
        })
        .children(item.count.map(|count| {
            div()
                .flex_none()
                .text_size(px(FONT_SMALL))
                .font_features(tabular())
                .text_color(ink(theme, CAPTION_TEXT))
                .child(count.to_string())
        }))
}

fn group_row(
    id: ElementId,
    depth: usize,
    title: SharedString,
    shut: bool,
    fold: Entity<bool>,
    theme: &Theme,
) -> Stateful<Div> {
    let turn = if shut { -FRAC_PI_2 } else { 0.0 };
    nav_row(id, depth)
        .gap_1()
        .when(depth == 0, |row| row.mt(px(GROUP_GAP)))
        .text_size(px(FONT_SMALL))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, CAPTION_TEXT))
        .on_click(move |_, _, cx| {
            fold.update(cx, |shut, cx| {
                *shut = !*shut;
                cx.notify();
            })
        })
        .child(div().min_w_0().truncate().child(title))
        .child(
            glyph(Glyph::Chevron, ICON_TINY, ink(theme, CAPTION_TEXT))
                .with_transformation(Transformation::rotate(radians(turn))),
        )
}
