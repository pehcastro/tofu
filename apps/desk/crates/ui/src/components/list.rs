use std::time::Instant;

use desk_motion::{Glide, GlideKind};
use gpui::{
    AnyElement, App, Bounds, Corners, Div, ElementId, Entity, List, ListState, Pixels, Rgba,
    SharedString, Stateful, Transformation, UniformListScrollHandle, Window, canvas, div, list,
    prelude::*, px, radians, relative, uniform_list,
};

use crate::components::card::caption;
use crate::components::chip::{badge, mono, tabular};
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink, pressed, ring, tint};
use crate::components::scroll::scrollbar;
use crate::components::size::{
    CAPTION_TEXT, DIM_TEXT, FONT_BODY, FONT_SMALL, GRID_GAP, GRID_ON, GRID_PAD_X, GRID_ROW,
    GROUP_PAD_BOTTOM, GROUP_PAD_TOP, HOVER, RADIUS_ROW, ROW_GAP, ROW_ON, ROW_PAD_X, ROW_PAD_Y,
    SEPARATOR_INSET, T1,
};
use crate::live::ActiveTheme;
use crate::metrics::{HAIRLINE, ICON_TINY};
use crate::theme::{ColorToken, Theme};

pub fn row(
    id: impl Into<ElementId>,
    selected: bool,
    focused: bool,
    theme: &Theme,
) -> Stateful<Div> {
    pressed(
        bare_row(id, selected, focused, theme),
        theme.color(ColorToken::CardsInnerFill),
    )
}

pub fn bare_row(
    id: impl Into<ElementId>,
    selected: bool,
    focused: bool,
    theme: &Theme,
) -> Stateful<Div> {
    div()
        .id(id)
        .w_full()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(ROW_GAP))
        .px(px(ROW_PAD_X))
        .py(px(ROW_PAD_Y))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .text_size(px(FONT_BODY))
        .text_color(ink(theme, T1))
        .when(focused, |row| {
            row.shadow(vec![ring(theme.color(ColorToken::FocusRing))])
        })
        .when(selected, |row| row.bg(ink(theme, ROW_ON)))
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum HoverVariant {
    #[default]
    Glide,
    Snap,
}

impl HoverVariant {
    pub const ALL: &'static [Self] = &[HoverVariant::Glide, HoverVariant::Snap];

    pub fn label(self) -> &'static str {
        match self {
            HoverVariant::Glide => GlideKind::Eased.label(),
            HoverVariant::Snap => "one bar, jumps with no glide",
        }
    }
}

pub(crate) struct Marker {
    pub(crate) at: usize,
    pub(crate) kind: GlideKind,
    pub(crate) fill: Rgba,
    pub(crate) corners: Corners<Pixels>,
}

enum Entry {
    Item(Box<Stateful<Div>>),
    Inert(AnyElement),
}

struct Glides {
    items: Vec<Option<Bounds<Pixels>>>,
    hovered: Option<usize>,
    chosen: Option<usize>,
    selected: Option<usize>,
    hover: Glide,
    marker: Glide,
}

impl Glides {
    fn lit(&self) -> Option<usize> {
        self.hovered.filter(|ix| Some(*ix) != self.selected)
    }

    fn measure(&mut self, bounds: &[Bounds<Pixels>], inert: &[bool], now: Instant) {
        let items: Vec<Option<Bounds<Pixels>>> = bounds
            .iter()
            .zip(inert)
            .map(|(bounds, &skip)| (!skip).then_some(*bounds))
            .collect();
        if items != self.items {
            self.items = items;
            if let Some(bounds) = self.bounds_of(self.lit()) {
                self.hover.retarget(bounds, now);
            }
        }
        if let Some(bounds) = self.bounds_of(self.chosen) {
            self.marker.retarget(bounds, now);
        }
    }

    fn bounds_of(&self, at: Option<usize>) -> Option<Bounds<Pixels>> {
        at.and_then(|ix| self.items.get(ix).copied().flatten())
    }

    fn area(&self) -> Option<Bounds<Pixels>> {
        covering(self.items.iter().flatten().copied())
    }
}

fn covering(items: impl Iterator<Item = Bounds<Pixels>>) -> Option<Bounds<Pixels>> {
    items.reduce(|area, item| area.union(&item))
}

#[derive(IntoElement)]
pub struct HoverList {
    id: ElementId,
    frame: Div,
    entries: Vec<Entry>,
    selected: Option<usize>,
    variant: HoverVariant,
    backdrop: Rgba,
    pointer: bool,
    fill: Rgba,
    corners: Corners<Pixels>,
    marker: Option<Marker>,
}

impl HoverList {
    pub fn new(id: impl Into<ElementId>, theme: &Theme) -> Self {
        Self::within(
            id,
            div().flex().flex_col(),
            ink(theme, HOVER),
            Corners::all(px(RADIUS_ROW)),
            theme,
        )
    }

    pub(crate) fn within(
        id: impl Into<ElementId>,
        frame: Div,
        fill: Rgba,
        corners: Corners<Pixels>,
        theme: &Theme,
    ) -> Self {
        Self {
            id: id.into(),
            frame,
            entries: Vec::new(),
            selected: None,
            variant: HoverVariant::default(),
            backdrop: theme.color(ColorToken::CardsInnerFill),
            pointer: true,
            fill,
            corners,
            marker: None,
        }
    }

    pub fn item(mut self, item: Stateful<Div>) -> Self {
        self.entries.push(Entry::Item(Box::new(item)));
        self
    }

    pub fn items(mut self, items: impl IntoIterator<Item = Stateful<Div>>) -> Self {
        self.entries
            .extend(items.into_iter().map(|item| Entry::Item(Box::new(item))));
        self
    }

    pub fn inert(mut self, child: impl IntoElement) -> Self {
        self.entries.push(Entry::Inert(child.into_any_element()));
        self
    }

    pub fn selected(mut self, at: Option<usize>) -> Self {
        self.selected = at;
        self
    }

    pub fn variant(mut self, variant: HoverVariant) -> Self {
        self.variant = variant;
        self
    }

    pub(crate) fn backdrop(mut self, backdrop: Rgba) -> Self {
        self.backdrop = backdrop;
        self
    }

    pub(crate) fn keyed(mut self, marker: Option<Marker>) -> Self {
        self.pointer = false;
        self.marker = marker;
        self
    }
}

impl RenderOnce for HoverList {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let glides = window.use_keyed_state(self.id.clone(), cx, |_, _| Glides {
            items: Vec::new(),
            hovered: None,
            chosen: None,
            selected: None,
            hover: Glide::new(GlideKind::Eased),
            marker: Glide::new(GlideKind::Eased),
        });
        let reduced = cx.reduce_motion();
        let snap = self.variant == HoverVariant::Snap;
        let gliding = self.pointer;
        let marked = self
            .marker
            .as_ref()
            .map(|marker| (marker.fill, marker.corners));
        glides.update(cx, |glides, _| {
            glides.chosen = self.marker.as_ref().map(|marker| marker.at);
            glides.selected = self.selected;
            glides.hover.set_reduced(reduced || snap);
            if let Some(marker) = &self.marker {
                glides.marker.set_kind(marker.kind);
                glides
                    .marker
                    .set_reduced(reduced || (snap && !self.pointer));
            }
        });
        let (fill, corners, backdrop) = (self.fill, self.corners, self.backdrop);
        let painter = glides.clone();
        let bars = canvas(
            |_, _, _| (),
            move |_, (), window, cx| {
                let now = Instant::now();
                let (hover, marker, moving, area) = painter.update(cx, |glides, _| {
                    let hover = glides.hover.sample(now);
                    let marker = glides.marker.sample(now);
                    (
                        hover,
                        marker,
                        glides.hover.moving() || glides.marker.moving(),
                        glides.area(),
                    )
                });
                let inside =
                    |bounds: Bounds<Pixels>| area.map_or(bounds, |area| bounds.intersect(&area));
                if let (true, Some((bounds, shade))) = (gliding, hover) {
                    let lit = tint(fill, fill.alpha * shade);
                    window.paint_quad(gpui::fill(inside(bounds), lit).corner_radii(corners));
                }
                if let (Some((fill, corners)), Some((bounds, _))) = (marked, marker) {
                    window.paint_quad(gpui::fill(inside(bounds), fill).corner_radii(corners));
                }
                if moving {
                    window.request_animation_frame();
                }
            },
        )
        .absolute()
        .top_0()
        .left_0()
        .size_full();
        let measured = glides.clone();
        let mover = glides.clone();
        let leaver = glides;
        let inert: Vec<bool> = self
            .entries
            .iter()
            .map(|entry| matches!(entry, Entry::Inert(_)))
            .collect();
        let children = self.entries.into_iter().map(|entry| match entry {
            Entry::Item(item) => pressed(*item, backdrop).into_any_element(),
            Entry::Inert(child) => child,
        });
        self.frame
            .relative()
            .on_children_prepainted(move |bounds, _, cx| {
                let items = bounds.get(1..).unwrap_or_default();
                measured.update(cx, |glides, _| {
                    glides.measure(items, &inert, Instant::now())
                });
            })
            .id(self.id)
            .when(gliding, |frame| {
                frame
                    .on_mouse_move(move |event, _, cx| {
                        mover.update(cx, |glides, cx| {
                            let found = glides.items.iter().enumerate().find_map(|(ix, bounds)| {
                                bounds
                                    .filter(|bounds| bounds.contains(&event.position))
                                    .map(|bounds| (ix, bounds))
                            });
                            let Some((ix, bounds)) =
                                found.filter(|(ix, _)| glides.hovered != Some(*ix))
                            else {
                                return;
                            };
                            glides.hovered = Some(ix);
                            match glides.lit() {
                                Some(_) => glides.hover.retarget(bounds, Instant::now()),
                                None => glides.hover.hide(Instant::now()),
                            }
                            cx.notify();
                        });
                    })
                    .on_hover(move |inside, _, cx| {
                        if !*inside {
                            leaver.update(cx, |glides, cx| {
                                glides.hovered = None;
                                glides.hover.hide(Instant::now());
                                cx.notify();
                            });
                        }
                    })
            })
            .child(bars)
            .children(children)
    }
}

struct RowGlides {
    glide: Glide,
    hovered: Option<usize>,
    seen: Vec<(usize, Bounds<Pixels>)>,
}

impl RowGlides {
    fn bounds_of(&self, ix: usize) -> Option<Bounds<Pixels>> {
        self.seen
            .iter()
            .find_map(|(at, bounds)| (*at == ix).then_some(*bounds))
    }
}

#[derive(Clone)]
pub struct RowGlide {
    state: Entity<RowGlides>,
    fill: Rgba,
    backdrop: Rgba,
}

impl RowGlide {
    pub fn new(
        id: impl Into<ElementId>,
        variant: HoverVariant,
        theme: &Theme,
        window: &mut Window,
        cx: &mut App,
    ) -> Self {
        let state = window.use_keyed_state(id.into(), cx, |_, _| RowGlides {
            glide: Glide::new(GlideKind::Eased),
            hovered: None,
            seen: Vec::new(),
        });
        let reduced = cx.reduce_motion();
        state.update(cx, |rows, _| {
            rows.glide
                .set_reduced(reduced || variant == HoverVariant::Snap);
        });
        Self {
            state,
            fill: ink(theme, HOVER),
            backdrop: theme.color(ColorToken::CardsInnerFill),
        }
    }

    pub fn row(&self, ix: usize, item: Stateful<Div>) -> Stateful<Div> {
        let item = pressed(item, self.backdrop);
        let (probe, mover) = (self.state.clone(), self.state.clone());
        item.relative()
            .on_mouse_move(move |_, _, cx| {
                mover.update(cx, |rows, cx| {
                    if rows.hovered != Some(ix) {
                        rows.hovered = Some(ix);
                        if let Some(bounds) = rows.bounds_of(ix) {
                            rows.glide.retarget(bounds, Instant::now());
                        }
                        cx.notify();
                    }
                });
            })
            .child(
                canvas(
                    move |bounds, _, cx| {
                        probe.update(cx, |rows, _| {
                            rows.seen.push((ix, bounds));
                            if rows.hovered == Some(ix) {
                                rows.glide.retarget(bounds, Instant::now());
                            }
                        })
                    },
                    |_, (), _, _| {},
                )
                .absolute()
                .top_0()
                .left_0()
                .size_full(),
            )
    }

    pub fn frame(&self, id: impl Into<ElementId>, list: impl IntoElement) -> Stateful<Div> {
        let (clearer, painter, leaver) =
            (self.state.clone(), self.state.clone(), self.state.clone());
        let fill = self.fill;
        div()
            .id(id)
            .relative()
            .on_hover(move |inside, _, cx| {
                if !*inside {
                    leaver.update(cx, |rows, cx| {
                        rows.hovered = None;
                        rows.glide.hide(Instant::now());
                        cx.notify();
                    });
                }
            })
            .child(
                canvas(
                    move |_, _, cx| clearer.update(cx, |rows, _| rows.seen.clear()),
                    move |bounds, (), window, cx| {
                        let (bar, moving, area) = painter.update(cx, |rows, _| {
                            (
                                rows.glide.sample(Instant::now()),
                                rows.glide.moving(),
                                covering(rows.seen.iter().map(|(_, row)| *row)),
                            )
                        });
                        if let Some((rect, shade)) = bar {
                            let lit = tint(fill, fill.alpha * shade);
                            let area = area.map_or(bounds, |area| area.intersect(&bounds));
                            window.paint_quad(
                                gpui::fill(rect.intersect(&area), lit)
                                    .corner_radii(Corners::all(px(RADIUS_ROW))),
                            );
                        }
                        if moving {
                            window.request_animation_frame();
                        }
                    },
                )
                .absolute()
                .top_0()
                .left_0()
                .size_full(),
            )
            .child(list)
    }
}

pub fn group_header(
    id: impl Into<ElementId>,
    title: &'static str,
    count: usize,
    open: bool,
    theme: &Theme,
) -> Stateful<Div> {
    let turn = if open {
        std::f32::consts::FRAC_PI_2
    } else {
        0.0
    };
    pressed(
        div()
            .id(id)
            .flex()
            .items_center()
            .gap_1p5()
            .px(px(ROW_PAD_X))
            .pt(px(GROUP_PAD_TOP))
            .pb(px(GROUP_PAD_BOTTOM))
            .rounded(px(RADIUS_ROW))
            .cursor_pointer(),
        theme.color(ColorToken::CardsInnerFill),
    )
    .child(
        glyph(Glyph::Chevron, ICON_TINY, ink(theme, CAPTION_TEXT))
            .with_transformation(Transformation::rotate(radians(turn))),
    )
    .child(caption(title, theme))
    .child(badge(count.to_string(), theme))
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Width {
    Fixed(f32),
    Flex(f32),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CellKind {
    Text,
    Mono,
    Time,
}

pub struct Column {
    pub title: &'static str,
    pub width: Width,
    pub kind: CellKind,
}

fn cell(column: &Column, theme: &Theme) -> Div {
    let frame = div().min_w_0().truncate();
    let frame = match column.width {
        Width::Fixed(width) => frame.flex_none().w(px(width)),
        Width::Flex(weight) => frame
            .flex_grow(weight)
            .flex_shrink(1.0)
            .flex_basis(relative(0.0)),
    };
    match column.kind {
        CellKind::Text => frame,
        CellKind::Mono => frame.font_family(mono(theme)).text_size(px(FONT_SMALL)),
        CellKind::Time => frame
            .font_features(tabular())
            .text_color(ink(theme, DIM_TEXT)),
    }
}

fn grid_line(theme: &Theme) -> Div {
    div()
        .h(px(GRID_ROW))
        .w_full()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(GRID_GAP))
        .px(px(GRID_PAD_X))
        .text_size(px(FONT_BODY))
        .text_color(ink(theme, T1))
}

type CellText = Box<dyn Fn(usize, usize) -> SharedString>;

#[derive(IntoElement)]
pub struct Table {
    id: &'static str,
    columns: &'static [Column],
    rows: usize,
    selected: Option<usize>,
    scroll: UniformListScrollHandle,
    theme: Theme,
    text: CellText,
    hover: HoverVariant,
}

pub fn table(
    id: &'static str,
    columns: &'static [Column],
    rows: usize,
    selected: Option<usize>,
    scroll: &UniformListScrollHandle,
    theme: &Theme,
    text: impl Fn(usize, usize) -> SharedString + 'static,
) -> Table {
    Table {
        id,
        columns,
        rows,
        selected,
        scroll: scroll.clone(),
        theme: theme.clone(),
        text: Box::new(text),
        hover: HoverVariant::default(),
    }
}

impl Table {
    pub fn hover(mut self, hover: HoverVariant) -> Self {
        self.hover = hover;
        self
    }
}

impl RenderOnce for Table {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let Self {
            id,
            columns,
            rows,
            selected,
            scroll,
            theme,
            text,
            hover,
        } = self;
        let glide = RowGlide::new(
            SharedString::from(format!("{id}-glide")),
            hover,
            &theme,
            window,
            cx,
        );
        let lines = glide.clone();
        let list = uniform_list(id, rows, move |range, _, cx| {
            let theme = ActiveTheme::theme(cx);
            range
                .map(|ix| {
                    lines.row(
                        ix,
                        grid_line(&theme)
                            .id((id, ix))
                            .cursor_pointer()
                            .when(selected == Some(ix), |line| line.bg(ink(&theme, GRID_ON)))
                            .children(
                                columns
                                    .iter()
                                    .enumerate()
                                    .map(|(at, column)| cell(column, &theme).child(text(ix, at))),
                            ),
                    )
                })
                .collect::<Vec<_>>()
        })
        .size_full()
        .track_scroll(&scroll);
        let base = scroll.0.borrow().base_handle.clone();
        div()
            .flex()
            .flex_col()
            .size_full()
            .min_h_0()
            .child(
                grid_line(&theme)
                    .border_b_1()
                    .border_color(theme.color(ColorToken::Separator))
                    .children(
                        columns.iter().map(|column| {
                            cell(column, &theme).child(caption(column.title, &theme))
                        }),
                    ),
            )
            .child(
                div()
                    .relative()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_h_0()
                    .child(
                        glide
                            .frame(SharedString::from(format!("{id}-frame")), list)
                            .flex_1()
                            .min_h_0(),
                    )
                    .child(scrollbar(SharedString::from(format!("{id}-bar")), &base)),
            )
    }
}

pub fn feed(
    state: &ListState,
    mut item: impl FnMut(usize, &mut Window, &mut App) -> AnyElement + 'static,
) -> List {
    list(state.clone(), move |ix, window, cx| {
        div().pb_2().child(item(ix, window, cx)).into_any_element()
    })
    .size_full()
}

pub fn separator(theme: &Theme) -> Div {
    div()
        .h(px(HAIRLINE))
        .mx(px(SEPARATOR_INSET))
        .bg(theme.color(ColorToken::Separator))
}
