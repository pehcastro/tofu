use gpui::{Div, FontWeight, Rgba, SharedString, TextTransform, div, prelude::*, px};

use super::composer::{Ask, ask_bar, button, compact, kbd, queued_line, tall, working};
use super::fixture::{four_turns, running_turn};
use super::paint::{
    ADD, CHAT, CHECK, DATABASE, GO, LIVE, MONO, ON_LIGHT, POP, REACT, RIGHT, SHELL, T1, T2, T3,
    TRACE, TRACE_MARK, WARN, black, drop_shadow, file_icon, glyph, hex, medium, mono, progress,
    ringed, spacer, spinner_on, text, tint, white,
};
use super::rows::{COLUMN, Row, code_chip, row};
use super::{Overlay, Wire};

const ENGINEER: Rgba = hex(0x79c0ff);
const FRONTEND: Rgba = hex(0xb9a6ea);
const EXPLORER: Rgba = hex(0x8fd0aa);
const RESEARCHER: Rgba = hex(0xe8c98a);

pub fn shell() -> Div {
    ringed(12.0, white(0.06))
        .flex()
        .flex_col()
        .min_h_0()
        .min_w_0()
        .px(px(3.0))
        .pb(px(3.0))
        .bg(SHELL)
}

fn head() -> Div {
    div()
        .h(px(28.0))
        .flex_none()
        .flex()
        .items_center()
        .gap(px(6.0))
        .pl(px(9.0))
        .pr(px(6.0))
        .text_size(px(12.0))
        .line_height(px(12.0))
        .font_weight(FontWeight::MEDIUM)
        .text_color(white(0.55))
}

fn tabbed_head() -> Div {
    div()
        .h(px(36.0))
        .flex_none()
        .flex()
        .items_end()
        .gap(px(2.0))
        .pl(px(12.0))
        .pr(px(6.0))
}

pub fn inner() -> Div {
    div()
        .flex_1()
        .min_h_0()
        .flex()
        .flex_col()
        .overflow_hidden()
        .rounded(px(11.0))
}

fn caps(body: impl Into<SharedString>) -> Div {
    text(10.0, 10.0, white(0.45), body)
        .font_weight(FontWeight::SEMIBOLD)
        .letter_spacing(px(0.7))
        .text_transform(TextTransform::Uppercase)
}

pub fn transcript(rows: &[Row], bottom: f32, tools: Option<&Wire>, scale: f32) -> Div {
    div()
        .flex_1()
        .min_h_0()
        .overflow_hidden()
        .flex()
        .justify_center()
        .child(
            div()
                .w(px(COLUMN))
                .flex_none()
                .flex()
                .flex_col()
                .pt(px(22.0))
                .pb(px(bottom))
                .children(
                    rows.iter()
                        .enumerate()
                        .map(|(index, each)| match (each, tools) {
                            (Row::Tools { .. }, Some(wire)) => {
                                wire.on(row(each, index == 0, scale), Overlay::Tools)
                            }
                            _ => row(each, index == 0, scale),
                        }),
                ),
        )
}

fn centred(child: Div) -> Div {
    div().flex().justify_center().child(child)
}

fn chat_head(scale: f32, title: &'static str) -> Div {
    head()
        .child(glyph(CHAT, 13.0, white(T2), scale))
        .child("Chat")
        .child(spacer())
        .child(text(12.0, 12.0, white(T3), title))
}

pub fn four_turns_board(scale: f32) -> Div {
    shell()
        .flex_1()
        .mb(px(8.0))
        .child(chat_head(scale, "amber-cedar-otter · four turns"))
        .child(
            inner()
                .child(transcript(&four_turns(), 30.0, None, scale))
                .child(
                    centred(compact("Ask tofu to build, inspect, or delegate", scale))
                        .pt(px(6.0))
                        .pb(px(12.0)),
                ),
        )
}

pub fn running_board(scale: f32, wire: &Wire) -> Div {
    shell()
        .flex_1()
        .mb(px(8.0))
        .child(chat_head(scale, "clear-sable-eagle · a turn running"))
        .child(
            inner()
                .child(transcript(
                    &running_turn(wire.overlay == Overlay::Tools),
                    20.0,
                    Some(wire),
                    scale,
                ))
                .child(centred(working(
                    "waiting on you",
                    "the queue keeps 1 you typed",
                    scale,
                )))
                .child(
                    centred(ask_bar(&Ask {
                        command: "rm -rf web/.cache && npm run build",
                        risk: "risk 1.62 over 1.50",
                        always: "Always here",
                    }))
                    .pb(px(8.0)),
                )
                .child(
                    centred(compact(
                        "A turn is running: what you send joins the queue",
                        scale,
                    ))
                    .pb(px(12.0)),
                ),
        )
}

fn bubble(max: f32, said: &'static str) -> Div {
    div()
        .self_end()
        .max_w(px(max))
        .py(px(9.0))
        .px(px(14.0))
        .rounded(px(14.0))
        .bg(white(0.08))
        .child(said)
}

fn plan_step(mark: Div, step: &'static str, done: bool) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(10.0))
        .child(mark)
        .child(text(
            13.5,
            23.0,
            if done { white(0.9) } else { white(T3) },
            step,
        ))
}

fn dot(fill: Option<Rgba>) -> Div {
    div().w(px(13.0)).flex().justify_center().child(match fill {
        Some(fill) => div().size(px(7.0)).rounded_full().bg(fill),
        None => div()
            .size(px(7.0))
            .rounded_full()
            .border_1()
            .border_color(white(0.3)),
    })
}

pub fn zoomed_board(scale: f32, wire: &Wire) -> Div {
    let plan = div()
        .py(px(10.0))
        .px(px(14.0))
        .rounded(px(12.0))
        .bg(white(0.03))
        .flex()
        .flex_col()
        .gap(px(6.0))
        .child(medium(11.5, 16.0, white(T3), "Plan"))
        .child(plan_step(
            div().child(glyph(CHECK, 13.0, ADD, scale)),
            "Write the seed with three blank titles",
            false,
        ))
        .child(plan_step(
            dot(Some(LIVE)),
            "Reset the dev database and run the seed",
            true,
        ))
        .child(plan_step(
            dot(None),
            "Open the app and read the badge",
            false,
        ));
    let ask = ringed(12.0, tint(0xe8c98a, 0.24))
        .py(px(12.0))
        .px(px(14.0))
        .bg(tint(0xe8c98a, 0.055))
        .flex()
        .flex_col()
        .gap(px(10.0))
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(8.0))
                .child(text(
                    13.5,
                    23.0,
                    WARN,
                    "I want to run this, and it deletes data",
                ))
                .child(spacer())
                .child(text(12.0, 23.0, white(T3), "the classifier: ask")),
        )
        .child(
            mono(13.0, 23.0, white(0.9), "rm -rf ./data && go run ./cmd/seed")
                .py(px(8.0))
                .px(px(10.0))
                .rounded(px(8.0))
                .bg(black(0.25)),
        )
        .child(
            div()
                .flex()
                .items_center()
                .gap(px(6.0))
                .child(button("Allow once", "1", true))
                .child(button("Deny", "2", false))
                .child(button("Always for bash here", "3", false)),
        );
    shell()
        .flex_1()
        .mb(px(8.0))
        .child(
            head()
                .child(glyph(CHAT, 13.0, white(T2), scale))
                .child("Chat")
                .child("zoomed")
                .child(spacer())
                .child(kbd("alt Z", false)),
        )
        .child(
            inner()
                .child(
                    div().flex_1().min_h_0().overflow_hidden().flex().justify_center().child(
                        div()
                            .w(px(740.0))
                            .flex()
                            .flex_col()
                            .gap(px(16.0))
                            .pt(px(8.0))
                            .child(bubble(
                                460.0,
                                "Reseed the dev database with ten notes, three blank, then check the badge in the browser.",
                            ))
                            .child(plan)
                            .child(ask),
                    ),
                )
                .child(
                    centred(
                        div()
                            .relative()
                            .w(px(740.0))
                            .flex()
                            .flex_col()
                            .gap(px(8.0))
                            .child(queued_line("after that, open the app and read the badge"))
                            .when(wire.overlay == Overlay::Form, |column| {
                                column.child(loop_form())
                            })
                            .child(tall("", "/", Overlay::Slash, wire, scale))
                            .when(wire.overlay == Overlay::Slash, |column| {
                                column.child(slash_menu(wire))
                            }),
                    )
                    .pt(px(6.0))
                    .pb(px(14.0)),
                ),
        )
}

fn trace_button(scale: f32) -> Div {
    div()
        .ml_auto()
        .size(px(20.0))
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .child(glyph(TRACE_MARK, 13.0, white(0.45), scale))
}

fn pop_row(icon: Div, label: Div, tail: Option<Div>) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(10.0))
        .py(px(7.0))
        .px(px(10.0))
        .rounded(px(8.0))
        .child(icon)
        .child(label.flex_1())
        .children(tail)
}

fn pop(width: f32) -> Div {
    ringed(12.0, white(0.12))
        .w(px(width))
        .p(px(6.0))
        .bg(POP)
        .shadow(drop_shadow(50.0, 22.0, 0.6))
}

fn lit(row: Div, on: bool) -> Div {
    row.when(on, |row| row.bg(white(0.08)))
}

fn trace_menu() -> Div {
    let item = |label: &'static str| text(13.0, 23.0, white(T1), label).py(px(7.0)).px(px(10.0));
    pop(240.0)
        .self_end()
        .mt(px(-6.0))
        .child(
            mono(13.0, 23.0, white(T3), "tool#77c1e0 · read 6 files")
                .py(px(6.0))
                .px(px(10.0)),
        )
        .child(item("Mention in chat"))
        .child(item("Open in Sub-agents"))
        .child(item("Copy trace"))
}

fn model_picker() -> Div {
    let source = |name: &'static str, quota: &'static str| {
        div()
            .flex()
            .items_center()
            .child(caps(name).flex_1())
            .child(medium(10.0, 10.0, white(0.45), quota))
    };
    let model = |name: &'static str, note: Option<&'static str>, on: bool| {
        lit(
            div()
                .flex()
                .items_center()
                .py(px(7.0))
                .px(px(10.0))
                .rounded(px(8.0))
                .child(text(13.5, 23.0, white(T1), name).flex_1())
                .children(note.map(|note| text(12.0, 23.0, white(T3), note))),
            on,
        )
    };
    let effort = |label: &'static str, on: bool| {
        text(12.5, 20.0, if on { white(1.0) } else { white(0.4) }, label)
            .py(px(2.0))
            .px(px(9.0))
            .rounded(px(6.0))
            .when(on, |seg| seg.bg(white(0.12)))
    };
    pop(340.0)
        .absolute()
        .right(px(60.0))
        .bottom(px(66.0))
        .child(
            source("claude-sub", "5h 34% · 7d 12%")
                .py(px(6.0))
                .px(px(10.0)),
        )
        .child(model("Opus 5", Some("lead default"), true))
        .child(model("Sonnet 5", Some("@smart"), false))
        .child(
            source("codex-sub", "5h 8%")
                .pt(px(8.0))
                .px(px(10.0))
                .pb(px(6.0)),
        )
        .child(model("gpt-5.6-sol", None, false))
        .child(
            div()
                .mt(px(6.0))
                .pt(px(10.0))
                .px(px(10.0))
                .pb(px(6.0))
                .border_t_1()
                .border_color(white(0.07))
                .flex()
                .flex_col()
                .gap(px(10.0))
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(8.0))
                        .child(text(12.5, 20.0, white(T3), "effort").w(px(44.0)))
                        .child(
                            div()
                                .flex()
                                .gap(px(2.0))
                                .p(px(2.0))
                                .rounded(px(8.0))
                                .bg(white(0.05))
                                .child(effort("low", false))
                                .child(effort("medium", true))
                                .child(effort("high", false))
                                .child(effort("max", false)),
                        ),
                )
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(8.0))
                        .child(
                            div()
                                .relative()
                                .w(px(28.0))
                                .h(px(16.0))
                                .rounded(px(8.0))
                                .bg(white(0.2))
                                .child(
                                    div()
                                        .absolute()
                                        .top(px(2.0))
                                        .left(px(2.0))
                                        .size(px(12.0))
                                        .rounded(px(6.0))
                                        .bg(white(1.0)),
                                ),
                        )
                        .child(text(12.5, 20.0, white(T2), "for the next turn only")),
                ),
        )
}

fn slash_menu(wire: &Wire) -> Div {
    let command = |name: &'static str, what: &'static str, on: bool| {
        lit(
            pop_row(
                mono(14.0, 23.0, if on { white(T1) } else { white(T2) }, name).w(px(76.0)),
                text(13.0, 23.0, if on { white(T2) } else { white(T3) }, what),
                None,
            ),
            on,
        )
    };
    pop(430.0)
        .absolute()
        .left_0()
        .bottom(px(96.0))
        .child(wire.on(
            command("/loop", "run a prompt every interval", true),
            Overlay::Form,
        ))
        .child(command("/goal", "repeat until a command exits 0", false))
        .child(command("/cron", "jobs on a schedule · 1 active", false))
        .child(div().h(px(1.0)).my(px(4.0)).mx(px(8.0)).bg(white(0.07)))
        .child(command(
            "/undo",
            "put back the files the last turn changed",
            false,
        ))
        .child(command(
            "/compact",
            "shrink old tool results, no model call",
            false,
        ))
        .child(command("/context", "open the Context screen", false))
}

fn loop_form() -> Div {
    let pill = |label: &'static str| {
        medium(12.5, 12.5, white(0.85), label)
            .h(px(24.0))
            .flex()
            .items_center()
            .px(px(9.0))
            .rounded(px(7.0))
            .bg(white(0.07))
    };
    div()
        .py(px(10.0))
        .px(px(14.0))
        .rounded(px(12.0))
        .bg(white(0.05))
        .flex()
        .items_center()
        .gap(px(10.0))
        .child(mono(13.0, 23.0, white(T1), "/loop"))
        .child(text(13.0, 23.0, white(T3), "every"))
        .child(pill("10m"))
        .child(text(13.0, 23.0, white(T3), "run"))
        .child(pill("check the badge after each reseed").flex_1())
        .child(
            medium(13.0, 13.0, ON_LIGHT, "Start")
                .h(px(28.0))
                .px(px(12.0))
                .flex()
                .items_center()
                .rounded(px(8.0))
                .bg(white(0.9)),
        )
}

const MIRROR: &str = r#"<rect x="2" y="3" width="5" height="10" rx="1"/><rect x="9" y="3" width="5" height="10" rx="1"/>"#;
const CHILD: &str = r#"<circle cx="5" cy="4" r="1.5"/><circle cx="11" cy="12" r="1.5"/><path d="M5 5.5v2c0 2 6 1 6 3"/>"#;
const CLEAN: &str = r#"<circle cx="8" cy="8" r="5.5"/>"#;

fn kind_picker(scale: f32) -> Div {
    let kind = |icon: &'static str, name: &'static str, what: &'static str| {
        div()
            .flex()
            .items_start()
            .gap(px(10.0))
            .py(px(7.0))
            .px(px(10.0))
            .child(div().mt(px(3.0)).child(glyph(icon, 16.0, white(T2), scale)))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .flex_col()
                    .text_size(px(14.0))
                    .line_height(px(19.0))
                    .text_color(white(T1))
                    .child(name)
                    .child(div().text_size(px(12.5)).text_color(white(T3)).child(what)),
            )
    };
    ringed(12.0, white(0.12))
        .absolute()
        .left(px(16.0))
        .right(px(12.0))
        .top(px(95.0))
        .p(px(8.0))
        .bg(POP)
        .shadow(drop_shadow(50.0, 22.0, 0.6))
        .flex()
        .flex_col()
        .gap(px(4.0))
        .child(medium(11.5, 16.0, white(T3), "Which chat?").pt(px(4.0)).px(px(10.0)).pb(px(6.0)))
        .child(kind(MIRROR, "The running chat", "Same conversation with the lead, its own scroll. Anything you send here steers the work."))
        .child(kind(CHILD, "Child chat", "Starts from what the lead knows now. Reads, answers, never edits or spawns; the work is not interrupted."))
        .child(kind(CLEAN, "Clean chat", "A quick question with no history. Can read the project; a cheaper model by default."))
}

fn mention_menu(scale: f32) -> Div {
    let trace = || div().child(glyph(TRACE_MARK, 13.0, TRACE, scale));
    pop(392.0)
        .absolute()
        .left(px(150.0))
        .bottom(px(96.0))
        .text_size(px(13.0))
        .child(caps("Files").py(px(6.0)).px(px(10.0)))
        .child(pop_row(
            div().child(file_icon(GO, 14.0, scale)),
            mono(12.5, 23.0, white(0.9), "notes/store.go"),
            Some(text(12.0, 23.0, white(T3), "go-dev is editing")),
        ))
        .child(pop_row(
            div().child(file_icon(DATABASE, 14.0, scale)),
            mono(12.5, 23.0, white(T2), "migrations/0008_sqlite.sql"),
            None,
        ))
        .child(caps("Traces").pt(px(8.0)).px(px(10.0)).pb(px(6.0)))
        .child(pop_row(
            trace(),
            text(
                13.0,
                23.0,
                white(0.9),
                "go-dev asked the lead about sort order",
            ),
            Some(mono(11.5, 23.0, white(T3), "ask#c41e09")),
        ))
        .child(pop_row(
            trace(),
            text(13.0, 23.0, white(T2), "go test ./notes/ -run Store"),
            Some(mono(11.5, 23.0, white(T3), "tool#8f3a4a")),
        ))
}

struct Worker {
    letter: &'static str,
    name: &'static str,
    color: Rgba,
    task: &'static str,
    doing: &'static str,
    time: &'static str,
    waiting: bool,
}

const WORKERS: [Worker; 6] = [
    Worker {
        letter: "G",
        name: "go-dev 1",
        color: ENGINEER,
        task: "Port Store.All to SQLite",
        doing: "editing store/sqlite.go",
        time: "for 11m",
        waiting: false,
    },
    Worker {
        letter: "G",
        name: "go-dev 3",
        color: ENGINEER,
        task: "Move undo snapshots into the store",
        doing: "editing store/undo.go",
        time: "for 6m",
        waiting: false,
    },
    Worker {
        letter: "T",
        name: "ts-dev 1",
        color: FRONTEND,
        task: "Wire the count into the badge",
        doing: "editing web/Header.tsx",
        time: "for 4m",
        waiting: false,
    },
    Worker {
        letter: "E",
        name: "explore 2",
        color: EXPLORER,
        task: "Find where notes are sorted",
        doing: "grep ORDER BY in store/",
        time: "for 2m",
        waiting: false,
    },
    Worker {
        letter: "R",
        name: "research 2",
        color: RESEARCHER,
        task: "SQLite WAL mode for one writer",
        doing: "reading sqlite.org/wal.html",
        time: "for 3m",
        waiting: false,
    },
    Worker {
        letter: "G",
        name: "go-dev 5",
        color: ENGINEER,
        task: "Fix the flaky TestDelete",
        doing: "asked: keep the JSON store as a fallback?",
        time: "2m ago",
        waiting: true,
    },
];

fn avatar(worker: &Worker, scale: f32) -> Div {
    let mut color = worker.color;
    color.alpha = 0.16;
    let badge = if worker.waiting {
        div()
            .absolute()
            .right(px(-4.0))
            .bottom(px(-4.0))
            .size(px(13.0))
            .rounded_full()
            .bg(hex(0x17161c))
            .flex()
            .items_center()
            .justify_center()
            .child(div().size(px(9.0)).rounded_full().bg(WARN))
    } else {
        div()
            .absolute()
            .left(px(-3.0))
            .top(px(-3.0))
            .child(spinner_on(22.0, tint(0x86e0b3, 0.18), scale))
    };
    div()
        .relative()
        .flex_none()
        .size(px(16.0))
        .rounded_full()
        .bg(color)
        .flex()
        .justify_center()
        .child(text(8.0, 16.0, worker.color, worker.letter).font_weight(FontWeight::SEMIBOLD))
        .child(badge)
}

fn table_row(first: Div, second: Div, third: Div) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(12.0))
        .child(first.w(px(96.0)).flex_none())
        .child(second.flex_1().min_w_0())
        .child(third.w(px(64.0)).flex_none())
}

fn agents_panel(scale: f32) -> Div {
    let tab = |label: &'static str, badge: &'static str, on: bool| {
        div()
            .h(px(31.0))
            .pr(px(3.0))
            .rounded_t(px(9.0))
            .flex()
            .items_center()
            .when(on, |tab| tab.bg(white(0.06)))
            .child(
                div()
                    .h(px(31.0))
                    .flex()
                    .items_center()
                    .gap(px(7.0))
                    .pl(px(10.0))
                    .pr(px(6.0))
                    .child(medium(
                        12.5,
                        12.5,
                        if on { white(1.0) } else { white(0.5) },
                        label,
                    ))
                    .child(
                        medium(10.5, 10.5, white(0.45), badge)
                            .font_family(MONO)
                            .py(px(3.0))
                            .px(px(5.0))
                            .rounded(px(5.0))
                            .bg(white(0.06)),
                    ),
            )
    };
    shell()
        .w(px(455.2))
        .flex_none()
        .child(
            tabbed_head()
                .child(tab("Sub-agents", "18 live", true))
                .child(tab("File edits", "20", false))
                .child(tab("Shells", "6", false)),
        )
        .child(
            inner()
                .child(
                    table_row(caps("agent"), caps("doing"), caps("time"))
                        .pt(px(10.0))
                        .px(px(14.0))
                        .pb(px(4.0)),
                )
                .child(caps("Working 5").pt(px(8.0)).px(px(14.0)).pb(px(4.0)))
                .children(WORKERS.iter().map(|worker| {
                    table_row(
                        div()
                            .flex()
                            .items_center()
                            .gap(px(8.0))
                            .child(avatar(worker, scale))
                            .child(text(12.5, 23.0, worker.color, worker.name)),
                        div()
                            .flex()
                            .flex_col()
                            .child(
                                text(12.5, 17.0, white(0.9), worker.task)
                                    .overflow_hidden()
                                    .text_ellipsis(),
                            )
                            .child(
                                text(11.5, 17.0, white(T3), worker.doing)
                                    .overflow_hidden()
                                    .text_ellipsis(),
                            ),
                        text(11.5, 23.0, white(T3), worker.time),
                    )
                    .min_h(px(30.0))
                    .py(px(3.0))
                    .px(px(14.0))
                }))
                .child(
                    caps("Failed 3 · Finished 17")
                        .pt(px(12.0))
                        .px(px(14.0))
                        .pb(px(4.0))
                        .opacity(0.7),
                ),
        )
}

pub fn split_board(scale: f32, wire: &Wire) -> Div {
    let chat = shell()
        .w(px(682.8))
        .flex_none()
        .child(
            head()
                .child(glyph(CHAT, 13.0, white(0.55), scale))
                .child(caps("Chat").flex_1())
                .child(text(12.0, 12.0, white(T3), "clear-sable-eagle")),
        )
        .child(
            inner()
                .child(
                    div()
                        .flex_1()
                        .min_h_0()
                        .overflow_hidden()
                        .pt(px(16.0))
                        .px(px(40.0))
                        .flex()
                        .flex_col()
                        .gap(px(15.0))
                        .child(bubble(
                            440.0,
                            "Port the store to SQLite and keep the API the same.",
                        ))
                        .child(
                            div()
                                .flex()
                                .items_center()
                                .gap(px(8.0))
                                .text_size(px(13.0))
                                .text_color(white(0.5))
                                .child(glyph(RIGHT, 13.0, white(0.5), scale))
                                .child("Read 6 files · 3s")
                                .child(wire.on(trace_button(scale), Overlay::Trace)),
                        )
                        .child(
                            div()
                                .flex()
                                .items_center()
                                .child("go-dev is moving ")
                                .child(code_chip("Store".into()))
                                .child(" to SQLite; scout read every caller first and found six."),
                        )
                        .child(
                            div()
                                .flex()
                                .items_center()
                                .gap(px(9.0))
                                .text_size(px(13.5))
                                .child(progress(13.0, scale))
                                .child("go-dev")
                                .child(
                                    div()
                                        .text_color(white(T3))
                                        .child("store on SQLite · step 5 of 12"),
                                )
                                .child(trace_button(scale)),
                        )
                        .when(wire.overlay == Overlay::Trace, |list| {
                            list.child(trace_menu())
                        }),
                )
                .child(
                    div()
                        .relative()
                        .pt(px(10.0))
                        .px(px(12.0))
                        .pb(px(12.0))
                        .child(tall(
                            "why is All sorted by id ",
                            "@",
                            Overlay::Closed,
                            wire,
                            scale,
                        ))
                        .child(match wire.overlay {
                            Overlay::Picker => model_picker(),
                            _ => mention_menu(scale),
                        }),
                ),
        );
    div()
        .flex_1()
        .min_h_0()
        .mb(px(8.0))
        .flex()
        .gap(px(6.0))
        .child(chat)
        .child(agents_panel(scale))
}

struct Edit {
    icon: &'static [u8],
    name: &'static str,
    status: Rgba,
    struck: bool,
    dir: &'static str,
    who: &'static str,
    color: Rgba,
    added: &'static str,
    removed: &'static str,
}

const EDITS: [Edit; 4] = [
    Edit {
        icon: GO,
        name: "sqlite.go",
        status: hex(0x52c68e),
        struck: false,
        dir: "store/",
        who: "go-dev 1",
        color: ENGINEER,
        added: "+52",
        removed: "",
    },
    Edit {
        icon: GO,
        name: "store.go",
        status: hex(0xdeb04e),
        struck: false,
        dir: "store/",
        who: "go-dev 1",
        color: ENGINEER,
        added: "+5",
        removed: "-6",
    },
    Edit {
        icon: GO,
        name: "json.go",
        status: hex(0xf1737d),
        struck: true,
        dir: "store/",
        who: "go-dev 8",
        color: ENGINEER,
        added: "",
        removed: "-31",
    },
    Edit {
        icon: REACT,
        name: "Header.tsx",
        status: hex(0xdeb04e),
        struck: false,
        dir: "web/",
        who: "ts-dev 1",
        color: FRONTEND,
        added: "+1",
        removed: "-1",
    },
];

fn add_menu(wire: &Wire) -> Div {
    let item = |label: &'static str| text(13.5, 23.0, white(T2), label).py(px(7.0)).px(px(10.0));
    pop(200.0)
        .absolute()
        .left(px(100.0))
        .top(px(40.0))
        .child(
            wire.on(
                item("Chat")
                    .text_color(white(T1))
                    .rounded(px(8.0))
                    .bg(white(0.08)),
                Overlay::Kind,
            ),
        )
        .child(item("Terminal"))
        .child(item("Editor"))
        .child(item("Preview"))
}

fn edits_panel(scale: f32, wire: &Wire) -> Div {
    shell()
        .flex_1()
        .min_w_0()
        .child(
            tabbed_head()
                .child(
                    medium(12.5, 12.5, white(1.0), "File edits")
                        .h(px(31.0))
                        .px(px(9.0))
                        .flex()
                        .items_center()
                        .rounded_t(px(9.0))
                        .bg(white(0.06)),
                )
                .child(
                    wire.on(
                        medium(12.0, 12.0, white(0.45), "+")
                            .self_center()
                            .size(px(24.0))
                            .flex()
                            .items_center()
                            .justify_center(),
                        Overlay::Add,
                    ),
                ),
        )
        .child(
            inner()
                .py(px(8.0))
                .px(px(14.0))
                .children(EDITS.iter().map(|edit| {
                    let figure = |body: &'static str, color: Rgba, width: f32| {
                        mono(11.5, 23.0, color, body)
                            .w(px(width))
                            .flex()
                            .justify_end()
                    };
                    div()
                        .h(px(30.0))
                        .flex()
                        .items_center()
                        .gap(px(8.0))
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .flex()
                                .items_center()
                                .gap(px(8.0))
                                .child(file_icon(edit.icon, 14.0, scale))
                                .child(
                                    text(12.5, 17.0, edit.status, edit.name)
                                        .when(edit.struck, |name| name.line_through()),
                                )
                                .child(text(11.5, 15.0, white(T3), edit.dir).ml(px(-4.8))),
                        )
                        .child(text(12.0, 23.0, edit.color, edit.who).w(px(64.0)))
                        .child(figure(edit.added, ADD, 36.0))
                        .child(figure(edit.removed, super::paint::DEL, 30.0))
                })),
        )
}

pub fn edits_board(scale: f32, wire: &Wire) -> Div {
    let chat = shell()
        .w(px(643.2))
        .flex_none()
        .child(
            head()
                .child(glyph(CHAT, 13.0, white(T2), scale))
                .child("Chat")
                .child("clear-sable-eagle")
                .child(spacer()),
        )
        .child(
            inner().child(
                div()
                    .flex_1()
                    .overflow_hidden()
                    .py(px(6.0))
                    .px(px(36.0))
                    .flex()
                    .flex_col()
                    .gap(px(16.0))
                    .child(bubble(400.0, "Port the store to SQLite and keep the API the same."))
                    .child(div().child(
                        "Three sub-agents in parallel; scout first reads every caller. This will take a few minutes.",
                    ))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap(px(8.0))
                            .text_size(px(13.0))
                            .text_color(white(0.5))
                            .child(div().size(px(6.0)).rounded(px(3.0)).bg(LIVE))
                            .child("Working · 2 sub-agents"),
                    ),
            ),
        );
    div()
        .flex_1()
        .min_h_0()
        .mb(px(8.0))
        .flex()
        .gap(px(6.0))
        .child(chat)
        .child(
            edits_panel(scale, wire)
                .when(wire.overlay == Overlay::Add, |panel| {
                    panel.child(add_menu(wire))
                })
                .when(wire.overlay == Overlay::Kind, |panel| {
                    panel.child(kind_picker(scale))
                }),
        )
}
