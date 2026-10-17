use std::collections::{BTreeMap, HashSet};
use std::f32::consts::FRAC_PI_2;

use desk_core::git::{DiffOf, Entry, Mark, State};
use desk_motion::tokens::{EASE_OUT, HOVER_MS};
use desk_ui::component::icon;
use desk_ui::components::card::caption;
use desk_ui::components::chip::{badge, mono};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::pill_tabs::PillTabs;
use desk_ui::components::size::{
    CAPTION_TEXT, DIM_TEXT, FONT_SMALL, FONT_TAB, HOVER, RADIUS_BADGE, RADIUS_ROW, T1, T2,
    TREE_INDENT,
};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::{ICON_SMALL, ICON_TINY};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, ClickEvent, Context, Div, Entity, Motion, SharedString, Stateful, Transformation,
    Window, div, prelude::*, px, radians,
};

use super::commit_box;
use super::confirm::{self, Discarded};
use super::detail;
use super::diff;
use super::model::{Action, GitScreen, Panel};

const TABS: f32 = 40.0;
const ROW: f32 = 26.0;
const ROW_PAD: f32 = 8.0;
const LIST_PAD: f32 = 6.0;
const LETTER: f32 = 14.0;
const ACT: f32 = 22.0;
const ACT_GAP: f32 = 2.0;
const ROW_GAP: f32 = 6.0;
const ROW_GROUP: &str = "git-row";

#[derive(Clone, Copy)]
pub(super) struct Letter {
    text: &'static str,
    color: ColorToken,
}

const UNTRACKED: Letter = Letter {
    text: "U",
    color: ColorToken::GitUntracked,
};
const CONFLICT: Letter = Letter {
    text: "!",
    color: ColorToken::StatusDanger,
};

pub(super) fn letter(mark: Mark) -> Option<Letter> {
    let (text, color) = match mark {
        Mark::Unmodified => return None,
        Mark::Modified | Mark::TypeChanged => ("M", ColorToken::GitModified),
        Mark::Added => ("A", ColorToken::GitAdded),
        Mark::Deleted => ("D", ColorToken::GitDeleted),
        Mark::Renamed => ("R", ColorToken::GitModified),
        Mark::Copied => ("C", ColorToken::GitAdded),
    };
    Some(Letter { text, color })
}

pub(super) fn letter_cell(letter: Option<Letter>, theme: &Theme) -> Div {
    div()
        .w(px(LETTER))
        .flex_none()
        .text_center()
        .font_family(mono(theme))
        .text_size(px(FONT_SMALL))
        .children(letter.map(|letter| {
            div()
                .text_color(theme.color(letter.color))
                .child(letter.text)
        }))
}

struct Change<'a> {
    path: &'a str,
    letter: Letter,
    discardable: bool,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Section {
    Staged,
    Changes,
}

impl Section {
    fn key(self) -> &'static str {
        match self {
            Section::Staged => "staged",
            Section::Changes => "changes",
        }
    }

    fn folds(self) -> &'static str {
        match self {
            Section::Staged => "git-folds-staged",
            Section::Changes => "git-folds-changes",
        }
    }

    fn title(self) -> &'static str {
        match self {
            Section::Staged => "Staged",
            Section::Changes => "Changes",
        }
    }

    fn of(self) -> DiffOf {
        match self {
            Section::Staged => DiffOf::Index,
            Section::Changes => DiffOf::Worktree,
        }
    }
}

#[derive(Clone, Copy)]
enum Act {
    Stage,
    Unstage,
    Discard,
}

impl Act {
    fn key(self) -> &'static str {
        match self {
            Act::Stage => "stage",
            Act::Unstage => "unstage",
            Act::Discard => "discard",
        }
    }

    fn face(self, theme: &Theme) -> AnyElement {
        let color = ink(theme, T2);
        match self {
            Act::Stage => icon(Icon::Plus, ICON_SMALL, color).into_any_element(),
            Act::Unstage => icon(Icon::Minimize, ICON_SMALL, color).into_any_element(),
            Act::Discard => glyph(Glyph::Undo, ICON_SMALL, color).into_any_element(),
        }
    }
}

enum Row<'a> {
    Folder {
        node: usize,
        label: String,
        key: String,
        open: bool,
        depth: u16,
    },
    File {
        name: &'a str,
        change: &'a Change<'a>,
        depth: u16,
    },
}

#[derive(Default)]
struct Node<'a> {
    folders: BTreeMap<&'a str, usize>,
    files: Vec<(&'a str, &'a Change<'a>)>,
}

pub fn panel(screen: &GitScreen, window: &mut Window, cx: &mut Context<GitScreen>) -> AnyElement {
    let theme = ActiveTheme::theme(cx);
    let draft = commit_box::draft(window, cx);
    let shown = screen.panel();
    let (staged, changed) = split(&screen.snapshot().changes);
    let body = match shown {
        Panel::Changes => changes(screen, &staged, &changed, &theme, window, cx),
        Panel::Commit(sha) => detail::render(screen, sha, &theme, cx),
        Panel::Diff { path, of, back } => diff::render(screen, path, of, back, &theme, window, cx),
    };
    let committing = matches!(root(shown), Panel::Changes);
    div()
        .flex()
        .flex_col()
        .size_full()
        .min_h_0()
        .child(tabs(root(shown), cx))
        .child(body)
        .when(committing, |panel| {
            panel.child(commit_box::render(
                screen,
                &draft,
                staged.len(),
                &theme,
                window,
                cx,
            ))
        })
        .into_any_element()
}

fn root(panel: &Panel) -> &Panel {
    let mut at = panel;
    while let Panel::Diff { back, .. } = at {
        at = back;
    }
    at
}

fn tabs(root: &Panel, cx: &mut Context<GitScreen>) -> Div {
    let picked = match root {
        Panel::Commit(sha) => Some(sha.clone()),
        Panel::Changes | Panel::Diff { .. } => None,
    };
    let strip = PillTabs::new("git-panel-tabs").tab(
        "Changes",
        picked.is_none(),
        cx.listener(|screen, _: &ClickEvent, _, cx| screen.show(Panel::Changes, cx)),
    );
    let strip = match picked {
        Some(sha) => strip.tab(
            "Commit",
            true,
            cx.listener(move |screen, _: &ClickEvent, _, cx| {
                screen.show(Panel::Commit(sha.clone()), cx)
            }),
        ),
        None => strip,
    };
    div()
        .flex()
        .flex_none()
        .items_center()
        .h(px(TABS))
        .px(px(LIST_PAD + ROW_PAD))
        .child(strip)
}

fn split(entries: &[Entry]) -> (Vec<Change<'_>>, Vec<Change<'_>>) {
    let (mut staged, mut changed) = (Vec::new(), Vec::new());
    for entry in entries {
        let path = entry.path.as_str();
        let (index, worktree, discardable) = match &entry.state {
            State::Tracked { index, worktree }
            | State::Renamed {
                index, worktree, ..
            } => (letter(*index), letter(*worktree), true),
            State::Conflicted(_) => (None, Some(CONFLICT), false),
            State::Untracked => (None, Some(UNTRACKED), false),
            State::Ignored => (None, None, false),
        };
        staged.extend(index.map(|letter| Change {
            path,
            letter,
            discardable: false,
        }));
        changed.extend(worktree.map(|letter| Change {
            path,
            letter,
            discardable,
        }));
    }
    (staged, changed)
}

fn changes(
    screen: &GitScreen,
    staged: &[Change],
    changed: &[Change],
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Div {
    let quiet = match (screen.read, staged.is_empty() && changed.is_empty()) {
        (false, _) => Some("Reading the working tree\u{2026}"),
        (true, true) => Some("Nothing to commit. Every file matches the last commit."),
        (true, false) => None,
    };
    let list = div()
        .id("git-changes")
        .flex()
        .flex_col()
        .size_full()
        .overflow_y_scroll()
        .px(px(LIST_PAD))
        .pb(px(LIST_PAD));
    let list = match quiet {
        Some(said) => list.child(
            div()
                .p(px(ROW_PAD))
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, DIM_TEXT))
                .child(said),
        ),
        None => list
            .children(section(screen, Section::Staged, staged, theme, window, cx))
            .children(section(
                screen,
                Section::Changes,
                changed,
                theme,
                window,
                cx,
            )),
    };
    div().flex_1().min_h_0().child(list)
}

fn section(
    screen: &GitScreen,
    section: Section,
    changes: &[Change],
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Option<Div> {
    if changes.is_empty() {
        return None;
    }
    let folds = window.use_keyed_state(section.folds(), cx, |_, _| HashSet::<String>::new());
    let rows = tree(changes, folds.read(cx));
    let header = section_header(screen, section, changes, theme, window, cx);
    let rows: Vec<AnyElement> = rows
        .into_iter()
        .map(|row| match row {
            Row::Folder {
                label,
                key,
                open,
                depth,
                ..
            } => folder_row(section, label, key, open, depth, &folds, theme),
            Row::File {
                name,
                change,
                depth,
            } => file_row(screen, section, name, change, depth, window, cx),
        })
        .collect();
    Some(div().flex().flex_col().child(header).children(rows))
}

fn tree<'a>(changes: &'a [Change<'a>], shut: &HashSet<String>) -> Vec<Row<'a>> {
    let mut nodes = vec![Node::default()];
    for change in changes {
        let (dir, name) = change.path.rsplit_once('/').unwrap_or(("", change.path));
        let mut at = 0;
        for part in dir.split('/').filter(|part| !part.is_empty()) {
            let next = nodes.len();
            let Some(node) = nodes.get_mut(at) else {
                break;
            };
            let found = *node.folders.entry(part).or_insert(next);
            if found == next {
                nodes.push(Node::default());
            }
            at = found;
        }
        if let Some(node) = nodes.get_mut(at) {
            node.files.push((name, change));
        }
    }
    for node in &mut nodes {
        node.files.sort_by(|a, b| a.0.cmp(b.0));
    }
    let mut rows = Vec::new();
    let mut pending = Vec::new();
    push_children(&nodes, 0, "", 0, shut, &mut pending);
    while let Some(row) = pending.pop() {
        if let Row::Folder {
            node,
            key,
            open: true,
            depth,
            ..
        } = &row
        {
            push_children(
                &nodes,
                *node,
                key,
                depth.saturating_add(1),
                shut,
                &mut pending,
            );
        }
        rows.push(row);
    }
    rows
}

fn push_children<'a>(
    nodes: &[Node<'a>],
    node: usize,
    prefix: &str,
    depth: u16,
    shut: &HashSet<String>,
    pending: &mut Vec<Row<'a>>,
) {
    let Some(node) = nodes.get(node) else {
        return;
    };
    for &(name, change) in node.files.iter().rev() {
        pending.push(Row::File {
            name,
            change,
            depth,
        });
    }
    for (&part, &child) in node.folders.iter().rev() {
        let (mut label, mut at) = (part.to_owned(), child);
        while let Some((&only, &deeper)) = nodes
            .get(at)
            .filter(|node| node.files.is_empty() && node.folders.len() == 1)
            .and_then(|node| node.folders.iter().next())
        {
            label = format!("{label}/{only}");
            at = deeper;
        }
        let key = match prefix {
            "" => label.clone(),
            _ => format!("{prefix}/{label}"),
        };
        pending.push(Row::Folder {
            node: at,
            label,
            open: !shut.contains(&key),
            key,
            depth,
        });
    }
}

pub(super) fn files(count: usize) -> String {
    match count {
        1 => "1 file".to_owned(),
        _ => format!("{count} files"),
    }
}

fn acts(buttons: Vec<AnyElement>) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(ACT_GAP))
        .children(buttons)
}

fn act_button(
    act: Act,
    key: String,
    tip: String,
    paths: Vec<String>,
    screen: &GitScreen,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> AnyElement {
    let theme = ActiveTheme::theme(cx);
    let hover = ink(&theme, HOVER);
    let tip_key = SharedString::from(format!("{key}-tip"));
    let face = div()
        .id(SharedString::from(key.clone()))
        .aria_label(SharedString::from(tip.clone()))
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(ACT))
        .rounded(px(RADIUS_BADGE))
        .cursor_pointer()
        .opacity(0.0)
        .transitions(|transitions| transitions.opacity(Motion::new(HOVER_MS).with_easing(EASE_OUT)))
        .group_hover(ROW_GROUP, |style| style.opacity(1.0))
        .hover(move |style| style.bg(hover))
        .child(act.face(&theme));
    let make = match act {
        Act::Stage => Action::Stage,
        Act::Unstage => Action::Unstage,
        Act::Discard => {
            let face = face.on_click(|_, _, cx| cx.stop_propagation());
            let tipped = tooltip(tip_key, face, Edge::Frame, tip, &theme, window, cx);
            let id = SharedString::from(format!("{key}-confirm"));
            return confirm::discard(id, tipped, Discarded::Files(paths), screen, window, cx);
        }
    };
    let face = face.on_click(cx.listener(move |screen, _: &ClickEvent, _, cx| {
        cx.stop_propagation();
        screen.run(make(paths.clone()), cx);
    }));
    tooltip(tip_key, face, Edge::Frame, tip, &theme, window, cx).into_any_element()
}

fn section_header(
    screen: &GitScreen,
    section: Section,
    changes: &[Change],
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Stateful<Div> {
    let all: Vec<String> = changes
        .iter()
        .map(|change| change.path.to_owned())
        .collect();
    let undoable: Vec<String> = changes
        .iter()
        .filter(|change| change.discardable)
        .map(|change| change.path.to_owned())
        .collect();
    let wanted = match section {
        Section::Staged => vec![(
            Act::Unstage,
            format!(
                "Unstage all: leave {} out of the next commit, keeping the changes",
                files(all.len())
            ),
            all,
        )],
        Section::Changes => {
            let stage = (
                Act::Stage,
                format!("Stage all: add {} to the next commit", files(all.len())),
                all,
            );
            match undoable.len() {
                0 => vec![stage],
                count => vec![
                    (
                        Act::Discard,
                        format!(
                            "Discard all: throw away your changes to {} permanently",
                            files(count)
                        ),
                        undoable,
                    ),
                    stage,
                ],
            }
        }
    };
    let buttons: Vec<AnyElement> = wanted
        .into_iter()
        .map(|(act, tip, paths)| {
            let key = format!("git-{}-all-{}", act.key(), section.key());
            act_button(act, key, tip, paths, screen, window, cx)
        })
        .collect();
    div()
        .id(section.key())
        .group(ROW_GROUP)
        .flex()
        .items_center()
        .gap(px(ROW_GAP))
        .h(px(ROW))
        .mt(px(LIST_PAD))
        .px(px(ROW_PAD))
        .child(caption(section.title(), theme))
        .child(badge(changes.len().to_string(), theme))
        .child(div().flex_1())
        .child(acts(buttons))
}

fn folder_row(
    section: Section,
    label: String,
    key: String,
    open: bool,
    depth: u16,
    folds: &Entity<HashSet<String>>,
    theme: &Theme,
) -> AnyElement {
    let folds = folds.clone();
    let hover = ink(theme, HOVER);
    let dim = ink(theme, CAPTION_TEXT);
    let turn = if open { FRAC_PI_2 } else { 0.0 };
    div()
        .id(SharedString::from(format!(
            "git-folder-{}-{key}",
            section.key()
        )))
        .flex()
        .items_center()
        .gap(px(ROW_GAP))
        .h(px(ROW))
        .pl(px(ROW_PAD + f32::from(depth) * TREE_INDENT))
        .pr(px(ROW_PAD))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .text_size(px(FONT_TAB))
        .hover(move |style| style.bg(hover))
        .on_click(move |_, _, cx| {
            folds.update(cx, |shut, cx| {
                if !shut.remove(&key) {
                    shut.insert(key.clone());
                }
                cx.notify();
            })
        })
        .child(
            glyph(Glyph::Chevron, ICON_TINY, dim)
                .with_transformation(Transformation::rotate(radians(turn))),
        )
        .child(glyph(Glyph::Folder, ICON_SMALL, dim))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .text_color(ink(theme, T2))
                .child(label),
        )
        .into_any_element()
}

fn file_row(
    screen: &GitScreen,
    section: Section,
    name: &str,
    change: &Change,
    depth: u16,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> AnyElement {
    let shared = ActiveTheme::theme(cx);
    let theme = &*shared;
    let hover = ink(theme, HOVER);
    let path = change.path.to_owned();
    let row_key = format!("{}-{path}", section.key());
    let wanted = match section {
        Section::Staged => vec![(
            Act::Unstage,
            format!("Unstage: leave {name} out of the next commit, keeping the change"),
        )],
        Section::Changes if change.discardable => vec![
            (
                Act::Discard,
                format!("Discard: throw away your changes to {name} permanently"),
            ),
            (Act::Stage, format!("Stage: add {name} to the next commit")),
        ],
        Section::Changes => vec![(Act::Stage, format!("Stage: add {name} to the next commit"))],
    };
    let buttons: Vec<AnyElement> = wanted
        .into_iter()
        .map(|(act, tip)| {
            let key = format!("git-{}-{row_key}", act.key());
            act_button(act, key, tip, vec![path.clone()], screen, window, cx)
        })
        .collect();
    let deleted = change.letter.text == "D";
    let open = cx.listener(move |screen, _: &ClickEvent, _, cx| {
        screen.show(
            Panel::Diff {
                path: path.clone(),
                of: section.of(),
                back: Box::new(Panel::Changes),
            },
            cx,
        )
    });
    div()
        .id(SharedString::from(format!("git-file-{row_key}")))
        .group(ROW_GROUP)
        .flex()
        .items_center()
        .gap(px(ROW_GAP))
        .h(px(ROW))
        .pl(px(ROW_PAD + f32::from(depth) * TREE_INDENT))
        .pr(px(ROW_PAD))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .text_size(px(FONT_TAB))
        .hover(move |style| style.bg(hover))
        .on_click(open)
        .child(glyph(Glyph::File, ICON_SMALL, ink(theme, CAPTION_TEXT)))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .text_color(ink(theme, T1))
                .when(deleted, |name| name.line_through())
                .child(name.to_owned()),
        )
        .child(acts(buttons))
        .child(letter_cell(Some(change.letter), theme))
        .into_any_element()
}
