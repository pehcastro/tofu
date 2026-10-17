use desk_ui::component::icon;
use desk_ui::components::avatar::spinner;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::MenuItem;
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::size::{FONT_BODY, RADIUS_ROW, T2, T3};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::{CONTROL, HAIRLINE, ICON, ICON_TINY};
use desk_ui::theme::ColorToken;
use gpui::{
    AnyElement, ClickEvent, Context, FontWeight, SharedString, Window, div, prelude::*, px,
};

use super::model::{Action, GitScreen};
use super::sync::{header_button, pull_button, push_button};
use super::undo;
use super::watch::Touched;

pub const HEIGHT: f32 = 40.0;

fn switcher(screen: &GitScreen, cx: &mut Context<GitScreen>) {
    let snapshot = screen.snapshot();
    let shown: SharedString = snapshot.branch.as_ref().map_or_else(
        || "detached HEAD".into(),
        |branch| branch.name.clone().into(),
    );
    let names: Vec<String> = snapshot
        .branches
        .iter()
        .filter(|branch| !branch.current)
        .map(|branch| branch.name.clone())
        .collect();
    let items = names
        .iter()
        .map(|name| MenuItem::action(format!("Switch to {name}")))
        .collect();
    let entity = cx.entity();
    screen.switcher.update(cx, |menu, cx| {
        menu.items(items, cx);
        menu.trigger(move |open, theme| {
            let hover = theme.color(ColorToken::StateActive);
            div()
                .id("git-branch-switcher")
                .flex()
                .items_center()
                .gap_1p5()
                .h(px(CONTROL))
                .px_2()
                .rounded(px(RADIUS_ROW))
                .cursor_pointer()
                .font_weight(FontWeight::MEDIUM)
                .when(open, |face| face.bg(hover))
                .hover(move |face| face.bg(hover))
                .child(glyph(Glyph::Branch, ICON, ink(theme, T2)))
                .child(shown.clone())
                .child(glyph(Glyph::Chevron, ICON_TINY, ink(theme, T3)))
        });
        menu.on_pick(move |at, _, cx| {
            let Some(name) = names.get(*at).cloned() else {
                return eprintln!("desk: git: the branch menu has no item {at}");
            };
            entity.update(cx, |screen, cx| screen.run(Action::Switch(name), cx));
        });
    });
}

pub fn render(screen: &GitScreen, window: &mut Window, cx: &mut Context<GitScreen>) -> AnyElement {
    let theme = ActiveTheme::theme(cx);
    switcher(screen, cx);
    let repo = screen.root().file_name().map_or_else(
        || screen.root().display().to_string(),
        |name| name.to_string_lossy().into_owned(),
    );
    let remotes = screen.snapshot().remotes.len();
    let can_fetch = remotes > 0 && !screen.running();
    let fetch_tip = match remotes {
        0 => "Nothing to fetch: this repository has no remote".to_owned(),
        1 => "Fetch: look at the remote for new commits without changing your files".to_owned(),
        count => format!(
            "Fetch: look at all {count} remotes for new commits without changing your files"
        ),
    };
    let color = theme.color(ColorToken::TextBase);
    let fetch = header_button(
        "git-fetch",
        "Fetch",
        glyph(Glyph::Globe, ICON, color),
        can_fetch,
        &theme,
    )
    .when(can_fetch, |face| {
        face.on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.run(Action::Fetch, cx)))
    });
    let refresh = header_button(
        "git-refresh",
        "",
        icon(Icon::Reset, ICON, color),
        true,
        &theme,
    )
    .on_click(cx.listener(|screen, _: &ClickEvent, _, cx| screen.reload(Touched::Git, cx)));
    let busy = screen.running() || screen.reloading;
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap_1()
        .h(px(HEIGHT))
        .px_2()
        .border_b(px(HAIRLINE))
        .border_color(theme.color(ColorToken::Separator))
        .text_size(px(FONT_BODY))
        .child(
            div()
                .flex()
                .flex_1()
                .min_w_0()
                .items_center()
                .gap_1()
                .child(
                    div()
                        .flex_none()
                        .px_1()
                        .font_weight(FontWeight::SEMIBOLD)
                        .child(repo),
                )
                .child(screen.switcher.clone()),
        )
        .child(
            div()
                .flex()
                .flex_none()
                .items_center()
                .gap_1()
                .child(undo::button(screen, window, cx))
                .child(tooltip(
                    "git-fetch-tip",
                    fetch,
                    Edge::Frame,
                    fetch_tip,
                    &theme,
                    window,
                    cx,
                ))
                .child(pull_button(screen, window, cx))
                .child(push_button(screen, window, cx)),
        )
        .child(
            div()
                .flex()
                .flex_1()
                .justify_end()
                .items_center()
                .gap_1()
                .when(busy, |strip| strip.child(spinner("git-busy", &theme)))
                .child(tooltip(
                    "git-refresh-tip",
                    refresh,
                    Edge::Frame,
                    "Refresh: read the repository again",
                    &theme,
                    window,
                    cx,
                )),
        )
        .into_any_element()
}
