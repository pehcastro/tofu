use std::borrow::Cow;
use std::process::ExitCode;
use std::time::{Duration, Instant};

use desk_motion::{Ease, Phase, Presence, reduced_motion, spin};
use gpui::{
    AnimationExt as _, App, AssetSource, Bounds, Context, Entity, MotionDurationExt, SharedString,
    StyleRefinement, Task, TitlebarOptions, Transformation, Window, WindowBounds, WindowOptions,
    div, hsla, millis, percentage, prelude::*, px, size, svg,
};

const TITLE: &str = "Motion demo";
const RING: &str = "ring.svg";
const RING_SVG: &[u8] = br##"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10" fill="none" stroke="#fff" stroke-opacity="0.2" stroke-width="2"/><path d="M12 2a10 10 0 0 1 10 10" fill="none" stroke="#fff" stroke-width="2"/></svg>"##;
const HOVER: Duration = millis(100);
const ENTER: Duration = millis(200);
const EXIT: Duration = millis(140);
const RING_PERIOD: Duration = millis(1000);
const RISE_PX: f32 = 4.0;
const HOLD: Duration = millis(2000);
const MAX_SLOW: u32 = 100;

struct Assets;

impl AssetSource for Assets {
    fn load(&self, path: &str) -> gpui::Result<Option<Cow<'static, [u8]>>> {
        Ok((path == RING).then_some(Cow::Borrowed(RING_SVG)))
    }

    fn list(&self, _path: &str) -> gpui::Result<Vec<SharedString>> {
        Ok(vec![RING.into()])
    }
}

struct Options {
    auto: bool,
    slow: u32,
}

fn parse(mut args: impl Iterator<Item = String>) -> Result<Options, String> {
    let mut options = Options {
        auto: false,
        slow: 1,
    };
    while let Some(arg) = args.next() {
        match arg.as_str() {
            "--auto" => options.auto = true,
            "--slow" => {
                let value = args.next().ok_or("--slow needs a factor")?;
                options.slow = value
                    .parse()
                    .ok()
                    .filter(|n| (1..=MAX_SLOW).contains(n))
                    .ok_or(format!(
                        "--slow {value}: not an integer from 1 to {MAX_SLOW}"
                    ))?;
            }
            other => {
                return Err(format!(
                    "unknown argument {other}; usage: motion_demo [--auto] [--slow N]"
                ));
            }
        }
    }
    Ok(options)
}

struct Panel {
    menu: Presence,
    enter: Duration,
    exit: Duration,
    reduced: bool,
    log: Option<Instant>,
    _script: Option<Task<()>>,
}

impl Panel {
    fn toggle(&mut self, open: bool) {
        let now = Instant::now();
        if let Some(started) = self.log {
            let ms = now.duration_since(started).as_millis();
            println!(
                "t={ms} set_open={open} at progress={:.3}",
                self.menu.progress(now)
            );
        }
        self.menu.set_open(open, now);
    }

    fn script(&self, cx: &mut Context<Self>) -> Task<()> {
        let steps = [
            (true, self.enter / 2),
            (false, self.exit + HOLD),
            (true, self.enter + HOLD),
            (false, self.exit + HOLD),
        ];
        cx.spawn(async move |this, cx| {
            loop {
                for (open, wait) in steps {
                    if this
                        .update(cx, |panel, cx| {
                            panel.toggle(open);
                            cx.notify();
                        })
                        .is_err()
                    {
                        return;
                    }
                    cx.background_executor().timer(wait).await;
                }
            }
        })
    }
}

impl Render for Panel {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let now = Instant::now();
        let phase = self.menu.phase(now);
        let progress = self.menu.progress(now);
        let rise = if self.reduced {
            0.0
        } else {
            (1.0 - progress) * RISE_PX
        };
        if matches!(phase, Phase::Opening | Phase::Closing) {
            window.request_animation_frame();
            if let Some(started) = self.log {
                let ms = now.duration_since(started).as_millis();
                println!("t={ms} {phase:?} progress={progress:.3} rise={rise:.2}");
            }
        }
        let open = matches!(phase, Phase::Opening | Phase::Open);
        div()
            .size_full()
            .flex()
            .flex_col()
            .gap_4()
            .p_6()
            .bg(hsla(0.0, 0.0, 0.08, 1.0))
            .text_color(hsla(0.0, 0.0, 0.9, 1.0))
            .text_sm()
            .child(
                div()
                    .id("row")
                    .px_3()
                    .py_2()
                    .rounded_md()
                    .bg(hsla(0.0, 0.0, 1.0, 0.0))
                    .transitions(|t| t.bg(HOVER.with_easing(|t| Ease::Standard.apply(t))))
                    .hover(|s| s.bg(hsla(0.0, 0.0, 1.0, 0.08)))
                    .child("Hover this row"),
            )
            .child(
                div()
                    .id("menu-button")
                    .w(px(64.0))
                    .px_3()
                    .py_1()
                    .rounded_md()
                    .bg(hsla(0.0, 0.0, 1.0, 0.1))
                    .cursor_pointer()
                    .on_click(cx.listener(move |panel, _, _, cx| {
                        panel.toggle(!open);
                        cx.notify();
                    }))
                    .child("Menu"),
            )
            .child(div().h(px(120.0)).children(self.menu.mounted(now).then(|| {
                div()
                    .relative()
                    .top(px(rise))
                    .w(px(180.0))
                    .p_2()
                    .rounded_lg()
                    .bg(hsla(0.0, 0.0, 0.16, 1.0))
                    .opacity(progress)
                    .child("Rename")
                    .child("Duplicate")
                    .child("Close")
            })))
    }
}

struct Demo {
    panel: Entity<Panel>,
}

impl Render for Demo {
    fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .relative()
            .size_full()
            .child(
                self.panel
                    .clone()
                    .cached(StyleRefinement::default().size_full()),
            )
            .child(
                svg()
                    .absolute()
                    .top_6()
                    .right_6()
                    .size(px(28.0))
                    .path(RING)
                    .text_color(hsla(0.38, 0.6, 0.5, 1.0))
                    .with_animation("ring", spin(RING_PERIOD), |ring, t| {
                        ring.with_transformation(Transformation::rotate(percentage(t)))
                    }),
            )
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
            let reduced = reduced_motion();
            println!("reduced_motion={reduced}");
            cx.on_window_closed(|cx, _| cx.quit()).detach();
            let window_options = WindowOptions {
                titlebar: Some(TitlebarOptions {
                    title: Some(TITLE.into()),
                    ..Default::default()
                }),
                window_bounds: Some(WindowBounds::Windowed(Bounds::centered(
                    None,
                    size(px(420.0), px(300.0)),
                    cx,
                ))),
                focus: false,
                ..Default::default()
            };
            let opened = cx.open_window(window_options, |_, cx| {
                let panel = cx.new(|cx| {
                    let mut panel = Panel {
                        menu: Presence::new(ENTER * options.slow, EXIT * options.slow),
                        enter: ENTER * options.slow,
                        exit: EXIT * options.slow,
                        reduced,
                        log: options.auto.then(Instant::now),
                        _script: None,
                    };
                    if options.auto {
                        panel._script = Some(panel.script(cx));
                    }
                    panel
                });
                cx.new(|_| Demo { panel })
            });
            if let Err(error) = opened {
                eprintln!("motion demo could not open its window: {error:#}");
                cx.quit();
            }
        });
    ExitCode::SUCCESS
}
