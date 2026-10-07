use std::borrow::Cow;
use std::cell::Cell;
use std::fs::File;
use std::io::{BufWriter, Write};
use std::path::{Path, PathBuf};
use std::process::ExitCode;
use std::rc::Rc;
use std::time::{Duration, Instant};

use desk_motion::{Glide, GlideKind, spin, system_reduced_motion};
use gpui::{
    AnimationExt as _, App, AssetSource, AsyncApp, Bounds, Context, FocusHandle, KeyDownEvent,
    MouseMoveEvent, Pixels, Point, SharedString, Task, TitlebarOptions, Transformation, WeakEntity,
    Window, WindowBounds, WindowOptions, canvas, div, fill, hsla, millis, percentage, prelude::*,
    px, size, svg,
};

const TITLE: &str = "Motion demo";
const RING: &str = "ring.svg";
const RING_SVG: &[u8] = br##"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10" fill="none" stroke="#fff" stroke-opacity="0.2" stroke-width="2"/><path d="M12 2a10 10 0 0 1 10 10" fill="none" stroke="#fff" stroke-width="2"/></svg>"##;
const ROWS: usize = 8;
const ROW_HEIGHT: f32 = 32.0;
const ROW_RADIUS: f32 = 6.0;
const LIST_WIDTH: f32 = 260.0;
const SPINNERS: usize = 30;
const SPINNER_PX: f32 = 44.0;
const SPINNER_COLUMNS: f32 = 6.0;
const SPIN_PERIOD: Duration = millis(1000);
const WARMUP: Duration = millis(1000);
const MEASURE: Duration = millis(10_000);
const ROW_DWELL: Duration = millis(110);
const SETTLE: Duration = millis(600);
const MID_FLIGHT: Duration = millis(60);
const FADE_OUT: Duration = millis(500);
const QUIET: Duration = millis(5000);
const TRACE_BEFORE: usize = 6;
const TRACE_AFTER: usize = 20;
const JUDGED_VSYNC_MS: f64 = 1000.0 / 144.0;
const LATE_FACTOR: f64 = 1.5;
const USAGE: &str = "usage: motion_demo [--kind eased|spring] [--script glide|spinners]; keys: k kind, s spinners, q finish";

struct Assets;

impl AssetSource for Assets {
    fn load(&self, path: &str) -> gpui::Result<Option<Cow<'static, [u8]>>> {
        Ok((path == RING).then_some(Cow::Borrowed(RING_SVG)))
    }

    fn list(&self, _path: &str) -> gpui::Result<Vec<SharedString>> {
        Ok(vec![RING.into()])
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Script {
    Pointer,
    Glide,
    Spinners,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Segment {
    Warmup,
    Pointer,
    Sweep,
    Reversal,
    Leave,
    Idle,
    Spinners,
}

struct Options {
    kind: GlideKind,
    script: Script,
    log: Option<PathBuf>,
}

fn parse(mut args: impl Iterator<Item = String>) -> Result<Options, String> {
    let mut options = Options {
        kind: GlideKind::Spring,
        script: Script::Pointer,
        log: std::env::var_os("DESK_FRAME_LOG").map(PathBuf::from),
    };
    while let Some(arg) = args.next() {
        match (arg.as_str(), args.next().as_deref()) {
            ("--kind", Some("eased")) => options.kind = GlideKind::Eased,
            ("--kind", Some("spring")) => options.kind = GlideKind::Spring,
            ("--script", Some("glide")) => options.script = Script::Glide,
            ("--script", Some("spinners")) => options.script = Script::Spinners,
            (flag, value) => return Err(format!("{flag} {value:?}: not understood; {USAGE}")),
        }
    }
    Ok(options)
}

struct Frame {
    at: Instant,
    segment: Segment,
    animating: bool,
    active: bool,
    bar_y: Option<f32>,
    painted: Rc<Cell<Option<Instant>>>,
}

struct Demo {
    glide: Glide,
    rows: Vec<Bounds<Pixels>>,
    hovered: Option<usize>,
    spinners: bool,
    segment: Segment,
    log: Option<PathBuf>,
    frames: Vec<Frame>,
    reversed_at: Option<Instant>,
    focus: FocusHandle,
    _script: Option<Task<()>>,
}

impl Demo {
    fn point(&mut self, position: Point<Pixels>, cx: &mut Context<Self>) {
        if let Some(row) = self.rows.iter().position(|row| row.contains(&position)) {
            self.hover(row, cx);
        }
    }

    fn hover(&mut self, row: usize, cx: &mut Context<Self>) {
        let Some(bounds) = self.rows.get(row).copied() else {
            return;
        };
        if self.hovered != Some(row) {
            self.hovered = Some(row);
            self.glide.retarget(bounds, Instant::now());
            if self.segment == Segment::Idle || self.segment == Segment::Leave {
                self.segment = Segment::Pointer;
            }
            cx.notify();
        }
    }

    fn leave(&mut self, cx: &mut Context<Self>) {
        self.hovered = None;
        self.glide.hide(Instant::now());
        self.segment = Segment::Leave;
        cx.notify();
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(FADE_OUT).await;
            this.update(cx, |demo, _| {
                if demo.segment == Segment::Leave {
                    demo.segment = Segment::Idle;
                }
            })
            .ok();
        })
        .detach();
    }

    fn script(script: Script, cx: &mut Context<Self>) -> Option<Task<()>> {
        let task = match script {
            Script::Pointer => return None,
            Script::Glide => cx.spawn(async move |this, cx| {
                let hover = |segment: Segment, row: usize| {
                    move |demo: &mut Demo, cx: &mut Context<Demo>| {
                        demo.segment = segment;
                        demo.hover(row, cx);
                    }
                };
                if !step(&this, cx, WARMUP, hover(Segment::Warmup, 0)).await {
                    return;
                }
                let sweep = Instant::now();
                let bounce = (0..ROWS).chain((1..ROWS - 1).rev()).cycle().skip(1);
                for row in bounce {
                    if sweep.elapsed() >= MEASURE
                        || !step(&this, cx, ROW_DWELL, hover(Segment::Sweep, row)).await
                    {
                        break;
                    }
                }
                step(&this, cx, SETTLE, hover(Segment::Reversal, 0)).await;
                step(&this, cx, MID_FLIGHT, hover(Segment::Reversal, ROWS - 1)).await;
                step(&this, cx, SETTLE, |demo, cx| {
                    demo.reversed_at = Some(Instant::now());
                    demo.hover(0, cx);
                })
                .await;
                step(&this, cx, FADE_OUT + QUIET, |demo, cx| demo.leave(cx)).await;
                step(&this, cx, Duration::ZERO, |demo, cx| demo.finish(cx)).await;
            }),
            Script::Spinners => cx.spawn(async move |this, cx| {
                let show = |segment: Segment| {
                    move |demo: &mut Demo, cx: &mut Context<Demo>| {
                        demo.spinners = true;
                        demo.segment = segment;
                        cx.notify();
                    }
                };
                if step(&this, cx, WARMUP, show(Segment::Warmup)).await
                    && step(&this, cx, MEASURE, show(Segment::Spinners)).await
                {
                    step(&this, cx, Duration::ZERO, |demo, cx| demo.finish(cx)).await;
                }
            }),
        };
        Some(task)
    }

    fn finish(&mut self, cx: &mut Context<Self>) {
        let measured = [
            Segment::Pointer,
            Segment::Sweep,
            Segment::Reversal,
            Segment::Leave,
            Segment::Idle,
            Segment::Spinners,
        ];
        for segment in measured {
            let frames: Vec<&Frame> = self
                .frames
                .iter()
                .filter(|f| f.segment == segment)
                .collect();
            let intervals: Vec<f64> = frames
                .windows(2)
                .filter_map(|pair| match pair {
                    [before, after] if before.animating => Some(signed_ms(after.at, before.at)),
                    _ => None,
                })
                .collect();
            let late = intervals
                .iter()
                .filter(|ms| **ms > LATE_FACTOR * JUDGED_VSYNC_MS)
                .count();
            let work: Vec<f64> = frames
                .iter()
                .filter_map(|f| f.painted.get().map(|painted| signed_ms(painted, f.at)))
                .collect();
            let inactive = frames.iter().filter(|f| !f.active).count();
            let counted = intervals.len();
            println!(
                "{segment:?}: frames={} inactive={inactive} intervals={counted} {} late={late} render-to-paint {}",
                frames.len(),
                spread(intervals),
                spread(work)
            );
        }
        self.print_reversal();
        if let Some(path) = &self.log
            && let Err(error) = self.write_log(path)
        {
            eprintln!("frame log {}: {error}", path.display());
        }
        cx.quit();
    }

    fn print_reversal(&self) {
        let Some(reversed) = self.reversed_at else {
            return;
        };
        let trace: Vec<(f64, f32)> = self
            .frames
            .iter()
            .filter(|f| f.segment == Segment::Reversal)
            .filter_map(|f| f.bar_y.map(|y| (signed_ms(f.at, reversed), y)))
            .collect();
        let pivot = trace
            .iter()
            .position(|(ms, _)| *ms >= 0.0)
            .unwrap_or(trace.len());
        let first = pivot.saturating_sub(TRACE_BEFORE);
        let shown = trace
            .get(first..(pivot + TRACE_AFTER).min(trace.len()))
            .unwrap_or(&[]);
        println!("reversal trace, t relative to the retarget back to row 1:");
        for pair in shown.windows(2) {
            if let [(_, before), (ms, y)] = pair {
                let mark = if *ms >= 0.0 { "after " } else { "before" };
                println!(
                    "  {mark} t={ms:8.2}ms y={y:8.2} step={:6.2}",
                    (y - before).abs()
                );
            }
        }
    }

    fn write_log(&self, path: &Path) -> std::io::Result<()> {
        let mut out = BufWriter::new(File::create(path)?);
        let Some(origin) = self.frames.first().map(|f| f.at) else {
            return out.flush();
        };
        writeln!(out, "t_ms\tsegment\tanimating\tactive\tbar_y\tpaint_ms")?;
        for frame in &self.frames {
            writeln!(
                out,
                "{:.3}\t{:?}\t{}\t{}\t{}\t{}",
                signed_ms(frame.at, origin),
                frame.segment,
                frame.animating,
                frame.active,
                frame.bar_y.map_or(String::from("-"), |y| format!("{y:.3}")),
                frame.painted.get().map_or(String::from("-"), |at| format!(
                    "{:.3}",
                    signed_ms(at, frame.at)
                ))
            )?;
        }
        out.flush()
    }
}

fn signed_ms(at: Instant, origin: Instant) -> f64 {
    match at.checked_duration_since(origin) {
        Some(after) => after.as_secs_f64() * 1000.0,
        None => -(origin.duration_since(at).as_secs_f64() * 1000.0),
    }
}

fn spread(mut values: Vec<f64>) -> String {
    values.sort_by(f64::total_cmp);
    let at = |q: f64| {
        let last = values.len().saturating_sub(1) as f64;
        values
            .get((last * q).round() as usize)
            .copied()
            .unwrap_or(0.0)
    };
    format!(
        "p50={:.2}ms p95={:.2}ms max={:.2}ms",
        at(0.5),
        at(0.95),
        values.last().copied().unwrap_or(0.0)
    )
}

async fn step(
    this: &WeakEntity<Demo>,
    cx: &mut AsyncApp,
    wait: Duration,
    act: impl FnOnce(&mut Demo, &mut Context<Demo>),
) -> bool {
    let alive = this.update(cx, act).is_ok();
    cx.background_executor().timer(wait).await;
    alive
}

impl Render for Demo {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let now = Instant::now();
        let bar = self.glide.sample(now);
        let animating = self.glide.moving() || self.spinners;
        if self.glide.moving() {
            window.request_animation_frame();
        }
        let painted = Rc::new(Cell::new(None));
        if self.log.is_some() {
            self.frames.push(Frame {
                at: now,
                segment: self.segment,
                animating,
                active: window.is_window_active(),
                bar_y: bar.map(|(bounds, _)| bounds.origin.y.as_f32()),
                painted: painted.clone(),
            });
        }
        let entity = cx.entity().downgrade();
        let rows = div()
            .on_children_prepainted(move |bounds, _, cx| {
                entity
                    .update(cx, |demo, _| {
                        if demo.rows != bounds {
                            demo.rows = bounds;
                        }
                    })
                    .ok();
            })
            .id("rows")
            .flex()
            .flex_col()
            .gap(px(1.0))
            .w(px(LIST_WIDTH))
            .on_mouse_move(cx.listener(|demo, event: &MouseMoveEvent, _, cx| {
                demo.point(event.position, cx);
            }))
            .on_hover(cx.listener(|demo, hovered: &bool, _, cx| {
                if !*hovered {
                    demo.leave(cx);
                }
            }))
            .children((0..ROWS).map(|row| {
                div()
                    .h(px(ROW_HEIGHT))
                    .px_2()
                    .flex()
                    .items_center()
                    .child(format!("Row {}", row + 1))
            }));
        let spinners = self.spinners.then(|| {
            div()
                .flex()
                .flex_wrap()
                .gap_2()
                .w(px((SPINNER_PX + 8.0) * SPINNER_COLUMNS))
                .children((0..SPINNERS).map(|ix| {
                    svg()
                        .size(px(SPINNER_PX))
                        .path(RING)
                        .text_color(hsla(0.38, 0.6, 0.5, 1.0))
                        .with_animation(("spin", ix), spin(SPIN_PERIOD), |ring, t| {
                            ring.with_transformation(Transformation::rotate(percentage(t)))
                        })
                }))
        });
        div()
            .id("demo")
            .track_focus(&self.focus)
            .on_key_down(cx.listener(|demo, event: &KeyDownEvent, _, cx| {
                match event.keystroke.key.as_str() {
                    "k" => demo.glide.set_kind(match demo.glide.kind() {
                        GlideKind::Eased => GlideKind::Spring,
                        GlideKind::Spring => GlideKind::Eased,
                    }),
                    "s" => demo.spinners = !demo.spinners,
                    "q" => {
                        demo.finish(cx);
                        return;
                    }
                    _ => return,
                }
                cx.notify();
            }))
            .relative()
            .size_full()
            .flex()
            .gap_6()
            .p_6()
            .bg(hsla(0.0, 0.0, 0.08, 1.0))
            .text_color(hsla(0.0, 0.0, 0.9, 1.0))
            .text_sm()
            .child(
                canvas(
                    move |_, _, _| bar,
                    move |_, bar, window, _| {
                        painted.set(Some(Instant::now()));
                        if let Some((bounds, shade)) = bar {
                            window.paint_quad(
                                fill(bounds, hsla(0.0, 0.0, 1.0, 0.08 * shade))
                                    .corner_radii(px(ROW_RADIUS)),
                            );
                        }
                    },
                )
                .absolute()
                .size_full(),
            )
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .child(format!("{} (k switches)", self.glide.kind().label()))
                    .child(rows),
            )
            .children(spinners)
    }
}

fn main() -> ExitCode {
    let options = match parse(std::env::args().skip(1)) {
        Ok(options) => options,
        Err(message) => {
            eprintln!("{message}");
            return ExitCode::FAILURE;
        }
    };
    gpui_platform::application()
        .with_assets(Assets)
        .run(move |cx: &mut App| {
            println!(
                "reduced_motion={} kind={:?}",
                system_reduced_motion(),
                options.kind
            );
            cx.on_window_closed(|cx, _| cx.quit()).detach();
            let window_options = WindowOptions {
                titlebar: Some(TitlebarOptions {
                    title: Some(TITLE.into()),
                    ..Default::default()
                }),
                window_bounds: Some(WindowBounds::Windowed(Bounds::centered(
                    None,
                    size(px(680.0), px(420.0)),
                    cx,
                ))),
                focus: true,
                ..Default::default()
            };
            let opened = cx.open_window(window_options, |window, cx| {
                cx.new(|cx| {
                    let focus = cx.focus_handle();
                    focus.focus(window, cx);
                    Demo {
                        glide: Glide::new(options.kind),
                        rows: Vec::new(),
                        hovered: None,
                        spinners: false,
                        segment: Segment::Idle,
                        log: options.log.clone(),
                        frames: Vec::new(),
                        reversed_at: None,
                        focus,
                        _script: Demo::script(options.script, cx),
                    }
                })
            });
            if let Err(error) = opened {
                eprintln!("motion demo could not open its window: {error:#}");
                cx.quit();
            }
        });
    ExitCode::SUCCESS
}
