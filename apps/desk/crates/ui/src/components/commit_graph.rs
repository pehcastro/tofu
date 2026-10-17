use gpui::{
    AnyElement, App, Bounds, FontWeight, PathBuilder, Pixels, Rgba, SharedString, Window, canvas,
    div, point, prelude::*, px, relative, rgb,
};

use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, halo, ink, tint};
use crate::components::size::{CHIP_FILL, T2};
use crate::theme::{ColorToken, Mode, Theme};

pub const ROW_HEIGHT: f32 = 32.0;
pub const LANE_WIDTH: f32 = 16.0;
pub const REF_BAND: f32 = 180.0;
const EDGE_WIDTH: f32 = 2.0;
const AVATAR: f32 = 20.0;
const AVATAR_RING: f32 = 2.0;
const HEAD_RING: f32 = 2.5;
const FONT_INITIALS: f32 = 9.0;
const PILL_HEIGHT: f32 = 18.0;
const PILL_PAD_X: f32 = 7.0;
const PILL_GAP: f32 = 4.0;
const FONT_PILL: f32 = 11.0;
const PILL_TINT: f32 = 0.18;
const PILL_EDGE: f32 = 0.12;
const PILL_MARK: f32 = 11.0;
const LANE_COLORS: u8 = 10;
const DARK_LANES: [u32; LANE_COLORS as usize] = [
    0x4f8cff, 0xf2994a, 0x9b6dff, 0x43b97f, 0xe5534b, 0x3ec6c0, 0xe8c341, 0xf26d9c, 0x7a86ff,
    0x8fbf4d,
];
const LIGHT_LANES: [u32; LANE_COLORS as usize] = [
    0x2f6fe0, 0xc4661a, 0x7348e0, 0x1f8a57, 0xc4372f, 0x1a8f8a, 0x9a7a12, 0xc43f74, 0x4a56d0,
    0x5a8a22,
];

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct LaneRow {
    pub node_lane: u16,
    pub lanes_through: Vec<(u16, u8)>,
    pub edges: Vec<Edge>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Edge {
    pub from_lane: u16,
    pub to_lane: u16,
    pub color: u8,
    pub kind: EdgeKind,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum EdgeKind {
    Straight,
    MergeIn,
    BranchOut,
}

#[derive(Clone, Debug, Default)]
pub struct LaneState {
    lanes: Vec<Option<Lane>>,
    next_color: u8,
}

#[derive(Clone, Debug)]
struct Lane {
    waits_for: String,
    color: u8,
}

impl LaneRow {
    pub fn node_color(&self) -> u8 {
        self.edges
            .iter()
            .find(|edge| edge.from_lane == self.node_lane && edge.to_lane == self.node_lane)
            .map_or(0, |edge| edge.color)
    }

    fn span(&self) -> u16 {
        let through = self.lanes_through.iter().map(|&(lane, _)| lane);
        let edges = self
            .edges
            .iter()
            .flat_map(|edge| [edge.from_lane, edge.to_lane]);
        through
            .chain(edges)
            .fold(self.node_lane, u16::max)
            .saturating_add(1)
    }
}

pub fn lay_out(rows: &[(String, Vec<String>)]) -> Vec<LaneRow> {
    lay_out_from(&mut LaneState::default(), rows)
}

pub fn lay_out_from(state: &mut LaneState, rows: &[(String, Vec<String>)]) -> Vec<LaneRow> {
    rows.iter()
        .map(|(sha, parents)| state.place(sha, parents))
        .collect()
}

impl LaneState {
    fn place(&mut self, sha: &str, parents: &[String]) -> LaneRow {
        let mut edges = Vec::new();
        let mut lanes_through = Vec::new();
        let mut arrived = None;
        for (ix, slot) in self.lanes.iter_mut().enumerate() {
            match slot.take_if(|lane| lane.waits_for == sha) {
                Some(lane) => {
                    let (node, _) = *arrived.get_or_insert((ix, lane.color));
                    if node != ix {
                        edges.push(edge(ix, node, lane.color, EdgeKind::BranchOut));
                    }
                }
                None => {
                    lanes_through.extend(slot.as_ref().map(|lane| (lane_index(ix), lane.color)))
                }
            }
        }
        let (node, color) = match arrived {
            Some(found) => found,
            None => (self.free_slot(), self.fresh_color()),
        };
        let mut parents = parents.iter();
        let first = parents.next();
        let own = match (arrived.is_some(), first) {
            (true, Some(_)) => Some(EdgeKind::Straight),
            (true, None) => Some(EdgeKind::BranchOut),
            (false, Some(_)) => Some(EdgeKind::MergeIn),
            (false, None) => None,
        };
        edges.extend(own.map(|kind| edge(node, node, color, kind)));
        if let (Some(first), Some(slot)) = (first, self.lanes.get_mut(node)) {
            *slot = Some(Lane {
                waits_for: first.clone(),
                color,
            });
        }
        for parent in parents {
            let (lane, lane_color) = self.lane_for(parent);
            if lane != node {
                edges.push(edge(node, lane, lane_color, EdgeKind::MergeIn));
            }
        }
        while self.lanes.last().is_some_and(Option::is_none) {
            self.lanes.pop();
        }
        LaneRow {
            node_lane: lane_index(node),
            lanes_through,
            edges,
        }
    }

    fn lane_for(&mut self, parent: &str) -> (usize, u8) {
        let waiting = self.lanes.iter().enumerate().find_map(|(ix, slot)| {
            slot.as_ref()
                .filter(|lane| lane.waits_for == parent)
                .map(|lane| (ix, lane.color))
        });
        if let Some(found) = waiting {
            return found;
        }
        let (ix, color) = (self.free_slot(), self.fresh_color());
        if let Some(slot) = self.lanes.get_mut(ix) {
            *slot = Some(Lane {
                waits_for: parent.to_owned(),
                color,
            });
        }
        (ix, color)
    }

    fn free_slot(&mut self) -> usize {
        self.lanes
            .iter()
            .position(Option::is_none)
            .unwrap_or_else(|| {
                self.lanes.push(None);
                self.lanes.len() - 1
            })
    }

    fn fresh_color(&mut self) -> u8 {
        let color = self.next_color;
        self.next_color = (color + 1) % LANE_COLORS;
        color
    }
}

fn edge(from: usize, to: usize, color: u8, kind: EdgeKind) -> Edge {
    Edge {
        from_lane: lane_index(from),
        to_lane: lane_index(to),
        color,
        kind,
    }
}

fn lane_index(ix: usize) -> u16 {
    u16::try_from(ix).unwrap_or(u16::MAX)
}

fn lane_rgba(color: u8, theme: &Theme) -> Rgba {
    let palette = match theme.mode() {
        Mode::Dark => DARK_LANES,
        Mode::Light => LIGHT_LANES,
    };
    rgb(palette[usize::from(color % LANE_COLORS)])
}

fn center(lane: u16) -> f32 {
    f32::from(lane) * LANE_WIDTH + LANE_WIDTH / 2.0
}

struct Stroke {
    from: (f32, f32),
    to: (f32, f32),
    color: Rgba,
}

#[derive(IntoElement)]
pub struct GraphCell {
    span: u16,
    node_lane: u16,
    strokes: Vec<Stroke>,
    initials: SharedString,
    fill: Rgba,
    ring: Rgba,
    text: Rgba,
    head: Option<Rgba>,
}

pub fn graph_cell(row: &LaneRow, initials: SharedString, head: bool, theme: &Theme) -> GraphCell {
    let (top, middle, bottom) = (0.0, ROW_HEIGHT / 2.0, ROW_HEIGHT);
    let through = row.lanes_through.iter().map(|&(lane, color)| Stroke {
        from: (center(lane), top),
        to: (center(lane), bottom),
        color: lane_rgba(color, theme),
    });
    let edges = row.edges.iter().map(|edge| {
        let (from_y, to_y) = match edge.kind {
            EdgeKind::Straight => (top, bottom),
            EdgeKind::BranchOut => (top, middle),
            EdgeKind::MergeIn => (middle, bottom),
        };
        Stroke {
            from: (center(edge.from_lane), from_y),
            to: (center(edge.to_lane), to_y),
            color: lane_rgba(edge.color, theme),
        }
    });
    let author = initials.bytes().fold(0u8, |hash, byte| {
        hash.wrapping_mul(31).wrapping_add(byte) % LANE_COLORS
    });
    GraphCell {
        span: row.span(),
        node_lane: row.node_lane,
        strokes: through.chain(edges).collect(),
        initials,
        fill: lane_rgba(author, theme),
        ring: lane_rgba(row.node_color(), theme),
        text: theme.color(ColorToken::ButtonPrimaryText),
        head: head.then(|| theme.color(ColorToken::TextStrong)),
    }
}

impl RenderOnce for GraphCell {
    fn render(self, _: &mut Window, _: &mut App) -> impl IntoElement {
        let strokes = self.strokes;
        div()
            .relative()
            .flex_none()
            .w(px(f32::from(self.span) * LANE_WIDTH))
            .h(px(ROW_HEIGHT))
            .child(
                canvas(
                    |_, _, _| (),
                    move |bounds, (), window, _| paint_strokes(&strokes, bounds, window),
                )
                .absolute()
                .inset_0(),
            )
            .child(
                div()
                    .absolute()
                    .left(px(center(self.node_lane) - AVATAR / 2.0))
                    .top(px((ROW_HEIGHT - AVATAR) / 2.0))
                    .size(px(AVATAR))
                    .flex()
                    .items_center()
                    .justify_center()
                    .rounded_full()
                    .bg(self.fill)
                    .border(px(AVATAR_RING))
                    .border_color(self.ring)
                    .when_some(self.head, |disc, ring| {
                        disc.shadow(vec![halo(ring, HEAD_RING)])
                    })
                    .text_size(px(FONT_INITIALS))
                    .font_weight(FontWeight::SEMIBOLD)
                    .line_height(relative(1.0))
                    .text_color(self.text)
                    .child(self.initials),
            )
    }
}

fn paint_strokes(strokes: &[Stroke], bounds: Bounds<Pixels>, window: &mut Window) {
    let at = |(x, y): (f32, f32)| bounds.origin + point(px(x), px(y));
    for stroke in strokes {
        let mut path = PathBuilder::stroke(px(EDGE_WIDTH));
        path.move_to(at(stroke.from));
        if stroke.from.0 == stroke.to.0 {
            path.line_to(at(stroke.to));
        } else {
            let bend = (stroke.from.1 + stroke.to.1) / 2.0;
            path.cubic_bezier_to(
                at(stroke.to),
                at((stroke.from.0, bend)),
                at((stroke.to.0, bend)),
            );
        }
        if let Ok(path) = path.build() {
            window.paint_path(path, stroke.color);
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum PillKind {
    Head,
    Local,
    LocalWithUpstream,
    Remote,
    Tag,
    Stash,
    Detached,
}

pub fn ref_pill(name: SharedString, kind: PillKind, lane_color: u8, theme: &Theme) -> AnyElement {
    let lane = lane_rgba(lane_color, theme);
    let on_solid = theme.color(ColorToken::ButtonPrimaryText);
    let toned = |tone: Rgba| (tint(tone, PILL_TINT), tone, tone);
    let (fill, edge, text) = match kind {
        PillKind::Head => (lane, lane, on_solid),
        PillKind::Local | PillKind::LocalWithUpstream => toned(lane),
        PillKind::Remote => (ink(theme, CHIP_FILL), ink(theme, PILL_EDGE), ink(theme, T2)),
        PillKind::Tag => toned(theme.color(ColorToken::StatusWarn)),
        PillKind::Stash => toned(theme.color(ColorToken::SyntaxKeyword)),
        PillKind::Detached => {
            let danger = theme.color(ColorToken::StatusDanger);
            (danger, danger, on_solid)
        }
    };
    let label = match kind {
        PillKind::Tag => SharedString::from(format!("# {name}")),
        _ => name,
    };
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(PILL_GAP))
        .max_w(px(REF_BAND))
        .h(px(PILL_HEIGHT))
        .px(px(PILL_PAD_X))
        .rounded_full()
        .bg(fill)
        .border(px(1.0))
        .border_color(edge)
        .text_size(px(FONT_PILL))
        .font_weight(FontWeight::SEMIBOLD)
        .line_height(relative(1.0))
        .text_color(text)
        .child(div().min_w_0().truncate().child(label))
        .when(kind == PillKind::LocalWithUpstream, |pill| {
            pill.child(glyph(Glyph::Globe, PILL_MARK, text))
        })
        .into_any_element()
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeMap;
    use std::process::Command;
    use std::time::Instant;

    use super::*;

    const COMMITS: &str = "2000";
    const PAGE: usize = 200;
    const BUDGET_MS: f64 = 5.0;

    type Boundary = BTreeMap<u16, Vec<u8>>;

    fn history() -> Result<Vec<(String, Vec<String>)>, String> {
        let out = Command::new("git")
            .args(["log", "--topo-order", "--format=%H %P", "-n", COMMITS])
            .current_dir(env!("CARGO_MANIFEST_DIR"))
            .output()
            .map_err(|error| error.to_string())?;
        if !out.status.success() {
            return Err(String::from_utf8_lossy(&out.stderr).into_owned());
        }
        Ok(String::from_utf8_lossy(&out.stdout)
            .lines()
            .filter_map(|line| {
                let mut words = line.split_whitespace().map(str::to_owned);
                Some((words.next()?, words.collect()))
            })
            .collect())
    }

    fn ends(row: &LaneRow) -> (Boundary, Boundary) {
        let (mut top, mut bottom) = (Boundary::new(), Boundary::new());
        for &(lane, color) in &row.lanes_through {
            top.entry(lane).or_default().push(color);
            bottom.entry(lane).or_default().push(color);
        }
        for edge in &row.edges {
            if matches!(edge.kind, EdgeKind::Straight | EdgeKind::BranchOut) {
                top.entry(edge.from_lane).or_default().push(edge.color);
            }
            if matches!(edge.kind, EdgeKind::Straight | EdgeKind::MergeIn) {
                bottom.entry(edge.to_lane).or_default().push(edge.color);
            }
        }
        (top, bottom)
    }

    fn first_colors(boundary: &Boundary) -> BTreeMap<u16, Option<u8>> {
        boundary
            .iter()
            .map(|(&lane, colors)| (lane, colors.first().copied()))
            .collect()
    }

    fn collisions(rows: &[LaneRow]) -> usize {
        let mut count = 0;
        let mut above: Option<Boundary> = None;
        for row in rows {
            let (top, bottom) = ends(row);
            count += row
                .lanes_through
                .iter()
                .filter(|&&(lane, _)| lane == row.node_lane)
                .count();
            count += top.values().filter(|colors| colors.len() > 1).count();
            count += bottom
                .values()
                .filter(|colors| colors.windows(2).any(|pair| pair.first() != pair.last()))
                .count();
            if let Some(above) = &above {
                count += usize::from(first_colors(above) != first_colors(&top));
            }
            above = Some(bottom);
        }
        count
    }

    fn check(rows: &[(String, Vec<String>)]) -> Result<String, String> {
        let started = Instant::now();
        let laid = lay_out(rows);
        let took = started.elapsed().as_secs_f64() * 1000.0;
        let mut state = LaneState::default();
        let paged: Vec<LaneRow> = rows
            .chunks(PAGE)
            .flat_map(|page| lay_out_from(&mut state, page))
            .collect();
        let max_lanes = laid.iter().map(LaneRow::span).max().unwrap_or(0);
        let collisions = collisions(&laid);
        let said = format!(
            "commits {} max lanes {max_lanes} collisions {collisions} paged same {} lay_out {took:.3} ms",
            rows.len(),
            paged == laid
        );
        if collisions > 0 || paged != laid || took >= BUDGET_MS {
            return Err(said);
        }
        Ok(said)
    }

    #[test]
    fn lays_out_this_repository() -> Result<(), String> {
        let rows = history()?;
        println!("{}", check(&rows)?);
        Ok(())
    }

    #[test]
    fn lays_out_forks_merges_and_octopus() -> Result<(), String> {
        let rows: Vec<(String, Vec<String>)> = [
            ("m", "k j"),
            ("k", "h i"),
            ("j", "e"),
            ("i", "f g d"),
            ("h", "e"),
            ("g", "b"),
            ("f", "c"),
            ("e", "c"),
            ("d", "a"),
            ("c", "b"),
            ("b", "a"),
            ("a", ""),
            ("z", ""),
        ]
        .iter()
        .map(|(sha, parents)| {
            let parents = parents.split_whitespace().map(str::to_owned).collect();
            ((*sha).to_owned(), parents)
        })
        .collect();
        println!("{}", check(&rows)?);
        Ok(())
    }
}
