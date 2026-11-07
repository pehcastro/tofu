use std::f32::consts::FRAC_PI_2;
use std::ops::Range;
use std::rc::Rc;
use std::sync::Arc;
use std::time::SystemTime;

use desk_core::sessions::age;
use gpui::{
    AnyElement, App, ClickEvent, Corners, Div, ElementId, FontWeight, ScrollHandle, SharedString,
    Stateful, Transformation, Window, div, prelude::*, px, radians,
};

use crate::component::{control, icon};
use crate::components::avatar::spinner;
use crate::components::glyph::Glyph;
use crate::components::list::{HoverList, bare_row};
use crate::components::overlay::{Align, MenuIcon, MenuItem, Placement, Side, context_menu};
use crate::components::paint::{glyph, ink, tint};
use crate::components::size::{
    FONT_CAP, FONT_CAP2, FONT_CHAT, FONT_SMALL, GROUP_PAD_BOTTOM, HOVER, LINE_CAP, LINE_WHO,
    RADIUS_BADGE, RADIUS_LIST, RADIUS_ROW, ROW_GAP, ROW_PAD_X, ROW_PAD_Y, SPINNER, T1, T2, T3,
};
use crate::components::title_bar::named_stop;
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
const CARD_HEAD: f32 = 2.0 * PICKER_PAD_Y + PICKER_NAME_LINE + LINE_CAP;
const CARD_GAP: f32 = 8.0;
const CAPTION_PAD_TOP: f32 = 16.0;
const NEW_SESSION: f32 = 16.0;
const SESSION_GAP: f32 = 9.0;
const SESSION_DOT: f32 = 4.0;
const WATCHING_DOT: f32 = 5.0;
const WATCHING_RING: f32 = WATCHING_DOT + 2.0;
const WATCHING_HALO: f32 = 0.25;
const WATCHING_GAP: f32 = 6.0;
const INACTIVE_GAP: f32 = 6.0;
const INACTIVE_INDENT: f32 = 8.0;
const SESSION_ROW: f32 = LINE_WHO + 2.0 * ROW_PAD_Y;
const ABOVE_INACTIVE: f32 =
    CAPTION_PAD_TOP + LINE_CAP + GROUP_PAD_BOTTOM + INACTIVE_GAP + SESSION_ROW;
const OVERSCAN_ROWS: usize = 2;
const NEW_SESSION_NAME: &str = "New session";
const UUID_LENGTH: usize = 36;
const UUID_DASHES: [usize; 4] = [8, 13, 18, 23];
pub const TOOLS_SHOWN: usize = 5;
const TOOL_MOVES: [(&str, ToolMove); 3] = [
    ("Move up", ToolMove::Up),
    ("Move down", ToolMove::Down),
    ("Remove", ToolMove::Remove),
];

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
    Unreported,
}

impl SessionState {
    pub const ALL: [SessionState; 6] = [
        SessionState::Running,
        SessionState::Idle,
        SessionState::Waiting,
        SessionState::Stopped,
        SessionState::Failed,
        SessionState::Unreported,
    ];

    pub fn word(self) -> &'static str {
        match self {
            SessionState::Running => "running",
            SessionState::Idle => "idle",
            SessionState::Waiting => "waiting for you",
            SessionState::Stopped => "stopped",
            SessionState::Failed => "failed",
            SessionState::Unreported => "not reported by tofu",
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
            "stopped" | "fork_stop" => SessionState::Stopped,
            _ => SessionState::Stopped,
        }
    }
}

#[derive(Clone, Default)]
pub struct SessionColumns {
    pub id: Vec<SharedString>,
    pub name: Vec<SharedString>,
    pub last_at: Vec<Option<SystemTime>>,
    pub state: Vec<SessionState>,
    pub listed: usize,
    pub active: Vec<u32>,
    pub inactive: Vec<u32>,
}

impl SessionColumns {
    pub fn push(
        &mut self,
        id: SharedString,
        name: SharedString,
        last_at: Option<SystemTime>,
        state: SessionState,
    ) -> u32 {
        self.id.push(id);
        self.name.push(name);
        self.last_at.push(last_at);
        self.state.push(state);
        u32::try_from(self.id.len() - 1).unwrap_or(u32::MAX)
    }

    pub fn find(&self, id: &str) -> Option<u32> {
        let at = self.id.iter().position(|known| known == id)?;
        u32::try_from(at).ok()
    }

    pub fn column(&self, at: SessionAt) -> Option<usize> {
        let column = match at {
            SessionAt::Active(ix) => self.active.get(ix),
            SessionAt::Inactive(ix) => self.inactive.get(ix),
        }?;
        usize::try_from(*column).ok()
    }

    fn drawn(&self, at: SessionAt, now: SystemTime) -> Option<Drawn> {
        let column = self.column(at)?;
        Some(Drawn {
            name: self.name.get(column)?.clone(),
            state: *self.state.get(column)?,
            age: self
                .last_at
                .get(column)?
                .and_then(|last| now.duration_since(last).ok())
                .map(age),
        })
    }

    pub fn at(&self, column: u32) -> Option<SessionAt> {
        let find = |list: &[u32]| list.iter().position(|known| *known == column);
        find(&self.active)
            .map(SessionAt::Active)
            .or_else(|| find(&self.inactive).map(SessionAt::Inactive))
    }
}

struct Drawn {
    name: SharedString,
    state: SessionState,
    age: Option<String>,
}

#[derive(Clone)]
pub struct Project {
    pub name: SharedString,
    pub branch: SharedString,
    pub changed: usize,
    pub folded: bool,
    pub shown: Option<SessionAt>,
    pub sessions: Rc<SessionColumns>,
}

#[derive(Clone)]
pub struct Tool {
    pub label: SharedString,
    pub glyph: Glyph,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ToolMove {
    Up,
    Down,
    Remove,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SidebarPick {
    Tool(usize),
    MoveTool(usize, ToolMove),
    Fold(usize),
    NewSession(usize),
    Open(usize, SessionAt),
    Rename(usize, SessionAt),
    CopyId(usize, SessionAt),
    Settings,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SessionAt {
    Active(usize),
    Inactive(usize),
}

impl SessionAt {
    fn list(self) -> (&'static str, usize) {
        match self {
            SessionAt::Active(ix) => ("active", ix),
            SessionAt::Inactive(ix) => ("inactive", ix),
        }
    }
}

#[derive(Clone, Copy)]
struct Place {
    project: usize,
    at: SessionAt,
    live: bool,
}

impl Place {
    fn key(self, part: &'static str) -> ElementId {
        let (list, ix) = self.at.list();
        let packed = (self.project as u64) << 32 | ix as u64;
        ElementId::NamedChild(Arc::new((list, packed).into()), part.into())
    }

    fn name(self) -> SharedString {
        let (list, ix) = self.at.list();
        format!("session-{}-{list}-{ix}", self.project).into()
    }
}

type MenuRow = (&'static str, MenuIcon, fn(usize, SessionAt) -> SidebarPick);

const OPEN_ROW: MenuRow = ("Open", MenuIcon::Icon(Icon::Arrow), SidebarPick::Open);
const RENAME_ROW: MenuRow = (
    "Rename",
    MenuIcon::Glyph(Glyph::Pencil),
    SidebarPick::Rename,
);
const COPY_ROW: MenuRow = (
    "Copy session id",
    MenuIcon::Glyph(Glyph::File),
    SidebarPick::CopyId,
);
const LIVE_MENU: [MenuRow; 3] = [OPEN_ROW, RENAME_ROW, COPY_ROW];
const OTHER_MENU: [MenuRow; 2] = [OPEN_ROW, COPY_ROW];

type OnPick = Rc<dyn Fn(&SidebarPick, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct Sidebar {
    id: ElementId,
    projects: Vec<Project>,
    selected: Option<usize>,
    tools: Vec<Tool>,
    more: Option<AnyElement>,
    adding: Option<AnyElement>,
    editing: Option<(usize, SessionAt, AnyElement)>,
    on_pick: OnPick,
}

impl Sidebar {
    pub fn new(
        id: impl Into<ElementId>,
        projects: Vec<Project>,
        selected: Option<usize>,
        on_pick: impl Fn(&SidebarPick, &mut Window, &mut App) + 'static,
    ) -> Self {
        Self {
            id: id.into(),
            projects,
            selected,
            tools: Vec::new(),
            more: None,
            adding: None,
            editing: None,
            on_pick: Rc::new(on_pick),
        }
    }

    pub fn tools(mut self, tools: Vec<Tool>, more: impl IntoElement) -> Self {
        self.tools = tools;
        self.more = Some(more.into_any_element());
        self
    }

    pub fn adding(mut self, control: impl IntoElement) -> Self {
        self.adding = Some(control.into_any_element());
        self
    }

    pub fn editing(mut self, project: usize, at: SessionAt, field: AnyElement) -> Self {
        self.editing = Some((project, at, field));
        self
    }

    fn tool_moves(&self, at: usize) -> Vec<MenuItem> {
        let count = self.tools.len();
        TOOL_MOVES
            .iter()
            .map(|&(label, step)| {
                MenuItem::action(label).enabled(match step {
                    ToolMove::Up => at > 0,
                    ToolMove::Down => at + 1 < count,
                    ToolMove::Remove => true,
                })
            })
            .collect()
    }

    fn tool_row(&self, at: usize, tool: &Tool, theme: &Theme) -> Stateful<Div> {
        let line = row(
            (
                ElementId::named_usize("tool", at),
                SharedString::from(format!("tool-{at}")),
            ),
            false,
            T2,
            theme,
        )
        .child(tool_mark(tool.glyph, theme))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .child(tool.label.clone()),
        )
        .on_click(self.picked(SidebarPick::Tool(at)));
        let on_pick = self.on_pick.clone();
        div()
            .id(ElementId::named_usize("tool-item", at))
            .w_full()
            .child(
                context_menu(self.tool_moves(at))
                    .id(ElementId::named_usize("tool-menu", at))
                    .on_pick(move |ix, window, cx| match TOOL_MOVES.get(*ix) {
                        Some(&(_, step)) => on_pick(&SidebarPick::MoveTool(at, step), window, cx),
                        None => eprintln!("desk_ui: tool menu row {ix} is not in the menu"),
                    })
                    .child(line),
            )
    }

    fn more_item(&self, more: AnyElement) -> Stateful<Div> {
        let step = TOOL_MOVES.len() + 1;
        let items = self
            .tools
            .iter()
            .enumerate()
            .skip(TOOLS_SHOWN)
            .map(|(at, tool)| MenuItem::Submenu {
                label: tool.label.clone(),
                icon: Some(tool.glyph.into()),
                items: self.tool_moves(at),
            })
            .collect();
        let on_pick = self.on_pick.clone();
        div().id("tools-more-item").w_full().child(
            context_menu(items)
                .id("tools-more-menu")
                .on_pick(move |ix, window, cx| {
                    let at = TOOLS_SHOWN + ix / step;
                    match (ix % step)
                        .checked_sub(1)
                        .and_then(|row| TOOL_MOVES.get(row))
                    {
                        Some(&(_, moved)) => on_pick(&SidebarPick::MoveTool(at, moved), window, cx),
                        None => eprintln!("desk_ui: more menu row {ix} is not a move"),
                    }
                })
                .child(more),
        )
    }

    fn tools_block(&mut self, theme: &Theme) -> Option<HoverList> {
        if self.tools.is_empty() {
            return None;
        }
        let rows: Vec<Stateful<Div>> = self
            .tools
            .iter()
            .take(TOOLS_SHOWN)
            .enumerate()
            .map(|(at, tool)| self.tool_row(at, tool, theme))
            .collect();
        let more = self
            .more
            .take()
            .filter(|_| self.tools.len() > TOOLS_SHOWN)
            .map(|more| self.more_item(more));
        let list = HoverList::within(
            "tools",
            div().flex().flex_col().flex_none().pt(px(SIDEBAR_PAD_TOP)),
            ink(theme, HOVER),
            Corners::all(px(RADIUS_ROW)),
            theme,
        )
        .items(rows);
        Some(match more {
            Some(more) => list.item(more),
            None => list,
        })
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
        place: Place,
        drawn: Drawn,
        selected: bool,
        theme: &Theme,
        window: &mut Window,
        cx: &mut App,
    ) -> Stateful<Div> {
        let Drawn { name, state, age } = drawn;
        let tip: SharedString = match &age {
            None => state.word().into(),
            Some(age) => format!("{} {age}", state.word()).into(),
        };
        let mark = tooltip(
            place.key("tip"),
            div()
                .id(place.key("state"))
                .flex_none()
                .size(px(SPINNER))
                .flex()
                .items_center()
                .justify_center()
                .child(state_mark(
                    place.key("spin"),
                    state,
                    cx.reduce_motion(),
                    theme,
                )),
            Edge::Frame,
            tip,
            theme,
            window,
            cx,
        );
        let alpha = match place.at {
            SessionAt::Active(_) => T2,
            SessionAt::Inactive(_) => T3,
        };
        let accent = theme.color(ColorToken::StatusAccent);
        let watching = selected.then(|| {
            tooltip(
                place.key("watching-tip"),
                div()
                    .id(place.key("watching"))
                    .flex_none()
                    .size(px(WATCHING_RING))
                    .rounded_full()
                    .border_1()
                    .border_color(tint(accent, WATCHING_HALO))
                    .child(div().size(px(WATCHING_DOT)).rounded_full().bg(accent)),
                Edge::Frame,
                "watching",
                theme,
                window,
                cx,
            )
        });
        let editing = self
            .editing
            .take_if(|(project, at, _)| *project == place.project && *at == place.at);
        let session_row = row((place.key("row"), place.name()), false, alpha, theme)
            .when(matches!(place.at, SessionAt::Inactive(_)), |row| {
                row.child(div().flex_none().w(px(INACTIVE_INDENT)))
            })
            .child(mark)
            .child(match editing {
                Some((_, _, field)) => div().flex_1().min_w_0().overflow_hidden().child(field),
                None => div()
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .items_center()
                    .gap(px(WATCHING_GAP))
                    .children(watching)
                    .child(
                        div()
                            .min_w_0()
                            .truncate()
                            .when(selected, |name| {
                                name.text_color(theme.color(ColorToken::TextStrong))
                                    .font_weight(FontWeight::MEDIUM)
                            })
                            .child(match unnamed(&name) {
                                true => div().text_color(ink(theme, T3)).child(NEW_SESSION_NAME),
                                false => div().child(name),
                            }),
                    ),
            })
            .when_some(age, |row, age| {
                row.child(
                    div()
                        .flex_none()
                        .text_size(px(FONT_CAP2))
                        .text_color(ink(theme, T3))
                        .child(age),
                )
            })
            .on_click(self.picked(SidebarPick::Open(place.project, place.at)));
        let on_pick = self.on_pick.clone();
        let menu: &'static [MenuRow] = match place.live {
            true => &LIVE_MENU,
            false => &OTHER_MENU,
        };
        let items = menu
            .iter()
            .map(|&(label, glyph, _)| MenuItem::action(label).icon(glyph))
            .collect();
        div().id(place.key("item")).w_full().child(
            context_menu(items)
                .id(place.key("menu"))
                .on_pick(move |ix, window, cx| match menu.get(*ix) {
                    Some(&(_, _, pick)) => on_pick(&pick(place.project, place.at), window, cx),
                    None => eprintln!("desk_ui: sidebar menu row {ix} is not in the menu"),
                })
                .child(session_row),
        )
    }
}

fn state_mark(spin: ElementId, state: SessionState, reduced: bool, theme: &Theme) -> AnyElement {
    match state {
        SessionState::Running if !reduced => spinner(spin, theme).into_any_element(),
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
        SessionState::Unreported => div().into_any_element(),
    }
}

fn tool_mark(mark: Glyph, theme: &Theme) -> Div {
    div()
        .flex_none()
        .size(px(SPINNER))
        .flex()
        .items_center()
        .justify_center()
        .child(glyph(mark, SPINNER, ink(theme, T2)))
}

pub fn more_row(theme: &Theme) -> Stateful<Div> {
    row(("tools-more", "tools-more"), false, T2, theme)
        .child(tool_mark(Glyph::Ellipsis, theme))
        .child(div().flex_1().min_w_0().truncate().child("More"))
        .child(chevron(false, theme))
}

pub fn more_placement() -> Placement {
    Placement {
        side: Side::Right,
        align: Align::Start,
        offset: SIDEBAR_PAD_X,
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
    (id, stop): (ElementId, SharedString),
    mark: AnyElement,
    name: SharedString,
    detail: SharedString,
    open: bool,
    theme: &Theme,
) -> Stateful<Div> {
    named_stop(control(id, "Project", theme), stop, theme)
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
        .child(chevron(open, theme))
}

fn chevron(open: bool, theme: &Theme) -> impl IntoElement {
    let turn = if open { 0.0 } else { -FRAC_PI_2 };
    glyph(Glyph::Chevron, ICON_SMALL, ink(theme, T3))
        .with_transformation(Transformation::rotate(radians(turn)))
}

fn row(
    (id, name): (impl Into<ElementId>, impl Into<SharedString>),
    selected: bool,
    alpha: f32,
    theme: &Theme,
) -> Stateful<Div> {
    named_stop(bare_row(id, selected, false, theme), name, theme)
        .gap(px(SESSION_GAP))
        .line_height(px(LINE_WHO))
        .text_color(ink(theme, alpha))
}

fn caption(text: &'static str, theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .px(px(ROW_PAD_X))
        .pb(px(GROUP_PAD_BOTTOM))
        .line_height(px(LINE_CAP))
        .text_size(px(FONT_CAP))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, T3))
        .child(div().flex_1().child(text))
}

fn add_button(
    (id, stop): (ElementId, SharedString),
    label: &'static str,
    theme: &Theme,
) -> Stateful<Div> {
    named_stop(control(id, label, theme), stop, theme)
        .size(px(NEW_SESSION))
        .rounded(px(RADIUS_BADGE))
        .child(icon(Icon::Plus, ICON_SMALL, ink(theme, T3)))
}

pub fn open_project_button(theme: &Theme) -> Stateful<Div> {
    add_button(
        ("open-project".into(), "open-project".into()),
        "Open a project",
        theme,
    )
}

fn shown_rows(rows: usize, first: f32, scroll: &ScrollHandle, window: &Window) -> Range<usize> {
    let top = f32::from(-scroll.offset().y);
    let bottom = top + f32::from(window.viewport_size().height);
    let row_at = |y: f32| ((y - first) / SESSION_ROW).max(0.0) as usize;
    let start = row_at(top).saturating_sub(OVERSCAN_ROWS).min(rows);
    let end = row_at(bottom)
        .saturating_add(OVERSCAN_ROWS + 1)
        .clamp(start, rows);
    start..end
}

fn card_name(part: &'static str, project: usize) -> SharedString {
    match project {
        0 => part.into(),
        _ => format!("{part}-{project}").into(),
    }
}

fn gap(rows: usize) -> Div {
    div().flex_none().h(px(rows as f32 * SESSION_ROW))
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
        let scroll = window
            .use_keyed_state(
                ElementId::NamedChild(Arc::new(self.id.clone()), "scroll".into()),
                cx,
                |_, _| ScrollHandle::new(),
            )
            .read(cx)
            .clone();
        let mut rows = HoverList::within(
            "rows",
            div().flex().flex_col().flex_grow(1.0).flex_shrink_0(),
            ink(&theme, HOVER),
            Corners::all(px(RADIUS_ROW)),
            &theme,
        );
        let settings = row(("settings", "settings"), false, T2, &theme)
            .child(glyph(Glyph::Settings, ICON_SMALL, ink(&theme, T2)))
            .child("Settings")
            .on_click(self.picked(SidebarPick::Settings));
        let tools = self.tools_block(&theme);
        let heading = caption("Projects", &theme)
            .pt(px(match tools {
                Some(_) => CAPTION_PAD_TOP,
                None => SIDEBAR_PAD_TOP,
            }))
            .children(self.adding.take());
        let now = SystemTime::now();
        let mut top = 0.0;
        for (project_at, project) in std::mem::take(&mut self.projects).into_iter().enumerate() {
            let letter: SharedString = project
                .name
                .chars()
                .next()
                .map(String::from)
                .unwrap_or_default()
                .into();
            let above = match project_at {
                0 => 0.0,
                _ => CARD_GAP,
            };
            let head = picker(
                (
                    ElementId::named_usize("project", project_at),
                    card_name("project", project_at),
                ),
                letter.into_any_element(),
                project.name.clone(),
                match project.branch.is_empty() {
                    true => format!("{} changed", project.changed),
                    false => format!("{} · {} changed", project.branch, project.changed),
                }
                .into(),
                !project.folded,
                &theme,
            )
            .mt(px(above))
            .on_click(self.picked(SidebarPick::Fold(project_at)));
            rows = rows.inert(head);
            top += above + CARD_HEAD;
            if project.folded {
                continue;
            }
            let inactive_name = card_name("inactive", project_at);
            let open = window.use_keyed_state(
                ElementId::NamedChild(Arc::new(self.id.clone()), inactive_name.clone()),
                cx,
                |_, _| false,
            );
            let inactive_open = *open.read(cx);
            let new_session = add_button(
                (
                    ElementId::named_usize("new-session", project_at),
                    card_name("new-session", project_at),
                ),
                "New session",
                &theme,
            )
            .on_click(self.picked(SidebarPick::NewSession(project_at)));
            let active_caption = caption("Active", &theme)
                .pt(px(CAPTION_PAD_TOP))
                .child(new_session);
            let sessions = &project.sessions;
            let total = match inactive_open {
                true => sessions.inactive.len(),
                false => 0,
            };
            let first = top + sessions.active.len() as f32 * SESSION_ROW + ABOVE_INACTIVE;
            let drawn = shown_rows(total, first, &scroll, window);
            top = first + total as f32 * SESSION_ROW;
            let live = self.selected == Some(project_at);
            let mut drawn_rows = |range: Range<usize>, at: fn(usize) -> SessionAt| {
                range
                    .filter_map(|ix| {
                        let at = at(ix);
                        let drawn = sessions.drawn(at, now)?;
                        let shown = project.shown == Some(at);
                        let place = Place {
                            project: project_at,
                            at,
                            live,
                        };
                        Some(self.session(place, drawn, shown, &theme, window, cx))
                    })
                    .collect::<Vec<Stateful<Div>>>()
            };
            let active = drawn_rows(0..sessions.active.len(), SessionAt::Active);
            let inactive = drawn_rows(drawn.clone(), SessionAt::Inactive);
            let toggle = open.clone();
            let header = row(
                (
                    ElementId::named_usize("inactive", project_at),
                    inactive_name,
                ),
                false,
                T3,
                &theme,
            )
            .mt(px(INACTIVE_GAP))
            .child(chevron(inactive_open, &theme))
            .child(div().flex_1().child("Inactive"))
            .child(word(sessions.inactive.len().to_string().into(), &theme))
            .on_click(move |_, _, cx| {
                toggle.update(cx, |open, cx| {
                    *open = !*open;
                    cx.notify();
                })
            });
            rows = rows
                .inert(active_caption)
                .items(active)
                .item(header)
                .inert(gap(drawn.start))
                .items(inactive)
                .inert(gap(total.saturating_sub(drawn.end)));
        }
        div()
            .id(self.id.clone())
            .w(px(SIDEBAR_COLUMN))
            .h_full()
            .flex_none()
            .flex()
            .flex_col()
            .px(px(SIDEBAR_PAD_X))
            .pb(px(SIDEBAR_PAD_BOTTOM))
            .children(tools)
            .child(heading)
            .child(
                div()
                    .id("sessions")
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .flex_col()
                    .overflow_y_scroll()
                    .track_scroll(&scroll)
                    .child(rows.inert(div().flex_1()).item(settings)),
            )
    }
}
