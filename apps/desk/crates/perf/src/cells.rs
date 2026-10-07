use crate::capture::Shot;
use crate::limits::{FLASH_CELL, FLASH_LUMINANCE, FLASH_RETURN_FRAMES};

const BYTES: usize = 4;

pub(crate) struct Grid {
    pub width: usize,
    pub height: usize,
    pub cols: usize,
    pub mean: Vec<[u8; 4]>,
    pub luminance: Vec<f32>,
}

pub(crate) struct Flash {
    pub shot: usize,
    pub frames: usize,
    pub cells: Vec<usize>,
    pub strongest: usize,
    pub from: f32,
    pub peak: f32,
}

impl Grid {
    pub fn of(shot: &Shot) -> Self {
        let cols = shot.width.div_ceil(FLASH_CELL);
        let rows = shot.height.div_ceil(FLASH_CELL);
        let mut sums = vec![[0u64; 4]; cols * rows];
        let mut counts = vec![0u64; cols * rows];
        for (y, line) in shot
            .bgra
            .chunks_exact(shot.width * BYTES)
            .take(shot.height)
            .enumerate()
        {
            let row = (y / FLASH_CELL) * cols;
            for (x, &[b, g, r, a]) in line.as_chunks::<BYTES>().0.iter().enumerate() {
                let cell = row + x / FLASH_CELL;
                if let (Some(sum), Some(count)) = (sums.get_mut(cell), counts.get_mut(cell)) {
                    for (total, byte) in sum.iter_mut().zip([r, g, b, a]) {
                        *total += u64::from(byte);
                    }
                    *count += 1;
                }
            }
        }
        let mean: Vec<[u8; 4]> = sums
            .iter()
            .zip(&counts)
            .map(|(sum, count)| sum.map(|total| (total / (*count).max(1)) as u8))
            .collect();
        let luminance = mean
            .iter()
            .map(|[r, g, b, _]| {
                0.2126 * f32::from(*r) + 0.7152 * f32::from(*g) + 0.0722 * f32::from(*b)
            })
            .collect();
        Grid {
            width: shot.width,
            height: shot.height,
            cols,
            mean,
            luminance,
        }
    }

    pub fn origin(&self, cell: usize) -> (usize, usize) {
        (
            (cell % self.cols) * FLASH_CELL,
            (cell / self.cols) * FLASH_CELL,
        )
    }

    fn same_size(&self, other: &Grid) -> bool {
        self.width == other.width && self.height == other.height
    }
}

pub(crate) fn changed(before: &Shot, after: &Shot, grid: &Grid) -> Vec<usize> {
    if before.width != after.width || before.height != after.height {
        return (0..grid.luminance.len()).collect();
    }
    let stride = before.width * BYTES;
    let mut changed = vec![false; grid.luminance.len()];
    for (y, (a, b)) in before
        .bgra
        .chunks_exact(stride)
        .zip(after.bgra.chunks_exact(stride))
        .enumerate()
    {
        let row = (y / FLASH_CELL) * grid.cols;
        for (col, (a, b)) in a
            .chunks(FLASH_CELL * BYTES)
            .zip(b.chunks(FLASH_CELL * BYTES))
            .enumerate()
        {
            if a != b
                && let Some(cell) = changed.get_mut(row + col)
            {
                *cell = true;
            }
        }
    }
    changed
        .iter()
        .enumerate()
        .filter_map(|(cell, moved)| moved.then_some(cell))
        .collect()
}

pub(crate) fn flashes(grids: &[Grid]) -> Vec<Flash> {
    let mut found: Vec<Flash> = Vec::new();
    for (shot, pair) in grids
        .windows(2)
        .enumerate()
        .map(|(at, pair)| (at + 1, pair))
    {
        let [base, away] = pair else { continue };
        if !base.same_size(away) {
            continue;
        }
        for (cell, (&from, &to)) in base.luminance.iter().zip(&away.luminance).enumerate() {
            if (to - from).abs() <= FLASH_LUMINANCE {
                continue;
            }
            let back = grids
                .iter()
                .enumerate()
                .skip(shot + 1)
                .take(FLASH_RETURN_FRAMES)
                .take_while(|(_, grid)| grid.same_size(base))
                .find(|(_, grid)| {
                    grid.luminance
                        .get(cell)
                        .is_some_and(|value| (value - from).abs() <= FLASH_LUMINANCE)
                });
            let Some((back, _)) = back else { continue };
            let frames = back - shot;
            let peak = grids
                .get(shot..back)
                .unwrap_or_default()
                .iter()
                .filter_map(|grid| grid.luminance.get(cell).copied())
                .fold(to, |peak, value| {
                    if (value - from).abs() > (peak - from).abs() {
                        value
                    } else {
                        peak
                    }
                });
            match found
                .iter_mut()
                .find(|flash| flash.shot == shot && flash.frames == frames)
            {
                Some(flash) => {
                    flash.cells.push(cell);
                    if (peak - from).abs() > (flash.peak - flash.from).abs() {
                        flash.strongest = cell;
                        flash.from = from;
                        flash.peak = peak;
                    }
                }
                None => found.push(Flash {
                    shot,
                    frames,
                    cells: vec![cell],
                    strongest: cell,
                    from,
                    peak,
                }),
            }
        }
    }
    found
}
