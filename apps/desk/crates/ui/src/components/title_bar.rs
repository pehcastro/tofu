use std::rc::Rc;

use desk_core::control::Control;
use gpui::{
    AnyElement, App, ClickEvent, Div, ElementId, Entity, FocusHandle, FontWeight, SharedString,
    Window, WindowControlArea, div, prelude::*, px,
};

use crate::component::{control, icon, icon_button};
use crate::components::avatar::{PersonSize, person_avatar};
use crate::components::card::caption;
use crate::components::chip::mono;
use crate::components::empty::empty_state;
use crate::components::glyph::Glyph;
use crate::components::list::bare_row;
use crate::components::overlay::{Align, Placement, Popover, Side};
use crate::components::paint::{glyph, ink};
use crate::components::size::{FONT_SMALL, MENU_PAD, POPOVER_PAD_X, POPOVER_PAD_Y, T2, T3};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{
    CAPTION_WIDTH, CONTROL, ICON_SMALL, RADIUS_CAPTION, RADIUS_TAB, SIDEBAR_CLOSED_WIDTH,
    TAB_HEIGHT, TEXT, TITLE_BAR_HEIGHT,
};
use crate::theme::{ColorToken, Theme};

const TITLE_GAP: f32 = 4.0;
const PRODUCT_NAME: &str = "tofu";
const BELL_DOT: f32 = 6.0;
const BELL_DOT_INSET: f32 = 6.0;
const POP_OFFSET: f32 = 4.0;
const BELL_POP_WIDTH: f32 = 330.0;
const ACCOUNT_POP_WIDTH: f32 = 250.0;
const NOTICE_LINE: f32 = 18.0;
const ACCOUNT_LINE: f32 = 17.0;
const POP_CAPTION_X: f32 = 10.0;
const POP_CAPTION_TOP: f32 = 8.0;
const POP_CAPTION_GROUP_TOP: f32 = 10.0;
const POP_CAPTION_BOTTOM: f32 = 6.0;
const ACCOUNT_HEAD_BOTTOM: f32 = 10.0;
const NO_NOTICES: &str = "No notices";
const NO_ACCOUNT: &str = "no account";

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TitlePick {
    Sidebar,
    NewWorkspace,
    Palette,
    Notice(usize),
    NoticesRead,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TitlePop {
    Notifications,
    Account,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum WindowKeys {
    Live,
    Shown,
}

#[derive(Clone)]
pub struct Notice {
    pub glyph: Glyph,
    pub text: SharedString,
    pub code: Option<SharedString>,
    pub detail: SharedString,
    pub needs_you: bool,
}

#[derive(Clone)]
pub struct Account {
    pub name: SharedString,
    pub found: SharedString,
}

#[derive(Clone)]
pub struct Title {
    pub sidebar_open: bool,
    pub palette_keys: SharedString,
    pub notices: Vec<Notice>,
    pub unread: bool,
    pub letter: Option<SharedString>,
    pub account: Option<Account>,
    pub version: Option<SharedString>,
    pub keys: WindowKeys,
    pub open: Option<TitlePop>,
    pub tabs_x: f32,
}

type OnPick = Rc<dyn Fn(&TitlePick, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct TitleBar {
    id: ElementId,
    title: Title,
    tabs: Option<AnyElement>,
    on_pick: OnPick,
}

impl TitleBar {
    pub fn new(
        id: impl Into<ElementId>,
        title: Title,
        tabs: Option<AnyElement>,
        on_pick: impl Fn(&TitlePick, &mut Window, &mut App) + 'static,
    ) -> Self {
        Self {
            id: id.into(),
            title,
            tabs,
            on_pick: Rc::new(on_pick),
        }
    }

    fn picked(
        &self,
        pick: TitlePick,
    ) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static + use<> {
        let on_pick = self.on_pick.clone();
        move |_, window, cx| on_pick(&pick, window, cx)
    }
}

struct Opened {
    pop: Option<TitlePop>,
    focus: FocusHandle,
}

fn shut(state: &Entity<Opened>, cx: &mut App) {
    state.update(cx, |opened, cx| {
        opened.pop = None;
        cx.notify();
    });
}

#[derive(Clone)]
struct PopRow {
    state: Entity<Opened>,
    on_pick: OnPick,
}

impl PopRow {
    fn picked(&self, pick: TitlePick) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
        let PopRow { state, on_pick } = self.clone();
        move |_, window, cx| {
            shut(&state, cx);
            on_pick(&pick, window, cx);
        }
    }

    fn body(&self, focus: &FocusHandle) -> Div {
        let (escaper, outside) = (self.state.clone(), self.state.clone());
        div()
            .flex()
            .flex_col()
            .mx(px(MENU_PAD - POPOVER_PAD_X))
            .my(px(MENU_PAD - POPOVER_PAD_Y))
            .track_focus(focus)
            .on_key_down(move |event, _, cx| {
                if event.keystroke.key == "escape" {
                    cx.stop_propagation();
                    shut(&escaper, cx);
                }
            })
            .on_mouse_down_out(move |_, _, cx| shut(&outside, cx))
    }
}

fn pop_caption(text: &'static str, top: f32, theme: &Theme) -> Div {
    div()
        .pt(px(top))
        .px(px(POP_CAPTION_X))
        .pb(px(POP_CAPTION_BOTTOM))
        .child(caption(text, theme))
}

fn notice_row(ix: usize, notice: &Notice, rows: &PopRow, theme: &Theme) -> impl IntoElement {
    let mark = match notice.needs_you {
        true => theme.color(ColorToken::StatusWarn),
        false => ink(theme, T3),
    };
    bare_row(("title-notice", ix), false, false, theme)
        .items_start()
        .line_height(px(NOTICE_LINE))
        .on_click(rows.picked(TitlePick::Notice(ix)))
        .child(div().h(px(NOTICE_LINE)).flex().items_center().child(glyph(
            notice.glyph,
            ICON_SMALL,
            mark,
        )))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .child(
                    div()
                        .flex()
                        .flex_wrap()
                        .gap_x_1()
                        .when(!notice.needs_you, |line| line.text_color(ink(theme, T2)))
                        .child(notice.text.clone())
                        .children(
                            notice
                                .code
                                .clone()
                                .map(|code| div().font_family(mono(theme)).child(code)),
                        ),
                )
                .child(
                    div()
                        .text_size(px(FONT_SMALL))
                        .text_color(ink(theme, T3))
                        .child(notice.detail.clone()),
                ),
        )
}

fn notices(title: &Title, rows: &PopRow, theme: &Theme) -> Vec<AnyElement> {
    if title.notices.is_empty() {
        return vec![
            empty_state(
                "title-no-notices",
                NO_NOTICES,
                None,
                &[],
                &[],
                theme,
                |_, _, _| {},
            )
            .into_any_element(),
        ];
    }
    let mut out = Vec::new();
    for (needs_you, name, top) in [
        (true, "Needs you", POP_CAPTION_TOP),
        (false, "Earlier", POP_CAPTION_GROUP_TOP),
    ] {
        let group: Vec<AnyElement> = title
            .notices
            .iter()
            .enumerate()
            .filter(|(_, notice)| notice.needs_you == needs_you)
            .map(|(ix, notice)| notice_row(ix, notice, rows, theme).into_any_element())
            .collect();
        if !group.is_empty() {
            out.push(pop_caption(name, top, theme).into_any_element());
            out.extend(group);
        }
    }
    out
}

fn account_menu(title: &Title, theme: &Theme) -> Div {
    let t3 = ink(theme, T3);
    let small = |text: SharedString| div().text_size(px(FONT_SMALL)).text_color(t3).child(text);
    let (name, found) = match &title.account {
        Some(account) => (account.name.clone(), Some(account.found.clone())),
        None => (NO_ACCOUNT.into(), None),
    };
    div()
        .flex()
        .flex_col()
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(POP_CAPTION_X))
                .pt(px(POP_CAPTION_TOP))
                .px(px(POP_CAPTION_X))
                .pb(px(ACCOUNT_HEAD_BOTTOM))
                .line_height(px(ACCOUNT_LINE))
                .child(person_avatar(None, PersonSize::Menu, theme))
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .child(name)
                        .children(found.map(small)),
                ),
        )
        .children(title.version.clone().map(|version| {
            div()
                .px(px(POP_CAPTION_X))
                .pb(px(POP_CAPTION_BOTTOM))
                .child(caption(format!("{PRODUCT_NAME} {version}"), theme))
        }))
}

#[derive(Clone, Copy)]
enum Caption {
    Minimize,
    Maximize,
    Restore,
    Close,
}

impl Caption {
    fn area(self) -> WindowControlArea {
        match self {
            Caption::Minimize => WindowControlArea::Min,
            Caption::Maximize | Caption::Restore => WindowControlArea::Max,
            Caption::Close => WindowControlArea::Close,
        }
    }

    fn icon(self) -> Icon {
        match self {
            Caption::Minimize => Icon::Minimize,
            Caption::Maximize => Icon::Maximize,
            Caption::Restore => Icon::Restore,
            Caption::Close => Icon::Close,
        }
    }

    fn label(self) -> &'static str {
        match self {
            Caption::Minimize => "Minimize",
            Caption::Maximize => "Maximize",
            Caption::Restore => "Restore",
            Caption::Close => "Close",
        }
    }

    fn hover(self) -> ColorToken {
        match self {
            Caption::Minimize | Caption::Maximize | Caption::Restore => {
                ColorToken::StateCaptionHover
            }
            Caption::Close => ColorToken::StateCloseHover,
        }
    }

    fn button(self, keys: WindowKeys, theme: &Theme) -> impl IntoElement {
        let label = self.label();
        let hover = theme.color(self.hover());
        let strong = theme.color(ColorToken::TextStrong);
        div()
            .id(label)
            .aria_label(label)
            .group(label)
            .occlude()
            .w(px(CAPTION_WIDTH))
            .h(px(CONTROL))
            .flex()
            .items_center()
            .justify_center()
            .rounded(px(RADIUS_CAPTION))
            .when(keys == WindowKeys::Live, |key| {
                key.window_control_area(self.area())
            })
            .hover(move |style| style.bg(hover))
            .child(
                icon(
                    self.icon(),
                    ICON_SMALL,
                    theme.color(ColorToken::TextCaption),
                )
                .group_hover(label, move |style| style.text_color(strong)),
            )
    }
}

fn captions(keys: WindowKeys, maximized: bool, theme: &Theme) -> impl IntoElement {
    let middle = if maximized {
        Caption::Restore
    } else {
        Caption::Maximize
    };
    div()
        .flex()
        .items_center()
        .gap_0p5()
        .mx_1p5()
        .child(Caption::Minimize.button(keys, theme))
        .child(middle.button(keys, theme))
        .child(Caption::Close.button(keys, theme))
}

impl RenderOnce for TitleBar {
    fn render(mut self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let tab = theme.color(ColorToken::TextTab);
        let tabs = match self.tabs.take() {
            Some(tabs) => div()
                .id("title-tabs")
                .flex_1()
                .min_w_0()
                .child(tabs)
                .into_any_element(),
            None => div()
                .flex_1()
                .flex()
                .child(
                    control("new-workspace", Control::NewWorkspace.label(), &theme)
                        .occlude()
                        .h(px(TAB_HEIGHT))
                        .px_2p5()
                        .rounded(px(RADIUS_TAB))
                        .child(icon(Icon::Plus, ICON_SMALL, tab))
                        .on_click(self.picked(TitlePick::NewWorkspace)),
                )
                .into_any_element(),
        };
        let title = &self.title;
        let starts_open = title.open;
        let state = window.use_keyed_state(self.id.clone(), cx, |_, cx| Opened {
            pop: starts_open,
            focus: cx.focus_handle(),
        });
        let (shown, focus) = {
            let opened = state.read(cx);
            (opened.pop, opened.focus.clone())
        };
        let rows = PopRow {
            state: state.clone(),
            on_pick: self.on_pick.clone(),
        };
        let toggle = |pop: TitlePop| {
            let (state, focus, on_pick) = (state.clone(), focus.clone(), self.on_pick.clone());
            move |_: &ClickEvent, window: &mut Window, cx: &mut App| {
                let next = (shown != Some(pop)).then_some(pop);
                state.update(cx, |opened, cx| {
                    opened.pop = next;
                    cx.notify();
                });
                if next.is_some() {
                    window.focus(&focus, cx);
                }
                if next == Some(TitlePop::Notifications) {
                    on_pick(&TitlePick::NoticesRead, window, cx);
                }
            }
        };
        let under = Placement {
            side: Side::Bottom,
            align: Align::End,
            offset: POP_OFFSET,
        };
        let column = title.tabs_x.max(SIDEBAR_CLOSED_WIDTH + TITLE_GAP) - TITLE_GAP;
        let warn = theme.color(ColorToken::StatusWarn);
        let unread = title.unread;
        let bell = icon_button(
            "notifications",
            Icon::Bell,
            Control::Notifications.label(),
            &theme,
        )
        .occlude()
        .when(unread, |bell| {
            bell.child(
                div()
                    .absolute()
                    .top(px(BELL_DOT_INSET))
                    .right(px(BELL_DOT_INSET))
                    .size(px(BELL_DOT))
                    .rounded_full()
                    .bg(warn),
            )
        })
        .on_click(toggle(TitlePop::Notifications));
        let account = control("account", Control::Account.label(), &theme)
            .occlude()
            .size(px(CONTROL))
            .rounded_full()
            .child(person_avatar(
                title.letter.clone(),
                PersonSize::Title,
                &theme,
            ))
            .on_click(toggle(TitlePop::Account));
        div()
            .id(self.id.clone())
            .w_full()
            .h(px(TITLE_BAR_HEIGHT))
            .flex_none()
            .flex()
            .items_center()
            .gap(px(TITLE_GAP))
            .when(title.keys == WindowKeys::Live, |bar| {
                bar.window_control_area(WindowControlArea::Drag)
            })
            .child(
                div()
                    .w(px(column))
                    .h_full()
                    .flex_none()
                    .flex()
                    .items_center()
                    .gap_1()
                    .pl_2p5()
                    .child(
                        icon_button("sidebar-toggle", Icon::Sidebar, "Toggle sidebar", &theme)
                            .occlude()
                            .on_click(self.picked(TitlePick::Sidebar)),
                    )
                    .when(title.sidebar_open, |name| {
                        name.child(
                            div()
                                .pl_1p5()
                                .font_weight(FontWeight::BOLD)
                                .text_color(theme.color(ColorToken::TextName))
                                .child(PRODUCT_NAME),
                        )
                    }),
            )
            .child(tabs)
            .child(
                control("palette", "Command palette", &theme)
                    .occlude()
                    .h(px(CONTROL))
                    .px_2p5()
                    .gap_2()
                    .rounded(px(RADIUS_TAB))
                    .child(icon(Icon::Search, ICON_SMALL, tab))
                    .child(
                        div()
                            .text_size(px(TEXT))
                            .text_color(theme.color(ColorToken::TextMuted))
                            .child(title.palette_keys.clone()),
                    )
                    .on_click(self.picked(TitlePick::Palette)),
            )
            .child(
                Popover::new("title-bell-pop", bell)
                    .open(shown == Some(TitlePop::Notifications))
                    .placement(under)
                    .width(BELL_POP_WIDTH)
                    .child(rows.body(&focus).children(notices(title, &rows, &theme))),
            )
            .child(
                Popover::new("title-account-pop", account)
                    .open(shown == Some(TitlePop::Account))
                    .placement(under)
                    .width(ACCOUNT_POP_WIDTH)
                    .child(rows.body(&focus).child(account_menu(title, &theme))),
            )
            .when(!cfg!(target_os = "macos"), |bar| {
                bar.child(captions(title.keys, window.is_maximized(), &theme))
            })
    }
}
