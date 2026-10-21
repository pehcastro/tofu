use desk_motion::tokens::{EASE_OUT, TOGGLE_MS};
use gpui::{
    App, Context, Div, ElementId, Entity, FontWeight, ImageSource, Motion, ObjectFit, Rgba,
    SharedString, StyleRefinement, StyledImage, Window, div, img, linear_color_stop,
    linear_gradient, prelude::*, px,
};

use crate::components::glyph::{Glyph, MARK_BLEED};
use crate::components::paint::{drop, glyph, halo, ink, spinning_arc, tint};
use crate::components::size::{
    AVATAR_FEED, AVATAR_LARGE, AVATAR_LIST, AVATAR_ROW, AVATAR_TINT_FEED, AVATAR_TINT_LIST,
    CAPTION_TEXT, CHECK, CHIP_FILL, DONE_MARK, DONE_OFFSET, FONT_AVATAR_FEED, FONT_AVATAR_LARGE,
    FONT_AVATAR_LIST, FONT_AVATAR_ROW, FONT_SMALL, MARK_RING, RADIUS_CHIP_SMALL, RING_OUTSET,
    RING_TRACK, SPINNER, SPINNER_TRACK, T1, T3, WAIT_MARK, WAIT_OFFSET,
};
use crate::live::ActiveTheme;
use crate::metrics::{ACCOUNT_GRADIENT_ANGLE, AVATAR};
use crate::theme::{ColorToken, Theme};

const STACK_OVERLAP: f32 = 0.35;
const STACK_FAN: f32 = 0.15;
const STACK_GROUP: &str = "avatar-stack";
const TIP_PAD_X: f32 = 8.0;
const TIP_PAD_Y: f32 = 4.0;
const TIP_SHADOW_Y: f32 = 4.0;
const TIP_SHADOW_BLUR: f32 = 12.0;
const PERSON_MENU: f32 = 32.0;
const FONT_PERSON_MENU: f32 = 14.0;
const NOBODY_TOP: f32 = 0.2;
const NOBODY_GAP: f32 = 0.06;
const NOBODY_HEAD: f32 = 0.32;
const NOBODY_BODY_WIDTH: f32 = 0.56;
const NOBODY_BODY_HEIGHT: f32 = 0.26;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Person {
    pub name: SharedString,
    pub kind: AgentKind,
}

#[derive(IntoElement)]
pub struct AvatarStack {
    id: ElementId,
    shown: Vec<(SharedString, Rgba)>,
    hidden: usize,
    size: AvatarSize,
    backing: Rgba,
    more_fill: Rgba,
    more_text: Rgba,
}

pub fn avatar_stack(
    people: &[Person],
    max: usize,
    key: impl Into<ElementId>,
    size: AvatarSize,
    theme: &Theme,
) -> AvatarStack {
    AvatarStack {
        id: key.into(),
        shown: people
            .iter()
            .take(max)
            .map(|person| (person.name.clone(), person.kind.color(theme)))
            .collect(),
        hidden: people.len().saturating_sub(max),
        size,
        backing: theme.color(ColorToken::AvatarRing),
        more_fill: ink(theme, CHIP_FILL),
        more_text: ink(theme, CAPTION_TEXT),
    }
}

impl RenderOnce for AvatarStack {
    fn render(self, _: &mut Window, _: &mut App) -> impl IntoElement {
        let (side, font, alpha) = self.size.metrics();
        let backing = self.backing;
        let disc = |ix: usize| {
            div()
                .id(("stack-disc", ix))
                .flex_none()
                .size(px(side))
                .flex()
                .items_center()
                .justify_center()
                .rounded_full()
                .font_weight(FontWeight::SEMIBOLD)
                .text_size(px(font))
                .line_height(px(side))
                .bg(backing)
                .shadow(vec![halo(backing, MARK_RING)])
                .when(ix > 0, |disc| {
                    disc.ml(px(-side * STACK_OVERLAP))
                        .group_hover(STACK_GROUP, |style| style.ml(px(side * STACK_FAN)))
                        .transitions(|transitions| {
                            transitions.ml(Motion::new(TOGGLE_MS).with_easing(EASE_OUT))
                        })
                })
        };
        let face = |fill: Rgba, color: Rgba, text: SharedString| {
            div()
                .size_full()
                .flex()
                .items_center()
                .justify_center()
                .rounded_full()
                .bg(fill)
                .text_color(color)
                .child(text)
        };
        let count = self.shown.len();
        let steps = (count + usize::from(self.hidden > 0)).saturating_sub(1) as f32;
        let width = |step: f32| px(side + steps * side * step);
        let shown = self
            .shown
            .into_iter()
            .enumerate()
            .map(|(ix, (name, tone))| {
                let initial: SharedString = name.chars().take(1).collect::<String>().into();
                disc(ix)
                    .child(face(tint(tone, alpha), tone, initial))
                    .tooltip(move |_, cx| cx.new(|_| NameTip(name.clone())).into())
            });
        let more = (self.hidden > 0).then(|| {
            disc(count).child(face(
                self.more_fill,
                self.more_text,
                format!("+{}", self.hidden).into(),
            ))
        });
        div()
            .id(self.id)
            .group(STACK_GROUP)
            .flex()
            .flex_none()
            .items_center()
            .w(width(1.0 - STACK_OVERLAP))
            .hover(|style| style.w(width(1.0 + STACK_FAN)))
            .transitions(|transitions| transitions.w(Motion::new(TOGGLE_MS).with_easing(EASE_OUT)))
            .children(shown)
            .children(more)
    }
}

pub(crate) struct NameTip(pub(crate) SharedString);

impl Render for NameTip {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        div()
            .px(px(TIP_PAD_X))
            .py(px(TIP_PAD_Y))
            .rounded(px(RADIUS_CHIP_SMALL))
            .bg(theme.color(ColorToken::ToastFill))
            .shadow(vec![drop(
                theme.color(ColorToken::CardsInnerShadow),
                TIP_SHADOW_Y,
                TIP_SHADOW_BLUR,
            )])
            .text_size(px(FONT_SMALL))
            .text_color(theme.color(ColorToken::TextStrong))
            .child(self.0.clone())
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum AgentKind {
    GoDev,
    TsDev,
    Explore,
    Research,
    Qa,
    Browser,
    PyDev,
    RustDev,
    RustDesigner,
    Other(char),
}

impl AgentKind {
    pub const ALL: [AgentKind; 9] = [
        AgentKind::GoDev,
        AgentKind::TsDev,
        AgentKind::Explore,
        AgentKind::Research,
        AgentKind::Qa,
        AgentKind::Browser,
        AgentKind::PyDev,
        AgentKind::RustDev,
        AgentKind::RustDesigner,
    ];

    pub fn named(name: &str) -> AgentKind {
        AgentKind::ALL
            .into_iter()
            .find(|kind| kind.name() == name)
            .unwrap_or_else(|| {
                AgentKind::Other(name.chars().next().map_or('?', |c| c.to_ascii_uppercase()))
            })
    }

    pub fn name(self) -> &'static str {
        match self {
            AgentKind::GoDev => "go-dev",
            AgentKind::TsDev => "ts-dev",
            AgentKind::Explore => "explore",
            AgentKind::Research => "research",
            AgentKind::Qa => "qa",
            AgentKind::Browser => "browser",
            AgentKind::PyDev => "py-dev",
            AgentKind::RustDev => "rust-dev",
            AgentKind::RustDesigner => "rust-designer",
            AgentKind::Other(_) => "agent",
        }
    }

    fn initial(self) -> SharedString {
        match self {
            AgentKind::GoDev => "G".into(),
            AgentKind::TsDev => "T".into(),
            AgentKind::Explore => "E".into(),
            AgentKind::Research => "R".into(),
            AgentKind::Qa => "Q".into(),
            AgentKind::Browser => "B".into(),
            AgentKind::PyDev => "P".into(),
            AgentKind::RustDev => "R".into(),
            AgentKind::RustDesigner => "D".into(),
            AgentKind::Other(initial) => initial.to_string().into(),
        }
    }

    pub fn color(self, theme: &Theme) -> Rgba {
        theme.color(match self {
            AgentKind::GoDev => ColorToken::AgentsGoDev,
            AgentKind::TsDev => ColorToken::AgentsTsDev,
            AgentKind::Explore => ColorToken::AgentsExplore,
            AgentKind::Research => ColorToken::AgentsResearch,
            AgentKind::Qa => ColorToken::AgentsQa,
            AgentKind::Browser => ColorToken::AgentsBrowser,
            AgentKind::PyDev => ColorToken::AgentsPyDev,
            AgentKind::RustDev => ColorToken::AgentsRustDev,
            AgentKind::RustDesigner => ColorToken::AgentsRustDesigner,
            AgentKind::Other(_) => ColorToken::TextMuted,
        })
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum AgentStatus {
    Working,
    Asking,
    Failed,
    Finished,
}

impl AgentStatus {
    pub const ALL: [AgentStatus; 4] = [
        AgentStatus::Working,
        AgentStatus::Asking,
        AgentStatus::Failed,
        AgentStatus::Finished,
    ];

    pub fn group(self) -> &'static str {
        match self {
            AgentStatus::Working => "Running",
            AgentStatus::Asking => "Asking the lead",
            AgentStatus::Failed => "Failed",
            AgentStatus::Finished => "Finished",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Agent {
    pub kind: AgentKind,
    pub instance: u32,
    pub status: AgentStatus,
}

impl Agent {
    pub fn name(&self) -> SharedString {
        format!("{}-{}", self.kind.name(), self.instance).into()
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum AvatarSize {
    Feed,
    List,
    Row,
    Large,
}

impl AvatarSize {
    fn metrics(self) -> (f32, f32, f32) {
        match self {
            AvatarSize::Feed => (AVATAR_FEED, FONT_AVATAR_FEED, AVATAR_TINT_FEED),
            AvatarSize::List => (AVATAR_LIST, FONT_AVATAR_LIST, AVATAR_TINT_LIST),
            AvatarSize::Row => (AVATAR_ROW, FONT_AVATAR_ROW, AVATAR_TINT_LIST),
            AvatarSize::Large => (AVATAR_LARGE, FONT_AVATAR_LARGE, AVATAR_TINT_LIST),
        }
    }
}

pub fn kind_avatar(kind: AgentKind, size: AvatarSize, theme: &Theme) -> Div {
    tinted(kind, size, 0.0, theme).child(div().relative().child(kind.initial()))
}

fn tinted(kind: AgentKind, size: AvatarSize, inset: f32, theme: &Theme) -> Div {
    let (side, font, alpha) = size.metrics();
    let tone = kind.color(theme);
    div()
        .relative()
        .flex_none()
        .size(px(side))
        .flex()
        .items_center()
        .justify_center()
        .text_color(tone)
        .font_weight(FontWeight::SEMIBOLD)
        .text_size(px(font))
        .line_height(px(side))
        .child(
            div()
                .absolute()
                .inset(px(inset))
                .rounded_full()
                .bg(tint(tone, alpha)),
        )
}

pub fn avatar(id: impl Into<ElementId>, agent: &Agent, size: AvatarSize, theme: &Theme) -> Div {
    let (side, _, _) = size.metrics();
    let backing = theme.color(ColorToken::AvatarRing);
    let mark = |side: f32, offset: f32, fill: Rgba| {
        div()
            .absolute()
            .right(px(-offset))
            .bottom(px(-offset))
            .size(px(side))
            .flex()
            .items_center()
            .justify_center()
            .rounded_full()
            .bg(fill)
            .shadow(vec![halo(backing, MARK_RING)])
    };
    let live = theme.color(ColorToken::StatusLive);
    let working = agent.status == AgentStatus::Working;
    let inset = if working { RING_OUTSET } else { 0.0 };
    tinted(agent.kind, size, inset, theme)
        .when(working, |disc| {
            disc.child(div().absolute().inset(px(MARK_BLEED)).child(spinning_arc(
                id,
                side - 2.0 * MARK_BLEED,
                tint(live, RING_TRACK),
                live,
                theme,
            )))
        })
        .child(div().relative().child(agent.kind.initial()))
        .map(|disc| match agent.status {
            AgentStatus::Working => disc,
            AgentStatus::Finished => disc.child(
                mark(DONE_MARK, DONE_OFFSET, theme.color(ColorToken::GitAdded)).child(glyph(
                    Glyph::Check,
                    CHECK,
                    theme.color(ColorToken::TextStrong),
                )),
            ),
            AgentStatus::Asking => disc.child(mark(
                WAIT_MARK,
                WAIT_OFFSET,
                theme.color(ColorToken::StatusWarn),
            )),
            AgentStatus::Failed => disc.child(mark(
                WAIT_MARK,
                WAIT_OFFSET,
                theme.color(ColorToken::StatusDanger),
            )),
        })
}

pub struct AvatarView {
    id: ElementId,
    agent: Agent,
    size: AvatarSize,
}

impl AvatarView {
    pub fn new(
        id: impl Into<ElementId>,
        agent: Agent,
        size: AvatarSize,
        cx: &mut App,
    ) -> Entity<AvatarView> {
        let id = id.into();
        cx.new(|_| AvatarView { id, agent, size })
    }

    pub fn cached(view: &Entity<AvatarView>, cx: &App) -> impl IntoElement + use<> {
        let (side, _, _) = view.read(cx).size.metrics();
        view.clone()
            .cached(StyleRefinement::default().size(px(side)).flex_none())
    }
}

impl Render for AvatarView {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        avatar(self.id.clone(), &self.agent, self.size, &theme)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum PersonSize {
    Title,
    Menu,
}

impl PersonSize {
    fn metrics(self) -> (f32, f32) {
        match self {
            PersonSize::Title => (AVATAR, FONT_AVATAR_LIST),
            PersonSize::Menu => (PERSON_MENU, FONT_PERSON_MENU),
        }
    }
}

#[derive(Clone)]
pub struct Face {
    pub letter: SharedString,
    pub picture: Option<ImageSource>,
}

pub fn person_avatar(face: Option<Face>, size: PersonSize, theme: &Theme) -> Div {
    let (side, font) = size.metrics();
    let disc = div()
        .relative()
        .flex_none()
        .size(px(side))
        .flex()
        .items_center()
        .rounded_full();
    let Some(face) = face else {
        let shape = ink(theme, T3);
        return disc
            .flex_col()
            .pt(px(side * NOBODY_TOP))
            .gap(px(side * NOBODY_GAP))
            .bg(ink(theme, CHIP_FILL))
            .child(div().size(px(side * NOBODY_HEAD)).rounded_full().bg(shape))
            .child(
                div()
                    .w(px(side * NOBODY_BODY_WIDTH))
                    .h(px(side * NOBODY_BODY_HEIGHT))
                    .rounded_t_full()
                    .bg(shape),
            );
    };
    disc.justify_center()
        .bg(linear_gradient(
            ACCOUNT_GRADIENT_ANGLE,
            linear_color_stop(theme.color(ColorToken::AccountFrom), 0.0),
            linear_color_stop(theme.color(ColorToken::AccountTo), 1.0),
        ))
        .text_size(px(font))
        .line_height(px(side))
        .font_weight(FontWeight::SEMIBOLD)
        .text_color(ink(theme, T1))
        .child(face.letter)
        .children(face.picture.map(|picture| {
            img(picture)
                .absolute()
                .top_0()
                .left_0()
                .size_full()
                .rounded_full()
                .object_fit(ObjectFit::Cover)
        }))
}

pub fn spinner(id: impl Into<ElementId>, theme: &Theme) -> impl IntoElement {
    spinning_arc(
        id,
        SPINNER,
        ink(theme, SPINNER_TRACK),
        ink(theme, T3),
        theme,
    )
}
