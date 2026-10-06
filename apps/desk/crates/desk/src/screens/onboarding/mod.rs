mod fixture;
mod setup;

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
const WELCOME_BLURRED: &[u8] = include_bytes!("../../../../../assets/intro/welcome-blur.jpg");
const WELCOME_BOTTOM: f32 = 150.0;

#[derive(Default)]
struct Connected {
    claude: bool,
    codex: bool,
    key: bool,
}

struct Onboarding {
    backdrop: Arc<Image>,
    image: Arc<Image>,
    blurred: Arc<Image>,
    step: usize,
    connected: Connected,
    key_open: bool,
    provider: usize,
    classifier: usize,
    project: Option<usize>,
    told: Option<SharedString>,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    load_fonts(cx)?;
    let (step, key_open) = match board {
        None | Some("IONB-1") => (0, false),
        Some("S-ONB-1") => (1, false),
        Some("S-ONB-2") => (2, true),
        Some("S-ONB-3") => (3, false),
        Some(other) => {
            return Err(format!(
                "the onboarding draws IONB-1 and S-ONB-1 to 3, not {other}"
            ));
        }
    };
    Ok(cx
        .new(|_| Onboarding {
            backdrop: jpeg(BACKDROP),
            image: jpeg(WELCOME),
            blurred: jpeg(WELCOME_BLURRED),
            step,
            connected: Connected::default(),
            key_open,
            provider: 0,
            classifier: 0,
            project: None,
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

    fn welcome(cx: &mut Context<Self>) -> gpui::Div {
        div()
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
            )
    }
}

impl Render for Onboarding {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let over = if self.step == 0 {
            Self::welcome(cx)
        } else {
            self.setup(scale, cx)
        };
        let main = div()
            .relative()
            .flex_1()
            .child(hero(&self.image))
            .child(over);
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
