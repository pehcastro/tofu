mod agents;
mod chat;
mod chrome;
mod fixture;
mod glyph;
mod paint;

use std::borrow::Cow;

use gpui::{
    AnyView, App, AppContext, Context, Div, FontWeight, IntoElement, MouseButton, MouseDownEvent,
    Render, Window, div, prelude::*, px,
};

use fixture::STACK_BADGES;
use glyph::{Glyph, glyph};
use paint::{CAPTION, FAINT, SANS, SHELL, SOFT, STRONG, ink, medium, mono, rgb, ring, text};

const SIDE_WIDTH: f32 = 232.0;
const WINDOW: gpui::Rgba = rgb(17, 16, 22, 1.0);
const GAP: f32 = 6.0;
const CHAT_SHARE: f32 = 55.0;
const STACK_SHARE: f32 = 45.0;
const TILE_LEAST: f32 = 300.0;
const TILES_LEAST: f32 = 220.0;
const SIDE_LEAST: f32 = 160.0;
const TILE_TALL: f32 = 240.0;
const TOAST_WIDTH: f32 = 400.0;
const TOAST_BOTTOM: f32 = 52.0;
const STACK: [(&str, Glyph); 3] = [
    ("Sub-agents", Glyph::People),
    ("File edits", Glyph::File),
    ("Shells", Glyph::Terminal),
];
const STACK_MORE: &str =
    "Stacks another module in this tile: Terminal, Browser, Editor, or another view of a module.";
const CLOSES: &str =
    "Closes the window. clear-sable-eagle is still working, so tofu asks before it stops the turn.";
const BELL_ROWS: [(&str, &str); 3] = [
    (
        "quiet-amber-heron wants to run rm -rf build/",
        "waiting 2m · opens the approval",
    ),
    (
        "ts-dev finished: sort order is now by date",
        "12m · opens Sub-agents",
    ),
    ("claude-sub · work is back in 40m", "1h · opens Limits"),
];
const ACCOUNT_ROWS: [(&str, &str); 4] = [
    ("Accounts and models", "5 accounts"),
    ("Settings", "ctrl ,"),
    ("What's new in 0.5.1", ""),
    ("Docs", ""),
];

const FONTS: [&[u8]; 6] = [
    include_bytes!("../../../../../assets/fonts/Geist-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/Geist-SemiBold.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Regular.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-Medium.ttf"),
    include_bytes!("../../../../../assets/fonts/GeistMono-SemiBold.ttf"),
];

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Platform {
    Windows,
    Macos,
    Linux,
}

impl Platform {
    pub fn left_width(self, side: bool) -> f32 {
        match (side, self) {
            (true, _) => SIDE_WIDTH,
            (false, Platform::Macos) => 118.0,
            (false, Platform::Windows | Platform::Linux) => 50.0,
        }
    }

    fn radius(self) -> f32 {
        match self {
            Platform::Windows => 8.0,
            Platform::Macos | Platform::Linux => 14.0,
        }
    }

    fn control_tells(self) -> [&'static str; 3] {
        match self {
            Platform::Windows => [
                "Minimizes tofu to the taskbar.",
                "Maximizes the window. Hold the pointer here for the Windows snap layouts.",
                CLOSES,
            ],
            Platform::Macos => [
                CLOSES,
                "Minimizes tofu to the Dock.",
                "Enters full screen; the title bar slides away until the pointer reaches the top.",
            ],
            Platform::Linux => ["Minimizes tofu.", "Maximizes the window.", CLOSES],
        }
    }
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let platform = match board {
        None | Some("IWIN-1") => Platform::Windows,
        Some("IWIN-2") => Platform::Macos,
        Some("IWIN-3") => Platform::Linux,
        Some(other) => {
            return Err(format!(
                "the platforms screen draws IWIN-1, IWIN-2 and IWIN-3, not {other}"
            ));
        }
    };
    cx.text_system()
        .add_fonts(FONTS.iter().map(|font| Cow::Borrowed(*font)).collect())
        .map_err(|error| format!("the platforms screen could not load Geist: {error:#}"))?;
    Ok(cx
        .new(|_| Platforms {
            platform,
            side: true,
            inactive: false,
            tab: 0,
            sheet: false,
            pop: Pop::None,
            told: None,
        })
        .into())
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Pop {
    None,
    Bell,
    Account,
}

#[derive(Clone, Copy)]
enum Action {
    Tell(&'static str),
    Side,
    Inactive,
    Tab(usize),
    Sheet,
    Open(Pop),
}

pub struct Platforms {
    platform: Platform,
    side: bool,
    inactive: bool,
    tab: usize,
    sheet: bool,
    pop: Pop,
    told: Option<&'static str>,
}

impl Platforms {
    fn act(&mut self, action: Action) {
        let pop = self.pop;
        self.pop = Pop::None;
        match action {
            Action::Tell(message) => self.told = Some(message),
            Action::Side => self.side = !self.side,
            Action::Inactive => self.inactive = !self.inactive,
            Action::Tab(index) => self.tab = index,
            Action::Sheet => self.sheet = !self.sheet,
            Action::Open(next) => self.pop = if pop == next { Pop::None } else { next },
        }
    }

    fn hot(element: Div, action: Action, cx: &mut Context<Self>) -> Div {
        element.cursor_pointer().on_mouse_down(
            MouseButton::Left,
            cx.listener(move |screen, _: &MouseDownEvent, _, cx| {
                screen.act(action);
                cx.notify();
                cx.stop_propagation();
            }),
        )
    }

    fn tile(grow: f32, body: Div) -> Div {
        div()
            .relative()
            .flex()
            .flex_col()
            .flex_grow(grow)
            .flex_basis(px(TILE_LEAST))
            .min_w_0()
            .h_full()
            .min_h(px(TILE_TALL))
            .rounded(px(12.0))
            .overflow_hidden()
            .bg(SHELL)
            .shadow(vec![ring(ink(0.06))])
            .child(body)
    }

    fn stack(&self, cx: &mut Context<Self>) -> Div {
        let tabs: Vec<Div> = STACK
            .iter()
            .zip(STACK_BADGES)
            .enumerate()
            .map(|(index, ((name, shape), badge))| {
                let on = index == self.tab;
                let tone = if on { ink(1.0) } else { ink(0.5) };
                let tab = div()
                    .flex()
                    .flex_none()
                    .items_center()
                    .gap(px(7.0))
                    .h(px(31.0))
                    .pl(px(10.0))
                    .pr(px(3.0))
                    .rounded_t(px(9.0))
                    .when(on, |tab| tab.bg(ink(0.06)))
                    .child(glyph(*shape, 13.0, tone))
                    .child(medium(*name, 12.5, tone))
                    .child(
                        div()
                            .px(px(5.0))
                            .rounded(px(5.0))
                            .bg(ink(0.06))
                            .child(mono(badge, 10.5, CAPTION).font_weight(FontWeight::MEDIUM)),
                    )
                    .child(
                        div()
                            .w(px(18.0))
                            .flex()
                            .justify_center()
                            .children(on.then(|| glyph(Glyph::Close, 11.0, CAPTION))),
                    );
                Self::hot(tab, Action::Tab(index), cx)
            })
            .collect();
        let header = div()
            .flex()
            .flex_none()
            .items_end()
            .gap(px(2.0))
            .h(px(36.0))
            .px(px(12.0))
            .overflow_hidden()
            .children(tabs)
            .child(Self::hot(
                div()
                    .flex()
                    .flex_none()
                    .items_center()
                    .justify_center()
                    .size(px(26.0))
                    .mb(px(3.0))
                    .child(medium("+", 12.0, CAPTION)),
                Action::Tell(STACK_MORE),
                cx,
            ));
        let table = (self.tab == 0).then(|| {
            Self::hot(
                div()
                    .flex_1()
                    .min_h_0()
                    .overflow_hidden()
                    .child(agents::table()),
                Action::Sheet,
                cx,
            )
        });
        div()
            .flex()
            .flex_col()
            .flex_1()
            .min_h_0()
            .relative()
            .child(header)
            .children(table)
            .children((self.tab == 0 && self.sheet).then(agents::sheet))
    }

    fn toast(&self, cx: &mut Context<Self>) -> Option<Div> {
        let message = self.told?;
        Some(
            div()
                .absolute()
                .left_0()
                .right_0()
                .bottom(px(TOAST_BOTTOM))
                .flex()
                .justify_center()
                .px(px(16.0))
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(10.0))
                        .w(px(TOAST_WIDTH))
                        .max_w_full()
                        .px(px(14.0))
                        .py(px(10.0))
                        .rounded(px(12.0))
                        .bg(rgb(30, 29, 36, 0.96))
                        .shadow(vec![ring(ink(0.12))])
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .child(text(message, 13.0, STRONG).whitespace_normal()),
                        )
                        .child(text("not drawn yet", 11.0, FAINT))
                        .child(text("×", 14.0, SOFT))
                        .on_mouse_down(
                            MouseButton::Left,
                            cx.listener(|screen, _: &MouseDownEvent, _, cx| {
                                screen.told = None;
                                cx.notify();
                                cx.stop_propagation();
                            }),
                        ),
                ),
        )
    }
}

impl Render for Platforms {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let tiles = div()
            .id("platforms-tiles")
            .flex_1()
            .min_w(px(TILES_LEAST))
            .overflow_y_scroll()
            .flex()
            .flex_wrap()
            .gap(px(GAP))
            .child(Self::tile(CHAT_SHARE, Self::chat(cx)))
            .child(Self::tile(STACK_SHARE, self.stack(cx)));
        let body = div()
            .flex_1()
            .min_h_0()
            .flex()
            .gap(px(GAP))
            .pr(px(8.0))
            .pb(px(2.0))
            .when(!self.side, |body| body.pl(px(8.0)))
            .when(self.side, |body| body.child(self.sidebar(cx)))
            .child(tiles);
        div()
            .flex_1()
            .min_h_0()
            .w_full()
            .relative()
            .flex()
            .flex_col()
            .overflow_hidden()
            .font_family(SANS)
            .bg(WINDOW)
            .rounded(px(self.platform.radius()))
            .shadow(vec![ring(ink(0.1))])
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|screen, _: &MouseDownEvent, _, cx| {
                    screen.pop = Pop::None;
                    cx.notify();
                }),
            )
            .child(self.title_bar(cx))
            .child(body)
            .child(Self::status_bar())
            .children(self.toast(cx))
    }
}
