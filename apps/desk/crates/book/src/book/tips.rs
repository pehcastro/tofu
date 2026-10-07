use desk_ui::component::icon_button;
use desk_ui::components::avatar::{Agent, AgentKind, AgentStatus, AvatarSize, avatar};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::paint::ink;
use desk_ui::components::size::{RING_OUTSET, T1, T2};
use desk_ui::components::tooltip::{Edge, hover_card, tooltip, tooltip_debug};
use desk_ui::icon::Icon;
use desk_ui::theme::Theme;
use gpui::{Context, Div, FontWeight, Window, div, prelude::*, px};

use super::Book;
use super::kit::{block, spread};

const ICONS: [(Icon, &str); 10] = [
    (Icon::Sidebar, "Toggle sidebar"),
    (Icon::Plus, "New workspace"),
    (Icon::Search, "Search"),
    (Icon::Bell, "Notifications"),
    (Icon::Branch, "Switch branch"),
    (Icon::Arrow, "Send"),
    (Icon::Minimize, "Minimize"),
    (Icon::Maximize, "Maximize"),
    (Icon::Restore, "Restore"),
    (Icon::Close, "Close"),
];

const WORDS: [(&str, &str, &str); 3] = [
    (
        "Jev",
        "the typed decision model that classifies state",
        " judges every step, the ",
    ),
    (
        "lead",
        "the agent that plans and hands work out",
        " plans it, and each ",
    ),
    (
        "ticket",
        "a unit of work with an Acceptance section",
        " says when it is done.",
    ),
];

const EDGES: [&str; 4] = ["Top left", "Top right", "Bottom left", "Bottom right"];

const WRAPPED: &str = "a phrase long enough to wrap across two lines of this paragraph";
const WRAP_WIDTH: f32 = 240.0;
const EVENT_TEXT: f32 = 11.0;
const EVENT_LINE: f32 = 16.0;
const PROSE_WIDTH: f32 = 520.0;
const EDGE_HEIGHT: f32 = 160.0;

fn status_word(status: AgentStatus) -> &'static str {
    match status {
        AgentStatus::Working => "working",
        AgentStatus::Asking => "asking the lead",
        AgentStatus::Failed => "failed",
        AgentStatus::Finished => "finished",
    }
}

pub(super) fn tips(theme: &Theme, window: &mut Window, cx: &mut Context<Book>) -> Div {
    let icons = ICONS.into_iter().enumerate().map(|(at, (glyph, label))| {
        tooltip(
            ("tip-icon", at),
            icon_button(("icon", at), glyph, label, theme),
            Edge::Frame,
            label,
            theme,
            window,
            cx,
        )
    });
    let icon_row = div().flex().children(icons.collect::<Vec<_>>());
    let events = div()
        .h(px(EVENT_LINE))
        .overflow_hidden()
        .whitespace_nowrap()
        .text_size(px(EVENT_TEXT))
        .line_height(px(EVENT_LINE))
        .text_color(ink(theme, T2))
        .child(tooltip_debug(cx.entity_id(), window, cx).events);
    let icon_row = div()
        .flex()
        .flex_col()
        .gap_2()
        .child(icon_row)
        .child(events);

    let wrapped = div()
        .id("wrapped")
        .w(px(WRAP_WIDTH))
        .text_color(ink(theme, T1))
        .underline()
        .child(WRAPPED);
    let top_word = div()
        .id("top-word")
        .text_color(ink(theme, T1))
        .underline()
        .child("top of the page");
    let top_line = div()
        .flex()
        .text_color(ink(theme, T2))
        .child("A word at the ")
        .child(tooltip(
            "tip-top",
            top_word,
            Edge::Text,
            "flips below when there is no room above",
            theme,
            window,
            cx,
        ));

    let mut prose = div()
        .w(px(PROSE_WIDTH))
        .text_color(ink(theme, T2))
        .child("The ");
    for (at, (word, about, after)) in WORDS.into_iter().enumerate() {
        let inline = div()
            .id(("word", at))
            .text_color(ink(theme, T1))
            .underline()
            .child(word);
        prose = prose
            .child(tooltip(
                ("tip-word", at),
                inline,
                Edge::Text,
                about,
                theme,
                window,
                cx,
            ))
            .child(after);
    }
    let prose = div()
        .flex()
        .flex_col()
        .gap_3()
        .child(prose.flex().flex_wrap())
        .child(tooltip(
            "tip-wrapped",
            wrapped,
            Edge::Text,
            "sits above the whole phrase",
            theme,
            window,
            cx,
        ));

    let agents: Vec<Agent> = AgentKind::ALL
        .into_iter()
        .zip(AgentStatus::ALL.into_iter().cycle())
        .map(|(kind, status)| Agent {
            kind,
            instance: 1,
            status,
        })
        .collect();
    let avatars = agents.iter().enumerate().map(|(at, agent)| {
        let ringed = agent.status == AgentStatus::Working;
        let face = div()
            .id(("face", at))
            .map(|face| match ringed {
                true => face.p(px(RING_OUTSET)),
                false => face.m(px(RING_OUTSET)),
            })
            .child(avatar(("avatar", at), agent, AvatarSize::Row, theme));
        let words = format!("{}, {}", agent.name(), status_word(agent.status));
        tooltip(
            ("tip-face", at),
            face,
            Edge::Frame,
            words,
            theme,
            window,
            cx,
        )
    });
    let avatar_row = spread(theme).children(avatars.collect::<Vec<_>>());

    let names = agents.iter().enumerate().map(|(at, agent)| {
        let name = div()
            .id(("name", at))
            .px_1()
            .text_color(ink(theme, T1))
            .child(agent.name());
        let card = div()
            .flex()
            .flex_col()
            .gap_2()
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(avatar(("card-avatar", at), agent, AvatarSize::Row, theme))
                    .child(div().font_weight(FontWeight::SEMIBOLD).child(agent.name())),
            )
            .child(
                div()
                    .text_color(ink(theme, T2))
                    .child(status_word(agent.status)),
            )
            .child(
                div()
                    .flex()
                    .gap_2()
                    .child(button(
                        ("card-open", at),
                        "Open sheet",
                        None,
                        ButtonKind::Plain,
                        theme,
                    ))
                    .child(button(
                        ("card-stop", at),
                        "Stop",
                        None,
                        ButtonKind::Text,
                        theme,
                    )),
            );
        hover_card(("card-name", at), name, Edge::Text, card, theme, window, cx)
    });
    let name_row = spread(theme).children(names.collect::<Vec<_>>());

    let corner = |at: usize, label: &'static str, window: &mut Window, cx: &mut Context<Book>| {
        tooltip(
            ("tip-edge", at),
            icon_button(("edge", at), Icon::Arrow, label, theme),
            Edge::Frame,
            label,
            theme,
            window,
            cx,
        )
    };
    let [top_left, top_right, bottom_left, bottom_right] = EDGES;
    let edges = div()
        .h(px(EDGE_HEIGHT))
        .flex()
        .flex_col()
        .justify_between()
        .child(
            div()
                .flex()
                .justify_between()
                .child(corner(0, top_left, window, cx))
                .child(corner(1, top_right, window, cx)),
        )
        .child(
            div()
                .flex()
                .justify_between()
                .child(corner(2, bottom_left, window, cx))
                .child(corner(3, bottom_right, window, cx)),
        );

    div()
        .flex()
        .flex_col()
        .gap_3()
        .child(top_line)
        .child(block(
            "Edges: scroll a corner to the window edge",
            theme,
            edges,
        ))
        .child(block("Icon row, packed edge to edge", theme, icon_row))
        .child(block("Inline words", theme, prose))
        .child(block("Avatars", theme, avatar_row))
        .child(block("Agent names with hover cards", theme, name_row))
}
