use std::f32::consts::FRAC_PI_2;
use std::time::{Duration, Instant};

use desk_core::git::{Branch, Commit, PullHow};
use desk_ui::component::icon;
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::overlay::{Popover, toggles_on_press};
use desk_ui::components::paint::{ink, tint};
use desk_ui::components::size::{FONT_BODY, FONT_SMALL, RADIUS_ROW, T1, T2, T3};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::{CONTROL, ICON};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, ClickEvent, Context, Div, Entity, FontWeight, MouseButton, MouseDownEvent,
    MouseUpEvent, SharedString, Stateful, Svg, Task, Transformation, Window, div, prelude::*, px,
    radians,
};

use super::model::{Action, GitScreen, Loaded};

const FORCE_HOLD: Duration = Duration::from_millis(1500);
const DISABLED: f32 = 0.45;
const POP_WIDTH: f32 = 400.0;
const LIST_MAX: f32 = 220.0;
const SHORT_SHA: usize = 7;

struct PushPop {
    open: bool,
    held: Option<Instant>,
    _fire: Option<Task<gpui::Result<()>>>,
}

enum Upstream<'a> {
    NoBranch,
    NoRemote,
    Unpublished {
        branch: &'a str,
        remote: &'a str,
    },
    Tracked {
        branch: &'a Branch,
        upstream: &'a str,
    },
}

fn upstream(screen: &GitScreen) -> Upstream<'_> {
    let snapshot = screen.snapshot();
    let Some(branch) = snapshot.branch.as_ref() else {
        return Upstream::NoBranch;
    };
    match (branch.upstream.as_deref(), snapshot.remotes.first()) {
        (Some(upstream), _) => Upstream::Tracked { branch, upstream },
        (None, Some((remote, _))) => Upstream::Unpublished {
            branch: &branch.name,
            remote,
        },
        (None, None) => Upstream::NoRemote,
    }
}

pub(super) fn plural(count: usize, noun: &str) -> String {
    match count {
        1 => format!("1 {noun}"),
        count => format!("{count} {noun}s"),
    }
}

pub(super) fn short(sha: &str) -> &str {
    sha.get(..SHORT_SHA).unwrap_or(sha)
}

pub(super) fn header_button(
    id: &'static str,
    label: impl Into<SharedString>,
    lead: Svg,
    enabled: bool,
    theme: &Theme,
) -> Stateful<Div> {
    let hover = theme.color(ColorToken::StateActive);
    div()
        .id(id)
        .flex()
        .flex_none()
        .items_center()
        .gap_1p5()
        .h(px(CONTROL))
        .px_2p5()
        .rounded(px(RADIUS_ROW))
        .text_size(px(FONT_BODY))
        .font_weight(FontWeight::MEDIUM)
        .text_color(theme.color(ColorToken::TextBase))
        .child(lead)
        .child(label.into())
        .when(enabled, |face| {
            face.cursor_pointer().hover(move |style| style.bg(hover))
        })
        .when(!enabled, |face| face.opacity(DISABLED))
}

fn arrow(turn: f32, theme: &Theme) -> Svg {
    icon(Icon::Arrow, ICON, theme.color(ColorToken::TextBase))
        .with_transformation(Transformation::rotate(radians(turn)))
}

fn counted(verb: &str, count: u32) -> String {
    match count {
        0 => verb.to_owned(),
        count => format!("{verb} {count}"),
    }
}

fn sentence(text: impl Into<SharedString>, alpha: f32, theme: &Theme) -> Div {
    div()
        .text_size(px(FONT_SMALL))
        .line_height(px(17.0))
        .text_color(ink(theme, alpha))
        .child(text.into())
}

pub fn pull_button(
    screen: &GitScreen,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> AnyElement {
    let theme = ActiveTheme::theme(cx);
    let open = window.use_keyed_state("git-pull-state", cx, |_, _| false);
    let was_open = *open.read(cx);
    let (count, tip, diverged) = match upstream(screen) {
        Upstream::NoBranch => (
            0,
            "Nothing to pull: you are not on a branch".to_owned(),
            None,
        ),
        Upstream::NoRemote => (
            0,
            "Nothing to pull: this repository has no remote".to_owned(),
            None,
        ),
        Upstream::Unpublished { branch, remote } => (
            0,
            format!("Nothing to pull: {branch} is not on {remote} yet"),
            None,
        ),
        Upstream::Tracked { branch, upstream } => match branch.behind {
            0 => (
                0,
                format!(
                    "Nothing to pull: {} already has everything on {upstream}",
                    branch.name
                ),
                None,
            ),
            behind => (
                behind,
                format!(
                    "Pull: bring {} from {upstream} into {}",
                    plural(behind as usize, "commit"),
                    branch.name
                ),
                (branch.ahead > 0).then(|| {
                    (
                        branch.name.clone(),
                        upstream.to_owned(),
                        branch.ahead,
                        behind,
                    )
                }),
            ),
        },
    };
    let face = header_button(
        "git-pull",
        counted("Pull", count),
        arrow(FRAC_PI_2, &theme),
        count > 0,
        &theme,
    );
    let face = match (count, diverged.is_some()) {
        (0, _) => face,
        (_, true) => toggles_on_press(face, {
            let open = open.clone();
            move |_, cx| set_open(&open, !was_open, cx)
        }),
        (_, false) => face.on_click(cx.listener(|screen, _: &ClickEvent, _, cx| {
            screen.run(Action::Pull(PullHow::Merge), cx)
        })),
    };
    let face = tooltip("git-pull-tip", face, Edge::Frame, tip, &theme, window, cx);
    let Some((name, upstream, ahead, behind)) = diverged else {
        return face.into_any_element();
    };
    let choice =
        |id: &'static str, how: PullHow, label: &'static str, kind: ButtonKind, said: String| {
            let open = open.clone();
            div()
                .flex()
                .items_start()
                .gap_3()
                .child(button(id, label, None, kind, &theme).on_click(cx.listener(
                    move |screen, _: &ClickEvent, _, cx| {
                        set_open(&open, false, cx);
                        screen.run(Action::Pull(how), cx)
                    },
                )))
                .child(sentence(said, T2, &theme).flex_1().pt_1())
        };
    let body = div()
        .flex()
        .flex_col()
        .gap_3()
        .on_mouse_down_out({
            let open = open.clone();
            move |_, _, cx| set_open(&open, false, cx)
        })
        .child(sentence(
            format!(
                "{name} and {upstream} have both moved: {} here, {} there. How should they come together?",
                plural(ahead as usize, "commit"),
                plural(behind as usize, "commit")
            ),
            T1,
            &theme,
        ))
        .child(choice(
            "git-pull-merge",
            PullHow::Merge,
            "Merge",
            ButtonKind::Primary,
            format!("Keep both histories as they are and join them with one merge commit on {name}."),
        ))
        .child(choice(
            "git-pull-rebase",
            PullHow::Rebase,
            "Rebase",
            ButtonKind::Plain,
            format!(
                "Replay your {} on top of {upstream}, so the history stays one straight line.",
                plural(ahead as usize, "commit")
            ),
        ));
    Popover::new("git-pull-pop", face)
        .open(was_open)
        .width(POP_WIDTH)
        .child(body)
        .into_any_element()
}

fn set_open(open: &Entity<bool>, to: bool, cx: &mut gpui::App) {
    open.update(cx, |open, cx| {
        *open = to;
        cx.notify();
    });
}

pub fn push_button(
    screen: &GitScreen,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> AnyElement {
    let theme = ActiveTheme::theme(cx);
    let pop = window.use_keyed_state("git-push-state", cx, |_, _| PushPop {
        open: false,
        held: None,
        _fire: None,
    });
    let (was_open, held) = {
        let state = pop.read(cx);
        (state.open, state.held)
    };
    let (enabled, count, tip, title, behind) = match upstream(screen) {
        Upstream::NoBranch => (
            false,
            0,
            "Nothing to push: you are not on a branch".to_owned(),
            String::new(),
            0,
        ),
        Upstream::NoRemote => (
            false,
            0,
            "Nothing to push: this repository has no remote to send to".to_owned(),
            String::new(),
            0,
        ),
        Upstream::Unpublished { branch, remote } => (
            true,
            0,
            format!("Push: publish {branch} to {remote} for the first time"),
            format!(
                "{branch} is not on {remote} yet. Push creates {remote}/{branch} and sends it there."
            ),
            0,
        ),
        Upstream::Tracked { branch, upstream } => {
            let commits = plural(branch.ahead as usize, "commit");
            match branch.ahead {
                0 => (
                    false,
                    0,
                    format!(
                        "Nothing to push: {upstream} already has every commit on {}",
                        branch.name
                    ),
                    String::new(),
                    branch.behind,
                ),
                ahead => (
                    true,
                    ahead,
                    format!("Push: send {commits} from {} to {upstream}", branch.name),
                    format!("These {commits} leave this machine for {upstream}:"),
                    branch.behind,
                ),
            }
        }
    };
    let face = header_button(
        "git-push",
        counted("Push", count),
        arrow(-FRAC_PI_2, &theme),
        enabled,
        &theme,
    );
    let face = if enabled {
        toggles_on_press(face, {
            let (pop, screen) = (pop.clone(), cx.entity());
            move |_, cx| {
                pop.update(cx, |pop, cx| {
                    pop.open = !was_open;
                    cx.notify();
                });
                if !was_open {
                    screen.update(cx, |screen, cx| screen.want_outgoing(cx));
                }
            }
        })
    } else {
        face
    };
    let face = tooltip("git-push-tip", face, Edge::Frame, tip, &theme, window, cx);
    let face = Popover::new("git-push-pop", face).width(POP_WIDTH);
    if !(enabled && was_open) {
        return face.into_any_element();
    }
    let close = {
        let pop = pop.clone();
        move |cx: &mut gpui::App| {
            pop.update(cx, |pop, cx| {
                pop.open = false;
                pop.held = None;
                pop._fire = None;
                cx.notify();
            })
        }
    };
    let rows: Vec<Div> = match screen.outgoing() {
        Loaded::Waiting => vec![sentence("Reading the commits to send", T2, &theme)],
        Loaded::Failed(said) => vec![sentence(said.clone(), T2, &theme)],
        Loaded::Ready(commits) => commits
            .iter()
            .map(|commit| commit_row(commit, &theme))
            .collect(),
    };
    let list = div()
        .id("git-push-list")
        .flex()
        .flex_col()
        .max_h(px(LIST_MAX))
        .overflow_y_scroll()
        .children(rows);
    let ending = if behind > 0 {
        refused(behind, held, &pop, &close, &theme, window, cx)
    } else {
        let close = close.clone();
        div()
            .flex()
            .justify_end()
            .child(
                button("git-push-send", "Push", None, ButtonKind::Primary, &theme).on_click(
                    cx.listener(move |screen, _: &ClickEvent, _, cx| {
                        close(cx);
                        screen.run(Action::Push { lease: false }, cx)
                    }),
                ),
            )
            .into_any_element()
    };
    let body = div()
        .flex()
        .flex_col()
        .gap_3()
        .on_mouse_down_out({
            let close = close.clone();
            move |_, _, cx| close(cx)
        })
        .child(sentence(title, T1, &theme))
        .child(list)
        .child(ending);
    face.open(true).child(body).into_any_element()
}

fn commit_row(commit: &Commit, theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap_2()
        .py_1()
        .text_size(px(FONT_SMALL))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .text_color(ink(theme, T1))
                .child(commit.subject.clone()),
        )
        .child(
            div()
                .flex_none()
                .text_color(ink(theme, T3))
                .child(short(&commit.sha).to_owned()),
        )
}

fn refused(
    behind: u32,
    held: Option<Instant>,
    pop: &Entity<PushPop>,
    close: &(impl Fn(&mut gpui::App) + Clone + 'static),
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> AnyElement {
    let danger = theme.color(ColorToken::StatusDanger);
    let progress = held.map_or(0.0, |since| {
        (since.elapsed().as_secs_f32() / FORCE_HOLD.as_secs_f32()).min(1.0)
    });
    if held.is_some() {
        window.request_animation_frame();
    }
    let release = {
        let pop = pop.clone();
        move |cx: &mut gpui::App| {
            pop.update(cx, |pop, cx| {
                if pop.held.is_some() {
                    pop.held = None;
                    pop._fire = None;
                    cx.notify();
                }
            })
        }
    };
    let press = {
        let (pop, close) = (pop.clone(), close.clone());
        cx.listener(move |_, _: &MouseDownEvent, _, cx| {
            let close = close.clone();
            let timer = cx.background_executor().timer(FORCE_HOLD);
            let fire = cx.spawn(async move |this, cx| {
                timer.await;
                this.update(cx, |screen, cx| {
                    close(cx);
                    screen.run(Action::Push { lease: true }, cx)
                })
            });
            pop.update(cx, |pop, cx| {
                pop.held = Some(Instant::now());
                pop._fire = Some(fire);
                cx.notify();
            });
        })
    };
    let force = div()
        .id("git-push-force")
        .relative()
        .flex()
        .items_center()
        .justify_center()
        .h(px(CONTROL))
        .px_3()
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .overflow_hidden()
        .bg(tint(danger, 0.14))
        .text_size(px(FONT_BODY))
        .font_weight(FontWeight::MEDIUM)
        .text_color(danger)
        .child(div().absolute().inset_0().bg(tint(danger, 0.45 * progress)))
        .child(div().relative().child(if held.is_some() {
            "Keep holding to force push"
        } else {
            "Hold to force push"
        }))
        .on_mouse_down(MouseButton::Left, press)
        .on_mouse_up(MouseButton::Left, {
            let release = release.clone();
            move |_: &MouseUpEvent, _, cx| release(cx)
        })
        .on_mouse_up_out(MouseButton::Left, {
            let release = release.clone();
            move |_: &MouseUpEvent, _, cx| release(cx)
        })
        .on_hover(move |hovered, _, cx| {
            if !hovered {
                release(cx)
            }
        });
    let close = close.clone();
    div()
        .flex()
        .flex_col()
        .gap_3()
        .child(sentence(
            format!(
                "The remote has {} you do not have, so a normal push would be refused. Pull them first.",
                plural(behind as usize, "commit")
            ),
            T1,
            theme,
        ))
        .child(sentence(
            "Force push replaces the remote branch with yours. It uses --force-with-lease, so it stops if someone pushed after your last fetch.",
            T2,
            theme,
        ))
        .child(
            div()
                .flex()
                .justify_end()
                .gap_2()
                .child(force)
                .child(
                    button("git-push-pull", "Pull", None, ButtonKind::Primary, theme).on_click(
                        cx.listener(move |screen, _: &ClickEvent, _, cx| {
                            close(cx);
                            screen.run(Action::Pull(PullHow::Merge), cx)
                        }),
                    ),
                ),
        )
        .into_any_element()
}
