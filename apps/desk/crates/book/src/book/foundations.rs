use std::time::{Duration, Instant};

use desk_motion::{Presence, reduced_motion, set_reduced_motion, tokens};
use desk_ui::component::{icon, icon_button};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{Header, dots, header_action, inner_card, shell};
use desk_ui::components::form::{TextInput, switch};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::{MenuItem, menu};
use desk_ui::components::paint::{glyph, ink, ms};
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::size::{
    CAPTION_TEXT, CHIP_TEXT, DIM_TEXT, FONT_BADGE, FONT_BODY, FONT_CAP, FONT_CAP2, FONT_CHAT,
    FONT_KBD, FONT_SMALL, FONT_TAB, FONT_TITLE, FONT_TREE, FONT_WHO, FOOT_TEXT, NUMBER_TEXT,
    RADIUS_POP, RADIUS_ROW, SHELL_TEXT, T1, T2, T3,
};
use desk_ui::icon::Icon;
use desk_ui::metrics::{ICON, ICON_SMALL, ICON_TINY};
use desk_ui::theme::{ColorToken, NumberToken, Theme};
use gpui::{
    App, ClickEvent, Context, Div, Entity, MouseButton, MouseDownEvent, MouseMoveEvent,
    MouseUpEvent, SharedString, SpringConfig, SpringState, Stateful, Window, deferred, div,
    prelude::*, px,
};

use super::Book;
use super::kit::{TILE_HEIGHT, block, label, named, present, spread, titled, toggle};
use super::lists::{CONNECTED_ROOM, Strip};
use crate::catalog::Page;

const SAMPLE: &str = "The lead asked go-dev 2 to run the tests.";
const MENU_TILE: f32 = TILE_HEIGHT / 2.0;
const SCROLL_ROWS: usize = 40;
const MENU_DROP: f32 = 40.0;
const MOTION_MENU: [(&str, Option<&str>); 2] = [("Rename", Some("F2")), ("Pin", None)];

const LANE: f32 = 320.0;
const LANE_HEIGHT: f32 = 28.0;
const DOT: f32 = 16.0;
const DOT_INSET: f32 = (LANE_HEIGHT - DOT) / 2.0;
const DOT_END: f32 = LANE - DOT - 2.0 * DOT_INSET;
const RISE: f32 = 8.0;
const STAGGER_GAP: Duration = Duration::from_millis(40);
const STAGGER_WIDTH: f32 = 240.0;
const STAGGER_ROWS: [&str; 5] = [
    "Read main.rs",
    "Edit lib.rs",
    "Run cargo check",
    "Read the diff",
    "Reply to the lead",
];
const CHIP_NAMES: [&str; 5] = ["main.rs", "lib.rs", "mod.rs", "kit.rs", "app.rs"];
const CHIP_WIDTH: f32 = 56.0;
const CHIP_PITCH: f32 = 64.0;
const CHIP_HEIGHT: f32 = 24.0;
const CHIP_INSET: f32 = (LANE_HEIGHT - CHIP_HEIGHT) / 2.0;
const PLACE_WIDTH: f32 = 148.0;
const PLACE_HEIGHT: f32 = 72.0;
const PLACE_GAP: f32 = 24.0;
const SHARED_WIDTH: f32 = 96.0;
const SHARED_HEIGHT: f32 = 28.0;
const SHARED_LEFT: f32 = (PLACE_WIDTH - SHARED_WIDTH) / 2.0;
const SHARED_RIGHT: f32 = SHARED_LEFT + PLACE_WIDTH + PLACE_GAP;
const SHARED_TOP: f32 = (PLACE_HEIGHT - SHARED_HEIGHT) / 2.0;
const PUCK: f32 = 24.0;
const PUCK_INSET: f32 = (LANE_HEIGHT - PUCK) / 2.0;
const PUCK_END: f32 = LANE - PUCK - 2.0 * PUCK_INSET;
const THROW_LOOKAHEAD: f32 = 0.2;
const THROW_SPEED: f32 = 1800.0;
const SAMPLE_GAP: Duration = Duration::from_millis(8);
const STALE_SAMPLE: Duration = Duration::from_millis(80);
const SETTLED_PX: f32 = 0.1;

const EASED: Curve = Curve::Eased(tokens::GLIDE_MS);
const SPRUNG: Curve = Curve::Spring(tokens::PANEL);
const ENTER: Curve = Curve::Eased(tokens::PANEL_IN_MS);
const EXIT: Curve = Curve::Eased(tokens::PANEL_OUT_MS);
const THROWN: Curve = Curve::Spring(tokens::TOGGLE);
const FADE: Curve = Curve::Eased(tokens::TOGGLE_MS);

const FONTS: [(&str, f32); 11] = [
    ("FONT_CAP2", FONT_CAP2),
    ("FONT_BADGE", FONT_BADGE),
    ("FONT_KBD", FONT_KBD),
    ("FONT_CAP", FONT_CAP),
    ("FONT_WHO", FONT_WHO),
    ("FONT_SMALL", FONT_SMALL),
    ("FONT_TAB", FONT_TAB),
    ("FONT_BODY", FONT_BODY),
    ("FONT_TREE", FONT_TREE),
    ("FONT_CHAT", FONT_CHAT),
    ("FONT_TITLE", FONT_TITLE),
];

const INKS: [(&str, f32); 9] = [
    ("T1", T1),
    ("CHIP_TEXT", CHIP_TEXT),
    ("T2", T2),
    ("SHELL_TEXT", SHELL_TEXT),
    ("DIM_TEXT", DIM_TEXT),
    ("CAPTION_TEXT", CAPTION_TEXT),
    ("T3", T3),
    ("FOOT_TEXT", FOOT_TEXT),
    ("NUMBER_TEXT", NUMBER_TEXT),
];

const ICONS: [(Icon, &str); 10] = [
    (Icon::Sidebar, "sidebar"),
    (Icon::Plus, "plus"),
    (Icon::Search, "search"),
    (Icon::Bell, "bell"),
    (Icon::Minimize, "minimize"),
    (Icon::Maximize, "maximize"),
    (Icon::Restore, "restore"),
    (Icon::Close, "close"),
    (Icon::Branch, "branch"),
    (Icon::Arrow, "arrow"),
];

const GLYPHS: [(Glyph, &str); 14] = [
    (Glyph::Pin, "pin"),
    (Glyph::Lock, "lock"),
    (Glyph::Check, "check"),
    (Glyph::Trace, "trace"),
    (Glyph::Chevron, "chevron"),
    (Glyph::Attach, "attach"),
    (Glyph::Send, "send"),
    (Glyph::File, "file"),
    (Glyph::Terminal, "terminal"),
    (Glyph::Chat, "chat"),
    (Glyph::Agents, "agents"),
    (Glyph::Pencil, "pencil"),
    (Glyph::Window, "window"),
    (Glyph::Cron, "cron"),
];

fn type_scale(sample: &Entity<TextInput>, theme: &Theme, cx: &mut Context<Book>) -> Div {
    let typed = sample.read(cx).text();
    let shown = SharedString::from(if typed.is_empty() {
        SAMPLE.to_owned()
    } else {
        typed.to_owned()
    });
    let fonts = FONTS.map(|(name, size)| {
        spread(theme)
            .child(label(format!("{name} {size}"), theme).w(px(140.0)))
            .child(div().text_size(px(size)).child(shown.clone()))
    });
    let inks = INKS.map(|(name, alpha)| {
        spread(theme)
            .child(label(format!("{name} {alpha}"), theme).w(px(140.0)))
            .child(div().text_color(ink(theme, alpha)).child(shown.clone()))
    });
    div()
        .flex()
        .flex_col()
        .gap_3()
        .child(block(
            "Type a sample: every line below shows it",
            theme,
            sample.clone(),
        ))
        .child(block(
            "Font sizes",
            theme,
            div().flex().flex_col().gap_2().children(fonts),
        ))
        .child(block(
            "Text alpha over text.strong",
            theme,
            div().flex().flex_col().gap_2().children(inks),
        ))
}

fn icons(theme: &Theme, cx: &mut Context<Book>) -> Div {
    let color = theme.color(ColorToken::TextIcon);
    let lines = ICONS.map(|(shown, name)| {
        named(
            name,
            theme,
            div()
                .flex()
                .items_center()
                .gap_2()
                .child(icon(shown, ICON_SMALL, color))
                .child(icon(shown, ICON, color))
                .child(icon_button(name, shown, name, theme).on_click(Book::clicked(name, cx))),
        )
    });
    let glyphs = GLYPHS.map(|(shown, name)| {
        named(
            name,
            theme,
            div()
                .flex()
                .items_center()
                .gap_2()
                .child(glyph(shown, ICON_TINY, color))
                .child(glyph(shown, ICON_SMALL, color))
                .child(glyph(shown, ICON, color)),
        )
    });
    div()
        .flex()
        .flex_col()
        .gap_3()
        .child(block(
            "Icon: 13, 16 and as an icon button (hover it, click it)",
            theme,
            spread(theme).gap_6().children(lines),
        ))
        .child(block(
            "Glyph: 11, 13 and 16",
            theme,
            spread(theme).gap_6().children(glyphs),
        ))
}

#[derive(Clone, Copy)]
enum Curve {
    Eased(Duration),
    Spring(SpringConfig),
}

#[derive(Clone, Copy)]
struct Track {
    curve: Curve,
    from: SpringState,
    to: f32,
    since: Instant,
}

impl Track {
    fn run(curve: Curve, from: f32, to: f32, since: Instant) -> Self {
        Track {
            curve,
            from: SpringState {
                position: from,
                velocity: 0.0,
            },
            to,
            since,
        }
    }

    fn rest(curve: Curve, at: f32, now: Instant) -> Self {
        Self::run(curve, at, at, now)
    }

    fn state(&self, now: Instant) -> SpringState {
        let elapsed = now.saturating_duration_since(self.since).as_secs_f32();
        match self.curve {
            Curve::Spring(config) => config.step(self.from, self.to, elapsed),
            Curve::Eased(span) => {
                let done = (elapsed / span.as_secs_f32()).min(1.0);
                SpringState {
                    position: self.from.position
                        + (self.to - self.from.position) * tokens::EASE_OUT(done),
                    velocity: 0.0,
                }
            }
        }
    }

    fn value(&self, now: Instant) -> f32 {
        self.state(now).position
    }

    fn placed(&self, now: Instant, reduced: bool) -> f32 {
        if reduced { self.to } else { self.value(now) }
    }

    fn moving(&self, now: Instant) -> bool {
        match self.curve {
            Curve::Spring(config) => !config.is_settled(self.state(now), self.to, SETTLED_PX),
            Curve::Eased(span) => now.saturating_duration_since(self.since) < span,
        }
    }

    fn go(&mut self, to: f32, now: Instant) {
        self.throw(self.state(now), to, now);
    }

    fn go_by(&mut self, curve: Curve, to: f32, now: Instant) {
        self.go(to, now);
        self.curve = curve;
    }

    fn throw(&mut self, from: SpringState, to: f32, now: Instant) {
        self.from = from;
        self.to = to;
        self.since = now;
    }
}

#[derive(Clone, Copy)]
enum Demo {
    Curves,
    Presence,
    Stagger,
    Reflow,
    Shared,
    Throw,
    Reduced,
}

impl Demo {
    const ALL: [Demo; 7] = [
        Demo::Curves,
        Demo::Presence,
        Demo::Stagger,
        Demo::Reflow,
        Demo::Shared,
        Demo::Throw,
        Demo::Reduced,
    ];

    fn id(self) -> &'static str {
        match self {
            Demo::Curves => "replay-curves",
            Demo::Presence => "replay-presence",
            Demo::Stagger => "replay-stagger",
            Demo::Reflow => "replay-reflow",
            Demo::Shared => "replay-shared",
            Demo::Throw => "replay-throw",
            Demo::Reduced => "replay-reduced",
        }
    }
}

struct Chip {
    name: usize,
    x: Track,
    shown: Track,
}

struct Grab {
    pointer: f32,
    from: f32,
    last: f32,
    sampled: Instant,
    velocity: f32,
}

struct MotionLab {
    eased: Track,
    sprung: Track,
    menu: Presence,
    menu_pick: Option<&'static str>,
    stagger: [Track; STAGGER_ROWS.len()],
    chips: Vec<Chip>,
    shared: Track,
    puck: Track,
    grab: Option<Grab>,
    slide: Track,
    fade: Track,
}

impl MotionLab {
    fn new(menu: Presence, now: Instant) -> Self {
        let mut x = 0.0;
        let chips = (0..CHIP_NAMES.len())
            .map(|name| {
                let chip = Chip {
                    name,
                    x: Track::rest(SPRUNG, x, now),
                    shown: Track::rest(ENTER, 1.0, now),
                };
                x += CHIP_PITCH;
                chip
            })
            .collect();
        MotionLab {
            eased: Track::rest(EASED, 0.0, now),
            sprung: Track::rest(SPRUNG, 0.0, now),
            menu,
            menu_pick: None,
            stagger: [Track::rest(ENTER, 1.0, now); STAGGER_ROWS.len()],
            chips,
            shared: Track::rest(SPRUNG, SHARED_LEFT, now),
            puck: Track::rest(THROWN, 0.0, now),
            grab: None,
            slide: Track::rest(SPRUNG, DOT_END, now),
            fade: Track::rest(FADE, 1.0, now),
        }
    }

    fn moving(&self, now: Instant, reduced: bool) -> bool {
        let fading = self
            .stagger
            .iter()
            .chain(self.chips.iter().map(|chip| &chip.shown))
            .chain([&self.fade])
            .any(|track| track.moving(now));
        let travelling = [
            &self.eased,
            &self.sprung,
            &self.shared,
            &self.puck,
            &self.slide,
        ]
        .into_iter()
        .chain(self.chips.iter().map(|chip| &chip.x))
        .any(|track| track.moving(now));
        fading || (!reduced && travelling)
    }

    fn replay(&mut self, demo: Demo, now: Instant) {
        match demo {
            Demo::Curves => {
                let to = if self.eased.to > 0.0 { 0.0 } else { DOT_END };
                self.eased.go(to, now);
                self.sprung.go(to, now);
            }
            Demo::Presence => toggle(&mut self.menu),
            Demo::Stagger => {
                let mut since = now;
                for row in &mut self.stagger {
                    *row = Track::run(ENTER, 0.0, 1.0, since);
                    since += STAGGER_GAP;
                }
            }
            Demo::Reflow => self.restore(now),
            Demo::Shared => {
                let to = if self.shared.to > SHARED_LEFT {
                    SHARED_LEFT
                } else {
                    SHARED_RIGHT
                };
                self.shared.go(to, now);
            }
            Demo::Throw => {
                let to = if self.puck.to > 0.0 { 0.0 } else { PUCK_END };
                let position = self.puck.value(now);
                let velocity = if to > position {
                    THROW_SPEED
                } else {
                    -THROW_SPEED
                };
                self.grab = None;
                self.puck.throw(SpringState { position, velocity }, to, now);
            }
            Demo::Reduced => {
                self.slide = Track::run(SPRUNG, 0.0, DOT_END, now);
                self.fade = Track::run(FADE, 0.0, 1.0, now);
            }
        }
    }

    fn restore(&mut self, now: Instant) {
        let mut x = 0.0;
        for name in 0..CHIP_NAMES.len() {
            match self.chips.iter_mut().find(|chip| chip.name == name) {
                Some(chip) => chip.shown.go_by(ENTER, 1.0, now),
                None => self.chips.push(Chip {
                    name,
                    x: Track::rest(SPRUNG, x, now),
                    shown: Track::run(ENTER, 0.0, 1.0, now),
                }),
            }
            x += CHIP_PITCH;
        }
        self.chips.sort_by_key(|chip| chip.name);
        self.reflow(now);
    }

    fn remove(&mut self, name: usize, now: Instant) {
        let Some(chip) = self
            .chips
            .iter_mut()
            .find(|chip| chip.name == name && chip.shown.to > 0.0)
        else {
            return;
        };
        chip.shown.go_by(EXIT, 0.0, now);
        self.reflow(now);
    }

    fn reflow(&mut self, now: Instant) {
        let mut x = 0.0;
        for chip in self.chips.iter_mut().filter(|chip| chip.shown.to > 0.0) {
            chip.x.go(x, now);
            x += CHIP_PITCH;
        }
    }

    fn prune(&mut self, now: Instant) {
        self.chips
            .retain(|chip| chip.shown.to > 0.0 || chip.shown.moving(now));
    }

    fn grab(&mut self, pointer: f32, now: Instant) {
        let at = self.puck.value(now).clamp(0.0, PUCK_END);
        self.puck = Track::rest(THROWN, at, now);
        self.grab = Some(Grab {
            pointer,
            from: at,
            last: at,
            sampled: now,
            velocity: 0.0,
        });
    }

    fn drag(&mut self, pointer: f32, now: Instant) {
        let Some(grab) = &mut self.grab else {
            return;
        };
        let at = (grab.from + pointer - grab.pointer).clamp(0.0, PUCK_END);
        let since = now.saturating_duration_since(grab.sampled);
        if since >= SAMPLE_GAP {
            grab.velocity = (at - grab.last) / since.as_secs_f32();
            grab.last = at;
            grab.sampled = now;
        }
        self.puck = Track::rest(THROWN, at, now);
    }

    fn release(&mut self, now: Instant) {
        let Some(grab) = self.grab.take() else {
            return;
        };
        let position = self.puck.value(now);
        let velocity = if now.saturating_duration_since(grab.sampled) > STALE_SAMPLE {
            0.0
        } else {
            grab.velocity
        };
        let projected = position + velocity * THROW_LOOKAHEAD;
        let to = if projected > PUCK_END / 2.0 {
            PUCK_END
        } else {
            0.0
        };
        self.puck.throw(SpringState { position, velocity }, to, now);
    }
}

fn spring_text(config: SpringConfig) -> String {
    format!(
        "stiffness {}, damping {}, mass {}",
        config.stiffness, config.damping, config.mass
    )
}

fn lane(theme: &Theme) -> Div {
    div()
        .relative()
        .flex_none()
        .w(px(LANE))
        .h(px(LANE_HEIGHT))
        .rounded(px(RADIUS_ROW))
        .bg(theme.color(ColorToken::SegmentedFill))
}

fn dot(x: f32, theme: &Theme) -> Div {
    div()
        .absolute()
        .top(px(DOT_INSET))
        .left(px(DOT_INSET + x))
        .size(px(DOT))
        .rounded_full()
        .bg(theme.color(ColorToken::StatusAccent))
}

fn replay_button(demo: Demo, theme: &Theme, cx: &mut Context<Book>) -> Stateful<Div> {
    button(demo.id(), "Replay", None, ButtonKind::Plain, theme).on_click(cx.listener(
        move |this, _: &ClickEvent, _, cx| {
            this.foundations.lab.replay(demo, Instant::now());
            cx.notify();
        },
    ))
}

fn demo(
    title: &'static str,
    tokens: String,
    replay: Demo,
    stage: impl IntoElement,
    theme: &Theme,
    cx: &mut Context<Book>,
) -> Div {
    block(
        title,
        theme,
        div()
            .flex()
            .flex_col()
            .gap_2()
            .child(label(tokens, theme))
            .child(stage)
            .child(div().flex().child(replay_button(replay, theme, cx))),
    )
}

fn released(cx: &mut Context<Book>) -> impl Fn(&MouseUpEvent, &mut Window, &mut App) + 'static {
    cx.listener(|this, _: &MouseUpEvent, _, cx| {
        this.foundations.lab.release(Instant::now());
        cx.notify();
    })
}

pub(super) struct FoundationsState {
    lab: MotionLab,
    surface: Strip,
    sample: Entity<TextInput>,
}

impl FoundationsState {
    pub(super) fn new(presence: Presence, window: &mut Window, cx: &mut Context<Book>) -> Self {
        let sample = TextInput::new(SAMPLE.into(), window, cx);
        cx.observe(&sample, |_, _, cx| cx.notify()).detach();
        FoundationsState {
            lab: MotionLab::new(presence, Instant::now()),
            surface: Strip::pair(),
            sample,
        }
    }

    pub(super) fn render(
        &mut self,
        page: Page,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> Div {
        match page {
            Page::Type => type_scale(&self.sample, theme, cx),
            Page::Icons => icons(theme, cx),
            Page::Surfaces => self.surfaces(theme, cx),
            Page::Motion => self.motion(theme, window, cx),
            _ => div(),
        }
    }

    fn surfaces(&self, theme: &Theme, cx: &mut Context<Book>) -> Div {
        let expand = header_action("surface-expand", Icon::Plus, "Expand", theme)
            .on_click(Book::clicked("Expand", cx))
            .into_any_element();
        let strip = &self.surface;
        let tabbed = strip.connected("surface-tabs", CONNECTED_ROOM, theme, cx, |book| {
            Some(&mut book.foundations.surface)
        });
        let reset = button(
            "surface-reset",
            "Reset tabs",
            None,
            ButtonKind::Plain,
            theme,
        )
        .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
            this.foundations.surface = Strip::pair();
            cx.notify();
        }));
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(
                div()
                    .flex()
                    .gap_3()
                    .h(px(TILE_HEIGHT))
                    .child(
                        shell(
                            Header::Title(Some(Glyph::Chat), "Chat".into(), Some(expand)),
                            theme,
                        )
                        .flex_1()
                        .child(
                            inner_card(theme)
                                .p_3()
                                .child(label("The header is 28 px.", theme)),
                        ),
                    )
                    .child(
                        shell(Header::Tabs(tabbed.into_any_element(), None), theme)
                            .flex_1()
                            .child(
                                inner_card(theme)
                                    .p_3()
                                    .flex()
                                    .flex_col()
                                    .gap_1()
                                    .child(strip.content())
                                    .child(label(
                                        "With tabs it is 36 px and the tabs join the inner card.",
                                        theme,
                                    ))
                                    .child(div().flex().child(reset)),
                            ),
                    ),
            )
            .child(
                div()
                    .flex()
                    .gap_3()
                    .h(px(TILE_HEIGHT))
                    .child(
                        shell(titled("Shell with a plain title"), theme)
                            .flex_1()
                            .child(inner_card(theme)),
                    )
                    .child(
                        shell(titled("Dots"), theme)
                            .flex_1()
                            .child(inner_card(theme).child(dots(theme))),
                    ),
            )
            .child(
                div().flex().gap_3().h(px(TILE_HEIGHT)).child(
                    shell(titled("Scroll area: hover or scroll to see the bar"), theme)
                        .flex_1()
                        .child(inner_card(theme).child(
                            ScrollArea::new("surface-scroll").child(
                                div().p_3().flex().flex_col().gap_1().children(
                                    (1..=SCROLL_ROWS).map(|row| {
                                        label(format!("Turn {row}: the lead read the diff"), theme)
                                    }),
                                ),
                            ),
                        )),
                ),
            )
    }

    fn motion(&mut self, theme: &Theme, window: &mut Window, cx: &mut Context<Book>) -> Div {
        let now = Instant::now();
        let reduced = reduced_motion(cx);
        self.lab.prune(now);
        if self.lab.moving(now, reduced) {
            window.request_animation_frame();
        }
        let lab = &self.lab;
        let accent = theme.color(ColorToken::StatusAccent);
        let fill = theme.color(ColorToken::ButtonFill);

        let replay_all = button(
            "replay-all",
            "Replay every demo",
            None,
            ButtonKind::Primary,
            theme,
        )
        .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
            let now = Instant::now();
            for demo in Demo::ALL {
                this.foundations.lab.replay(demo, now);
            }
            cx.notify();
        }));

        let curves = div()
            .flex()
            .flex_col()
            .gap_2()
            .child(
                spread(theme)
                    .child(lane(theme).child(dot(lab.eased.placed(now, reduced), theme)))
                    .child(label("eased", theme)),
            )
            .child(
                spread(theme)
                    .child(lane(theme).child(dot(lab.sprung.placed(now, reduced), theme)))
                    .child(label("spring", theme)),
            );

        let items = MOTION_MENU.map(|(label, keys)| MenuItem::Action {
            label: label.into(),
            keys: keys.map(Into::into),
            icon: None,
        });
        let presence = div()
            .relative()
            .h(px(MENU_TILE))
            .children(
                present(
                    &lab.menu,
                    reduced,
                    window,
                    div().absolute().top(px(MENU_DROP)).left_0().child(menu(
                        "motion-menu",
                        &items,
                        theme,
                        cx.listener(|this, at: &usize, _, cx| {
                            let lab = &mut this.foundations.lab;
                            lab.menu_pick = MOTION_MENU.get(*at).map(|(label, _)| *label);
                            lab.menu.set_open(false, Instant::now());
                            cx.notify();
                        }),
                    )),
                )
                .map(deferred),
            )
            .child(label(
                lab.menu_pick.map_or(
                    "Replay opens the menu; replay again, or pick a row, and it leaves with its exit.".to_owned(),
                    |picked| format!("Picked {picked}: the menu left with its exit."),
                ),
                theme,
            ));

        let rows = lab.stagger.iter().zip(STAGGER_ROWS).map(|(row, text)| {
            let shown = row.value(now).clamp(0.0, 1.0);
            let rise = if reduced { 0.0 } else { RISE * (1.0 - shown) };
            div()
                .relative()
                .top(px(rise))
                .opacity(shown)
                .px_2()
                .py_1()
                .rounded(px(RADIUS_ROW))
                .bg(fill)
                .child(text)
        });
        let stagger = div()
            .flex()
            .flex_col()
            .gap_1()
            .w(px(STAGGER_WIDTH))
            .children(rows);

        let chips: Vec<_> = lab
            .chips
            .iter()
            .map(|chip| {
                let name = chip.name;
                div()
                    .id(("reflow-chip", name))
                    .absolute()
                    .top(px(CHIP_INSET))
                    .left(px(chip.x.placed(now, reduced)))
                    .w(px(CHIP_WIDTH))
                    .h(px(CHIP_HEIGHT))
                    .flex()
                    .items_center()
                    .justify_center()
                    .rounded(px(RADIUS_ROW))
                    .bg(fill)
                    .opacity(chip.shown.value(now).clamp(0.0, 1.0))
                    .text_size(px(FONT_SMALL))
                    .cursor_pointer()
                    .child(CHIP_NAMES.get(name).copied().unwrap_or_default())
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.foundations.lab.remove(name, Instant::now());
                        cx.notify();
                    }))
            })
            .collect();
        let reflow = lane(theme).children(chips);

        let right = lab.shared.to > SHARED_LEFT;
        let place = |lit: bool| {
            div()
                .w(px(PLACE_WIDTH))
                .h(px(PLACE_HEIGHT))
                .rounded(px(RADIUS_POP))
                .bg(theme.color(if lit {
                    ColorToken::StateActive
                } else {
                    ColorToken::SegmentedFill
                }))
        };
        let shared = div()
            .relative()
            .flex()
            .gap(px(PLACE_GAP))
            .child(place(!right))
            .child(place(right))
            .child(
                div()
                    .id("shared-chip")
                    .absolute()
                    .top(px(SHARED_TOP))
                    .left(px(lab.shared.placed(now, reduced)))
                    .w(px(SHARED_WIDTH))
                    .h(px(SHARED_HEIGHT))
                    .flex()
                    .items_center()
                    .justify_center()
                    .rounded(px(RADIUS_ROW))
                    .bg(accent)
                    .text_color(ink(theme, T1))
                    .cursor_pointer()
                    .child("plan.md")
                    .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                        this.foundations.lab.replay(Demo::Shared, Instant::now());
                        cx.notify();
                    })),
            );

        let puck = div()
            .absolute()
            .top(px(PUCK_INSET))
            .left(px(PUCK_INSET + lab.puck.placed(now, reduced)))
            .size(px(PUCK))
            .rounded_full()
            .bg(accent)
            .cursor_pointer()
            .on_mouse_down(
                MouseButton::Left,
                cx.listener(|this, event: &MouseDownEvent, _, cx| {
                    this.foundations
                        .lab
                        .grab(event.position.x.into(), Instant::now());
                    cx.notify();
                }),
            );
        let throw = lane(theme)
            .child(puck)
            .on_mouse_move(cx.listener(|this, event: &MouseMoveEvent, _, cx| {
                let lab = &mut this.foundations.lab;
                if lab.grab.is_none() {
                    return;
                }
                let now = Instant::now();
                if event.pressed_button == Some(MouseButton::Left) {
                    lab.drag(event.position.x.into(), now);
                } else {
                    lab.release(now);
                }
                cx.notify();
            }))
            .on_mouse_up(MouseButton::Left, released(cx))
            .on_mouse_up_out(MouseButton::Left, released(cx));

        let reduced_switch = switch("motion-reduced", "Reduced motion", reduced, theme).on_click(
            cx.listener(|_, _: &ClickEvent, _, cx| {
                set_reduced_motion(!reduced_motion(cx), cx);
                cx.notify();
            }),
        );
        let calm = div()
            .flex()
            .flex_col()
            .gap_2()
            .child(div().flex().child(reduced_switch))
            .child(
                lane(theme).child(
                    dot(lab.slide.placed(now, reduced), theme)
                        .opacity(lab.fade.value(now).clamp(0.0, 1.0)),
                ),
            );

        let panel = spring_text(tokens::PANEL);
        let enter_ms = ms(theme, NumberToken::MotionEnter).as_millis();
        let exit_ms = ms(theme, NumberToken::MotionExit).as_millis();
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(
                spread(theme)
                    .child(replay_all)
                    .child(label("Plays all seven demos at once.", theme)),
            )
            .child(demo(
                "Eased against spring, the same move",
                format!(
                    "eased: cubic-bezier(0.23, 1, 0.32, 1), {} ms (tokens::GLIDE_MS). spring: {panel} (tokens::PANEL). Replay mid-flight: the spring turns back with its speed, the tween starts its curve again.",
                    tokens::GLIDE_MS.as_millis()
                ),
                Demo::Curves,
                curves,
                theme,
                cx,
            ))
            .child(demo(
                "Presence: enter and exit",
                format!(
                    "enter {enter_ms} ms, exit {exit_ms} ms, critically damped springs from the theme's motion.enter and motion.exit. Reduced motion keeps the fade and drops the rise."
                ),
                Demo::Presence,
                presence,
                theme,
                cx,
            ))
            .child(demo(
                "Staggered list",
                format!(
                    "{} rows, {} ms apart, each fades in and rises {RISE} px over {} ms, cubic-bezier(0.23, 1, 0.32, 1) (tokens::PANEL_IN_MS).",
                    STAGGER_ROWS.len(),
                    STAGGER_GAP.as_millis(),
                    tokens::PANEL_IN_MS.as_millis()
                ),
                Demo::Stagger,
                stagger,
                theme,
                cx,
            ))
            .child(demo(
                "Reflow when an item is removed",
                format!(
                    "Click a chip to remove it: it fades out in {} ms (tokens::PANEL_OUT_MS) and the rest slide into place on {panel} (tokens::PANEL). Replay brings every chip back, fading in over {} ms.",
                    tokens::PANEL_OUT_MS.as_millis(),
                    tokens::PANEL_IN_MS.as_millis()
                ),
                Demo::Reflow,
                reflow,
                theme,
                cx,
            ))
            .child(demo(
                "Shared element between two places",
                format!(
                    "One element, two places: it moves between them on {panel} (tokens::PANEL). Click it, or replay mid-flight, and it turns back with its speed."
                ),
                Demo::Shared,
                shared,
                theme,
                cx,
            ))
            .child(demo(
                "Drag and release with momentum",
                format!(
                    "Drag the dot and let go: it springs to the nearer end after projecting the throw {} ms ahead, carrying its speed, on {} (tokens::TOGGLE). Replay throws it at {THROW_SPEED} px/s.",
                    Duration::from_secs_f32(THROW_LOOKAHEAD).as_millis(),
                    spring_text(tokens::TOGGLE)
                ),
                Demo::Throw,
                throw,
                theme,
                cx,
            ))
            .child(demo(
                "Reduced motion",
                format!(
                    "Reduced motion is {}. The dot slides {DOT_END} px on {panel} (tokens::PANEL) and fades in over {} ms (tokens::TOGGLE_MS). With it on, the dot lands at once and only the fade runs.",
                    if reduced { "on" } else { "off" },
                    tokens::TOGGLE_MS.as_millis()
                ),
                Demo::Reduced,
                calm,
                theme,
                cx,
            ))
    }
}
