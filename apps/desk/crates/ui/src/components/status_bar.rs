use std::rc::Rc;

use gpui::{App, ClickEvent, Div, ElementId, SharedString, Stateful, Window, div, prelude::*, px};

use crate::component::{icon, status_item};
use crate::components::paint::ink;
use crate::components::size::T3;
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{ICON_SMALL, STATUS_BAR_HEIGHT, TEXT_SMALL};
use crate::theme::{ColorToken, Theme};

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
    Quota,
    Classifier,
    Cron,
}

type OnPick = Rc<dyn Fn(&StatusPick, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct StatusBar {
    id: ElementId,
    status: Status,
    on_pick: OnPick,
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
        }
    }

    fn item(&self, pick: StatusPick, label: &'static str, theme: &Theme) -> Stateful<Div> {
        let on_pick = self.on_pick.clone();
        status_item(label, label, theme)
            .on_click(move |_: &ClickEvent, window, cx| on_pick(&pick, window, cx))
    }
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
    fn render(self, _: &mut Window, cx: &mut App) -> impl IntoElement {
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
        let quota =
            self.item(StatusPick::Quota, "Limits", &theme)
                .map(|item| match &status.quota {
                    None => item.child("no account"),
                    Some(quota) => item
                        .child(quota.account.clone())
                        .child(dim(quota.window.clone(), &theme))
                        .child(format!("{}%", quota.percent)),
                });
        let classifier = self
            .item(StatusPick::Classifier, "Classifier", &theme)
            .child(format!("classifier {}", status.classifier));
        let cron = self
            .item(StatusPick::Cron, "Cron", &theme)
            .child(format!("cron {}", status.cron));
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
