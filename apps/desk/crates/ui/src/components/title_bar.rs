use std::rc::Rc;

use desk_core::control::Control;
use gpui::{
    AnyElement, App, ClickEvent, Div, ElementId, Entity, FocusHandle, FontWeight, ImageSource,
    KeyContext, Rgba, SharedString, Size, Stateful, Window, WindowControlArea, div, img,
    prelude::*, px,
};

use crate::component::{control, icon, wrapped_icon_button};
use crate::components::avatar::{Face, PersonSize, person_avatar};
use crate::components::chip::mono;
use crate::components::glyph::Glyph;
use crate::components::overlay::{Align, Placement, Popover, Side, toggles_on_press};
use crate::components::paint::{focus_ring, glyph, ink};
use crate::components::sidebar::{CONTENT_LEFT, CONTENT_RIGHT, LayoutEdge, probe};
use crate::components::size::{FONT_SMALL, MENU_PAD, POPOVER_PAD_X, POPOVER_PAD_Y, T3};
use crate::components::tooltip::{Edge, tooltip};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{
    CAPTION_WIDTH, CONTROL, ICON, ICON_SMALL, RADIUS_CAPTION, RADIUS_CONTROL, RADIUS_TAB,
    SIDEBAR_CLOSED_WIDTH, TAB_HEIGHT, TITLE_BAR_HEIGHT,
};
use crate::theme::{ColorToken, Mode, Theme};

const TITLE_GAP: f32 = 4.0;
const PRODUCT_NAME: &str = "tofu";
const MARK_HEIGHT: f32 = 18.0;
const MARK_WIDTH: f32 = 17.0;
const TO_LIGHT: &str = "Switch to light theme";
const TO_DARK: &str = "Switch to dark theme";
const POP_OFFSET: f32 = 4.0;
const ACCOUNT_POP_WIDTH: f32 = 250.0;
const ACCOUNT_LINE: f32 = 17.0;
const POP_CAPTION_X: f32 = 10.0;
const POP_CAPTION_TOP: f32 = 8.0;
const ACCOUNT_HEAD_BOTTOM: f32 = 10.0;
const ASKING: &str = "Asking gh who is signed in";
const SIGNED_OUT: &str = "Not signed in to GitHub";
const SIGN_IN: &str = "gh auth login";
const FOCUS_NAME: &str = "focus";
const CAPTION_GAP: f32 = 2.0;
const CAPTION_MARGIN: f32 = 6.0;
const DARK_GLYPH_ALPHA: f32 = 0.8;

pub fn named_stop(
    element: Stateful<Div>,
    name: impl Into<SharedString>,
    theme: &Theme,
) -> Stateful<Div> {
    let mut context = KeyContext::default();
    context.set(FOCUS_NAME, name);
    focus_ring(element, theme).key_context(context)
}

pub fn focused_name(window: &Window) -> Option<SharedString> {
    window
        .context_stack()
        .iter()
        .rev()
        .find_map(|context| context.get(FOCUS_NAME).cloned())
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TitlePick {
    Sidebar,
    Palette,
    NewWorkspace,
    Theme,
    Close,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TitlePop {
    Account,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum WindowKeys {
    Live,
    Shown,
}

#[derive(Clone)]
pub struct GithubUser {
    pub login: SharedString,
    pub name: Option<SharedString>,
    pub picture: Option<ImageSource>,
}

#[derive(Clone)]
pub enum Github {
    Asking,
    SignedOut,
    SignedIn(GithubUser),
}

impl Github {
    fn face(&self) -> Option<Face> {
        let Github::SignedIn(user) = self else {
            return None;
        };
        let first = user.login.chars().find(|c| c.is_alphanumeric())?;
        Some(Face {
            letter: first.to_lowercase().collect::<String>().into(),
            picture: user.picture.clone(),
        })
    }

    fn tip(&self) -> Option<SharedString> {
        match self {
            Github::Asking => None,
            Github::SignedOut => Some(SIGNED_OUT.into()),
            Github::SignedIn(user) => Some(user.login.clone()),
        }
    }
}

#[derive(Clone)]
pub struct Title {
    pub sidebar_open: bool,
    pub github: Github,
    pub keys: WindowKeys,
    pub open: Option<TitlePop>,
    pub tabs_x: f32,
}

#[derive(Clone)]
pub struct Marks {
    pub on_dark: ImageSource,
    pub on_light: ImageSource,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Shade {
    Light,
    Dark,
}

#[derive(Clone, Copy)]
pub struct CaptionInk {
    pub glyph: ColorToken,
    pub glyph_alpha: f32,
    pub strong: ColorToken,
    pub hover: ColorToken,
}

impl CaptionInk {
    pub const DESK: Self = Self {
        glyph: ColorToken::TextCaption,
        glyph_alpha: 1.0,
        strong: ColorToken::TextStrong,
        hover: ColorToken::StateCaptionHover,
    };

    pub const fn intro(under: Shade) -> Self {
        let (glyph, glyph_alpha) = match under {
            Shade::Light => (ColorToken::IntroSendIcon, DARK_GLYPH_ALPHA),
            Shade::Dark => (ColorToken::IntroModelText, 1.0),
        };
        Self {
            glyph,
            glyph_alpha,
            strong: ColorToken::IntroSendFill,
            hover: ColorToken::IntroComposerFill,
        }
    }
}

pub fn captions_span(scale: f32) -> Size<u32> {
    let width = 3.0 * CAPTION_WIDTH + 2.0 * CAPTION_GAP + 2.0 * CAPTION_MARGIN;
    Size {
        width: (width * scale).ceil() as u32,
        height: (TITLE_BAR_HEIGHT * scale).ceil() as u32,
    }
}

type OnPick = Rc<dyn Fn(&TitlePick, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct TitleBar {
    id: ElementId,
    title: Title,
    tabs: Option<AnyElement>,
    on_pick: OnPick,
    controls_only: bool,
    marks: Option<Marks>,
    ink: CaptionInk,
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
            controls_only: false,
            marks: None,
            ink: CaptionInk::DESK,
        }
    }

    pub fn caption_ink(mut self, ink: CaptionInk) -> Self {
        self.ink = ink;
        self
    }

    pub fn marks(mut self, marks: Marks) -> Self {
        self.marks = Some(marks);
        self
    }

    pub fn controls_only(mut self) -> Self {
        self.controls_only = true;
        self
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
}

impl PopRow {
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

fn person_menu(github: &Github, theme: &Theme) -> Div {
    let small = div().text_size(px(FONT_SMALL)).text_color(ink(theme, T3));
    let (name, below) = match github {
        Github::Asking => (ASKING.into(), None),
        Github::SignedOut => (
            SIGNED_OUT.into(),
            Some(small.font_family(mono(theme)).child(SIGN_IN)),
        ),
        Github::SignedIn(user) => (
            user.name.clone().unwrap_or_else(|| user.login.clone()),
            Some(small.child(user.login.clone())),
        ),
    };
    div()
        .flex()
        .items_center()
        .gap(px(POP_CAPTION_X))
        .pt(px(POP_CAPTION_TOP))
        .px(px(POP_CAPTION_X))
        .pb(px(ACCOUNT_HEAD_BOTTOM))
        .line_height(px(ACCOUNT_LINE))
        .child(person_avatar(github.face(), PersonSize::Menu, theme))
        .child(div().flex().flex_col().child(name).children(below))
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

    fn hover(self, ink: CaptionInk) -> ColorToken {
        match self {
            Caption::Minimize | Caption::Maximize | Caption::Restore => ink.hover,
            Caption::Close => ColorToken::StateCloseHover,
        }
    }

    fn act(self, on_pick: &OnPick, window: &mut Window, cx: &mut App) {
        eprintln!("desk: caption {} click reached the client", self.label());
        cx.stop_propagation();
        match self {
            Caption::Minimize => window.minimize_window(),
            Caption::Maximize | Caption::Restore => window.zoom_window(),
            Caption::Close => on_pick(&TitlePick::Close, window, cx),
        }
    }

    fn button(
        self,
        keys: WindowKeys,
        ink: CaptionInk,
        on_pick: &OnPick,
        theme: &Theme,
    ) -> impl IntoElement {
        let label = self.label();
        let on_pick = on_pick.clone();
        let hover = theme.color(self.hover(ink));
        let strong = theme.color(ink.strong);
        let glyph = theme.color(ink.glyph);
        let glyph = Rgba {
            alpha: glyph.alpha * ink.glyph_alpha,
            ..glyph
        };
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
                    .on_click(move |_, window, cx| self.act(&on_pick, window, cx))
            })
            .hover(move |style| style.bg(hover))
            .child(
                icon(self.icon(), ICON_SMALL, glyph)
                    .group_hover(label, move |style| style.text_color(strong)),
            )
    }
}

fn captions(
    keys: WindowKeys,
    ink: CaptionInk,
    maximized: bool,
    on_pick: &OnPick,
    theme: &Theme,
) -> impl IntoElement {
    let middle = if maximized {
        Caption::Restore
    } else {
        Caption::Maximize
    };
    div()
        .flex()
        .items_center()
        .gap(px(CAPTION_GAP))
        .mx(px(CAPTION_MARGIN))
        .child(Caption::Minimize.button(keys, ink, on_pick, theme))
        .child(middle.button(keys, ink, on_pick, theme))
        .child(Caption::Close.button(keys, ink, on_pick, theme))
}

fn product_name(marks: Option<&Marks>, theme: &Theme) -> Div {
    let name = div().flex().items_center();
    let Some(marks) = marks else {
        return name
            .font_weight(FontWeight::BOLD)
            .text_color(theme.color(ColorToken::TextName))
            .child(PRODUCT_NAME);
    };
    let mark = match theme.mode() {
        Mode::Dark => marks.on_dark.clone(),
        Mode::Light => marks.on_light.clone(),
    };
    name.child(img(mark).w(px(MARK_WIDTH)).h(px(MARK_HEIGHT)))
}

impl RenderOnce for TitleBar {
    fn render(mut self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let bar = div()
            .id(self.id.clone())
            .w_full()
            .h(px(TITLE_BAR_HEIGHT))
            .flex_none()
            .flex()
            .items_center()
            .gap(px(TITLE_GAP))
            .when(self.title.keys == WindowKeys::Live, |bar| {
                bar.window_control_area(WindowControlArea::Drag)
            });
        let controls = (!cfg!(target_os = "macos")).then(|| {
            captions(
                self.title.keys,
                self.ink,
                window.is_maximized(),
                &self.on_pick,
                &theme,
            )
        });
        if self.controls_only {
            return bar
                .child(product_name(self.marks.as_ref(), &theme).flex_1().pl_4())
                .children(controls);
        }
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
                    named_stop(
                        control("new-workspace", Control::NewWorkspace.label(), &theme),
                        "new-workspace",
                        &theme,
                    )
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
        };
        let toggle = |pop: TitlePop| {
            let (state, focus) = (state.clone(), focus.clone());
            move |window: &mut Window, cx: &mut App| {
                let next = (shown != Some(pop)).then_some(pop);
                state.update(cx, |opened, cx| {
                    opened.pop = next;
                    cx.notify();
                });
                if next.is_some() {
                    window.focus(&focus, cx);
                }
            }
        };
        let under = Placement {
            side: Side::Bottom,
            align: Align::End,
            offset: POP_OFFSET,
        };
        let column = title.tabs_x.max(SIDEBAR_CLOSED_WIDTH + TITLE_GAP) - TITLE_GAP;
        let icon_inset = (CONTROL - ICON) / 2.0;
        let (theme_glyph, theme_tip) = match theme.mode() {
            Mode::Dark => (Glyph::Sun, TO_LIGHT),
            Mode::Light => (Glyph::Moon, TO_DARK),
        };
        let switch_theme = tooltip(
            "title-theme-tip",
            named_stop(control("theme", theme_tip, &theme), "theme", &theme)
                .occlude()
                .size(px(CONTROL))
                .rounded(px(RADIUS_CONTROL))
                .child(glyph(theme_glyph, ICON, theme.color(ColorToken::TextIcon)))
                .on_click(self.picked(TitlePick::Theme)),
            Edge::Frame,
            theme_tip,
            &theme,
            window,
            cx,
        );
        let search = title.sidebar_open.then(|| {
            tooltip(
                "title-search-tip",
                named_stop(
                    wrapped_icon_button(
                        "palette",
                        Icon::Search,
                        "Command palette",
                        &theme,
                        |icon| probe("palette", LayoutEdge::Right, icon),
                    ),
                    "palette",
                    &theme,
                )
                .occlude()
                .on_click(self.picked(TitlePick::Palette)),
                Edge::Frame,
                Control::Palette.label(),
                &theme,
                window,
                cx,
            )
        });
        let account_tip = title
            .github
            .tip()
            .unwrap_or(Control::Account.label().into());
        let account = toggles_on_press(
            named_stop(
                control("account", Control::Account.label(), &theme),
                "account",
                &theme,
            )
            .occlude()
            .size(px(CONTROL))
            .rounded_full()
            .child(person_avatar(
                title.github.face(),
                PersonSize::Title,
                &theme,
            )),
            toggle(TitlePop::Account),
        );
        let account = tooltip(
            "title-account-tip",
            account,
            Edge::Frame,
            account_tip,
            &theme,
            window,
            cx,
        );
        bar.child(
            div()
                .w(px(column))
                .h_full()
                .flex_none()
                .flex()
                .items_center()
                .gap_1()
                .pl(px(CONTENT_LEFT - icon_inset))
                .pr(px((column - CONTENT_RIGHT - icon_inset).max(0.0)))
                .child(
                    named_stop(
                        wrapped_icon_button(
                            "sidebar-toggle",
                            Icon::Sidebar,
                            "Toggle sidebar",
                            &theme,
                            |icon| probe("sidebar-toggle", LayoutEdge::Left, icon),
                        ),
                        "sidebar-toggle",
                        &theme,
                    )
                    .occlude()
                    .on_click(self.picked(TitlePick::Sidebar)),
                )
                .when(title.sidebar_open, |name| {
                    name.child(product_name(self.marks.as_ref(), &theme).pl_1p5())
                })
                .when_some(search, |row, search| {
                    row.child(div().flex_1()).child(search)
                }),
        )
        .child(tabs)
        .child(switch_theme)
        .child(
            Popover::new("title-account-pop", account)
                .open(shown == Some(TitlePop::Account))
                .placement(under)
                .width(ACCOUNT_POP_WIDTH)
                .child(rows.body(&focus).child(person_menu(&title.github, &theme))),
        )
        .children(controls)
    }
}
