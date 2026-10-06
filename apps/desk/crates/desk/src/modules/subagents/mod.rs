mod chrome;
mod feed;
mod fixture;
mod paint;

use std::borrow::Cow;
use std::sync::Arc;

use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, Image, ImageFormat,
    IntoElement, Render, Window, div, prelude::*, px,
};

use chrome::{CONTROLS, Wire};
use fixture::{Agent, Event, State};
use paint::{SHELL, T3, glyph, ringed, white};

const BOARD: &str = "IWY-7";
const FOCUSED: &str = "S-WORK-5";
const FOCUS: &str = "go-dev 1";
const BACKDROP: &[u8] = include_bytes!("assets/backdrop.jpg");
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];
const PEOPLE: &str = r#"<circle cx="6" cy="6" r="2"/><circle cx="11" cy="10" r="2"/><path d="M3 13c.4-1.6 1.6-2.4 3-2.4M14 15c-.4-1.6-1.6-2.4-3-2.4"/>"#;
pub const MENTION: &str = "Mentions this in the chat as a reference the lead can read.";

struct Subagents {
    backdrop: Arc<Image>,
    agents: Vec<Agent>,
    events: Vec<Event>,
    open: [bool; 4],
    feed: Option<usize>,
    inactive_open: bool,
    tell: Option<&'static str>,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let focus = match board {
        None | Some(BOARD) => None,
        Some(FOCUSED) => Some(FOCUS),
        Some(other) => {
            return Err(format!(
                "the sub-agents module draws {BOARD} and {FOCUSED}, not {other}"
            ));
        }
    };
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the sub-agents module cannot load the Geist fonts: {error}"))?;
    let agents = fixture::agents();
    let events = fixture::events(&agents);
    let feed = match focus {
        None => None,
        Some(id) => Some(
            agents
                .iter()
                .position(|a| a.id == id)
                .ok_or(format!("the sub-agents fixture has no agent {id}"))?,
        ),
    };
    Ok(cx
        .new(|_| Subagents {
            backdrop: Arc::new(Image::from_bytes(ImageFormat::Jpeg, BACKDROP.to_vec())),
            agents,
            events,
            open: [true, true, true, false],
            feed,
            inactive_open: false,
            tell: None,
        })
        .into())
}

impl Subagents {
    fn summary(&self) -> String {
        let count = |state| self.agents.iter().filter(|a| a.state == state).count();
        format!(
            "{} working · {} asking · {} failed · {} finished",
            count(State::Run),
            count(State::Wait),
            count(State::Fail),
            count(State::Done)
        )
    }

    fn list(&self, scale: f32, cx: &Context<Self>) -> Div {
        let all = div()
            .id("all-activity")
            .flex()
            .items_center()
            .gap(px(10.0))
            .h(px(32.0))
            .px(px(10.0))
            .rounded(px(9.0))
            .text_size(px(13.0))
            .when(self.feed.is_none(), |row| row.bg(white(0.07)))
            .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                this.feed = None;
                cx.notify();
            }))
            .child(
                div()
                    .flex_1()
                    .font_weight(gpui::FontWeight::SEMIBOLD)
                    .child("All activity"),
            )
            .child(paint::mono(
                11.5,
                17.0,
                white(T3),
                self.events.len().to_string(),
            ));
        let mut list = div().flex().flex_col().py(px(8.0)).px(px(6.0)).child(all);
        for (group, (state, label)) in State::GROUPS.into_iter().enumerate() {
            let members: Vec<usize> = (0..self.agents.len())
                .filter(|&i| self.agents[i].state == state)
                .collect();
            if members.is_empty() {
                continue;
            }
            let open = self.open[group];
            list = list.child(
                div()
                    .id(("group", group))
                    .flex()
                    .items_center()
                    .gap(px(6.0))
                    .pt(px(14.0))
                    .px(px(10.0))
                    .pb(px(6.0))
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.open[group] = !this.open[group];
                        cx.notify();
                    }))
                    .child(glyph(
                        if open { paint::DOWN } else { paint::RIGHT },
                        11.0,
                        white(0.45),
                        scale,
                    ))
                    .child(paint::semibold(10.0, 10.0, white(0.45), label.to_uppercase()).flex_1())
                    .child(
                        paint::mono(10.0, 10.0, white(0.45), members.len().to_string())
                            .font_weight(gpui::FontWeight::SEMIBOLD),
                    ),
            );
            if open {
                list = list.children(members.into_iter().map(|i| self.agent_row(i, scale, cx)));
            }
        }
        list
    }

    fn agent_row(&self, index: usize, scale: f32, cx: &Context<Self>) -> AnyElement {
        let agent = &self.agents[index];
        div()
            .id(("agent", index))
            .flex()
            .items_center()
            .gap(px(10.0))
            .py(px(6.0))
            .px(px(10.0))
            .rounded(px(9.0))
            .when(self.feed == Some(index), |row| row.bg(white(0.07)))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.feed = Some(index);
                cx.notify();
            }))
            .child(feed::avatar(agent.kind, 22.0, Some(agent.state), scale))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .flex_col()
                    .child(
                        div()
                            .flex()
                            .gap(px(8.0))
                            .child(
                                paint::text(
                                    13.0,
                                    17.0,
                                    paint::hex(agent.kind.rgb()),
                                    agent.id.clone(),
                                )
                                .flex_1(),
                            )
                            .child(paint::text(11.0, 17.0, white(T3), agent.time())),
                    )
                    .child(
                        div()
                            .overflow_hidden()
                            .whitespace_nowrap()
                            .text_ellipsis()
                            .text_size(px(11.5))
                            .line_height(px(17.0))
                            .text_color(white(T3))
                            .child(agent.line()),
                    ),
            )
            .into_any_element()
    }
}

impl Render for Subagents {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let control = |at: usize, body: Div| -> AnyElement {
            body.id(("control", at))
                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    if at == 0 {
                        this.tell = Some("The sidebar toggle is not drawn in this module window.");
                    } else {
                        this.tell = CONTROLS.get(at).copied();
                    }
                    cx.notify();
                }))
                .into_any_element()
        };
        let inactive = |body: Div| -> AnyElement {
            body.id("inactive")
                .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                    this.inactive_open = !this.inactive_open;
                    cx.notify();
                }))
                .into_any_element()
        };
        let wire = Wire {
            scale,
            inactive_open: self.inactive_open,
            control: &control,
            inactive: &inactive,
        };
        let shell = ringed(12.0, white(0.06))
            .flex_1()
            .mb(px(8.0))
            .flex()
            .flex_col()
            .min_h_0()
            .px(px(3.0))
            .pb(px(3.0))
            .bg(SHELL)
            .child(
                div()
                    .flex()
                    .flex_none()
                    .items_center()
                    .gap(px(6.0))
                    .h(px(28.0))
                    .pl(px(9.0))
                    .pr(px(6.0))
                    .child(glyph(PEOPLE, 13.0, white(0.55), scale))
                    .child(paint::medium(12.0, 12.0, white(0.55), "Sub-agents"))
                    .child(paint::spacer())
                    .child(paint::text(12.0, 16.0, white(T3), self.summary())),
            )
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .rounded(px(11.0))
                    .overflow_hidden()
                    .child(
                        div()
                            .id("agents")
                            .w(px(340.0))
                            .flex_none()
                            .overflow_y_scroll()
                            .child(self.list(scale, cx)),
                    )
                    .child(div().w(px(1.0)).flex_none().bg(paint::black(0.35)))
                    .child(
                        div()
                            .id("feed")
                            .flex_1()
                            .min_w_0()
                            .overflow_y_scroll()
                            .child(feed::feed(self, scale, cx)),
                    ),
            );
        let root = chrome::window(&self.backdrop, &wire, shell);
        match self.tell {
            None => root,
            Some(message) => root.child(chrome::toast(
                message,
                div()
                    .id("dismiss")
                    .flex()
                    .flex_none()
                    .items_center()
                    .justify_center()
                    .size(px(22.0))
                    .rounded(px(7.0))
                    .text_color(white(0.45))
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                        this.tell = None;
                        cx.notify();
                    }))
                    .child("×")
                    .into_any_element(),
                scale,
            )),
        }
    }
}
