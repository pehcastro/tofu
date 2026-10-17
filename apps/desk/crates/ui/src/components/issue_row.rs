use std::f32::consts::FRAC_PI_2;

use gpui::{
    AnyElement, App, ClickEvent, ElementId, FontWeight, SharedString, Transformation, Window, div,
    prelude::*, px, radians,
};

use crate::component::icon;
use crate::components::chip::tabular;
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink, pressed};
use crate::components::priority_icon::{Priority, priority_icon};
use crate::components::size::{
    CAPTION_TEXT, FONT_BODY, FONT_SMALL, HOVER, LROW_PAD_X, RADIUS_BADGE, RADIUS_ROW, ROW_GAP,
    ROW_ON, T1, T2,
};
use crate::components::status_icon::{TicketStatus, status_icon};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{ICON_SMALL, ICON_TINY};
use crate::theme::ColorToken;

const ISSUE_ROW: f32 = 40.0;
const GROUP_ROW: f32 = 34.0;
const GROUP_FILL: f32 = 0.035;
const TICKET_WIDTH: f32 = 76.0;
const DATE_WIDTH: f32 = 52.0;
const STATUS_MARK: f32 = 14.0;
const ADD_BUTTON: f32 = 22.0;

type OnClick = Box<dyn Fn(&ClickEvent, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct IssueRow {
    id: ElementId,
    priority: Option<Priority>,
    ticket: Option<SharedString>,
    status: Option<TicketStatus>,
    age: Option<SharedString>,
    title: SharedString,
    chips: Vec<AnyElement>,
    avatar: Option<AnyElement>,
    date: Option<SharedString>,
    selected: bool,
    on_click: Option<OnClick>,
}

pub fn issue_row(id: impl Into<ElementId>) -> IssueRow {
    IssueRow {
        id: id.into(),
        priority: None,
        ticket: None,
        status: None,
        age: None,
        title: SharedString::default(),
        chips: Vec::new(),
        avatar: None,
        date: None,
        selected: false,
        on_click: None,
    }
}

impl IssueRow {
    pub fn priority(mut self, priority: Priority) -> Self {
        self.priority = Some(priority);
        self
    }

    pub fn ticket(mut self, ticket: impl Into<SharedString>) -> Self {
        self.ticket = Some(ticket.into());
        self
    }

    pub fn status(mut self, status: TicketStatus) -> Self {
        self.status = Some(status);
        self
    }

    pub fn age(mut self, age: impl Into<SharedString>) -> Self {
        self.age = Some(age.into());
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

    pub fn avatar(mut self, avatar: impl IntoElement) -> Self {
        self.avatar = Some(avatar.into_any_element());
        self
    }

    pub fn date(mut self, date: impl Into<SharedString>) -> Self {
        self.date = Some(date.into());
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

impl RenderOnce for IssueRow {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let hover = ink(&theme, HOVER);
        let muted = ink(&theme, CAPTION_TEXT);
        let row = div()
            .id(self.id)
            .w_full()
            .h(px(ISSUE_ROW))
            .flex()
            .flex_none()
            .items_center()
            .gap(px(ROW_GAP))
            .px(px(LROW_PAD_X))
            .rounded(px(RADIUS_ROW))
            .cursor_pointer()
            .text_size(px(FONT_BODY))
            .when(self.selected, |row| row.bg(ink(&theme, ROW_ON)))
            .when(!self.selected, |row| {
                row.hover(move |style| style.bg(hover))
            })
            .when_some(self.on_click, |row, on_click| row.on_click(on_click))
            .children(self.priority.map(priority_icon))
            .child(
                div()
                    .w(px(TICKET_WIDTH))
                    .flex_none()
                    .truncate()
                    .text_size(px(FONT_SMALL))
                    .font_features(tabular())
                    .text_color(muted)
                    .children(self.ticket),
            )
            .children(
                self.status
                    .map(|status| status_icon(status, px(STATUS_MARK))),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .items_center()
                    .gap_2()
                    .children(self.age.map(|age| {
                        div()
                            .flex_none()
                            .text_size(px(FONT_SMALL))
                            .text_color(muted)
                            .child(age)
                    }))
                    .child(
                        div()
                            .min_w_0()
                            .truncate()
                            .font_weight(FontWeight::MEDIUM)
                            .text_color(ink(&theme, T1))
                            .child(self.title),
                    ),
            )
            .when(!self.chips.is_empty(), |row| {
                row.child(div().flex().flex_none().gap_1().children(self.chips))
            })
            .children(self.avatar)
            .child(
                div()
                    .w(px(DATE_WIDTH))
                    .flex_none()
                    .flex()
                    .justify_end()
                    .text_size(px(FONT_SMALL))
                    .font_features(tabular())
                    .text_color(muted)
                    .children(self.date),
            );
        pressed(row, theme.color(ColorToken::CardsInnerFill))
    }
}

#[derive(IntoElement)]
pub struct GroupHeader {
    status: TicketStatus,
    title: SharedString,
    count: usize,
    collapsed: bool,
    on_add: OnClick,
    on_toggle: Option<OnClick>,
}

pub fn group_header(
    status: TicketStatus,
    title: impl Into<SharedString>,
    count: usize,
    collapsed: bool,
    on_add: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> GroupHeader {
    GroupHeader {
        status,
        title: title.into(),
        count,
        collapsed,
        on_add: Box::new(on_add),
        on_toggle: None,
    }
}

impl GroupHeader {
    pub fn on_toggle(
        mut self,
        on_toggle: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
    ) -> Self {
        self.on_toggle = Some(Box::new(on_toggle));
        self
    }
}

impl RenderOnce for GroupHeader {
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let hover = ink(&theme, HOVER);
        let turn = if self.collapsed { -FRAC_PI_2 } else { 0.0 };
        let on_add = self.on_add;
        let add = pressed(
            div()
                .id("group-add")
                .aria_label("Add")
                .flex()
                .flex_none()
                .items_center()
                .justify_center()
                .size(px(ADD_BUTTON))
                .rounded(px(RADIUS_BADGE))
                .cursor_pointer()
                .hover(move |style| style.bg(hover))
                .on_click(move |event, window, cx| {
                    cx.stop_propagation();
                    on_add(event, window, cx);
                }),
            theme.color(ColorToken::CardsInnerFill),
        )
        .child(icon(Icon::Plus, ICON_SMALL, ink(&theme, T2)));
        div()
            .id(("issue-group", self.status as usize))
            .w_full()
            .h(px(GROUP_ROW))
            .flex()
            .flex_none()
            .items_center()
            .gap_2()
            .px(px(LROW_PAD_X))
            .rounded(px(RADIUS_ROW))
            .bg(ink(&theme, GROUP_FILL))
            .text_size(px(FONT_BODY))
            .when_some(self.on_toggle, |header, on_toggle| {
                header.cursor_pointer().on_click(on_toggle)
            })
            .child(
                glyph(Glyph::Chevron, ICON_TINY, ink(&theme, CAPTION_TEXT))
                    .with_transformation(Transformation::rotate(radians(turn))),
            )
            .child(status_icon(self.status, px(STATUS_MARK)))
            .child(
                div()
                    .min_w_0()
                    .truncate()
                    .font_weight(FontWeight::MEDIUM)
                    .text_color(ink(&theme, T1))
                    .child(self.title),
            )
            .child(
                div()
                    .flex_none()
                    .text_size(px(FONT_SMALL))
                    .font_features(tabular())
                    .text_color(ink(&theme, CAPTION_TEXT))
                    .child(self.count.to_string()),
            )
            .child(div().flex_1())
            .child(add)
    }
}
