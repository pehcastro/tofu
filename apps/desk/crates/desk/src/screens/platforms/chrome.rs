use gpui::{
    Context, Div, FontWeight, div, linear_color_stop, linear_gradient, prelude::*, px, relative,
};

use super::fixture::{
    ACCOUNT_INITIAL, INACTIVE, INACTIVE_SESSIONS, PALETTE_KEY, PROJECT, PROJECT_INITIAL,
    PROJECT_LINE, RUNNING, SESSION, STATUS_BRANCH, STATUS_CONTEXT, STATUS_QUOTA, STATUS_RIGHT,
    TABS,
};
use super::glyph::{Glyph, glyph};
use super::paint::{FAINT, GREEN, SOFT, STRONG, WARN, dropping, ink, medium, rgb, strong, text};
use super::{Action, Platform, Platforms, Pop};

const TITLE_HEIGHT: f32 = 40.0;
const STATUS_HEIGHT: f32 = 30.0;
const ROW_HEIGHT: f32 = 32.0;
const BAR_WIDTH: f32 = 40.0;
const LIGHTS: [(u8, u8, u8); 3] = [(255, 95, 87), (254, 188, 46), (40, 200, 64)];
const CONTROL_GLYPHS: [Glyph; 3] = [Glyph::Minimize, Glyph::Maximize, Glyph::Close];
const WORKSPACE_GLYPHS: [Glyph; 3] = [Glyph::Chat, Glyph::Code, Glyph::Data];
const WORKSPACE_CLOSES: [&str; 3] = [
    "",
    "Closes the editor workspace; its tiles are kept and come back with ctrl shift t.",
    "Closes the data workspace; its tiles are kept and come back with ctrl shift t.",
];
const NEW_WORKSPACE: &str = "Opens a new workspace with one empty tile: pick a module, or start from a preset (work 1+2, editor, data).";
const PALETTE: &str = "Opens the command palette: files, sessions, screens, settings and commands in one search (alt k).";
const NEW_SESSION: &str =
    "Starts a new session in notes-app and opens its chat in the work workspace.";
const SWITCH_SESSION: &str =
    "Switches the desk to quiet-amber-heron: its chat and feeds replace these, the tabs stay.";
const REOPEN: [&str; 3] = [
    "Reopens fond-sandy-mink where it stopped; nothing runs until you send.",
    "Reopens tidy-ochre-wren where it stopped; nothing runs until you send.",
    "Reopens crisp-azure-swift where it stopped; nothing runs until you send.",
];
const BELL_POP_WIDTH: f32 = 330.0;
const ACCOUNT_POP_WIDTH: f32 = 250.0;

fn square(size: f32) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(size))
}

impl Platforms {
    fn controls(&self, cx: &mut Context<Self>) -> Div {
        let platform = self.platform;
        let gap = match platform {
            Platform::Windows => 2.0,
            Platform::Linux | Platform::Macos => 8.0,
        };
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(gap))
            .children(
                CONTROL_GLYPHS
                    .into_iter()
                    .zip(LIGHTS)
                    .zip(platform.control_tells())
                    .map(|((shape, (r, g, b)), tell)| {
                        let control = match platform {
                            Platform::Windows => square(28.0)
                                .w(px(34.0))
                                .rounded(px(8.0))
                                .child(glyph(shape, 13.0, ink(0.62))),
                            Platform::Linux => square(20.0)
                                .rounded_full()
                                .bg(ink(0.08))
                                .child(glyph(shape, 11.0, ink(0.6))),
                            Platform::Macos => square(12.0).rounded_full().bg(rgb(r, g, b, 1.0)),
                        };
                        Self::hot(control, Action::Tell(tell), cx)
                    }),
            )
    }

    fn workspace_tab(
        on: bool,
        shape: Glyph,
        name: &'static str,
        close: &'static str,
        cx: &mut Context<Self>,
    ) -> Div {
        let tone = if on { ink(1.0) } else { SOFT };
        let tail = if on {
            square(18.0).child(glyph(Glyph::Pin, 11.0, FAINT))
        } else {
            Self::hot(square(18.0), Action::Tell(close), cx)
        };
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(7.0))
            .h(px(28.0))
            .pl(px(10.0))
            .pr(px(3.0))
            .rounded(px(8.0))
            .when(on, |tab| tab.bg(ink(0.1)))
            .child(glyph(shape, 13.0, tone))
            .child(medium(name, 13.0, tone))
            .child(tail)
    }

    fn popover(&self) -> Option<Div> {
        let (rows, wide, caption): (&[(&str, &str)], f32, &str) = match self.pop {
            Pop::None => return None,
            Pop::Bell => (&super::BELL_ROWS, BELL_POP_WIDTH, "Needs you"),
            Pop::Account => (&super::ACCOUNT_ROWS, ACCOUNT_POP_WIDTH, "pehcastro"),
        };
        Some(
            div()
                .absolute()
                .top(relative(1.0))
                .right_0()
                .mt(px(4.0))
                .w(px(wide))
                .p(px(6.0))
                .rounded(px(12.0))
                .bg(rgb(30, 29, 36, 0.94))
                .shadow(vec![super::paint::ring(ink(0.12))])
                .child(div().px(px(10.0)).pt(px(8.0)).pb(px(6.0)).child(medium(
                    caption,
                    10.0,
                    ink(0.45),
                )))
                .children(rows.iter().map(|(title, line)| {
                    div()
                        .px(px(10.0))
                        .py(px(7.0))
                        .rounded(px(8.0))
                        .flex()
                        .gap(px(8.0))
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .child(text(*title, 13.0, STRONG).flex_shrink_1().truncate()),
                        )
                        .child(text(*line, 12.0, FAINT))
                })),
        )
    }

    fn opener(&self, pop: Pop, body: Div, cx: &mut Context<Self>) -> Div {
        let opened = (self.pop == pop).then(|| self.popover()).flatten();
        Self::hot(body.relative(), Action::Open(pop), cx).children(opened)
    }

    pub(super) fn title_bar(&self, cx: &mut Context<Self>) -> Div {
        let platform = self.platform;
        let toggle = Self::hot(
            square(28.0)
                .rounded(px(7.0))
                .child(glyph(Glyph::Sidebar, 16.0, ink(0.45))),
            Action::Side,
            cx,
        );
        let left = div()
            .flex()
            .flex_shrink_1()
            .min_w_0()
            .overflow_hidden()
            .items_center()
            .gap(px(8.0))
            .w(px(platform.left_width(self.side)))
            .pl(px(10.0))
            .when(platform == Platform::Macos, |left| {
                left.pl(px(14.0)).child(self.controls(cx).mr(px(10.0)))
            })
            .child(toggle)
            .when(self.side, |left| {
                left.child(text("tofu", 13.0, ink(0.75)).font_weight(FontWeight::BOLD))
            });
        let tabs: Vec<Div> = TABS
            .into_iter()
            .zip(WORKSPACE_GLYPHS)
            .zip(WORKSPACE_CLOSES)
            .enumerate()
            .map(|(index, ((name, shape), close))| {
                Self::workspace_tab(index == 0, shape, name, close, cx)
            })
            .collect();
        let bell = self.opener(
            Pop::Bell,
            square(28.0)
                .child(glyph(Glyph::Bell, 16.0, ink(0.45)))
                .child(
                    div()
                        .absolute()
                        .inset_0()
                        .flex()
                        .justify_end()
                        .p(px(6.0))
                        .child(div().size(px(6.0)).rounded_full().bg(WARN)),
                ),
            cx,
        );
        let account = self.opener(
            Pop::Account,
            square(28.0).child(
                square(22.0)
                    .rounded_full()
                    .bg(linear_gradient(
                        135.0,
                        linear_color_stop(rgb(107, 122, 143, 1.0), 0.0),
                        linear_color_stop(rgb(58, 66, 80, 1.0), 1.0),
                    ))
                    .child(strong(ACCOUNT_INITIAL, 10.0, STRONG)),
            ),
            cx,
        );
        div()
            .h(px(TITLE_HEIGHT))
            .flex()
            .flex_none()
            .items_center()
            .gap(px(3.0))
            .pr(px(10.0))
            .child(left)
            .child(
                div()
                    .flex()
                    .flex_1()
                    .min_w_0()
                    .items_center()
                    .gap(px(3.0))
                    .overflow_hidden()
                    .children(tabs)
                    .child(Self::hot(
                        square(28.0).w(px(26.0)).child(medium("+", 13.0, SOFT)),
                        Action::Tell(NEW_WORKSPACE),
                        cx,
                    )),
            )
            .child(
                dropping(TITLE_HEIGHT).child(Self::hot(
                    div()
                        .flex()
                        .flex_none()
                        .items_center()
                        .gap(px(8.0))
                        .h(px(28.0))
                        .px(px(10.0))
                        .child(glyph(Glyph::Search, 13.0, SOFT))
                        .child(medium(PALETTE_KEY, 13.0, FAINT)),
                    Action::Tell(PALETTE),
                    cx,
                )),
            )
            .child(bell)
            .child(account)
            .when(platform != Platform::Macos, |bar| {
                bar.child(self.controls(cx).ml(px(6.0)))
            })
    }

    fn session_row(lead: Div, name: &'static str, tail: &'static str, on: bool) -> Div {
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(8.0))
            .h(px(ROW_HEIGHT))
            .px(px(10.0))
            .rounded(px(9.0))
            .when(on, |row| row.bg(ink(0.08)))
            .child(lead)
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .child(text(name, 13.0, if on { STRONG } else { ink(0.6) }).truncate()),
            )
            .child(text(tail, 11.5, FAINT))
    }

    pub(super) fn sidebar(&self, cx: &mut Context<Self>) -> Div {
        let running = RUNNING.iter().map(|session| {
            let row = Self::session_row(
                text("●", 8.0, GREEN),
                session.name,
                session.state,
                session.on,
            );
            if session.on {
                row
            } else {
                Self::hot(row, Action::Tell(SWITCH_SESSION), cx)
            }
        });
        let running: Vec<Div> = running.collect();
        let inactive: Vec<Div> = INACTIVE_SESSIONS
            .into_iter()
            .zip(REOPEN)
            .filter(|_| self.inactive)
            .map(|((name, age), tell)| {
                Self::hot(
                    Self::session_row(div().w(px(13.0)), name, age, false),
                    Action::Tell(tell),
                    cx,
                )
            })
            .collect();
        div()
            .w(px(super::SIDE_WIDTH))
            .flex_shrink_1()
            .min_w(px(super::SIDE_LEAST))
            .flex()
            .flex_col()
            .min_h_0()
            .overflow_hidden()
            .pl(px(8.0))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(10.0))
                    .px(px(10.0))
                    .py(px(8.0))
                    .child(square(26.0).rounded(px(8.0)).bg(ink(0.1)).child(strong(
                        PROJECT_INITIAL,
                        12.0,
                        STRONG,
                    )))
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .flex()
                            .flex_col()
                            .child(strong(PROJECT, 14.0, STRONG))
                            .child(text(PROJECT_LINE, 12.0, FAINT)),
                    )
                    .child(glyph(Glyph::Down, 13.0, FAINT)),
            )
            .child(
                div()
                    .flex()
                    .items_center()
                    .h(px(28.0))
                    .px(px(10.0))
                    .mt(px(8.0))
                    .child(div().flex_1().child(medium("Running", 11.5, FAINT)))
                    .child(Self::hot(
                        square(18.0).child(medium("+", 11.5, FAINT)),
                        Action::Tell(NEW_SESSION),
                        cx,
                    )),
            )
            .children(running)
            .child(Self::hot(
                div()
                    .flex()
                    .flex_none()
                    .items_center()
                    .gap(px(8.0))
                    .h(px(ROW_HEIGHT))
                    .px(px(10.0))
                    .child(glyph(
                        if self.inactive {
                            Glyph::Down
                        } else {
                            Glyph::Right
                        },
                        13.0,
                        FAINT,
                    ))
                    .child(div().flex_1().child(text("Inactive", 13.0, FAINT)))
                    .child(text(INACTIVE, 11.5, FAINT)),
                Action::Inactive,
                cx,
            ))
            .children(inactive)
    }

    pub(super) fn status_bar() -> Div {
        let (label, used, share) = STATUS_CONTEXT;
        let (quota, window, percent) = STATUS_QUOTA;
        let item = || {
            div()
                .flex()
                .flex_none()
                .items_center()
                .gap(px(6.0))
                .px(px(8.0))
        };
        div()
            .h(px(STATUS_HEIGHT))
            .flex()
            .flex_none()
            .items_center()
            .gap(px(2.0))
            .px(px(10.0))
            .overflow_hidden()
            .child(
                item()
                    .child(glyph(Glyph::Branch, 13.0, ink(0.5)))
                    .child(medium(STATUS_BRANCH.0, 12.0, ink(0.5)))
                    .child(medium(STATUS_BRANCH.1, 12.0, FAINT)),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .px(px(8.0))
                    .child(medium(SESSION, 12.0, ink(0.5)).truncate()),
            )
            .child(
                dropping(STATUS_HEIGHT)
                    .child(
                        item()
                            .child(medium(label, 12.0, ink(0.5)))
                            .child(
                                div()
                                    .w(px(BAR_WIDTH))
                                    .h(px(5.0))
                                    .rounded(px(3.0))
                                    .bg(ink(0.08))
                                    .child(
                                        div()
                                            .h_full()
                                            .w(relative(share))
                                            .rounded(px(3.0))
                                            .bg(ink(0.6)),
                                    ),
                            )
                            .child(medium(used, 12.0, ink(0.5))),
                    )
                    .child(
                        item()
                            .child(medium(quota, 12.0, ink(0.5)))
                            .child(medium(window, 12.0, FAINT))
                            .child(medium(percent, 12.0, ink(0.5))),
                    )
                    .children(
                        STATUS_RIGHT
                            .iter()
                            .map(|status| item().child(medium(*status, 12.0, ink(0.5)))),
                    ),
            )
    }
}
