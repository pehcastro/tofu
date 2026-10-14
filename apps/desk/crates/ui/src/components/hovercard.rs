use std::collections::HashMap;
use std::hash::{DefaultHasher, Hash, Hasher};
use std::panic::Location;
use std::time::{Duration, Instant};

use desk_motion::tokens::{PANEL_IN_MS, PANEL_OUT_MS};
use desk_motion::{Phase, Presence, reduced_motion};
use gpui::{
    AnyElement, App, AppContext, AvailableSpace, Bounds, Context, DispatchPhase, Element,
    ElementId, Entity, EntityId, FontWeight, Global, GlobalElementId, InspectorElementId, LayoutId,
    MouseMoveEvent, ObjectFit, Pixels, Point, Position, SharedString, Style, Task, Window,
    WindowId, deferred, div, img, point, prelude::*, px, size,
};

use crate::components::linkmeta::{self, Lookup};
use crate::components::overlay::popover;
use crate::components::paint::ink;
use crate::components::size::{FONT_SMALL, T1, T2, T3};
use crate::live::ActiveTheme;

const OPEN_AFTER: Duration = Duration::from_millis(400);
const EDGE: f32 = 8.0;
const GAP: f32 = 6.0;
const IMAGE_HEIGHT: f32 = 120.0;
const DESCRIPTION_LINES: usize = 3;

#[derive(Clone)]
struct Target {
    key: u64,
    url: SharedString,
    line: Bounds<Pixels>,
}

impl Target {
    fn same(&self, other: &Target) -> bool {
        self.key == other.key && self.url == other.url
    }
}

struct Card {
    hovered: Option<Target>,
    shown: Option<Target>,
    presence: Presence,
    since: Instant,
    painted: bool,
    placed: Option<Bounds<Pixels>>,
    hosts: Vec<EntityId>,
    timer: Option<Task<()>>,
}

impl Card {
    fn aim(&mut self, target: Target, cx: &mut Context<Self>) {
        if self
            .hovered
            .as_ref()
            .is_some_and(|hovered| hovered.same(&target))
        {
            return;
        }
        self.hovered = Some(target.clone());
        self.since = Instant::now();
        self.painted = false;
        let card = cx.entity().downgrade();
        linkmeta::fetch(
            &target.url,
            move |cx| {
                if let Err(error) = card.update(cx, |card, cx| card.notify_hosts(cx)) {
                    eprintln!("desk: linkmeta answered a closed card: {error}");
                }
            },
            cx,
        );
        self.timer = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(OPEN_AFTER).await;
            if let Err(error) = this.update(cx, |card, cx| card.open(cx)) {
                eprintln!("desk: hovercard opened after its window closed: {error}");
            }
        }));
    }

    fn left_link(&mut self) {
        self.hovered = None;
        self.timer = None;
    }

    fn open(&mut self, cx: &mut Context<Self>) {
        self.shown = self.hovered.clone();
        self.placed = None;
        self.presence.set_open(true, Instant::now());
        self.notify_hosts(cx);
    }

    fn dismiss(&mut self, cx: &mut Context<Self>) {
        self.left_link();
        self.presence.set_open(false, Instant::now());
        self.notify_hosts(cx);
    }

    fn travel(&mut self, at: Point<Pixels>, cx: &mut Context<Self>) {
        let open = matches!(
            self.presence.phase(Instant::now()),
            Phase::Opening | Phase::Open
        );
        let over = |line: Option<&Target>| line.is_some_and(|target| target.line.contains(&at));
        let on_card = self
            .placed
            .is_some_and(|placed| placed.dilate(px(GAP)).contains(&at));
        if open && !on_card && !over(self.shown.as_ref()) && !over(self.hovered.as_ref()) {
            self.dismiss(cx);
        }
    }

    fn notify_hosts(&self, cx: &mut Context<Self>) {
        for host in &self.hosts {
            AppContext::notify(cx, *host);
        }
    }

    fn painted(&mut self, url: &SharedString, settled: Instant) {
        if self.painted || self.shown.as_ref().map(|shown| &shown.url) != Some(url) {
            return;
        }
        self.painted = true;
        let source = match settled <= self.since {
            true => "cached",
            false => "fetched",
        };
        eprintln!(
            "desk: linkmeta {url} painted after {} ms ({source})",
            self.since.elapsed().as_millis()
        );
    }
}

#[derive(Default)]
struct Cards(HashMap<WindowId, Entity<Card>>);

impl Global for Cards {}

fn card(window: &Window, cx: &mut App) -> Entity<Card> {
    let id = window.window_handle().window_id();
    if let Some(found) = cx.default_global::<Cards>().0.get(&id).cloned() {
        return found;
    }
    let made = cx.new(|_| Card {
        hovered: None,
        shown: None,
        presence: Presence::new(PANEL_IN_MS, PANEL_OUT_MS),
        since: Instant::now(),
        painted: false,
        placed: None,
        hosts: Vec::new(),
        timer: None,
    });
    cx.default_global::<Cards>().0.insert(id, made.clone());
    made
}

pub fn hover(
    key: u64,
    link: Option<(SharedString, Bounds<Pixels>)>,
    window: &Window,
    cx: &mut App,
) {
    card(window, cx).update(cx, |card, cx| match link {
        Some((url, line)) => card.aim(Target { key, url, line }, cx),
        None => card.left_link(),
    });
}

#[derive(IntoElement)]
pub struct LinkCard {
    key: u64,
}

pub fn link_card(key: u64) -> LinkCard {
    LinkCard { key }
}

impl RenderOnce for LinkCard {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let card = card(window, cx);
        let view = window.current_view();
        let now = Instant::now();
        let reduced = reduced_motion(cx);
        let shown = card.update(cx, |card, _| {
            if !card.hosts.contains(&view) {
                card.hosts.push(view);
            }
            let phase = card.presence.phase(now);
            let alpha = match reduced {
                true => f32::from(u8::from(phase != Phase::Closing)),
                false => card.presence.progress(now),
            };
            card.shown
                .clone()
                .filter(|shown| shown.key == self.key && phase != Phase::Closed && alpha > 0.0)
                .map(|shown| (shown, alpha, phase))
        });
        let Some((target, alpha, phase)) = shown else {
            return div().into_any_element();
        };
        if matches!(phase, Phase::Opening | Phase::Closing) && !reduced {
            window.request_animation_frame();
        }
        let theme = ActiveTheme::theme(cx);
        let (lookup, settled) = linkmeta::cached(&target.url, cx).unwrap_or((Lookup::Pending, now));
        let small = |text: SharedString, tone: f32| {
            div()
                .text_size(px(FONT_SMALL))
                .text_color(ink(&theme, tone))
                .truncate()
                .child(text)
        };
        let host: SharedString = linkmeta::host(&target.url).to_owned().into();
        let mut body = popover(&theme).id("link-card").opacity(alpha);
        if let Lookup::Found(meta) = &lookup {
            body = body
                .children(meta.image.clone().map(|image| {
                    img(image)
                        .w_full()
                        .h(px(IMAGE_HEIGHT))
                        .object_fit(ObjectFit::Cover)
                }))
                .children(meta.title.clone().map(|title| {
                    div()
                        .font_weight(FontWeight::SEMIBOLD)
                        .text_color(ink(&theme, T1))
                        .child(title)
                }))
                .children(meta.description.clone().map(|description| {
                    div()
                        .text_size(px(FONT_SMALL))
                        .text_color(ink(&theme, T2))
                        .line_clamp(DESCRIPTION_LINES)
                        .child(description)
                }));
        }
        body = body.child(small(host, T3));
        if !matches!(lookup, Lookup::Found(_)) {
            body = body.child(small(target.url.clone(), T3));
        }
        let settled = (!matches!(lookup, Lookup::Pending)).then_some(settled);
        deferred(Placed {
            card,
            url: target.url,
            line: target.line,
            settled,
            panel: body.into_any_element(),
        })
        .priority(1)
        .into_any_element()
    }
}

struct Placed {
    card: Entity<Card>,
    url: SharedString,
    line: Bounds<Pixels>,
    settled: Option<Instant>,
    panel: AnyElement,
}

impl IntoElement for Placed {
    type Element = Self;

    fn into_element(self) -> Self {
        self
    }
}

impl Element for Placed {
    type RequestLayoutState = ();
    type PrepaintState = ();

    fn id(&self) -> Option<ElementId> {
        None
    }

    fn source_location(&self) -> Option<&'static Location<'static>> {
        None
    }

    fn request_layout(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (LayoutId, ()) {
        let style = Style {
            position: Position::Absolute,
            ..Style::default()
        };
        (window.request_layout(style, [], cx), ())
    }

    fn prepaint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) {
        let room = size(AvailableSpace::MaxContent, AvailableSpace::MaxContent);
        let measured = self.panel.layout_as_root(room, window, cx);
        let viewport = window.viewport_size();
        let (gap, edge) = (px(GAP), px(EDGE));
        let below = self.line.bottom() + gap;
        let above = self.line.top() - gap - measured.height;
        let room_below = viewport.height - edge - self.line.bottom();
        let room_above = self.line.top() - edge;
        let y = match below + measured.height <= viewport.height - edge
            || (above < edge && room_below >= room_above)
        {
            true => below,
            false => above,
        };
        let widest = (viewport.width - measured.width - edge).max(edge);
        let x = self.line.left().clamp(edge, widest);
        let origin = point(x.round(), y.round());
        self.panel.prepaint_at(origin, window, cx);
        self.card.update(cx, |card, _| {
            card.placed = Some(Bounds::new(origin, measured));
        });
    }

    fn paint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) {
        self.panel.paint(window, cx);
        if let Some(settled) = self.settled {
            self.card
                .update(cx, |card, _| card.painted(&self.url, settled));
        }
        let card = self.card.clone();
        window.on_mouse_event(move |event: &MouseMoveEvent, phase, _, cx| {
            if phase == DispatchPhase::Bubble {
                card.update(cx, |card, cx| card.travel(event.position, cx));
            }
        });
    }
}

pub fn link_key(text: &str, piece: usize) -> u64 {
    let mut hasher = DefaultHasher::new();
    (text, piece).hash(&mut hasher);
    hasher.finish()
}
