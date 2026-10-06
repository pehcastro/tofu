mod chrome;
mod fixture;
mod paint;

use std::borrow::Cow;
use std::sync::Arc;

use gpui::{
    AnyElement, AnyView, App, AppContext, BoxShadow, Context, Div, Image, ImageFormat, IntoElement,
    Render, SharedString, Stateful, Window, div, point, prelude::*, px,
};

use fixture::{SHELLS, SUMMARY, Shell};
use paint::{
    ARROW, CLOSE, PROMPT, T2, T3, TRACE, TRACE_MARK, glyph, hex, medium, mono, spacer, square,
    text, tint, white,
};

const BACKDROP: &[u8] = include_bytes!("../chat/assets/backdrop.jpg");
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

const OPEN_SAYS: &str = "Opens this shell as a Terminal tile you can type into.";
const KILL_SAYS: &str =
    "Kills the process after a confirm; the agent that started it is told it ended by your hand.";
const MENTION_SAYS: &str = "Mentions this in the chat as a reference the lead can read.";
const CLOSE_TAB_SAYS: &str =
    "Closes this tab. The process keeps running; it stays in the shell list.";

struct Shells {
    backdrop: Arc<Image>,
    selected: usize,
    menu: bool,
    told: Option<SharedString>,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let menu = match board {
        None | Some("IWY-9") => false,
        Some("S-WORK-7") => true,
        Some(other) => {
            return Err(format!(
                "the shells module draws IWY-9 and S-WORK-7, not {other}"
            ));
        }
    };
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the shells module cannot load the Geist fonts: {error}"))?;
    let backdrop = Arc::new(Image::from_bytes(ImageFormat::Jpeg, BACKDROP.to_vec()));
    Ok(cx
        .new(|_| Shells {
            backdrop,
            selected: 0,
            menu,
            told: None,
        })
        .into())
}

impl Shells {
    fn teller(
        &self,
        id: impl Into<gpui::ElementId>,
        says: &'static str,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        div()
            .id(id)
            .cursor_pointer()
            .on_click(cx.listener(move |shells, _, _, cx| {
                shells.told = Some(says.into());
                cx.notify();
            }))
    }

    fn tab(&self, index: usize, shell: &Shell, scale: f32, cx: &mut Context<Self>) -> Div {
        let on = index == self.selected;
        let ink = if on { white(1.0) } else { white(0.5) };
        div()
            .flex()
            .flex_none()
            .items_center()
            .h(px(31.0))
            .pr(px(3.0))
            .rounded_t(px(9.0))
            .when(on, |tab| tab.bg(white(0.05)).shadow(top_line()))
            .child(
                div()
                    .id(("shell-tab", index))
                    .cursor_pointer()
                    .flex()
                    .items_center()
                    .gap(px(7.0))
                    .h(px(31.0))
                    .pl(px(10.0))
                    .pr(px(6.0))
                    .on_click(cx.listener(move |shells, _, _, cx| {
                        shells.selected = index;
                        cx.notify();
                    }))
                    .child(
                        div()
                            .flex_none()
                            .size(px(6.0))
                            .rounded(px(3.0))
                            .bg(shell.state.dot()),
                    )
                    .child(mono(12.0, 12.0, ink, format!("shell-{}", index + 1))),
            )
            .child(
                self.teller(("shell-close", index), CLOSE_TAB_SAYS, cx)
                    .flex()
                    .items_center()
                    .justify_center()
                    .size(px(18.0))
                    .rounded(px(5.0))
                    .when(on, |close| {
                        close.child(glyph(CLOSE, 11.0, white(0.45), scale))
                    }),
            )
    }

    fn details(&self, shell: &Shell, scale: f32, cx: &mut Context<Self>) -> Div {
        let quiet = white(T3);
        let word = |body: &'static str| text(11.5, 23.0, quiet, body);
        div()
            .flex()
            .items_center()
            .gap(px(12.0))
            .pt(px(10.0))
            .px(px(14.0))
            .pb(px(4.0))
            .child(
                mono(12.0, 23.0, white(T2), format!("$ {}", shell.command))
                    .flex_1()
                    .min_w_0()
                    .overflow_hidden(),
            )
            .child(
                div()
                    .flex()
                    .flex_none()
                    .items_center()
                    .gap(px(10.0))
                    .child(mono(11.5, 23.0, quiet, shell.pid))
                    .child(
                        div()
                            .flex()
                            .child(word("by "))
                            .child(text(11.5, 23.0, shell.ink, shell.by)),
                    )
                    .when(!shell.port.is_empty(), |row| {
                        row.child(mono(11.5, 23.0, white(T2), shell.port))
                    })
                    .child(word(shell.age))
                    .child(self.teller("shell-open", OPEN_SAYS, cx).child(text(
                        11.5,
                        23.0,
                        white(T2),
                        "open",
                    )))
                    .child(self.teller("shell-kill", KILL_SAYS, cx).child(text(
                        11.5,
                        23.0,
                        white(T2),
                        "kill",
                    )))
                    .child(
                        self.teller("shell-mention", MENTION_SAYS, cx)
                            .opacity(0.7)
                            .child(glyph(TRACE_MARK, 13.0, TRACE, scale)),
                    ),
            )
    }

    fn panel(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let shell = &SHELLS[self.selected];
        div()
            .flex_1()
            .min_h_0()
            .mb(px(8.0))
            .flex()
            .flex_col()
            .px(px(3.0))
            .pb(px(3.0))
            .rounded(px(12.0))
            .bg(hex(0x19181f))
            .shadow(vec![ring(white(0.06))])
            .child(
                div()
                    .flex()
                    .flex_none()
                    .items_center()
                    .gap(px(6.0))
                    .h(px(28.0))
                    .pl(px(9.0))
                    .pr(px(6.0))
                    .child(glyph(PROMPT, 13.0, white(0.55), scale))
                    .child(medium(12.0, 12.0, white(0.55), "Shells"))
                    .child(spacer())
                    .child(text(12.0, 12.0, white(T3), SUMMARY)),
            )
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .flex_col()
                    .overflow_hidden()
                    .rounded(px(11.0))
                    .relative()
                    .shadow(top_line())
                    .child(
                        div()
                            .flex()
                            .flex_none()
                            .items_end()
                            .h(px(36.0))
                            .pl(px(12.0))
                            .pr(px(8.0))
                            .children(
                                SHELLS
                                    .iter()
                                    .enumerate()
                                    .map(|(index, shell)| self.tab(index, shell, scale, cx)),
                            ),
                    )
                    .child(
                        div()
                            .flex_1()
                            .min_h_0()
                            .flex()
                            .flex_col()
                            .bg(white(0.045))
                            .child(self.details(shell, scale, cx))
                            .child(
                                div()
                                    .flex_1()
                                    .min_h_0()
                                    .pt(px(4.0))
                                    .px(px(14.0))
                                    .pb(px(14.0))
                                    .children(shell.output.iter().map(|line| {
                                        mono(12.0, 19.0, white(0.68), *line).h(px(19.0))
                                    })),
                            ),
                    )
                    .when(self.menu, |inner| inner.child(self.menu(cx))),
            )
    }

    fn menu(&self, cx: &mut Context<Self>) -> Div {
        div()
            .absolute()
            .left(px(8.0))
            .top(px(38.0))
            .w(px(440.0))
            .p(px(6.0))
            .rounded(px(12.0))
            .bg(tint(0x1e1d24, 0.94))
            .shadow(vec![
                ring(white(0.12)),
                BoxShadow {
                    color: tint(0x000000, 0.6).into(),
                    offset: point(px(0.0), px(22.0)),
                    blur_radius: px(50.0),
                    spread_radius: px(0.0),
                    inset: false,
                },
            ])
            .children(SHELLS.iter().enumerate().map(|(index, shell)| {
                div()
                    .id(("shell-menu-row", index))
                    .cursor_pointer()
                    .flex()
                    .items_center()
                    .gap(px(10.0))
                    .h(px(37.0))
                    .px(px(10.0))
                    .rounded(px(8.0))
                    .when(index == self.selected, |row| row.bg(white(0.07)))
                    .on_click(cx.listener(move |shells, _, _, cx| {
                        shells.selected = index;
                        shells.menu = false;
                        cx.notify();
                    }))
                    .child(
                        div()
                            .flex_none()
                            .size(px(6.0))
                            .rounded(px(3.0))
                            .bg(shell.state.dot()),
                    )
                    .child(
                        mono(12.0, 23.0, white(0.9), format!("shell-{}", index + 1))
                            .flex_none()
                            .w(px(58.0)),
                    )
                    .child(
                        mono(12.0, 23.0, white(T2), shell.command)
                            .flex_1()
                            .min_w_0()
                            .overflow_hidden(),
                    )
                    .child(text(12.0, 23.0, shell.ink, shell.by))
            }))
    }

    fn toast(&self, scale: f32, cx: &mut Context<Self>) -> Option<AnyElement> {
        let said = self.told.clone()?;
        Some(
            div()
                .absolute()
                .left_0()
                .right_0()
                .bottom(px(44.0))
                .flex()
                .justify_center()
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(10.0))
                        .max_w(px(620.0))
                        .py(px(9.0))
                        .pl(px(14.0))
                        .pr(px(10.0))
                        .rounded(px(12.0))
                        .bg(tint(0x1e1d24, 0.96))
                        .shadow(vec![ring(white(0.12))])
                        .text_size(px(13.0))
                        .line_height(px(18.0))
                        .child(glyph(ARROW, 13.0, white(T3), scale))
                        .child(div().flex_1().min_w_0().child(said))
                        .child(
                            text(11.0, 18.0, white(T3), "not drawn yet")
                                .px(px(7.0))
                                .rounded(px(999.0))
                                .bg(white(0.06)),
                        )
                        .child(
                            square(22.0, 7.0)
                                .id("toast-dismiss")
                                .cursor_pointer()
                                .on_click(cx.listener(|shells, _, _, cx| {
                                    shells.told = None;
                                    cx.notify();
                                }))
                                .child(glyph(CLOSE, 11.0, white(0.45), scale)),
                        ),
                )
                .into_any_element(),
        )
    }
}

fn ring(color: gpui::Rgba) -> BoxShadow {
    BoxShadow {
        color: color.into(),
        offset: point(px(0.0), px(0.0)),
        blur_radius: px(0.0),
        spread_radius: px(1.0),
        inset: true,
    }
}

fn top_line() -> Vec<BoxShadow> {
    vec![BoxShadow {
        color: white(0.07).into(),
        offset: point(px(0.0), px(1.0)),
        blur_radius: px(0.0),
        spread_radius: px(0.0),
        inset: true,
    }]
}

impl Render for Shells {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let panel = self.panel(scale, cx);
        let toast = self.toast(scale, cx);
        chrome::window(&self.backdrop, scale, panel, toast)
    }
}
