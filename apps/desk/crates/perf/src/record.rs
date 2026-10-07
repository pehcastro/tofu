mod report;

use std::env;
use std::fs::{self, File};
use std::io::{self, BufWriter, Write};
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use gpui::{FrameEvent, FrameTiming, MouseButton, Pixels, Point, PresentTiming, Window};

use crate::capture::{self, Capture, CaptureError, Shot, Shots};
use crate::cells::Grid;
use crate::frames::ms;
use crate::limits::RECORD_DIR;
use crate::span::SpanRecord;
use crate::trace::Recording;

#[derive(Clone, Copy, PartialEq, Eq)]
pub(crate) enum Probe {
    Layout,
    Prepaint,
    Paint,
}

#[derive(Clone, Copy)]
pub(crate) enum InputKind {
    Down,
    Up,
    Move,
    Scroll,
    Key,
}

impl InputKind {
    fn label(self) -> &'static str {
        match self {
            Self::Down => "down",
            Self::Up => "up",
            Self::Move => "move",
            Self::Scroll => "scroll",
            Self::Key => "key",
        }
    }
}

pub(crate) struct Input {
    pub at: Instant,
    pub kind: InputKind,
    pub position: Option<Point<Pixels>>,
    pub button: Option<MouseButton>,
    pub key: Option<String>,
    pub delta: Option<Point<f32>>,
}

impl Input {
    fn button_label(&self) -> Option<String> {
        self.button
            .map(|button| format!("{button:?}").to_lowercase())
    }
}

#[derive(Clone, Copy)]
pub(crate) enum Stop {
    Key,
    Limit,
}

pub(crate) struct Recorder {
    pub started: Instant,
    name: String,
    refresh_hz: Option<u32>,
    capture: Option<Capture>,
    shots: Shots,
    problems: Vec<CaptureError>,
    input: Vec<Input>,
    probes: Vec<(Probe, Instant)>,
    events: Vec<FrameEvent>,
    trace: Recording,
    ended: Option<(Instant, Stop)>,
}

struct Row {
    draw: FrameTiming,
    present: Option<PresentTiming>,
}

impl Row {
    fn shown(&self) -> Instant {
        self.present
            .map_or(self.draw.draw_end, |present| present.present_end)
    }

    fn animation_interval(&self) -> Option<Duration> {
        self.present?.animation_interval
    }
}

impl Recorder {
    pub fn start(capture: bool, window: &Window) -> Self {
        let (capture, problems) = match capture.then(|| Capture::start(window)) {
            Some(Ok(capture)) => (Some(capture), Vec::new()),
            Some(Err(error)) => (None, vec![error]),
            None => (None, Vec::new()),
        };
        Recorder {
            started: Instant::now(),
            name: chrono::Local::now().format("%Y%m%d-%H%M%S").to_string(),
            refresh_hz: capture::refresh_hz(window),
            capture,
            shots: Shots::default(),
            problems,
            input: Vec::new(),
            probes: Vec::new(),
            events: Vec::new(),
            trace: Recording::new(),
            ended: None,
        }
    }

    pub fn extend(
        &mut self,
        spans: &[SpanRecord],
        frames: &[FrameTiming],
        dropped: u64,
        events: &[FrameEvent],
    ) {
        self.trace.extend(spans, frames, dropped);
        self.events.extend_from_slice(events);
    }

    pub fn probe(&mut self, probe: Probe) {
        self.probes.push((probe, Instant::now()));
    }

    pub fn input(&mut self, input: Input) {
        self.input.push(input);
    }

    pub fn finish(mut self, stop: Stop) -> Self {
        self.ended = Some((Instant::now(), stop));
        if let Some(capture) = self.capture.take() {
            let (shots, problem) = capture.stop();
            self.shots = shots;
            self.problems.extend(problem);
        }
        self
    }

    pub fn write(self) -> io::Result<PathBuf> {
        let folder = env::current_exe()?
            .parent()
            .and_then(Path::parent)
            .map(|target| target.join(RECORD_DIR).join(&self.name))
            .ok_or_else(|| io::Error::other("the executable has no target directory above it"))?;
        fs::create_dir_all(folder.join("frames"))?;
        let rows = self.rows();
        let shown: Vec<Instant> = rows.iter().map(Row::shown).collect();
        let shots = &self.shots.taken;
        let shot_rows: Vec<Option<usize>> = shots
            .iter()
            .map(|shot| shown.partition_point(|at| *at <= shot.at).checked_sub(1))
            .collect();
        let grids: Vec<Grid> = shots.iter().map(Grid::of).collect();
        self.write_frames(&folder, &rows, &shot_rows)?;
        self.write_input(&folder)?;
        let changed = self.write_drawn(&folder, &grids)?;
        self.trace.write_chrome_trace(&folder.join("trace.json"))?;
        let mut rgba = Vec::new();
        for (index, shot) in shots.iter().enumerate() {
            write_png(
                &folder.join("frames").join(png_name(index)),
                shot,
                &mut rgba,
            )?;
        }
        let summary = self
            .summary(&rows, &shot_rows, &grids, &changed)
            .map_err(io::Error::other)?;
        fs::write(folder.join("summary.txt"), summary)?;
        Ok(folder)
    }

    fn at(&self, instant: Instant) -> f64 {
        ms(instant.saturating_duration_since(self.started))
    }

    fn rows(&self) -> Vec<Row> {
        let mut rows: Vec<Row> = Vec::new();
        for event in &self.events {
            match event {
                FrameEvent::Draw(draw) => rows.push(Row {
                    draw: *draw,
                    present: None,
                }),
                FrameEvent::Present(present) => {
                    if let Some(row) = rows.last_mut().filter(|row| row.present.is_none()) {
                        row.present = Some(*present);
                    }
                }
            }
        }
        rows
    }

    fn probe_in(&self, probe: Probe, row: &Row) -> Option<Instant> {
        let from = self
            .probes
            .partition_point(|(_, at)| *at < row.draw.draw_start);
        self.probes
            .get(from..)?
            .iter()
            .take_while(|(_, at)| *at <= row.draw.draw_end)
            .find(|(kind, _)| *kind == probe)
            .map(|(_, at)| *at)
    }
}

fn png_name(index: usize) -> String {
    format!("{:05}.png", index + 1)
}

fn write_png(path: &Path, shot: &Shot, rgba: &mut Vec<u8>) -> io::Result<()> {
    rgba.clear();
    rgba.extend(
        shot.bgra
            .as_chunks::<4>()
            .0
            .iter()
            .flat_map(|&[b, g, r, a]| [r, g, b, a]),
    );
    let width = u32::try_from(shot.width).map_err(io::Error::other)?;
    let height = u32::try_from(shot.height).map_err(io::Error::other)?;
    let mut file = BufWriter::new(File::create(path)?);
    let mut encoder = png::Encoder::new(&mut file, width, height);
    encoder.set_color(png::ColorType::Rgba);
    encoder.set_depth(png::BitDepth::Eight);
    encoder.set_compression(png::Compression::Fast);
    let mut writer = encoder.write_header().map_err(io::Error::other)?;
    writer.write_image_data(rgba).map_err(io::Error::other)?;
    writer.finish().map_err(io::Error::other)?;
    file.flush()
}
