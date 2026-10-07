use std::fmt::{self, Write as _};
use std::fs::File;
use std::io::{self, BufWriter, Write};
use std::path::Path;
use std::time::{Duration, Instant};

use super::{Input, Recorder, Row, Stop, png_name};
use crate::cells::{self, Flash, Grid};
use crate::frames::{FrameSummary, ms};
use crate::limits::{FLASH_CELL, FLASH_LUMINANCE, FLASH_RETURN_FRAMES, SLOW_FRAME_FACTOR};

use super::Probe;

fn cell(value: Option<f64>) -> String {
    value.map_or_else(|| "-".to_owned(), |value| format!("{value:.3}"))
}

impl Recorder {
    pub(super) fn write_frames(
        &self,
        folder: &Path,
        rows: &[Row],
        shot_rows: &[Option<usize>],
    ) -> io::Result<()> {
        let mut file = BufWriter::new(File::create(folder.join("frames.tsv"))?);
        writeln!(
            file,
            "frame\tt_ms\tinterval_ms\tlayout_ms\tprepaint_ms\tpaint_ms\tpresent_ms\tanimating\tpng"
        )?;
        let mut previous: Option<Instant> = None;
        for (index, row) in rows.iter().enumerate() {
            let shown = row.shown();
            let layout = self.probe_in(Probe::Layout, row);
            let prepaint = self.probe_in(Probe::Prepaint, row);
            let paint = self.probe_in(Probe::Paint, row);
            let between = |from: Option<Instant>, to: Option<Instant>| {
                Some(ms(to?.saturating_duration_since(from?)))
            };
            let png = shot_rows
                .iter()
                .position(|shot_row| *shot_row == Some(index))
                .map_or_else(|| "-".to_owned(), png_name);
            writeln!(
                file,
                "{}\t{:.3}\t{}\t{}\t{}\t{}\t{}\t{}\t{png}",
                index + 1,
                self.at(shown),
                cell(previous.map(|previous| ms(shown.saturating_duration_since(previous)))),
                cell(between(Some(row.draw.draw_start), layout)),
                cell(between(layout, prepaint)),
                cell(between(prepaint, paint)),
                cell(row.present.map(|present| ms(present.present_duration()))),
                u8::from(row.animation_interval().is_some()),
            )?;
            previous = Some(shown);
        }
        file.flush()
    }

    pub(super) fn write_input(&self, folder: &Path) -> io::Result<()> {
        let mut file = BufWriter::new(File::create(folder.join("input.tsv"))?);
        writeln!(file, "t_ms\tkind\tx\ty\tbutton\tkey\tdelta")?;
        for input in &self.input {
            writeln!(
                file,
                "{:.3}\t{}\t{}\t{}\t{}\t{}\t{}",
                self.at(input.at),
                input.kind.label(),
                cell(input.position.map(|at| f64::from(at.x.as_f32()))),
                cell(input.position.map(|at| f64::from(at.y.as_f32()))),
                input.button_label().unwrap_or_else(|| "-".to_owned()),
                input.key.as_deref().unwrap_or("-"),
                input.delta.map_or_else(
                    || "-".to_owned(),
                    |delta| format!("{:.1},{:.1}", delta.x, delta.y)
                ),
            )?;
        }
        file.flush()
    }

    pub(super) fn write_drawn(&self, folder: &Path, grids: &[Grid]) -> io::Result<Vec<usize>> {
        let mut file = BufWriter::new(File::create(folder.join("drawn.tsv"))?);
        writeln!(
            file,
            "png\tt_ms\tcell_x\tcell_y\tr\tg\tb\ta\tluminance_before\tluminance_after"
        )?;
        let shots = &self.shots.taken;
        let mut counts = vec![0; shots.len()];
        for (index, ((pair, grid), count)) in shots
            .windows(2)
            .zip(grids.windows(2))
            .zip(counts.iter_mut().skip(1))
            .enumerate()
        {
            let ([before, after], [old, new]) = (pair, grid) else {
                continue;
            };
            let changed = cells::changed(before, after, new);
            *count = changed.len();
            for at in changed {
                let (x, y) = new.origin(at);
                let [r, g, b, a] = new.mean.get(at).copied().unwrap_or_default();
                writeln!(
                    file,
                    "{}\t{:.3}\t{x}\t{y}\t{r}\t{g}\t{b}\t{a}\t{}\t{:.1}",
                    png_name(index + 1),
                    self.at(after.at),
                    cell(old.luminance.get(at).map(|value| f64::from(*value))),
                    new.luminance.get(at).copied().unwrap_or_default(),
                )?;
            }
        }
        file.flush()?;
        Ok(counts)
    }

    pub(super) fn summary(
        &self,
        rows: &[Row],
        shot_rows: &[Option<usize>],
        grids: &[Grid],
        changed: &[usize],
    ) -> Result<String, fmt::Error> {
        let (ended, stop) = self.ended.unwrap_or((Instant::now(), Stop::Key));
        let intervals: Vec<Duration> = rows.iter().filter_map(Row::animation_interval).collect();
        let animating = FrameSummary::of(intervals);
        let refresh = self
            .refresh_hz
            .filter(|hz| *hz > 0)
            .map(|hz| 1000.0 / f64::from(hz));
        let mut out = String::new();
        writeln!(out, "recording {}", self.name)?;
        writeln!(
            out,
            "length {:.1} ms, stopped by {}",
            self.at(ended),
            match stop {
                Stop::Key => "F9",
                Stop::Limit => "the time limit",
            }
        )?;
        writeln!(
            out,
            "draws {}, pngs {}, shots dropped over the memory cap {}, input lines {}, drawn lines {}",
            rows.len(),
            self.shots.taken.len(),
            self.shots.dropped,
            self.input.len(),
            changed.iter().sum::<usize>()
        )?;
        for problem in &self.problems {
            writeln!(out, "capture problem: {problem}")?;
        }
        let ratio = |value: f64| {
            refresh.map_or_else(String::new, |period| format!(" ({:.2}x)", value / period))
        };
        match (refresh, self.refresh_hz) {
            (Some(period), Some(hz)) => writeln!(out, "refresh {hz} Hz, {period:.2} ms")?,
            _ => writeln!(out, "refresh unknown")?,
        }
        writeln!(
            out,
            "interval while animating, {} frames: p50 {:.2} ms{}  p95 {:.2} ms{}  max {:.2} ms{}",
            animating.frames,
            animating.p50_ms,
            ratio(animating.p50_ms),
            animating.p95_ms,
            ratio(animating.p95_ms),
            animating.max_ms,
            ratio(animating.max_ms)
        )?;
        if let Some(period) = refresh {
            let slow = period * SLOW_FRAME_FACTOR;
            writeln!(
                out,
                "frames over {SLOW_FRAME_FACTOR} x refresh ({slow:.2} ms):"
            )?;
            for (index, row) in rows.iter().enumerate() {
                let Some(interval) = row
                    .animation_interval()
                    .filter(|interval| ms(*interval) > slow)
                else {
                    continue;
                };
                writeln!(
                    out,
                    "  frame {} at {:.1} ms: interval {:.2} ms, draw {:.2} ms",
                    index + 1,
                    self.at(row.shown()),
                    ms(interval),
                    ms(row.draw.draw_duration())
                )?;
            }
        }
        let flashes = cells::flashes(grids);
        writeln!(
            out,
            "flash candidates ({FLASH_CELL} px cells, luminance moves over {FLASH_LUMINANCE}/255 and returns within {FLASH_RETURN_FRAMES} frames): {}",
            flashes.len()
        )?;
        for flash in &flashes {
            self.flash_line(&mut out, flash, shot_rows, grids, changed)?;
        }
        Ok(out)
    }

    fn flash_line(
        &self,
        out: &mut String,
        flash: &Flash,
        shot_rows: &[Option<usize>],
        grids: &[Grid],
        changed: &[usize],
    ) -> fmt::Result {
        let (Some(shot), Some(grid)) = (self.shots.taken.get(flash.shot), grids.get(flash.shot))
        else {
            return Ok(());
        };
        let origins: Vec<(usize, usize)> = flash.cells.iter().map(|at| grid.origin(*at)).collect();
        let span = |pick: fn(&(usize, usize)) -> usize| {
            let low = origins.iter().map(pick).min().unwrap_or_default();
            let high = origins.iter().map(pick).max().unwrap_or_default() + FLASH_CELL;
            format!("{low}..{high}")
        };
        let (x, y) = grid.origin(flash.strongest);
        let nearest = self
            .input
            .get(..self.input.partition_point(|input| input.at <= shot.at))
            .and_then(<[Input]>::last)
            .map_or_else(|| "none".to_owned(), |input| self.describe(input));
        writeln!(
            out,
            "  png {} at {:.1} ms (draw frame {}), away {} frame(s): before {}, away {}..{}, back {}; {} cells in x {} y {} px; strongest cell {x},{y} luminance {:.1} -> {:.1}; nearest input before: {nearest}; drawn.tsv rows on that png: {}",
            png_name(flash.shot),
            self.at(shot.at),
            shot_rows
                .get(flash.shot)
                .copied()
                .flatten()
                .map_or_else(|| "-".to_owned(), |row| (row + 1).to_string()),
            flash.frames,
            png_name(flash.shot.saturating_sub(1)),
            png_name(flash.shot),
            png_name(flash.shot + flash.frames - 1),
            png_name(flash.shot + flash.frames),
            flash.cells.len(),
            span(|origin| origin.0),
            span(|origin| origin.1),
            flash.from,
            flash.peak,
            changed.get(flash.shot).copied().unwrap_or_default(),
        )
    }

    fn describe(&self, input: &Input) -> String {
        let place = input.position.map_or_else(
            || input.key.clone().unwrap_or_default(),
            |at| format!("at {:.0},{:.0}", at.x.as_f32(), at.y.as_f32()),
        );
        format!(
            "{:.1} ms {} {} {place}",
            self.at(input.at),
            input.kind.label(),
            input.button_label().unwrap_or_default()
        )
    }
}
