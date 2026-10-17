use desk_core::git::{Branch, Stash};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::caption;
use desk_ui::components::chip::{badge, mono};
use desk_ui::components::form::TextInput;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::size::{FONT_BODY, FONT_SMALL, RADIUS_ROW, T2, T3, TREE_ROW};
use desk_ui::components::skeleton::skeleton_lines;
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::ICON_TINY;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, App, ClickEvent, Context, Div, ElementId, Entity, Focusable, KeyDownEvent,
    SharedString, Stateful, Subscription, Window, div, prelude::*, px,
};

use super::model::{Action, GitScreen};
use super::sync::plural;

const PAD: f32 = 8.0;
const DOT: f32 = 7.0;
const SKELETON_LINES: usize = 10;
const DISABLED: f32 = 0.45;

#[derive(Clone, Copy, PartialEq, Eq)]
enum Section {
    Branches,
    Remotes,
    Tags,
    Stashes,
}

impl Section {
    fn title(self) -> &'static str {
        match self {
            Section::Branches => "Branches",
            Section::Remotes => "Remotes",
            Section::Tags => "Tags",
            Section::Stashes => "Stashes",
        }
    }
}

#[derive(Clone, PartialEq, Eq)]
enum Picked {
    Branch(String),
    Stash(u32),
}

pub struct Sidebar {
    filter: Entity<TextInput>,
    naming: Option<Entity<TextInput>>,
    folded: Vec<Section>,
    picked: Option<Picked>,
    _filter: Subscription,
}

impl Sidebar {
    pub fn new(window: &mut Window, cx: &mut Context<GitScreen>) -> Sidebar {
        let filter = TextInput::new("Filter".into(), window, cx);
        Sidebar {
            _filter: cx.observe(&filter, |_, _, cx| cx.notify()),
            filter,
            naming: None,
            folded: Vec::new(),
            picked: None,
        }
    }
}

fn pick(
    picked: Picked,
) -> impl Fn(&mut GitScreen, &ClickEvent, &mut Window, &mut Context<GitScreen>) {
    move |screen, _, _, cx| {
        let again = screen.sidebar.picked.as_ref() == Some(&picked);
        screen.sidebar.picked = (!again).then(|| picked.clone());
        cx.notify();
    }
}

fn row(id: impl Into<ElementId>, picked: bool, theme: &Theme) -> Stateful<Div> {
    let hover = theme.color(ColorToken::StateActive);
    div()
        .id(id)
        .flex()
        .flex_none()
        .items_center()
        .gap_2()
        .h(px(TREE_ROW))
        .px(px(PAD))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .text_size(px(FONT_BODY))
        .when(picked, |row| row.bg(hover))
        .hover(move |row| row.bg(hover))
}

fn name(text: impl Into<SharedString>) -> Div {
    div().flex_1().min_w_0().truncate().child(text.into())
}

fn small(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .flex_none()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T3))
        .child(text.into())
}

fn act(
    id: String,
    label: &'static str,
    tip: String,
    action: Action,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Stateful<Div> {
    let key = SharedString::from(format!("{id}-tip"));
    let face = button(SharedString::from(id), label, None, ButtonKind::Text, theme).on_click(
        cx.listener(move |screen, _: &ClickEvent, _, cx| {
            cx.stop_propagation();
            screen.sidebar.picked = None;
            screen.run(action.clone(), cx);
        }),
    );
    tooltip(key, face, Edge::Frame, tip, theme, window, cx)
}

fn branch_row(
    branch: &Branch,
    current: Option<&str>,
    picked: bool,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> AnyElement {
    let lit = theme.color(ColorToken::ButtonPrimary);
    let dot = div()
        .flex_none()
        .size(px(DOT))
        .rounded_full()
        .border_1()
        .border_color(match branch.current {
            true => lit,
            false => ink(theme, T3),
        })
        .when(branch.current, |dot| dot.bg(lit));
    let drift = [(branch.ahead, '\u{2191}'), (branch.behind, '\u{2193}')]
        .into_iter()
        .filter(|(count, _)| *count > 0)
        .map(|(count, arrow)| format!("{arrow}{count}"))
        .collect::<Vec<_>>()
        .join(" ");
    let switch = (picked && !branch.current).then(|| {
        let from = current.unwrap_or("HEAD");
        act(
            format!("git-switch-{}", branch.name),
            "Switch",
            format!("Switch: leave {from} and work on {}", branch.name),
            Action::Switch(branch.name.clone()),
            theme,
            window,
            cx,
        )
    });
    row(
        SharedString::from(format!("git-branch-{}", branch.name)),
        picked,
        theme,
    )
    .on_click(cx.listener(pick(Picked::Branch(branch.name.clone()))))
    .when(branch.current, |row| {
        row.text_color(lit).font_weight(gpui::FontWeight::MEDIUM)
    })
    .child(dot)
    .child(name(branch.name.clone()))
    .when(!drift.is_empty(), |row| row.child(small(drift, theme)))
    .children(switch)
    .into_any_element()
}

fn stash_row(
    stash: &Stash,
    picked: bool,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> AnyElement {
    let at = stash.index;
    let actions = picked.then(|| {
        div()
            .flex()
            .flex_none()
            .child(act(
                format!("git-stash-pop-{at}"),
                "Pop",
                format!("Pop: put stash@{{{at}}} back into your files and drop it from the list"),
                Action::StashPop(at),
                theme,
                window,
                cx,
            ))
            .child(act(
                format!("git-stash-drop-{at}"),
                "Drop",
                format!("Drop: delete stash@{{{at}}} for good"),
                Action::StashDrop(at),
                theme,
                window,
                cx,
            ))
    });
    row(("git-stash", at as usize), picked, theme)
        .on_click(cx.listener(pick(Picked::Stash(at))))
        .child(
            div()
                .flex_none()
                .font_family(mono(theme))
                .text_size(px(FONT_SMALL))
                .child(format!("stash@{{{at}}}")),
        )
        .child(name(stash.message.clone()).text_color(ink(theme, T2)))
        .children(actions)
        .into_any_element()
}

fn naming(screen: &GitScreen, cx: &mut Context<GitScreen>) -> Option<AnyElement> {
    let input = screen.sidebar.naming.clone()?;
    let shown = input.clone();
    let at = screen
        .snapshot()
        .branch
        .as_ref()
        .map_or_else(|| "HEAD".to_owned(), |branch| branch.name.clone());
    Some(
        div()
            .px(px(PAD))
            .pb_1()
            .overflow_hidden()
            .on_key_down(cx.listener(move |screen, event: &KeyDownEvent, _, cx| {
                match event.keystroke.key.as_str() {
                    "enter" => {
                        let name = input.read(cx).text().trim().to_owned();
                        screen.sidebar.naming = None;
                        if !name.is_empty() {
                            screen.run(
                                Action::CreateBranch {
                                    name,
                                    at: at.clone(),
                                },
                                cx,
                            );
                        }
                    }
                    "escape" => screen.sidebar.naming = None,
                    _ => return,
                }
                cx.stop_propagation();
                cx.notify();
            }))
            .child(shown)
            .into_any_element(),
    )
}

fn header_act(
    id: &'static str,
    label: &'static str,
    tip: String,
    on: Option<impl Fn(&ClickEvent, &mut Window, &mut App) + 'static>,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> AnyElement {
    let face = match on {
        Some(on) => button(id, label, None, ButtonKind::Text, theme).on_click(on),
        None => button(id, label, None, ButtonKind::Text, theme).opacity(DISABLED),
    };
    let key = SharedString::from(format!("{id}-tip"));
    tooltip(key, face, Edge::Frame, tip, theme, window, cx).into_any_element()
}

fn section(
    screen: &GitScreen,
    which: Section,
    rows: Vec<AnyElement>,
    trailing: AnyElement,
    theme: &Theme,
    cx: &mut Context<GitScreen>,
) -> Div {
    let folded = screen.sidebar.folded.contains(&which);
    let chevron = glyph(Glyph::Chevron, ICON_TINY, ink(theme, T3)).with_transformation(
        gpui::Transformation::rotate(gpui::radians(match folded {
            true => 0.0,
            false => std::f32::consts::FRAC_PI_2,
        })),
    );
    let head = div()
        .id(which.title())
        .flex()
        .items_center()
        .gap_1p5()
        .h(px(TREE_ROW))
        .px(px(PAD))
        .cursor_pointer()
        .on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| {
            let folded = &mut screen.sidebar.folded;
            match folded.iter().position(|section| *section == which) {
                Some(at) => {
                    folded.remove(at);
                }
                None => folded.push(which),
            }
            cx.notify();
        }))
        .child(chevron)
        .child(caption(which.title(), theme).flex_1())
        .child(trailing)
        .child(badge(rows.len().to_string(), theme));
    div()
        .flex()
        .flex_col()
        .child(head)
        .when(!folded, |section| section.children(rows))
}

pub fn render(screen: &GitScreen, window: &mut Window, cx: &mut Context<GitScreen>) -> AnyElement {
    let theme = ActiveTheme::theme(cx);
    let filter = div()
        .flex_none()
        .m(px(PAD))
        .overflow_hidden()
        .rounded(px(RADIUS_ROW))
        .child(screen.sidebar.filter.clone());
    if !screen.read {
        return div()
            .flex()
            .flex_col()
            .size_full()
            .child(filter)
            .child(
                div()
                    .p(px(PAD))
                    .child(skeleton_lines("git-side", SKELETON_LINES, &theme, cx)),
            )
            .into_any_element();
    }
    let query = screen.sidebar.filter.read(cx).text().to_lowercase();
    let hits = |text: &str| query.is_empty() || text.to_lowercase().contains(&query);
    let snapshot = screen.snapshot();
    let picked = screen.sidebar.picked.as_ref();
    let current = snapshot.branch.as_ref().map(|branch| branch.name.as_str());
    let branches = snapshot
        .branches
        .iter()
        .filter(|branch| hits(&branch.name))
        .map(|branch| {
            let on = picked == Some(&Picked::Branch(branch.name.clone()));
            branch_row(branch, current, on, &theme, window, cx)
        })
        .collect();
    let remotes = snapshot
        .remotes
        .iter()
        .filter(|(name, url)| hits(name) || hits(url))
        .map(|(remote, url)| {
            row(
                SharedString::from(format!("git-remote-{remote}")),
                false,
                &theme,
            )
            .child(div().flex_none().child(remote.clone()))
            .child(
                name(url.clone())
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(&theme, T3)),
            )
            .into_any_element()
        })
        .collect();
    let tags = snapshot
        .tags
        .iter()
        .filter(|tag| hits(tag))
        .map(|tag| {
            row(SharedString::from(format!("git-tag-{tag}")), false, &theme)
                .child(small("#", &theme))
                .child(name(tag.clone()))
                .into_any_element()
        })
        .collect();
    let stashes = snapshot
        .stashes
        .iter()
        .filter(|stash| hits(&stash.message))
        .map(|stash| {
            let on = picked == Some(&Picked::Stash(stash.index));
            stash_row(stash, on, &theme, window, cx)
        })
        .collect();
    let at = current.unwrap_or("HEAD").to_owned();
    let open = cx.listener(|screen, _: &ClickEvent, window, cx| {
        let input = TextInput::new("New branch name".into(), window, cx);
        let handle = input.read(cx).focus_handle(cx);
        handle.focus(window, cx);
        screen.sidebar.naming = Some(input);
        cx.stop_propagation();
        cx.notify();
    });
    let new_branch = header_act(
        "git-new-branch",
        "New",
        format!("New branch: start a branch at {at}, name it and press Enter"),
        Some(open),
        &theme,
        window,
        cx,
    );
    let dirty = snapshot.changes.len();
    let message = format!("desk: stashed on {at}");
    let push = cx.listener(move |screen, _: &ClickEvent, _, cx| {
        cx.stop_propagation();
        screen.run(Action::StashPush(message.clone()), cx)
    });
    let stash = header_act(
        "git-stash-push",
        "Stash",
        match dirty {
            0 => "Nothing to stash: there are no uncommitted changes".to_owned(),
            count => format!(
                "Stash: put {} aside and clean the tree",
                plural(count, "changed file")
            ),
        },
        (dirty > 0).then_some(push),
        &theme,
        window,
        cx,
    );
    let naming = naming(screen, cx);
    let empty = || div().into_any_element();
    div()
        .flex()
        .flex_col()
        .size_full()
        .min_h_0()
        .child(filter)
        .child(
            div()
                .id("git-side-scroll")
                .flex()
                .flex_col()
                .flex_1()
                .min_h_0()
                .px(px(PAD / 2.0))
                .overflow_y_scroll()
                .child(section(
                    screen,
                    Section::Branches,
                    branches,
                    new_branch,
                    &theme,
                    cx,
                ))
                .children(naming)
                .child(section(
                    screen,
                    Section::Remotes,
                    remotes,
                    empty(),
                    &theme,
                    cx,
                ))
                .child(section(screen, Section::Tags, tags, empty(), &theme, cx))
                .child(section(
                    screen,
                    Section::Stashes,
                    stashes,
                    stash,
                    &theme,
                    cx,
                )),
        )
        .into_any_element()
}
