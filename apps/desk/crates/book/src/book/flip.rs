use desk_motion::FlipTracker;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::inner_card;
use desk_ui::components::flip::{Flip, flip};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    App, ClickEvent, Context, Div, ElementId, Entity, SharedString, Window, div, prelude::*, px,
};

use super::Book;
use super::kit::{block, label, spread};

const COLUMNS: [&str; 3] = ["Todo", "Doing", "Done"];
const CROWD: usize = 20;
const COLUMN_WIDTH: f32 = 220.0;
const COLUMN_RADIUS: f32 = 8.0;
const CARD_HEIGHT: f32 = 44.0;
const CROWD_CARD_HEIGHT: f32 = 20.0;
const LANE_HEIGHT: f32 = 120.0;
const CROWD_LANE_HEIGHT: f32 = 520.0;

struct FlipPage {
    tracker: Entity<FlipTracker>,
    lone: usize,
    crowd: usize,
}

pub(super) fn flip_page(theme: &Theme, window: &mut Window, cx: &mut Context<Book>) -> Div {
    let state = window.use_keyed_state("flip-page", cx, |_, cx| FlipPage {
        tracker: cx.new(|_| FlipTracker::default()),
        lone: 0,
        crowd: 0,
    });
    let (tracker, lone, crowd) = {
        let page = state.read(cx);
        (page.tracker.clone(), page.lone, page.crowd)
    };
    let lone_lanes = COLUMNS.iter().enumerate().map(|(at, name)| {
        let card = (at == lone).then(|| {
            card(
                "flip-lone",
                "DESK-385 move me",
                CARD_HEIGHT,
                &tracker,
                theme,
            )
        });
        lane(name, LANE_HEIGHT, theme).children(card)
    });
    let crowd_lanes = COLUMNS.iter().enumerate().map(|(at, name)| {
        let cards = (at == crowd)
            .then(|| {
                (0..CROWD).map(|ix| {
                    let text = format!("Card {}", ix + 1);
                    card(("flip-crowd", ix), text, CROWD_CARD_HEIGHT, &tracker, theme)
                })
            })
            .into_iter()
            .flatten();
        lane(name, CROWD_LANE_HEIGHT, theme).children(cards)
    });
    let (one, all) = (state.clone(), state);
    div()
        .flex()
        .flex_col()
        .gap_3()
        .child(block(
            "Move sends the card to the next column; a second click mid-flight turns it from where it is. With reduced motion on it lands in one frame",
            theme,
            div()
                .flex()
                .flex_col()
                .gap_3()
                .child(spread(theme).child(
                    button("flip-move", "Move", None, ButtonKind::Primary, theme).on_click(
                        move |_: &ClickEvent, _: &mut Window, cx: &mut App| {
                            one.update(cx, |page, cx| {
                                page.lone = (page.lone + 1) % COLUMNS.len();
                                cx.notify();
                            });
                        },
                    ),
                ))
                .child(div().flex().gap_3().children(lone_lanes)),
        ))
        .child(block(
            "Twenty cards move at once",
            theme,
            div()
                .flex()
                .flex_col()
                .gap_3()
                .child(spread(theme).child(
                    button("flip-move-all", "Move 20", None, ButtonKind::Plain, theme).on_click(
                        move |_: &ClickEvent, _: &mut Window, cx: &mut App| {
                            all.update(cx, |page, cx| {
                                page.crowd = (page.crowd + 1) % COLUMNS.len();
                                cx.notify();
                            });
                        },
                    ),
                ))
                .child(div().flex().gap_3().children(crowd_lanes)),
        ))
}

fn lane(name: &'static str, height: f32, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_col()
        .gap_1()
        .p_2()
        .w(px(COLUMN_WIDTH))
        .h(px(height))
        .rounded(px(COLUMN_RADIUS))
        .bg(theme.color(ColorToken::CardsOuterFill))
        .child(label(name, theme))
}

fn card(
    id: impl Into<ElementId>,
    text: impl Into<SharedString>,
    height: f32,
    tracker: &Entity<FlipTracker>,
    theme: &Theme,
) -> Flip<Div> {
    flip(
        id,
        tracker,
        inner_card(theme)
            .flex_none()
            .justify_center()
            .h(px(height))
            .px_3()
            .child(text.into()),
    )
}
