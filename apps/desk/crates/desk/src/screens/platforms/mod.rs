mod agents;
mod chat;
mod chrome;
mod fixture;
mod glyph;
mod paint;

use std::borrow::Cow;
use std::sync::Arc;

use gpui::{
    AnyView, App, AppContext, Context, Div, FontWeight, Image, ImageFormat, IntoElement,
    MouseButton, MouseDownEvent, Render, Window, div, img, prelude::*, px,
};

use fixture::{SESSION, STACK_BADGES};
use glyph::{Glyph, glyph, glyph_at};
use paint::{CAPTION, FAINT, SANS, SHELL, SOFT, STRONG, at, ink, medium, mono, rgb, ring, text};

pub const BOARD_WIDTH: f32 = 1400.0;
pub const SIDE_WIDTH: f32 = 232.0;
const INSET_X: f32 = 20.0;
const INSET_Y: f32 = 18.0;
const AREA_TOP: f32 = 42.0;
const AREA_RIGHT: f32 = 8.0;
const AREA_BOTTOM: f32 = 38.0;
const GAP: f32 = 6.0;
const CHAT_SHARE: f32 = 0.5496;
const SINGLE_HEADER: f32 = 28.0;
const STACK_HEADER: f32 = 36.0;
const POP_TOP: f32 = 38.0;
const BACKDROP: &[u8] = include_bytes!("../../modules/chat/assets/backdrop.jpg");
const STACK: [(&str, Glyph); 3] = [
    ("Sub-agents", Glyph::People),
    ("File edits", Glyph::File),
    ("Shells", Glyph::Terminal),
];
const TAB_SPANS: [(f32, f32); 3] = [(15.0, 171.0), (188.0, 140.0), (330.0, 117.0)];
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
    pub fn controls_width(self) -> f32 {
        match self {
            Platform::Windows => 118.0,
            Platform::Macos => 10.0,
            Platform::Linux => 98.0,
        }
    }

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

    fn pop_right(self) -> f32 {
        match self {
            Platform::Windows => 130.0,
            Platform::Macos => 12.0,
            Platform::Linux => 100.0,
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
            backdrop: Arc::new(Image::from_bytes(ImageFormat::Jpeg, BACKDROP.to_vec())),
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
    backdrop: Arc<Image>,
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

    fn hot(
        &self,
        (x, y, w, h): (f32, f32, f32, f32),
        action: Action,
        cx: &mut Context<Self>,
    ) -> Div {
        at(div().w(px(w)).h(px(h)), x, y)
            .cursor_pointer()
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(move |screen, _: &MouseDownEvent, _, cx| {
                    screen.act(action);
                    cx.notify();
                    cx.stop_propagation();
                }),
            )
    }

    fn hotspots(
        &self,
        width: f32,
        height: f32,
        chat: (f32, f32),
        stack: (f32, f32),
        cx: &mut Context<Self>,
    ) -> Vec<Div> {
        let platform = self.platform;
        let left = |x| chrome::left(platform, self.side, x);
        let right = |x| chrome::right(platform, width, x);
        let (chat_x, chat_w) = chat;
        let (stack, stack_w) = stack;
        let bottom = height - AREA_BOTTOM;
        let mut spots: Vec<((f32, f32, f32, f32), Action)> = chrome::control_rects(platform, width)
            .into_iter()
            .zip(platform.control_tells())
            .map(|(rect, tell)| (rect, Action::Tell(tell)))
            .collect();
        spots.extend([
            ((chrome::toggle_x(platform) - 6.0, 6.0, 28.0, 28.0), Action::Side),
            ((left(398.0), 11.0, 18.0, 18.0), Action::Tell("Closes the editor workspace; its tiles are kept and come back with ctrl shift t.")),
            ((left(486.0), 11.0, 18.0, 18.0), Action::Tell("Closes the data workspace; its tiles are kept and come back with ctrl shift t.")),
            ((left(508.0), 6.0, 26.0, 28.0), Action::Tell("Opens a new workspace with one empty tile: pick a module, or start from a preset (work 1+2, editor, data).")),
            ((right(1146.0), 6.0, 70.0, 28.0), Action::Tell("Opens the command palette: files, sessions, screens, settings and commands in one search (alt k).")),
            ((right(1220.0), 6.0, 28.0, 28.0), Action::Open(Pop::Bell)),
            ((right(1251.0), 6.0, 28.0, 28.0), Action::Open(Pop::Account)),
            ((chat_x + 18.0, bottom - 49.0, 28.0, 28.0), Action::Tell("Attaches a file or an image; it goes to the lead as a reference, like an @ mention.")),
            ((chat_x + chat_w - 210.0, bottom - 47.0, 80.0, 24.0), Action::Tell("Picks the model the lead runs on from your accounts; the change applies from the next turn.")),
            ((chat_x + chat_w - 124.0, bottom - 47.0, 72.0, 24.0), Action::Tell("Cycles the effort: low, medium, high.")),
            ((chat_x + 24.0, bottom - 98.0, 100.0, 26.0), Action::Tell("Allowed once: bash runs npm run build.")),
            ((chat_x + 130.0, bottom - 98.0, 70.0, 26.0), Action::Tell("Denied: the lead is told npm run build was refused.")),
            ((chat_x + 206.0, bottom - 98.0, 110.0, 26.0), Action::Tell("Always here: npm run build is allowed in notes-app from now on.")),
            ((stack + 449.0, AREA_TOP + 5.0, 26.0, 26.0), Action::Tell("Stacks another module in this tile: Terminal, Browser, Editor, or another view of a module.")),
        ]);
        if self.tab == 0 {
            spots.push((
                (
                    stack,
                    AREA_TOP + STACK_HEADER,
                    stack_w,
                    bottom - AREA_TOP - STACK_HEADER,
                ),
                Action::Sheet,
            ));
        }
        spots.extend(
            TAB_SPANS
                .iter()
                .enumerate()
                .map(|(index, (x, w))| ((stack + x, AREA_TOP + 5.0, *w, 31.0), Action::Tab(index))),
        );
        if self.side {
            spots.extend([
                ((220.0, 105.0, 16.0, 18.0), Action::Tell("Starts a new session in notes-app and opens its chat in the work workspace.")),
                ((8.0, 159.0, SIDE_WIDTH, 32.0), Action::Tell("Switches the desk to quiet-amber-heron: its chat and feeds replace these, the tabs stay.")),
                ((8.0, chrome::INACTIVE_TOP, SIDE_WIDTH, 32.0), Action::Inactive),
            ]);
            let reopen = [
                "Reopens fond-sandy-mink where it stopped; nothing runs until you send.",
                "Reopens tidy-ochre-wren where it stopped; nothing runs until you send.",
                "Reopens crisp-azure-swift where it stopped; nothing runs until you send.",
            ];
            spots.extend(
                reopen
                    .into_iter()
                    .enumerate()
                    .filter(|_| self.inactive)
                    .map(|(index, tell)| {
                        (
                            (
                                8.0,
                                chrome::INACTIVE_TOP + 32.0 * (index as f32 + 1.0),
                                SIDE_WIDTH,
                                32.0,
                            ),
                            Action::Tell(tell),
                        )
                    }),
            );
        }
        spots
            .into_iter()
            .map(|(rect, action)| self.hot(rect, action, cx))
            .collect()
    }

    fn chat_header(width: f32) -> Div {
        div()
            .absolute()
            .left_0()
            .top_0()
            .w(px(width))
            .h(px(SINGLE_HEADER))
            .child(glyph_at(Glyph::Chat, 13.0, ink(0.6), 12.0, 7.5))
            .child(at(medium("Chat", 12.0, SOFT), 31.0, 6.0))
            .child(at(
                div()
                    .w(px(width - 40.0))
                    .flex()
                    .justify_end()
                    .child(medium(SESSION, 12.0, FAINT)),
                31.0,
                6.0,
            ))
    }

    fn tabs(&self) -> Div {
        let tabs =
            STACK
                .iter()
                .zip(STACK_BADGES)
                .enumerate()
                .map(|(index, ((name, shape), badge))| {
                    let on = index == self.tab;
                    let tone = if on { ink(1.0) } else { ink(0.5) };
                    div()
                        .flex()
                        .items_center()
                        .h(px(31.0))
                        .pb(px(1.4))
                        .pl(px(30.0))
                        .pr(px(3.0))
                        .relative()
                        .rounded_t(px(9.0))
                        .when(on, |tab| tab.bg(ink(0.06)))
                        .child(glyph_at(*shape, 13.0, tone, 8.0, 9.0))
                        .child(medium(*name, 12.5, tone))
                        .child(
                            div()
                                .ml(px(7.0))
                                .h(px(16.5))
                                .px(px(5.0))
                                .pt(px(1.0))
                                .rounded(px(5.0))
                                .bg(ink(0.06))
                                .child(mono(badge, 10.5, CAPTION).font_weight(FontWeight::MEDIUM)),
                        )
                        .child(
                            div()
                                .w(px(24.0))
                                .flex()
                                .justify_center()
                                .children(on.then(|| glyph(Glyph::Close, 11.0, CAPTION))),
                        )
                });
        at(
            div().flex().gap(px(2.0)).children(tabs).child(
                div()
                    .ml(px(2.0))
                    .size(px(26.0))
                    .pt(px(5.0))
                    .flex()
                    .justify_center()
                    .child(medium("+", 12.0, CAPTION)),
            ),
            15.0,
            5.0,
        )
    }

    fn tile(x: f32, w: f32, h: f32, body: Vec<Div>, header: Div) -> Div {
        at(div(), x, AREA_TOP)
            .w(px(w))
            .h(px(h))
            .rounded(px(12.0))
            .overflow_hidden()
            .bg(SHELL)
            .shadow(vec![ring(ink(0.06))])
            .child(at(div().w(px(w)).h(px(h)).children(body), 0.0, 0.0))
            .child(header)
    }

    fn popover(&self, width: f32) -> Option<Div> {
        let (rows, wide, caption): (&[(&str, &str)], f32, &str) = match self.pop {
            Pop::None => return None,
            Pop::Bell => (&BELL_ROWS, 330.0, "Needs you"),
            Pop::Account => (&ACCOUNT_ROWS, 250.0, "pehcastro"),
        };
        let items = rows.iter().map(|(title, line)| {
            div()
                .px(px(10.0))
                .py(px(7.0))
                .rounded(px(8.0))
                .flex()
                .gap(px(8.0))
                .child(div().flex_1().child(text(*title, 13.0, STRONG)))
                .child(text(*line, 12.0, FAINT))
        });
        Some(at(
            div()
                .w(px(wide))
                .p(px(6.0))
                .rounded(px(12.0))
                .bg(rgb(30, 29, 36, 0.94))
                .shadow(vec![ring(ink(0.12))])
                .child(div().px(px(10.0)).pt(px(8.0)).pb(px(6.0)).child(medium(
                    caption,
                    10.0,
                    ink(0.45),
                )))
                .children(items),
            width - self.platform.pop_right() - wide,
            POP_TOP,
        ))
    }

    fn toast(&self, width: f32, height: f32, cx: &mut Context<Self>) -> Option<Div> {
        let message = self.told?;
        Some(
            at(
                div()
                    .w(px(560.0))
                    .px(px(14.0))
                    .py(px(10.0))
                    .rounded(px(12.0))
                    .bg(rgb(30, 29, 36, 0.96))
                    .shadow(vec![ring(ink(0.12))])
                    .flex()
                    .items_center()
                    .gap(px(10.0))
                    .child(div().flex_1().child(text(message, 13.0, STRONG)))
                    .child(text("not drawn yet", 11.0, FAINT))
                    .child(text("×", 14.0, SOFT)),
                (width - 560.0) / 2.0,
                height - 110.0,
            )
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|screen, _: &MouseDownEvent, _, cx| {
                    screen.told = None;
                    cx.notify();
                    cx.stop_propagation();
                }),
            ),
        )
    }
}

impl Render for Platforms {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let size = window.viewport_size();
        let width = f32::from(size.width) - 2.0 * INSET_X;
        let height = f32::from(size.height) - 2.0 * INSET_Y;
        let area_left = if self.side { SIDE_WIDTH + 16.0 } else { 8.0 };
        let area_w = width - area_left - AREA_RIGHT;
        let area_h = height - AREA_TOP - AREA_BOTTOM;
        let chat_w = (area_w - GAP) * CHAT_SHARE;
        let stack_x = area_left + chat_w + GAP;
        let stack_w = area_w - chat_w - GAP;
        let mut stack_body = Vec::new();
        if self.tab == 0 {
            stack_body.push(agents::table(stack_w, area_h));
            stack_body.extend(self.sheet.then(|| agents::sheet(stack_w, area_h)));
        }
        let tiles = [
            Self::tile(
                area_left,
                chat_w,
                area_h,
                chat::body(chat_w, area_h),
                Self::chat_header(chat_w),
            ),
            Self::tile(stack_x, stack_w, area_h, stack_body, self.tabs()),
        ];
        let hotspots = self.hotspots(width, height, (area_left, chat_w), (stack_x, stack_w), cx);
        let popover = self.popover(width);
        let toast = self.toast(width, height, cx);
        div()
            .size_full()
            .relative()
            .font_family(SANS)
            .child(img(self.backdrop.clone()).absolute().size_full())
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|screen, _: &MouseDownEvent, _, cx| {
                    screen.pop = Pop::None;
                    cx.notify();
                }),
            )
            .child(
                at(div(), INSET_X, INSET_Y)
                    .w(px(width))
                    .h(px(height))
                    .rounded(px(self.platform.radius()))
                    .shadow(vec![ring(ink(0.1))])
                    .children(chrome::title_bar(width, self.platform, self.side))
                    .children(if self.side {
                        chrome::sidebar(self.inactive)
                    } else {
                        Vec::new()
                    })
                    .children(chrome::status_bar(width, height))
                    .children(tiles)
                    .children(hotspots)
                    .children(popover)
                    .children(toast),
            )
    }
}
