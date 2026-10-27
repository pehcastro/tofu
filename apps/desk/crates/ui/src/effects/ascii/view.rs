use std::sync::Arc;
use std::time::{Duration, Instant};

use desk_motion::{Ease, reduced_motion};
use gpui::{
    Bounds, ContentMask, Context, DispatchPhase, FontId, GlyphId, IntoElement, MouseButton,
    MouseDownEvent, MouseMoveEvent, Pixels, Render, Rgba, SharedString, TextRun, Window, canvas,
    fill, font, linear_color_stop, linear_gradient, point, prelude::*, px, size,
};

use super::super::Stage;
use super::field::{Cells, Field, Finish, Frame, Metrics, RINGS, Ring, SPOTS, Spot};
use super::{Ascii, Click, Knob, Toggle};

const TRAIL: usize = SPOTS - 1;
const PULSES: usize = RINGS;
const TRAIL_LIFE: f32 = 0.6;
const WAVE_LIFE: f32 = 1.4;
const WAVE_SPEED: f32 = 700.0;
const WAVE_WIDTH: f32 = 70.0;
const BURST_LIFE: f32 = 0.8;
const BURST_SPEED: f32 = 520.0;
const LONGEST_STEP: Duration = Duration::from_millis(100);
const GLYPH_SHARE: f32 = 0.82;
const LIGHT_BASE: Rgba = Rgba::new(0.953, 0.941, 0.918, 1.0);

#[derive(Clone, Copy)]
struct Touch {
    x: f32,
    y: f32,
    at: Instant,
}

#[derive(Clone, Copy)]
struct Pulse {
    x: f32,
    y: f32,
    at: Instant,
    kind: Click,
}

struct Glyphs {
    chars: String,
    size: Pixels,
    ids: Vec<Option<(FontId, GlyphId)>>,
    advance: f32,
    ascent: f32,
    descent: f32,
}

pub struct GradientAscii {
    ascii: Arc<Ascii>,
    finish: Finish,
    font: SharedString,
    motion: bool,
    field: Arc<Field>,
    cells: Cells,
    spare: Cells,
    stepping: bool,
    stale: bool,
    pointer: Option<(f32, f32)>,
    glyphs: Glyphs,
    born: Instant,
    clock: Instant,
    time: f32,
    touches: [Option<Touch>; TRAIL],
    touched: usize,
    pulses: [Option<Pulse>; PULSES],
    pulsed: usize,
    warned: bool,
    timing: Timing,
}

#[derive(Default)]
struct Timing {
    on: bool,
    frames: u32,
    steps: u32,
    step: Duration,
    gradient: Duration,
    glow: Duration,
    glyphs: Duration,
    painted: [usize; 3],
}

impl Timing {
    fn frame(&mut self) {
        if !self.on {
            return;
        }
        self.frames += 1;
        if self.frames < 240 {
            return;
        }
        let per = |d: Duration, n: u32| d.as_secs_f64() * 1000.0 / f64::from(n.max(1));
        eprintln!(
            "ascii-timing step {:.3} ms ({} steps) gradient {:.3} ms glow {:.3} ms glyphs {:.3} ms per frame, painted gradient {} glow {} glyphs {}",
            per(self.step, self.steps),
            self.steps,
            per(self.gradient, self.frames),
            per(self.glow, self.frames),
            per(self.glyphs, self.frames),
            self.painted[0] / self.frames as usize,
            self.painted[1] / self.frames as usize,
            self.painted[2] / self.frames as usize,
        );
        *self = Timing {
            on: true,
            ..Timing::default()
        };
    }
}

impl GradientAscii {
    pub fn new(ascii: Ascii, after: &[Stage], font: SharedString, motion: bool) -> GradientAscii {
        let mut field = Field::new();
        field.tune(&ascii);
        let now = Instant::now();
        GradientAscii {
            ascii: Arc::new(ascii),
            finish: Finish::of(after),
            font,
            motion,
            field: Arc::new(field),
            cells: Cells::default(),
            spare: Cells::default(),
            stepping: false,
            stale: true,
            pointer: None,
            glyphs: Glyphs {
                chars: String::new(),
                size: px(0.0),
                ids: Vec::new(),
                advance: 0.0,
                ascent: 0.0,
                descent: 0.0,
            },
            born: now,
            clock: now,
            time: 0.0,
            touches: [None; TRAIL],
            touched: 0,
            pulses: [None; PULSES],
            pulsed: 0,
            warned: false,
            timing: Timing {
                on: std::env::var_os("TOFU_DESK_FRAME_CHECK").is_some(),
                ..Timing::default()
            },
        }
    }

    pub fn underneath(&self) -> bool {
        self.ascii.toggle(Toggle::Underneath)
    }

    pub fn set(&mut self, ascii: Ascii, after: &[Stage], motion: bool, cx: &mut Context<Self>) {
        let finish = Finish::of(after);
        if *self.ascii == ascii && self.motion == motion && self.finish == finish {
            return;
        }
        self.finish = finish;
        if self.ascii.knob(Knob::Intro) != ascii.knob(Knob::Intro) {
            self.born = Instant::now();
        }
        Arc::make_mut(&mut self.field).tune(&ascii);
        self.ascii = Arc::new(ascii);
        self.motion = motion;
        self.stale = true;
        cx.notify();
    }

    fn hover(&mut self, at: Option<(f32, f32)>) {
        self.pointer = at.map(|(x, y)| self.field.source(x, y));
        if let Some((x, y)) = self.pointer {
            self.touch(x, y);
        }
    }

    fn touch(&mut self, x: f32, y: f32) {
        self.touches[self.touched % TRAIL] = Some(Touch {
            x,
            y,
            at: Instant::now(),
        });
        self.touched = self.touched.wrapping_add(1);
    }

    fn pulse(&mut self, x: f32, y: f32) {
        if self.ascii.click == Click::Off {
            return;
        }
        let (x, y) = self.field.source(x, y);
        self.pulses[self.pulsed % PULSES] = Some(Pulse {
            x,
            y,
            at: Instant::now(),
            kind: self.ascii.click,
        });
        self.pulsed = self.pulsed.wrapping_add(1);
    }

    fn shape_glyphs(&mut self, size: Pixels, window: &mut Window) {
        let chars = self.ascii.characters();
        if self.glyphs.chars == chars && self.glyphs.size == size {
            return;
        }
        let run = TextRun {
            len: chars.len(),
            font: font(self.font.clone()),
            ..TextRun::default()
        };
        let layout = window.text_system().layout_line(chars, size, &[run], None);
        let glyphs = &mut self.glyphs;
        glyphs.chars.clear();
        glyphs.chars.push_str(chars);
        glyphs.size = size;
        glyphs.ids.clear();
        glyphs.ids.extend(chars.char_indices().map(|(byte, ch)| {
            if ch.is_whitespace() {
                return None;
            }
            layout.runs.iter().find_map(|run| {
                run.glyphs
                    .iter()
                    .find(|glyph| glyph.index == byte)
                    .map(|glyph| (run.font_id, glyph.id))
            })
        }));
        let count = chars.chars().count().max(1);
        glyphs.advance = f32::from(layout.width) / count as f32;
        glyphs.ascent = f32::from(layout.ascent);
        glyphs.descent = f32::from(layout.descent);
    }

    fn paint(&mut self, bounds: Bounds<Pixels>, window: &mut Window, cx: &mut Context<Self>) {
        let now = Instant::now();
        let moving = self.motion && !reduced_motion(cx);
        let step = now.duration_since(self.clock).min(LONGEST_STEP);
        self.clock = now;
        if moving {
            self.time += step.as_secs_f32();
        }
        let (width, height) = (f32::from(bounds.size.width), f32::from(bounds.size.height));
        let cell = self.ascii.knob(Knob::Cell);
        if width <= 0.0 || height <= 0.0 || cell <= 0.0 {
            return;
        }
        if !self.field.fits(cell, width, height, self.finish) {
            Arc::make_mut(&mut self.field).fit(cell, width, height, self.finish);
            self.stale = true;
        }
        self.shape_glyphs(px(cell * GLYPH_SHARE), window);
        let intro = self.ascii.knob(Knob::Intro);
        let reveal = match moving && intro > 0.0 {
            true => Ease::Out.apply((now.duration_since(self.born).as_secs_f32() / intro).min(1.0)),
            false => 1.0,
        };
        let mut spots = [Spot::default(); SPOTS];
        let mut lit = 0;
        let mouse = moving && self.ascii.toggle(Toggle::Mouse);
        if let Some((x, y)) = self.pointer.filter(|_| mouse) {
            spots[0] = Spot {
                x,
                y,
                strength: 1.0,
            };
            lit = 1;
        }
        for touch in self.touches.iter().flatten().filter(|_| mouse) {
            let age = now.duration_since(touch.at).as_secs_f32();
            if age < TRAIL_LIFE {
                spots[lit] = Spot {
                    x: touch.x,
                    y: touch.y,
                    strength: 1.0 - age / TRAIL_LIFE,
                };
                lit += 1;
            }
        }
        let mut rings = [Ring::default(); PULSES];
        let mut waves = 0;
        for pulse in self.pulses.iter().flatten().filter(|_| moving) {
            let age = now.duration_since(pulse.at).as_secs_f32();
            let ring = match pulse.kind {
                Click::Wave if age < WAVE_LIFE => Ring {
                    x: pulse.x,
                    y: pulse.y,
                    radius: age * WAVE_SPEED,
                    width: WAVE_WIDTH,
                    strength: 1.0 - age / WAVE_LIFE,
                },
                Click::Burst if age < BURST_LIFE => {
                    let radius = age * BURST_SPEED;
                    Ring {
                        x: pulse.x,
                        y: pulse.y,
                        radius: 0.0,
                        width: radius.max(1.0),
                        strength: 1.0 - age / BURST_LIFE,
                    }
                }
                Click::Wave | Click::Burst | Click::Off => continue,
            };
            rings[waves] = ring;
            waves += 1;
        }
        if moving || self.stale {
            self.step(
                Frame {
                    time: self.time,
                    reveal,
                    glyphs: self.glyphs.ids.len(),
                    metrics: Metrics {
                        lift: (self.glyphs.ascent - self.glyphs.descent.abs()) / 2.0,
                        inset: self.glyphs.advance / 2.0,
                    },
                    spots,
                    lit,
                    rings,
                    waves,
                },
                cx,
            );
        }
        window.with_content_mask(
            Some(ContentMask {
                bounds,
                ..ContentMask::default()
            }),
            |window| window.paint_layer(bounds, |window| self.draw(bounds, window)),
        );
        if moving {
            self.listen(bounds, mouse, cx, window);
            window.request_animation_frame();
        }
    }

    fn step(&mut self, frame: Frame, cx: &mut Context<Self>) {
        if self.stepping {
            return;
        }
        self.stepping = true;
        self.stale = false;
        let (field, ascii) = (self.field.clone(), self.ascii.clone());
        let mut cells = std::mem::take(&mut self.spare);
        cx.spawn(async move |this, cx| {
            let stepped = cx
                .background_executor()
                .spawn(async move {
                    let started = Instant::now();
                    field.step(&ascii, &frame, &mut cells);
                    (field, cells, started.elapsed())
                })
                .await;
            if let Err(error) = this.update(cx, |view, cx| view.landed(stepped, cx)) {
                eprintln!("desk: gradient ascii closed while its field stepped: {error}");
            }
        })
        .detach();
    }

    fn landed(
        &mut self,
        (field, cells, took): (Arc<Field>, Cells, Duration),
        cx: &mut Context<Self>,
    ) {
        self.stepping = false;
        self.timing.step += took;
        self.timing.steps += 1;
        match Arc::ptr_eq(&field, &self.field) {
            true => self.spare = std::mem::replace(&mut self.cells, cells),
            false => {
                self.spare = cells;
                self.stale = true;
            }
        }
        cx.notify();
    }

    fn draw(&mut self, bounds: Bounds<Pixels>, window: &mut Window) {
        let ascii = &self.ascii;
        if ascii.toggle(Toggle::Light) && !ascii.toggle(Toggle::Underneath) {
            window.paint_quad(fill(bounds, LIGHT_BASE));
        }
        let cells = &self.cells;
        let origin = bounds.origin;
        let mut clock = Instant::now();
        let lap = |clock: &mut Instant| {
            let now = Instant::now();
            let took = now.duration_since(*clock);
            *clock = now;
            took
        };
        for wash in &cells.washes {
            window.paint_quad(fill(
                Bounds::new(
                    origin + point(px(wash.at[0]), px(wash.at[1])),
                    size(px(wash.size[0]), px(wash.size[1])),
                ),
                linear_gradient(
                    180.0,
                    linear_color_stop(wash.top, wash.top_from),
                    linear_color_stop(wash.bottom, 1.0),
                ),
            ));
        }
        self.timing.gradient += lap(&mut clock);
        for halo in &cells.halos {
            let centre = point(px(halo.centre[0]), px(halo.centre[1]));
            let half = halo.side / 2.0;
            window.paint_quad(
                fill(
                    Bounds::new(
                        origin + centre - point(px(half), px(half)),
                        size(px(halo.side), px(halo.side)),
                    ),
                    halo.color,
                )
                .corner_radii(px(half)),
            );
        }
        self.timing.glow += lap(&mut clock);
        let glyphs = &self.glyphs;
        let mut failed = None;
        for ink in &cells.inks {
            let Some(Some((font_id, glyph_id))) = glyphs.ids.get(ink.glyph) else {
                continue;
            };
            let baseline = origin + point(px(ink.at[0]), px(ink.at[1]));
            if let Err(error) =
                window.paint_glyph(baseline, *font_id, *glyph_id, glyphs.size, ink.color)
            {
                failed = Some(error);
            }
        }
        self.timing.glyphs += lap(&mut clock);
        self.timing.painted[0] += cells.washes.len();
        self.timing.painted[1] += cells.halos.len();
        self.timing.painted[2] += cells.inks.len();
        self.timing.frame();
        if let Some(error) = failed.filter(|_| !self.warned) {
            self.warned = true;
            eprintln!("desk: gradient ascii could not paint a glyph: {error}");
        }
    }

    fn listen(
        &self,
        bounds: Bounds<Pixels>,
        mouse: bool,
        cx: &mut Context<Self>,
        window: &mut Window,
    ) {
        let origin = bounds.origin;
        if mouse {
            let moved = cx.entity().downgrade();
            window.on_mouse_event(move |event: &MouseMoveEvent, phase, _, cx| {
                if phase != DispatchPhase::Capture {
                    return;
                }
                let at = event.position - origin;
                let inside = bounds
                    .contains(&event.position)
                    .then(|| (f32::from(at.x), f32::from(at.y)));
                if let Err(error) = moved.update(cx, |ascii, _| ascii.hover(inside)) {
                    eprintln!("desk: gradient ascii closed under the mouse: {error}");
                }
            });
        }
        let clicked = cx.entity().downgrade();
        window.on_mouse_event(move |event: &MouseDownEvent, phase, _, cx| {
            if phase != DispatchPhase::Capture
                || event.button != MouseButton::Left
                || !bounds.contains(&event.position)
            {
                return;
            }
            let at = event.position - origin;
            if let Err(error) =
                clicked.update(cx, |ascii, _| ascii.pulse(f32::from(at.x), f32::from(at.y)))
            {
                eprintln!("desk: gradient ascii closed under a click: {error}");
            }
        });
    }
}

impl Render for GradientAscii {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let this = cx.entity();
        canvas(
            |_, _, _| {},
            move |bounds, (), window, cx| {
                this.update(cx, |ascii, cx| ascii.paint(bounds, window, cx));
            },
        )
        .absolute()
        .inset_0()
    }
}
