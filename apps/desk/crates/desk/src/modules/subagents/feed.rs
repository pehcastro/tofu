use gpui::{
    AnyElement, ClickEvent, Context, Div, FontWeight, IntoElement, Rgba, div, prelude::*, px,
};

use super::fixture::{
    self, Detail, Event, FEED_LIMIT, FETCH, GREP_FILES, GREP_HITS, GREP_QUERY, GREP_SCOPE, Kind,
    Sign, State, WEB_RESULTS,
};
use super::paint::{
    ADD, CHECK, DEL, FAIL, GLOBE, LINK, LIVE, SEARCH, T2, T3, TRACE, TRACE_MARK, WARN, black,
    file_icon, glyph, hex, mono, ring_arc, ringed, semibold, spacer, text, tint, white,
};
use super::{MENTION, Subagents};

const fn state_ink(state: State) -> Rgba {
    match state {
        State::Run => LIVE,
        State::Wait => WARN,
        State::Done => white(0.4),
        State::Fail => FAIL,
    }
}

pub fn avatar(kind: Kind, size: f32, state: Option<State>, scale: f32) -> Div {
    let font = (size * 0.48).round();
    let face = div()
        .relative()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .size(px(size))
        .rounded(px(size / 2.0))
        .bg(tint(kind.rgb(), 0.16))
        .child(semibold(font, size, hex(kind.rgb()), kind.initial()));
    let badge = |d: f32, offset: f32, color: Rgba| {
        div()
            .absolute()
            .right(px(offset))
            .bottom(px(offset))
            .size(px(d + 4.0))
            .rounded(px(d))
            .bg(hex(0x17161c))
            .flex()
            .items_center()
            .justify_center()
            .child(div().size(px(d)).rounded(px(d / 2.0)).bg(color))
    };
    match state {
        None => face,
        Some(State::Run) => face.child(
            div()
                .absolute()
                .top(px(-3.0))
                .left(px(-3.0))
                .child(ring_arc(size + 6.0, scale)),
        ),
        Some(State::Done) => face.child(
            badge(12.0, -5.0, hex(0x52c68e)).child(div().absolute().child(glyph(
                CHECK,
                8.0,
                hex(0x0d1a12),
                scale,
            ))),
        ),
        Some(State::Wait) => face.child(badge(9.0, -4.0, WARN)),
        Some(State::Fail) => face.child(badge(9.0, -4.0, FAIL)),
    }
}

fn mention(scale: f32, cx: &Context<Subagents>, id: (&'static str, usize)) -> AnyElement {
    div()
        .id(id)
        .flex_none()
        .opacity(0.7)
        .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
            this.tell = Some(MENTION);
            cx.notify();
        }))
        .child(glyph(TRACE_MARK, 13.0, TRACE, scale))
        .into_any_element()
}

fn well(fill: Rgba, ring: Rgba) -> Div {
    ringed(10.0, ring).mt(px(6.0)).bg(fill).overflow_hidden()
}

fn chip() -> Div {
    ringed(7.0, white(0.05))
        .flex()
        .items_center()
        .gap(px(6.0))
        .h(px(24.0))
        .px(px(8.0))
        .bg(white(0.05))
        .text_size(px(12.5))
}

fn body(detail: &Detail, scale: f32) -> Option<Div> {
    Some(match detail {
        Detail::Read { path, lines } => div().mt(px(5.0)).flex().child(
            chip()
                .children(file_icon(path, 13.0, scale))
                .child(mono(12.0, 16.0, white(0.9), *path))
                .child(text(11.5, 16.0, white(T3), *lines)),
        ),
        Detail::Grep => div()
            .mt(px(5.0))
            .flex()
            .flex_col()
            .gap(px(6.0))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .text_size(px(12.5))
                    .child(
                        chip()
                            .child(glyph(SEARCH, 13.0, white(T3), scale))
                            .child(mono(12.0, 16.0, white(0.9), GREP_QUERY)),
                    )
                    .child(
                        div()
                            .flex()
                            .items_baseline()
                            .text_color(white(T3))
                            .child("in ")
                            .child(mono(11.5, 16.0, white(T3), GREP_SCOPE))
                            .child(format!(" · {GREP_HITS}")),
                    ),
            )
            .child(
                div()
                    .flex()
                    .flex_wrap()
                    .gap(px(4.0))
                    .children(GREP_FILES.map(|path| {
                        mono(11.5, 16.0, white(T2), path)
                            .px(px(7.0))
                            .py(px(2.0))
                            .rounded(px(6.0))
                            .bg(white(0.04))
                    })),
            ),
        Detail::Web { query } => div()
            .mt(px(5.0))
            .text_size(px(13.0))
            .line_height(px(20.0))
            .child(div().text_color(white(T2)).child(format!("\"{query}\"")))
            .child(
                div()
                    .mt(px(4.0))
                    .flex()
                    .flex_col()
                    .gap(px(2.0))
                    .children(WEB_RESULTS.map(|result| {
                        div()
                            .flex()
                            .items_center()
                            .gap(px(7.0))
                            .child(div().size(px(5.0)).rounded(px(3.0)).bg(tint(0x9db8f0, 0.7)))
                            .child(text(12.5, 20.0, LINK, result))
                    })),
            ),
        Detail::Fetch => well(black(0.26), white(0.06))
            .flex()
            .items_center()
            .gap(px(10.0))
            .py(px(9.0))
            .px(px(12.0))
            .child(glyph(GLOBE, 13.0, LINK, scale))
            .child(
                div()
                    .flex_1()
                    .flex()
                    .flex_col()
                    .child(text(13.0, 18.0, white(0.9), FETCH[0]))
                    .child(mono(11.5, 18.0, white(T3), FETCH[1])),
            )
            .child(text(11.5, 18.0, white(T3), FETCH[2])),
        Detail::Edit {
            path,
            add,
            del,
            hunk,
        } => well(black(0.26), white(0.06))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .py(px(7.0))
                    .px(px(12.0))
                    .bg(white(0.025))
                    .children(file_icon(path, 13.0, scale))
                    .child(mono(12.0, 18.0, white(0.9), *path).flex_1())
                    .child(mono(11.5, 18.0, ADD, format!("+{add}")))
                    .child(mono(11.5, 18.0, DEL, format!("-{del}"))),
            )
            .child(div().py(px(4.0)).children(hunk.iter().map(|line| {
                let (fill, ink, sign) = match line.sign {
                    Sign::Add => (tint(0x52c68e, 0.1), hex(0xc6ecd6), "+"),
                    Sign::Del => (tint(0xf1737d, 0.1), hex(0xf5c0c4), "-"),
                };
                div()
                    .flex()
                    .bg(fill)
                    .text_color(ink)
                    .font_family(super::paint::MONO)
                    .text_size(px(12.0))
                    .line_height(px(20.0))
                    .whitespace_nowrap()
                    .child(
                        div()
                            .w(px(42.0))
                            .flex_none()
                            .flex()
                            .justify_end()
                            .pr(px(12.0))
                            .text_color(white(0.22))
                            .child(line.n.to_string()),
                    )
                    .child(div().w(px(14.0)).flex_none().opacity(0.7).child(sign))
                    .child(
                        div()
                            .overflow_hidden()
                            .child(line.text.replace('\t', "    ")),
                    )
            }))),
        Detail::Bash { cmd, failed, out } => well(black(0.26), white(0.06))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .py(px(7.0))
                    .px(px(12.0))
                    .h(px(41.0))
                    .child(mono(12.5, 23.0, white(T3), "$"))
                    .child(
                        mono(12.0, 23.0, white(0.9), *cmd)
                            .flex_1()
                            .min_w_0()
                            .overflow_hidden(),
                    )
                    .child(
                        mono(
                            11.0,
                            23.0,
                            if *failed { FAIL } else { LIVE },
                            if *failed { "exit 1" } else { "exit 0" },
                        )
                        .px(px(7.0))
                        .py(px(2.0))
                        .rounded(px(6.0))
                        .bg(if *failed {
                            tint(0xf1737d, 0.14)
                        } else {
                            tint(0x86e0b3, 0.12)
                        }),
                    ),
            )
            .child(
                div()
                    .pl(px(26.0))
                    .pr(px(12.0))
                    .pb(px(8.0))
                    .children(out.iter().map(|line| {
                        mono(11.5, 18.0, white(0.55), line.replace('\t', "    ")).overflow_hidden()
                    })),
            ),
        Detail::Browse { target, result, .. } => div()
            .mt(px(5.0))
            .flex()
            .items_center()
            .gap(px(8.0))
            .text_size(px(12.5))
            .child(chip().child(mono(12.0, 16.0, white(0.9), *target)))
            .child(text(12.5, 20.0, white(T3), "→"))
            .child(text(12.5, 20.0, white(T2), *result)),
        Detail::Ask { question } => well(tint(0xe8c98a, 0.07), tint(0xe8c98a, 0.2))
            .py(px(9.0))
            .px(px(12.0))
            .text_size(px(13.5))
            .line_height(px(21.0))
            .child(*question)
            .child(text(12.0, 18.0, white(T3), "the lead has not answered yet").mt(px(2.0))),
        Detail::Fail { text: body } => well(tint(0xf1737d, 0.07), tint(0xf1737d, 0.22))
            .py(px(9.0))
            .px(px(12.0))
            .child(mono(13.0, 20.0, white(0.9), *body)),
        Detail::Report { text: body, worked } => well(white(0.04), white(0.07))
            .mt(px(6.0))
            .py(px(10.0))
            .px(px(12.0))
            .text_size(px(13.5))
            .line_height(px(21.0))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(7.0))
                    .mb(px(3.0))
                    .child(glyph(CHECK, 13.0, ADD, scale))
                    .child(text(12.0, 21.0, ADD, "done"))
                    .child(text(12.0, 21.0, white(T3), format!("· {worked}"))),
            )
            .child(body.clone()),
        Detail::Spawn {
            agent,
            kind,
            task,
            owns,
        } => div()
            .mt(px(3.0))
            .flex()
            .items_baseline()
            .text_size(px(13.5))
            .line_height(px(21.0))
            .child(
                div()
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(hex(kind.rgb()))
                    .child(agent.clone()),
            )
            .child(div().text_color(white(T2)).child(format!(" {task}")))
            .child(text(12.0, 21.0, white(T3), " · owns "))
            .child(mono(11.5, 21.0, white(T3), *owns)),
    })
}

fn item(view: &Subagents, at: usize, event: &Event, scale: f32, cx: &Context<Subagents>) -> Div {
    let kind = event.who.map_or(Kind::Lead, |i| view.agents[i].kind);
    let who = event
        .who
        .map_or_else(|| "lead".to_owned(), |i| view.agents[i].id.clone());
    let action = match &event.detail {
        Detail::Browse { action, .. } => Some(text(13.0, 20.0, white(T3), *action)),
        _ => None,
    };
    div()
        .flex()
        .gap(px(12.0))
        .py(px(9.0))
        .child(avatar(kind, 24.0, None, scale))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap(px(8.0))
                        .text_size(px(13.0))
                        .line_height(px(20.0))
                        .child(semibold(13.0, 20.0, hex(kind.rgb()), who))
                        .child(text(13.0, 20.0, white(T3), event.detail.label()))
                        .children(action)
                        .child(spacer())
                        .child(text(11.5, 20.0, white(T3), fixture::ago(event.at)))
                        .child(mention(scale, cx, ("mention", at))),
                )
                .children(body(&event.detail, scale)),
        )
}

fn head(view: &Subagents, index: usize, scale: f32, cx: &Context<Subagents>) -> Div {
    let agent = &view.agents[index];
    div()
        .flex()
        .items_center()
        .gap(px(14.0))
        .pt(px(18.0))
        .pb(px(6.0))
        .child(avatar(agent.kind, 40.0, Some(agent.state), scale))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .child(
                    div()
                        .flex()
                        .items_baseline()
                        .gap(px(10.0))
                        .child(semibold(
                            17.0,
                            20.0,
                            hex(agent.kind.rgb()),
                            agent.id.clone(),
                        ))
                        .child(text(12.5, 20.0, state_ink(agent.state), agent.state.word()))
                        .child(text(
                            12.5,
                            20.0,
                            white(T3),
                            format!("{} · owns", agent.time()),
                        ))
                        .child(mono(12.0, 20.0, white(T3), agent.owns)),
                )
                .child(text(14.0, 20.0, white(0.9), agent.task)),
        )
        .child(mention(scale, cx, ("mention-head", index)))
        .child(
            div()
                .id("feed-all")
                .flex()
                .items_center()
                .h(px(26.0))
                .px(px(12.0))
                .rounded(px(8.0))
                .bg(white(0.07))
                .text_size(px(12.0))
                .font_weight(FontWeight::MEDIUM)
                .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                    this.feed = None;
                    cx.notify();
                }))
                .child("All activity"),
        )
}

pub fn feed(view: &Subagents, scale: f32, cx: &Context<Subagents>) -> Div {
    let mut column = div().flex().flex_col().px(px(28.0)).pb(px(20.0));
    column = match view.feed {
        None => column.child(
            div()
                .flex()
                .items_baseline()
                .gap(px(10.0))
                .pt(px(18.0))
                .pb(px(2.0))
                .child(semibold(16.0, 21.0, white(0.9), "All activity"))
                .child(text(12.5, 21.0, white(T3), "every sub-agent, newest first")),
        ),
        Some(index) => column.child(head(view, index, scale, cx)),
    };
    let mut last = "";
    let shown = view
        .events
        .iter()
        .enumerate()
        .filter(|(_, e)| view.feed.is_none() || e.who == view.feed)
        .take(FEED_LIMIT);
    for (at, event) in shown {
        let label = fixture::bucket(event.at);
        if label != last {
            last = label;
            column = column.child(
                semibold(10.0, 10.0, white(0.45), label.to_uppercase())
                    .pt(px(18.0))
                    .pb(px(6.0)),
            );
        }
        column = column.child(item(view, at, event, scale, cx));
    }
    column
}
