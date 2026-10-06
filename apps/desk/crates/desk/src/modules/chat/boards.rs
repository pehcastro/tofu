use gpui::{Div, FontWeight, Rgba, SharedString, TextTransform, div, prelude::*, px};

use super::composer::{Ask, ask_bar, button, compact, kbd, queued_line, tall, working};
use super::fixture::{four_turns, running_turn};
use super::paint::{
    ADD, CHAT, CHECK, DATABASE, GO, LIVE, MONO, POP, REACT, RIGHT, SHELL, T2, T3, TRACE,
    TRACE_MARK, WARN, black, drop_shadow, file_icon, glyph, hex, medium, mono, progress, ringed,
    spacer, spinner_on, text, tint, white,
};
use super::rows::{COLUMN, Row, code_chip, row};

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

pub fn transcript(rows: &[Row], bottom: f32, scale: f32) -> Div {
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
                        .map(|(index, each)| row(each, index == 0, scale)),
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
            inner().child(transcript(&four_turns(), 30.0, scale)).child(
                centred(compact("Ask tofu to build, inspect, or delegate", scale))
                    .pt(px(6.0))
                    .pb(px(12.0)),
            ),
        )
}

pub fn running_board(scale: f32) -> Div {
    shell()
        .flex_1()
        .mb(px(8.0))
        .child(chat_head(scale, "clear-sable-eagle · a turn running"))
        .child(
            inner()
                .child(transcript(&running_turn(), 20.0, scale))
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

pub fn zoomed_board(scale: f32) -> Div {
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
                            .w(px(740.0))
                            .flex()
                            .flex_col()
                            .gap(px(8.0))
                            .child(queued_line("after that, open the app and read the badge"))
                            .child(tall("", "/", scale)),
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

fn mention_menu(scale: f32) -> Div {
    let trace = || div().child(glyph(TRACE_MARK, 13.0, TRACE, scale));
    ringed(12.0, white(0.12))
        .absolute()
        .left(px(150.0))
        .bottom(px(96.0))
        .w(px(392.0))
        .p(px(6.0))
        .bg(POP)
        .shadow(drop_shadow(50.0, 22.0, 0.6))
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

pub fn split_board(scale: f32) -> Div {
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
                                .child(trace_button(scale)),
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
                        ),
                )
                .child(
                    div()
                        .relative()
                        .pt(px(10.0))
                        .px(px(12.0))
                        .pb(px(12.0))
                        .child(tall("why is All sorted by id ", "@", scale))
                        .child(mention_menu(scale)),
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

fn edits_panel(scale: f32) -> Div {
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
                    medium(12.0, 12.0, white(0.45), "+")
                        .self_center()
                        .size(px(24.0))
                        .flex()
                        .items_center()
                        .justify_center(),
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

pub fn edits_board(scale: f32) -> Div {
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
        .child(edits_panel(scale))
}
