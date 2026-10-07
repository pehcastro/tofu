use crate::{
    Axis, CORNER_ZONE, GAP, HEADER_ZONE, HOLD, Node, Part, Rect, SIDE_ZONE, SLACK, Side, Size,
    Stack, Target, TileId, WORKSPACE_EDGE, Zone,
};

#[derive(Clone, Debug, PartialEq)]
pub struct Divider {
    pub path: Vec<usize>,
    pub index: usize,
    pub axis: Axis,
    pub split: Rect,
    pub rect: Rect,
}

#[derive(Clone, Debug, PartialEq)]
pub struct Corner {
    pub tile: TileId,
    pub horizontal: Side,
    pub vertical: Side,
    pub rect: Rect,
    pub dividers: Vec<Divider>,
}

impl Corner {
    pub fn falls_left(&self) -> bool {
        matches!(
            (self.horizontal, self.vertical),
            (Side::Left, Side::Top) | (Side::Right, Side::Bottom)
        )
    }
}

pub(crate) fn corners(node: &Node, area: Rect) -> Vec<Corner> {
    let all = dividers(node, area);
    let mut out = Vec::new();
    let covers = |start: f32, length: f32, from: f32, span: f32| {
        start <= from + SLACK && start + length + SLACK >= from + span
    };
    for (stack, tile) in solve(node, area) {
        let (zone_w, zone_h) = (CORNER_ZONE.min(tile.w / 2.0), CORNER_ZONE.min(tile.h / 2.0));
        for (horizontal, vertical) in [
            (Side::Left, Side::Top),
            (Side::Right, Side::Top),
            (Side::Left, Side::Bottom),
            (Side::Right, Side::Bottom),
        ] {
            let (right, bottom) = (horizontal == Side::Right, vertical == Side::Bottom);
            let edge_x = if right { tile.x + tile.w } else { tile.x };
            let edge_y = if bottom { tile.y + tile.h } else { tile.y };
            let row = all.iter().find(|divider| {
                let near = if right {
                    divider.rect.x
                } else {
                    divider.rect.x + divider.rect.w
                };
                divider.axis == Axis::Row
                    && (near - edge_x).abs() < SLACK
                    && covers(divider.rect.y, divider.rect.h, tile.y, tile.h)
            });
            let column = all.iter().find(|divider| {
                let near = if bottom {
                    divider.rect.y
                } else {
                    divider.rect.y + divider.rect.h
                };
                divider.axis == Axis::Column
                    && (near - edge_y).abs() < SLACK
                    && covers(divider.rect.x, divider.rect.w, tile.x, tile.w)
            });
            let dividers: Vec<Divider> = row.into_iter().chain(column).cloned().collect();
            if dividers.is_empty() {
                continue;
            }
            out.push(Corner {
                tile: stack.id,
                horizontal,
                vertical,
                rect: Rect {
                    x: if right { edge_x - zone_w } else { edge_x },
                    y: if bottom { edge_y - zone_h } else { edge_y },
                    w: zone_w,
                    h: zone_h,
                },
                dividers,
            });
        }
    }
    out
}

pub(crate) fn least(node: &Node, axis: Axis) -> f32 {
    match node {
        Node::Tile(stack) => match axis {
            Axis::Row => stack.minimum().w,
            Axis::Column => stack.minimum().h,
        },
        Node::Split { axis: own, parts } if *own == axis => {
            parts
                .iter()
                .map(|part| least(&part.node, axis))
                .sum::<f32>()
                + GAP * parts.len().saturating_sub(1) as f32
        }
        Node::Split { parts, .. } => parts
            .iter()
            .map(|part| least(&part.node, axis))
            .fold(0.0, f32::max),
    }
}

fn lengths(axis: Axis, parts: &[Part], room: f32) -> Vec<f32> {
    let last = parts.len().saturating_sub(1);
    let all_fixed = parts.iter().all(|part| matches!(part.size, Size::Fixed(_)));
    let floors: Vec<f32> = parts.iter().map(|part| least(&part.node, axis)).collect();
    let wants: Vec<Option<f32>> = parts
        .iter()
        .zip(&floors)
        .enumerate()
        .map(|(index, (part, floor))| match part.size {
            Size::Fixed(pixels) if !(all_fixed && index == last) => Some(pixels.max(*floor)),
            Size::Fixed(_) | Size::Share(_) => None,
        })
        .collect();
    let share = |index: usize| match parts.get(index).map(|part| part.size) {
        Some(Size::Share(share)) => share,
        Some(Size::Fixed(_)) | None => 1.0,
    };
    let fixed: f32 = wants.iter().flatten().sum();
    let floor_of = |index: usize| floors.get(index).copied().unwrap_or_default();
    let share_floors: f32 = (0..parts.len())
        .filter(|index| wants.get(*index).is_some_and(Option::is_none))
        .map(floor_of)
        .sum();
    let all_floors: f32 = floors.iter().sum();
    if room >= fixed + share_floors {
        let mut pinned: Vec<bool> = wants.iter().map(Option::is_some).collect();
        let mut free_room = room - fixed;
        loop {
            let free_share: f32 = (0..parts.len())
                .filter(|index| !pinned.get(*index).copied().unwrap_or(true))
                .map(share)
                .sum();
            let short: Vec<usize> = (0..parts.len())
                .filter(|index| {
                    !pinned.get(*index).copied().unwrap_or(true)
                        && free_room * share(*index) / free_share < floor_of(*index)
                })
                .collect();
            for index in &short {
                if let Some(pin) = pinned.get_mut(*index) {
                    *pin = true;
                }
                free_room -= floor_of(*index);
            }
            if short.is_empty() {
                return (0..parts.len())
                    .map(|index| match wants.get(index).copied().flatten() {
                        Some(pixels) => pixels,
                        None if pinned.get(index).copied().unwrap_or(true) => floor_of(index),
                        None => free_room * share(index) / free_share,
                    })
                    .collect();
            }
        }
    }
    if room >= all_floors {
        let excess: f32 = wants
            .iter()
            .zip(&floors)
            .filter_map(|(want, floor)| want.map(|want| want - floor))
            .sum();
        let spare = room - all_floors;
        return wants
            .iter()
            .zip(&floors)
            .map(|(want, floor)| match want {
                Some(want) if excess > 0.0 => floor + spare * (want - floor) / excess,
                Some(_) | None => *floor,
            })
            .collect();
    }
    floors
        .iter()
        .map(|floor| floor * room / all_floors)
        .collect()
}

pub(crate) fn spans(axis: Axis, parts: &[Part], area: Rect) -> Vec<Rect> {
    let (start, length) = area.along(axis);
    let count = parts.len().max(1) as f32;
    let gap = GAP.min(length.max(0.0) / count);
    let room = (length - gap * (count - 1.0)).max(0.0);
    let mut at = start;
    lengths(axis, parts, room)
        .into_iter()
        .map(|size| {
            let rect = match axis {
                Axis::Row => Rect {
                    x: at,
                    w: size,
                    ..area
                },
                Axis::Column => Rect {
                    y: at,
                    h: size,
                    ..area
                },
            };
            at += size + gap;
            rect
        })
        .collect()
}

pub fn solve(node: &Node, area: Rect) -> Vec<(&Stack, Rect)> {
    let mut out = Vec::new();
    let mut pending = vec![(node, area)];
    while let Some((node, area)) = pending.pop() {
        match node {
            Node::Tile(stack) => out.push((stack, area)),
            Node::Split { axis, parts } => pending.extend(
                parts
                    .iter()
                    .zip(spans(*axis, parts, area))
                    .map(|(part, rect)| (&part.node, rect)),
            ),
        }
    }
    out.sort_by_key(|(stack, _)| stack.id);
    out
}

pub(crate) fn dividers(node: &Node, area: Rect) -> Vec<Divider> {
    let mut out = Vec::new();
    let mut pending = vec![(node, area, Vec::new())];
    while let Some((node, area, path)) = pending.pop() {
        let Node::Split { axis, parts } = node else {
            continue;
        };
        let rects = spans(*axis, parts, area);
        for (index, pair) in rects.windows(2).enumerate() {
            let [before, after] = pair else { continue };
            let rect = match axis {
                Axis::Row => Rect {
                    x: before.x + before.w,
                    w: after.x - before.x - before.w,
                    ..area
                },
                Axis::Column => Rect {
                    y: before.y + before.h,
                    h: after.y - before.y - before.h,
                    ..area
                },
            };
            out.push(Divider {
                path: path.clone(),
                index,
                axis: *axis,
                split: area,
                rect,
            });
        }
        for (index, (part, rect)) in parts.iter().zip(rects).enumerate() {
            let mut inner = path.clone();
            inner.push(index);
            pending.push((&part.node, rect, inner));
        }
    }
    out
}

fn zones(tile: Rect, modules: usize) -> [(Zone, Rect); 6] {
    let stack = Zone::Stack { at: modules };
    let header = tile.h.min(HEADER_ZONE);
    let body = Rect {
        y: tile.y + header,
        h: tile.h - header,
        ..tile
    };
    let (band_w, band_h) = (body.w * SIDE_ZONE, body.h * SIDE_ZONE);
    let middle = Rect {
        x: body.x + band_w,
        w: body.w - 2.0 * band_w,
        ..body
    };
    [
        (stack, Rect { h: header, ..tile }),
        (Zone::Side(Side::Left), Rect { w: band_w, ..body }),
        (
            Zone::Side(Side::Right),
            Rect {
                x: body.x + body.w - band_w,
                w: band_w,
                ..body
            },
        ),
        (
            Zone::Side(Side::Top),
            Rect {
                h: band_h,
                ..middle
            },
        ),
        (
            Zone::Side(Side::Bottom),
            Rect {
                y: body.y + body.h - band_h,
                h: band_h,
                ..middle
            },
        ),
        (
            stack,
            Rect {
                y: body.y + band_h,
                h: body.h - 2.0 * band_h,
                ..middle
            },
        ),
    ]
}

fn band(area: Rect, side: Side) -> Rect {
    let outer = Rect {
        x: area.x - WORKSPACE_EDGE,
        y: area.y - WORKSPACE_EDGE,
        w: area.w + 2.0 * WORKSPACE_EDGE,
        h: area.h + 2.0 * WORKSPACE_EDGE,
    };
    match side {
        Side::Left => Rect {
            w: WORKSPACE_EDGE,
            ..outer
        },
        Side::Right => Rect {
            x: area.x + area.w,
            w: WORKSPACE_EDGE,
            ..outer
        },
        Side::Top => Rect {
            h: WORKSPACE_EDGE,
            ..outer
        },
        Side::Bottom => Rect {
            y: area.y + area.h,
            h: WORKSPACE_EDGE,
            ..outer
        },
    }
}

fn near(rect: Rect, x: f32, y: f32) -> bool {
    let dx = (rect.x - x).max(x - rect.x - rect.w).max(0.0);
    let dy = (rect.y - y).max(y - rect.y - rect.h).max(0.0);
    dx.max(dy) <= HOLD
}

fn same(a: Zone, b: Zone) -> bool {
    a == b || matches!((a, b), (Zone::Stack { .. }, Zone::Stack { .. }))
}

pub fn aim(
    node: Option<&Node>,
    area: Rect,
    x: f32,
    y: f32,
    held: Option<Target>,
) -> Option<Target> {
    match held {
        Some(held) if hold(node, area, x, y, held) => Some(held),
        Some(_) | None => target(node, area, x, y),
    }
}

fn hold(node: Option<&Node>, area: Rect, x: f32, y: f32, held: Target) -> bool {
    match held {
        Target::Edge(side) => near(band(area, side), x, y),
        Target::Tile(id, zone) => node
            .map(|node| solve(node, area))
            .unwrap_or_default()
            .into_iter()
            .find(|(stack, _)| stack.id == id)
            .is_some_and(|(stack, rect)| {
                zones(rect, stack.modules.len())
                    .into_iter()
                    .any(|(kind, region)| same(kind, zone) && near(region, x, y))
            }),
    }
}

pub fn target(node: Option<&Node>, area: Rect, x: f32, y: f32) -> Option<Target> {
    let tiles = node.map(|node| solve(node, area)).unwrap_or_default();
    if let Some((stack, rect)) = tiles.iter().find(|(_, rect)| rect.contains(x, y)) {
        let all = zones(*rect, stack.modules.len());
        let zone = all
            .iter()
            .find(|(_, region)| region.contains(x, y))
            .map_or(all[0].0, |(zone, _)| *zone);
        return Some(Target::Tile(stack.id, zone));
    }
    let (side, distance) = Side::ALL
        .into_iter()
        .map(|side| {
            let distance = match side {
                Side::Left => x - area.x,
                Side::Right => area.x + area.w - x,
                Side::Top => y - area.y,
                Side::Bottom => area.y + area.h - y,
            };
            (side, distance)
        })
        .min_by(|a, b| a.1.total_cmp(&b.1))?;
    let reach = match node {
        Some(_) => WORKSPACE_EDGE,
        None => f32::INFINITY,
    };
    (distance >= -WORKSPACE_EDGE && distance < reach).then_some(Target::Edge(side))
}
