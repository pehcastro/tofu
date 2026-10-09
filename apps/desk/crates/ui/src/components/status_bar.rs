use std::cmp::Reverse;
use std::rc::Rc;
use std::sync::Arc;

use gpui::{
    AnyElement, AnyView, App, ClickEvent, Div, ElementId, Entity, FocusHandle, FontWeight, Rgba,
    SharedString, Stateful, Window, div, prelude::*, px, relative,
};

use crate::component::{icon, status_item};
use crate::components::avatar::spinner;
use crate::components::button::{ButtonKind, button};
use crate::components::card::caption;
use crate::components::chip::tabular;
use crate::components::glyph::Glyph;
use crate::components::list::{row, separator};
use crate::components::overlay::{Align, Placement, Popover, Side};
use crate::components::paint::{glyph, ink};
use crate::components::size::{
    CAPTION_TEXT, CHIP_FILL, FONT_BODY, FONT_SMALL, MENU_PAD, POPOVER_PAD_X, POPOVER_PAD_Y,
    RADIUS_CHIP_SMALL, ROW_GAP, ROW_PAD_X, T1, T2, T3,
};
use crate::components::tooltip::{Edge, tooltip};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{HAIRLINE, ICON_SMALL, STATUS_BAR_HEIGHT, STATUS_ITEM_HEIGHT, TEXT_SMALL};
use crate::theme::{ColorToken, Theme};

const NO_ACCOUNT: &str = "no account";
const NO_ACCOUNT_CONNECTED: &str = "No account connected";
const READING_QUOTA: &str = "Reading quota";
const UNREAD: &str = "quota unread";
const UNREAD_TITLE: &str = "Quota could not be read";
const NOT_REPORTED: &str = "Quota not reported";
const NEEDS_REAUTH: &str = "Needs re-authentication";
const SIGN_IN: &str = "Sign in";
const NEW_SESSION: &str = "New session";
const ALL_PROVIDERS: &str = "All providers";
const POP_OFFSET: f32 = 4.0;
const QUOTA_POP_WIDTH: f32 = 300.0;
const BLOCK_Y: f32 = 8.0;
const BLOCK_GAP: f32 = 6.0;
const SECTION_BREAK: f32 = 4.0;
const TILE: f32 = 20.0;
const EMPTY_GLYPH: f32 = 13.0;
const WINDOW_ROW: f32 = 18.0;
const WINDOW_GAP: f32 = 8.0;
const LABEL_WIDTH: f32 = 58.0;
const PERCENT_WIDTH: f32 = 32.0;
const RESET_WIDTH: f32 = 76.0;
const XS_BUTTON: f32 = 22.0;
const LOADING_DOT: f32 = 6.0;
const DIVIDER_HEIGHT: f32 = 14.0;
const DIVIDER_INK: f32 = 0.18;
const WARN_FROM: u8 = 70;
const NEAR_LIMIT_FROM: u8 = 90;
const CTX_BAR_WIDTH: f32 = 40.0;
const BAR_HEIGHT: f32 = 5.0;
const BAR_RADIUS: f32 = 3.0;
const BAR_TRACK: f32 = 0.08;
const BAR_FILL: f32 = 0.6;

#[derive(Clone)]
pub struct Branch {
    pub name: SharedString,
    pub ahead: usize,
}

#[derive(Clone)]
pub struct ContextUse {
    pub tokens: SharedString,
    pub share: f32,
}

#[derive(Clone)]
pub struct Reset {
    pub left: SharedString,
    pub at: SharedString,
}

#[derive(Clone)]
pub struct UsageWindow {
    pub label: SharedString,
    pub percent: u8,
    pub reset: Option<Reset>,
}

#[derive(Clone)]
pub enum Standing {
    Serving,
    Reauth(SharedString),
    Trouble(SharedString),
}

#[derive(Clone)]
pub struct Account {
    pub provider: SharedString,
    pub email: Option<SharedString>,
    pub source: SharedString,
    pub plan: Option<SharedString>,
    pub standing: Standing,
    pub windows: Vec<UsageWindow>,
}

#[derive(Clone, Default)]
pub enum Accounts {
    #[default]
    Reading,
    Unread(SharedString),
    Read(Vec<Account>),
}

#[derive(Clone, Default)]
pub struct SessionGroup {
    pub context: Option<ContextUse>,
    pub classifier: usize,
    pub cron: usize,
}

#[derive(Clone, Default)]
pub struct Status {
    pub branch: Option<Branch>,
    pub session: Option<SharedString>,
    pub session_group: Option<SessionGroup>,
    pub accounts: Accounts,
    pub problem: Option<SharedString>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum StatusPick {
    Branch,
    Session,
    Context,
    AllProviders,
    SignIn(SharedString),
    Classifier,
    Cron,
}

type OnPick = Rc<dyn Fn(&StatusPick, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct StatusBar {
    id: ElementId,
    status: Status,
    on_pick: OnPick,
    cron_menu: Option<AnyView>,
    quota_open: bool,
}

impl StatusBar {
    pub fn new(
        id: impl Into<ElementId>,
        status: Status,
        on_pick: impl Fn(&StatusPick, &mut Window, &mut App) + 'static,
    ) -> Self {
        Self {
            id: id.into(),
            status,
            on_pick: Rc::new(on_pick),
            cron_menu: None,
            quota_open: false,
        }
    }

    pub fn cron_menu(mut self, menu: Option<AnyView>) -> Self {
        self.cron_menu = menu;
        self
    }

    pub fn quota_open(mut self, open: bool) -> Self {
        self.quota_open = open;
        self
    }

    fn quota(&self, window: &mut Window, cx: &mut App, theme: &Theme) -> Popover {
        let starts_open = self.quota_open;
        let state = window.use_keyed_state(self.id.clone(), cx, |_, cx| QuotaOpen {
            open: starts_open,
            focus: cx.focus_handle(),
        });
        let (open, focus) = {
            let opened = state.read(cx);
            (opened.open, opened.focus.clone())
        };
        let toggle = {
            let (state, focus) = (state.clone(), focus.clone());
            move |_: &ClickEvent, window: &mut Window, cx: &mut App| {
                set_open(&state, !open, cx);
                if !open {
                    window.focus(&focus, cx);
                }
            }
        };
        let item = status_item("Limits", "Limits", theme).on_click(toggle);
        let lock = || tile(theme).child(glyph(Glyph::Lock, EMPTY_GLYPH, ink(theme, T3)));
        let (trigger, sections): (_, Vec<Div>) = match &self.status.accounts {
            Accounts::Reading => (
                item.child(loading("status-quota-spin", theme, cx)),
                vec![notice(
                    tile(theme).child(loading("status-quota-pop-spin", theme, cx)),
                    READING_QUOTA,
                    None,
                    theme,
                )],
            ),
            Accounts::Unread(reason) => (
                item.child(UNREAD),
                vec![notice(lock(), UNREAD_TITLE, Some(reason.clone()), theme)],
            ),
            Accounts::Read(accounts) if accounts.is_empty() => (
                item.child(NO_ACCOUNT),
                vec![notice(lock(), NO_ACCOUNT_CONNECTED, None, theme)],
            ),
            Accounts::Read(accounts) => (
                fullest_item(item, accounts, theme),
                accounts
                    .iter()
                    .enumerate()
                    .map(|(at, account)| {
                        account_block(at, account, &self.on_pick, theme, window, cx)
                    })
                    .collect(),
            ),
        };
        let hover = theme.color(ColorToken::StateHover);
        let (escaper, outside, picker) = (state.clone(), state.clone(), state);
        let on_pick = self.on_pick.clone();
        let mut body = div()
            .flex()
            .flex_col()
            .mx(px(MENU_PAD - POPOVER_PAD_X))
            .my(px(MENU_PAD - POPOVER_PAD_Y))
            .track_focus(&focus)
            .on_key_down(move |event, _, cx| {
                if event.keystroke.key == "escape" {
                    cx.stop_propagation();
                    set_open(&escaper, false, cx);
                }
            })
            .on_mouse_down_out(move |_, _, cx| set_open(&outside, false, cx));
        for section in sections {
            body = body
                .child(section)
                .child(separator(theme).mx_0().my(px(SECTION_BREAK)));
        }
        let body = body.child(
            row("status-all-providers", false, false, theme)
                .hover(move |style| style.bg(hover))
                .on_click(move |_, window, cx| {
                    set_open(&picker, false, cx);
                    on_pick(&StatusPick::AllProviders, window, cx);
                })
                .child(div().flex_1().child(ALL_PROVIDERS))
                .child(icon(Icon::Arrow, ICON_SMALL, ink(theme, CAPTION_TEXT))),
        );
        Popover::new("status-quota-pop", trigger)
            .open(open)
            .placement(Placement {
                side: Side::Top,
                align: Align::End,
                offset: POP_OFFSET,
            })
            .width(QUOTA_POP_WIDTH)
            .child(body)
    }

    fn item(&self, pick: StatusPick, label: &'static str, theme: &Theme) -> Stateful<Div> {
        let on_pick = self.on_pick.clone();
        status_item(label, label, theme)
            .on_click(move |_: &ClickEvent, window, cx| on_pick(&pick, window, cx))
    }
}

struct QuotaOpen {
    open: bool,
    focus: FocusHandle,
}

fn set_open(state: &Entity<QuotaOpen>, open: bool, cx: &mut App) {
    state.update(cx, |opened, cx| {
        opened.open = open;
        cx.notify();
    });
}

pub fn cron_trigger(count: usize, theme: &Theme) -> Stateful<Div> {
    status_item("Cron", "Cron", theme).child(format!("cron {count}"))
}

fn inert_cron(count: usize, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .h(px(STATUS_ITEM_HEIGHT))
        .px_2()
        .text_size(px(TEXT_SMALL))
        .font_weight(FontWeight::MEDIUM)
        .text_color(theme.color(ColorToken::TextStatus))
        .child(format!("cron {count}"))
}

fn dim(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div().text_color(ink(theme, T3)).child(text.into())
}

fn meter_bar(share: f32, fill: Rgba, theme: &Theme) -> Div {
    div()
        .flex_none()
        .h(px(BAR_HEIGHT))
        .rounded(px(BAR_RADIUS))
        .bg(ink(theme, BAR_TRACK))
        .overflow_hidden()
        .child(
            div()
                .h_full()
                .w(relative(share.clamp(0.0, 1.0)))
                .rounded(px(BAR_RADIUS))
                .bg(fill),
        )
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Level {
    Normal,
    Warning,
    NearLimit,
}

impl Level {
    fn of(percent: u8) -> Level {
        match percent {
            NEAR_LIMIT_FROM.. => Level::NearLimit,
            WARN_FROM.. => Level::Warning,
            _ => Level::Normal,
        }
    }

    fn fill(self, theme: &Theme) -> Rgba {
        match self {
            Level::Normal => ink(theme, BAR_FILL),
            Level::Warning => theme.color(ColorToken::StatusWarn),
            Level::NearLimit => theme.color(ColorToken::StatusDanger),
        }
    }

    fn text(self, theme: &Theme) -> Rgba {
        match self {
            Level::Normal => ink(theme, T1),
            Level::Warning | Level::NearLimit => self.fill(theme),
        }
    }
}

fn fullest_item(item: Stateful<Div>, accounts: &[Account], theme: &Theme) -> Stateful<Div> {
    let fullest = accounts
        .iter()
        .flat_map(|account| {
            account
                .windows
                .iter()
                .map(move |window| (account, window, window.percent))
        })
        .min_by_key(|(_, _, percent)| Reverse(*percent));
    match (fullest, accounts.first()) {
        (Some((account, window, percent)), _) => {
            let level = Level::of(percent);
            item.child(account.provider.clone())
                .child(dim(window.label.clone(), theme))
                .child(
                    div()
                        .font_features(tabular())
                        .when(level != Level::Normal, |text| {
                            text.text_color(level.text(theme))
                        })
                        .child(format!("{percent}%")),
                )
        }
        (None, Some(account)) => item.child(account.provider.clone()).child(icon(
            Icon::Unreported,
            ICON_SMALL,
            ink(theme, T3),
        )),
        (None, None) => item.child(NO_ACCOUNT),
    }
}

fn tile(theme: &Theme) -> Div {
    div()
        .flex_none()
        .size(px(TILE))
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(RADIUS_CHIP_SMALL))
        .bg(ink(theme, CHIP_FILL))
}

fn block(theme: &Theme) -> Div {
    div()
        .flex()
        .flex_col()
        .gap(px(BLOCK_GAP))
        .px(px(ROW_PAD_X))
        .py(px(BLOCK_Y))
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T3))
}

fn loading(id: &'static str, theme: &Theme, cx: &App) -> AnyElement {
    if cx.reduce_motion() {
        return div()
            .size(px(LOADING_DOT))
            .rounded_full()
            .bg(ink(theme, T3))
            .into_any_element();
    }
    spinner(id, theme).into_any_element()
}

fn tipped(
    key: &dyn Fn(&str) -> ElementId,
    part: &str,
    target: Stateful<Div>,
    text: impl Into<SharedString>,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Stateful<Div> {
    tooltip(
        key(&format!("{part}-tip")),
        target,
        Edge::Frame,
        text,
        theme,
        window,
        cx,
    )
}

fn account_block(
    at: usize,
    account: &Account,
    on_pick: &OnPick,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Div {
    let key = |part: &str| -> ElementId {
        ElementId::NamedChild(Arc::new(("status-account", at).into()), part.into())
    };
    let letter: String = account
        .provider
        .chars()
        .filter(|c| c.is_alphanumeric())
        .take(1)
        .flat_map(char::to_uppercase)
        .collect();
    let mark = |glyph: Icon, color: Rgba| {
        div()
            .id(key("alert"))
            .flex_none()
            .child(icon(glyph, ICON_SMALL, color))
    };
    let alert = match &account.standing {
        Standing::Serving => None,
        Standing::Reauth(state) => Some(tipped(
            &key,
            "alert",
            mark(Icon::Warning, theme.color(ColorToken::StatusWarn)),
            format!("{NEEDS_REAUTH}: {state}"),
            theme,
            window,
            cx,
        )),
        Standing::Trouble(state) => Some(tipped(
            &key,
            "alert",
            mark(Icon::Notice, ink(theme, T2)),
            state.clone(),
            theme,
            window,
            cx,
        )),
    };
    let sign_in = matches!(account.standing, Standing::Reauth(_)).then(|| {
        let (on_pick, source) = (on_pick.clone(), account.source.clone());
        button(key("sign-in"), SIGN_IN, None, ButtonKind::Text, theme)
            .h(px(XS_BUTTON))
            .px_2()
            .text_size(px(FONT_SMALL))
            .on_click(move |_, window, cx| on_pick(&StatusPick::SignIn(source.clone()), window, cx))
    });
    let unreported = (account.windows.is_empty() && matches!(account.standing, Standing::Serving))
        .then(|| {
            let target = div()
                .id(key("unreported"))
                .flex()
                .items_center()
                .h(px(WINDOW_ROW))
                .child(icon(Icon::Unreported, ICON_SMALL, ink(theme, T3)));
            tipped(&key, "unreported", target, NOT_REPORTED, theme, window, cx)
        });
    let windows: Vec<Div> = account
        .windows
        .iter()
        .enumerate()
        .map(|(ix, usage)| window_row(&key, ix, usage, theme, window, cx))
        .collect();
    block(theme)
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(WINDOW_GAP))
                .min_w_0()
                .child(
                    tile(theme)
                        .font_weight(FontWeight::SEMIBOLD)
                        .text_color(ink(theme, T2))
                        .child(letter),
                )
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .flex()
                        .flex_col()
                        .child(
                            div()
                                .flex()
                                .items_center()
                                .gap(px(WINDOW_GAP))
                                .min_w_0()
                                .child(
                                    div()
                                        .flex_1()
                                        .min_w_0()
                                        .truncate()
                                        .text_size(px(FONT_BODY))
                                        .text_color(ink(theme, T1))
                                        .child(account.provider.clone()),
                                )
                                .children(account.plan.clone().map(|plan| {
                                    div().flex_none().text_color(ink(theme, T3)).child(plan)
                                })),
                        )
                        .children(account.email.clone().map(|email| {
                            div()
                                .truncate()
                                .text_size(px(FONT_SMALL))
                                .text_color(ink(theme, T3))
                                .child(email)
                        })),
                )
                .children(alert)
                .children(sign_in),
        )
        .children(windows)
        .children(unreported)
}

fn window_row(
    key: &dyn Fn(&str) -> ElementId,
    ix: usize,
    usage: &UsageWindow,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Div {
    let level = Level::of(usage.percent);
    let reset = usage.reset.as_ref().map(|reset| {
        let part = format!("reset-{ix}");
        let target = div()
            .id(key(&part))
            .flex()
            .flex_none()
            .items_center()
            .justify_end()
            .gap_1()
            .w(px(RESET_WIDTH))
            .font_features(tabular())
            .child(icon(Icon::Reset, ICON_SMALL, ink(theme, T3)))
            .child(div().flex_none().child(reset.left.clone()));
        tipped(key, &part, target, reset.at.clone(), theme, window, cx)
    });
    div()
        .flex()
        .items_center()
        .gap(px(WINDOW_GAP))
        .h(px(WINDOW_ROW))
        .child(
            div()
                .flex_none()
                .w(px(LABEL_WIDTH))
                .truncate()
                .child(caption(usage.label.clone(), theme)),
        )
        .child(meter_bar(f32::from(usage.percent) / 100.0, level.fill(theme), theme).flex_1())
        .child(
            div()
                .flex_none()
                .w(px(PERCENT_WIDTH))
                .text_right()
                .font_weight(FontWeight::MEDIUM)
                .font_features(tabular())
                .text_color(level.text(theme))
                .child(format!("{}%", usage.percent)),
        )
        .children(reset)
}

fn notice(mark: Div, title: &str, detail: Option<SharedString>, theme: &Theme) -> Div {
    block(theme)
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(ROW_GAP))
                .child(mark)
                .child(
                    div()
                        .text_size(px(FONT_BODY))
                        .text_color(ink(theme, T2))
                        .child(title.to_owned()),
                ),
        )
        .children(detail.map(|detail| div().w_full().truncate().child(detail)))
}

fn divider(theme: &Theme) -> Div {
    div()
        .flex_none()
        .w(px(HAIRLINE))
        .h(px(DIVIDER_HEIGHT))
        .mx_1()
        .bg(ink(theme, DIVIDER_INK))
}

impl RenderOnce for StatusBar {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let status = &self.status;
        let branch = self
            .item(StatusPick::Branch, "Branch", &theme)
            .child(icon(
                Icon::Branch,
                ICON_SMALL,
                theme.color(ColorToken::TextStatus),
            ))
            .map(|item| match &status.branch {
                None => item.child("no branch"),
                Some(branch) => item
                    .child(branch.name.clone())
                    .when(branch.ahead > 0, |item| {
                        item.child(dim(format!("↑{}", branch.ahead), &theme))
                    }),
            });
        let session = self
            .item(StatusPick::Session, "Session", &theme)
            .map(|item| match &status.session {
                None => item.child("no session"),
                Some(name) if name.is_empty() => item.child(dim(NEW_SESSION, &theme)),
                Some(name) => item.child(name.clone()),
            });
        let group = status.session_group.clone().map(|group| {
            let share = group.context.as_ref().map_or(0.0, |context| context.share);
            let context = self
                .item(StatusPick::Context, "Context", &theme)
                .child("ctx")
                .child(meter_bar(share, ink(&theme, BAR_FILL), &theme).w(px(CTX_BAR_WIDTH)))
                .children(group.context.map(|context| context.tokens));
            let classifier = self
                .item(StatusPick::Classifier, "Classifier", &theme)
                .child(format!("classifier {}", group.classifier));
            let cron = match self.cron_menu.clone() {
                Some(menu) => menu.into_any_element(),
                None => inert_cron(group.cron, &theme).into_any_element(),
            };
            div()
                .flex()
                .flex_none()
                .items_center()
                .gap_0p5()
                .child(context)
                .child(classifier)
                .child(cron)
                .child(divider(&theme))
        });
        let quota = self.quota(window, cx, &theme);
        div()
            .id(self.id.clone())
            .w_full()
            .h(px(STATUS_BAR_HEIGHT))
            .flex_none()
            .flex()
            .items_center()
            .gap_0p5()
            .px_2p5()
            .child(branch)
            .child(session)
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .px_2()
                    .truncate()
                    .text_size(px(TEXT_SMALL))
                    .text_color(theme.color(ColorToken::StatusDanger))
                    .children(status.problem.clone()),
            )
            .children(group)
            .child(quota)
    }
}
