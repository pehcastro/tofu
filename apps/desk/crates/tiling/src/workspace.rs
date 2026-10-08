use crate::solve::{corners, dividers, least, spans};
use crate::{
    Axis, CLOSED_DEPTH, Corner, Divider, EDGE_BAR, HISTORY_DEPTH, MAX_TILES, Module, Node, Part,
    Preset, Rect, Refusal, SLACK, STRIP_HEIGHT, STRIP_WIDTH, Side, Size, Stack, Target, TileId,
    Zone, solve,
};

#[derive(Clone, Debug, PartialEq)]
pub enum Grab {
    New(Module),
    Placed { tile: TileId, index: usize },
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Preview {
    Stack {
        tile: TileId,
        outline: Rect,
        at: usize,
    },
    Split(Rect),
    Edge {
        strip: Rect,
        bar: Rect,
    },
}

#[derive(Clone, Debug, PartialEq)]
pub enum Spawned {
    Here(Workspace),
    NewTab(Workspace),
}

#[derive(Clone, Debug, PartialEq)]
pub struct Workspace {
    pub name: String,
    pub preset: Preset,
    pub locked: bool,
    pub pinned: bool,
    tree: Option<Node>,
    focus: Option<TileId>,
    zoom: Option<TileId>,
    history: Vec<Option<Node>>,
    closed: Vec<(Module, usize)>,
    next: u32,
}

fn walk_mut(node: &mut Node, visit: &mut dyn FnMut(&mut Stack) -> bool) -> bool {
    let mut pending = vec![node];
    while let Some(node) = pending.pop() {
        match node {
            Node::Tile(stack) => {
                if visit(stack) {
                    return true;
                }
            }
            Node::Split { parts, .. } => {
                pending.extend(parts.iter_mut().map(|part| &mut part.node))
            }
        }
    }
    false
}

fn path_of(node: &Node, id: TileId) -> Option<Vec<usize>> {
    let mut pending = vec![(node, Vec::new())];
    while let Some((node, path)) = pending.pop() {
        match node {
            Node::Tile(stack) if stack.id == id => return Some(path),
            Node::Tile(_) => {}
            Node::Split { parts, .. } => {
                for (index, part) in parts.iter().enumerate() {
                    let mut inner = path.clone();
                    inner.push(index);
                    pending.push((&part.node, inner));
                }
            }
        }
    }
    None
}

fn at_mut<'a>(node: &'a mut Node, path: &[usize]) -> Option<&'a mut Node> {
    let mut node = node;
    for index in path {
        let Node::Split { parts, .. } = node else {
            return None;
        };
        node = &mut parts.get_mut(*index)?.node;
    }
    Some(node)
}

struct Reach {
    start: f32,
    gap: f32,
    room: f32,
    least: (f32, f32),
    sizes: (Size, Size),
}

impl Reach {
    fn bounds(&self) -> (f32, f32) {
        let from = self.start + self.gap / 2.0;
        (from + self.least.0, from + self.room - self.least.1)
    }
}

fn reach(tree: Option<&Node>, divider: &Divider) -> Result<Reach, Refusal> {
    let mut node = tree.ok_or(Refusal::NoSuchTile)?;
    for index in &divider.path {
        let Node::Split { parts, .. } = node else {
            return Err(Refusal::NoSuchTile);
        };
        node = &parts.get(*index).ok_or(Refusal::NoSuchTile)?.node;
    }
    let Node::Split { axis, parts } = node else {
        return Err(Refusal::NoSuchTile);
    };
    if *axis != divider.axis {
        return Err(Refusal::NoSuchTile);
    }
    let rects = spans(*axis, parts, divider.split);
    let pair = (
        rects.get(divider.index),
        rects.get(divider.index + 1),
        parts.get(divider.index),
        parts.get(divider.index + 1),
    );
    let (Some(a), Some(b), Some(a_part), Some(b_part)) = pair else {
        return Err(Refusal::NoSuchTile);
    };
    let ((start, a_length), (b_start, b_length)) = (a.along(*axis), b.along(*axis));
    let gap = b_start - start - a_length;
    Ok(Reach {
        start,
        gap,
        room: b_start + b_length - start - gap,
        least: (least(&a_part.node, *axis), least(&b_part.node, *axis)),
        sizes: (a_part.size, b_part.size),
    })
}

fn pruned(node: Node, tile: TileId) -> Option<Node> {
    match node {
        Node::Tile(stack) => {
            (stack.id != tile || !stack.modules.is_empty()).then_some(Node::Tile(stack))
        }
        Node::Split { axis, parts } => {
            let mut parts: Vec<Part> = parts
                .into_iter()
                .filter_map(|part| {
                    pruned(part.node, tile).map(|node| Part {
                        size: part.size,
                        node,
                    })
                })
                .collect();
            match parts.len() {
                0 => None,
                1 => parts.pop().map(|part| part.node),
                _ => Some(Node::Split { axis, parts }),
            }
        }
    }
}

fn halves(size: Size) -> Size {
    match size {
        Size::Share(share) => Size::Share(share / 2.0),
        Size::Fixed(pixels) => Size::Fixed(pixels / 2.0),
    }
}

fn place(tree: Option<Node>, target: Target, fresh: Stack) -> Result<Node, Refusal> {
    let Some(mut root) = tree else {
        return Ok(Node::Tile(fresh));
    };
    match target {
        Target::Edge(side) => {
            let strip = Part {
                size: Size::Fixed(match side.axis() {
                    Axis::Row => STRIP_WIDTH,
                    Axis::Column => STRIP_HEIGHT,
                }),
                node: Node::Tile(fresh),
            };
            let mut parts = match root {
                Node::Split { axis, parts } if axis == side.axis() => parts,
                other => vec![Part {
                    size: Size::Share(1.0),
                    node: other,
                }],
            };
            if side.leads() {
                parts.insert(0, strip);
            } else {
                parts.push(strip);
            }
            Ok(Node::Split {
                axis: side.axis(),
                parts,
            })
        }
        Target::Tile(id, Zone::Stack { at }) => {
            let mut modules = fresh.modules;
            let found = walk_mut(&mut root, &mut |stack| {
                if stack.id != id {
                    return false;
                }
                let at = at.min(stack.modules.len());
                stack.active = at;
                stack.modules.splice(at..at, modules.drain(..));
                true
            });
            found.then_some(root).ok_or(Refusal::NoSuchTile)
        }
        Target::Tile(id, Zone::Side(side)) => {
            let path = path_of(&root, id).ok_or(Refusal::NoSuchTile)?;
            let (parent, index) = match path.split_last() {
                Some((index, parent)) => (parent, *index),
                None => (&[][..], 0),
            };
            let at = if side.leads() { index } else { index + 1 };
            if let Some(Node::Split { axis, parts }) = at_mut(&mut root, parent)
                && *axis == side.axis()
            {
                let part = parts.get_mut(index).ok_or(Refusal::NoSuchTile)?;
                part.size = halves(part.size);
                let size = part.size;
                parts.insert(
                    at,
                    Part {
                        size,
                        node: Node::Tile(fresh),
                    },
                );
                return Ok(root);
            }
            let tile = at_mut(&mut root, &path).ok_or(Refusal::NoSuchTile)?;
            let old = std::mem::replace(tile, Node::Tile(fresh.clone()));
            let half = |node| Part {
                size: Size::Share(0.5),
                node,
            };
            let (first, second) = if side.leads() {
                (Node::Tile(fresh), old)
            } else {
                (old, Node::Tile(fresh))
            };
            *tile = Node::Split {
                axis: side.axis(),
                parts: vec![half(first), half(second)],
            };
            Ok(root)
        }
    }
}

fn fits(node: &Node, area: Rect) -> Result<(), Refusal> {
    let tiles = solve(node, area);
    if tiles.len() > MAX_TILES {
        return Err(Refusal::TooMany);
    }
    let small = tiles.iter().any(|(stack, rect)| {
        let least = stack.minimum();
        rect.w + SLACK < least.w || rect.h + SLACK < least.h
    });
    if small {
        Err(Refusal::TooSmall)
    } else {
        Ok(())
    }
}

fn count(node: Option<&Node>) -> usize {
    node.map_or(0, |node| {
        solve(
            node,
            Rect {
                x: 0.0,
                y: 0.0,
                w: 0.0,
                h: 0.0,
            },
        )
        .len()
    })
}

fn centre(rect: Rect) -> (f32, f32) {
    (rect.x + rect.w / 2.0, rect.y + rect.h / 2.0)
}

fn longer_side(rect: Rect) -> Side {
    if rect.w > rect.h {
        Side::Right
    } else {
        Side::Bottom
    }
}

impl Workspace {
    pub fn new(name: impl Into<String>, preset: Preset) -> Self {
        let mut next = 0;
        let tree = preset.tree(&mut next);
        Self::restore(name.into(), preset, false, tree, None, next)
    }

    pub(crate) fn restore(
        name: String,
        preset: Preset,
        locked: bool,
        tree: Option<Node>,
        focus: Option<TileId>,
        next: u32,
    ) -> Self {
        let mut workspace = Workspace {
            name,
            preset,
            locked,
            pinned: false,
            tree,
            focus,
            zoom: None,
            history: Vec::new(),
            closed: Vec::new(),
            next,
        };
        workspace.settle();
        workspace
    }

    pub fn tree(&self) -> Option<&Node> {
        self.tree.as_ref()
    }

    pub fn focus(&self) -> Option<TileId> {
        self.focus
    }

    pub fn zoomed(&self) -> Option<TileId> {
        self.zoom
    }

    pub fn can_undo(&self) -> bool {
        !self.history.is_empty()
    }

    pub(crate) fn next_id(&self) -> u32 {
        self.next
    }

    pub fn tiles(&self, area: Rect) -> Vec<(&Stack, Rect)> {
        let Some(tree) = &self.tree else {
            return Vec::new();
        };
        let all = solve(tree, area);
        match self.zoom {
            Some(zoom) => all
                .into_iter()
                .filter(|(stack, _)| stack.id == zoom)
                .map(|(stack, _)| (stack, area))
                .collect(),
            None => all,
        }
    }

    pub fn dividers(&self, area: Rect) -> Vec<Divider> {
        match (&self.tree, self.zoom) {
            (Some(tree), None) => dividers(tree, area),
            (Some(_), Some(_)) | (None, _) => Vec::new(),
        }
    }

    pub fn stack(&self, id: TileId) -> Option<&Stack> {
        let tree = self.tree.as_ref()?;
        solve(
            tree,
            Rect {
                x: 0.0,
                y: 0.0,
                w: 0.0,
                h: 0.0,
            },
        )
        .into_iter()
        .map(|(stack, _)| stack)
        .find(|stack| stack.id == id)
    }

    fn settle(&mut self) {
        let ids: Vec<TileId> = self
            .tree
            .as_ref()
            .map(|tree| {
                solve(
                    tree,
                    Rect {
                        x: 0.0,
                        y: 0.0,
                        w: 0.0,
                        h: 0.0,
                    },
                )
                .into_iter()
                .map(|(stack, _)| stack.id)
                .collect()
            })
            .unwrap_or_default();
        if !self.focus.is_some_and(|focus| ids.contains(&focus)) {
            self.focus = ids.first().copied();
        }
        if !self.zoom.is_some_and(|zoom| ids.contains(&zoom)) {
            self.zoom = None;
        }
    }

    fn changed(&self, tree: Option<Node>, focus: Option<TileId>) -> Self {
        let mut next = self.clone();
        if tree != self.tree {
            next.history.push(self.tree.clone());
            if next.history.len() > HISTORY_DEPTH {
                next.history.remove(0);
            }
            next.tree = tree;
            next.zoom = None;
        }
        next.focus = focus;
        next.settle();
        next
    }

    fn fresh(&self, module: Module) -> Stack {
        Stack {
            id: TileId(self.next),
            modules: vec![module],
            active: 0,
        }
    }

    fn with_new(&self, tree: Node, area: Rect, focus: TileId) -> Result<Self, Refusal> {
        fits(&tree, area)?;
        let mut next = self.changed(Some(tree), Some(focus));
        next.next = self.next.saturating_add(1);
        Ok(next)
    }

    pub fn open(&self, module: Module, area: Rect) -> Result<Self, Refusal> {
        if self.locked {
            return Err(Refusal::Locked);
        }
        if count(self.tree.as_ref()) >= MAX_TILES {
            return Err(Refusal::TooMany);
        }
        let fresh = self.fresh(module);
        let Some(tree) = &self.tree else {
            return self.with_new(Node::Tile(fresh.clone()), area, fresh.id);
        };
        let mut candidates = solve(tree, area);
        candidates.sort_by(|a, b| (b.1.w * b.1.h).total_cmp(&(a.1.w * a.1.h)));
        let mut refusal = Refusal::NoSuchTile;
        for (stack, rect) in candidates {
            let placed = place(
                Some(tree.clone()),
                Target::Tile(stack.id, Zone::Side(longer_side(rect))),
                fresh.clone(),
            )
            .and_then(|tree| self.with_new(tree, area, fresh.id));
            match placed {
                Ok(next) => return Ok(next),
                Err(error) => refusal = error,
            }
        }
        Err(refusal)
    }

    pub fn spawn(&self, module: Module, area: Rect) -> Result<Spawned, Refusal> {
        match self.open(module.clone(), area) {
            Ok(next) => Ok(Spawned::Here(next)),
            Err(Refusal::TooSmall | Refusal::TooMany | Refusal::Locked) => {
                Workspace::new(module.name().to_lowercase(), Preset::Empty)
                    .open(module, area)
                    .map(Spawned::NewTab)
            }
            Err(refusal) => Err(refusal),
        }
    }

    pub fn focused_rect(&self, area: Rect) -> Option<Rect> {
        let focus = self.focus?;
        self.tiles(area)
            .into_iter()
            .find(|(stack, _)| stack.id == focus)
            .map(|(_, rect)| rect)
    }

    pub fn open_at(&self, module: Module, target: Target, area: Rect) -> Result<Self, Refusal> {
        if self.locked {
            return Err(Refusal::Locked);
        }
        let stacking = matches!(target, Target::Tile(_, Zone::Stack { .. }));
        if !stacking && count(self.tree.as_ref()) >= MAX_TILES {
            return Err(Refusal::TooMany);
        }
        let fresh = self.fresh(module);
        let tree = place(self.tree.clone(), target, fresh.clone())?;
        let focus = match target {
            Target::Tile(id, Zone::Stack { .. }) => id,
            Target::Tile(_, Zone::Side(_)) | Target::Edge(_) => fresh.id,
        };
        self.with_new(tree, area, focus)
    }

    pub fn dropped(&self, grab: &Grab, target: Target, area: Rect) -> Result<Self, Refusal> {
        match grab {
            Grab::New(module) => self.open_at(module.clone(), target, area),
            Grab::Placed { tile, index } => self.move_to(*tile, *index, target, area),
        }
    }

    pub fn preview(&self, grab: &Grab, target: Target, area: Rect) -> Option<Preview> {
        let landed = self.dropped(grab, target, area).ok()?;
        if landed.tree == self.tree {
            return None;
        }
        let rect = landed.focused_rect(area)?;
        if self.tree.is_none() {
            return Some(Preview::Split(rect));
        }
        Some(match target {
            Target::Tile(tile, Zone::Stack { at }) => Preview::Stack {
                tile,
                outline: self
                    .tiles(area)
                    .into_iter()
                    .find(|(stack, _)| stack.id == tile)
                    .map(|(_, outline)| outline)?,
                at,
            },
            Target::Tile(_, Zone::Side(_)) => Preview::Split(rect),
            Target::Edge(side) => Preview::Edge {
                strip: rect,
                bar: match side {
                    Side::Left => Rect {
                        w: EDGE_BAR,
                        ..rect
                    },
                    Side::Right => Rect {
                        x: rect.x + rect.w - EDGE_BAR,
                        w: EDGE_BAR,
                        ..rect
                    },
                    Side::Top => Rect {
                        h: EDGE_BAR,
                        ..rect
                    },
                    Side::Bottom => Rect {
                        y: rect.y + rect.h - EDGE_BAR,
                        h: EDGE_BAR,
                        ..rect
                    },
                },
            },
        })
    }

    pub fn send_to_new(&self, tile: TileId, area: Rect) -> Result<(Self, Self), Refusal> {
        let stack = self.stack(tile).ok_or(Refusal::NoSuchTile)?;
        let name = stack
            .modules
            .get(stack.active)
            .map_or("workspace", Module::name)
            .to_lowercase();
        let moved = Stack {
            id: TileId(0),
            ..stack.clone()
        };
        let fresh =
            Workspace::restore(name, Preset::Empty, false, Some(Node::Tile(moved)), None, 1);
        let left = Workspace {
            closed: self.closed.clone(),
            ..self.close_tile(tile, area)?
        };
        Ok((left, fresh))
    }

    pub fn move_to(
        &self,
        tile: TileId,
        index: usize,
        target: Target,
        area: Rect,
    ) -> Result<Self, Refusal> {
        let mut tree = self.tree.clone().ok_or(Refusal::NoSuchTile)?;
        let alone = self.stack(tile).ok_or(Refusal::NoSuchTile)?.modules.len() == 1;
        if alone && matches!(target, Target::Tile(id, _) if id == tile) {
            return self.focus_tile(tile);
        }
        let mut taken = None;
        walk_mut(&mut tree, &mut |stack| {
            if stack.id != tile || index >= stack.modules.len() {
                return false;
            }
            taken = Some(stack.modules.remove(index));
            stack.active = stack.active.min(stack.modules.len().saturating_sub(1));
            true
        });
        let module = taken.ok_or(Refusal::NoSuchTile)?;
        let target = match target {
            Target::Tile(on, Zone::Stack { at }) if on == tile && index < at => {
                Target::Tile(on, Zone::Stack { at: at - 1 })
            }
            other => other,
        };
        let id = if alone { tile } else { TileId(self.next) };
        let fresh = Stack {
            id,
            modules: vec![module],
            active: 0,
        };
        let tree = place(pruned(tree, tile), target, fresh)?;
        let focus = match target {
            Target::Tile(on, Zone::Stack { .. }) => on,
            Target::Tile(_, Zone::Side(_)) | Target::Edge(_) => id,
        };
        if alone {
            fits(&tree, area)?;
            Ok(self.changed(Some(tree), Some(focus)))
        } else {
            self.with_new(tree, area, focus)
        }
    }

    pub fn swap(&self, a: TileId, b: TileId) -> Result<Self, Refusal> {
        let first = self.stack(a).ok_or(Refusal::NoSuchTile)?.clone();
        let second = self.stack(b).ok_or(Refusal::NoSuchTile)?.clone();
        let mut tree = self.tree.clone().ok_or(Refusal::NoSuchTile)?;
        walk_mut(&mut tree, &mut |stack| {
            if stack.id == a {
                *stack = second.clone();
            } else if stack.id == b {
                *stack = first.clone();
            }
            false
        });
        Ok(self.changed(Some(tree), self.focus))
    }

    pub fn activate(&self, tile: TileId, module: usize) -> Result<Self, Refusal> {
        let mut tree = self.tree.clone().ok_or(Refusal::NoSuchTile)?;
        let found = walk_mut(&mut tree, &mut |stack| {
            if stack.id != tile || module >= stack.modules.len() {
                return false;
            }
            stack.active = module;
            true
        });
        if !found {
            return Err(Refusal::NoSuchTile);
        }
        let mut next = self.clone();
        next.tree = Some(tree);
        next.focus = Some(tile);
        Ok(next)
    }

    pub fn focus_tile(&self, tile: TileId) -> Result<Self, Refusal> {
        self.stack(tile).ok_or(Refusal::NoSuchTile)?;
        let mut next = self.clone();
        next.focus = Some(tile);
        Ok(next)
    }

    pub fn close_module(&self, tile: TileId, module: usize, area: Rect) -> Result<Self, Refusal> {
        self.close(tile, Some(module), area)
    }

    pub fn close_tile(&self, tile: TileId, area: Rect) -> Result<Self, Refusal> {
        self.close(tile, None, area)
    }

    fn close(&self, tile: TileId, module: Option<usize>, area: Rect) -> Result<Self, Refusal> {
        let tree = self.tree.as_ref().ok_or(Refusal::NoSuchTile)?;
        let (_, gone) = solve(tree, area)
            .into_iter()
            .find(|(stack, _)| stack.id == tile)
            .ok_or(Refusal::NoSuchTile)?;
        let mut tree = tree.clone();
        let mut removed = Vec::new();
        let found = walk_mut(&mut tree, &mut |stack| {
            if stack.id != tile || module.is_some_and(|module| module >= stack.modules.len()) {
                return false;
            }
            removed = match module {
                Some(module) => vec![(stack.modules.remove(module), module)],
                None => stack.modules.drain(..).zip(0..).collect(),
            };
            stack.active = stack.active.min(stack.modules.len().saturating_sub(1));
            true
        });
        if !found {
            return Err(Refusal::NoSuchTile);
        }
        let tree = pruned(tree, tile);
        let (cx, cy) = centre(gone);
        let focus = tree.as_ref().and_then(|tree| {
            solve(tree, area)
                .into_iter()
                .min_by(|a, b| {
                    let distance = |rect: Rect| {
                        let (x, y) = centre(rect);
                        (x - cx).hypot(y - cy)
                    };
                    distance(a.1).total_cmp(&distance(b.1))
                })
                .map(|(stack, _)| stack.id)
        });
        let focus = if Some(tile) == self.focus || focus.is_none() {
            focus
        } else {
            self.focus
        };
        let mut next = self.changed(tree, focus);
        next.closed.extend(removed);
        let excess = next.closed.len().saturating_sub(CLOSED_DEPTH);
        next.closed.drain(..excess);
        Ok(next)
    }

    pub fn close_tab(&self, area: Rect) -> Result<Self, Refusal> {
        if self.locked {
            return Err(Refusal::Locked);
        }
        let focus = self.focus.ok_or(Refusal::NoSuchTile)?;
        let stack = self.stack(focus).ok_or(Refusal::NoSuchTile)?;
        if stack.modules.is_empty() {
            self.close_tile(focus, area)
        } else {
            self.close_module(focus, stack.active, area)
        }
    }

    pub fn reopen(&self, area: Rect) -> Result<Self, Refusal> {
        let mut closed = self.closed.clone();
        let (module, at) = closed.pop().ok_or(Refusal::NothingClosed)?;
        let mut next = match self.focus {
            Some(tile) => self.open_at(module, Target::Tile(tile, Zone::Stack { at }), area),
            None => self.open(module, area),
        }?;
        next.closed = closed;
        Ok(next)
    }

    pub fn step_tab(&self, forward: bool) -> Result<Self, Refusal> {
        let mut tabs = Vec::new();
        let mut pending: Vec<&Node> = self.tree.iter().collect();
        while let Some(node) = pending.pop() {
            match node {
                Node::Tile(stack) if stack.modules.is_empty() => tabs.push((stack.id, None)),
                Node::Tile(stack) => {
                    tabs.extend((0..stack.modules.len()).map(|index| (stack.id, Some(index))))
                }
                Node::Split { parts, .. } => {
                    pending.extend(parts.iter().rev().map(|part| &part.node))
                }
            }
        }
        if !forward {
            tabs.reverse();
        }
        let current = self.focus.and_then(|focus| self.stack(focus)).map(|stack| {
            (
                stack.id,
                (!stack.modules.is_empty()).then_some(stack.active),
            )
        });
        let here = tabs.iter().position(|tab| Some(*tab) == current);
        let (tile, module) = tabs
            .iter()
            .cycle()
            .nth(here.map_or(0, |here| here + 1))
            .copied()
            .ok_or(Refusal::NoSuchTile)?;
        let mut next = match module {
            Some(module) => self.activate(tile, module)?,
            None => self.focus_tile(tile)?,
        };
        if next.zoom.is_some() {
            next.zoom = next.focus;
        }
        Ok(next)
    }

    pub fn split(&self, area: Rect) -> Result<Self, Refusal> {
        if self.locked {
            return Err(Refusal::Locked);
        }
        if count(self.tree.as_ref()) >= MAX_TILES {
            return Err(Refusal::TooMany);
        }
        let focus = self.focus.ok_or(Refusal::NoSuchTile)?;
        let rect = self.focused_rect(area).ok_or(Refusal::NoSuchTile)?;
        let fresh = Stack {
            id: TileId(self.next),
            modules: Vec::new(),
            active: 0,
        };
        let id = fresh.id;
        let target = Target::Tile(focus, Zone::Side(longer_side(rect)));
        let tree = place(self.tree.clone(), target, fresh)?;
        self.with_new(tree, area, id)
    }

    pub fn corners(&self, area: Rect) -> Vec<Corner> {
        match (&self.tree, self.zoom) {
            (Some(tree), None) => corners(tree, area),
            (Some(_), Some(_)) | (None, _) => Vec::new(),
        }
    }

    pub fn resize_corner(&self, corner: &Corner, dx: f32, dy: f32) -> Result<Self, Refusal> {
        let mut next = self.clone();
        for divider in &corner.dividers {
            let reach = reach(next.tree.as_ref(), divider)?;
            let (x, y) = centre(divider.rect);
            let (low, high) = reach.bounds();
            next = match divider.axis {
                Axis::Row => next.resize(divider, (x + dx).max(low).min(high), y)?,
                Axis::Column => next.resize(divider, x, (y + dy).max(low).min(high))?,
            };
        }
        Ok(self.changed(next.tree, self.focus))
    }

    pub fn resize(&self, divider: &Divider, x: f32, y: f32) -> Result<Self, Refusal> {
        let Reach {
            start,
            gap,
            room,
            least: (a_least, b_least),
            sizes: (a_size, b_size),
        } = reach(self.tree.as_ref(), divider)?;
        let at = match divider.axis {
            Axis::Row => x,
            Axis::Column => y,
        };
        let first = at - start - gap / 2.0;
        if first + SLACK < a_least || room - first + SLACK < b_least {
            return Err(Refusal::TooSmall);
        }
        let first = first.max(a_least).min(room - b_least);
        let mut tree = self.tree.clone().ok_or(Refusal::NoSuchTile)?;
        let Some(Node::Split { parts, .. }) = at_mut(&mut tree, &divider.path) else {
            return Err(Refusal::NoSuchTile);
        };
        let sizes = match (a_size, b_size) {
            (Size::Share(one), Size::Share(two)) => {
                let pair = one + two;
                (
                    Size::Share(pair * first / room),
                    Size::Share(pair * (room - first) / room),
                )
            }
            (Size::Fixed(_), Size::Fixed(_)) => (Size::Fixed(first), Size::Fixed(room - first)),
            (Size::Fixed(_), share) => (Size::Fixed(first), share),
            (share, Size::Fixed(_)) => (share, Size::Fixed(room - first)),
        };
        if let Some(part) = parts.get_mut(divider.index) {
            part.size = sizes.0;
        }
        if let Some(part) = parts.get_mut(divider.index + 1) {
            part.size = sizes.1;
        }
        Ok(self.changed(Some(tree), self.focus))
    }

    pub fn nudge(&self, side: Side, step: f32, area: Rect) -> Result<Self, Refusal> {
        let tree = self.tree.as_ref().ok_or(Refusal::NoSuchTile)?;
        let focus = self.focus.ok_or(Refusal::NoSuchTile)?;
        let mut path = path_of(tree, focus).ok_or(Refusal::NoSuchTile)?;
        let all = dividers(tree, area);
        while let Some(index) = path.pop() {
            let wanted = if side.leads() {
                index.checked_sub(1)
            } else {
                Some(index)
            };
            let found = all.iter().find(|divider| {
                divider.path == path && divider.axis == side.axis() && Some(divider.index) == wanted
            });
            if let Some(divider) = found {
                let delta = if side.leads() { -step } else { step };
                let (x, y) = centre(divider.rect);
                return match side.axis() {
                    Axis::Row => self.resize(divider, x + delta, y),
                    Axis::Column => self.resize(divider, x, y + delta),
                };
            }
        }
        Err(Refusal::NoSuchTile)
    }

    fn neighbour(&self, side: Side, area: Rect) -> Result<TileId, Refusal> {
        let tree = self.tree.as_ref().ok_or(Refusal::NoSuchTile)?;
        let focus = self.focus.ok_or(Refusal::NoSuchTile)?;
        let tiles = solve(tree, area);
        let (_, from) = tiles
            .iter()
            .find(|(stack, _)| stack.id == focus)
            .ok_or(Refusal::NoSuchTile)?;
        let from = *from;
        let axis = side.axis();
        let (from_start, from_length) = from.along(axis);
        let (from_cross, from_span) = from.across(axis);
        tiles
            .iter()
            .filter(|(stack, _)| stack.id != focus)
            .filter_map(|(stack, rect)| {
                let (start, length) = rect.along(axis);
                let (cross, span) = rect.across(axis);
                let distance = if side.leads() {
                    from_start - (start + length)
                } else {
                    start - (from_start + from_length)
                };
                let overlap = (cross + span).min(from_cross + from_span) - cross.max(from_cross);
                (distance >= -SLACK && overlap > 0.0).then_some((stack.id, distance, overlap))
            })
            .min_by(|a, b| a.1.total_cmp(&b.1).then(b.2.total_cmp(&a.2)))
            .map(|(id, _, _)| id)
            .ok_or(Refusal::NoSuchTile)
    }

    pub fn focus_toward(&self, side: Side, area: Rect) -> Result<Self, Refusal> {
        let id = self.neighbour(side, area)?;
        self.focus_tile(id)
    }

    pub fn move_focused(&self, side: Side, area: Rect) -> Result<Self, Refusal> {
        let focus = self.focus.ok_or(Refusal::NoSuchTile)?;
        let id = self.neighbour(side, area)?;
        self.swap(focus, id)
    }

    pub fn zoom(&self) -> Result<Self, Refusal> {
        let focus = self.focus.ok_or(Refusal::NoSuchTile)?;
        let mut next = self.clone();
        next.zoom = match self.zoom {
            Some(_) => None,
            None => Some(focus),
        };
        Ok(next)
    }

    fn evened(node: &mut Node, path: Option<&[usize]>) {
        let mut pending = vec![(node, Vec::new())];
        while let Some((node, at)) = pending.pop() {
            let Node::Split { parts, .. } = node else {
                continue;
            };
            if path.is_none_or(|path| path == at.as_slice()) {
                let shares = parts
                    .iter()
                    .filter(|part| matches!(part.size, Size::Share(_)))
                    .count()
                    .max(1) as f32;
                for part in parts.iter_mut() {
                    if let Size::Share(_) = part.size {
                        part.size = Size::Share(1.0 / shares);
                    }
                }
            }
            for (index, part) in parts.iter_mut().enumerate() {
                let mut inner = at.clone();
                inner.push(index);
                pending.push((&mut part.node, inner));
            }
        }
    }

    pub fn even(&self) -> Self {
        if self.locked {
            return self.clone();
        }
        let mut tree = self.tree.clone();
        if let Some(tree) = &mut tree {
            Self::evened(tree, None);
        }
        self.changed(tree, self.focus)
    }

    pub fn even_split(&self, divider: &Divider) -> Self {
        if self.locked {
            return self.clone();
        }
        let mut tree = self.tree.clone();
        if let Some(tree) = &mut tree {
            Self::evened(tree, Some(&divider.path));
        }
        self.changed(tree, self.focus)
    }

    pub fn undo(&self) -> Result<Self, Refusal> {
        let mut next = self.clone();
        let tree = next.history.pop().ok_or(Refusal::NoHistory)?;
        next.tree = tree;
        next.zoom = None;
        next.settle();
        Ok(next)
    }

    pub fn reset(&self) -> Self {
        if self.locked {
            return self.clone();
        }
        let mut id = self.next;
        let tree = self.preset.tree(&mut id);
        let mut next = self.changed(tree, None);
        next.next = id;
        next
    }
}
