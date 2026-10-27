use std::time::Instant;

use super::frame;

use crate::modules::chat::Chat;
use desk_core::model::Store;
use desk_core::protocol::request::QueryDrain;
use desk_core::protocol::{DrainReport, SessionParams, drain};
use desk_core::query::Read;
use desk_ui::components::card::caption;
use desk_ui::components::charts::{BarLayout, BarLook, Bars, MINT, Said, Series, cached, hues};
use desk_ui::components::chip::mono;
use desk_ui::components::empty::empty_state;
use desk_ui::components::paint::ink;
use desk_ui::components::size::T2;
use desk_ui::components::status_bar::meter_bar;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, Context, Div, Entity, EntityId, FontWeight, SharedString,
    Subscription, WeakEntity, Window, canvas, div, prelude::*, px, relative, rgb,
};

use frame::{ellipsis, fraction, load_fonts, note, panes, reread, titled, window};

const NO_TOFU: &str =
    "Drain reads query.drain from the tofu the work screen runs, and no work screen is open here.";
const NO_SESSION: &str =
    "Open a session in the work screen. Drain shows where its tokens went, from query.drain.";
const SECTION_LEAST: f32 = 320.0;
const FIGURE_LEAST: f32 = 150.0;
const FIGURE: f32 = 28.0;
const VALUE_ROW: f32 = 18.0;
const VALUE_GAP: f32 = 8.0;
const VALUE_WIDTH: f32 = 112.0;
const KIND_LABEL: f32 = 112.0;
const MS_PER_SECOND: f64 = 1000.0;

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(board) = board {
        return Err(format!(
            "the drain screen draws what tofu query.drain reports, not the board {board}"
        ));
    }
    load_fonts(cx)?;
    Ok(cx
        .new(|_| DrainScreen {
            source: None,
            open: None,
            drained: None,
            failed: None,
            asking: false,
            asked: None,
            opened: Some(Instant::now()),
        })
        .into())
}

pub struct DrainScreen {
    source: Option<Source>,
    open: Option<String>,
    drained: Option<Drained>,
    failed: Option<String>,
    asking: bool,
    asked: Option<Instant>,
    opened: Option<Instant>,
}

struct Source {
    chat: WeakEntity<Chat>,
    id: EntityId,
    _watch: Subscription,
}

struct Drained {
    read: Read<DrainReport>,
    wakes: Entity<Bars>,
    child_wakes: Entity<Bars>,
    bands: Entity<Bars>,
}

impl DrainScreen {
    pub fn read_from(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        if self
            .source
            .as_ref()
            .is_some_and(|source| source.id == chat.entity_id())
        {
            return;
        }
        let store = chat.read(cx).store().clone();
        let watch = cx.observe(&store, |screen, store, cx| screen.saw(&store, cx));
        self.source = Some(Source {
            chat: chat.downgrade(),
            id: chat.entity_id(),
            _watch: watch,
        });
        self.saw(&store, cx);
        cx.notify();
    }

    fn chat(&self) -> Option<Entity<Chat>> {
        self.source
            .as_ref()
            .and_then(|source| source.chat.upgrade())
    }

    fn saw(&mut self, store: &Entity<Store>, cx: &mut Context<Self>) {
        let open = store.read(cx).open.clone();
        if open == self.open {
            return;
        }
        self.open = open;
        self.opened.get_or_insert_with(Instant::now);
        self.drained = None;
        self.failed = None;
        self.ask(cx);
        cx.notify();
    }

    fn again(&mut self, cx: &mut Context<Self>) {
        eprintln!("desk: drain: read again");
        self.ask(cx);
    }

    fn ask(&mut self, cx: &mut Context<Self>) {
        let (Some(chat), Some(open)) = (self.chat(), self.open.clone()) else {
            return;
        };
        eprintln!("desk: drain asks query.drain for {open}");
        self.asking = true;
        self.asked = Some(Instant::now());
        let params = SessionParams {
            session: Some(open.clone()),
        };
        let (sent, reply) = flume::bounded(1);
        chat.update(cx, |chat, cx| {
            chat.request::<QueryDrain>(&params, cx, move |_, answer, _| {
                if sent.send(answer).is_err() {
                    eprintln!("desk: drain closed before query.drain answered");
                }
            });
        });
        cx.spawn(async move |screen, cx| {
            let Ok(answer) = reply.recv_async().await else {
                return;
            };
            if let Err(error) = screen.update(cx, |screen, cx| screen.landed(&open, answer, cx)) {
                eprintln!("desk: drain closed before query.drain for {open} landed: {error}");
            }
        })
        .detach();
        cx.notify();
    }

    fn landed(
        &mut self,
        session: &str,
        answer: Result<DrainReport, String>,
        cx: &mut Context<Self>,
    ) {
        if self.open.as_deref() != Some(session) {
            return eprintln!(
                "desk: drain dropped query.drain for {session}, now {}",
                self.open.as_deref().unwrap_or("none")
            );
        }
        self.asking = false;
        if let Some(asked) = self.asked.take() {
            eprintln!(
                "desk: drain query.drain for {session} answered {} ms after it was asked",
                asked.elapsed().as_millis()
            );
        }
        match answer {
            Ok(report) => {
                eprintln!(
                    "desk: drain read {} {}: calls {} weighted_tokens {} usd {} wakes {} bands {} rebuilds {} findings {}",
                    report.handle,
                    report.session,
                    report.total.calls,
                    report.total.weighted_tokens,
                    report.total.usd,
                    report.wakes.len(),
                    report.bands.len(),
                    report.rebuilds.len(),
                    report.findings.len()
                );
                self.drained = Some(Drained {
                    wakes: bars(&report.wakes, cx),
                    child_wakes: bars(&report.child_wakes, cx),
                    bands: bars(&report.bands, cx),
                    read: Read::now(report),
                });
                self.failed = None;
            }
            Err(error) => {
                eprintln!("desk: drain: query.drain for {session} failed: {error}");
                self.failed = Some(error);
            }
        }
        cx.notify();
    }
}

fn bars(lines: &[drain::Line], cx: &mut Context<DrainScreen>) -> Entity<Bars> {
    let tips = lines
        .iter()
        .map(|line| {
            Said::titled(line.name.clone())
                .row("calls", line.calls.to_string(), None)
                .row("weighted tokens", short(line.weighted_tokens), None)
                .foot("usd", usd(line.usd), None)
        })
        .collect();
    let series = vec![Series {
        name: "weighted tokens".into(),
        color: rgb(MINT),
        values: lines
            .iter()
            .map(|line| line.weighted_tokens as f32)
            .collect(),
    }];
    let labels = lines.iter().map(|line| line.name.clone().into()).collect();
    let colors = hues(lines.iter().map(|line| line.name.as_str()));
    cx.new(|_| Bars::new(series, labels, tips, BarLook::Gradient, BarLayout::Rows).hued(colors))
}

fn short(value: f64) -> String {
    match value.abs() {
        size if size < 1e3 => format!("{value:.0}"),
        size if size < 1e6 => format!("{:.1}k", value / 1e3),
        _ => format!("{:.2}M", value / 1e6),
    }
}

fn usd(value: f64) -> String {
    format!("${value:.2}")
}

fn seconds(ms: i64) -> String {
    format!("{:.0} s", ms as f64 / MS_PER_SECOND)
}

fn section(title: &'static str, side: Option<String>, theme: &Theme) -> Div {
    div().flex().flex_col().gap(px(10.0)).child(
        div()
            .flex()
            .items_center()
            .gap(px(8.0))
            .child(caption(title, theme))
            .child(div().flex_1())
            .children(side.map(|side| note(side, theme))),
    )
}

fn figure(label: &'static str, value: String, exact: String, theme: &Theme) -> Div {
    fraction(div(), 1.0, FIGURE_LEAST)
        .flex()
        .flex_col()
        .gap(px(4.0))
        .child(caption(label, theme))
        .child(
            div()
                .text_size(px(FIGURE))
                .line_height(relative(1.0))
                .font_weight(FontWeight::SEMIBOLD)
                .child(value),
        )
        .child(note(exact, theme))
}

fn valued(lines: &[drain::Line], chart: &Entity<Bars>, theme: &Theme, cx: &App) -> Div {
    div()
        .flex()
        .gap(px(10.0))
        .child(div().flex_1().min_w_0().child(cached(chart, cx)))
        .child(
            div()
                .w(px(VALUE_WIDTH))
                .flex_none()
                .flex()
                .flex_col()
                .items_end()
                .gap(px(VALUE_GAP))
                .font_family(mono(theme))
                .text_size(px(12.0))
                .line_height(px(VALUE_ROW))
                .text_color(ink(theme, T2))
                .children(lines.iter().map(|line| {
                    div().h(px(VALUE_ROW)).child(format!(
                        "{} · {}",
                        short(line.weighted_tokens),
                        line.calls
                    ))
                })),
        )
}

fn lines_section(
    title: &'static str,
    side: &str,
    lines: &[drain::Line],
    chart: &Entity<Bars>,
    theme: &Theme,
    cx: &App,
) -> Div {
    let body = if lines.is_empty() {
        note("query.drain reported none", theme)
    } else {
        valued(lines, chart, theme, cx)
    };
    fraction(
        section(title, Some(side.to_owned()), theme).child(body),
        1.0,
        SECTION_LEAST,
    )
}

fn kinds(report: &DrainReport, theme: &Theme) -> Div {
    let (tokens, weights) = (&report.tokens, &report.default_weights);
    let rows = [
        ("input", tokens.input, weights.input),
        ("output", tokens.output, weights.output),
        ("cache read", tokens.cache_read, weights.cache_read),
        (
            "cache write 5m",
            tokens.cache_write_5m,
            weights.cache_write_5m,
        ),
        (
            "cache write 1h",
            tokens.cache_write_1h,
            weights.cache_write_1h,
        ),
    ];
    let all = rows
        .iter()
        .fold(0_i64, |sum, row| sum.saturating_add(row.1))
        .max(1) as f64;
    let colors = hues(rows.iter().map(|row| row.0));
    let section = section(
        "Tokens by kind",
        Some(format!("weights from {}", weights.from)),
        theme,
    );
    fraction(
        section.child(
            div()
                .flex()
                .flex_col()
                .gap(px(VALUE_GAP))
                .font_family(mono(theme))
                .text_size(px(12.0))
                .line_height(px(VALUE_ROW))
                .children(
                    rows.iter()
                        .zip(colors)
                        .map(|((name, count, weight), color)| {
                            div()
                                .flex()
                                .items_center()
                                .gap(px(10.0))
                                .child(
                                    div()
                                        .w(px(KIND_LABEL))
                                        .flex_none()
                                        .text_color(ink(theme, T2))
                                        .child(*name),
                                )
                                .child(
                                    meter_bar((*count as f64 / all) as f32, color, theme).flex_1(),
                                )
                                .child(
                                    div()
                                        .w(px(VALUE_WIDTH))
                                        .flex_none()
                                        .flex()
                                        .justify_end()
                                        .text_color(ink(theme, T2))
                                        .child(format!("{} ×{weight}", short(*count as f64))),
                                )
                        }),
                ),
        ),
        1.0,
        SECTION_LEAST,
    )
}

fn rebuilds(report: &DrainReport, theme: &Theme) -> Div {
    let section = section(
        "Rebuilds",
        Some(format!("{} cache rebuilds", report.rebuilds.len())),
        theme,
    );
    if report.rebuilds.is_empty() {
        return section.child(note("query.drain reported no cache rebuild", theme));
    }
    section.children(report.rebuilds.iter().map(|rebuild| {
        let differs = rebuild
            .differs
            .as_deref()
            .map_or_else(String::new, |differs| format!(", {differs} differ"));
        let agent = rebuild
            .agent
            .as_deref()
            .map_or_else(String::new, |agent| format!(" · {agent}"));
        div()
            .flex()
            .flex_col()
            .child(div().child(format!(
                "{}{differs}: {} tokens written, {} weighted",
                rebuild.cause,
                short(rebuild.written as f64),
                short(rebuild.weighted_tokens)
            )))
            .child(ellipsis(note(
                format!(
                    "{} after the last call, cache ttl {} · {}{agent} · {}",
                    seconds(rebuild.gap_ms),
                    seconds(rebuild.ttl_ms),
                    rebuild.turn,
                    rebuild.request
                ),
                theme,
            )))
    }))
}

fn findings(report: &DrainReport, theme: &Theme) -> Div {
    let section = section(
        "Findings",
        Some(format!("{} findings", report.findings.len())),
        theme,
    );
    if report.findings.is_empty() {
        return section.child(note("query.drain found nothing to flag", theme));
    }
    section.children(report.findings.iter().map(|finding| {
        let tone = match finding.severity.as_str() {
            "warning" => theme.color(ColorToken::StatusWarn),
            _ => ink(theme, T2),
        };
        let place = [
            Some(finding.turn.as_str()),
            finding.agent.as_deref(),
            finding.at.as_deref(),
        ]
        .into_iter()
        .flatten()
        .collect::<Vec<_>>()
        .join(" · ");
        div()
            .flex()
            .flex_col()
            .child(
                div()
                    .flex()
                    .gap(px(8.0))
                    .child(div().text_color(tone).child(finding.severity.clone()))
                    .child(
                        div()
                            .font_weight(FontWeight::SEMIBOLD)
                            .child(finding.title.clone()),
                    ),
            )
            .child(div().child(finding.evidence.clone()))
            .child(ellipsis(note(format!("{} · {place}", finding.rule), theme)))
    }))
}

impl Drained {
    fn body(&self, theme: &Theme, cx: &App) -> Div {
        let report = &self.read.value;
        let spend = |name: &str, spend: &drain::Spend| {
            format!(
                "{name} {} calls, {} weighted, {}",
                spend.calls,
                short(spend.weighted_tokens),
                usd(spend.usd)
            )
        };
        let totals = panes()
            .child(figure(
                "Calls",
                report.total.calls.to_string(),
                "model calls in the session".to_owned(),
                theme,
            ))
            .child(figure(
                "Weighted tokens",
                short(report.total.weighted_tokens),
                format!("{:.2} exactly", report.total.weighted_tokens),
                theme,
            ))
            .child(figure(
                "Cost",
                usd(report.total.usd),
                format!("{} usd", report.total.usd),
                theme,
            ));
        div()
            .flex()
            .flex_col()
            .gap(px(22.0))
            .child(ellipsis(note(
                format!(
                    "{} · {} · context ceiling {} · query.drain read {}",
                    report.handle,
                    report.session,
                    short(report.context_ceiling as f64),
                    self.read.at
                ),
                theme,
            )))
            .child(totals)
            .child(note(
                format!(
                    "{} · {} · {} weighted of cache writes never read",
                    spend("lead", &report.lead),
                    spend("memory", &report.memory),
                    short(report.unread_weighted)
                ),
                theme,
            ))
            .child(
                panes()
                    .gap(px(22.0))
                    .child(kinds(report, theme))
                    .child(lines_section(
                        "What woke the lead",
                        "weighted · calls",
                        &report.wakes,
                        &self.wakes,
                        theme,
                        cx,
                    )),
            )
            .child(
                panes()
                    .gap(px(22.0))
                    .child(lines_section(
                        "Bands",
                        "by context size at the call",
                        &report.bands,
                        &self.bands,
                        theme,
                        cx,
                    ))
                    .child(lines_section(
                        "What woke sub-agents",
                        "weighted · calls",
                        &report.child_wakes,
                        &self.child_wakes,
                        theme,
                        cx,
                    )),
            )
            .child(rebuilds(report, theme))
            .child(findings(report, theme))
    }
}

fn painted(opened: Instant) -> AnyElement {
    canvas(
        |_, _, _| (),
        move |_, (), _, _| {
            eprintln!(
                "desk: drain painted {} ms after open",
                opened.elapsed().as_millis()
            );
        },
    )
    .absolute()
    .size_0()
    .into_any_element()
}

impl Render for DrainScreen {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let empty = |id: &'static str, head: &'static str, text: SharedString| {
            empty_state(id, head, Some(text), &[], &[], &theme, |_, _, _| {}).into_any_element()
        };
        let failed = self
            .failed
            .clone()
            .map(|error| note(error, &theme).text_color(theme.color(ColorToken::StatusWarn)));
        let body = match (&self.source, &self.open, &self.drained) {
            (None, _, _) => empty("drain-no-tofu", "No tofu to ask", NO_TOFU.into()),
            (Some(_), None, _) => empty("drain-no-session", "No session open", NO_SESSION.into()),
            (Some(_), Some(_), Some(drained)) => drained.body(&theme, cx).into_any_element(),
            (Some(_), Some(open), None) if self.failed.is_none() => {
                note(format!("reading query.drain for {open}"), &theme).into_any_element()
            }
            (Some(_), Some(_), None) => div().into_any_element(),
        };
        let first = self
            .drained
            .is_some()
            .then(|| self.opened.take())
            .flatten()
            .map(painted);
        let trailing = self.source.is_some().then(|| {
            reread("drain", self.asking, &theme, cx, DrainScreen::again).into_any_element()
        });
        window(
            titled("drain", trailing),
            &theme,
            div()
                .relative()
                .flex_1()
                .flex()
                .flex_col()
                .gap(px(10.0))
                .children(failed)
                .child(body)
                .children(first),
        )
    }
}
