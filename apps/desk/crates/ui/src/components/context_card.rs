use std::rc::Rc;
use std::time::Instant;

use desk_motion::tokens::{EASE_OUT, TOGGLE_MS};
use gpui::{
    App, ClickEvent, Context, Div, Entity, FocusHandle, Focusable, FontWeight, MouseMoveEvent,
    Rgba, SharedString, Subscription, Window, div, prelude::*, px, relative,
};

use crate::component::icon;
use crate::components::charts::{Chart, DotGrid, GridPart, PartHovered, cached};
use crate::components::chip::tabular;
use crate::components::list::{bare_row, separator};
use crate::components::logo::{Brand, logo};
use crate::components::paint::{ink, tint};
use crate::components::size::{CHIP_FILL, FONT_BODY, FONT_SMALL, RADIUS_CHIP_SMALL, T1, T2, T3};
use crate::components::status_bar::{ContextUse, Level};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{HAIRLINE, ICON_SMALL};
use crate::theme::{ColorToken, Theme};

const TITLE: &str = "Context window";
const OPEN_CONTEXT: &str = "Open context";
const READING: &str = "Reading the bands from query.context";
const DOTS: usize = 200;
const COUNT_MS: f32 = 0.35;
const HEADER_ROW: f32 = 22.0;
const FIGURE_ROW: f32 = 32.0;
const LEGEND_ROW: f32 = 26.0;
const FOOTER_ROW: f32 = 20.0;
const GAP: f32 = 12.0;
const SECTIONS: f32 = 6.0;
const FIGURE: f32 = 28.0;
const SWATCH: f32 = 8.0;
const SWATCH_RADIUS: f32 = 2.0;
const CHIP_PAD: f32 = 6.0;
const CHIP_WIDEST: f32 = 180.0;
const BADGE_FILL: f32 = 0.16;
const BADGE_RADIUS: f32 = 4.0;
const DIM: f32 = 0.4;
const STRONG_SWATCH: f32 = 0.55;
const SINCE_SWATCH: f32 = 0.22;

#[derive(Clone, PartialEq)]
pub struct Band {
    pub name: SharedString,
    pub tokens: i64,
}

#[derive(Clone, PartialEq)]
pub enum Bands {
    Reading,
    Unread(SharedString),
    Read { bands: Vec<Band>, mark: i64 },
}

pub fn band_color(at: usize, theme: &Theme) -> Rgba {
    match at {
        0 => ink(theme, STRONG_SWATCH),
        1 => theme.color(ColorToken::StatusAccent),
        2 => theme.color(ColorToken::StatusLive),
        3 => theme.color(ColorToken::Trace),
        _ => ink(theme, SINCE_SWATCH),
    }
}

struct Count {
    from: f32,
    to: f32,
    since: Instant,
}

impl Count {
    fn at(&self, now: Instant) -> f32 {
        let t = (now.duration_since(self.since).as_secs_f32() / COUNT_MS).clamp(0.0, 1.0);
        self.from + (self.to - self.from) * EASE_OUT(t)
    }

    fn moving(&self, now: Instant) -> bool {
        now.duration_since(self.since).as_secs_f32() < COUNT_MS && self.from != self.to
    }

    fn retarget(&mut self, to: f32, reduced: bool) {
        let now = Instant::now();
        self.from = if reduced { to } else { self.at(now) };
        self.to = to;
        self.since = now;
    }
}

type OnOpen = Rc<dyn Fn(&mut Window, &mut App)>;

pub struct ContextCard {
    shown: ContextUse,
    grid: Entity<DotGrid>,
    in_grid: Option<usize>,
    in_legend: Option<usize>,
    lit: Option<usize>,
    was: Option<usize>,
    swapped: Instant,
    figure: Count,
    focus: FocusHandle,
    on_open: OnOpen,
    _hovered: Subscription,
}

impl ContextCard {
    pub fn new(
        shown: ContextUse,
        on_open: impl Fn(&mut Window, &mut App) + 'static,
        cx: &mut Context<Self>,
    ) -> Self {
        let theme = ActiveTheme::theme(cx);
        let (parts, fork_at, per_dot) = parts(&shown, &theme);
        let grid = cx.new(|_| DotGrid::new(parts, DOTS, fork_at, None).per_dot(per_dot));
        let hovered = cx.subscribe(&grid, |card, _, hovered: &PartHovered, cx| {
            card.in_grid = hovered.0;
            card.light(cx);
        });
        ContextCard {
            figure: Count {
                from: shown.used as f32,
                to: shown.used as f32,
                since: Instant::now(),
            },
            shown,
            grid,
            in_grid: None,
            in_legend: None,
            lit: None,
            was: None,
            swapped: Instant::now(),
            focus: cx.focus_handle(),
            on_open: Rc::new(on_open),
            _hovered: hovered,
        }
    }

    pub fn set(&mut self, shown: ContextUse, cx: &mut Context<Self>) {
        if shown == self.shown {
            return;
        }
        let theme = ActiveTheme::theme(cx);
        let (parts, fork_at, _) = parts(&shown, &theme);
        self.grid.update(cx, |grid, cx| {
            grid.refill(parts, DOTS, fork_at);
            cx.notify();
        });
        self.shown = shown;
        let count = self.bands().len();
        for pointed in [&mut self.in_grid, &mut self.in_legend, &mut self.lit] {
            *pointed = pointed.filter(|&at| at < count);
        }
        let target = self.target();
        self.figure.retarget(target, cx.reduce_motion());
        cx.notify();
    }

    pub fn height(&self, cx: &App) -> f32 {
        let rows = match &self.shown.bands {
            Bands::Read { bands, .. } => bands.len().max(1),
            Bands::Reading | Bands::Unread(_) => 1,
        };
        HEADER_ROW
            + FIGURE_ROW
            + self.grid.read(cx).extent().1
            + rows as f32 * LEGEND_ROW
            + HAIRLINE
            + FOOTER_ROW
            + (SECTIONS - 1.0) * GAP
    }

    fn bands(&self) -> &[Band] {
        match &self.shown.bands {
            Bands::Read { bands, .. } => bands,
            Bands::Reading | Bands::Unread(_) => &[],
        }
    }

    fn band(&self, at: Option<usize>) -> Option<&Band> {
        self.bands().get(at?)
    }

    fn target(&self) -> f32 {
        self.band(self.lit)
            .map_or(self.shown.used, |band| band.tokens) as f32
    }

    fn light(&mut self, cx: &mut Context<Self>) {
        let at = self.in_legend.or(self.in_grid);
        if at == self.lit {
            return;
        }
        self.was = self.lit;
        self.lit = at;
        self.swapped = Instant::now();
        let target = self.target();
        self.figure.retarget(target, cx.reduce_motion());
        self.grid.update(cx, |grid, cx| {
            grid.highlight(at);
            cx.notify();
        });
        cx.notify();
    }

    fn header(&self, theme: &Theme) -> Div {
        let chip = self.shown.model.clone().map(|slug| {
            let brand = slug.split('/').next().and_then(Brand::for_source);
            div()
                .flex()
                .flex_none()
                .items_center()
                .gap_1()
                .max_w(px(CHIP_WIDEST))
                .h_full()
                .px(px(CHIP_PAD))
                .rounded(px(RADIUS_CHIP_SMALL))
                .bg(ink(theme, CHIP_FILL))
                .text_size(px(FONT_SMALL))
                .text_color(ink(theme, T2))
                .children(brand.map(|brand| logo(brand, px(ICON_SMALL), theme)))
                .child(div().min_w_0().truncate().child(slug))
        });
        div()
            .flex()
            .items_center()
            .gap_2()
            .h(px(HEADER_ROW))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .text_size(px(FONT_BODY))
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(ink(theme, T1))
                    .child(TITLE),
            )
            .children(chip)
    }

    fn figure(&self, now: Instant, swap: f32, theme: &Theme) -> Div {
        let label = match self.band(self.lit) {
            Some(band) => div()
                .text_color(ink(theme, T1))
                .opacity(swap)
                .child(band.name.clone()),
            None => div()
                .text_color(ink(theme, T2))
                .opacity(swap)
                .child(format!("of {} tokens", short(self.shown.ceiling as f32))),
        };
        let badge = percent(self.shown.used, self.shown.ceiling).map(|percent| {
            let level = Level::of(percent);
            let fill = match level {
                Level::Normal => ink(theme, CHIP_FILL),
                Level::Warning | Level::NearLimit => tint(level.fill(theme), BADGE_FILL),
            };
            div()
                .flex_none()
                .px_1p5()
                .rounded(px(BADGE_RADIUS))
                .bg(fill)
                .text_size(px(FONT_SMALL))
                .font_weight(FontWeight::MEDIUM)
                .font_features(tabular())
                .text_color(level.text(theme))
                .child(format!("{percent}%"))
        });
        div()
            .flex()
            .items_center()
            .gap_2()
            .h(px(FIGURE_ROW))
            .child(
                div()
                    .flex_none()
                    .text_size(px(FIGURE))
                    .line_height(relative(1.0))
                    .font_weight(FontWeight::SEMIBOLD)
                    .font_features(tabular())
                    .text_color(ink(theme, T1))
                    .child(short(self.figure.at(now))),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .text_size(px(FONT_SMALL))
                    .child(label),
            )
            .children(badge)
    }

    fn legend(&self, swap: f32, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let legend = div().flex().flex_col();
        let note = |text: SharedString, color: Rgba| {
            div()
                .h(px(LEGEND_ROW))
                .flex()
                .items_center()
                .truncate()
                .text_size(px(FONT_SMALL))
                .text_color(color)
                .child(text)
        };
        let bands = match &self.shown.bands {
            Bands::Reading => return legend.child(note(READING.into(), ink(theme, T3))),
            Bands::Unread(reason) => {
                return legend.child(note(reason.clone(), theme.color(ColorToken::StatusWarn)));
            }
            Bands::Read { bands, .. } if bands.is_empty() => {
                return legend.child(note(
                    "query.context reported no bands".into(),
                    ink(theme, T3),
                ));
            }
            Bands::Read { bands, .. } => bands,
        };
        let hover = theme.color(ColorToken::StateHover);
        let blend = |at: usize, of: fn(Option<usize>, usize) -> f32| {
            of(self.was, at) + (of(self.lit, at) - of(self.was, at)) * swap
        };
        legend.children(bands.iter().enumerate().map(|(at, band)| {
            let shade = blend(at, |lit, at| match lit {
                Some(lit) if lit != at => DIM,
                Some(_) | None => 1.0,
            });
            let raised = blend(at, |lit, at| if lit == Some(at) { 1.0 } else { 0.0 });
            bare_row(("context-card-band", at), false, false, theme)
                .h(px(LEGEND_ROW))
                .py_0()
                .bg(tint(hover, hover.alpha * raised))
                .text_color(ink(theme, T1 * shade))
                .on_mouse_move(cx.listener(move |card, _: &MouseMoveEvent, _, cx| {
                    if card.in_legend != Some(at) {
                        card.in_legend = Some(at);
                        card.light(cx);
                    }
                }))
                .on_hover(cx.listener(move |card, over: &bool, _, cx| {
                    if !*over && card.in_legend == Some(at) {
                        card.in_legend = None;
                        card.light(cx);
                    }
                }))
                .child(
                    div()
                        .flex_none()
                        .size(px(SWATCH))
                        .rounded(px(SWATCH_RADIUS))
                        .bg(band_color(at, theme))
                        .opacity(shade),
                )
                .child(div().flex_1().min_w_0().truncate().child(band.name.clone()))
                .child(
                    div()
                        .flex_none()
                        .font_features(tabular())
                        .child(short(band.tokens as f32)),
                )
        }))
    }

    fn footer(&self, swap: f32, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let said = match self.band(self.lit) {
            Some(band) => div().text_color(ink(theme, T1)).child(format!(
                "{} · {} of the window",
                band.name,
                share(band.tokens, self.shown.ceiling)
            )),
            None => {
                let level =
                    percent(self.shown.used, self.shown.ceiling).map_or(Level::Normal, Level::of);
                div().text_color(level.text(theme)).child(format!(
                    "{} left",
                    short(self.shown.ceiling.saturating_sub(self.shown.used).max(0) as f32)
                ))
            }
        };
        let on_open = self.on_open.clone();
        div()
            .flex()
            .items_center()
            .gap_2()
            .h(px(FOOTER_ROW))
            .text_size(px(FONT_SMALL))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .opacity(swap)
                    .child(said),
            )
            .child(
                div()
                    .id("context-card-open")
                    .flex()
                    .flex_none()
                    .items_center()
                    .gap_1()
                    .cursor_pointer()
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(ink(theme, T2))
                    .hover(|style| style.text_color(ink(theme, T1)))
                    .on_click(cx.listener(move |_, _: &ClickEvent, window, cx| on_open(window, cx)))
                    .child(OPEN_CONTEXT)
                    .child(icon(Icon::Arrow, ICON_SMALL, ink(theme, T2))),
            )
    }
}

impl Focusable for ContextCard {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

impl Render for ContextCard {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let now = Instant::now();
        let elapsed = now.duration_since(self.swapped).as_secs_f32() / TOGGLE_MS.as_secs_f32();
        let swap = if cx.reduce_motion() {
            1.0
        } else {
            EASE_OUT(elapsed.clamp(0.0, 1.0))
        };
        if swap < 1.0 || self.figure.moving(now) {
            window.request_animation_frame();
        }
        div()
            .track_focus(&self.focus)
            .size_full()
            .flex()
            .flex_col()
            .gap(px(GAP))
            .overflow_hidden()
            .child(self.header(&theme))
            .child(self.figure(now, swap, &theme))
            .child(cached(&self.grid, cx))
            .child(self.legend(swap, &theme, cx))
            .child(separator(&theme).mx_0())
            .child(self.footer(swap, &theme, cx))
    }
}

fn parts(shown: &ContextUse, theme: &Theme) -> (Vec<GridPart>, usize, usize) {
    let whole = shown.ceiling.max(shown.used).max(1);
    let per_dot = whole.saturating_add(DOTS as i64 - 1) / DOTS as i64;
    let dots = |tokens: i64| {
        usize::try_from(tokens.max(0).saturating_add(per_dot / 2) / per_dot).unwrap_or(DOTS)
    };
    let per_dot_tokens = usize::try_from(per_dot).unwrap_or(usize::MAX);
    match &shown.bands {
        Bands::Read { bands, mark } => (
            bands
                .iter()
                .enumerate()
                .map(|(at, band)| GridPart {
                    name: band.name.clone(),
                    tokens: dots(band.tokens),
                    color: band_color(at, theme),
                })
                .collect(),
            dots(*mark),
            per_dot_tokens,
        ),
        Bands::Reading | Bands::Unread(_) => (Vec::new(), 0, per_dot_tokens),
    }
}

fn percent(used: i64, ceiling: i64) -> Option<u8> {
    let percent = used.max(0).saturating_mul(100).checked_div(ceiling)?;
    u8::try_from(percent.min(100)).ok()
}

fn share(tokens: i64, ceiling: i64) -> String {
    if ceiling <= 0 {
        return "none".to_owned();
    }
    let share = tokens.max(0) as f32 / ceiling as f32 * 100.0;
    if share < 10.0 {
        format!("{share:.1}%")
    } else {
        format!("{share:.0}%")
    }
}

fn short(tokens: f32) -> String {
    match tokens {
        t if t >= 1_000_000.0 => format!("{:.2}M", t / 1_000_000.0),
        t if t >= 1_000.0 => format!("{:.1}K", t / 1_000.0),
        t => format!("{t:.0}"),
    }
}
