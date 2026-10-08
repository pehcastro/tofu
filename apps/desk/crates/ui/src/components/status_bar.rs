use std::rc::Rc;

use gpui::{
    AnyView, App, ClickEvent, Div, ElementId, Entity, FocusHandle, FontWeight, SharedString,
    Stateful, Window, div, prelude::*, px,
};

use crate::component::{icon, status_item};
use crate::components::card::caption;
use crate::components::list::bare_row;
use crate::components::overlay::{Align, Placement, Popover, Side};
use crate::components::paint::ink;
use crate::components::size::{
    CAPTION_TEXT, FONT_SMALL, MENU_PAD, POPOVER_PAD_X, POPOVER_PAD_Y, T3,
};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{ICON_SMALL, STATUS_BAR_HEIGHT, STATUS_ITEM_HEIGHT, TEXT_SMALL};
use crate::theme::{ColorToken, Theme};

const PRODUCT_NAME: &str = "tofu";
const NO_ACCOUNT: &str = "no account";
const ALL_PROVIDERS: &str = "View all providers";
const POP_OFFSET: f32 = 4.0;
const QUOTA_POP_WIDTH: f32 = 250.0;
const ACCOUNT_LINE: f32 = 17.0;
const POP_CAPTION_X: f32 = 10.0;
const POP_CAPTION_TOP: f32 = 8.0;
const POP_CAPTION_BOTTOM: f32 = 6.0;
const ACCOUNT_HEAD_BOTTOM: f32 = 10.0;
const CTX_BAR_WIDTH: f32 = 40.0;
const CTX_BAR_HEIGHT: f32 = 5.0;
const CTX_BAR_RADIUS: f32 = 3.0;
const CTX_TRACK: f32 = 0.08;
const CTX_FILL: f32 = 0.6;

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
        let small = |text: SharedString| {
            div()
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, T3))
                .child(text)
        };
        let (name, found) = match &self.status.quota {
            Some(quota) => (
                quota.account.clone(),
                Some(format!("{} window at {}%", quota.window, quota.percent).into()),
            ),
            None => (NO_ACCOUNT.into(), None),
        };
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
            .child(
                div()
                    .flex()
                    .flex_col()
                    .pt(px(POP_CAPTION_TOP))
                    .px(px(POP_CAPTION_X))
                    .pb(px(ACCOUNT_HEAD_BOTTOM))
                    .line_height(px(ACCOUNT_LINE))
                    .child(name)
                    .children(found.map(small)),
            )
            .children(self.version.clone().map(|version| {
                div()
                    .px(px(POP_CAPTION_X))
                    .pb(px(POP_CAPTION_BOTTOM))
                    .child(caption(format!("{PRODUCT_NAME} {version}"), theme))
            }))
            .child(
                bare_row("status-all-providers", false, false, theme)
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

fn context_bar(share: f32, theme: &Theme) -> Div {
    div()
        .flex_none()
        .w(px(CTX_BAR_WIDTH))
        .h(px(CTX_BAR_HEIGHT))
        .rounded(px(CTX_BAR_RADIUS))
        .bg(ink(theme, CTX_TRACK))
        .overflow_hidden()
        .child(
            div()
                .h_full()
                .w(px(CTX_BAR_WIDTH * share.clamp(0.0, 1.0)))
                .rounded(px(CTX_BAR_RADIUS))
                .bg(ink(theme, CTX_FILL)),
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
            .child(context_bar(share, &theme))
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
