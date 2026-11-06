use std::rc::Rc;
use std::time::{Duration, Instant, SystemTime};

use gpui::{
    App, ClickEvent, Context, Entity, FocusHandle, Focusable, FontWeight, SharedString,
    StyleRefinement, Task, Window, div, prelude::*, px,
};

use crate::components::button::{ButtonKind, button};
use crate::components::chip::{mono, tabular};
use crate::components::context_card::{
    FOOTER_ROW, GAP, HEADER_ROW, LEGEND_ROW, card_frame, card_header, footer_row, header_chip, note,
};
use crate::components::list::{group_header, separator};
use crate::components::paint::ink;
use crate::components::size::{FONT_BODY, FONT_SMALL, T1, T2, T3};
use crate::live::ActiveTheme;
use crate::metrics::HAIRLINE;
use crate::theme::{ColorToken, Theme};

pub const CRON_CARD_WIDTH: f32 = 380.0;
const TITLE: &str = "Cron jobs";
const NO_JOB: &str = "No job in this session. /loop 10m <prompt> starts one";
const NO_LIVE: &str = "No job is running";
const ENDED: &str = "Ended";
const GROUP_ROW: f32 = 30.0;
const UNREPORTED: &str = "Last fire and expiry are not reported by tofu yet";
const AFTER_TURN: &str = "after each turn";
const JOB_ROW: f32 = 44.0;
const JOB_GAP: f32 = 4.0;
const CLOCK_WIDTH: f32 = 92.0;
const CLOCK_HEIGHT: f32 = 18.0;
const TOGGLE_HEIGHT: f32 = 22.0;
const SETTLE: Duration = Duration::from_millis(5);
const WEEKDAYS: [&str; 8] = [
    "Sunday",
    "Monday",
    "Tuesday",
    "Wednesday",
    "Thursday",
    "Friday",
    "Saturday",
    "Sunday",
];

#[derive(Clone, PartialEq)]
pub struct Job {
    pub id: SharedString,
    pub schedule: SharedString,
    pub prompt: SharedString,
    pub paused: bool,
    pub next: Option<SystemTime>,
    pub ended: Option<SharedString>,
}

pub struct Countdown {
    due: Instant,
    tick: Option<Task<()>>,
}

impl Render for Countdown {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        desk_perf::span!("cron-countdown");
        let (text, wait) = countdown(self.due.saturating_duration_since(Instant::now()));
        self.tick = wait.map(|wait| {
            cx.spawn(async move |clock, cx| {
                cx.background_executor().timer(wait + SETTLE).await;
                if let Err(error) = clock.update(cx, |_, cx| cx.notify()) {
                    eprintln!("desk_ui: a cron countdown closed before its next tick: {error}");
                }
            })
        });
        div()
            .size_full()
            .flex()
            .items_center()
            .justify_end()
            .font_features(tabular())
            .child(text)
    }
}

struct Row {
    job: Job,
    words: SharedString,
    line: Option<SharedString>,
    clock: Option<Entity<Countdown>>,
}

type OnCommand = Rc<dyn Fn(String, &mut Window, &mut App)>;

pub struct CronCard {
    rows: Vec<Row>,
    ended_open: bool,
    focus: FocusHandle,
    on_command: OnCommand,
}

impl CronCard {
    pub fn new(
        jobs: Vec<Job>,
        on_command: impl Fn(String, &mut Window, &mut App) + 'static,
        cx: &mut Context<Self>,
    ) -> Self {
        CronCard {
            rows: rows(ended_last(jobs), Vec::new(), cx),
            ended_open: false,
            focus: cx.focus_handle(),
            on_command: Rc::new(on_command),
        }
    }

    pub fn set(&mut self, jobs: Vec<Job>, cx: &mut Context<Self>) {
        let jobs = ended_last(jobs);
        if self.rows.iter().map(|row| &row.job).eq(jobs.iter()) {
            return;
        }
        let was = std::mem::take(&mut self.rows);
        self.rows = rows(jobs, was, cx);
        cx.notify();
    }

    fn active(&self) -> usize {
        self.rows.partition_point(|row| row.job.ended.is_none())
    }

    fn live(&self) -> usize {
        self.rows
            .iter()
            .filter(|row| !row.job.paused && row.job.ended.is_none())
            .count()
    }

    pub fn height(&self) -> f32 {
        let active = self.active();
        let ended = self.rows.len() - active;
        let shown_ended = if self.ended_open { ended } else { 0 };
        let notes = usize::from(self.rows.is_empty() || self.live() == 0);
        let groups = usize::from(ended > 0);
        let items = notes + active + groups + shown_ended;
        let list = notes as f32 * LEGEND_ROW
            + (active + shown_ended) as f32 * JOB_ROW
            + groups as f32 * GROUP_ROW
            + items.saturating_sub(1) as f32 * JOB_GAP;
        HEADER_ROW + list + HAIRLINE + FOOTER_ROW + 3.0 * GAP
    }

    fn row(&self, at: usize, row: &Row, theme: &Theme) -> impl IntoElement {
        let job = &row.job;
        let slot = match (&row.clock, job.paused, &job.ended) {
            (_, _, Some(_)) => div().text_color(ink(theme, T3)).child("ended"),
            (_, true, None) => div()
                .text_color(theme.color(ColorToken::StatusWarn))
                .child("paused"),
            (Some(clock), false, None) => div()
                .w(px(CLOCK_WIDTH))
                .h(px(CLOCK_HEIGHT))
                .child(clock.clone().cached(StyleRefinement::default().size_full())),
            (None, false, None) if job.schedule.as_ref() == AFTER_TURN => {
                div().text_color(ink(theme, T3)).child("waits for a turn")
            }
            (None, false, None) => div()
                .text_color(ink(theme, T3))
                .child("next fire unreported"),
        };
        let toggle = job.ended.is_none().then(|| {
            let verb = if job.paused { "resume" } else { "pause" };
            let line = format!("/cron {verb} {}", job.id);
            let on_command = self.on_command.clone();
            let label = if job.paused { "Resume" } else { "Pause" };
            button(
                ("cron-card-toggle", at),
                label,
                None,
                ButtonKind::Text,
                theme,
            )
            .h(px(TOGGLE_HEIGHT))
            .px_2()
            .text_size(px(FONT_SMALL))
            .on_click(move |_, window, cx| on_command(line.clone(), window, cx))
        });
        let said = job.ended.clone().unwrap_or_else(|| job.prompt.clone());
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap_2()
            .h(px(JOB_ROW))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .flex_col()
                    .gap_0p5()
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap_2()
                            .text_size(px(FONT_BODY))
                            .child(
                                div()
                                    .flex_1()
                                    .min_w_0()
                                    .truncate()
                                    .font_weight(FontWeight::MEDIUM)
                                    .text_color(ink(theme, T1))
                                    .child(row.words.clone()),
                            )
                            .child(
                                slot.flex_none()
                                    .text_size(px(FONT_SMALL))
                                    .font_features(tabular())
                                    .text_color(ink(theme, T2)),
                            ),
                    )
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap_1p5()
                            .min_w_0()
                            .text_size(px(FONT_SMALL))
                            .text_color(ink(theme, T3))
                            .child(div().flex_none().child(job.id.clone()))
                            .children(
                                row.line.clone().map(|line| {
                                    div().flex_none().font_family(mono(theme)).child(line)
                                }),
                            )
                            .child(div().flex_1().min_w_0().truncate().child(said)),
                    ),
            )
            .children(toggle)
    }
}

impl Focusable for CronCard {
    fn focus_handle(&self, _: &App) -> FocusHandle {
        self.focus.clone()
    }
}

impl Render for CronCard {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let (live, active) = (self.live(), self.active());
        let paused = active - live;
        let chip = (!self.rows.is_empty()).then(|| {
            header_chip(
                format!("{live} live · {paused} paused").into(),
                None,
                &theme,
            )
        });
        let (shown, ended) = self.rows.split_at(active);
        let opening = note(
            if self.rows.is_empty() {
                NO_JOB
            } else {
                NO_LIVE
            },
            ink(&theme, T2),
        );
        let group = (!ended.is_empty()).then(|| {
            div()
                .h(px(GROUP_ROW))
                .flex()
                .flex_col()
                .justify_end()
                .child(
                    group_header(
                        "cron-card-ended",
                        ENDED,
                        ended.len(),
                        self.ended_open,
                        &theme,
                    )
                    .on_click(cx.listener(|card, _: &ClickEvent, _, cx| {
                        card.ended_open = !card.ended_open;
                        cx.notify();
                    })),
                )
        });
        let ended = ended.iter().filter(|_| self.ended_open);
        let list = div()
            .flex()
            .flex_col()
            .gap(px(JOB_GAP))
            .children((live == 0).then_some(opening))
            .children(
                shown
                    .iter()
                    .enumerate()
                    .map(|(at, row)| self.row(at, row, &theme).into_any_element()),
            )
            .children(group)
            .children(
                ended
                    .enumerate()
                    .map(|(at, row)| self.row(active + at, row, &theme).into_any_element()),
            );
        card_frame(&self.focus)
            .child(card_header(TITLE, chip, &theme))
            .child(list)
            .child(separator(&theme).mx_0())
            .child(footer_row(
                div().text_color(ink(&theme, T3)).child(UNREPORTED),
            ))
    }
}

fn ended_last(mut jobs: Vec<Job>) -> Vec<Job> {
    jobs.sort_by_key(|job| job.ended.is_some());
    jobs
}

fn rows(jobs: Vec<Job>, mut was: Vec<Row>, cx: &mut Context<CronCard>) -> Vec<Row> {
    let now = (Instant::now(), SystemTime::now());
    jobs.into_iter()
        .map(|job| {
            let kept = was
                .iter()
                .position(|row| row.job.id == job.id && row.job.next == job.next)
                .and_then(|at| was.swap_remove(at).clock);
            let ticking = !job.paused && job.ended.is_none();
            let clock = match (kept, job.next) {
                (Some(clock), _) if ticking => Some(clock),
                (_, Some(next)) if ticking => {
                    let due = now.0 + next.duration_since(now.1).unwrap_or_default();
                    Some(cx.new(|_| Countdown { due, tick: None }))
                }
                _ => None,
            };
            let (words, line) = plain(&job.schedule);
            Row {
                job,
                words: words.into(),
                line: line.map(Into::into),
                clock,
            }
        })
        .collect()
}

fn countdown(left: Duration) -> (String, Option<Duration>) {
    let seconds = left.as_secs() + u64::from(left.subsec_nanos() > 0);
    if seconds == 0 {
        return ("due now".to_owned(), None);
    }
    if seconds <= 3600 {
        let wait = left.saturating_sub(Duration::from_secs(seconds - 1));
        return (
            format!("in {}:{:02}", seconds / 60, seconds % 60),
            Some(wait),
        );
    }
    let minutes = seconds.div_ceil(60);
    let wait = left.saturating_sub(Duration::from_secs((minutes - 1) * 60));
    (
        format!("in {} h {:02} min", minutes / 60, minutes % 60),
        Some(wait),
    )
}

fn plain(schedule: &str) -> (String, Option<String>) {
    let schedule = schedule.trim();
    if schedule == AFTER_TURN {
        return ("After each turn".to_owned(), None);
    }
    if let Some(span) = schedule.strip_prefix("every ") {
        let span = span.trim();
        let words = span_words(span).unwrap_or_else(|| span.to_owned());
        return (format!("Every {words}"), None);
    }
    if let Some(at) = schedule.strip_prefix("once at ") {
        return (format!("Once, at {}", at.trim()), None);
    }
    let line = match schedule {
        "@hourly" => "0 * * * *",
        "@daily" | "@midnight" => "0 0 * * *",
        "@weekly" => "0 0 * * 0",
        "@monthly" => "0 0 1 * *",
        line => line,
    };
    let words = line_words(line).unwrap_or_else(|| "On a custom cron schedule".to_owned());
    (words, Some(schedule.to_owned()))
}

fn every(count: u64, unit: &str) -> String {
    match count {
        1 => unit.to_owned(),
        count => format!("{count} {unit}s"),
    }
}

fn span_words(span: &str) -> Option<String> {
    let mut parts = Vec::new();
    let mut rest = span;
    while !rest.is_empty() {
        let digits = rest.find(|c: char| !c.is_ascii_digit())?;
        let (count, tail) = rest.split_at(digits);
        let count: u64 = count.parse().ok()?;
        let unit_end = tail
            .find(|c: char| c.is_ascii_digit())
            .unwrap_or(tail.len());
        let (unit, next) = tail.split_at(unit_end);
        let unit = match unit {
            "d" => "day",
            "h" => "hour",
            "m" => "minute",
            "s" => "second",
            _ => return None,
        };
        if count > 0 {
            parts.push((count, unit));
        }
        rest = next;
    }
    match parts.as_slice() {
        [] => None,
        [(count, unit)] => Some(every(*count, unit)),
        parts => Some(
            parts
                .iter()
                .map(|(count, unit)| match count {
                    1 => format!("1 {unit}"),
                    count => format!("{count} {unit}s"),
                })
                .collect::<Vec<_>>()
                .join(" "),
        ),
    }
}

fn line_words(line: &str) -> Option<String> {
    let fields: Vec<&str> = line.split_whitespace().collect();
    let [minute, hour, "*", "*", weekday] = fields.as_slice() else {
        return None;
    };
    if *hour == "*" && *weekday == "*" {
        if *minute == "*" {
            return Some("Every minute".to_owned());
        }
        if let Some(step) = minute.strip_prefix("*/") {
            return Some(format!("Every {}", every(step.parse().ok()?, "minute")));
        }
        let minute: u8 = minute.parse().ok()?;
        return Some(format!("Every hour at :{minute:02}"));
    }
    let minute: u8 = minute.parse().ok()?;
    if let Some(step) = hour.strip_prefix("*/")
        && *weekday == "*"
    {
        let hours = every(step.parse().ok()?, "hour");
        return Some(format!("Every {hours} at :{minute:02}"));
    }
    let at = format!("{:02}:{minute:02}", hour.parse::<u8>().ok()?);
    match *weekday {
        "*" => Some(format!("Every day at {at}")),
        "1-5" => Some(format!("Weekdays at {at}")),
        "0,6" | "6,0" => Some(format!("Weekends at {at}")),
        day => {
            let name = WEEKDAYS.get(usize::from(day.parse::<u8>().ok()?))?;
            Some(format!("Every {name} at {at}"))
        }
    }
}
