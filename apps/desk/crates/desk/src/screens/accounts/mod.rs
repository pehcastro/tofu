#[cfg(not(feature = "screen-settings"))]
#[path = "../settings/board.rs"]
pub mod board;
#[cfg(feature = "screen-settings")]
pub use crate::screens::settings::board;
mod fixture;

use board::inner;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{caption, outer_card};
use desk_ui::components::chip::mono;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::popover;
use desk_ui::components::paint::{glyph, ink, tint};
use desk_ui::components::size::T3;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::ICON_SMALL;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Div, FontWeight, Render, Rgba, SharedString,
    Window, div, prelude::*, px, relative,
};

use fixture::{ACCOUNTS, ACCOUNTS_NOTE, ADD_GROUPS, MORE_TELL, ROLES, ROLES_NOTE, State};

const DOT_INK: f32 = 0.9;
const SHELL_HEADER: f32 = 28.0;
const TITLE: f32 = 19.0;
const DESC: f32 = 13.0;
const SMALL: f32 = 12.0;
const ROW_LINE: f32 = 18.0;
const NOTE_LINE: f32 = 17.0;
const ROW_RADIUS: f32 = 10.0;
const ROW_FILL: f32 = 0.18;
const STATE_TEXT: f32 = 11.5;
const STATE_TINT: f32 = 0.12;
const STATE_WARM_TINT: f32 = 0.14;
const STANDBY_FILL: f32 = 0.06;
const STANDBY_INK: f32 = 0.5;
const MORE: f32 = 24.0;
const MORE_DOT: f32 = 1.6;
const POP_WIDTH: f32 = 440.0;
const POP_TOP: f32 = 40.0;
const POP_TEXT: f32 = 13.5;
const POP_TITLE: f32 = 15.0;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    board::load_fonts(cx)?;
    match board {
        None | Some("ISET-2") => Ok(cx
            .new(|_| Accounts {
                picks: ROLES.map(|role| role.start),
                adding: false,
                told: None,
            })
            .into()),
        Some(other) => Err(format!("the accounts screen draws ISET-2, not {other}")),
    }
}

struct Accounts {
    picks: [usize; 6],
    adding: bool,
    told: Option<&'static str>,
}

impl Accounts {
    fn tell(
        message: &'static str,
        cx: &mut Context<Self>,
    ) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
        cx.listener(move |this, _: &ClickEvent, _, cx| {
            this.told = Some(message);
            cx.notify();
        })
    }

    fn toggle_add(cx: &mut Context<Self>) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
        cx.listener(|this, _: &ClickEvent, _, cx| {
            this.adding = !this.adding;
            cx.notify();
        })
    }

    fn accounts(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let rows: Vec<_> = ACCOUNTS
            .iter()
            .enumerate()
            .map(|(at, account)| {
                let (fill, text) = state_colors(account.state, theme);
                row(theme)
                    .child(
                        div()
                            .flex_1()
                            .line_height(px(ROW_LINE))
                            .child(
                                div().flex().gap_1().child(account.source).child(
                                    div()
                                        .text_color(ink(theme, T3))
                                        .child(format!("\u{b7} {}", account.name)),
                                ),
                            )
                            .child(small(account.kind, theme)),
                    )
                    .child(
                        div()
                            .py(px(1.0))
                            .px_2()
                            .rounded_full()
                            .bg(fill)
                            .text_size(px(STATE_TEXT))
                            .line_height(relative(1.3))
                            .text_color(text)
                            .child(account.state.label()),
                    )
                    .child(
                        div()
                            .id(("more", at))
                            .flex()
                            .items_center()
                            .justify_center()
                            .gap(px(2.4))
                            .size(px(MORE))
                            .rounded_md()
                            .cursor_pointer()
                            .on_click(Self::tell(MORE_TELL, cx))
                            .children((0..3).map(|_| {
                                div()
                                    .size(px(MORE_DOT))
                                    .rounded_full()
                                    .bg(ink(theme, DOT_INK))
                            })),
                    )
            })
            .collect();
        column(
            "Accounts",
            "stored in ~/.tofu/agent.db",
            rows,
            ACCOUNTS_NOTE,
            theme,
        )
    }

    fn roles(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let rows: Vec<_> = ROLES
            .iter()
            .enumerate()
            .map(|(at, role)| {
                let pick = self.picks.get(at).copied().unwrap_or(role.start);
                let model = role.options.get(pick).copied().unwrap_or_default();
                row(theme)
                    .id(("role", at))
                    .cursor_pointer()
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        if let Some(pick) = this.picks.get_mut(at) {
                            *pick = (*pick + 1) % role.options.len();
                            cx.notify();
                        }
                    }))
                    .child(
                        div()
                            .flex_1()
                            .line_height(px(ROW_LINE))
                            .child(role.role)
                            .child(small(role.desc, theme)),
                    )
                    .child(
                        div()
                            .font_family(mono(theme))
                            .text_size(px(SMALL))
                            .child(model),
                    )
                    .child(glyph(Glyph::Chevron, ICON_SMALL, ink(theme, T3)))
                    .into_any_element()
            })
            .collect();
        column(
            "Who runs on what",
            "click a model to change it",
            rows,
            ROLES_NOTE,
            theme,
        )
    }

    fn add(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let groups = ADD_GROUPS.iter().flat_map(|group| {
            [
                caption(group.caption, theme).into_any_element(),
                div()
                    .flex()
                    .flex_wrap()
                    .gap_1p5()
                    .children(group.sources.iter().map(|(name, tell)| {
                        button(*name, name, None, ButtonKind::Plain, theme)
                            .on_click(Self::tell(tell, cx))
                    }))
                    .into_any_element(),
            ]
        });
        let groups: Vec<_> = groups.collect();
        div()
            .absolute()
            .top(px(POP_TOP))
            .left_0()
            .right_0()
            .flex()
            .justify_center()
            .child(
                popover(theme)
                    .w(px(POP_WIDTH))
                    .py_4()
                    .px(px(18.0))
                    .flex()
                    .flex_col()
                    .gap_3()
                    .text_size(px(POP_TEXT))
                    .child(
                        div()
                            .font_weight(FontWeight::SEMIBOLD)
                            .text_size(px(POP_TITLE))
                            .child("Add an account"),
                    )
                    .children(groups)
                    .child(
                        div().flex().justify_end().child(
                            button("close-add", "Close", None, ButtonKind::Plain, theme)
                                .on_click(Self::toggle_add(cx)),
                        ),
                    ),
            )
    }
}

fn state_colors(state: State, theme: &Theme) -> (Rgba, Rgba) {
    match state {
        State::InUse => {
            let live = theme.color(ColorToken::StatusLive);
            (tint(live, STATE_TINT), live)
        }
        State::Away => {
            let warn = theme.color(ColorToken::StatusWarn);
            (tint(warn, STATE_WARM_TINT), warn)
        }
        State::Standby => (ink(theme, STANDBY_FILL), ink(theme, STANDBY_INK)),
    }
}

fn small(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .text_size(px(SMALL))
        .text_color(ink(theme, T3))
        .child(text.into())
}

fn row(theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap_2p5()
        .py_2p5()
        .px_3()
        .rounded(px(ROW_RADIUS))
        .bg(tint(theme.color(ColorToken::Shadow), ROW_FILL))
}

fn column(
    title: &'static str,
    note: &'static str,
    rows: Vec<impl IntoElement>,
    footer: &'static str,
    theme: &Theme,
) -> Div {
    outer_card(theme)
        .flex_col()
        .flex_1()
        .min_w_0()
        .child(
            div()
                .flex()
                .flex_none()
                .items_center()
                .h(px(SHELL_HEADER))
                .pl(px(9.0))
                .pr_1p5()
                .child(caption(title, theme).flex_1())
                .child(small(note, theme)),
        )
        .child(
            inner(theme)
                .p_2()
                .gap_1()
                .text_size(px(DESC))
                .children(rows)
                .child(
                    small(footer, theme)
                        .py_2()
                        .px_1()
                        .line_height(px(NOTE_LINE)),
                ),
        )
}

impl Render for Accounts {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let title = div()
            .flex()
            .flex_none()
            .items_center()
            .gap_2p5()
            .px_1()
            .child(
                div()
                    .text_size(px(TITLE))
                    .line_height(relative(1.0))
                    .font_weight(FontWeight::SEMIBOLD)
                    .child("Accounts and models"),
            )
            .child(
                small(
                    "what pays for each model, and which model does which job",
                    &theme,
                )
                .text_size(px(DESC)),
            )
            .child(div().flex_1())
            .child(
                button(
                    "add-account",
                    "+ Add account",
                    None,
                    ButtonKind::Primary,
                    &theme,
                )
                .on_click(Self::toggle_add(cx)),
            );
        let body = div()
            .flex_1()
            .min_h_0()
            .flex()
            .flex_col()
            .gap_2p5()
            .mb_2()
            .pt_1p5()
            .px_1p5()
            .overflow_hidden()
            .child(title)
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .relative()
                    .flex()
                    .gap_2p5()
                    .child(self.accounts(&theme, cx))
                    .child(self.roles(&theme, cx))
                    .when(self.adding, |grid| grid.child(self.add(&theme, cx))),
            );
        board::root(
            &theme,
            body,
            self.told.map(Into::into),
            cx.listener(|this, _: &ClickEvent, _, cx| {
                this.told = None;
                cx.notify();
            }),
        )
    }
}
