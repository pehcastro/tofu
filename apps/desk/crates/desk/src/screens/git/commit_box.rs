use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::form::TextArea;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::size::{FONT_SMALL, RADIUS_BADGE, T2, T3};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::metrics::{HAIRLINE, ICON_TINY};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AppContext, ClickEvent, Context, Div, Entity, KeyDownEvent, Stateful, Window, div, prelude::*,
    px,
};

use super::changes::files;
use super::model::{Action, GitScreen, Told};

const HINT: &str = "Say what this commit changes";
const MESSAGE_LINES: usize = 6;
const PAD: f32 = 12.0;
const GAP: f32 = 8.0;
const CHECK: f32 = 14.0;
const CHECK_RADIUS: f32 = 4.0;
const DISABLED: f32 = 0.45;

pub(super) struct Draft {
    message: Entity<TextArea>,
    amend: bool,
}

pub(super) fn draft(window: &mut Window, cx: &mut Context<GitScreen>) -> Entity<Draft> {
    window.use_keyed_state("git-commit-draft", cx, |window, cx| Draft {
        message: cx.new(|cx| TextArea::new(HINT.into(), window, cx).max_lines(MESSAGE_LINES)),
        amend: false,
    })
}

pub(super) fn render(
    screen: &GitScreen,
    draft: &Entity<Draft>,
    staged: usize,
    theme: &Theme,
    window: &mut Window,
    cx: &mut Context<GitScreen>,
) -> Div {
    let (message, amend) = {
        let draft = draft.read(cx);
        (draft.message.clone(), draft.amend)
    };
    let ready = staged > 0 || amend;
    let branch = screen.snapshot().branch.as_ref();
    let name = branch.map_or("this branch", |branch| branch.name.as_str());
    let upstream = branch
        .and_then(|branch| branch.upstream.as_deref())
        .unwrap_or("its remote");
    let what = match (amend, staged) {
        (true, 0) => format!("reword the last commit on {name}"),
        (true, _) => format!("fold {} into the last commit on {name}", files(staged)),
        (false, _) => format!("record {} as a new commit on {name}", files(staged)),
    };
    let (commit_tip, push_tip) = match ready {
        true => (
            format!("Commit: {what}. Ctrl+Enter does the same."),
            format!("Commit & Push: {what}, then send it to {upstream}"),
        ),
        false => (NOTHING_STAGED.to_owned(), NOTHING_STAGED.to_owned()),
    };
    let commit_button = wire(
        button(
            "git-commit",
            "Commit",
            Some(Glyph::Check),
            ButtonKind::Primary,
            theme,
        ),
        draft,
        false,
        ready,
        cx,
    );
    let push_button = wire(
        button(
            "git-commit-push",
            "Commit & Push",
            None,
            ButtonKind::Plain,
            theme,
        ),
        draft,
        true,
        ready,
        cx,
    );
    let toggle = draft.clone();
    let check = div()
        .id("git-amend")
        .flex()
        .items_center()
        .gap(px(GAP))
        .cursor_pointer()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T2))
        .on_click(move |_, _, cx| {
            toggle.update(cx, |draft, cx| {
                draft.amend = !draft.amend;
                cx.notify();
            })
        })
        .child(
            div()
                .flex()
                .flex_none()
                .items_center()
                .justify_center()
                .size(px(CHECK))
                .rounded(px(CHECK_RADIUS))
                .border(px(HAIRLINE))
                .border_color(ink(theme, T3))
                .when(amend, |check| {
                    check
                        .border_color(theme.color(ColorToken::ButtonPrimary))
                        .bg(theme.color(ColorToken::ButtonPrimary))
                        .child(glyph(
                            Glyph::Check,
                            ICON_TINY,
                            theme.color(ColorToken::ButtonPrimaryText),
                        ))
                }),
        )
        .child("Amend last commit");
    let keys = draft.clone();
    div()
        .flex()
        .flex_col()
        .flex_none()
        .gap(px(GAP))
        .p(px(PAD))
        .border_t(px(HAIRLINE))
        .border_color(theme.color(ColorToken::Separator))
        .on_key_down(
            cx.listener(move |screen, event: &KeyDownEvent, _, cx| {
                let pressed = &event.keystroke;
                if pressed.modifiers.secondary() && pressed.key == "enter" {
                    cx.stop_propagation();
                    commit(screen, &keys, false, ready, cx);
                }
            }),
        )
        .child(
            div()
                .rounded(px(RADIUS_BADGE))
                .bg(theme.color(ColorToken::FieldFill))
                .child(message),
        )
        .child(tooltip(
            "git-amend-tip",
            check,
            Edge::Frame,
            "Amend: put this message and the staged changes into the last commit instead of making a new one",
            theme,
            window,
            cx,
        ))
        .child(
            div()
                .flex()
                .gap(px(GAP))
                .child(tooltip(
                    "git-commit-tip",
                    commit_button.flex_1(),
                    Edge::Frame,
                    commit_tip,
                    theme,
                    window,
                    cx,
                ))
                .child(tooltip(
                    "git-commit-push-tip",
                    push_button.flex_1(),
                    Edge::Frame,
                    push_tip,
                    theme,
                    window,
                    cx,
                )),
        )
}

const NOTHING_STAGED: &str =
    "Nothing is staged: stage a file first, or tick Amend last commit to reword it.";

fn wire(
    face: Stateful<Div>,
    draft: &Entity<Draft>,
    push: bool,
    ready: bool,
    cx: &mut Context<GitScreen>,
) -> Stateful<Div> {
    if !ready {
        return face.opacity(DISABLED).cursor_default();
    }
    let draft = draft.clone();
    face.on_click(
        cx.listener(move |screen, _: &ClickEvent, _, cx| commit(screen, &draft, push, ready, cx)),
    )
}

fn commit(
    screen: &mut GitScreen,
    draft: &Entity<Draft>,
    push: bool,
    ready: bool,
    cx: &mut Context<GitScreen>,
) {
    let (area, amend) = {
        let draft = draft.read(cx);
        (draft.message.clone(), draft.amend)
    };
    let message = area.read(cx).text().trim().to_owned();
    let refused = match (ready, message.is_empty()) {
        (false, _) => Some(NOTHING_STAGED),
        (true, true) => Some("Write a commit message first: say what this commit changes."),
        (true, false) => None,
    };
    if let Some(said) = refused {
        return screen.tell(
            Told::Failed {
                said: said.to_owned(),
                fix: None,
            },
            cx,
        );
    }
    screen.run(
        Action::Commit {
            message,
            amend,
            push,
        },
        cx,
    );
    area.update(cx, |area, cx| area.clear(cx));
    draft.update(cx, |draft, cx| {
        draft.amend = false;
        cx.notify();
    });
}
