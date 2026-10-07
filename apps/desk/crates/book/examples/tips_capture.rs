#[path = "../src/book/mod.rs"]
mod book;
#[path = "../src/catalog.rs"]
mod catalog;
#[path = "../src/data.rs"]
mod data;
#[path = "../src/themes.rs"]
mod themes;

mod headless;

use std::collections::{BTreeMap, HashSet};
use std::env;
use std::path::PathBuf;
use std::process::ExitCode;
use std::sync::Arc;
use std::time::Duration;

use desk_ui::components::tooltip::tooltip_debug;
use desk_ui::metrics::WINDOW_WIDTH;
use desk_ui::theme::Mode;
use gpui::{
    Bounds, EntityId, Modifiers, MouseMoveEvent, Pixels, PlatformInput, Point, point, px, size,
};
use image::RgbaImage;

use catalog::Page;
use headless::{SETTLE, Session, Setup, platform, save};
use themes::{Choice, DEFAULT_THEME};

pub struct Launch {
    pub page: Page,
    pub theme: usize,
    pub themes: Vec<Choice>,
    pub mode: Option<Mode>,
}

const USAGE: &str = "usage: tips_capture <out dir> <label> [--theme <name>] [--mode light|dark]";
const PAGE: &str = "tooltips";
const CHANGED: u8 = 8;
const FAINT: u8 = 6;
const CAPTURE_HEIGHT: f32 = 1100.0;
const OPEN_WAIT: Duration = Duration::from_millis(700);
const COOL_WAIT: Duration = Duration::from_millis(700);
const STILL: Duration = Duration::from_millis(200);
const SCAN_STEP: f32 = 0.25;
const SCAN_PAD: f32 = 40.0;
const REACH_ABOVE: f32 = 220.0;
const BODY_SHARE: f32 = 0.6;
const MAP_PAD: f32 = 6.0;

struct Target {
    key: &'static str,
    name: &'static str,
    aim: f32,
}

const TARGETS: [Target; 5] = [
    Target {
        key: "tip-icon-9",
        name: "icon-close",
        aim: 0.5,
    },
    Target {
        key: "tip-word-0",
        name: "word-jev",
        aim: 0.5,
    },
    Target {
        key: "tip-wrapped",
        name: "phrase-line-2",
        aim: 0.75,
    },
    Target {
        key: "tip-face-3",
        name: "avatar-research-1",
        aim: 0.5,
    },
    Target {
        key: "card-name-2",
        name: "card-explore-1",
        aim: 0.5,
    },
];

struct Args {
    out: PathBuf,
    label: String,
    theme: String,
    mode: Option<Mode>,
}

fn parse(args: &[String]) -> Result<Args, String> {
    let [out, label, rest @ ..] = args else {
        return Err(USAGE.to_owned());
    };
    let mut parsed = Args {
        out: PathBuf::from(out),
        label: label.clone(),
        theme: DEFAULT_THEME.to_owned(),
        mode: None,
    };
    let mut rest = rest;
    while let [flag, value, tail @ ..] = rest {
        match flag.as_str() {
            "--theme" => parsed.theme = value.clone(),
            "--mode" => parsed.mode = Some(themes::parse_mode(value)?),
            _ => return Err(USAGE.to_owned()),
        }
        rest = tail;
    }
    match rest.is_empty() {
        true => Ok(parsed),
        false => Err(USAGE.to_owned()),
    }
}

fn open(args: &Args) -> Result<Session, String> {
    let page = Page::parse(PAGE).ok_or_else(|| format!("no page {PAGE}"))?;
    let (text, renderer) = platform()?;
    let setup = Setup {
        page,
        theme: &args.theme,
        mode: args.mode,
        size: size(px(WINDOW_WIDTH), px(CAPTURE_HEIGHT)),
    };
    Session::open(&setup, Arc::new(text), Box::new(renderer))
}

impl Session {
    fn book(&mut self) -> Result<EntityId, String> {
        self.cx
            .update_window(self.window, |root, _, _| root.entity_id())
            .map_err(|error| format!("the book view is gone: {error:#}"))
    }

    fn point_at(&mut self, at: Point<Pixels>) -> Result<Vec<String>, String> {
        let book = self.book()?;
        self.with(|window, cx| {
            window.dispatch_event(
                PlatformInput::MouseMove(MouseMoveEvent {
                    position: at,
                    modifiers: Modifiers::none(),
                    pressed_button: None,
                }),
                cx,
            );
            tooltip_debug(book, window, cx).hovered
        })
    }

    fn wait(&mut self, span: Duration) -> Result<(), String> {
        self.advance(span);
        self.settle(SETTLE)
    }

    fn hint(&mut self, key: &str) -> Result<Bounds<Pixels>, String> {
        let book = self.book()?;
        self.with(|window, cx| tooltip_debug(book, window, cx).targets)?
            .into_iter()
            .find(|(found, _)| found == key)
            .map(|(_, rect)| rect)
            .ok_or_else(|| format!("{key} is not on the page"))
    }

    fn hit_rect(&mut self, key: &str) -> Result<Hit, String> {
        let hint = self.hint(key)?;
        let rows = scan(
            hint.top().as_f32() - hint.size.height.as_f32() - SCAN_PAD,
            hint.bottom().as_f32() + SCAN_PAD,
        );
        let mut ys = Vec::new();
        for y in rows {
            if self
                .point_at(point(hint.center().x, px(y)))?
                .iter()
                .any(|k| k == key)
            {
                ys.push(y);
            }
        }
        let (Some(&top), Some(&last)) = (ys.first(), ys.last()) else {
            return Err(format!("no point near {key} hovers it"));
        };
        let middle = px((top + last) / 2.0);
        let mut xs = Vec::new();
        for x in scan(
            hint.left().as_f32() - SCAN_PAD,
            hint.right().as_f32() + SCAN_PAD,
        ) {
            if self
                .point_at(point(px(x), middle))?
                .iter()
                .any(|k| k == key)
            {
                xs.push(x);
            }
        }
        let (Some(&left), Some(&right)) = (xs.first(), xs.last()) else {
            return Err(format!("no row through {key} hovers it"));
        };
        Ok(Hit {
            left,
            right: right + SCAN_STEP,
            top,
            bottom: last + SCAN_STEP,
            hint,
        })
    }
}

fn scan(from: f32, to: f32) -> Vec<f32> {
    let steps = ((to - from) / SCAN_STEP).max(0.0) as usize;
    (0..=steps).map(|at| from + at as f32 * SCAN_STEP).collect()
}

struct Hit {
    left: f32,
    right: f32,
    top: f32,
    bottom: f32,
    hint: Bounds<Pixels>,
}

impl Hit {
    fn centre(&self) -> f32 {
        (self.left + self.right) / 2.0
    }
}

struct Painted {
    box_left: f32,
    box_right: f32,
    box_top: f32,
    box_bottom: f32,
    tip: f32,
    arrow_rows: Vec<(f32, f32)>,
    target_top: f32,
    box_fill: [u8; 3],
    arrow_fill: Option<[u8; 3]>,
}

fn differ(a: &RgbaImage, b: &RgbaImage, x: u32, y: u32) -> u8 {
    match (a.get_pixel_checked(x, y), b.get_pixel_checked(x, y)) {
        (Some(a), Some(b)) => (0..3)
            .map(|channel| a.0[channel].abs_diff(b.0[channel]))
            .max()
            .unwrap_or(0),
        _ => 0,
    }
}

fn device(value: f32, scale: f32) -> u32 {
    (value * scale).round().max(0.0) as u32
}

fn measure(shown: &RgbaImage, still: &RgbaImage, hit: &Hit, scale: f32) -> Result<Painted, String> {
    let (column_left, column_right) = (device(hit.left, scale), device(hit.right, scale));
    let reference = device(hit.top, scale).saturating_sub(1);
    let target_top = (device(hit.top, scale)..device(hit.bottom, scale))
        .find(|&y| {
            (column_left..column_right).any(|x| {
                let (Some(here), Some(above)) = (
                    still.get_pixel_checked(x, y),
                    still.get_pixel_checked(x, reference),
                ) else {
                    return false;
                };
                (0..3).any(|channel| here.0[channel].abs_diff(above.0[channel]) > FAINT)
            })
        })
        .unwrap_or(device(hit.top, scale));
    let column = device(hit.centre(), scale);
    let seed = (device(hit.top - REACH_ABOVE, scale)..target_top)
        .rev()
        .find(|&y| differ(shown, still, column, y) > CHANGED)
        .ok_or("no box above the target in the capture")?;
    let mut rows: BTreeMap<u32, (u32, u32)> = BTreeMap::new();
    let mut seen = HashSet::from([(column, seed)]);
    let mut pending = vec![(column, seed)];
    while let Some((x, y)) = pending.pop() {
        let span = rows.entry(y).or_insert((x, x));
        *span = (span.0.min(x), span.1.max(x));
        let around = [
            (x.wrapping_sub(1), y),
            (x + 1, y),
            (x, y.wrapping_sub(1)),
            (x, y + 1),
        ];
        for next in around {
            if differ(shown, still, next.0, next.1) > CHANGED && seen.insert(next) {
                pending.push(next);
            }
        }
    }
    let spans: Vec<(u32, u32, u32)> = rows.into_iter().map(|(y, (a, b))| (y, a, b)).collect();
    let widest = spans.iter().map(|&(_, a, b)| b - a).max().unwrap_or(0);
    let body: Vec<&(u32, u32, u32)> = spans
        .iter()
        .filter(|&&(_, a, b)| (b - a) as f32 >= widest as f32 * BODY_SHARE)
        .collect();
    let (Some(first), Some(last)) = (body.first(), body.last()) else {
        return Err("no box in the capture".to_owned());
    };
    let box_left = body.iter().map(|&&(_, a, _)| a).min().unwrap_or(0);
    let box_right = body.iter().map(|&&(_, _, b)| b).max().unwrap_or(0) + 1;
    let arrow: Vec<&(u32, u32, u32)> = spans.iter().filter(|&&(y, _, _)| y > last.0).collect();
    let tip = arrow.last().map_or(last.0, |&&(y, _, _)| y) + 1;
    let logical = |value: u32| value as f32 / scale;
    let colour = |x: u32, y: u32| {
        shown
            .get_pixel_checked(x, y)
            .map_or([0; 3], |pixel| [pixel.0[0], pixel.0[1], pixel.0[2]])
    };
    let inset = scale.ceil() as u32 * 3;
    let box_fill = colour(box_left + inset, (first.0 + last.0) / 2);
    let arrow_fill = arrow
        .get(arrow.len() / 3)
        .map(|&&(y, a, b)| colour((a + b) / 2, y));
    Ok(Painted {
        box_fill,
        arrow_fill,
        box_left: logical(box_left),
        box_right: logical(box_right),
        box_top: logical(first.0),
        box_bottom: logical(last.0 + 1),
        tip: logical(tip),
        arrow_rows: arrow
            .iter()
            .map(|&&(_, a, b)| (logical(a), logical(b + 1)))
            .collect(),
        target_top: logical(target_top),
    })
}

fn shade(shown: &RgbaImage, x: u32, y: u32) -> char {
    const RAMP: [char; 10] = [' ', '.', ':', '-', '=', '+', '*', '#', '%', '@'];
    let level = shown.get_pixel_checked(x, y).map_or(0, |pixel| {
        (u32::from(pixel.0[0]) + u32::from(pixel.0[1]) + u32::from(pixel.0[2])) / 3
    });
    RAMP.get((level * 10 / 256) as usize)
        .copied()
        .unwrap_or(' ')
}

fn map(shown: &RgbaImage, painted: &Painted, hit: &Hit, scale: f32) -> String {
    let left = device(painted.box_left.min(hit.left) - MAP_PAD, scale);
    let right = device(painted.box_right.max(hit.right) + MAP_PAD, scale);
    let top = device(painted.box_top.min(hit.top) - MAP_PAD, scale);
    let bottom = device(hit.bottom.max(painted.tip) + MAP_PAD, scale);
    let step = scale.max(1.0) as usize;
    (top..bottom)
        .step_by(step)
        .map(|y| {
            let row: String = (left..right)
                .step_by(step)
                .map(|x| shade(shown, x, y))
                .collect();
            format!("{:7.1} |{row}|", y as f32 / scale)
        })
        .collect::<Vec<_>>()
        .join("\n")
}

fn capture_one(
    session: &mut Session,
    target: &Target,
    args: &Args,
) -> Result<(String, String), String> {
    let away = point(px(WINDOW_WIDTH - 2.0), px(CAPTURE_HEIGHT - 2.0));
    session.point_at(away)?;
    session.wait(COOL_WAIT)?;
    let hit = session.hit_rect(target.key)?;
    session.point_at(away)?;
    session.wait(COOL_WAIT)?;
    let aim = point(
        px(hit.centre()),
        px(hit.top + (hit.bottom - hit.top) * target.aim),
    );
    session.point_at(aim)?;
    session.settle(STILL)?;
    let still = session.capture()?;
    session.wait(OPEN_WAIT)?;
    let shown = session.capture()?;
    let file = args.out.join(format!("{}-{}.png", args.label, target.name));
    save(&shown, &file)?;
    let painted = measure(&shown, &still, &hit, session.scale)?;
    let gap = painted.target_top - painted.tip;
    let box_centre = (painted.box_left + painted.box_right) / 2.0;
    let arrow = match painted.arrow_rows.is_empty() {
        true => "none".to_owned(),
        false => painted
            .arrow_rows
            .iter()
            .map(|(a, b)| format!("{:.1}", b - a))
            .collect::<Vec<_>>()
            .join(","),
    };
    let summary = format!(
        "{}: gap {gap:.1} px from arrow tip {:.1} to target painted top {:.1} | box centre {box_centre:.1} target centre {:.1} off by {:+.1} px | arrow rows {} widths [{arrow}] fill {:?} | box fill {:?}",
        target.name,
        painted.tip,
        painted.target_top,
        hit.centre(),
        box_centre - hit.centre(),
        painted.arrow_rows.len(),
        painted.arrow_fill,
        painted.box_fill,
    );
    let detail = format!(
        "{} ({}): {}\n  pointer {:.1},{:.1} | target hit rect x {:.1}..{:.1} y {:.1}..{:.1} | probe rect y {:.1}..{:.1}\n  box x {:.1}..{:.1} y {:.1}..{:.1}\n{}",
        target.name,
        target.key,
        file.display(),
        aim.x.as_f32(),
        aim.y.as_f32(),
        hit.left,
        hit.right,
        hit.top,
        hit.bottom,
        hit.hint.top().as_f32(),
        hit.hint.bottom().as_f32(),
        painted.box_left,
        painted.box_right,
        painted.box_top,
        painted.box_bottom,
        map(&shown, &painted, &hit, session.scale),
    );
    Ok((summary, detail))
}

fn run(args: &Args) -> Result<(), String> {
    std::fs::create_dir_all(&args.out)
        .map_err(|error| format!("{} was not made: {error}", args.out.display()))?;
    let mut session = open(args)?;
    println!(
        "headless {WINDOW_WIDTH}x{CAPTURE_HEIGHT} at scale {}, theme {}",
        session.scale, args.theme
    );
    let page = session.capture()?;
    save(&page, &args.out.join(format!("{}-page.png", args.label)))?;
    let mut details = Vec::new();
    for target in &TARGETS {
        let (summary, detail) = capture_one(&mut session, target, args)?;
        println!("{summary}");
        details.push(detail);
    }
    println!("\n{}", details.join("\n"));
    Ok(())
}

fn main() -> ExitCode {
    let args: Vec<String> = env::args().skip(1).collect();
    match parse(&args).and_then(|args| run(&args)) {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("{error}");
            ExitCode::FAILURE
        }
    }
}
