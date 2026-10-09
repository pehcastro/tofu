use std::f32::consts::FRAC_PI_2;
use std::rc::Rc;
use std::sync::Arc;

use gpui::{
    AnyElement, App, ClickEvent, Corners, Div, ElementId, FontWeight, SharedString, Stateful,
    Transformation, Window, div, prelude::*, px, radians,
};

use crate::component::{control, icon};
use crate::components::avatar::spinner;
use crate::components::glyph::Glyph;
use crate::components::list::{HoverList, bare_row};
use crate::components::overlay::{MenuIcon, MenuItem, context_menu};
use crate::components::paint::{glyph, ink};
use crate::components::size::{
    FONT_CAP, FONT_CAP2, FONT_CHAT, FONT_SMALL, GROUP_PAD_BOTTOM, HOVER, LINE_CAP, LINE_WHO,
    RADIUS_BADGE, RADIUS_LIST, RADIUS_ROW, ROW_GAP, ROW_PAD_X, SPINNER, T1, T2, T3,
};
use crate::components::tooltip::{Edge, tooltip};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{ICON_SMALL, SIDEBAR_WIDTH};
use crate::theme::{ColorToken, Theme};

const SIDEBAR_PAD_X: f32 = 8.0;
const SIDEBAR_PAD_TOP: f32 = 2.0;
pub const SIDEBAR_COLUMN: f32 = SIDEBAR_WIDTH + 2.0 * SIDEBAR_PAD_X;
const SIDEBAR_PAD_BOTTOM: f32 = 10.0;
const PICKER_PAD_Y: f32 = 8.0;
const PICKER_MARK: f32 = 26.0;
const PICKER_MARK_FILL: f32 = 0.1;
const PICKER_NAME_LINE: f32 = 17.0;
const CAPTION_PAD_TOP: f32 = 16.0;
const NEW_SESSION: f32 = 16.0;
const SESSION_GAP: f32 = 9.0;
const SESSION_DOT: f32 = 4.0;
const INACTIVE_GAP: f32 = 6.0;
const INACTIVE_INDENT: f32 = 8.0;
const NEW_SESSION_NAME: &str = "New session";
const UUID_LENGTH: usize = 36;
const UUID_DASHES: [usize; 4] = [8, 13, 18, 23];

pub fn unnamed(name: &str) -> bool {
    name.is_empty()
        || (name.len() == UUID_LENGTH
            && name
                .char_indices()
                .all(|(at, c)| match UUID_DASHES.contains(&at) {
                    true => c == '-',
                    false => c.is_ascii_hexdigit(),
                }))
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SessionState {
    Running,
    Idle,
    Waiting,
    Stopped,
    Failed,
}

impl SessionState {
    pub const ALL: [SessionState; 5] = [
        SessionState::Running,
        SessionState::Idle,
        SessionState::Waiting,
        SessionState::Stopped,
        SessionState::Failed,
    ];

    pub fn word(self) -> &'static str {
        match self {
            SessionState::Running => "running",
            SessionState::Idle => "idle",
            SessionState::Waiting => "waiting for approval",
            SessionState::Stopped => "stopped",
            SessionState::Failed => "failed",
        }
    }
}

impl From<&str> for SessionState {
    fn from(word: &str) -> Self {
        match word.to_lowercase().as_str() {
            "running" => SessionState::Running,
            "idle" => SessionState::Idle,
            "waiting" | "waiting for approval" => SessionState::Waiting,
            "failed" | "error" | "loop_guard" => SessionState::Failed,
            _ => SessionState::Stopped,
        }
    }
}

#[derive(Clone)]
pub struct Session {
    pub name: SharedString,
    pub state: SessionState,
    pub age: SharedString,
}

#[derive(Clone)]
pub struct Project {
    pub name: SharedString,
    pub branch: SharedString,
    pub changed: usize,
    pub active: Vec<Session>,
    pub shown: Option<SessionAt>,
    pub inactive: Vec<Session>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SidebarPick {
    Project,
    NewSession,
    Open(SessionAt),
    Rename(SessionAt),
    CopyId(SessionAt),
    Docs,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SessionAt {
    Active(usize),
    Inactive(usize),
}

impl SessionAt {
    fn key(self, part: &'static str) -> ElementId {
        let (list, ix) = match self {
            SessionAt::Active(ix) => ("active", ix),
            SessionAt::Inactive(ix) => ("inactive", ix),
        };
        ElementId::NamedChild(Arc::new((list, ix).into()), part.into())
    }
}

type MenuRow = (&'static str, MenuIcon, fn(SessionAt) -> SidebarPick);

const MENU: [MenuRow; 3] = [
    ("Open", MenuIcon::Icon(Icon::Arrow), SidebarPick::Open),
    (
        "Rename",
        MenuIcon::Glyph(Glyph::Pencil),
        SidebarPick::Rename,
    ),
    (
        "Copy session id",
        MenuIcon::Glyph(Glyph::File),
        SidebarPick::CopyId,
    ),
];

type OnPick = Rc<dyn Fn(&SidebarPick, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct Sidebar {
    id: ElementId,
    project: Option<Project>,
    editing: Option<(SessionAt, AnyElement)>,
    on_pick: OnPick,
}

impl Sidebar {
    pub fn new(
        id: impl Into<ElementId>,
        project: Option<Project>,
        on_pick: impl Fn(&SidebarPick, &mut Window, &mut App) + 'static,
    ) -> Self {
        Self {
            id: id.into(),
            project,
            editing: None,
            on_pick: Rc::new(on_pick),
        }
    }

    pub fn editing(mut self, at: SessionAt, field: AnyElement) -> Self {
        self.editing = Some((at, field));
        self
    }

    fn picked(
        &self,
        pick: SidebarPick,
    ) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static + use<> {
        let on_pick = self.on_pick.clone();
        move |_, window, cx| on_pick(&pick, window, cx)
    }

    fn session(
        &mut self,
        at: SessionAt,
        session: &Session,
        selected: bool,
        theme: &Theme,
        window: &mut Window,
        cx: &mut App,
    ) -> Stateful<Div> {
        let tip: SharedString = match session.age.is_empty() {
            true => session.state.word().into(),
            false => format!("{} {}", session.state.word(), session.age).into(),
        };
        let mark = tooltip(
            at.key("tip"),
            div()
                .id(at.key("state"))
                .flex_none()
                .size(px(SPINNER))
                .flex()
                .items_center()
                .justify_center()
                .child(state_mark(at, session.state, cx.reduce_motion(), theme)),
            Edge::Frame,
            tip,
            theme,
            window,
            cx,
        );
        let alpha = match (at, selected) {
            (_, true) => T1,
            (SessionAt::Active(_), false) => T2,
            (SessionAt::Inactive(_), false) => T3,
        };
        let session_row = row(at.key("row"), selected, alpha, theme)
            .when(matches!(at, SessionAt::Inactive(_)), |row| {
                row.child(div().flex_none().w(px(INACTIVE_INDENT)))
            })
            .child(mark)
            .child(match self.editing.take_if(|(editing, _)| *editing == at) {
                Some((_, field)) => div().flex_1().min_w_0().overflow_hidden().child(field),
                None => div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .child(match unnamed(&session.name) {
                        true => div().text_color(ink(theme, T3)).child(NEW_SESSION_NAME),
                        false => div().child(session.name.clone()),
                    }),
            })
            .when(!session.age.is_empty(), |row| {
                row.child(
                    div()
                        .flex_none()
                        .text_size(px(FONT_CAP2))
                        .text_color(ink(theme, T3))
                        .child(session.age.clone()),
                )
            })
            .on_click(self.picked(SidebarPick::Open(at)));
        let on_pick = self.on_pick.clone();
        let items = MENU
            .iter()
            .map(|&(label, glyph, _)| MenuItem::action(label).icon(glyph))
            .collect();
        div().id(at.key("item")).w_full().child(
            context_menu(items)
                .id(at.key("menu"))
                .on_pick(move |ix, window, cx| match MENU.get(*ix) {
                    Some(&(_, _, pick)) => on_pick(&pick(at), window, cx),
                    None => eprintln!("desk_ui: sidebar menu row {ix} is not in the menu"),
                })
                .child(session_row),
        )
    }
}

fn state_mark(at: SessionAt, state: SessionState, reduced: bool, theme: &Theme) -> AnyElement {
    match state {
        SessionState::Running if !reduced => spinner(at.key("spin"), theme).into_any_element(),
        SessionState::Running | SessionState::Idle => div()
            .size(px(SESSION_DOT))
            .rounded_full()
            .bg(theme.color(ColorToken::StatusLive))
            .into_any_element(),
        SessionState::Waiting => {
            glyph(Glyph::Lock, SPINNER, theme.color(ColorToken::StatusWarn)).into_any_element()
        }
        SessionState::Stopped => icon(Icon::Maximize, SPINNER, ink(theme, T3)).into_any_element(),
        SessionState::Failed => {
            icon(Icon::Close, SPINNER, theme.color(ColorToken::StatusDanger)).into_any_element()
        }
    }
}

fn line(height: f32, size: f32) -> Div {
    div()
        .min_w_0()
        .truncate()
        .line_height(px(height))
        .text_size(px(size))
}

fn picker(
    id: &'static str,
    label: &'static str,
    mark: AnyElement,
    name: SharedString,
    detail: SharedString,
    theme: &Theme,
) -> Stateful<Div> {
    control(id, label, theme)
        .w_full()
        .justify_start()
        .gap(px(ROW_GAP))
        .px(px(ROW_PAD_X))
        .py(px(PICKER_PAD_Y))
        .rounded(px(RADIUS_LIST))
        .child(
            div()
                .flex_none()
                .size(px(PICKER_MARK))
                .flex()
                .items_center()
                .justify_center()
                .rounded(px(RADIUS_ROW))
                .bg(ink(theme, PICKER_MARK_FILL))
                .text_size(px(FONT_SMALL))
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(ink(theme, T1))
                .child(mark),
        )
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .flex_col()
                .child(
                    line(PICKER_NAME_LINE, FONT_CHAT)
                        .font_weight(FontWeight::SEMIBOLD)
                        .text_color(ink(theme, T1))
                        .child(name),
                )
                .child(
                    line(LINE_CAP, FONT_SMALL)
                        .text_color(ink(theme, T3))
                        .child(detail),
                ),
        )
        .child(chevron(true, theme))
}

fn chevron(open: bool, theme: &Theme) -> impl IntoElement {
    let turn = if open { 0.0 } else { -FRAC_PI_2 };
    glyph(Glyph::Chevron, ICON_SMALL, ink(theme, T3))
        .with_transformation(Transformation::rotate(radians(turn)))
}

fn row(id: impl Into<ElementId>, selected: bool, alpha: f32, theme: &Theme) -> Stateful<Div> {
    bare_row(id, selected, false, theme)
        .gap(px(SESSION_GAP))
        .line_height(px(LINE_WHO))
        .text_color(ink(theme, alpha))
}

fn word(text: SharedString, theme: &Theme) -> Div {
    div()
        .flex_none()
        .text_size(px(FONT_CAP))
        .text_color(ink(theme, T3))
        .child(text)
}

impl RenderOnce for Sidebar {
    fn render(mut self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let open = window.use_keyed_state(
            ElementId::NamedChild(Arc::new(self.id.clone()), "inactive".into()),
            cx,
            |_, _| false,
        );
        let inactive_open = *open.read(cx);
        let rows = HoverList::within(
            "rows",
            div().flex().flex_col().flex_grow(1.0).flex_shrink_0(),
            ink(&theme, HOVER),
            Corners::all(px(RADIUS_ROW)),
            &theme,
        );
        let docs = row("docs", false, T2, &theme)
            .child("Docs")
            .on_click(self.picked(SidebarPick::Docs));
        let project = self.project.take();
        let (head, rows) = match &project {
            None => (
                picker(
                    "open-project",
                    "Open a project",
                    icon(Icon::Plus, ICON_SMALL, ink(&theme, T2)).into_any_element(),
                    "Open a project".into(),
                    "no project open".into(),
                    &theme,
                ),
                rows,
            ),
            Some(project) => {
                let letter: SharedString = project
                    .name
                    .chars()
                    .next()
                    .map(String::from)
                    .unwrap_or_default()
                    .into();
                let head = picker(
                    "project",
                    "Projects",
                    letter.into_any_element(),
                    project.name.clone(),
                    format!("{} · {} changed", project.branch, project.changed).into(),
                    &theme,
                );
                let caption = div()
                    .flex()
                    .items_center()
                    .px(px(ROW_PAD_X))
                    .pt(px(CAPTION_PAD_TOP))
                    .pb(px(GROUP_PAD_BOTTOM))
                    .line_height(px(LINE_CAP))
                    .text_size(px(FONT_CAP))
                    .font_weight(FontWeight::MEDIUM)
                    .text_color(ink(&theme, T3))
                    .child(div().flex_1().child("Active"))
                    .child(
                        control("new-session", "New session", &theme)
                            .size(px(NEW_SESSION))
                            .rounded(px(RADIUS_BADGE))
                            .child(icon(Icon::Plus, ICON_SMALL, ink(&theme, T3)))
                            .on_click(self.picked(SidebarPick::NewSession)),
                    );
                let active: Vec<Stateful<Div>> = project
                    .active
                    .iter()
                    .enumerate()
                    .map(|(ix, session)| {
                        let at = SessionAt::Active(ix);
                        let shown = project.shown == Some(at);
                        self.session(at, session, shown, &theme, window, cx)
                    })
                    .collect();
                let toggle = open.clone();
                let header = row("inactive", false, T3, &theme)
                    .mt(px(INACTIVE_GAP))
                    .child(chevron(inactive_open, &theme))
                    .child(div().flex_1().child("Inactive"))
                    .child(word(project.inactive.len().to_string().into(), &theme))
                    .on_click(move |_, _, cx| {
                        toggle.update(cx, |open, cx| {
                            *open = !*open;
                            cx.notify();
                        })
                    });
                let inactive: Vec<Stateful<Div>> = project
                    .inactive
                    .iter()
                    .enumerate()
                    .filter(|_| inactive_open)
                    .map(|(ix, session)| {
                        let at = SessionAt::Inactive(ix);
                        let shown = project.shown == Some(at);
                        self.session(at, session, shown, &theme, window, cx)
                    })
                    .collect();
                (
                    head,
                    rows.inert(caption)
                        .items(active)
                        .item(header)
                        .items(inactive),
                )
            }
        };
        div()
            .id(self.id.clone())
            .w(px(SIDEBAR_COLUMN))
            .h_full()
            .flex_none()
            .flex()
            .flex_col()
            .px(px(SIDEBAR_PAD_X))
            .pt(px(SIDEBAR_PAD_TOP))
            .pb(px(SIDEBAR_PAD_BOTTOM))
            .child(head.on_click(self.picked(SidebarPick::Project)))
            .child(
                div()
                    .id("sessions")
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .flex_col()
                    .overflow_y_scroll()
                    .child(rows.inert(div().flex_1()).item(docs)),
            )
    }
}
