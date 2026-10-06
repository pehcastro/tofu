mod chrome;
mod fixture;
mod page;
mod paint;

use std::borrow::Cow;
use std::sync::Arc;

use desk_core::limits::TOAST_LIFETIME;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Div, Image, ImageFormat, IntoElement, Render,
    Stateful, Task, Window, div, prelude::*, px,
};

use fixture::{Engine, FIRST_STEP, STEPS};
use paint::{CROSS, POP, T3, glyph, ringed, shadow, text, white};

const BACKDROP: &[u8] = include_bytes!("../chat/assets/backdrop.jpg");
const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];
const LAST_STEP: usize = STEPS.len() - 1;

#[derive(Clone, Copy, PartialEq, Eq)]
enum Tab {
    Page,
    Console,
}

#[derive(Clone, Copy)]
enum Act {
    Tell(&'static str),
    Untell,
    Side,
    Inactive,
    Bell,
    Account,
    Engine(Engine),
    Tab(Tab),
    Pick,
    Mention,
    Prev,
    Next,
    Take,
}

pub struct Browser {
    backdrop: Arc<Image>,
    side: bool,
    inactive: bool,
    bell: bool,
    account: bool,
    engine: Engine,
    tab: Tab,
    step: usize,
    took: bool,
    picking: bool,
    picked: bool,
    told: Option<Told>,
}

struct Told {
    said: &'static str,
    _expiry: Task<gpui::Result<()>>,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let (tab, picking) = match board {
        None | Some("IBROWSER-1") => (Tab::Page, false),
        Some("S-BROWSER-1") => (Tab::Page, true),
        Some("S-BROWSER-2") => (Tab::Console, false),
        Some(other) => {
            return Err(format!(
                "the browser draws IBROWSER-1, S-BROWSER-1 and S-BROWSER-2, not {other}"
            ));
        }
    };
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the browser cannot load the Geist fonts: {error}"))?;
    let backdrop = Arc::new(Image::from_bytes(ImageFormat::Jpeg, BACKDROP.to_vec()));
    Ok(cx
        .new(|_| Browser {
            backdrop,
            side: true,
            inactive: false,
            bell: false,
            account: false,
            engine: Engine::Built,
            tab,
            step: FIRST_STEP,
            took: false,
            picking,
            picked: false,
            told: None,
        })
        .into())
}

impl Browser {
    fn apply(&mut self, act: Act, cx: &mut Context<Self>) {
        match act {
            Act::Tell(said) => {
                let expiry = cx.spawn(async move |this, cx| {
                    cx.background_executor().timer(TOAST_LIFETIME).await;
                    this.update(cx, |browser, cx| {
                        browser.told = None;
                        cx.notify();
                    })
                });
                self.told = Some(Told {
                    said,
                    _expiry: expiry,
                });
            }
            Act::Untell => self.told = None,
            Act::Side => self.side = !self.side,
            Act::Inactive => self.inactive = !self.inactive,
            Act::Bell => {
                self.bell = !self.bell;
                self.account = false;
            }
            Act::Account => {
                self.account = !self.account;
                self.bell = false;
            }
            Act::Engine(engine) => self.engine = engine,
            Act::Tab(tab) => self.tab = tab,
            Act::Pick => self.picking = !self.picking,
            Act::Mention => {
                self.picking = false;
                self.picked = true;
            }
            Act::Prev => self.step = self.step.saturating_sub(1),
            Act::Next => self.step = (self.step + 1).min(LAST_STEP),
            Act::Take => self.took = !self.took,
        }
    }

    fn hit(
        id: impl Into<gpui::ElementId>,
        el: Div,
        act: Act,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        el.id(id.into()).cursor_pointer().on_click(cx.listener(
            move |this, _: &ClickEvent, _, cx| {
                this.apply(act, cx);
                cx.notify();
            },
        ))
    }

    fn toast(&self, said: &'static str, scale: f32, cx: &mut Context<Self>) -> Div {
        div()
            .absolute()
            .left_0()
            .right_0()
            .bottom(px(62.0))
            .flex()
            .justify_center()
            .child(
                ringed(12.0, white(0.12))
                    .max_w(px(620.0))
                    .flex()
                    .items_center()
                    .gap(px(10.0))
                    .py(px(9.0))
                    .pl(px(14.0))
                    .pr(px(10.0))
                    .bg(POP)
                    .shadow(shadow(40.0, 18.0, 0.6))
                    .text_size(px(13.0))
                    .line_height(px(18.0))
                    .child(glyph(paint::RIGHT, 13.0, white(T3), scale))
                    .child(div().flex_1().child(said))
                    .child(
                        text(11.0, 16.0, white(T3), "not drawn yet")
                            .px(px(7.0))
                            .py(px(2.0))
                            .rounded_full()
                            .bg(white(0.06)),
                    )
                    .child(Self::hit(
                        "untell",
                        paint::square(22.0, 7.0).child(glyph(CROSS, 11.0, white(0.45), scale)),
                        Act::Untell,
                        cx,
                    )),
            )
    }
}

impl Render for Browser {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let main = div()
            .flex_1()
            .min_h_0()
            .mb(px(8.0))
            .flex()
            .gap(px(6.0))
            .child(
                self.chat_tile(scale, cx)
                    .flex_basis(px(395.8))
                    .flex_shrink_0(),
            )
            .child(self.browser_tile(scale, cx).flex_1().min_w_0());
        let toast = self
            .told
            .as_ref()
            .map(|told| told.said)
            .map(|said| self.toast(said, scale, cx));
        self.window(main, scale, cx).children(toast)
    }
}
