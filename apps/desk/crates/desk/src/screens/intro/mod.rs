pub mod chrome;
pub mod effects;
mod fixture;
pub mod paint;

use std::sync::Arc;

use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Div, FontWeight, Image, IntoElement, Render,
    SharedString, Stateful, Window, div, img, prelude::*, px, relative,
};

use effects::hero;
use fixture::{
    ACCOUNT, CHECKOUT, EFFORT, MODEL, PROJECTS, Run, TELL_ACCOUNT, TELL_ALL_SESSIONS, TELL_ATTACH,
    TELL_BRANCH, TELL_CHECKOUT, TELL_FOLDER, TELL_MODEL, TELL_SESSION,
};
use paint::{
    BACKDROP, BRANCH, CARET, DOWN, FOLDER, LIVE, MONO, PLUS, POP, SEND, T3, WARN, glyph, hex, jpeg,
    load_fonts, medium, ringed, shadow, spacer, text, tint, white,
};

const STREET: &[u8] = include_bytes!("../../../../../assets/intro/street.jpg");
const ROOM: &[u8] = include_bytes!("../../../../../assets/intro/room.jpg");
const DUSK: &[u8] = include_bytes!("../../../../../assets/intro/dusk.jpg");
const STREET_CARD: &[u8] = include_bytes!("../../../../../assets/intro/card-street.jpg");
const ROOM_CARD: &[u8] = include_bytes!("../../../../../assets/intro/card-room.jpg");
const DUSK_CARD: &[u8] = include_bytes!("../../../../../assets/intro/card-dusk.jpg");
const COMPOSER_TOP: f32 = 330.0;
const COMPOSER_WIDTH: f32 = 740.0;
const LINK_INK: f32 = 0.72;
const CHEVRON_INK: f32 = 0.43;

#[derive(Clone, Copy, PartialEq, Eq)]
enum Menu {
    Closed,
    Projects,
    Resume,
}

struct Intro {
    backdrop: Arc<Image>,
    image: Arc<Image>,
    behind_card: Arc<Image>,
    project: usize,
    menu: Menu,
    told: Option<SharedString>,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    load_fonts(cx)?;
    let (image, behind_card, menu) = match board {
        None | Some("IHOME-1") => (STREET, STREET_CARD, Menu::Closed),
        Some("IHOME-2") => (ROOM, ROOM_CARD, Menu::Closed),
        Some("IHOME-3") => (DUSK, DUSK_CARD, Menu::Closed),
        Some("S-HOME-1") => (STREET, STREET_CARD, Menu::Resume),
        Some("S-HOME-2") => (STREET, STREET_CARD, Menu::Projects),
        Some(other) => {
            return Err(format!(
                "the intro draws IHOME-1 to 3, S-HOME-1 and S-HOME-2, not {other}"
            ));
        }
    };
    Ok(cx
        .new(|_| Intro {
            backdrop: jpeg(BACKDROP),
            image: jpeg(image),
            behind_card: jpeg(behind_card),
            project: 0,
            menu,
            told: None,
        })
        .into())
}

fn link(id: &'static str, icon: &str, label: impl Into<SharedString>, scale: f32) -> Stateful<Div> {
    div()
        .id(id)
        .flex()
        .items_center()
        .gap(px(6.0))
        .cursor_pointer()
        .text_color(white(LINK_INK))
        .child(glyph(icon, 13.0, white(LINK_INK), scale))
        .child(label.into())
        .child(glyph(DOWN, 13.0, white(CHEVRON_INK), scale))
}

fn caption(label: impl Into<SharedString>) -> Div {
    text(10.0, 10.0, white(0.45), label.into().to_uppercase())
        .font_weight(FontWeight::SEMIBOLD)
        .px(px(10.0))
        .py(px(6.0))
}

fn row(id: impl Into<gpui::ElementId>) -> Stateful<Div> {
    div()
        .id(id.into())
        .flex()
        .items_center()
        .gap(px(10.0))
        .px(px(10.0))
        .py(px(7.0))
        .rounded(px(8.0))
        .cursor_pointer()
}

fn divider() -> Div {
    div().h(px(1.0)).mx(px(6.0)).my(px(4.0)).bg(white(0.07))
}

fn pop(width: f32) -> Div {
    ringed(12.0, 0.12)
        .absolute()
        .right_0()
        .w(px(width))
        .p(px(6.0))
        .bg(POP)
        .shadow(shadow(25.0, 22.0, 0.6))
        .text_size(px(13.0))
        .flex()
        .flex_col()
}

impl Intro {
    fn tell(
        message: &'static str,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.told = Some(message.into());
            this.menu = Menu::Closed;
            cx.notify();
        }
    }

    fn toggle(menu: Menu) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.menu = if this.menu == menu {
                Menu::Closed
            } else {
                menu
            };
            cx.notify();
        }
    }

    fn card(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let project = &PROJECTS[self.project];
        div()
            .relative()
            .rounded(px(18.0))
            .shadow(shadow(70.0, 24.0, 0.4))
            .child(
                div()
                    .absolute()
                    .inset_0()
                    .rounded(px(18.0))
                    .overflow_hidden()
                    .child(img(self.behind_card.clone()).size_full())
                    .child(div().absolute().inset_0().bg(tint(0x18171e, 0.55))),
            )
            .pt(px(14.0))
            .pr(px(12.0))
            .pb(px(10.0))
            .pl(px(16.0))
            .flex()
            .flex_col()
            .gap(px(20.0))
            .child(
                div()
                    .flex()
                    .items_center()
                    .text_size(px(15.0))
                    .line_height(px(22.0))
                    .text_color(white(0.45))
                    .child(div().w(px(1.5)).h(px(17.0)).mr(px(2.0)).bg(CARET))
                    .child(format!("Do anything in {}", project.name)),
            )
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .text_size(px(12.5))
                    .child(
                        div()
                            .id("attach")
                            .flex()
                            .items_center()
                            .justify_center()
                            .size(px(26.0))
                            .rounded(px(7.0))
                            .cursor_pointer()
                            .on_click(cx.listener(Self::tell(TELL_ATTACH)))
                            .child(glyph(PLUS, 16.0, white(0.45), scale)),
                    )
                    .child(spacer())
                    .child(
                        div()
                            .id("model")
                            .flex()
                            .items_center()
                            .gap(px(6.0))
                            .h(px(24.0))
                            .px(px(9.0))
                            .cursor_pointer()
                            .on_click(cx.listener(Self::tell(TELL_MODEL)))
                            .child(medium(12.5, 12.5, white(0.85), MODEL))
                            .child(medium(12.5, 12.5, white(T3), EFFORT))
                            .child(glyph(DOWN, 13.0, white(0.85 * 0.6), scale)),
                    )
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .justify_center()
                            .size(px(30.0))
                            .rounded(px(15.0))
                            .bg(white(0.92))
                            .child(glyph(SEND, 16.0, hex(0x111111), scale)),
                    ),
            )
            .child(ringed(18.0, 0.16).absolute().inset_0())
    }

    fn resume(&self, cx: &mut Context<Self>) -> Div {
        let project = &PROJECTS[self.project];
        pop(432.0)
            .top(relative(1.0))
            .mt(px(6.0))
            .child(caption(format!("Sessions in {}", project.name)))
            .children(project.sessions.iter().enumerate().map(|(index, session)| {
                let dot = match session.run {
                    Run::Working => LIVE,
                    Run::Finished => white(0.3),
                    Run::Stopped => WARN,
                };
                row(("session", index))
                    .items_start()
                    .on_click(cx.listener(Self::tell(TELL_SESSION)))
                    .child(
                        div()
                            .flex_none()
                            .size(px(6.0))
                            .mt(px(7.0))
                            .rounded(px(3.0))
                            .bg(dot),
                    )
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .line_height(px(18.0))
                            .flex()
                            .flex_col()
                            .child(
                                div()
                                    .flex()
                                    .items_center()
                                    .gap(px(7.0))
                                    .child(session.name)
                                    .when(index == 0, |line| {
                                        line.child(
                                            text(10.5, 15.0, hex(0xc4d4f6), "Latest")
                                                .px(px(6.0))
                                                .py(px(1.0))
                                                .rounded(px(5.0))
                                                .bg(tint(0x9db8f0, 0.16)),
                                        )
                                    })
                                    .child(spacer())
                                    .child(text(11.5, 18.0, white(T3), session.when)),
                            )
                            .child(
                                div()
                                    .text_size(px(12.0))
                                    .text_color(white(T3))
                                    .overflow_hidden()
                                    .whitespace_nowrap()
                                    .text_ellipsis()
                                    .child(session.prompt),
                            ),
                    )
            }))
            .child(divider())
            .child(
                row("all-sessions")
                    .text_color(white(0.6))
                    .on_click(cx.listener(Self::tell(TELL_ALL_SESSIONS)))
                    .child("All sessions")
                    .child(spacer())
                    .child(
                        text(11.0, 16.0, white(0.5), "ctrl r")
                            .font_family(MONO)
                            .font_weight(FontWeight::MEDIUM)
                            .px(px(6.0))
                            .py(px(1.0))
                            .rounded(px(5.0))
                            .bg(white(0.08)),
                    ),
            )
    }

    fn projects(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        pop(332.0)
            .top(px(26.0))
            .child(caption("Recent projects"))
            .children(PROJECTS.iter().enumerate().map(|(index, project)| {
                row(("project", index))
                    .when(index == self.project, |row| row.bg(white(0.08)))
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.project = index;
                        this.menu = Menu::Closed;
                        cx.notify();
                    }))
                    .child(
                        div()
                            .flex()
                            .flex_none()
                            .items_center()
                            .justify_center()
                            .size(px(22.0))
                            .rounded(px(7.0))
                            .bg(white(0.1))
                            .child(
                                text(11.0, 11.0, white(0.9), &project.name[..1])
                                    .font_weight(FontWeight::SEMIBOLD),
                            ),
                    )
                    .child(
                        div()
                            .flex_1()
                            .flex()
                            .flex_col()
                            .line_height(px(16.0))
                            .child(project.name)
                            .child(text(11.0, 16.0, white(T3), project.path).font_family(MONO)),
                    )
                    .child(text(11.5, 16.0, white(T3), project.when))
            }))
            .child(divider())
            .child(
                row("open-folder")
                    .on_click(cx.listener(Self::tell(TELL_FOLDER)))
                    .child(glyph(FOLDER, 16.0, white(0.9), scale))
                    .child("Open a folder"),
            )
    }
}

impl Render for Intro {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let project = &PROJECTS[self.project];
        let latest = project.sessions[0].name;
        let column = div()
            .relative()
            .w(px(COMPOSER_WIDTH))
            .flex()
            .flex_col()
            .gap(px(10.0))
            .text_size(px(12.5))
            .child(
                div()
                    .flex()
                    .justify_end()
                    .gap(px(16.0))
                    .child(
                        link("account", r#"<rect x="2.5" y="4" width="11" height="8" rx="1.5"/><path d="M6 14h4"/>"#, ACCOUNT, scale)
                            .on_click(cx.listener(Self::tell(TELL_ACCOUNT))),
                    )
                    .child(
                        link("project", FOLDER, project.name, scale)
                            .on_click(cx.listener(Self::toggle(Menu::Projects))),
                    ),
            )
            .child(self.card(scale, cx))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(16.0))
                    .px(px(6.0))
                    .child(
                        link("checkout", FOLDER, CHECKOUT, scale)
                            .on_click(cx.listener(Self::tell(TELL_CHECKOUT))),
                    )
                    .child(
                        link("branch", BRANCH, project.branch, scale)
                            .on_click(cx.listener(Self::tell(TELL_BRANCH))),
                    )
                    .child(spacer())
                    .child(
                        div()
                            .id("resume")
                            .flex()
                            .items_center()
                            .gap(px(6.0))
                            .cursor_pointer()
                            .text_color(white(LINK_INK))
                            .on_click(cx.listener(Self::toggle(Menu::Resume)))
                            .child(glyph(
                                r#"<circle cx="8" cy="8" r="5.5"/><path d="M8 5v3l2 1.5"/>"#,
                                13.0,
                                white(LINK_INK),
                                scale,
                            ))
                            .child("Resume")
                            .child(div().text_color(white(T3)).child(format!("· {latest}")))
                            .child(glyph(DOWN, 13.0, white(CHEVRON_INK), scale)),
                    ),
            )
            .when(self.menu == Menu::Resume, |column| column.child(self.resume(cx)))
            .when(self.menu == Menu::Projects, |column| {
                column.child(self.projects(scale, cx))
            });
        let main = div().relative().flex_1().child(hero(&self.image)).child(
            div()
                .absolute()
                .inset_0()
                .flex()
                .flex_col()
                .items_center()
                .pt(px(COMPOSER_TOP))
                .child(column),
        );
        chrome::window(&self.backdrop, scale, main).children(self.told.clone().map(|message| {
            paint::toast(
                message,
                scale,
                cx.listener(|this, _: &ClickEvent, _, cx| {
                    this.told = None;
                    cx.notify();
                }),
            )
        }))
    }
}
