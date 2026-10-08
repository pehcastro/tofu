use std::rc::Rc;

use gpui::{
    AnyView, App, ClickEvent, Div, ElementId, Entity, FocusHandle, FontWeight, Rgba, SharedString,
    Stateful, Window, div, prelude::*, px, relative,
};

use crate::component::{icon, status_item};
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
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{ICON_SMALL, STATUS_BAR_HEIGHT, STATUS_ITEM_HEIGHT, TEXT_SMALL};
use crate::theme::{ColorToken, Theme};

const PRODUCT_NAME: &str = "tofu";
const NO_ACCOUNT: &str = "no account";
const NO_ACCOUNT_CONNECTED: &str = "No account connected";
const ALL_PROVIDERS: &str = "View all providers";
const POP_OFFSET: f32 = 4.0;
const QUOTA_POP_WIDTH: f32 = 250.0;
const SECTION_TOP: f32 = 8.0;
const SECTION_BOTTOM: f32 = 10.0;
const SECTION_GAP: f32 = 12.0;
const SECTION_BREAK: f32 = 4.0;
const METER_GAP: f32 = 7.0;
const TILE: f32 = 24.0;
const EMPTY_GLYPH: f32 = 14.0;
const FOOTER_TOP: f32 = 4.0;
const FOOTER_BOTTOM: f32 = 6.0;
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
pub struct Quota {
    pub account: SharedString,
    pub window: SharedString,
    pub percent: u8,
}

#[derive(Clone, Default)]
pub struct Status {
    pub branch: Option<Branch>,
    pub session: Option<SharedString>,
    pub context: Option<ContextUse>,
    pub quota: Option<Quota>,
    pub classifier: usize,
    pub cron: usize,
    pub problem: Option<SharedString>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum StatusPick {
    Branch,
    Session,
    Context,
    AllProviders,
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
    version: Option<SharedString>,
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
            version: None,
            quota_open: false,
        }
    }

    pub fn cron_menu(mut self, menu: Option<AnyView>) -> Self {
        self.cron_menu = menu;
        self
    }

    pub fn version(mut self, version: Option<SharedString>) -> Self {
        self.version = version;
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
        let trigger = match &self.status.quota {
            None => item.child(NO_ACCOUNT),
            Some(quota) => item
                .child(quota.account.clone())
                .child(dim(quota.window.clone(), theme))
                .child(format!("{}%", quota.percent)),
        };
        let section = match &self.status.quota {
            Some(quota) => account_card(quota, theme),
            None => no_account(theme),
        };
        let hover = theme.color(ColorToken::StateHover);
        let (escaper, outside, picker) = (state.clone(), state.clone(), state);
        let on_pick = self.on_pick.clone();
        let body = div()
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
            .on_mouse_down_out(move |_, _, cx| set_open(&outside, false, cx))
            .child(section)
            .child(separator(theme).mx_0().my(px(SECTION_BREAK)))
            .child(
                row("status-all-providers", false, false, theme)
                    .hover(move |style| style.bg(hover))
                    .on_click(move |_, window, cx| {
                        set_open(&picker, false, cx);
                        on_pick(&StatusPick::AllProviders, window, cx);
                    })
                    .child(div().flex_1().child(ALL_PROVIDERS))
                    .child(icon(Icon::Arrow, ICON_SMALL, ink(theme, CAPTION_TEXT))),
            )
            .children(self.version.clone().map(|version| {
                div()
                    .px(px(ROW_PAD_X))
                    .pt(px(FOOTER_TOP))
                    .pb(px(FOOTER_BOTTOM))
                    .text_size(px(FONT_SMALL))
                    .font_features(tabular())
                    .text_color(ink(theme, T3))
                    .child(format!("{PRODUCT_NAME} {version}"))
            }));
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

fn account_card(quota: &Quota, theme: &Theme) -> Div {
    let (provider, window) = quota
        .window
        .split_once(' ')
        .unwrap_or((quota.account.as_ref(), quota.window.as_ref()));
    let letter: String = provider
        .chars()
        .filter(|c| c.is_alphanumeric())
        .take(1)
        .flat_map(char::to_uppercase)
        .collect();
    let account = (quota.account.as_ref() != provider).then(|| quota.account.clone());
    let level = Level::of(quota.percent);
    let share = f32::from(quota.percent) / 100.0;
    div()
        .flex()
        .flex_col()
        .gap(px(SECTION_GAP))
        .px(px(ROW_PAD_X))
        .pt(px(SECTION_TOP))
        .pb(px(SECTION_BOTTOM))
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(ROW_GAP))
                .min_w_0()
                .child(
                    tile(theme)
                        .text_size(px(FONT_SMALL))
                        .font_weight(FontWeight::SEMIBOLD)
                        .text_color(ink(theme, T2))
                        .child(letter),
                )
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .truncate()
                        .text_size(px(FONT_BODY))
                        .font_weight(FontWeight::MEDIUM)
                        .text_color(ink(theme, T1))
                        .child(provider.to_owned()),
                )
                .children(account.map(|account| {
                    div()
                        .flex_none()
                        .text_size(px(FONT_SMALL))
                        .text_color(ink(theme, T3))
                        .child(account)
                })),
        )
        .child(
            div()
                .flex()
                .flex_col()
                .gap(px(METER_GAP))
                .child(
                    div()
                        .flex()
                        .items_center()
                        .justify_between()
                        .child(caption(window.to_owned(), theme))
                        .child(
                            div()
                                .text_size(px(FONT_SMALL))
                                .font_weight(FontWeight::MEDIUM)
                                .font_features(tabular())
                                .text_color(level.text(theme))
                                .child(format!("{}%", quota.percent)),
                        ),
                )
                .child(meter_bar(share, level.fill(theme), theme).w_full()),
        )
}

fn no_account(theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(ROW_GAP))
        .px(px(ROW_PAD_X))
        .pt(px(SECTION_TOP))
        .pb(px(SECTION_BOTTOM))
        .child(tile(theme).child(glyph(Glyph::Lock, EMPTY_GLYPH, ink(theme, T3))))
        .child(
            div()
                .text_size(px(FONT_BODY))
                .text_color(ink(theme, T2))
                .child(NO_ACCOUNT_CONNECTED),
        )
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
            .child(status.session.clone().unwrap_or("no session".into()));
        let share = status.context.as_ref().map_or(0.0, |context| context.share);
        let context = self
            .item(StatusPick::Context, "Context", &theme)
            .child("ctx")
            .child(meter_bar(share, ink(&theme, BAR_FILL), &theme).w(px(CTX_BAR_WIDTH)))
            .children(
                status
                    .context
                    .as_ref()
                    .map(|context| context.tokens.clone()),
            );
        let quota = self.quota(window, cx, &theme);
        let classifier = self
            .item(StatusPick::Classifier, "Classifier", &theme)
            .child(format!("classifier {}", status.classifier));
        let cron = match self.cron_menu {
            Some(menu) => menu.into_any_element(),
            None => inert_cron(status.cron, &theme).into_any_element(),
        };
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
            .child(context)
            .child(quota)
            .child(classifier)
            .child(cron)
    }
}
