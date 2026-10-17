use gpui::{
    AnyElement, App, Axis, ClickEvent, Div, ElementId, FontWeight, ScrollHandle, SharedString,
    Stateful, Window, div, prelude::*, px,
};

use crate::component::icon;
use crate::components::chip::tabular;
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink, pressed, ring};
use crate::components::scroll::scrollbar;
use crate::components::size::{
    CAPTION_TEXT, FONT_BODY, FONT_SMALL, HOVER, RADIUS_BADGE, RADIUS_LIST, T1, T2,
};
use crate::components::status_icon::{TicketStatus, status_icon};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{ICON_SMALL, TEXT_TINY};
use crate::theme::{ColorToken, Theme};

pub const COLUMN_WIDTH: f32 = 330.0;
const COLUMN_GAP: f32 = 10.0;
const COLUMN_PAD: f32 = 6.0;
const COLUMN_HEADER: f32 = 38.0;
const COLUMN_FILL: f32 = 0.025;
const CARD_GAP: f32 = 6.0;
const CARD_PAD: f32 = 11.0;
const CARD_FILL: f32 = 0.05;
const CARD_HOVER: f32 = 0.075;
const CARD_RING: f32 = 0.07;
const COLUMN_BUTTON: f32 = 22.0;
const STATUS_MARK: f32 = 14.0;
const TITLE_NUDGE: f32 = 2.0;

type OnClick = Box<dyn Fn(&ClickEvent, &mut Window, &mut App)>;

pub struct ColumnHeader {
    pub status: TicketStatus,
    pub title: SharedString,
    pub count: usize,
}

#[derive(IntoElement)]
pub struct BoardColumn {
    id: ElementId,
    header: ColumnHeader,
    children: Vec<AnyElement>,
    on_add: Option<OnClick>,
    on_more: Option<OnClick>,
}

pub fn board_column(
    id: impl Into<ElementId>,
    header: ColumnHeader,
    children: impl IntoIterator<Item = impl IntoElement>,
) -> BoardColumn {
    BoardColumn {
        id: id.into(),
        header,
        children: children
            .into_iter()
            .map(IntoElement::into_any_element)
            .collect(),
        on_add: None,
        on_more: None,
    }
}

impl BoardColumn {
    pub fn on_add(mut self, on_add: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static) -> Self {
        self.on_add = Some(Box::new(on_add));
        self
    }

    pub fn on_more(
        mut self,
        on_more: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
    ) -> Self {
        self.on_more = Some(Box::new(on_more));
        self
    }
}

fn column_button(
    id: &'static str,
    label: &'static str,
    mark: impl IntoElement,
    on_click: OnClick,
    theme: &Theme,
) -> Stateful<Div> {
    let hover = ink(theme, HOVER);
    pressed(
        div()
            .id(id)
            .aria_label(label)
            .flex()
            .flex_none()
            .items_center()
            .justify_center()
            .size(px(COLUMN_BUTTON))
            .rounded(px(RADIUS_BADGE))
            .cursor_pointer()
            .hover(move |style| style.bg(hover))
            .on_click(on_click),
        theme.color(ColorToken::CardsInnerFill),
    )
    .child(mark)
}

impl RenderOnce for BoardColumn {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let handle = window
            .use_keyed_state(ElementId::from((self.id.clone(), "scroll")), cx, |_, _| {
                ScrollHandle::new()
            })
            .read(cx)
            .clone();
        let mark = ink(&theme, T2);
        let header = div()
            .flex()
            .flex_none()
            .items_center()
            .gap_2()
            .h(px(COLUMN_HEADER))
            .px(px(CARD_PAD))
            .child(status_icon(self.header.status, px(STATUS_MARK)))
            .child(
                div()
                    .min_w_0()
                    .truncate()
                    .text_size(px(FONT_BODY))
                    .font_weight(FontWeight::MEDIUM)
                    .text_color(ink(&theme, T1))
                    .child(self.header.title),
            )
            .child(
                div()
                    .flex_none()
                    .text_size(px(FONT_SMALL))
                    .font_features(tabular())
                    .text_color(ink(&theme, CAPTION_TEXT))
                    .child(self.header.count.to_string()),
            )
            .child(div().flex_1())
            .children(self.on_more.map(|on_more| {
                column_button(
                    "column-more",
                    "More",
                    glyph(Glyph::Ellipsis, ICON_SMALL, mark),
                    on_more,
                    &theme,
                )
            }))
            .children(self.on_add.map(|on_add| {
                column_button(
                    "column-add",
                    "Add",
                    icon(Icon::Plus, ICON_SMALL, mark),
                    on_add,
                    &theme,
                )
            }));
        div()
            .id(self.id)
            .flex()
            .flex_col()
            .flex_none()
            .w(px(COLUMN_WIDTH))
            .h_full()
            .rounded(px(RADIUS_LIST))
            .bg(ink(&theme, COLUMN_FILL))
            .child(header)
            .child(
                div()
                    .relative()
                    .flex_1()
                    .min_h_0()
                    .child(
                        div()
                            .id("cards")
                            .size_full()
                            .overflow_y_scroll()
                            .restrict_scroll_to_axis()
                            .track_scroll(&handle)
                            .flex()
                            .flex_col()
                            .gap(px(CARD_GAP))
                            .px(px(COLUMN_PAD))
                            .pb(px(COLUMN_PAD))
                            .children(self.children),
                    )
                    .child(scrollbar("cards-bar", &handle)),
            )
    }
}

#[derive(IntoElement)]
pub struct BoardCard {
    id: ElementId,
    id_line: Option<AnyElement>,
    status: Option<TicketStatus>,
    title: SharedString,
    chips: Vec<AnyElement>,
    footer: Option<SharedString>,
    avatar: Option<AnyElement>,
    selected: bool,
    on_click: Option<OnClick>,
}

pub fn board_card(id: impl Into<ElementId>) -> BoardCard {
    BoardCard {
        id: id.into(),
        id_line: None,
        status: None,
        title: SharedString::default(),
        chips: Vec::new(),
        footer: None,
        avatar: None,
        selected: false,
        on_click: None,
    }
}

impl BoardCard {
    pub fn id_line(mut self, line: impl IntoElement) -> Self {
        self.id_line = Some(line.into_any_element());
        self
    }

    pub fn status(mut self, status: TicketStatus) -> Self {
        self.status = Some(status);
        self
    }

    pub fn title(mut self, title: impl Into<SharedString>) -> Self {
        self.title = title.into();
        self
    }

    pub fn chips(mut self, chips: impl IntoIterator<Item = impl IntoElement>) -> Self {
        self.chips
            .extend(chips.into_iter().map(IntoElement::into_any_element));
        self
    }

    pub fn footer(mut self, footer: impl Into<SharedString>) -> Self {
        self.footer = Some(footer.into());
        self
    }

    pub fn avatar(mut self, avatar: impl IntoElement) -> Self {
        self.avatar = Some(avatar.into_any_element());
        self
    }

    pub fn selected(mut self, selected: bool) -> Self {
        self.selected = selected;
        self
    }

    pub fn on_click(
        mut self,
        on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
    ) -> Self {
        self.on_click = Some(Box::new(on_click));
        self
    }
}

impl RenderOnce for BoardCard {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let backdrop = theme.color(ColorToken::CardsInnerFill);
        let hover = ink(&theme, CARD_HOVER);
        let edge = if self.selected {
            theme.color(ColorToken::FocusRing)
        } else {
            ink(&theme, CARD_RING)
        };
        let card = div()
            .id(self.id)
            .flex()
            .flex_col()
            .flex_none()
            .gap_2()
            .p(px(CARD_PAD))
            .rounded(px(RADIUS_LIST))
            .cursor_pointer()
            .bg(ink(&theme, CARD_FILL))
            .shadow(vec![ring(edge)])
            .hover(move |style| style.bg(hover))
            .when_some(self.on_click, |card, on_click| card.on_click(on_click))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .text_size(px(FONT_SMALL))
                    .font_features(tabular())
                    .text_color(ink(&theme, CAPTION_TEXT))
                    .child(div().flex_1().min_w_0().truncate().children(self.id_line))
                    .children(self.avatar),
            )
            .child(
                div()
                    .flex()
                    .items_start()
                    .gap_2()
                    .children(self.status.map(|status| {
                        div()
                            .flex_none()
                            .pt(px(TITLE_NUDGE))
                            .child(status_icon(status, px(STATUS_MARK)))
                    }))
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .text_size(px(FONT_BODY))
                            .font_weight(FontWeight::MEDIUM)
                            .text_color(ink(&theme, T1))
                            .line_clamp(2)
                            .text_ellipsis()
                            .child(self.title),
                    ),
            )
            .when(!self.chips.is_empty(), |card| {
                card.child(div().flex().flex_wrap().gap_1().children(self.chips))
            })
            .children(self.footer.map(|footer| {
                div()
                    .text_size(px(TEXT_TINY))
                    .text_color(ink(&theme, CAPTION_TEXT))
                    .child(footer)
            }));
        pressed(card, backdrop)
    }
}

#[derive(IntoElement)]
pub struct Board {
    id: ElementId,
    columns: Vec<AnyElement>,
}

pub fn board(
    id: impl Into<ElementId>,
    columns: impl IntoIterator<Item = impl IntoElement>,
) -> Board {
    Board {
        id: id.into(),
        columns: columns
            .into_iter()
            .map(IntoElement::into_any_element)
            .collect(),
    }
}

impl RenderOnce for Board {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let handle = window
            .use_keyed_state(ElementId::from((self.id.clone(), "scroll")), cx, |_, _| {
                ScrollHandle::new()
            })
            .read(cx)
            .clone();
        let bar = ElementId::from((self.id.clone(), "bar"));
        div()
            .relative()
            .size_full()
            .min_w_0()
            .child(
                div()
                    .id(self.id)
                    .size_full()
                    .overflow_x_scroll()
                    .restrict_scroll_to_axis()
                    .track_scroll(&handle)
                    .flex()
                    .gap(px(COLUMN_GAP))
                    .children(self.columns),
            )
            .child(scrollbar(bar, &handle).axis(Axis::Horizontal))
    }
}
