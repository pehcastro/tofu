mod fixture;
#[cfg(feature = "screen-library")]
use super::library::kit;
#[cfg(not(feature = "screen-library"))]
#[path = "../library/kit.rs"]
#[expect(
    dead_code,
    reason = "the kit is shared with the library screen, which uses parts the classifier does not"
)]
mod kit;
mod ledger;
mod sandbox;

use desk_ui::components::paint::{ink, tint};
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Render, Rgba, SharedString, Window, prelude::*,
    rgb,
};

use fixture::{LEDGER, SHIPPED_ASK, SHIPPED_DENY, Verdict};
use kit::frame;

const ALLOW: u32 = 0x86e0b3;
const ASK: u32 = 0xe8c98a;
const DENY: u32 = 0xf1737d;
const DENY_TEXT: u32 = 0xee8a8f;
const SHADOW_FILL: u32 = 0xb9a6ea;
const SHADOW_TEXT: u32 = 0xcfc2f2;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    kit::load_fonts(cx)?;
    let page = match board {
        None | Some("IJEV-1") => Page::Ledger,
        Some("IJEV-2") => Page::Sandbox,
        Some(other) => {
            return Err(format!(
                "the classifier screen draws IJEV-1 and IJEV-2, not {other}"
            ));
        }
    };
    Ok(cx.new(|_| Classifier::new(page)).into())
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Page {
    Ledger,
    Sandbox,
}

pub struct Classifier {
    page: Page,
    point: usize,
    row: usize,
    labels: [Option<Verdict>; LEDGER.len()],
    ask: i32,
    deny: i32,
    told: Option<SharedString>,
}

impl Classifier {
    fn new(page: Page) -> Self {
        Classifier {
            page,
            point: 0,
            row: 0,
            labels: LEDGER.map(|entry| entry.label),
            ask: SHIPPED_ASK,
            deny: SHIPPED_DENY,
            told: None,
        }
    }

    fn tell(
        message: &'static str,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.told = Some(message.into());
            cx.notify();
        }
    }
}

fn verdict_colors(verdict: Verdict) -> (Rgba, Rgba) {
    let color = rgb(match verdict {
        Verdict::Allow => ALLOW,
        Verdict::Ask => ASK,
        Verdict::Deny => DENY,
    });
    (
        tint(
            color,
            if verdict == Verdict::Allow {
                0.12
            } else {
                0.14
            },
        ),
        color,
    )
}

fn mode_colors(enforced: bool, theme: &desk_ui::theme::Theme) -> (Rgba, Rgba) {
    if enforced {
        (ink(theme, 0.12), rgb(0xffffff))
    } else {
        (tint(rgb(SHADOW_FILL), 0.14), rgb(SHADOW_TEXT))
    }
}

impl Render for Classifier {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = match self.page {
            Page::Ledger => self.ledger(&theme, cx),
            Page::Sandbox => self.sandbox(&theme, cx),
        };
        frame(
            &theme,
            body,
            self.told.clone(),
            cx.listener(|this, _: &ClickEvent, _, cx| {
                this.told = None;
                cx.notify();
            }),
        )
    }
}
