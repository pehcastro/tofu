use std::f32::consts::PI;
use std::rc::Rc;
use std::sync::Arc;

use desk_motion::GlideKind;
use gpui::{
    AnyElement, App, ClickEvent, Context, Corners, Div, ElementId, Entity, FontWeight, Image,
    ImageFormat, Img, MouseButton, MouseDownEvent, Pixels, Point, Rgba, SharedString, Stateful,
    Transformation, Window, div, img, prelude::*, px, radians,
};

use crate::component::icon;
use crate::components::avatar::spinner;
use crate::components::chip::{file_chip, kbd, mention, mono, remove_x};
use crate::components::form::TextArea;
use crate::components::glyph::Glyph;
use crate::components::list::{HoverList, Marker};
use crate::components::overlay::{Align, Placement, Popover, Side};
use crate::components::paint::{drop, glyph, ink, pressed, ring, tint};
use crate::components::size::{
    CAPTION_TEXT, CHIP, CHIP_PAD, COMPOSER_PAD_BOTTOM, COMPOSER_PAD_LEFT, COMPOSER_PAD_RIGHT,
    COMPOSER_PAD_TOP, COMPOSER_RING, FCHIP, FCHIP_PAD, FONT_BADGE, FONT_CAP2, FONT_CHAT,
    FONT_SMALL, FONT_TAB, HOVER, LINE_CHAT, MENTION_TINT, MENU_PAD, POPOVER_PAD_X, POPOVER_PAD_Y,
    RADIUS_BADGE, RADIUS_CHIP, RADIUS_COMPOSER, RADIUS_ROW, ROW_PAD_X, ROW_PAD_Y, T1, T2, T3,
};
use crate::components::tooltip::{Edge, tooltip};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::{HAIRLINE, ICON, ICON_SMALL};
use crate::theme::{ColorToken, Theme};

const SEND: f32 = 28.0;
const SEND_IDLE: f32 = 0.18;
const SEND_IDLE_MARK: f32 = 0.6;
const ATTACH: f32 = 26.0;
const ATTACH_ONE_LINE: f32 = 28.0;
const ONE_LINE_PAD: f32 = 6.0;
const ONE_LINE_GAP: f32 = 4.0;
const GROWN_GAP: f32 = 10.0;
const ROW_GAP: f32 = 6.0;
const CHIP_TEXT: f32 = 0.85;
const CHEVRON: f32 = 0.6;
const STATUS_GAP: f32 = 10.0;
const STATUS_PAD_X: f32 = 4.0;
const STATUS_PAD_BOTTOM: f32 = 6.0;
const QUEUED_MAX: f32 = 520.0;
const QUEUED_RADIUS: f32 = 14.0;
const QUEUED_RING: f32 = 0.14;
const QUEUED_PAD_TOP: f32 = 7.0;
const QUEUED_PAD_X: f32 = 14.0;
const QUEUED_PAD_BOTTOM: f32 = 10.0;
const WHO_TEXT: f32 = 0.45;
const FONT_WHO: f32 = 11.5;
const LINE_WHO: f32 = 18.0;
const WHO_GAP: f32 = 8.0;
const HOME_STACK_GAP: f32 = 10.0;
const HOME_RADIUS: f32 = 18.0;
const HOME_FILL: f32 = 0.55;
const HOME_BLUR: f32 = 26.0;
const HOME_RING: f32 = 0.16;
const HOME_DROP_Y: f32 = 24.0;
const HOME_DROP_BLUR: f32 = 70.0;
const HOME_DROP: f32 = 0.4;
const HOME_PAD_TOP: f32 = 14.0;
const HOME_PAD_RIGHT: f32 = 12.0;
const HOME_PAD_BOTTOM: f32 = 10.0;
const HOME_PAD_LEFT: f32 = 16.0;
const HOME_GAP: f32 = 20.0;
const HOME_FONT: f32 = 15.0;
const HOME_ROW_GAP: f32 = 8.0;
const HOME_SEND: f32 = 30.0;
const HOME_SEND_FILL: f32 = 0.92;
const LINK_GAP: f32 = 16.0;
const LINK_INNER_GAP: f32 = 6.0;
const LINK_TEXT: f32 = 0.72;
const UNDER_PAD_X: f32 = 6.0;
const MENU_OFFSET: f32 = 6.0;
const MENU_MIN: f32 = 120.0;
const ROW_INNER_GAP: f32 = 10.0;
const NAME_GAP: f32 = 7.0;
const CAPTION_PAD_Y: f32 = 6.0;
const CAPTION_TRACK: f32 = 0.7;
const RULE: f32 = 0.07;
const RULE_X: f32 = 6.0;
const RULE_Y: f32 = 4.0;
const TILE: f32 = 22.0;
const TILE_RADIUS: f32 = 7.0;
const TILE_FILL: f32 = 0.1;
const FONT_TILE: f32 = 11.0;
const FONT_PATH: f32 = 11.0;
const LINE_PATH: f32 = 16.0;
const DOT: f32 = 6.0;
const DOT_TOP: f32 = 7.0;
const FINISHED_DOT: f32 = 0.3;
const LATEST_PAD_X: f32 = 6.0;
const LATEST_PAD_Y: f32 = 1.0;
const LATEST_FILL: f32 = 0.16;
const LATEST_LIFT: f32 = 0.4;
const LINE_LATEST: f32 = 14.0;
const ICON_RASTER: f32 = 4.0;
const LATEST: &str = "Latest";
const RESUME: &str = "Resume";
const ALL_SESSIONS_KEYS: &str = "ctrl r";
const EFFORTS: [&str; 3] = ["low", "medium", "high"];
const CARD: &str = r#"<rect x="2.5" y="4" width="11" height="8" rx="1.5"/><path d="M6 14h4"/>"#;
const FOLDER: &str = r#"<path d="M2.5 4.5h4l1.5 1.5h5.5v6.5h-11z"/>"#;
const CLOCK: &str = r#"<circle cx="8" cy="8" r="5.5"/><path d="M8 5v3l2 1.5"/>"#;
const WORKING: &str = "working";

const CHIPS_SHOWN: usize = 3;
const MENU_SHADE: f32 = 1.0;

type OnText = Rc<dyn Fn(&str, &mut Window, &mut App)>;
type OnAction = Rc<dyn Fn(&mut Window, &mut App)>;
type OnIndex = Rc<dyn Fn(usize, &mut Window, &mut App)>;

#[derive(Clone)]
pub struct TraceChip {
    pub glyph: Glyph,
    pub label: SharedString,
    pub broken: Option<SharedString>,
}

#[derive(Clone)]
pub struct MentionRow {
    pub glyph: Glyph,
    pub label: SharedString,
    pub detail: SharedString,
}

#[derive(Clone)]
pub struct MentionGroup {
    pub title: SharedString,
    pub rows: Vec<MentionRow>,
}

#[derive(Clone)]
pub struct MentionMenu {
    pub groups: Vec<MentionGroup>,
    pub at: usize,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum ComposerVariant {
    #[default]
    Grows,
    OneLine,
    Grown,
}

impl ComposerVariant {
    pub const ALL: &'static [Self] = &[Self::Grows, Self::OneLine, Self::Grown];

    pub fn label(self) -> &'static str {
        match self {
            Self::Grows => "one line, grows once there is text",
            Self::OneLine => "always one line",
            Self::Grown => "always grown",
        }
    }
}

fn up() -> Transformation {
    Transformation::rotate(radians(PI))
}

pub fn picker(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    theme: &Theme,
) -> Stateful<Div> {
    let hover = ink(theme, HOVER);
    div()
        .id(id)
        .flex()
        .flex_none()
        .items_center()
        .gap_1p5()
        .h(px(CHIP))
        .px(px(CHIP_PAD))
        .rounded(px(RADIUS_CHIP))
        .cursor_pointer()
        .text_size(px(FONT_TAB))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, CHIP_TEXT))
        .hover(move |style| style.bg(hover))
        .child(label.into())
        .child(glyph(Glyph::Chevron, ICON_SMALL, ink(theme, T3)).with_transformation(up()))
}

fn circle(id: &'static str, label: &'static str, size: f32, fill: Rgba) -> Stateful<Div> {
    pressed(
        div()
            .id(id)
            .aria_label(label)
            .flex()
            .flex_none()
            .items_center()
            .justify_center()
            .size(px(size))
            .rounded_full()
            .cursor_pointer()
            .bg(fill),
        fill,
    )
}

fn attach(
    id: &'static str,
    size: f32,
    on_attach: Option<OnAction>,
    theme: &Theme,
) -> Stateful<Div> {
    let hover = ink(theme, HOVER);
    let color = theme.color(ColorToken::TextIcon);
    pressed(
        div()
            .id(id)
            .aria_label("Attach")
            .flex()
            .flex_none()
            .items_center()
            .justify_center()
            .size(px(size))
            .rounded(px(RADIUS_CHIP))
            .cursor_pointer()
            .hover(move |style| style.bg(hover)),
        color,
    )
    .child(icon(Icon::Plus, ICON, color))
    .when_some(on_attach, |attach, on_attach| {
        attach.on_click(move |_, window, cx| on_attach(window, cx))
    })
}

fn submit(area: &Entity<TextArea>, on_send: &OnText, window: &mut Window, cx: &mut App) {
    let text = area.read(cx).text();
    on_send(&text, window, cx);
    area.update(cx, |area, cx| area.clear(cx));
}

#[derive(IntoElement)]
pub struct Composer {
    id: ElementId,
    area: Entity<TextArea>,
    variant: ComposerVariant,
    busy: bool,
    phase: Option<SharedString>,
    files: Vec<SharedString>,
    traces: Vec<TraceChip>,
    held: bool,
    menu: Option<(MentionMenu, OnIndex)>,
    queued: Vec<SharedString>,
    model: Option<AnyElement>,
    effort: Option<AnyElement>,
    on_send: Option<OnText>,
    on_stop: Option<OnAction>,
    on_attach: Option<OnAction>,
    on_remove: Option<OnIndex>,
    on_remove_trace: Option<OnIndex>,
    on_unqueue: Option<OnIndex>,
    content_width: Option<Pixels>,
}

impl Composer {
    pub fn new(id: impl Into<ElementId>, area: Entity<TextArea>) -> Self {
        Self {
            id: id.into(),
            area,
            variant: ComposerVariant::default(),
            busy: false,
            phase: None,
            files: Vec::new(),
            traces: Vec::new(),
            held: false,
            menu: None,
            queued: Vec::new(),
            model: None,
            effort: None,
            on_send: None,
            on_stop: None,
            on_attach: None,
            on_remove: None,
            on_remove_trace: None,
            on_unqueue: None,
            content_width: None,
        }
    }

    pub fn content_width(mut self, width: Pixels) -> Self {
        self.content_width = Some(width);
        self
    }

    pub fn variant(mut self, variant: ComposerVariant) -> Self {
        self.variant = variant;
        self
    }

    pub fn busy(mut self, busy: bool) -> Self {
        self.busy = busy;
        self
    }

    pub fn phase(mut self, phase: impl Into<SharedString>) -> Self {
        self.phase = Some(phase.into());
        self
    }

    pub fn attachments(mut self, files: Vec<SharedString>) -> Self {
        self.files = files;
        self
    }

    pub fn traces(mut self, traces: Vec<TraceChip>) -> Self {
        self.traces = traces;
        self
    }

    pub fn held(mut self, held: bool) -> Self {
        self.held = held;
        self
    }

    pub fn menu(
        mut self,
        menu: Option<MentionMenu>,
        on_pick: impl Fn(usize, &mut Window, &mut App) + 'static,
    ) -> Self {
        let on_pick: OnIndex = Rc::new(on_pick);
        self.menu = menu.map(|menu| (menu, on_pick));
        self
    }

    pub fn queued(mut self, queued: Vec<SharedString>) -> Self {
        self.queued = queued;
        self
    }

    pub fn model(mut self, picker: impl IntoElement) -> Self {
        self.model = Some(picker.into_any_element());
        self
    }

    pub fn effort(mut self, picker: impl IntoElement) -> Self {
        self.effort = Some(picker.into_any_element());
        self
    }

    pub fn on_send(mut self, on_send: impl Fn(&str, &mut Window, &mut App) + 'static) -> Self {
        self.on_send = Some(Rc::new(on_send));
        self
    }

    pub fn on_stop(mut self, on_stop: impl Fn(&mut Window, &mut App) + 'static) -> Self {
        self.on_stop = Some(Rc::new(on_stop));
        self
    }

    pub fn on_attach(mut self, on_attach: impl Fn(&mut Window, &mut App) + 'static) -> Self {
        self.on_attach = Some(Rc::new(on_attach));
        self
    }

    pub fn on_remove(mut self, on_remove: impl Fn(usize, &mut Window, &mut App) + 'static) -> Self {
        self.on_remove = Some(Rc::new(on_remove));
        self
    }

    pub fn on_remove_trace(
        mut self,
        on_remove_trace: impl Fn(usize, &mut Window, &mut App) + 'static,
    ) -> Self {
        self.on_remove_trace = Some(Rc::new(on_remove_trace));
        self
    }

    pub fn on_unqueue(
        mut self,
        on_unqueue: impl Fn(usize, &mut Window, &mut App) + 'static,
    ) -> Self {
        self.on_unqueue = Some(Rc::new(on_unqueue));
        self
    }
}

impl RenderOnce for Composer {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let typed = !self.area.read(cx).text().trim().is_empty();
        let chips = !self.files.is_empty() || !self.traces.is_empty();
        let grown = match self.variant {
            ComposerVariant::Grows => typed || chips,
            ComposerVariant::OneLine => false,
            ComposerVariant::Grown => true,
        };
        let size = if grown { ATTACH } else { ATTACH_ONE_LINE };
        let attach = attach("composer-attach", size, self.on_attach, &theme);
        let action = send(
            &self.area,
            (typed || !self.traces.is_empty()) && !self.held,
            self.busy,
            self.on_send,
            self.on_stop,
            &theme,
        );
        let field = div()
            .flex_1()
            .min_w(px(0.0))
            .text_size(px(FONT_CHAT))
            .line_height(px(LINE_CHAT))
            .child(self.area);
        let pickers = div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(ROW_GAP))
            .children(self.model)
            .children(self.effort);
        let on_remove = self.on_remove;
        let on_remove_trace = self.on_remove_trace;
        let files = self.files.into_iter().enumerate().map(|(ix, name)| {
            let on_remove = on_remove.clone();
            file_chip(
                glyph(Glyph::File, ICON_SMALL, ink(&theme, T2)),
                name,
                &theme,
            )
            .child(
                remove_x(("composer-file-x", ix), ink(&theme, T3), &theme)
                    .when_some(on_remove, |x, on_remove| {
                        x.on_click(move |_, window, cx| on_remove(ix, window, cx))
                    }),
            )
        });
        let shown = self.traces.len().min(CHIPS_SHOWN);
        let mut traces: Vec<AnyElement> = self
            .traces
            .iter()
            .take(shown)
            .enumerate()
            .map(|(ix, chip)| {
                trace_chip("composer", ix, chip, &on_remove_trace, &theme, window, cx)
            })
            .collect();
        if self.traces.len() > CHIPS_SHOWN {
            traces.push(
                more_chips(&self.traces, &on_remove_trace, &theme, window, cx).into_any_element(),
            );
        }
        let shell = div()
            .flex()
            .rounded(px(RADIUS_COMPOSER))
            .bg(theme.color(ColorToken::FieldFill))
            .shadow(vec![ring(ink(&theme, COMPOSER_RING))])
            .text_size(px(FONT_TAB))
            .text_color(ink(&theme, T2));
        let shell = if grown {
            shell
                .flex_col()
                .gap(px(GROWN_GAP))
                .pt(px(COMPOSER_PAD_TOP))
                .pr(px(COMPOSER_PAD_RIGHT))
                .pb(px(COMPOSER_PAD_BOTTOM))
                .pl(px(COMPOSER_PAD_LEFT))
                .when(chips, |shell| {
                    shell.child(
                        div()
                            .flex()
                            .flex_wrap()
                            .gap(px(ROW_GAP))
                            .children(files)
                            .children(traces),
                    )
                })
                .child(field)
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(ROW_GAP))
                        .child(attach)
                        .child(div().flex_1())
                        .child(pickers)
                        .child(action),
                )
        } else {
            shell
                .items_center()
                .gap(px(ONE_LINE_GAP))
                .p(px(ONE_LINE_PAD))
                .child(attach)
                .child(field.pl(px(ONE_LINE_GAP / 2.0)))
                .child(pickers)
                .child(action)
        };
        let menu = self
            .menu
            .map(|(menu, on_pick)| mention_rows(&menu, &on_pick, &theme));
        let shell = Popover::new("composer-mention-menu", shell)
            .open(menu.is_some())
            .placement(Placement {
                side: Side::Top,
                align: Align::Start,
                offset: MENU_OFFSET,
            })
            .fit(MENU_MIN)
            .children(menu.map(|menu| {
                div()
                    .flex()
                    .flex_col()
                    .mx(px(MENU_PAD - POPOVER_PAD_X))
                    .my(px(MENU_PAD - POPOVER_PAD_Y))
                    .child(menu)
            }));
        let on_unqueue = self.on_unqueue;
        let waiting = self.queued.len();
        let queued =
            self.queued.into_iter().enumerate().map(|(ix, text)| {
                div()
                    .self_end()
                    .max_w(px(QUEUED_MAX))
                    .rounded(px(QUEUED_RADIUS))
                    .shadow(vec![ring(ink(&theme, QUEUED_RING))])
                    .pt(px(QUEUED_PAD_TOP))
                    .px(px(QUEUED_PAD_X))
                    .pb(px(QUEUED_PAD_BOTTOM))
                    .text_size(px(FONT_CHAT))
                    .line_height(px(LINE_CHAT))
                    .text_color(ink(&theme, T1))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap(px(WHO_GAP))
                            .text_size(px(FONT_WHO))
                            .line_height(px(LINE_WHO))
                            .text_color(ink(&theme, WHO_TEXT))
                            .child("You")
                            .child("queued")
                            .child(div().flex_1())
                            .child(
                                remove_x(("composer-unqueue", ix), ink(&theme, T3), &theme)
                                    .when_some(on_unqueue.clone(), |x, on_unqueue| {
                                        x.on_click(move |_, window, cx| on_unqueue(ix, window, cx))
                                    }),
                            ),
                    )
                    .child(text)
            });
        let status = self.busy.then(|| {
            div()
                .flex()
                .items_center()
                .gap(px(STATUS_GAP))
                .px(px(STATUS_PAD_X))
                .pb(px(STATUS_PAD_BOTTOM))
                .text_size(px(FONT_SMALL))
                .text_color(ink(&theme, T3))
                .child(spinner("composer-busy", &theme))
                .child(self.phase.unwrap_or_else(|| WORKING.into()))
                .child(div().flex_1())
                .when(waiting > 0, |status| {
                    status
                        .child(format!("the queue keeps {waiting} you typed"))
                        .when_some(on_unqueue.clone(), |status, on_unqueue| {
                            status.child(
                                div()
                                    .id("composer-take-back")
                                    .cursor_pointer()
                                    .text_color(ink(&theme, T2))
                                    .on_click(move |_, window, cx| {
                                        on_unqueue(waiting - 1, window, cx)
                                    })
                                    .child("take it back"),
                            )
                        })
                })
        });
        div()
            .id(self.id)
            .when_some(self.content_width, |column, width| {
                column.w_full().max_w(width).mx_auto()
            })
            .flex()
            .flex_col()
            .gap(px(ROW_GAP))
            .children(queued)
            .children(status)
            .child(shell)
    }
}

fn trace_chip(
    id: &'static str,
    ix: usize,
    chip: &TraceChip,
    on_remove: &Option<OnIndex>,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> AnyElement {
    let remove = on_remove.clone();
    let body = mention(
        (id, ix),
        chip.glyph,
        chip.label.clone(),
        chip.broken.is_some(),
        theme,
        move |_, window, cx| {
            if let Some(remove) = &remove {
                remove(ix, window, cx);
            }
        },
    );
    match &chip.broken {
        None => body.into_any_element(),
        Some(why) => tooltip(
            ElementId::Name(format!("{id}-tip-{ix}").into()),
            body.id(ElementId::Name(format!("{id}-chip-{ix}").into())),
            Edge::Frame,
            why.clone(),
            theme,
            window,
            cx,
        )
        .into_any_element(),
    }
}

fn more_chips(
    traces: &[TraceChip],
    on_remove: &Option<OnIndex>,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Popover {
    let open = window.use_keyed_state("composer-more-open", cx, |_, _| false);
    let shown = *open.read(cx);
    let flip = open.clone();
    let broken = traces
        .iter()
        .skip(CHIPS_SHOWN)
        .any(|chip| chip.broken.is_some());
    let mark = match broken {
        true => theme.color(ColorToken::StatusDanger),
        false => theme.color(ColorToken::MentionText),
    };
    let trigger = pressed(
        div()
            .id("composer-more")
            .flex()
            .flex_none()
            .items_center()
            .h(px(FCHIP))
            .px(px(FCHIP_PAD))
            .rounded(px(RADIUS_CHIP))
            .cursor_pointer()
            .bg(tint(mark, MENTION_TINT))
            .font_family(mono(theme))
            .text_size(px(FONT_SMALL))
            .font_weight(FontWeight::MEDIUM)
            .text_color(mark)
            .on_click(move |_, _, cx| {
                flip.update(cx, |open, cx| {
                    *open = !*open;
                    cx.notify();
                })
            })
            .child(format!("+{}", traces.len() - CHIPS_SHOWN)),
        mark,
    );
    let close = open.clone();
    let all = traces
        .iter()
        .enumerate()
        .map(|(ix, chip)| trace_chip("composer-all", ix, chip, on_remove, theme, window, cx));
    Popover::new("composer-more-menu", trigger)
        .open(shown)
        .placement(Placement {
            side: Side::Top,
            align: Align::Start,
            offset: MENU_OFFSET,
        })
        .fit(MENU_MIN)
        .child(
            div()
                .flex()
                .flex_col()
                .items_start()
                .gap(px(ROW_GAP))
                .on_mouse_down_out(move |_, _, cx| {
                    close.update(cx, |open, cx| {
                        *open = false;
                        cx.notify();
                    })
                })
                .children(all),
        )
}

fn mention_rows(menu: &MentionMenu, on_pick: &OnIndex, theme: &Theme) -> HoverList {
    let fill = ink(theme, HOVER);
    let corners = Corners::all(px(RADIUS_ROW));
    let row = |ix: usize, entry: &MentionRow| {
        let on_pick = on_pick.clone();
        div()
            .id(("composer-mention-row", ix))
            .flex()
            .items_center()
            .gap(px(ROW_INNER_GAP))
            .px(px(ROW_PAD_X))
            .py(px(ROW_PAD_Y))
            .rounded(px(RADIUS_ROW))
            .cursor_pointer()
            .text_color(ink(theme, T1))
            .hover(move |style| style.bg(fill))
            .on_mouse_down(MouseButton::Left, |_, window, _| window.prevent_default())
            .on_click(move |_, window, cx| on_pick(ix, window, cx))
            .child(glyph(
                entry.glyph,
                ICON_SMALL,
                theme.color(ColorToken::Trace),
            ))
            .child(
                div()
                    .flex_none()
                    .font_family(mono(theme))
                    .text_size(px(FONT_SMALL))
                    .child(entry.label.clone()),
            )
            .child(
                div()
                    .flex_none()
                    .pl(px(ROW_INNER_GAP))
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(theme, T3))
                    .child(entry.detail.clone()),
            )
    };
    let mut entries = 0;
    let mut marked = None;
    let mut list = HoverList::within(
        "composer-mentions",
        div().flex().flex_col(),
        fill,
        corners,
        theme,
    )
    .backdrop(tint(theme.color(ColorToken::ToastFill), MENU_SHADE));
    let mut first = 0;
    for group in &menu.groups {
        if group.rows.is_empty() {
            continue;
        }
        list = list.inert(caption(&group.title, theme));
        entries += 1;
        for (offset, entry) in group.rows.iter().enumerate() {
            if first + offset == menu.at {
                marked = Some(entries);
            }
            list = list.item(row(first + offset, entry));
            entries += 1;
        }
        first += group.rows.len();
    }
    if entries == 0 {
        list = list.inert(caption("Nothing matches", theme));
    }
    list.keyed(marked.map(|at| Marker {
        at,
        kind: GlideKind::Eased,
        fill,
        corners,
    }))
}

fn send(
    area: &Entity<TextArea>,
    typed: bool,
    busy: bool,
    on_send: Option<OnText>,
    on_stop: Option<OnAction>,
    theme: &Theme,
) -> Stateful<Div> {
    let lit = theme.color(ColorToken::ButtonPrimary);
    let mark = theme.color(ColorToken::ButtonPrimaryText);
    let (fill, mark) = if typed {
        (lit, mark)
    } else {
        (ink(theme, SEND_IDLE), tint(mark, SEND_IDLE_MARK))
    };
    let area = area.clone();
    circle("composer-send", "Send", SEND, fill)
        .child(glyph(Glyph::Send, ICON, mark))
        .on_click(move |_, window, cx| match (typed, &on_send, &on_stop) {
            (true, Some(on_send), _) => submit(&area, on_send, window, cx),
            (false, _, Some(on_stop)) if busy => on_stop(window, cx),
            _ => {}
        })
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SessionStatus {
    Running,
    Finished,
    Stopped,
}

pub struct HomeSession {
    pub name: SharedString,
    pub prompt: SharedString,
    pub when: SharedString,
    pub status: SessionStatus,
}

pub struct HomeProject {
    pub name: SharedString,
    pub path: SharedString,
    pub when: SharedString,
    pub branches: Vec<SharedString>,
    pub folders: Vec<(SharedString, SharedString)>,
    pub sessions: Vec<HomeSession>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum HomeMenu {
    Account,
    Project,
    Checkout,
    Branch,
    Resume,
    Model,
}

impl HomeMenu {
    fn id(self) -> &'static str {
        match self {
            HomeMenu::Account => "home-account",
            HomeMenu::Project => "home-project",
            HomeMenu::Checkout => "home-checkout",
            HomeMenu::Branch => "home-branch",
            HomeMenu::Resume => "home-resume",
            HomeMenu::Model => "home-model",
        }
    }
}

type OnShared = Rc<dyn Fn(SharedString, &mut Window, &mut App)>;

pub struct HomeComposer {
    area: Entity<TextArea>,
    account: SharedString,
    model: SharedString,
    effort: SharedString,
    projects: Vec<HomeProject>,
    current: usize,
    folder: usize,
    branch: usize,
    open: Option<HomeMenu>,
    opens: usize,
    hovered: Option<usize>,
    dismissed: Option<(HomeMenu, Point<Pixels>)>,
    on_send: Option<OnShared>,
}

impl HomeComposer {
    pub fn new(
        area: Entity<TextArea>,
        account: SharedString,
        model: SharedString,
        effort: SharedString,
        projects: Vec<HomeProject>,
        current: usize,
        cx: &mut App,
    ) -> Entity<HomeComposer> {
        cx.new(|cx| {
            cx.observe(&area, |_, _, cx| cx.notify()).detach();
            HomeComposer {
                area,
                account,
                model,
                effort,
                projects,
                current,
                folder: 0,
                branch: 0,
                open: None,
                opens: 0,
                hovered: None,
                dismissed: None,
                on_send: None,
            }
        })
    }

    pub fn on_send(&mut self, on_send: impl Fn(SharedString, &mut Window, &mut App) + 'static) {
        self.on_send = Some(Rc::new(on_send));
    }

    fn toggle(&mut self, menu: HomeMenu, at: Point<Pixels>, cx: &mut Context<Self>) {
        if self.dismissed.take() == Some((menu, at)) {
            return;
        }
        self.open = Some(menu);
        self.opens += 1;
        self.hovered = None;
        cx.notify();
    }

    fn dismiss(&mut self, at: Point<Pixels>, cx: &mut Context<Self>) {
        if let Some(menu) = self.open.take() {
            self.dismissed = Some((menu, at));
            cx.notify();
        }
    }

    fn close(&mut self, cx: &mut Context<Self>) {
        self.open = None;
        cx.notify();
    }

    fn send(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let text = self.area.read(cx).text();
        if text.trim().is_empty() {
            return;
        }
        if let Some(on_send) = self.on_send.clone() {
            on_send(text.into(), window, cx);
        }
        self.area.update(cx, |area, cx| area.clear(cx));
    }

    fn project(&self) -> Option<&HomeProject> {
        self.projects.get(self.current)
    }

    fn dropdown(
        &self,
        menu: HomeMenu,
        trigger: Stateful<Div>,
        align: Align,
        body: HoverList,
        cx: &mut Context<Self>,
    ) -> Popover {
        let trigger = trigger.on_mouse_down(
            MouseButton::Left,
            cx.listener(move |this, event: &MouseDownEvent, _, cx| {
                this.toggle(menu, event.position, cx)
            }),
        );
        Popover::new((menu.id(), self.opens), trigger)
            .open(self.open == Some(menu))
            .placement(Placement {
                side: Side::Bottom,
                align,
                offset: MENU_OFFSET,
            })
            .fit(MENU_MIN)
            .child(
                div()
                    .flex()
                    .flex_col()
                    .mx(px(MENU_PAD - POPOVER_PAD_X))
                    .my(px(MENU_PAD - POPOVER_PAD_Y))
                    .on_mouse_down_out(cx.listener(|this, event: &MouseDownEvent, _, cx| {
                        this.dismiss(event.position, cx)
                    }))
                    .child(body),
            )
    }

    fn sheet(&self, id: &'static str, chosen: usize, theme: &Theme) -> HoverList {
        let fill = ink(theme, HOVER);
        let corners = Corners::all(px(RADIUS_ROW));
        HoverList::within(
            (id, self.opens),
            div().flex().flex_col(),
            fill,
            corners,
            theme,
        )
        .backdrop(tint(theme.color(ColorToken::ToastFill), 1.0))
        .keyed(Some(Marker {
            at: self.hovered.unwrap_or(chosen),
            kind: GlideKind::Eased,
            fill,
            corners,
        }))
    }

    fn pick(
        &self,
        id: impl Into<ElementId>,
        at: usize,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Stateful<Div> {
        row(id, theme).on_mouse_move(cx.listener(move |this, _, _, cx| {
            if this.hovered != Some(at) {
                this.hovered = Some(at);
                cx.notify();
            }
        }))
    }

    fn account_menu(&self, theme: &Theme, cx: &mut Context<Self>) -> HoverList {
        self.sheet("home-accounts", 1, theme)
            .inert(caption("Accounts", theme))
            .item(
                self.pick("home-account-row", 1, theme, cx)
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.close(cx)))
                    .child(line_icon(CARD, ink(theme, T2)))
                    .child(div().flex_1().child(self.account.clone()))
                    .child(glyph(Glyph::Check, ICON_SMALL, ink(theme, T2))),
            )
            .inert(rule(theme))
            .item(
                self.pick("home-account-add", 3, theme, cx)
                    .text_color(ink(theme, T2))
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.close(cx)))
                    .child(icon(Icon::Plus, ICON_SMALL, ink(theme, T2)))
                    .child("Add an account"),
            )
    }

    fn project_menu(&self, theme: &Theme, cx: &mut Context<Self>) -> HoverList {
        let mut rows = Vec::with_capacity(self.projects.len());
        for (ix, project) in self.projects.iter().enumerate() {
            rows.push(
                self.pick(("home-project-row", ix), ix + 1, theme, cx)
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.current = ix;
                        this.folder = 0;
                        this.branch = 0;
                        this.close(cx);
                    }))
                    .child(tile(&project.name, theme))
                    .child(named_path(&project.name, &project.path, theme))
                    .child(when(&project.when, theme))
                    .when(ix == self.current, |row| {
                        row.child(glyph(Glyph::Check, ICON_SMALL, ink(theme, T2)))
                    }),
            );
        }
        self.sheet("home-projects", self.current + 1, theme)
            .inert(caption("Recent projects", theme))
            .items(rows)
            .inert(rule(theme))
            .item(
                self.pick("home-open-folder", self.projects.len() + 2, theme, cx)
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.close(cx)))
                    .child(line_icon(FOLDER, ink(theme, T2)))
                    .child("Open a folder"),
            )
    }

    fn checkout_menu(&self, theme: &Theme, cx: &mut Context<Self>) -> HoverList {
        let folders = self
            .project()
            .map_or(&[][..], |project| &project.folders[..]);
        let mut rows = Vec::with_capacity(folders.len());
        for (ix, (name, path)) in folders.iter().enumerate() {
            rows.push(
                self.pick(("home-folder-row", ix), ix + 1, theme, cx)
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.folder = ix;
                        this.close(cx);
                    }))
                    .child(line_icon(FOLDER, ink(theme, T2)))
                    .child(named_path(name, path, theme))
                    .when(ix == self.folder, |row| {
                        row.child(glyph(Glyph::Check, ICON_SMALL, ink(theme, T2)))
                    }),
            );
        }
        self.sheet("home-folders", self.folder + 1, theme)
            .inert(caption("Work in", theme))
            .items(rows)
            .inert(rule(theme))
            .item(
                self.pick("home-new-worktree", folders.len() + 2, theme, cx)
                    .text_color(ink(theme, T2))
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.close(cx)))
                    .child(icon(Icon::Plus, ICON_SMALL, ink(theme, T2)))
                    .child("New worktree"),
            )
    }

    fn branch_menu(&self, theme: &Theme, cx: &mut Context<Self>) -> HoverList {
        let branches = self
            .project()
            .map_or(&[][..], |project| &project.branches[..]);
        let mut rows = Vec::with_capacity(branches.len());
        for (ix, branch) in branches.iter().enumerate() {
            rows.push(
                self.pick(("home-branch-row", ix), ix + 1, theme, cx)
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.branch = ix;
                        this.close(cx);
                    }))
                    .child(icon(Icon::Branch, ICON_SMALL, ink(theme, T2)))
                    .child(div().flex_1().child(branch.clone()))
                    .when(ix == self.branch, |row| {
                        row.child(glyph(Glyph::Check, ICON_SMALL, ink(theme, T2)))
                    }),
            );
        }
        self.sheet("home-branches", self.branch + 1, theme)
            .inert(caption("Branches", theme))
            .items(rows)
    }

    fn resume_menu(&self, theme: &Theme, cx: &mut Context<Self>) -> HoverList {
        let (name, sessions) = self
            .project()
            .map_or((SharedString::default(), &[][..]), |project| {
                (project.name.clone(), &project.sessions[..])
            });
        let accent = theme.color(ColorToken::StatusAccent);
        let mut rows = Vec::with_capacity(sessions.len());
        for (ix, session) in sessions.iter().enumerate() {
            let dot = match session.status {
                SessionStatus::Running => theme.color(ColorToken::StatusLive),
                SessionStatus::Finished => ink(theme, FINISHED_DOT),
                SessionStatus::Stopped => theme.color(ColorToken::StatusWarn),
            };
            let latest = (ix == 0).then(|| {
                div()
                    .flex_none()
                    .px(px(LATEST_PAD_X))
                    .py(px(LATEST_PAD_Y))
                    .rounded(px(RADIUS_BADGE))
                    .bg(tint(accent, LATEST_FILL))
                    .text_size(px(FONT_BADGE))
                    .line_height(px(LINE_LATEST))
                    .text_color(mix(accent, ink(theme, 1.0), LATEST_LIFT))
                    .child(LATEST)
            });
            rows.push(
                self.pick(("home-session-row", ix), ix + 1, theme, cx)
                    .items_start()
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.close(cx)))
                    .child(
                        div()
                            .flex_none()
                            .mt(px(DOT_TOP))
                            .size(px(DOT))
                            .rounded_full()
                            .bg(dot),
                    )
                    .child(
                        div()
                            .flex_1()
                            .min_w(px(0.0))
                            .line_height(px(LINE_WHO))
                            .child(
                                div()
                                    .flex()
                                    .items_center()
                                    .gap(px(NAME_GAP))
                                    .child(session.name.clone())
                                    .children(latest)
                                    .child(div().flex_1())
                                    .child(when(&session.when, theme)),
                            )
                            .child(
                                div()
                                    .text_size(px(FONT_SMALL))
                                    .text_color(ink(theme, T3))
                                    .truncate()
                                    .child(session.prompt.clone()),
                            ),
                    ),
            );
        }
        self.sheet("home-sessions", 1, theme)
            .inert(caption(&format!("Sessions in {name}"), theme))
            .items(rows)
            .inert(rule(theme))
            .item(
                self.pick("home-all-sessions", sessions.len() + 2, theme, cx)
                    .text_color(ink(theme, T2))
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.close(cx)))
                    .child(div().flex_1().child("All sessions"))
                    .child(kbd(ALL_SESSIONS_KEYS, theme)),
            )
    }

    fn model_menu(&self, theme: &Theme, cx: &mut Context<Self>) -> HoverList {
        let mut efforts = Vec::with_capacity(EFFORTS.len());
        for (ix, &effort) in EFFORTS.iter().enumerate() {
            efforts.push(
                self.pick(("home-effort-row", ix), ix + 3, theme, cx)
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.effort = effort.into();
                        this.close(cx);
                    }))
                    .child(div().flex_1().child(effort))
                    .when(self.effort == effort, |row| {
                        row.child(glyph(Glyph::Check, ICON_SMALL, ink(theme, T2)))
                    }),
            );
        }
        self.sheet("home-models", 1, theme)
            .inert(caption("Model", theme))
            .item(
                self.pick("home-model-row", 1, theme, cx)
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.close(cx)))
                    .child(div().flex_1().child(self.model.clone()))
                    .child(glyph(Glyph::Check, ICON_SMALL, ink(theme, T2))),
            )
            .inert(caption("Effort", theme))
            .items(efforts)
    }
}

fn row(id: impl Into<ElementId>, theme: &Theme) -> Stateful<Div> {
    div()
        .id(id)
        .flex()
        .items_center()
        .gap(px(ROW_INNER_GAP))
        .px(px(ROW_PAD_X))
        .py(px(ROW_PAD_Y))
        .rounded(px(RADIUS_ROW))
        .cursor_pointer()
        .text_color(ink(theme, T1))
}

fn caption(text: &str, theme: &Theme) -> Div {
    div()
        .px(px(ROW_PAD_X))
        .py(px(CAPTION_PAD_Y))
        .text_size(px(FONT_CAP2))
        .line_height(px(FONT_CAP2))
        .letter_spacing(px(CAPTION_TRACK))
        .font_weight(FontWeight::SEMIBOLD)
        .text_color(ink(theme, CAPTION_TEXT))
        .child(text.to_uppercase())
}

fn rule(theme: &Theme) -> Div {
    div()
        .h(px(HAIRLINE))
        .mx(px(RULE_X))
        .my(px(RULE_Y))
        .bg(ink(theme, RULE))
}

fn when(text: &SharedString, theme: &Theme) -> Div {
    div()
        .flex_none()
        .text_size(px(FONT_WHO))
        .text_color(ink(theme, T3))
        .child(text.clone())
}

fn tile(name: &str, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(TILE))
        .rounded(px(TILE_RADIUS))
        .bg(ink(theme, TILE_FILL))
        .text_size(px(FONT_TILE))
        .line_height(px(FONT_TILE))
        .font_weight(FontWeight::SEMIBOLD)
        .child(name.chars().take(1).collect::<String>())
}

fn named_path(name: &SharedString, path: &SharedString, theme: &Theme) -> Div {
    div()
        .flex_1()
        .min_w(px(0.0))
        .line_height(px(LINE_PATH))
        .child(name.clone())
        .child(
            div()
                .truncate()
                .font_family(mono(theme))
                .text_size(px(FONT_PATH))
                .text_color(ink(theme, T3))
                .child(path.clone()),
        )
}

fn mix(from: Rgba, to: Rgba, share: f32) -> Rgba {
    Rgba::new(
        from.red + (to.red - from.red) * share,
        from.green + (to.green - from.green) * share,
        from.blue + (to.blue - from.blue) * share,
        from.alpha,
    )
}

fn line_icon(body: &str, color: Rgba) -> Img {
    let byte = |channel: f32| (channel * 255.0).round() as u8;
    let side = ICON_SMALL * ICON_RASTER;
    let svg = format!(
        r##"<svg xmlns="http://www.w3.org/2000/svg" width="{side}" height="{side}" viewBox="0 0 16 16" fill="none" stroke="#{:02x}{:02x}{:02x}" stroke-opacity="{}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">{body}</svg>"##,
        byte(color.red),
        byte(color.green),
        byte(color.blue),
        color.alpha,
    );
    img(Arc::new(Image::from_bytes(
        ImageFormat::Svg,
        svg.into_bytes(),
    )))
    .size(px(ICON_SMALL))
    .flex_none()
}

fn link(
    id: &'static str,
    lead: impl IntoElement,
    label: SharedString,
    detail: Option<SharedString>,
    open: bool,
    theme: &Theme,
) -> Stateful<Div> {
    let color = ink(theme, LINK_TEXT);
    let lit = ink(theme, T1);
    div()
        .id(id)
        .flex()
        .flex_none()
        .items_center()
        .gap(px(LINK_INNER_GAP))
        .cursor_pointer()
        .text_size(px(FONT_TAB))
        .text_color(if open { lit } else { color })
        .hover(move |style| style.text_color(lit))
        .child(lead)
        .child(label)
        .children(detail.map(|detail| {
            div()
                .text_color(ink(theme, T3))
                .child(SharedString::from(format!("\u{b7} {detail}")))
        }))
        .child(glyph(
            Glyph::Chevron,
            ICON_SMALL,
            ink(theme, LINK_TEXT * CHEVRON),
        ))
}

fn model_chip(
    model: SharedString,
    effort: SharedString,
    open: bool,
    theme: &Theme,
) -> Stateful<Div> {
    let hover = ink(theme, HOVER);
    div()
        .id("home-model-chip")
        .flex()
        .flex_none()
        .items_center()
        .gap(px(LINK_INNER_GAP))
        .h(px(CHIP))
        .px(px(CHIP_PAD))
        .rounded(px(RADIUS_CHIP))
        .cursor_pointer()
        .text_size(px(FONT_TAB))
        .font_weight(FontWeight::MEDIUM)
        .text_color(ink(theme, CHIP_TEXT))
        .when(open, |chip| chip.bg(hover))
        .hover(move |style| style.bg(hover))
        .child(model)
        .child(div().text_color(ink(theme, T3)).child(effort))
        .child(glyph(Glyph::Chevron, ICON_SMALL, ink(theme, CHEVRON)))
}

impl Render for HomeComposer {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let fill = tint(theme.color(ColorToken::ToastFill), HOME_FILL);
        let shadow = drop(
            tint(gpui::rgba(0x000000ff), HOME_DROP),
            HOME_DROP_Y,
            HOME_DROP_BLUR,
        );
        let project = self.project();
        let name = project.map(|project| project.name.clone());
        let folder = project
            .and_then(|project| project.folders.get(self.folder))
            .map(|(folder, _)| folder.clone());
        let branch = project.and_then(|project| project.branches.get(self.branch).cloned());
        let latest = project
            .and_then(|project| project.sessions.first())
            .map(|session| session.name.clone());
        let color = ink(&theme, LINK_TEXT);
        let open = self.open;
        let account = self.dropdown(
            HomeMenu::Account,
            link(
                "home-account-trigger",
                line_icon(CARD, color),
                self.account.clone(),
                None,
                open == Some(HomeMenu::Account),
                &theme,
            ),
            Align::End,
            self.account_menu(&theme, cx),
            cx,
        );
        let project = self.dropdown(
            HomeMenu::Project,
            link(
                "home-project-trigger",
                line_icon(FOLDER, color),
                name.unwrap_or_default(),
                None,
                open == Some(HomeMenu::Project),
                &theme,
            ),
            Align::End,
            self.project_menu(&theme, cx),
            cx,
        );
        let checkout = self.dropdown(
            HomeMenu::Checkout,
            link(
                "home-checkout-trigger",
                line_icon(FOLDER, color),
                folder.unwrap_or_default(),
                None,
                open == Some(HomeMenu::Checkout),
                &theme,
            ),
            Align::Start,
            self.checkout_menu(&theme, cx),
            cx,
        );
        let branch = self.dropdown(
            HomeMenu::Branch,
            link(
                "home-branch-trigger",
                icon(Icon::Branch, ICON_SMALL, color),
                branch.unwrap_or_default(),
                None,
                open == Some(HomeMenu::Branch),
                &theme,
            ),
            Align::Start,
            self.branch_menu(&theme, cx),
            cx,
        );
        let resume = self.dropdown(
            HomeMenu::Resume,
            link(
                "home-resume-trigger",
                line_icon(CLOCK, color),
                RESUME.into(),
                latest,
                open == Some(HomeMenu::Resume),
                &theme,
            ),
            Align::End,
            self.resume_menu(&theme, cx),
            cx,
        );
        let model = self.dropdown(
            HomeMenu::Model,
            model_chip(
                self.model.clone(),
                self.effort.clone(),
                open == Some(HomeMenu::Model),
                &theme,
            ),
            Align::End,
            self.model_menu(&theme, cx),
            cx,
        );
        let send = circle(
            "home-send",
            "Send",
            HOME_SEND,
            tint(theme.color(ColorToken::ButtonPrimary), HOME_SEND_FILL),
        )
        .child(glyph(
            Glyph::Send,
            ICON,
            theme.color(ColorToken::ButtonPrimaryText),
        ))
        .on_click(cx.listener(|this, _: &ClickEvent, window, cx| this.send(window, cx)));
        let surface = div()
            .flex()
            .flex_col()
            .gap(px(HOME_GAP))
            .rounded(px(HOME_RADIUS))
            .bg(fill)
            .backdrop_blur(px(HOME_BLUR))
            .shadow(vec![ring(ink(&theme, HOME_RING)), shadow])
            .pt(px(HOME_PAD_TOP))
            .pr(px(HOME_PAD_RIGHT))
            .pb(px(HOME_PAD_BOTTOM))
            .pl(px(HOME_PAD_LEFT))
            .child(
                div()
                    .text_size(px(HOME_FONT))
                    .line_height(px(LINE_CHAT))
                    .child(self.area.clone()),
            )
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(HOME_ROW_GAP))
                    .text_size(px(FONT_TAB))
                    .child(attach("home-attach", ATTACH, None, &theme))
                    .child(div().flex_1())
                    .child(model)
                    .child(send),
            );
        div()
            .flex()
            .flex_col()
            .gap(px(HOME_STACK_GAP))
            .child(
                div()
                    .flex()
                    .justify_end()
                    .gap(px(LINK_GAP))
                    .child(account)
                    .child(project),
            )
            .child(surface)
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(LINK_GAP))
                    .px(px(UNDER_PAD_X))
                    .child(checkout)
                    .child(branch)
                    .child(div().flex_1())
                    .child(resume),
            )
    }
}
