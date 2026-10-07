mod fixture;

#[cfg(not(feature = "screen-usage"))]
#[path = "../usage/frame.rs"]
pub mod frame;
#[cfg(feature = "screen-usage")]
use crate::screens::usage::frame;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{caption, dots, inner_card};
use desk_ui::components::chip::mono;
use desk_ui::components::paint::{ink, ring, tint};
use desk_ui::components::size::{T2, T3};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyView, App, AppContext, Bounds, ClickEvent, Context, Div, FontWeight, PathBuilder, Pixels,
    Rgba, Window, canvas, div, fill, point, prelude::*, px, relative, size,
};

use fixture::{
    ACCOUNTS, AWAY, AXIS, Account, BOARD, HOURS, Lane, NOW, OPEN_USAGE, PROJECTED_UNTIL,
    SWITCHING_ORDER, Segment, Tone,
};
use frame::{LINE, Mark, note, panel, rich, title, told, window};

const ROW_ON: f32 = 0.06;
const METER_ON: f32 = 0.8;
const METER_OFF: f32 = 0.09;
const METER_WARN_ABOVE: u8 = 70;
const METER_CELLS: usize = 20;
const ASIDE_FILL: f32 = 0.06;
const ASIDE_TEXT: f32 = 0.5;
const STATE_FILL: f32 = 0.13;
const LABEL_OFF: f32 = 0.55;
const SERVE_FILL: f32 = 0.16;
const SERVE_RING: f32 = 0.28;
const SPENT_FILL: f32 = 0.1;
const SPENT_STRIPE: f32 = 0.2;
const STRIPE: f32 = 4.0;
const NIGHT_DOT: f32 = 0.12;
const DOT_PITCH: f32 = 6.0;
const LIGHT_FILL: f32 = 0.12;
const LIGHT_RING: f32 = 0.25;
const IDLE_FILL: f32 = 0.03;
const IDLE_TEXT: f32 = 0.35;
const AWAY_TEXT: f32 = 0.3;
const FOOT_RULE: f32 = 0.3;
const LEGEND_SERVE: f32 = 0.2;
const LEGEND_SPENT: f32 = 0.35;
const LEGEND_IDLE: f32 = 0.05;
const LEGEND_IDLE_RING: f32 = 0.1;
const GRID: [f32; 2] = [200.0, 92.0];

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    match board {
        None | Some(BOARD) => {}
        Some(other) => return Err(format!("the limits screen draws {BOARD}, not {other}")),
    }
    frame::load_fonts(cx)?;
    Ok(cx
        .new(|_| Limits {
            picked: 0,
            aside: [false; 3],
            read: false,
            told: None,
        })
        .into())
}

struct Limits {
    picked: usize,
    aside: [bool; 3],
    read: bool,
    told: Option<&'static str>,
}

impl Limits {
    fn header(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let ago = if self.read {
            "read just now"
        } else {
            "read 2 min ago"
        };
        let label = if self.read {
            "Read just now"
        } else {
            "Read quota now"
        };
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(10.0))
            .px(px(4.0))
            .child(title("Limits"))
            .child(
                div()
                    .text_size(px(13.0))
                    .text_color(ink(theme, T3))
                    .child(format!("every subscription window tofu can run on · {ago}")),
            )
            .child(div().flex_1())
            .child(
                button("refresh", label, None, ButtonKind::Plain, theme).on_click(cx.listener(
                    |limits, _: &ClickEvent, _, cx| {
                        limits.read = true;
                        cx.notify();
                    },
                )),
            )
    }

    fn runway(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        panel(
            "Runway",
            Some(note("at the pace of the last 30 minutes", theme).into_any_element()),
            theme,
        )
        .flex_none()
        .child(
            inner_card(theme)
                .flex_row()
                .items_center()
                .gap(px(24.0))
                .px(px(18.0))
                .py(px(12.0))
                .child(dots(theme))
                .child(
                    div()
                        .flex()
                        .flex_col()
                        .gap(px(2.0))
                        .child(
                            div()
                                .text_size(px(30.0))
                                .line_height(relative(1.1))
                                .font_weight(FontWeight::SEMIBOLD)
                                .child("no gap"),
                        )
                        .child(note("until about 23:30", theme)),
                )
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .text_size(px(13.5))
                        .line_height(px(21.0))
                        .child(rich(
                            &[
                                ("personal", Mark::Strong),
                                (" fills about ", Mark::Plain),
                                ("19:30", Mark::Warn),
                                (", 20 minutes before it resets. ", Mark::Plain),
                                ("work", Mark::Strong),
                                (
                                    " is back at 18:20, so tofu moves the lead there at 19:30 and nothing waits. Sub-agents on ",
                                    Mark::Plain,
                                ),
                                ("@smart", Mark::Mono),
                                (" stay on codex-sub, at 8%.", Mark::Plain),
                            ],
                            theme,
                        )),
                )
                .child(
                    button("order", "Switching order", None, ButtonKind::Plain, theme).on_click(
                        cx.listener(|limits, _: &ClickEvent, _, cx| {
                            limits.told = Some(SWITCHING_ORDER);
                            cx.notify();
                        }),
                    ),
                ),
        )
    }

    fn windows(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let head = grid()
            .pt(px(8.0))
            .px(px(10.0))
            .pb(px(6.0))
            .child(caption("account", theme).w(px(GRID[0])))
            .child(caption("5 hours", theme).flex_1())
            .child(caption("7 days", theme).flex_1())
            .child(caption("7 days, per model", theme).flex_1())
            .child(div().w(px(GRID[1])));
        let rows = ACCOUNTS.iter().enumerate().map(|(at, account)| {
            let aside = self.aside.get(at).copied().unwrap_or(false);
            let (state, fill, text) = if aside {
                ("set aside", ink(theme, ASIDE_FILL), ink(theme, ASIDE_TEXT))
            } else {
                let color = tone(account.tone, theme);
                (account.state, tint(color, STATE_FILL), color)
            };
            grid()
                .id(("account", at))
                .items_center()
                .p(px(10.0))
                .rounded(px(9.0))
                .when(at == self.picked, |row| row.bg(ink(theme, ROW_ON)))
                .cursor_pointer()
                .on_click(cx.listener(move |limits, _: &ClickEvent, _, cx| {
                    limits.picked = at;
                    cx.notify();
                }))
                .child(
                    div()
                        .w(px(GRID[0]))
                        .flex()
                        .flex_col()
                        .items_start()
                        .line_height(px(18.0))
                        .child(
                            div()
                                .flex()
                                .items_baseline()
                                .gap(px(4.0))
                                .text_size(px(13.5))
                                .child(account.source)
                                .child(div().text_color(ink(theme, T2)).child(account.name))
                                .child(
                                    div()
                                        .text_size(px(11.5))
                                        .text_color(ink(theme, T3))
                                        .child(account.plan),
                                ),
                        )
                        .child(
                            div().h(px(18.0)).flex().items_center().child(
                                div()
                                    .text_size(px(11.0))
                                    .line_height(px(13.0))
                                    .px(px(7.0))
                                    .py(px(1.0))
                                    .rounded_full()
                                    .bg(fill)
                                    .text_color(text)
                                    .child(state),
                            ),
                        ),
                )
                .children(account.windows.iter().map(|window| {
                    div()
                        .flex_1()
                        .min_w_0()
                        .flex()
                        .flex_col()
                        .gap(px(4.0))
                        .child(
                            div()
                                .flex()
                                .items_baseline()
                                .gap(px(8.0))
                                .min_h(px(LINE))
                                .children(window.percent.map(|percent| {
                                    div()
                                        .font_family(mono(theme))
                                        .text_size(px(15.0))
                                        .child(format!("{percent}%"))
                                }))
                                .child(
                                    div()
                                        .text_size(px(11.0))
                                        .text_color(if window.warn {
                                            theme.color(ColorToken::StatusWarn)
                                        } else {
                                            ink(theme, T3)
                                        })
                                        .child(window.projection),
                                ),
                        )
                        .child(
                            div()
                                .flex()
                                .gap(px(2.0))
                                .max_w(px(150.0))
                                .when(window.percent.is_some(), |meter| meter.h(px(5.0)))
                                .children(window.percent.into_iter().flat_map(|percent| {
                                    let filled = (usize::from(percent) + 2) / 5;
                                    let lit = meter(percent, theme);
                                    (0..METER_CELLS).map(move |cell| {
                                        div().flex_1().h(px(5.0)).rounded(px(1.5)).bg(
                                            if cell < filled {
                                                lit
                                            } else {
                                                ink(theme, METER_OFF)
                                            },
                                        )
                                    })
                                })),
                        )
                        .child(
                            div()
                                .text_size(px(11.0))
                                .text_color(ink(theme, T3))
                                .child(window.reset),
                        )
                }))
                .child(
                    button(
                        ("aside", at),
                        if aside { "Bring back" } else { "Set aside" },
                        None,
                        ButtonKind::Plain,
                        theme,
                    )
                    .w(px(GRID[1]))
                    .h(px(26.0))
                    .text_size(px(12.0))
                    .on_click(cx.listener(
                        move |limits, _: &ClickEvent, _, cx| {
                            cx.stop_propagation();
                            if let Some(aside) = limits.aside.get_mut(at) {
                                *aside = !*aside;
                            }
                            cx.notify();
                        },
                    )),
                )
        });
        panel(
            "Windows",
            Some(note("click an account for who spends it", theme).into_any_element()),
            theme,
        )
        .flex_1()
        .min_w_0()
        .child(
            inner_card(theme)
                .py(px(4.0))
                .px(px(6.0))
                .child(head)
                .children(rows)
                .child(div().flex_1())
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(12.0))
                        .p(px(10.0))
                        .border_t_1()
                        .border_color(Rgba::new(0.0, 0.0, 0.0, FOOT_RULE))
                        .text_size(px(12.5))
                        .child(caption("keys", theme))
                        .child(rich(
                            &[
                                ("meta ", Mark::Plain),
                                ("0.42 USD", Mark::Mono),
                                (" this week", Mark::Dim),
                            ],
                            theme,
                        ))
                        .child(rich(
                            &[
                                ("openrouter ", Mark::Plain),
                                ("0.03 USD", Mark::Mono),
                                (" classifier only", Mark::Dim),
                            ],
                            theme,
                        ))
                        .child(div().flex_1())
                        .child(
                            div()
                                .text_color(ink(theme, T3))
                                .child("no window, priced per token"),
                        ),
                ),
        )
    }

    fn spenders(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let account = ACCOUNTS.get(self.picked).unwrap_or(&ACCOUNTS[0]);
        panel("Who spends it", None, theme)
            .w(px(300.0))
            .flex_none()
            .child(
                inner_card(theme)
                    .px(px(14.0))
                    .py(px(12.0))
                    .gap(px(10.0))
                    .text_size(px(13.0))
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .line_height(px(18.0))
                            .child(format!("{} · {}", account.source, account.name))
                            .child(note(account.pace, theme)),
                    )
                    .children(account.spend.iter().map(|(who, share)| {
                        div()
                            .flex()
                            .flex_col()
                            .child(
                                div()
                                    .flex()
                                    .text_size(px(12.5))
                                    .child(div().flex_1().child(*who))
                                    .child(
                                        div()
                                            .font_family(mono(theme))
                                            .text_color(ink(theme, T2))
                                            .child(format!("{share}%")),
                                    ),
                            )
                            .child(
                                div()
                                    .mt(px(5.0))
                                    .h(px(5.0))
                                    .rounded(px(3.0))
                                    .overflow_hidden()
                                    .bg(ink(theme, 0.08))
                                    .child(
                                        div()
                                            .h(px(5.0))
                                            .rounded(px(3.0))
                                            .w(relative(f32::from(*share) / 100.0))
                                            .bg(theme.color(ColorToken::SwitchOn)),
                                    ),
                            )
                    }))
                    .child(div().flex_1())
                    .child(
                        button("usage", "Open Usage", None, ButtonKind::Plain, theme).on_click(
                            cx.listener(|limits, _: &ClickEvent, _, cx| {
                                limits.told = Some(OPEN_USAGE);
                                cx.notify();
                            }),
                        ),
                    ),
            )
    }

    fn timeline(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let legend = div()
            .flex()
            .gap(px(12.0))
            .text_size(px(11.5))
            .text_color(ink(theme, T3))
            .child(key(ink(theme, LEGEND_SERVE), None, "serving"))
            .child(key(
                tint(theme.color(ColorToken::StatusWarn), LEGEND_SPENT),
                None,
                "spent",
            ))
            .child(key(
                ink(theme, LEGEND_IDLE),
                Some(ink(theme, LEGEND_IDLE_RING)),
                "standby",
            ));
        let lanes = ACCOUNTS.iter().enumerate().map(|(at, account)| {
            let aside = self.aside.get(at).copied().unwrap_or(false);
            div()
                .flex()
                .items_center()
                .gap(px(10.0))
                .h(px(30.0))
                .child(
                    div()
                        .id(("lane", at))
                        .w(px(160.0))
                        .flex_none()
                        .text_size(px(12.5))
                        .text_color(if at == self.picked {
                            theme.color(ColorToken::TextStrong)
                        } else {
                            ink(theme, LABEL_OFF)
                        })
                        .cursor_pointer()
                        .on_click(cx.listener(move |limits, _: &ClickEvent, _, cx| {
                            limits.picked = at;
                            cx.notify();
                        }))
                        .child(format!("{} · {}", account.source, account.name)),
                )
                .child(track(account, aside, theme))
        });
        let axis = div()
            .flex()
            .gap(px(10.0))
            .h(px(16.0))
            .child(div().w(px(160.0)).flex_none())
            .child(
                div()
                    .flex_1()
                    .relative()
                    .text_color(ink(theme, T3))
                    .line_height(px(16.0))
                    .child(div().absolute().left_0().text_size(px(11.0)).child(NOW))
                    .children(AXIS.iter().map(|(hour, label)| {
                        div()
                            .absolute()
                            .left(relative((hour + 1.0 / 3.0) / HOURS))
                            .w_0()
                            .flex()
                            .justify_center()
                            .child(
                                div()
                                    .flex_none()
                                    .font_family(mono(theme))
                                    .text_size(px(10.5))
                                    .child(*label),
                            )
                    })),
            );
        panel("Next 12 hours", Some(legend.into_any_element()), theme)
            .flex_none()
            .child(
                inner_card(theme)
                    .pt(px(10.0))
                    .px(px(14.0))
                    .pb(px(8.0))
                    .gap(px(6.0))
                    .children(lanes)
                    .child(axis),
            )
    }
}

fn grid() -> Div {
    div().flex().gap(px(14.0))
}

fn tone(tone: Tone, theme: &Theme) -> Rgba {
    theme.color(match tone {
        Tone::Live => ColorToken::StatusLive,
        Tone::Warn => ColorToken::StatusWarn,
        Tone::Accent => ColorToken::StatusAccent,
    })
}

fn meter(percent: u8, theme: &Theme) -> Rgba {
    if percent > METER_WARN_ABOVE {
        theme.color(ColorToken::StatusWarn)
    } else {
        ink(theme, METER_ON)
    }
}

fn key(fill: Rgba, edge: Option<Rgba>, label: &'static str) -> Div {
    div()
        .flex()
        .items_center()
        .child(
            div()
                .w(px(10.0))
                .h(px(8.0))
                .mr(px(5.0))
                .rounded(px(2.0))
                .bg(fill)
                .when_some(edge, |swatch, edge| swatch.shadow(vec![ring(edge)])),
        )
        .child(label)
}

#[derive(Clone, Copy)]
enum Pattern {
    Stripes(Rgba),
    Dots(Rgba),
}

fn pattern(kind: Pattern) -> impl IntoElement {
    let paint = move |bounds: Bounds<Pixels>, (), window: &mut Window, _: &mut App| {
        let width = f32::from(bounds.size.width);
        let height = f32::from(bounds.size.height);
        let at = |x: f32, y: f32| bounds.origin + point(px(x), px(y));
        match kind {
            Pattern::Stripes(color) => {
                let band = STRIPE * std::f32::consts::SQRT_2;
                let mut start = -height;
                while start < width {
                    let mut path = PathBuilder::fill();
                    path.move_to(at(start + height, 0.0));
                    path.line_to(at(start + height + band, 0.0));
                    path.line_to(at(start + band, height));
                    path.line_to(at(start, height));
                    path.close();
                    if let Ok(path) = path.build() {
                        window.paint_path(path, color);
                    }
                    start += 2.0 * band;
                }
            }
            Pattern::Dots(color) => {
                let mut y = DOT_PITCH / 2.0;
                while y < height {
                    let mut x = DOT_PITCH / 2.0;
                    while x < width {
                        window.paint_quad(
                            fill(
                                Bounds::new(at(x - 1.0, y - 1.0), size(px(2.0), px(2.0))),
                                color,
                            )
                            .corner_radii(px(1.0)),
                        );
                        x += DOT_PITCH;
                    }
                    y += DOT_PITCH;
                }
            }
        }
    };
    div()
        .absolute()
        .inset_0()
        .child(canvas(|_, _, _| (), paint).size_full())
}

fn track(account: &Account, aside: bool, theme: &Theme) -> Div {
    let set_aside = [Segment {
        lane: Lane::Idle,
        from: 0.0,
        to: PROJECTED_UNTIL,
        text: "set aside, tofu will not pick it",
    }];
    let segments = if aside { &set_aside[..] } else { account.lane };
    let shape = |from: f32| {
        div()
            .absolute()
            .top_0()
            .bottom_0()
            .left(relative(from / HOURS))
            .rounded(px(6.0))
            .overflow_hidden()
            .px(px(8.0))
            .text_size(px(11.5))
            .line_height(px(24.0))
            .whitespace_nowrap()
            .text_ellipsis()
    };
    div()
        .flex_1()
        .relative()
        .h(px(24.0))
        .children(segments.iter().map(|segment| {
            let (fill, edge, text) = match segment.lane {
                Lane::Serve => (
                    ink(theme, SERVE_FILL),
                    Some(ink(theme, SERVE_RING)),
                    theme.color(ColorToken::TextStrong),
                ),
                Lane::Spent => {
                    let warn = theme.color(ColorToken::StatusWarn);
                    (tint(warn, SPENT_FILL), None, warn)
                }
                Lane::Light => {
                    let accent = theme.color(ColorToken::StatusAccent);
                    (
                        tint(accent, LIGHT_FILL),
                        Some(tint(accent, LIGHT_RING)),
                        accent,
                    )
                }
                Lane::Idle => (ink(theme, IDLE_FILL), None, ink(theme, IDLE_TEXT)),
            };
            let warn = theme.color(ColorToken::StatusWarn);
            shape(segment.from)
                .w(relative((segment.to - segment.from) / HOURS))
                .bg(fill)
                .when_some(edge, |segment, edge| segment.shadow(vec![ring(edge)]))
                .when(segment.lane == Lane::Spent, |spent| {
                    spent.child(pattern(Pattern::Stripes(tint(warn, SPENT_STRIPE))))
                })
                .text_color(text)
                .child(div().relative().child(segment.text))
        }))
        .when(!aside, |track| {
            track.child(
                shape(PROJECTED_UNTIL)
                    .right_0()
                    .text_color(ink(theme, AWAY_TEXT))
                    .child(pattern(Pattern::Dots(ink(theme, NIGHT_DOT))))
                    .when(account.away, |night| {
                        night.child(div().relative().child(AWAY))
                    }),
            )
        })
        .children(account.ticks.iter().map(|tick| {
            div()
                .absolute()
                .top(px(-3.0))
                .bottom(px(-3.0))
                .left(relative(tick / HOURS))
                .w(px(2.0))
                .rounded(px(1.0))
                .bg(theme.color(ColorToken::TextStrong))
        }))
}

impl Render for Limits {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let body = div()
            .gap(px(8.0))
            .pt(px(4.0))
            .px(px(4.0))
            .pb(px(8.0))
            .child(self.header(&theme, cx))
            .child(self.runway(&theme, cx))
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .gap(px(8.0))
                    .child(self.windows(&theme, cx))
                    .child(self.spenders(&theme, cx)),
            )
            .child(self.timeline(&theme, cx));
        let told = told(self.told, &theme, cx, |limits: &mut Limits| {
            limits.told = None
        });
        window(&theme, body).children(told)
    }
}
