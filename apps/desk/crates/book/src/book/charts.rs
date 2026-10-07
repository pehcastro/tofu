use desk_ui::components::charts::{
    AMBER, BarLayout, BarLook, Bars, ContextLine, ContextRibbon, Donut, DotColumns, DotGrid,
    DotMeter, GridPart, LILAC, MINT, Mark, ROSE, Radar, RadarGrid, RadarLook, Radial, RadialShape,
    RibbonBranch, RibbonNote, RibbonSegment, SKY, Said, Series, Slice, Sparkline, Trend, TrendFill,
    TrendStroke, cached, legend,
};
use desk_ui::components::chip::mono;
use desk_ui::components::form::segmented;
use desk_ui::components::paint::{ink, tint};
use desk_ui::components::size::{T2, T3};
use desk_ui::theme::Theme;
use gpui::{
    AnyElement, App, Context, Div, Entity, FontWeight, Rgba, SharedString, Window, div, prelude::*,
    px, rgb,
};

use super::Book;
use super::kit::block;

const NARROW: f32 = 320.0;
const WIDE_MIN: f32 = 560.0;
const RIBBON_NARROW: f32 = 0.25;
const MODES: [&str; 3] = ["Tokens", "Requests", "Turn time"];
const MODE_IDS: [&str; 2] = ["activity-mode-board", "activity-mode-narrow"];
const HEADLINE: f32 = 38.0;
const WINDOW_HEADLINE: f32 = 34.0;
const JEV_COUNT: f32 = 22.0;
const SMALL: f32 = 11.5;
const LEGEND_SWATCH: f32 = 8.0;
const FORK_SWATCH: (f32, f32) = (2.0, 10.0);
const PILL: f32 = 11.0;
const ACCOUNT_WIDTH: f32 = 200.0;
const ACCOUNT_TAIL: f32 = 92.0;
const JEV_CARD: f32 = 200.0;
const JEV_FILL: f32 = 0.04;
const BOARD_RIBBON_SCROLL: &str = "ribbon-board";
const CAPACITY: usize = 250;
const FORK_AT: usize = 200;
const PROJECT_SHARE: f32 = 0.8;
const ROSE_SWATCH: u32 = 0xf1737d;
const STATE_TINT: f32 = 0.14;
const STRONG_SWATCH: f32 = 0.55;
const FAINT_SWATCH: f32 = 0.28;

const SEEDS: [[usize; 14]; 3] = [
    [3, 4, 5, 4, 6, 7, 5, 8, 9, 7, 6, 8, 10, 9],
    [2, 3, 3, 4, 5, 4, 4, 6, 7, 6, 5, 7, 8, 7],
    [1, 2, 3, 2, 4, 5, 3, 5, 6, 4, 4, 5, 6, 5],
];

enum Span {
    HalfDays { first: usize },
    Hours,
    Days { first: usize },
}

impl Span {
    fn when(&self, index: usize) -> String {
        let day = |first: usize, offset: usize| {
            let date = first + offset;
            if date <= SEPTEMBER {
                format!("Sep {date}")
            } else {
                format!("Oct {}", date - SEPTEMBER)
            }
        };
        match self {
            Span::HalfDays { first } => {
                let half = if index.is_multiple_of(2) { "am" } else { "pm" };
                format!("{} {half}", day(*first, index / 2))
            }
            Span::Hours => format!("{index:02}:00"),
            Span::Days { first } => day(*first, index),
        }
    }
}

const SEPTEMBER: usize = 30;

struct Activity {
    range: &'static str,
    columns: usize,
    seed: usize,
    headline: &'static str,
    unit: &'static str,
    delta: &'static str,
    start: &'static str,
    per_cell: usize,
    suffix: &'static str,
    noun: &'static str,
    span: Span,
}

const ACTIVITY: [Activity; 3] = [
    Activity {
        range: "7 days",
        columns: 14,
        seed: 0,
        headline: "6.8M",
        unit: "tokens",
        delta: "+18% on last week",
        start: "Sep 29",
        per_cell: 60,
        suffix: "k",
        noun: "tokens",
        span: Span::HalfDays { first: 29 },
    },
    Activity {
        range: "Today",
        columns: 24,
        seed: 1,
        headline: "1,284",
        unit: "requests",
        delta: "+9%",
        start: "00:00",
        per_cell: 12,
        suffix: "",
        noun: "requests",
        span: Span::Hours,
    },
    Activity {
        range: "30 days",
        columns: 30,
        seed: 2,
        headline: "11h 20m",
        unit: "of turns, 4h waiting on providers",
        delta: "-6%",
        start: "Sep 6",
        per_cell: 7,
        suffix: " min",
        noun: "turn time",
        span: Span::Days { first: 6 },
    },
];

const GROWTH: [(f32, f32, u32, u32); 15] = [
    (0.0, 140.0, 1, 17),
    (60.0, 130.0, 5, 33),
    (120.0, 118.0, 9, 53),
    (180.0, 100.0, 13, 83),
    (240.0, 78.0, 17, 120),
    (280.0, 58.0, 20, 153),
    (290.0, 58.0, 22, 182),
    (290.0, 124.0, 22, 31),
    (360.0, 118.0, 26, 43),
    (420.0, 104.0, 29, 77),
    (470.0, 92.0, 32, 97),
    (520.0, 86.0, 34, 107),
    (580.0, 80.0, 36, 117),
    (640.0, 72.0, 38, 130),
    (700.0, 66.0, 41, 140),
];
const REPORTS: [(f32, f32); 2] = [(420.0, 104.0), (580.0, 80.0)];
const THRESHOLD: f32 = 30.0;
const COMPACTED_AT: f32 = 290.0;

struct Window5 {
    percent: Option<u32>,
    reset: &'static str,
    projection: &'static str,
    warn: bool,
}

struct Account {
    source: &'static str,
    name: &'static str,
    plan: &'static str,
    state: &'static str,
    state_color: u32,
    windows: [Window5; 3],
}

const ACCOUNTS: [Account; 3] = [
    Account {
        source: "claude-sub",
        name: "personal",
        plan: "Max",
        state: "serving the lead",
        state_color: MINT,
        windows: [
            Window5 {
                percent: Some(34),
                reset: "resets 19:50",
                projection: "full at 19:30",
                warn: true,
            },
            Window5 {
                percent: Some(12),
                reset: "resets Fri",
                projection: "fine",
                warn: false,
            },
            Window5 {
                percent: Some(9),
                reset: "Opus · resets Fri",
                projection: "fine",
                warn: false,
            },
        ],
    },
    Account {
        source: "claude-sub",
        name: "work",
        plan: "Max",
        state: "back at 18:20",
        state_color: AMBER,
        windows: [
            Window5 {
                percent: Some(100),
                reset: "resets 18:20",
                projection: "spent",
                warn: true,
            },
            Window5 {
                percent: Some(57),
                reset: "resets Tue",
                projection: "fine",
                warn: false,
            },
            Window5 {
                percent: Some(40),
                reset: "Opus · resets Tue",
                projection: "fine",
                warn: false,
            },
        ],
    },
    Account {
        source: "codex-sub",
        name: "personal",
        plan: "Pro",
        state: "serving @smart",
        state_color: SKY,
        windows: [
            Window5 {
                percent: Some(8),
                reset: "5h · resets 21:42",
                projection: "fine",
                warn: false,
            },
            Window5 {
                percent: Some(3),
                reset: "week · resets Mon",
                projection: "fine",
                warn: false,
            },
            Window5 {
                percent: None,
                reset: "no per model window",
                projection: "",
                warn: false,
            },
        ],
    },
];

const JEV: [(&str, &str, &str, &str, [u32; 7]); 5] = [
    (
        "tool_gate",
        "shadow",
        "88",
        "3 would ask · 92% agree with labels",
        [9, 14, 11, 16, 12, 13, 13],
    ),
    (
        "shell_sift",
        "enforced",
        "214",
        "1.4M tokens cut · 344 ms",
        [22, 31, 28, 35, 30, 33, 35],
    ),
    (
        "stop_check",
        "shadow",
        "41",
        "2 stops early",
        [4, 7, 5, 6, 8, 5, 6],
    ),
    (
        "ask",
        "shadow",
        "12",
        "answers sub-agents",
        [1, 2, 0, 3, 2, 1, 3],
    ),
    (
        "page_sift",
        "shadow",
        "17",
        "browser pages",
        [0, 2, 4, 1, 3, 5, 2],
    ),
];
const JEV_DAYS: [&str; 7] = ["Wed", "Thu", "Fri", "Sat", "Sun", "Mon", "today"];

const SEGMENTS: [RibbonSegment; 4] = [
    RibbonSegment {
        name: "crisp-azure-swift",
        kind: "root",
        lane: 0,
        x0: 70.0,
        context: &[18, 30, 41, 52, 63, 74, 120, 180, 238],
        turns: &[
            "Plan the port of the store to SQLite",
            "Read every caller of Store",
            "Sketch the schema",
            "Ask about soft deletes",
            "Write the migration",
            "Try WAL mode",
            "Design the undo store",
            "Read the old tests",
            "Hand the plan over",
        ],
    },
    RibbonSegment {
        name: "tidy-ochre-wren",
        kind: "auto fork",
        lane: 0,
        x0: 380.0,
        context: &[31, 78, 130, 181],
        turns: &[
            "Pick up the plan",
            "go-dev ports Store.All",
            "ts-dev updates the count badge",
            "Fix the sort order",
        ],
    },
    RibbonSegment {
        name: "clear-sable-eagle",
        kind: "compact · head",
        lane: 0,
        x0: 540.0,
        context: &[12, 13, 13, 14, 15, 15, 16, 16, 17, 18, 18, 19, 19, 20],
        turns: &[
            "Continue after /compact",
            "Add the blank titles rule",
            "go-dev: Store.Search",
            "Review the diff",
            "Run the store tests",
            "Fix a flaky test",
            "ts-dev: the list view",
            "Ask about the deposit row",
            "Add the index",
            "Check the migration",
            "Port Store.Delete",
            "Check the undo snapshots",
            "Keep the API the same",
            "Store on SQLite, step 5 of 12",
        ],
    },
    RibbonSegment {
        name: "brave-umber-lynx",
        kind: "your fork",
        lane: 1,
        x0: 270.0,
        context: &[74, 88, 97],
        turns: &[
            "Try SQLite in WAL mode",
            "Measure the writes",
            "Dropped: no gain",
        ],
    },
];
const NOTES: [RibbonNote; 3] = [
    RibbonNote {
        at: (300.0, 128.0),
        title: "auto fork",
        value: Some("238k → 31k"),
    },
    RibbonNote {
        at: (460.0, 128.0),
        title: "/compact",
        value: Some("181k → 12k"),
    },
    RibbonNote {
        at: (234.0, 166.0),
        title: "fork at turn 6",
        value: None,
    },
];
const RIBBON_LEGEND: &str = "the ribbon is the context: thicker means fuller, 250k at most";

const MONTHS: [&str; 12] = [
    "Nov", "Dec", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct",
];
const USAGE: [(&str, u32, [f32; 12]); 3] = [
    (
        "claude-sub",
        MINT,
        [
            12.0, 18.0, 15.0, 22.0, 28.0, 24.0, 31.0, 35.0, 30.0, 38.0, 42.0, 40.0,
        ],
    ),
    (
        "codex-sub",
        SKY,
        [
            8.0, 10.0, 14.0, 12.0, 16.0, 20.0, 18.0, 22.0, 26.0, 24.0, 28.0, 30.0,
        ],
    ),
    (
        "opencode-sub",
        LILAC,
        [
            4.0, 6.0, 5.0, 8.0, 7.0, 10.0, 12.0, 11.0, 14.0, 13.0, 16.0, 18.0,
        ],
    ),
];
#[derive(Clone, Copy)]
enum Tell {
    Delta,
    Total,
    Share,
    Compare,
    Each,
    Plain,
}

const TRENDS: [(&str, TrendFill, TrendStroke, usize, bool, bool, Tell); 5] = [
    (
        "Trend, area gradient · tip: change on last month",
        TrendFill::Gradient,
        TrendStroke::Solid,
        1,
        false,
        false,
        Tell::Delta,
    ),
    (
        "Trend, area stacked solid · tip: rows and total",
        TrendFill::Solid,
        TrendStroke::Solid,
        3,
        true,
        false,
        Tell::Total,
    ),
    (
        "Trend, area stacked dotted · tip: share of month",
        TrendFill::Dotted,
        TrendStroke::Solid,
        2,
        true,
        false,
        Tell::Share,
    ),
    (
        "Trend, line glowing · tip: two series compared",
        TrendFill::Line,
        TrendStroke::Glowing,
        2,
        false,
        true,
        Tell::Compare,
    ),
    (
        "Trend, line dashed · tip: dashed key",
        TrendFill::Line,
        TrendStroke::Dashed,
        3,
        false,
        true,
        Tell::Each,
    ),
];
const BARS: [(&str, BarLook, usize, Tell); 3] = [
    (
        "Bars, stacked columns · tip: two series compared",
        BarLook::Plain,
        2,
        Tell::Compare,
    ),
    (
        "Bars, duotone · tip: change on last month",
        BarLook::Duotone,
        1,
        Tell::Delta,
    ),
    (
        "Bars, hatched · tip: compact",
        BarLook::Hatched,
        1,
        Tell::Plain,
    ),
];
const AGENT_BARS: &str = "Bars, gradient rows · tip: rank and share";
const AGENTS: [(&str, f32); 6] = [
    ("go-dev", 42.0),
    ("rust-designer", 31.0),
    ("ts-dev", 24.0),
    ("Explore", 18.0),
    ("bench", 11.0),
    ("Plan", 6.0),
];
const SHARE: [(&str, u32, f32); 5] = [
    ("claude-sub", MINT, 46.0),
    ("codex-sub", SKY, 28.0),
    ("opencode-sub", LILAC, 14.0),
    ("anthropic", AMBER, 8.0),
    ("openai", ROSE, 4.0),
];
const SHARE_TOKENS: f32 = 6.8;
const DONUT_HOLE: f32 = 0.62;
const RINGS: [(&str, u32, f32); 3] = [
    ("5 hours", MINT, 34.0),
    ("7 days", SKY, 12.0),
    ("7 days, Opus", LILAC, 9.0),
];
const RING_MAX: f32 = 100.0;
const SKILLS: [&str; 6] = ["read", "edit", "shell", "search", "browse", "ask"];
const SKILL_USE: [(&str, u32, [f32; 6]); 2] = [
    ("claude-sub", MINT, [90.0, 72.0, 64.0, 80.0, 38.0, 22.0]),
    ("codex-sub", SKY, [70.0, 84.0, 88.0, 52.0, 20.0, 34.0]),
];
const ROUND_GAP: f32 = 16.0;

fn usage(lines: usize) -> Vec<Series> {
    USAGE
        .iter()
        .take(lines)
        .map(|&(name, color, values)| Series {
            name: name.into(),
            color: rgb(color),
            values: values.to_vec(),
        })
        .collect()
}

fn ahead(said: Said, rows: &[(&str, Rgba, f32)], unit: &str) -> Said {
    let mut ranked = rows.to_vec();
    ranked.sort_by(|a, b| b.2.total_cmp(&a.2));
    match ranked.as_slice() {
        [first, second, ..] => said.foot(
            format!("{} ahead by", first.0),
            format!("{:.0}{unit}", first.2 - second.2),
            Some(first.1),
        ),
        _ => said,
    }
}

fn usage_tips(lines: usize, tell: Tell) -> Vec<Said> {
    let month_rows = |index: usize| -> Vec<(&str, Rgba, f32)> {
        USAGE
            .iter()
            .take(lines)
            .filter_map(|&(name, color, values)| {
                values.get(index).map(|&value| (name, rgb(color), value))
            })
            .collect()
    };
    let total = |index: usize| month_rows(index).iter().map(|row| row.2).sum::<f32>();
    MONTHS
        .iter()
        .enumerate()
        .map(|(index, month)| {
            let rows = month_rows(index);
            let sum = total(index);
            let listed = |said: Said, mark: Mark| {
                rows.iter()
                    .fold(said.mark(mark), |said, &(name, color, value)| {
                        said.row(name, format!("{value:.0}M"), Some(color))
                    })
            };
            let said = Said::titled(format!("{month} · tokens"));
            match tell {
                Tell::Delta => {
                    let said = said.row("this month", format!("{sum:.0}M"), None);
                    match index.checked_sub(1) {
                        Some(before) => {
                            let change = sum - total(before);
                            let tone = if change < 0.0 { ROSE } else { MINT };
                            said.foot("vs last month", format!("{change:+.0}M"), Some(rgb(tone)))
                        }
                        None => said.note("first month on record"),
                    }
                }
                Tell::Total => listed(said, Mark::Dot).foot("total", format!("{sum:.0}M"), None),
                Tell::Share => rows
                    .iter()
                    .fold(said.mark(Mark::Line), |said, &(name, color, value)| {
                        said.row(name, format!("{:.0}%", value / sum * 100.0), Some(color))
                    })
                    .note(format!("of {sum:.0}M tokens in {month}")),
                Tell::Compare => ahead(listed(said, Mark::Line), &rows, "M"),
                Tell::Each => listed(said, Mark::Dashed),
                Tell::Plain => Said::new(format!("{sum:.0}M"), format!("tokens · {month}")),
            }
        })
        .collect()
}

fn slices(table: &[(&str, u32, f32)]) -> Vec<Slice> {
    table
        .iter()
        .map(|&(name, color, value)| Slice {
            name: name.into(),
            value,
            color: rgb(color),
        })
        .collect()
}

fn share_tips() -> Vec<Said> {
    SHARE
        .iter()
        .map(|&(name, color, value)| {
            Said::titled(name)
                .mark(Mark::Dot)
                .row("share", format!("{value:.0}%"), Some(rgb(color)))
                .note(format!(
                    "{:.1}M of {SHARE_TOKENS}M tokens",
                    SHARE_TOKENS * value / 100.0
                ))
        })
        .collect()
}

fn ring_tips() -> Vec<Said> {
    RINGS
        .iter()
        .map(|&(name, color, value)| {
            Said::titled(format!("{name} window"))
                .mark(Mark::Line)
                .row("used", format!("{value:.0}%"), Some(rgb(color)))
                .foot("left", format!("{:.0}%", RING_MAX - value), None)
        })
        .collect()
}

fn agent_tips() -> Vec<Said> {
    let sum: f32 = AGENTS.iter().map(|&(_, value)| value).sum();
    AGENTS
        .iter()
        .enumerate()
        .map(|(rank, &(name, value))| {
            Said::titled(name)
                .row("tokens", format!("{value:.0}M"), None)
                .foot("share", format!("{:.0}%", value / sum * 100.0), None)
                .note(format!("#{} of {} agents", rank + 1, AGENTS.len()))
        })
        .collect()
}

fn keyed(table: &[(&str, u32, f32)]) -> Vec<(SharedString, Rgba)> {
    table
        .iter()
        .map(|&(name, color, _)| (name.into(), rgb(color)))
        .collect()
}

fn agent_bars() -> Bars {
    Bars::new(
        vec![Series {
            name: "agents".into(),
            color: rgb(AMBER),
            values: AGENTS.iter().map(|&(_, value)| value).collect(),
        }],
        AGENTS.iter().map(|&(name, _)| name.into()).collect(),
        agent_tips(),
        BarLook::Gradient,
        BarLayout::Rows,
    )
}

fn radar_chart(look: RadarLook, grid: RadarGrid) -> Radar {
    Radar::new(
        SKILLS.iter().map(|&skill| skill.into()).collect(),
        SKILL_USE
            .iter()
            .map(|&(name, color, values)| Series {
                name: name.into(),
                color: rgb(color),
                values: values.to_vec(),
            })
            .collect(),
        look,
        grid,
        SKILLS
            .iter()
            .enumerate()
            .map(|(axis, skill)| {
                let rows: Vec<(&str, Rgba, f32)> = SKILL_USE
                    .iter()
                    .filter_map(|&(name, color, values)| {
                        values.get(axis).map(|&value| (name, rgb(color), value))
                    })
                    .collect();
                let said = rows.iter().fold(
                    Said::titled(format!("{skill} · score")).mark(Mark::Line),
                    |said, &(name, color, value)| {
                        said.row(name, format!("{value:.0}"), Some(color))
                    },
                );
                ahead(said, &rows, "")
            })
            .collect(),
    )
}

struct Charts {
    activity: Vec<Entity<DotColumns>>,
    windows: [Entity<DotGrid>; 2],
    line: Entity<ContextLine>,
    meters: Vec<[Entity<DotMeter>; 3]>,
    sparks: Vec<Entity<Sparkline>>,
    ribbon: Entity<ContextRibbon>,
    trends: Vec<Entity<Trend>>,
    bars: Vec<Entity<Bars>>,
    agents: Entity<Bars>,
    donuts: [Entity<Donut>; 2],
    radials: [Entity<Radial>; 2],
    radars: [Entity<Radar>; 2],
}

impl Charts {
    fn new(ribbon_scale: f32, cx: &mut Context<Book>) -> Self {
        Charts {
            trends: TRENDS
                .iter()
                .map(|&(_, fill, stroke, lines, stacked, resting, tell)| {
                    cx.new(|_| {
                        let trend = Trend::new(
                            usage(lines),
                            usage_tips(lines, tell),
                            (MONTHS[0].into(), MONTHS[11].into()),
                            fill,
                            stroke,
                        );
                        let trend = if stacked { trend.stacked() } else { trend };
                        if resting { trend.resting_dots() } else { trend }
                    })
                })
                .collect(),
            bars: BARS
                .iter()
                .map(|&(_, look, lines, tell)| {
                    cx.new(|_| {
                        Bars::new(
                            usage(lines),
                            MONTHS.iter().map(|&month| month.into()).collect(),
                            usage_tips(lines, tell),
                            look,
                            BarLayout::Columns,
                        )
                    })
                })
                .collect(),
            agents: cx.new(|_| agent_bars()),
            donuts: [0.0, DONUT_HOLE].map(|hole| {
                cx.new(|_| {
                    Donut::new(
                        slices(&SHARE),
                        hole,
                        Some(Said::new(format!("{SHARE_TOKENS}M"), "tokens")),
                        share_tips(),
                    )
                })
            }),
            radials: [RadialShape::Full, RadialShape::Semi].map(|shape| {
                cx.new(|_| {
                    Radial::new(
                        slices(&RINGS),
                        RING_MAX,
                        shape,
                        Some(Said::new("34%", "5 hours")),
                        ring_tips(),
                    )
                })
            }),
            radars: [
                (RadarLook::Filled, RadarGrid::Polygon),
                (RadarLook::Lines, RadarGrid::Circle),
            ]
            .map(|(look, grid)| cx.new(|_| radar_chart(look, grid))),
            activity: ACTIVITY
                .iter()
                .map(|activity| cx.new(|_| activity_chart(activity)))
                .collect(),
            windows: [
                cx.new(|_| DotGrid::new(parts(false), CAPACITY, FORK_AT, Some(3))),
                cx.new(|_| DotGrid::new(parts(true), CAPACITY, FORK_AT, Some(3))),
            ],
            line: cx.new(|_| {
                ContextLine::new(
                    GROWTH.iter().map(|&(x, y, _, _)| (x, y)).collect(),
                    GROWTH
                        .iter()
                        .map(|&(_, _, step, tokens)| {
                            Said::new(format!("{tokens}k"), format!("step {step}"))
                        })
                        .collect(),
                    THRESHOLD,
                    COMPACTED_AT,
                    REPORTS.to_vec(),
                )
            }),
            meters: ACCOUNTS
                .iter()
                .map(|account| {
                    account.windows.each_ref().map(|window| {
                        cx.new(|_| {
                            DotMeter::new(
                                window.percent,
                                window.projection,
                                window.warn,
                                window.reset,
                            )
                        })
                    })
                })
                .collect(),
            sparks: JEV
                .iter()
                .map(|(_, _, _, _, values)| {
                    cx.new(|_| {
                        Sparkline::new(
                            values.to_vec(),
                            JEV_DAYS
                                .iter()
                                .zip(values)
                                .map(|(day, value)| Said::new(value.to_string(), *day))
                                .collect(),
                        )
                    })
                })
                .collect(),
            ribbon: cx.new(|_| {
                ContextRibbon::new(
                    SEGMENTS.to_vec(),
                    vec![(0, 1), (1, 2)],
                    Some(RibbonBranch {
                        from: 0,
                        turn: 5,
                        to: 3,
                    }),
                    NOTES.to_vec(),
                    RIBBON_LEGEND,
                    2,
                )
                .scale(ribbon_scale)
            }),
        }
    }
}

fn activity_chart(activity: &Activity) -> DotColumns {
    let seed = SEEDS.get(activity.seed).copied().unwrap_or(SEEDS[0]);
    let heights: Vec<usize> = (0..activity.columns)
        .map(|index| {
            let raw = seed.get(index % seed.len()).copied().unwrap_or(1) as f32;
            ((raw * PROJECT_SHARE).round() as usize).max(1)
        })
        .collect();
    let last = heights.len().saturating_sub(1);
    let tips = heights
        .iter()
        .enumerate()
        .map(|(index, height)| {
            let when = if index == last {
                "now".to_string()
            } else {
                activity.span.when(index)
            };
            Said::new(
                format!("{}{}", height * activity.per_cell, activity.suffix),
                format!("{} · {when}", activity.noun),
            )
        })
        .collect();
    DotColumns::new(heights, tips, (activity.start.into(), "now".into()))
}

fn parts(compact: bool) -> Vec<GridPart> {
    let part = |name: &str, tokens: usize, color: Rgba| GridPart {
        name: SharedString::from(name.to_string()),
        tokens,
        color,
    };
    vec![
        part(
            "System and tools",
            9,
            Rgba::new(1.0, 1.0, 1.0, STRONG_SWATCH),
        ),
        part(
            "Rules and instructions",
            4,
            Rgba::new(1.0, 1.0, 1.0, FAINT_SWATCH),
        ),
        part("Files read", 11, rgb(SKY)),
        part("Tool results", if compact { 12 } else { 38 }, rgb(MINT)),
        part("Sub-agent reports", 6, rgb(LILAC)),
        part("Conversation", 16, rgb(AMBER)),
    ]
}

fn small(text: impl Into<SharedString>, alpha: f32, theme: &Theme) -> Div {
    div()
        .text_size(px(SMALL))
        .text_color(ink(theme, alpha))
        .min_w_0()
        .truncate()
        .child(text.into())
}

fn activity_block(
    activity: &Activity,
    switch: Div,
    chart: &Entity<DotColumns>,
    theme: &Theme,
    cx: &App,
) -> Div {
    div()
        .flex()
        .flex_col()
        .gap_3()
        .min_w_0()
        .child(
            div()
                .flex()
                .items_center()
                .gap_2()
                .min_w_0()
                .child(switch)
                .child(div().flex_1())
                .child(small(activity.range, T3, theme).flex_none()),
        )
        .child(
            div()
                .flex()
                .items_baseline()
                .gap(px(10.0))
                .min_w_0()
                .child(
                    div()
                        .flex_none()
                        .text_size(px(HEADLINE))
                        .font_weight(FontWeight::SEMIBOLD)
                        .child(activity.headline),
                )
                .child(small(activity.unit, T2, theme).flex_1())
                .child(
                    div()
                        .flex_none()
                        .text_size(px(SMALL))
                        .text_color(rgb(MINT))
                        .child(activity.delta),
                ),
        )
        .child(cached(chart, cx))
}

fn window_block(compact: bool, chart: &Entity<DotGrid>, theme: &Theme, cx: &App) -> Div {
    let used: usize = parts(compact).iter().map(|part| part.tokens).sum();
    let forecast = if compact {
        "after compact · forks in about 40 steps"
    } else {
        "at this pace, forks in about 11 steps"
    };
    let swatch = |color: Rgba, label: &'static str| {
        div()
            .flex()
            .items_center()
            .gap(px(6.0))
            .flex_none()
            .child(div().size(px(LEGEND_SWATCH)).rounded(px(2.0)).bg(color))
            .child(label)
    };
    div()
        .flex()
        .flex_col()
        .gap_3()
        .min_w_0()
        .child(
            div()
                .flex()
                .items_baseline()
                .gap(px(10.0))
                .min_w_0()
                .child(
                    div()
                        .flex_none()
                        .text_size(px(WINDOW_HEADLINE))
                        .font_weight(FontWeight::SEMIBOLD)
                        .child(format!("{used}k")),
                )
                .child(
                    div()
                        .flex_none()
                        .text_color(ink(theme, T2))
                        .child("of 250k"),
                )
                .child(div().flex_1())
                .child(small(forecast, T3, theme)),
        )
        .child(cached(chart, cx))
        .child(
            div()
                .flex()
                .flex_wrap()
                .gap_x(px(14.0))
                .gap_y(px(4.0))
                .text_size(px(12.0))
                .text_color(ink(theme, T2))
                .child(swatch(ink(theme, STRONG_SWATCH), "system and tools"))
                .child(swatch(ink(theme, FAINT_SWATCH), "rules and instructions"))
                .child(swatch(rgb(SKY), "files read"))
                .child(swatch(rgb(MINT), "tool results"))
                .child(swatch(rgb(LILAC), "sub-agent reports"))
                .child(swatch(rgb(AMBER), "conversation"))
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(6.0))
                        .child(
                            div()
                                .w(px(FORK_SWATCH.0))
                                .h(px(FORK_SWATCH.1))
                                .bg(rgb(ROSE_SWATCH)),
                        )
                        .child("fork at 200k"),
                ),
        )
}

fn line_block(chart: &Entity<ContextLine>, theme: &Theme, cx: &App) -> Div {
    div()
        .flex()
        .flex_col()
        .gap_2()
        .min_w_0()
        .child(cached(chart, cx))
        .child(
            div()
                .flex()
                .gap(px(16.0))
                .min_w_0()
                .text_size(px(SMALL))
                .text_color(ink(theme, T3))
                .child(div().flex_none().child("step 1"))
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .flex()
                        .justify_center()
                        .child(div().truncate().child("compacted at step 22 · 182k to 31k")),
                )
                .child(
                    div()
                        .flex_none()
                        .text_color(rgb(LILAC))
                        .child("● sub-agent reports"),
                )
                .child(div().flex_none().child("step 41")),
        )
}

fn account_label(account: &Account, theme: &Theme) -> Div {
    div()
        .flex()
        .flex_col()
        .items_start()
        .gap_1()
        .min_w_0()
        .child(
            div()
                .flex()
                .items_baseline()
                .gap(px(4.0))
                .min_w_0()
                .text_size(px(13.5))
                .child(div().flex_none().child(account.source))
                .child(
                    div()
                        .min_w_0()
                        .truncate()
                        .text_color(ink(theme, T2))
                        .child(account.name),
                )
                .child(
                    div()
                        .flex_none()
                        .text_size(px(SMALL))
                        .text_color(ink(theme, T3))
                        .child(account.plan),
                ),
        )
        .child(
            div()
                .px(px(7.0))
                .py(px(1.0))
                .rounded_full()
                .text_size(px(PILL))
                .bg(tint(rgb(account.state_color), STATE_TINT))
                .text_color(rgb(account.state_color))
                .child(account.state),
        )
}

fn limits_block(narrow: bool, meters: &[[Entity<DotMeter>; 3]], theme: &Theme, cx: &App) -> Div {
    let header = ["5 hours", "7 days", "7 days, per model"];
    let cells = |row: &[Entity<DotMeter>; 3]| {
        div().flex().gap(px(14.0)).flex_1().min_w_0().children(
            row.iter()
                .map(|meter| div().flex_1().min_w_0().child(cached(meter, cx))),
        )
    };
    let mut table = div().flex().flex_col().min_w_0().gap_1();
    if !narrow {
        table = table.child(
            div()
                .flex()
                .gap(px(14.0))
                .px(px(10.0))
                .text_size(px(10.0))
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(ink(theme, T3))
                .child(div().w(px(ACCOUNT_WIDTH)).flex_none().child("ACCOUNT"))
                .children(header.iter().map(|title| {
                    div()
                        .flex_1()
                        .min_w_0()
                        .truncate()
                        .child(title.to_uppercase())
                }))
                .child(div().w(px(ACCOUNT_TAIL)).flex_none()),
        );
    }
    table.children(ACCOUNTS.iter().zip(meters).map(|(account, row)| {
        let label = account_label(account, theme);
        if narrow {
            div()
                .flex()
                .flex_col()
                .gap_2()
                .p(px(10.0))
                .min_w_0()
                .child(label)
                .child(cells(row))
                .into_any_element()
        } else {
            div()
                .flex()
                .items_center()
                .gap(px(14.0))
                .p(px(10.0))
                .min_w_0()
                .child(label.w(px(ACCOUNT_WIDTH)).flex_none())
                .child(cells(row))
                .child(div().w(px(ACCOUNT_TAIL)).flex_none())
                .into_any_element()
        }
    }))
}

fn jev_block(sparks: &[Entity<Sparkline>], theme: &Theme, narrow: bool, cx: &App) -> Div {
    let cards = JEV
        .iter()
        .zip(sparks)
        .map(|((name, mode, count, sub, _), spark)| {
            div()
                .flex()
                .flex_col()
                .gap_1()
                .p(px(10.0))
                .rounded(px(9.0))
                .bg(ink(theme, JEV_FILL))
                .min_w_0()
                .when(!narrow, |card| card.w(px(JEV_CARD)).flex_none())
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap_2()
                        .min_w_0()
                        .child(
                            div()
                                .min_w_0()
                                .truncate()
                                .font_family(mono(theme))
                                .text_size(px(12.5))
                                .child(*name),
                        )
                        .child(small(*mode, T3, theme).flex_none()),
                )
                .child(
                    div()
                        .flex()
                        .items_end()
                        .gap(px(10.0))
                        .child(
                            div()
                                .flex_1()
                                .text_size(px(JEV_COUNT))
                                .font_weight(FontWeight::SEMIBOLD)
                                .child(*count),
                        )
                        .child(cached(spark, cx)),
                )
                .child(small(*sub, T3, theme))
        });
    let row = div().flex().gap_2().min_w_0();
    if narrow {
        row.flex_col().children(cards)
    } else {
        row.children(cards)
    }
}

pub(super) struct ChartsPage {
    board: Charts,
    narrow: Charts,
    modes: [usize; 2],
}

impl ChartsPage {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        ChartsPage {
            board: Charts::new(1.0, cx),
            narrow: Charts::new(RIBBON_NARROW, cx),
            modes: [0; 2],
        }
    }

    fn set(
        charts: &Charts,
        side: usize,
        mode: usize,
        theme: &Theme,
        cx: &mut Context<Book>,
    ) -> Vec<(&'static str, AnyElement)> {
        let narrow = side == 1;
        let switch = segmented(
            MODE_IDS.get(side).copied().unwrap_or_default(),
            &MODES,
            mode,
            theme,
            cx.listener(move |this, picked: &usize, _, cx| {
                if let Some(slot) = this.charts.modes.get_mut(side) {
                    *slot = *picked;
                    cx.notify();
                }
            }),
        );
        let activity = ACTIVITY
            .iter()
            .zip(&charts.activity)
            .nth(mode)
            .map(|(activity, chart)| activity_block(activity, switch, chart, theme, cx))
            .unwrap_or_else(div);
        let windows = div()
            .flex()
            .flex_col()
            .gap_4()
            .min_w_0()
            .child(small("Tool results picked", T3, theme))
            .child(window_block(false, &charts.windows[0], theme, cx))
            .child(small("Preview compact", T3, theme))
            .child(window_block(true, &charts.windows[1], theme, cx));
        let ribbon = div().min_w_0().child(cached(&charts.ribbon, cx));
        let ribbon = if narrow {
            ribbon.into_any_element()
        } else {
            ribbon
                .id(BOARD_RIBBON_SCROLL)
                .overflow_x_scroll()
                .into_any_element()
        };
        let keyed_usage = |lines: usize| {
            usage(lines)
                .into_iter()
                .map(|line| (line.name, line.color))
                .collect::<Vec<_>>()
        };
        let plotted = |chart: AnyElement, key: Vec<(SharedString, Rgba)>| {
            div()
                .flex()
                .flex_col()
                .gap_3()
                .min_w_0()
                .child(chart)
                .child(legend(key, theme))
                .into_any_element()
        };
        let pair = |left: AnyElement, right: AnyElement| {
            div()
                .flex()
                .flex_wrap()
                .items_end()
                .gap(px(ROUND_GAP))
                .child(left)
                .child(right)
                .into_any_element()
        };
        let mut set: Vec<(&'static str, AnyElement)> = vec![
            (
                "DotColumns, Activity (IUSAGE-1)",
                activity.into_any_element(),
            ),
            (
                "DotGrid, The window (ICONTEXT-1)",
                windows.into_any_element(),
            ),
            (
                "ContextLine, How it grew (ICONTEXT-1)",
                line_block(&charts.line, theme, cx).into_any_element(),
            ),
            (
                "DotMeter, Windows (ILIMITS-1)",
                limits_block(narrow, &charts.meters, theme, cx).into_any_element(),
            ),
            (
                "Sparkline, Jev points (IJEV-1)",
                jev_block(&charts.sparks, theme, narrow, cx).into_any_element(),
            ),
            ("ContextRibbon, Sessions (ISESSION-2)", ribbon),
        ];
        set.extend(TRENDS.iter().zip(&charts.trends).map(|(row, chart)| {
            (
                row.0,
                plotted(cached(chart, cx).into_any_element(), keyed_usage(row.3)),
            )
        }));
        set.extend(BARS.iter().zip(&charts.bars).map(|(row, chart)| {
            (
                row.0,
                plotted(cached(chart, cx).into_any_element(), keyed_usage(row.2)),
            )
        }));
        set.push((
            AGENT_BARS,
            plotted(
                cached(&charts.agents, cx).into_any_element(),
                vec![("tokens by agent".into(), rgb(AMBER))],
            ),
        ));
        let [pie, donut] = &charts.donuts;
        let [full, semi] = &charts.radials;
        let [filled, lines] = &charts.radars;
        set.extend([
            (
                "Donut, pie and donut · tip: slice of a total",
                plotted(
                    pair(
                        cached(pie, cx).into_any_element(),
                        cached(donut, cx).into_any_element(),
                    ),
                    keyed(&SHARE),
                ),
            ),
            (
                "Radial, full and half · tip: used and left",
                plotted(
                    pair(
                        cached(full, cx).into_any_element(),
                        cached(semi, cx).into_any_element(),
                    ),
                    keyed(&RINGS),
                ),
            ),
            (
                "Radar, filled and lines · tip: two series compared",
                plotted(
                    pair(
                        cached(filled, cx).into_any_element(),
                        cached(lines, cx).into_any_element(),
                    ),
                    SKILL_USE
                        .iter()
                        .map(|&(name, color, _)| (name.into(), rgb(color)))
                        .collect(),
                ),
            ),
        ]);
        set
    }

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, cx: &mut Context<Book>) -> Div {
        let [board_mode, narrow_mode] = self.modes;
        let board = Self::set(&self.board, 0, board_mode, theme, cx);
        let narrow = Self::set(&self.narrow, 1, narrow_mode, theme, cx);
        div()
            .flex()
            .flex_col()
            .gap_3()
            .children(
                board
                    .into_iter()
                    .zip(narrow)
                    .map(|((title, wide), (_, slim))| {
                        div()
                            .flex()
                            .items_start()
                            .gap_3()
                            .child(
                                div()
                                    .flex_1()
                                    .min_w(px(WIDE_MIN))
                                    .child(block(title, theme, wide)),
                            )
                            .child(
                                div()
                                    .w(px(NARROW))
                                    .flex_none()
                                    .child(block("320 px", theme, slim)),
                            )
                    }),
            )
    }

    pub(super) fn key(&mut self, _: &str) -> bool {
        false
    }
}
