use std::cell::Cell;
use std::ops::Range;
use std::rc::Rc;

use desk_motion::tokens::{EASE_OUT, PANEL_IN_MS};
use gpui::{
    Animation, AnimationExt, AnyElement, App, Axis, ClickEvent, Div, ElementId, FontWeight, Rgba,
    ScrollHandle, SharedString, Stateful, Window, canvas, div, prelude::*, px,
};

use crate::component::icon;
use crate::components::card::caption;
use crate::components::chip::tabular;
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink, pressed, ring};
use crate::components::scroll::{RESTING_GUTTER, scrollbar};
use crate::components::size::{
    CAPTION_TEXT, FONT_BADGE, FONT_BODY, FONT_SMALL, HOVER, RADIUS_BADGE, RADIUS_LIST, RADIUS_ROW,
    T1, T2, T3,
};
use crate::components::status_icon::{RunState, TicketStatus, run_mark, status_icon};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{ICON_SMALL, ICON_TINY, TEXT_TINY};
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
const INBOX_WIDTH: f32 = 230.0;
const INBOX_PAD: f32 = 2.0;
const INBOX_HEADER: f32 = 30.0;
const INBOX_CARD_GAP: f32 = 4.0;
const INBOX_BADGE: f32 = 16.0;
const SPARKLE: f32 = 12.0;
const HAIRLINE: f32 = 1.0;
const BULLET: f32 = 4.0;
const BULLET_NUDGE: f32 = 6.0;
const BULLET_OLD: f32 = 0.3;
const ROW_GUESS: f32 = 112.0;
const FIRST_VIEW: f32 = 1200.0;
const OVERDRAW: f32 = 240.0;
const SUMMARY_LINE: f32 = 16.0;
const SUMMARY_LINES: f32 = 2.0;

type OnClick = Box<dyn Fn(&ClickEvent, &mut Window, &mut App)>;
type DrawRows = Box<dyn Fn(Range<usize>, &mut Window, &mut App) -> Vec<AnyElement>>;

enum Cards {
    Built(Vec<AnyElement>),
    Visible {
        count: usize,
        tall: Option<usize>,
        draw: DrawRows,
    },
}

#[derive(Clone, Copy)]
struct Heights {
    row: f32,
    tall: Option<(usize, f32)>,
}

impl Heights {
    fn of(self, at: usize, tall: Option<usize>) -> f32 {
        match self.tall {
            Some((measured, height)) if tall == Some(at) && measured == at => height,
            _ => self.row,
        }
    }
}

pub struct Moment {
    pub text: SharedString,
    pub when: SharedString,
}

pub struct ColumnHeader {
    pub status: TicketStatus,
    pub title: SharedString,
    pub count: usize,
}

#[derive(IntoElement)]
pub struct BoardColumn {
    id: ElementId,
    header: ColumnHeader,
    cards: Cards,
    on_add: Option<OnClick>,
    on_more: Option<OnClick>,
    inbox: Option<Rgba>,
}

pub fn board_column(
    id: impl Into<ElementId>,
    header: ColumnHeader,
    children: impl IntoIterator<Item = impl IntoElement>,
) -> BoardColumn {
    let built = children
        .into_iter()
        .map(IntoElement::into_any_element)
        .collect();
    column(id.into(), header, Cards::Built(built))
}

pub fn board_rows(
    id: impl Into<ElementId>,
    header: ColumnHeader,
    tall: Option<usize>,
    draw: impl Fn(Range<usize>, &mut Window, &mut App) -> Vec<AnyElement> + 'static,
) -> BoardColumn {
    let cards = Cards::Visible {
        count: header.count,
        tall,
        draw: Box::new(draw),
    };
    column(id.into(), header, cards)
}

fn column(id: ElementId, header: ColumnHeader, cards: Cards) -> BoardColumn {
    BoardColumn {
        id,
        header,
        cards,
        on_add: None,
        on_more: None,
        inbox: None,
    }
}

impl BoardColumn {
    pub fn inbox(mut self, tint: Rgba) -> Self {
        self.inbox = Some(tint);
        self
    }

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
        let (pad, gap, radius) = match self.inbox {
            Some(_) => (INBOX_PAD, INBOX_CARD_GAP, RADIUS_ROW),
            None => (COLUMN_PAD, CARD_GAP, RADIUS_LIST),
        };
        let header = match self.inbox {
            Some(tint) => inbox_header(self.header, tint, &theme),
            None => classic_header(self.header, self.on_more, self.on_add, &theme),
        };
        let cards = match self.cards {
            Cards::Built(built) => div().flex().flex_col().gap(px(gap)).children(built),
            Cards::Visible { count, tall, draw } => {
                let heights = window
                    .use_keyed_state(ElementId::from((self.id.clone(), "heights")), cx, |_, _| {
                        Rc::new(Cell::new(Heights {
                            row: ROW_GUESS,
                            tall: None,
                        }))
                    })
                    .read(cx)
                    .clone();
                visible(&handle, &heights, count, tall, gap, &draw, window, cx)
            }
        };
        div()
            .id(self.id)
            .flex()
            .flex_col()
            .map(|column| match self.inbox {
                Some(_) => column.flex_1().min_w(px(INBOX_WIDTH)),
                None => column.flex_none().w(px(COLUMN_WIDTH)),
            })
            .h_full()
            .rounded(px(radius))
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
                            .pl(px(pad))
                            .pr(px(RESTING_GUTTER))
                            .pb(px(pad))
                            .child(cards),
                    )
                    .child(scrollbar("cards-bar", &handle).resting(true)),
            )
    }
}

#[expect(
    clippy::too_many_arguments,
    reason = "the visible range needs the scroll, the measured heights and the drawing callback together"
)]
fn visible(
    handle: &ScrollHandle,
    heights: &Rc<Cell<Heights>>,
    count: usize,
    tall: Option<usize>,
    gap: f32,
    draw: &DrawRows,
    window: &mut Window,
    cx: &mut App,
) -> Div {
    let seen = heights.get();
    let viewport = f32::from(handle.bounds().size.height);
    let viewport = if viewport > 0.0 { viewport } else { FIRST_VIEW };
    let top = -f32::from(handle.offset().y) - OVERDRAW;
    let bottom = top + viewport + 2.0 * OVERDRAW;
    let (mut y, mut before, mut start, mut end) = (0.0, 0.0, count, count);
    for at in 0..count {
        let height = seen.of(at, tall);
        if start == count && y + height > top {
            start = at;
            before = y;
        }
        if y > bottom {
            end = at;
            break;
        }
        y += height;
    }
    let after: f32 = (end..count).map(|at| seen.of(at, tall)).sum();
    let drawn = draw(start..end, window, cx);
    let rows = drawn.into_iter().zip(start..).map(|(row, at)| {
        let heights = heights.clone();
        div().relative().pb(px(gap)).child(row).child(
            canvas(
                move |bounds, window, _| {
                    let height = f32::from(bounds.size.height);
                    let mut known = heights.get();
                    if (known.of(at, tall) - height).abs() < 0.5 {
                        return;
                    }
                    match tall == Some(at) {
                        true => known.tall = Some((at, height)),
                        false => known.row = height,
                    }
                    heights.set(known);
                    window.request_animation_frame();
                },
                |_, _, _, _| {},
            )
            .absolute()
            .inset_0(),
        )
    });
    div()
        .flex()
        .flex_col()
        .child(div().flex_none().h(px(before)))
        .children(rows)
        .child(div().flex_none().h(px(after)))
}

fn inbox_header(header: ColumnHeader, tint: Rgba, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .h(px(INBOX_HEADER))
        .px(px(CARD_PAD))
        .child(glyph(Glyph::Sparkle, SPARKLE, tint))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .justify_center()
                .child(caption(header.title, theme)),
        )
        .child(
            div()
                .flex()
                .flex_none()
                .items_center()
                .justify_center()
                .min_w(px(INBOX_BADGE))
                .h(px(INBOX_BADGE))
                .px_1()
                .rounded(px(RADIUS_BADGE))
                .bg(theme.color(ColorToken::CardsOuterFill))
                .text_size(px(FONT_BADGE))
                .font_features(tabular())
                .text_color(ink(theme, T2))
                .child(header.count.to_string()),
        )
}

fn classic_header(
    header: ColumnHeader,
    on_more: Option<OnClick>,
    on_add: Option<OnClick>,
    theme: &Theme,
) -> Div {
    let mark = ink(theme, T2);
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap_2()
        .h(px(COLUMN_HEADER))
        .px(px(CARD_PAD))
        .child(status_icon(header.status, px(STATUS_MARK)))
        .child(
            div()
                .min_w_0()
                .truncate()
                .text_size(px(FONT_BODY))
                .font_weight(FontWeight::MEDIUM)
                .text_color(ink(theme, T1))
                .child(header.title),
        )
        .child(
            div()
                .flex_none()
                .text_size(px(FONT_SMALL))
                .font_features(tabular())
                .text_color(ink(theme, CAPTION_TEXT))
                .child(header.count.to_string()),
        )
        .child(div().flex_1())
        .children(on_more.map(|on_more| {
            column_button(
                "column-more",
                "More",
                glyph(Glyph::Ellipsis, ICON_SMALL, mark),
                on_more,
                theme,
            )
        }))
        .children(on_add.map(|on_add| {
            column_button(
                "column-add",
                "Add",
                icon(Icon::Plus, ICON_SMALL, mark),
                on_add,
                theme,
            )
        }))
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
    run: Option<(RunState, SharedString)>,
    summary: Option<SharedString>,
    timeline: Vec<Moment>,
    actions: Vec<AnyElement>,
    unfolded: bool,
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
        run: None,
        summary: None,
        timeline: Vec::new(),
        actions: Vec::new(),
        unfolded: false,
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

    pub fn run(mut self, state: RunState, said: impl Into<SharedString>) -> Self {
        self.run = Some((state, said.into()));
        self
    }

    pub fn summary(mut self, summary: impl Into<SharedString>) -> Self {
        self.summary = Some(summary.into());
        self
    }

    pub fn unfolded(
        mut self,
        timeline: Vec<Moment>,
        actions: impl IntoIterator<Item = impl IntoElement>,
    ) -> Self {
        self.unfolded = true;
        self.timeline = timeline;
        self.actions = actions
            .into_iter()
            .map(IntoElement::into_any_element)
            .collect();
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

fn hairline(theme: &Theme) -> Div {
    div().flex_none().h(px(HAIRLINE)).bg(ink(theme, CARD_RING))
}

fn run_line(state: RunState, said: SharedString, theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap_1p5()
        .min_w_0()
        .text_size(px(TEXT_TINY))
        .font_features(tabular())
        .text_color(ink(theme, T2))
        .child(run_mark(state, px(ICON_TINY)))
        .child(div().min_w_0().truncate().child(said))
}

fn timeline(moments: Vec<Moment>, theme: &Theme) -> Div {
    let newest = moments.len().saturating_sub(1);
    div()
        .flex()
        .flex_col()
        .gap_2()
        .children(moments.into_iter().enumerate().map(|(at, moment)| {
            let (bullet, text) = match at == newest {
                true => (ink(theme, T1), ink(theme, T1)),
                false => (ink(theme, BULLET_OLD), ink(theme, T2)),
            };
            div()
                .flex()
                .items_start()
                .gap_2()
                .text_size(px(TEXT_TINY))
                .child(
                    div()
                        .flex_none()
                        .mt(px(BULLET_NUDGE))
                        .size(px(BULLET))
                        .rounded_full()
                        .bg(bullet),
                )
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .text_color(text)
                        .line_clamp(2)
                        .text_ellipsis()
                        .child(moment.text),
                )
                .child(
                    div()
                        .flex_none()
                        .font_features(tabular())
                        .text_color(ink(theme, T3))
                        .child(moment.when),
                )
        }))
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
        let unfold = ElementId::from((self.id.clone(), "unfold"));
        let headline = self.run.is_some();
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
            .when(self.id_line.is_some() || self.avatar.is_some(), |card| {
                card.child(
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
            })
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
                    .children(self.run.as_ref().map(|(state, _)| {
                        div().flex_none().pt(px(TITLE_NUDGE)).child(glyph(
                            Glyph::Sparkle,
                            SPARKLE,
                            state.color(&theme),
                        ))
                    }))
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .text_size(px(FONT_BODY))
                            .text_color(ink(&theme, T1))
                            .text_ellipsis()
                            .map(|title| match headline {
                                true => title.font_weight(FontWeight::SEMIBOLD).line_clamp(1),
                                false => title.font_weight(FontWeight::MEDIUM).line_clamp(2),
                            })
                            .child(self.title),
                    ),
            )
            .when(headline || self.summary.is_some(), |card| {
                card.child(
                    div()
                        .flex_none()
                        .text_size(px(FONT_SMALL))
                        .line_height(px(SUMMARY_LINE))
                        .when(headline, |summary| {
                            summary.h(px(SUMMARY_LINE * SUMMARY_LINES))
                        })
                        .text_color(ink(&theme, T2))
                        .line_clamp(2)
                        .text_ellipsis()
                        .children(self.summary),
                )
            })
            .when(!self.chips.is_empty(), |card| {
                card.child(div().flex().flex_wrap().gap_1().children(self.chips))
            })
            .children(self.run.map(|(state, said)| run_line(state, said, &theme)))
            .children(self.footer.map(|footer| {
                div()
                    .text_size(px(TEXT_TINY))
                    .text_color(ink(&theme, CAPTION_TEXT))
                    .child(footer)
            }))
            .when(self.unfolded, |card| {
                card.child(
                    div()
                        .flex()
                        .flex_col()
                        .gap_3()
                        .child(hairline(&theme))
                        .when(!self.timeline.is_empty(), |body| {
                            body.child(timeline(self.timeline, &theme))
                        })
                        .when(!self.actions.is_empty(), |body| {
                            body.child(
                                div()
                                    .flex()
                                    .justify_end()
                                    .items_center()
                                    .gap_2()
                                    .children(self.actions),
                            )
                        })
                        .with_animation(
                            unfold,
                            Animation::new(PANEL_IN_MS).with_easing(EASE_OUT),
                            |body, shown| body.opacity(shown),
                        ),
                )
            });
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
