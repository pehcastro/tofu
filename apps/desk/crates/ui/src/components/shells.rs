use std::rc::Rc;

use gpui::{
    AnyElement, App, Div, ElementId, FontWeight, MouseButton, Rgba, SharedString, Stateful, Window,
    div, prelude::*, px,
};

use crate::component::icon;
use crate::components::chip::{mono, tabular, trace};
use crate::components::glyph::Glyph;
use crate::components::overlay::Popover;
use crate::components::paint::{glyph, ink, pressed};
use crate::components::size::{
    BADGE_PAD_X, CAPTION_TEXT, CLOSE_BOX, DIM_TEXT, FONT_SMALL, FONT_TAB, HEADER_TABBED, HOVER,
    RADIUS_BADGE, RADIUS_ROW, RADIUS_TAB, ROW_PAD_X, ROW_PAD_Y, T1, T2, TAB_GAP, TAB_IN_HEADER,
    TAB_PAD_LEFT, TAB_PAD_TAIL,
};
use crate::components::term::{TermCard, TermStatus};
use crate::components::width::{Fit, Width};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{ICON_SMALL, ICON_TINY};
use crate::theme::{ColorToken, Theme};

const VISIBLE: usize = 3;
const SHELL_TAB: f32 = 108.0;
const SHELL_MORE: f32 = 80.0;
const ROW_PAD_LEFT: f32 = 12.0;
const ROW_PAD_RIGHT: f32 = 8.0;
const STATUS_DOT: f32 = 6.0;
const PANEL_FILL: f32 = 0.05;
const PANEL_PAD_X: f32 = 14.0;
const PANEL_PAD_TOP: f32 = 10.0;
const PANEL_PAD_BOTTOM: f32 = 4.0;
const LIST_WIDTH: f32 = 440.0;
const LIST_INSET: f32 = 16.0;
const META_GAP: f32 = 10.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ShellState {
    Running,
    Failed,
    Waiting,
}

impl ShellState {
    fn color(self, theme: &Theme) -> Rgba {
        theme.color(match self {
            ShellState::Running => ColorToken::GitAdded,
            ShellState::Failed => ColorToken::StatusDanger,
            ShellState::Waiting => ColorToken::StatusWarn,
        })
    }
}

#[derive(Clone)]
pub struct Shell {
    pub name: SharedString,
    pub state: ShellState,
    pub command: SharedString,
    pub pid: u32,
    pub by: SharedString,
    pub age: SharedString,
    pub port: Option<u16>,
    pub lines: Vec<SharedString>,
    pub status: TermStatus,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ShellEvent {
    Pick(usize),
    Close(usize),
    More,
    Dismiss,
    Open(usize),
    Kill(usize),
    Trace(usize),
}

type OnShell = Rc<dyn Fn(&ShellEvent, &mut Window, &mut App)>;

#[derive(IntoElement)]
pub struct Shells {
    id: SharedString,
    shells: Vec<Shell>,
    active: usize,
    listing: bool,
    on: OnShell,
}

impl Shells {
    pub fn new(
        id: impl Into<SharedString>,
        shells: &[Shell],
        active: usize,
        listing: bool,
        on: impl Fn(&ShellEvent, &mut Window, &mut App) + 'static,
    ) -> Self {
        Shells {
            id: id.into(),
            shells: shells.to_vec(),
            active,
            listing,
            on: Rc::new(on),
        }
    }
}

fn fitting(count: usize, width: Option<gpui::Pixels>) -> usize {
    let cap = count.min(VISIBLE);
    let Some(width) = width else {
        return cap;
    };
    let room = width.as_f32() - ROW_PAD_LEFT - ROW_PAD_RIGHT;
    (1..=cap)
        .rev()
        .find(|shown| {
            let more = if *shown < count { SHELL_MORE } else { 0.0 };
            *shown as f32 * SHELL_TAB + more <= room
        })
        .unwrap_or(1)
}

fn emit(
    on: &OnShell,
    event: ShellEvent,
) -> impl Fn(&gpui::ClickEvent, &mut Window, &mut App) + use<> {
    let on = on.clone();
    move |_, window, cx| {
        cx.stop_propagation();
        on(&event, window, cx)
    }
}

fn dot(state: ShellState, theme: &Theme) -> Div {
    div()
        .flex_none()
        .size(px(STATUS_DOT))
        .rounded_full()
        .bg(state.color(theme))
}

fn shell_tab(
    id: &SharedString,
    ix: usize,
    shell: &Shell,
    on: bool,
    theme: &Theme,
    emit_on: &OnShell,
) -> Stateful<Div> {
    let group = SharedString::from(format!("{id}-tab-{ix}"));
    let hover = ink(theme, HOVER);
    let x = div()
        .id(ElementId::Name(format!("{id}-close-{ix}").into()))
        .aria_label("Close shell")
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(CLOSE_BOX))
        .rounded_full()
        .opacity(0.0)
        .group_hover(group.clone(), |close| close.opacity(1.0))
        .hover(move |close| close.bg(hover))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_click(emit(emit_on, ShellEvent::Close(ix)))
        .child(icon(Icon::Close, ICON_TINY, ink(theme, CAPTION_TEXT)));
    let text = if on {
        ink(theme, T1)
    } else {
        ink(theme, DIM_TEXT)
    };
    pressed(
        div()
            .id(ElementId::Name(format!("{id}-shell-{ix}").into()))
            .group(group)
            .flex()
            .flex_none()
            .items_center()
            .gap(px(TAB_GAP))
            .w(px(SHELL_TAB))
            .h(px(TAB_IN_HEADER))
            .pl(px(TAB_PAD_LEFT))
            .pr(px(TAB_PAD_TAIL))
            .rounded_t(px(RADIUS_TAB))
            .cursor_pointer()
            .when(on, |tab| tab.bg(ink(theme, PANEL_FILL)))
            .when(!on, |tab| tab.hover(move |tab| tab.bg(hover)))
            .text_color(text)
            .on_click(emit(emit_on, ShellEvent::Pick(ix)))
            .child(dot(shell.state, theme))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .font_family(mono(theme))
                    .text_size(px(FONT_SMALL))
                    .child(shell.name.clone()),
            )
            .child(x),
        theme.color(ColorToken::CardsInnerFill),
    )
}

fn list_row(
    id: &SharedString,
    ix: usize,
    shell: &Shell,
    on: bool,
    theme: &Theme,
    emit_on: &OnShell,
) -> Stateful<Div> {
    let hover = ink(theme, HOVER);
    div()
        .id(ElementId::Name(format!("{id}-row-{ix}").into()))
        .flex()
        .items_center()
        .gap(px(TAB_GAP))
        .px(px(ROW_PAD_X))
        .py(px(ROW_PAD_Y))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .when(on, |row| row.bg(hover))
        .hover(move |row| row.bg(hover))
        .on_click(emit(emit_on, ShellEvent::Pick(ix)))
        .child(dot(shell.state, theme))
        .child(
            div()
                .flex_none()
                .font_family(mono(theme))
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, T1))
                .child(shell.name.clone()),
        )
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .font_family(mono(theme))
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, CAPTION_TEXT))
                .child(format!("$ {}", shell.command)),
        )
}

fn action(
    id: SharedString,
    label: &'static str,
    theme: &Theme,
    click: impl Fn(&gpui::ClickEvent, &mut Window, &mut App) + 'static,
) -> AnyElement {
    let hover = ink(theme, HOVER);
    pressed(
        div()
            .id(ElementId::Name(id))
            .flex_none()
            .px(px(BADGE_PAD_X))
            .rounded(px(RADIUS_BADGE))
            .cursor_pointer()
            .text_color(ink(theme, T2))
            .hover(move |button| button.bg(hover))
            .on_click(click)
            .child(label),
        theme.color(ColorToken::CardsInnerFill),
    )
    .into_any_element()
}

fn meta(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .flex_none()
        .font_features(tabular())
        .text_color(ink(theme, CAPTION_TEXT))
        .child(text.into())
}

fn panel(
    id: &SharedString,
    ix: usize,
    shell: &Shell,
    fit: Fit,
    theme: &Theme,
    on: &OnShell,
) -> Div {
    let wide = fit == Fit::Wide;
    let header = div()
        .flex()
        .items_center()
        .gap(px(META_GAP))
        .min_w_0()
        .px(px(PANEL_PAD_X))
        .pt(px(PANEL_PAD_TOP))
        .pb(px(PANEL_PAD_BOTTOM))
        .font_family(mono(theme))
        .text_size(px(FONT_SMALL))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .truncate()
                .text_color(ink(theme, T2))
                .child(format!("$ {}", shell.command)),
        )
        .when(wide, |header| {
            header
                .child(meta(format!("pid {}", shell.pid), theme))
                .child(meta(format!("by {}", shell.by), theme))
                .child(meta(shell.age.clone(), theme))
        })
        .children(shell.port.map(|port| meta(format!(":{port}"), theme)))
        .child(action(
            format!("{id}-open").into(),
            "open",
            theme,
            emit(on, ShellEvent::Open(ix)),
        ))
        .child(action(
            format!("{id}-kill").into(),
            "kill",
            theme,
            emit(on, ShellEvent::Kill(ix)),
        ))
        .child(
            trace(
                ElementId::Name(format!("{id}-trace").into()),
                shell.name.clone(),
                theme,
            )
            .flex_none()
            .on_click(emit(on, ShellEvent::Trace(ix))),
        );
    div()
        .flex()
        .flex_col()
        .min_w_0()
        .rounded(px(RADIUS_TAB))
        .bg(ink(theme, PANEL_FILL))
        .child(header)
        .child(div().p(px(BADGE_PAD_X)).child(TermCard::new(
            ElementId::Name(format!("{id}-term-{ix}").into()),
            shell.command.clone(),
            shell.lines.clone(),
            shell.status,
        )))
}

impl RenderOnce for Shells {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let Shells {
            id,
            shells,
            active,
            listing,
            on,
        } = self;
        let theme = ActiveTheme::theme(cx);
        let width = Width::of(format!("{id}-width"), window, cx);
        let measured = width.get(cx);
        let fit = width.fit(cx);
        let count = fitting(shells.len(), measured);
        let mut visible: Vec<usize> = (0..count).collect();
        if (count..shells.len()).contains(&active)
            && let Some(last) = visible.last_mut()
        {
            *last = active;
        }
        let hidden = shells.len().saturating_sub(visible.len());
        let tabs = visible.iter().filter_map(|ix| {
            let shell = shells.get(*ix)?;
            Some(shell_tab(&id, *ix, shell, *ix == active, &theme, &on).into_any_element())
        });
        let list_width = measured.map_or(LIST_WIDTH, |width| {
            LIST_WIDTH.min(width.as_f32() - LIST_INSET)
        });
        let more = (hidden > 0).then(|| {
            let trigger = pressed(
                div()
                    .id(ElementId::Name(format!("{id}-more").into()))
                    .aria_label("More shells")
                    .flex()
                    .flex_none()
                    .items_center()
                    .justify_center()
                    .gap_1()
                    .w(px(SHELL_MORE))
                    .h(px(TAB_IN_HEADER))
                    .rounded_t(px(RADIUS_TAB))
                    .cursor_pointer()
                    .text_size(px(FONT_TAB))
                    .font_weight(FontWeight::MEDIUM)
                    .text_color(ink(&theme, DIM_TEXT))
                    .on_click(emit(
                        &on,
                        if listing {
                            ShellEvent::Dismiss
                        } else {
                            ShellEvent::More
                        },
                    ))
                    .child(format!("{hidden} more"))
                    .child(glyph(Glyph::Chevron, ICON_SMALL, ink(&theme, DIM_TEXT))),
                theme.color(ColorToken::CardsInnerFill),
            );
            let dismiss = on.clone();
            Popover::new(ElementId::Name(format!("{id}-list").into()), trigger)
                .open(listing)
                .width(list_width)
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .on_mouse_down_out(move |_, window, cx| {
                            dismiss(&ShellEvent::Dismiss, window, cx)
                        })
                        .children(shells.iter().enumerate().map(|(ix, shell)| {
                            list_row(&id, ix, shell, ix == active, &theme, &on)
                        })),
                )
        });
        let body = shells
            .get(active)
            .map(|shell| panel(&id, active, shell, fit, &theme, &on));
        div()
            .relative()
            .flex()
            .flex_col()
            .min_w_0()
            .child(width.probe())
            .child(
                div()
                    .flex()
                    .items_end()
                    .min_w_0()
                    .h(px(HEADER_TABBED))
                    .pl(px(ROW_PAD_LEFT))
                    .pr(px(ROW_PAD_RIGHT))
                    .children(tabs)
                    .children(more),
            )
            .children(body.map(|body| {
                div()
                    .px(px(ROW_PAD_RIGHT))
                    .pb(px(ROW_PAD_RIGHT))
                    .child(body)
            }))
    }
}
