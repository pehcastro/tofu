use std::time::{Duration, Instant};

use desk_motion::tokens::{EASE_OUT, HOVER_MS, PRESS_MS};
use gpui::{
    App, Bounds, Div, FontWeight, PathBuilder, Pixels, Rgba, SharedString, Stateful, Window,
    canvas, div, point, prelude::*, px, rgb, rgba,
};

use crate::components::chip::{mono, tabular};
use crate::components::glyph::Glyph;
use crate::components::paint::{drop, glyph, ink, ring};
use crate::components::size::{FONT_BODY, FONT_KBD, FONT_SMALL, FONT_WHO, T1, T3};
use crate::theme::{ColorToken, Theme};

const MAP_W: f32 = 748.0;
const MAP_FOOT: f32 = 44.0;
const FIRST_LEAF: f32 = 34.0;
const LEAF_PITCH: f32 = 52.0;
const GROUP_GAP: f32 = 66.0;
const LEAD_X: f32 = 24.0;
const LEAD_W: f32 = 128.0;
const LEAD_H: f32 = 56.0;
const AGENT_X: f32 = 208.0;
const AGENT_W: f32 = 184.0;
const AGENT_H: f32 = 46.0;
const LEAF_X: f32 = 450.0;
const LEAF_W: f32 = 270.0;
const LEAF_H: f32 = 36.0;
const CROSS_BULGE: f32 = 22.0;
const LEAD_EDGE: f32 = 1.4;
const LEAF_EDGE: f32 = 1.2;
const DASH: [f32; 2] = [3.0, 3.0];
const CROSS_DASH: [f32; 2] = [4.0, 3.0];
const EDGE_LIT: f32 = 0.62;
const EDGE_REST: f32 = 0.28;
const EDGE_DIM: f32 = 0.12;
const NODE_DIM: f32 = 0.45;
const LEAD_DIM: f32 = 0.6;
const CROSS_SHOWN: f32 = 0.8;
const PLAIN_FILL: u32 = 0x1e1d24eb;
const PICKED_FILL: u32 = 0x2c2a34f5;
const PLAIN_RING: f32 = 0.1;
const PICKED_RING: f32 = 0.34;
const PICKED_SHADOW: u32 = 0x00000059;
const ADD_TEXT: u32 = 0x7fd6a6;
const DEL_TEXT: u32 = 0xee8a8f;
const OUTLINE_ROW: f32 = 28.0;
const OUTLINE_INDENT: f32 = 14.0;

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Node {
    Lead,
    Agent(usize),
    File(usize, usize),
}

impl Node {
    fn agent(self) -> Option<usize> {
        match self {
            Node::Lead => None,
            Node::Agent(agent) | Node::File(agent, _) => Some(agent),
        }
    }
}

#[derive(Clone, Copy)]
pub enum Tone {
    Live,
    Failed,
    Done,
}

#[derive(Clone, Copy)]
pub enum Change {
    New,
    Modified,
}

#[derive(Clone)]
pub struct FileLeaf {
    pub path: SharedString,
    pub add: SharedString,
    pub del: SharedString,
    pub change: Change,
    pub undone: bool,
}

#[derive(Clone)]
pub enum Leaves {
    Files(Vec<FileLeaf>),
    Reads(SharedString),
}

impl Leaves {
    fn count(&self) -> usize {
        match self {
            Leaves::Files(files) => files.len().max(1),
            Leaves::Reads(_) => 1,
        }
    }
}

#[derive(Clone)]
pub struct AgentNode {
    pub name: SharedString,
    pub meta: SharedString,
    pub tone: Tone,
    pub owns: SharedString,
    pub leaves: Leaves,
}

#[derive(Clone, Copy)]
pub struct Cross {
    pub from: (usize, usize),
    pub to: (usize, usize),
}

impl Cross {
    fn shown_for(self, node: Node) -> bool {
        node == Node::File(self.from.0, self.from.1)
            || node == Node::File(self.to.0, self.to.1)
            || node == Node::Agent(self.from.0)
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Fit {
    Board,
    Outline,
}

#[derive(IntoElement)]
pub struct DelegationMap {
    id: SharedString,
    theme: Theme,
    lead: (SharedString, SharedString),
    agents: Vec<AgentNode>,
    cross: Option<Cross>,
    picked: Option<Node>,
    fit: Fit,
}

impl DelegationMap {
    pub fn new(
        id: impl Into<SharedString>,
        theme: &Theme,
        lead: (SharedString, SharedString),
        agents: Vec<AgentNode>,
    ) -> Self {
        DelegationMap {
            id: id.into(),
            theme: theme.clone(),
            lead,
            agents,
            cross: None,
            picked: None,
            fit: Fit::Board,
        }
    }

    pub fn cross(mut self, cross: Cross) -> Self {
        self.cross = Some(cross);
        self
    }

    pub fn picked(mut self, node: Node) -> Self {
        self.picked = Some(node);
        self
    }

    pub fn fit(mut self, fit: Fit) -> Self {
        self.fit = fit;
        self
    }
}

#[derive(Clone, Copy)]
struct Fade {
    from: f32,
    to: f32,
    start: Instant,
    span: Duration,
}

impl Fade {
    fn still(value: f32, now: Instant) -> Self {
        Fade {
            from: value,
            to: value,
            start: now,
            span: HOVER_MS,
        }
    }

    fn progress(&self, now: Instant) -> f32 {
        let spent = now.saturating_duration_since(self.start).as_secs_f32();
        (spent / self.span.as_secs_f32()).clamp(0.0, 1.0)
    }

    fn at(&self, now: Instant) -> f32 {
        self.from + (self.to - self.from) * EASE_OUT(self.progress(now))
    }

    fn aim(&mut self, to: f32, settling: bool, now: Instant) {
        if to != self.to {
            *self = Fade {
                from: self.at(now),
                to,
                start: now,
                span: if settling { PRESS_MS } else { HOVER_MS },
            };
        }
    }
}

struct Looks {
    edges: Vec<f32>,
    nodes: Vec<f32>,
    lead: f32,
    cross: f32,
    picked: Option<Node>,
}

struct Focus {
    hovered: Option<Node>,
    picked: Option<Node>,
    edges: Vec<Fade>,
    nodes: Vec<Fade>,
    lead: Fade,
    cross: Fade,
}

impl Focus {
    fn targets(&self, agents: usize, cross: Option<Cross>) -> (Vec<f32>, Vec<f32>, f32, f32) {
        let node = self.hovered.or(self.picked);
        let focus = node.and_then(Node::agent);
        let edges = (0..agents)
            .map(|ix| match focus {
                Some(agent) if agent == ix => EDGE_LIT,
                Some(_) => EDGE_DIM,
                None => EDGE_REST,
            })
            .collect();
        let nodes = (0..agents)
            .map(|ix| match focus {
                Some(agent) if agent != ix => NODE_DIM,
                _ => 1.0,
            })
            .collect();
        let lead = if focus.is_some() { LEAD_DIM } else { 1.0 };
        let shown = matches!((cross, node), (Some(cross), Some(node)) if cross.shown_for(node));
        (edges, nodes, lead, if shown { CROSS_SHOWN } else { 0.0 })
    }

    fn new(picked: Option<Node>, agents: usize, cross: Option<Cross>, now: Instant) -> Self {
        let mut focus = Focus {
            hovered: None,
            picked,
            edges: Vec::new(),
            nodes: Vec::new(),
            lead: Fade::still(1.0, now),
            cross: Fade::still(0.0, now),
        };
        let (edges, nodes, lead, shown) = focus.targets(agents, cross);
        focus.edges = edges.into_iter().map(|v| Fade::still(v, now)).collect();
        focus.nodes = nodes.into_iter().map(|v| Fade::still(v, now)).collect();
        focus.lead = Fade::still(lead, now);
        focus.cross = Fade::still(shown, now);
        focus
    }

    fn frame(&mut self, agents: usize, cross: Option<Cross>, now: Instant) -> (Looks, bool) {
        let (edges, nodes, lead, shown) = self.targets(agents, cross);
        let settling = self.hovered.or(self.picked).is_none();
        let aim = |fades: &mut Vec<Fade>, to: Vec<f32>| {
            fades.resize(to.len(), Fade::still(1.0, now));
            for (fade, to) in fades.iter_mut().zip(to) {
                fade.aim(to, settling, now);
            }
        };
        aim(&mut self.edges, edges);
        aim(&mut self.nodes, nodes);
        self.lead.aim(lead, settling, now);
        self.cross.aim(shown, settling, now);
        let all = || {
            self.edges
                .iter()
                .chain(&self.nodes)
                .chain([&self.lead, &self.cross])
        };
        let moving = all().any(|fade| fade.progress(now) < 1.0);
        let looks = Looks {
            edges: self.edges.iter().map(|fade| fade.at(now)).collect(),
            nodes: self.nodes.iter().map(|fade| fade.at(now)).collect(),
            lead: self.lead.at(now),
            cross: self.cross.at(now),
            picked: self.picked,
        };
        (looks, moving)
    }

    fn hover(&mut self, node: Node, inside: bool) -> bool {
        let next = if inside {
            Some(node)
        } else {
            self.hovered.filter(|held| *held != node)
        };
        let changed = next != self.hovered;
        self.hovered = next;
        changed
    }

    fn pick(&mut self, node: Node) {
        self.picked = if self.picked == Some(node) {
            None
        } else {
            Some(node)
        };
    }
}

struct Edge {
    from: (f32, f32),
    to: (f32, f32),
    width: f32,
    dash: Option<[f32; 2]>,
    owner: Option<usize>,
}

struct Layout {
    lead: f32,
    agents: Vec<f32>,
    leaves: Vec<Vec<f32>>,
    height: f32,
}

fn layout(agents: &[AgentNode]) -> Layout {
    let mut next = FIRST_LEAF;
    let mut centers = Vec::new();
    let mut leaves = Vec::new();
    for agent in agents {
        let ys: Vec<f32> = (0..agent.leaves.count())
            .map(|ix| next + ix as f32 * LEAF_PITCH)
            .collect();
        let first = ys.first().copied().unwrap_or(next);
        let last = ys.last().copied().unwrap_or(next);
        centers.push((first + last) / 2.0);
        next = last + GROUP_GAP;
        leaves.push(ys);
    }
    let top = centers.first().copied().unwrap_or(FIRST_LEAF);
    let bottom = centers.last().copied().unwrap_or(FIRST_LEAF);
    Layout {
        lead: (top + bottom) / 2.0,
        agents: centers,
        height: next - GROUP_GAP + MAP_FOOT,
        leaves,
    }
}

fn edges(agents: &[AgentNode], at: &Layout, cross: Option<Cross>) -> Vec<Edge> {
    let mut out = Vec::new();
    for (ix, agent) in agents.iter().enumerate() {
        let y = at.agents.get(ix).copied().unwrap_or_default();
        let dash = matches!(agent.leaves, Leaves::Reads(_)).then_some(DASH);
        out.push(Edge {
            from: (LEAD_X + LEAD_W, at.lead),
            to: (AGENT_X, y),
            width: LEAD_EDGE,
            dash,
            owner: Some(ix),
        });
        for leaf in at.leaves.get(ix).into_iter().flatten() {
            out.push(Edge {
                from: (AGENT_X + AGENT_W, y),
                to: (LEAF_X, *leaf),
                width: LEAF_EDGE,
                dash,
                owner: Some(ix),
            });
        }
    }
    let leaf_y = |(agent, file): (usize, usize)| {
        at.leaves
            .get(agent)
            .and_then(|ys| ys.get(file))
            .copied()
            .unwrap_or_default()
    };
    if let Some(cross) = cross {
        out.push(Edge {
            from: (LEAF_X + LEAF_W, leaf_y(cross.from)),
            to: (LEAF_X + LEAF_W, leaf_y(cross.to)),
            width: LEAF_EDGE,
            dash: Some(CROSS_DASH),
            owner: None,
        });
    }
    out
}

fn paint_edges(
    edges: &[Edge],
    (lit, cross): (&[f32], f32),
    theme: &Theme,
    bounds: Bounds<Pixels>,
    window: &mut Window,
) {
    let at = |(x, y): (f32, f32)| bounds.origin + point(px(x), px(y));
    for edge in edges {
        let color = match edge.owner {
            Some(ix) => ink(theme, lit.get(ix).copied().unwrap_or(EDGE_REST)),
            None => Rgba {
                alpha: cross,
                ..rgb(DEL_TEXT)
            },
        };
        if color.alpha <= 0.0 {
            continue;
        }
        let mut path = PathBuilder::stroke(px(edge.width));
        if let Some(dash) = edge.dash {
            path = path.dash_array(&dash.map(px));
        }
        path.move_to(at(edge.from));
        match edge.owner {
            None => path.cubic_bezier_to(
                at(edge.to),
                at((edge.from.0 + CROSS_BULGE, edge.from.1)),
                at((edge.to.0 + CROSS_BULGE, edge.to.1)),
            ),
            Some(_) if edge.from.1 == edge.to.1 => path.line_to(at(edge.to)),
            Some(_) => {
                let mid = (edge.from.0 + edge.to.0) / 2.0;
                path.cubic_bezier_to(at(edge.to), at((mid, edge.from.1)), at((mid, edge.to.1)));
            }
        }
        if let Ok(path) = path.build() {
            window.paint_path(path, color);
        }
    }
}

fn surface(theme: &Theme, picked: bool) -> Div {
    let card = div().overflow_hidden().min_w_0();
    if picked {
        card.bg(rgba(PICKED_FILL)).shadow(vec![
            ring(ink(theme, PICKED_RING)),
            drop(rgba(PICKED_SHADOW), 6.0, 18.0),
        ])
    } else {
        card.bg(rgba(PLAIN_FILL))
            .shadow(vec![ring(ink(theme, PLAIN_RING))])
    }
}

fn tone(theme: &Theme, tone: Tone) -> Rgba {
    match tone {
        Tone::Live => theme.color(ColorToken::StatusLive),
        Tone::Failed => rgb(DEL_TEXT),
        Tone::Done => ink(theme, T3),
    }
}

fn path_color(theme: &Theme, leaf: &FileLeaf) -> Rgba {
    match (leaf.undone, leaf.change) {
        (true, _) => theme.color(ColorToken::GitDeleted),
        (false, Change::New) => theme.color(ColorToken::GitAdded),
        (false, Change::Modified) => theme.color(ColorToken::GitModified),
    }
}

fn line(text: SharedString) -> Div {
    div().min_w_0().truncate().child(text)
}

fn agent_body(theme: &Theme, agent: &AgentNode, outline: bool) -> Div {
    let head = div()
        .flex()
        .items_center()
        .gap(px(7.0))
        .min_w_0()
        .text_size(px(FONT_BODY))
        .line_height(px(17.0))
        .child(
            div()
                .text_size(px(8.0))
                .text_color(tone(theme, agent.tone))
                .child("●"),
        )
        .child(line(agent.name.clone()).flex_1().text_color(ink(theme, T1)))
        .child(
            div()
                .flex_none()
                .text_size(px(FONT_WHO))
                .text_color(match agent.tone {
                    Tone::Failed => rgb(DEL_TEXT),
                    Tone::Live | Tone::Done => ink(theme, T3),
                })
                .child(agent.meta.clone()),
        );
    if outline {
        return head;
    }
    div()
        .flex()
        .flex_col()
        .justify_center()
        .gap(px(2.0))
        .min_w_0()
        .child(head)
        .child(
            line(agent.owns.clone())
                .font_family(mono(theme))
                .text_size(px(FONT_KBD))
                .line_height(px(14.0))
                .text_color(ink(theme, T3)),
        )
}

fn file_body(theme: &Theme, leaf: &FileLeaf, crossed: bool) -> Div {
    let figure = |text: SharedString, color: u32| {
        div()
            .flex_none()
            .font_family(mono(theme))
            .font_features(tabular())
            .text_size(px(FONT_KBD))
            .text_color(rgb(color))
            .child(text)
    };
    let (add, del) = if leaf.undone {
        (SharedString::default(), SharedString::from("undone"))
    } else {
        (leaf.add.clone(), leaf.del.clone())
    };
    div()
        .flex()
        .items_center()
        .gap(px(8.0))
        .min_w_0()
        .child(glyph(Glyph::File, 14.0, ink(theme, T3)))
        .child(
            line(leaf.path.clone())
                .flex_1()
                .font_family(mono(theme))
                .text_size(px(FONT_WHO))
                .text_color(path_color(theme, leaf))
                .when(leaf.undone, |text| text.line_through()),
        )
        .when(crossed, |row| row.child(figure("cross".into(), DEL_TEXT)))
        .child(figure(add, ADD_TEXT))
        .child(figure(del, DEL_TEXT))
}

fn reads_body(theme: &Theme, reads: &SharedString) -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(8.0))
        .min_w_0()
        .text_size(px(FONT_SMALL))
        .text_color(ink(theme, T3))
        .child(line(reads.clone()))
}

impl RenderOnce for DelegationMap {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let DelegationMap {
            id,
            theme,
            lead,
            agents,
            cross,
            picked,
            fit,
        } = self;
        let count = agents.len();
        let now = Instant::now();
        let state =
            window.use_keyed_state(id.clone(), cx, |_, _| Focus::new(picked, count, cross, now));
        let (looks, moving) = state.update(cx, |focus, _| focus.frame(count, cross, now));
        if moving {
            window.request_animation_frame();
        }
        let node = |name: String, node: Node, card: Div| -> Stateful<Div> {
            let (hover, click) = (state.clone(), state.clone());
            card.id(SharedString::from(format!("{id}-{name}")))
                .cursor_pointer()
                .on_hover(move |inside, _, cx| {
                    hover.update(cx, |focus, cx| {
                        if focus.hover(node, *inside) {
                            cx.notify();
                        }
                    });
                })
                .on_click(move |_, _, cx| {
                    click.update(cx, |focus, cx| {
                        focus.pick(node);
                        cx.notify();
                    });
                })
        };
        let is = |n: Node| looks.picked == Some(n);
        let crossed = |agent: usize, file: usize| {
            cross.is_some_and(|c| c.from == (agent, file) || c.to == (agent, file))
        };
        let dim = |agent: usize| looks.nodes.get(agent).copied().unwrap_or(1.0);
        let lead_body = div()
            .flex()
            .flex_col()
            .justify_center()
            .min_w_0()
            .line_height(px(18.0))
            .child(
                line(lead.0.clone())
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(ink(&theme, T1)),
            )
            .child(
                line(lead.1.clone())
                    .text_size(px(FONT_SMALL))
                    .text_color(ink(&theme, T3)),
            );
        let mut items = Vec::new();
        match fit {
            Fit::Board => {
                let at = layout(&agents);
                let lines = edges(&agents, &at, cross);
                let painted = looks.edges.clone();
                let cross_alpha = looks.cross;
                let paint_theme = theme.clone();
                items.push(
                    canvas(
                        |_, _, _| (),
                        move |bounds, (), window, _| {
                            paint_edges(
                                &lines,
                                (&painted, cross_alpha),
                                &paint_theme,
                                bounds,
                                window,
                            );
                        },
                    )
                    .absolute()
                    .size_full()
                    .into_any_element(),
                );
                let place = |card: Stateful<Div>, x: f32, y: f32, w: f32, h: f32, r: f32| {
                    card.absolute()
                        .left(px(x))
                        .top(px(y - h / 2.0))
                        .w(px(w))
                        .h(px(h))
                        .rounded(px(r))
                        .into_any_element()
                };
                items.push(place(
                    node(
                        "lead".into(),
                        Node::Lead,
                        surface(&theme, is(Node::Lead))
                            .px(px(14.0))
                            .flex()
                            .child(lead_body.flex_1()),
                    )
                    .opacity(looks.lead),
                    LEAD_X,
                    at.lead,
                    LEAD_W,
                    LEAD_H,
                    12.0,
                ));
                for (ix, agent) in agents.iter().enumerate() {
                    let y = at.agents.get(ix).copied().unwrap_or_default();
                    let card = surface(&theme, is(Node::Agent(ix)))
                        .px(px(12.0))
                        .flex()
                        .child(agent_body(&theme, agent, false).flex_1());
                    items.push(place(
                        node(format!("a{ix}"), Node::Agent(ix), card).opacity(dim(ix)),
                        AGENT_X,
                        y,
                        AGENT_W,
                        AGENT_H,
                        10.0,
                    ));
                    let ys = at.leaves.get(ix).cloned().unwrap_or_default();
                    match &agent.leaves {
                        Leaves::Files(files) => {
                            for (fx, (leaf, y)) in files.iter().zip(ys).enumerate() {
                                let card = surface(&theme, is(Node::File(ix, fx)))
                                    .px(px(11.0))
                                    .flex()
                                    .items_center()
                                    .child(file_body(&theme, leaf, false).flex_1());
                                items.push(place(
                                    node(format!("f{ix}-{fx}"), Node::File(ix, fx), card)
                                        .opacity(dim(ix)),
                                    LEAF_X,
                                    y,
                                    LEAF_W,
                                    LEAF_H,
                                    9.0,
                                ));
                            }
                        }
                        Leaves::Reads(reads) => {
                            let card = div()
                                .px(px(11.0))
                                .flex()
                                .items_center()
                                .shadow(vec![ring(ink(&theme, PLAIN_RING))])
                                .child(reads_body(&theme, reads).flex_1());
                            items.push(place(
                                node(format!("r{ix}"), Node::Agent(ix), card).opacity(dim(ix)),
                                LEAF_X,
                                ys.first().copied().unwrap_or_default(),
                                LEAF_W,
                                LEAF_H,
                                9.0,
                            ));
                        }
                    }
                }
                div()
                    .relative()
                    .flex_none()
                    .w(px(MAP_W))
                    .h(px(at.height))
                    .children(items)
            }
            Fit::Outline => {
                let row = |depth: usize, card: Div| {
                    card.flex()
                        .items_center()
                        .h(px(OUTLINE_ROW))
                        .pl(px(8.0 + depth as f32 * OUTLINE_INDENT))
                        .pr(px(8.0))
                        .rounded(px(8.0))
                        .min_w_0()
                };
                items.push(
                    node(
                        "lead".into(),
                        Node::Lead,
                        row(0, surface(&theme, is(Node::Lead)))
                            .h(px(LEAD_H - 8.0))
                            .child(lead_body.flex_1()),
                    )
                    .opacity(looks.lead)
                    .into_any_element(),
                );
                for (ix, agent) in agents.iter().enumerate() {
                    items.push(
                        node(
                            format!("a{ix}"),
                            Node::Agent(ix),
                            row(1, surface(&theme, is(Node::Agent(ix))))
                                .child(agent_body(&theme, agent, true).flex_1()),
                        )
                        .opacity(dim(ix))
                        .into_any_element(),
                    );
                    match &agent.leaves {
                        Leaves::Files(files) => {
                            for (fx, leaf) in files.iter().enumerate() {
                                items.push(
                                    node(
                                        format!("f{ix}-{fx}"),
                                        Node::File(ix, fx),
                                        row(2, surface(&theme, is(Node::File(ix, fx)))).child(
                                            file_body(&theme, leaf, crossed(ix, fx)).flex_1(),
                                        ),
                                    )
                                    .opacity(dim(ix))
                                    .into_any_element(),
                                );
                            }
                        }
                        Leaves::Reads(reads) => items.push(
                            node(
                                format!("r{ix}"),
                                Node::Agent(ix),
                                row(2, div().shadow(vec![ring(ink(&theme, PLAIN_RING))]))
                                    .child(reads_body(&theme, reads).flex_1()),
                            )
                            .opacity(dim(ix))
                            .into_any_element(),
                        ),
                    }
                }
                div()
                    .flex()
                    .flex_col()
                    .gap(px(4.0))
                    .w_full()
                    .min_w_0()
                    .children(items)
            }
        }
    }
}
