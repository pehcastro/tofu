mod fixture;

use std::sync::Arc;

use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, FontWeight, Image, IntoElement, Render,
    SharedString, Window, div, prelude::*, px,
};

use super::intro::effects::hero;
use super::intro::{chrome, paint};
use fixture::{IMPORT, START, TAGLINE, TELL_IMPORT, TELL_START, TITLE};
use paint::{BACKDROP, hex, jpeg, load_fonts, shadow, white};

const WELCOME: &[u8] = include_bytes!("../../../../../assets/intro/welcome.jpg");
const WELCOME_BOTTOM: f32 = 150.0;

struct Onboarding {
    backdrop: Arc<Image>,
    image: Arc<Image>,
    told: Option<SharedString>,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    load_fonts(cx)?;
    match board {
        None | Some("IONB-1") => {}
        Some(other) => return Err(format!("the onboarding draws IONB-1, not {other}")),
    }
    Ok(cx
        .new(|_| Onboarding {
            backdrop: jpeg(BACKDROP),
            image: jpeg(WELCOME),
            told: None,
        })
        .into())
}

impl Onboarding {
    fn tell(
        message: &'static str,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.told = Some(message.into());
            cx.notify();
        }
    }
}

impl Render for Onboarding {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let welcome = div()
            .absolute()
            .inset_0()
            .flex()
            .flex_col()
            .items_center()
            .justify_end()
            .pb(px(WELCOME_BOTTOM))
            .gap(px(18.0))
            .text_center()
            .child(
                div()
                    .text_size(px(96.0))
                    .line_height(px(96.0))
                    .font_weight(FontWeight::BOLD)
                    .text_color(white(1.0))
                    .child(TITLE),
            )
            .child(
                div()
                    .max_w(px(560.0))
                    .text_size(px(17.0))
                    .line_height(px(26.0))
                    .text_color(white(0.78))
                    .child(TAGLINE),
            )
            .child(
                div()
                    .id("start")
                    .mt(px(10.0))
                    .h(px(42.0))
                    .px(px(26.0))
                    .flex()
                    .items_center()
                    .rounded(px(21.0))
                    .bg(white(0.92))
                    .shadow(shadow(40.0, 10.0, 0.35))
                    .text_color(hex(0x111111))
                    .text_size(px(14.0))
                    .font_weight(FontWeight::SEMIBOLD)
                    .cursor_pointer()
                    .on_click(cx.listener(Self::tell(TELL_START)))
                    .child(START),
            )
            .child(
                div()
                    .id("import")
                    .text_size(px(12.5))
                    .text_color(white(0.55))
                    .cursor_pointer()
                    .on_click(cx.listener(Self::tell(TELL_IMPORT)))
                    .child(IMPORT),
            );
        let main = div()
            .relative()
            .flex_1()
            .child(hero(&self.image))
            .child(welcome);
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
