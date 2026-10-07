use std::f32::consts::FRAC_PI_2;
use std::rc::Rc;
use std::sync::Arc;

use gpui::{
    AnyElement, App, ClickEvent, Corners, Div, ElementId, FontWeight, SharedString, Stateful,
    Transformation, Window, div, prelude::*, px, radians,
};

use crate::component::{control, icon};
use crate::components::glyph::Glyph;
use crate::components::list::{HoverList, bare_row};
use crate::components::paint::{glyph, ink};
use crate::components::size::{
    FONT_CAP, FONT_CHAT, FONT_SMALL, GROUP_PAD_BOTTOM, HOVER, LINE_CAP, LINE_WHO, RADIUS_BADGE,
    RADIUS_LIST, RADIUS_ROW, ROW_GAP, ROW_PAD_X, T1, T2, T3,
};
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

#[derive(Clone)]
pub struct Session {
    pub name: SharedString,
    pub state: SharedString,
}

#[derive(Clone)]
pub struct Project {
    pub name: SharedString,
    pub branch: SharedString,
    pub changed: usize,
    pub running: Vec<Session>,
    pub shown: Option<usize>,
    pub inactive: Vec<Session>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SidebarPick {
    Project,
    NewSession,
    Running(usize),
    Inactive(usize),
    Docs,
}

type OnPick = Rc<dyn Fn(&SidebarPick, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct Sidebar {
    id: ElementId,
    project: Option<Project>,
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
            on_pick: Rc::new(on_pick),
        }
    }

    fn picked(
        &self,
        pick: SidebarPick,
    ) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static + use<> {
        let on_pick = self.on_pick.clone();
        move |_, window, cx| on_pick(&pick, window, cx)
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

fn session_row(row: Stateful<Div>, lead: Div, session: &Session, theme: &Theme) -> Stateful<Div> {
    row.child(lead.flex_none())
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .child(session.name.clone()),
        )
        .child(word(session.state.clone(), theme))
}

fn word(text: SharedString, theme: &Theme) -> Div {
    div()
        .flex_none()
        .text_size(px(FONT_CAP))
        .text_color(ink(theme, T3))
        .child(text)
}

impl RenderOnce for Sidebar {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let open = window.use_keyed_state(
            ElementId::NamedChild(Arc::new(self.id.clone()), "inactive".into()),
            cx,
            |_, _| false,
        );
        let inactive_open = *open.read(cx);
        let rows = HoverList::within(
            "rows",
            div().flex().flex_col().flex_1().min_h_0(),
            ink(&theme, HOVER),
            Corners::all(px(RADIUS_ROW)),
            &theme,
        );
        let docs = row("docs", false, T2, &theme)
            .child("Docs")
            .on_click(self.picked(SidebarPick::Docs));
        let (head, rows) = match &self.project {
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
                    .child(div().flex_1().child("Running"))
                    .child(
                        control("new-session", "New session", &theme)
                            .size(px(NEW_SESSION))
                            .rounded(px(RADIUS_BADGE))
                            .child(icon(Icon::Plus, ICON_SMALL, ink(&theme, T3)))
                            .on_click(self.picked(SidebarPick::NewSession)),
                    );
                let running = project.running.iter().enumerate().map(|(ix, session)| {
                    let shown = project.shown == Some(ix);
                    let dot = div()
                        .size(px(SESSION_DOT))
                        .rounded_full()
                        .bg(theme.color(ColorToken::StatusLive));
                    let alpha = if shown { T1 } else { T2 };
                    session_row(
                        row(("running", ix), shown, alpha, &theme),
                        dot,
                        session,
                        &theme,
                    )
                    .on_click(self.picked(SidebarPick::Running(ix)))
                });
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
                let inactive = project
                    .inactive
                    .iter()
                    .enumerate()
                    .filter(|_| inactive_open)
                    .map(|(ix, session)| {
                        session_row(
                            row(("inactive", ix), false, T3, &theme),
                            div().w(px(INACTIVE_INDENT)),
                            session,
                            &theme,
                        )
                        .on_click(self.picked(SidebarPick::Inactive(ix)))
                    });
                (
                    head,
                    rows.inert(caption)
                        .items(running)
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
            .child(rows.inert(div().flex_1()).item(docs))
    }
}
