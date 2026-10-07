#[path = "../src/book/mod.rs"]
mod book;
#[path = "../src/catalog.rs"]
mod catalog;
#[path = "../src/data.rs"]
mod data;
#[path = "../src/themes.rs"]
mod themes;

mod headless;

use std::borrow::Cow;
use std::cell::RefCell;
use std::collections::HashMap;
use std::env;
use std::path::{Path, PathBuf};
use std::process::ExitCode;
use std::rc::Rc;
use std::sync::{Arc, Mutex, PoisonError};
use std::time::Duration;

use desk_ui::components::form::TextArea;
use desk_ui::metrics::{WINDOW_HEIGHT, WINDOW_WIDTH};
use desk_ui::theme::Mode;
use gpui::{
    App, AtlasKey, AtlasTextureId, AtlasTile, Bounds, DevicePixels, EntityInputHandler, Focusable,
    Font, FontId, FontMetrics, FontRun, GlyphId, Hsla, KeyUpEvent, Keystroke, LineLayout,
    Modifiers, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, Pixels, PlatformAtlas,
    PlatformHeadlessRenderer, PlatformInput, PlatformTextSystem, Point, RenderGlyphParams,
    ScaledPixels, Scene, ScrollDelta, ScrollWheelEvent, SharedString, Size, TextRenderingMode,
    WeakEntity, Window, hsla_to_rgba, point, px, size,
};
use gpui_wgpu::{CosmicTextSystem, WgpuHeadlessRenderer};
use image::RgbaImage;
use serde_json::Value;

use catalog::Page;
use headless::{Session, Setup, platform, save};
use themes::{Choice, DEFAULT_THEME};

pub struct Launch {
    pub page: Page,
    pub theme: usize,
    pub themes: Vec<Choice>,
    pub mode: Option<Mode>,
}

const USAGE: &str = "usage: drive <script.json> <out dir>
script: {\"page\": \"tiling\", \"theme\": \"...\", \"mode\": \"dark|light\", \"size\": [w, h], \"steps\": [...]}
steps: [\"move\", [x,y]] [\"down\", [x,y], \"left|right\"] [\"up\", [x,y], \"left|right\"]
       [\"drag\", [x,y], [x,y], steps] (moves with the held button) [\"key\", \"ctrl-alt-e\"]
       [\"scroll\", [x,y], dy] [\"wait\", ms] [\"shot\", \"name\"]
       [\"ime\", \"mark|commit\", \"text\"] (to the focused text area's input handler)";
const GRID: f32 = 20.0;
const GRID_SAMPLES: u32 = 4;
const QUANT: u8 = 3;
const LEGEND: &str = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
const SPACE_GAP: f32 = 0.2;
const RUN_BREAK: f32 = 1.2;
const ROW_SHARE: f32 = 0.5;
const VISIBLE: f32 = 0.01;

enum Step {
    Move(Point<Pixels>),
    Down(Point<Pixels>, MouseButton),
    Up(Point<Pixels>, MouseButton),
    Drag(Point<Pixels>, Point<Pixels>, usize),
    Key(Keystroke),
    Scroll(Point<Pixels>, f32),
    Wait(Duration),
    Shot(String),
    Ime(Ime, String),
}

#[derive(Clone, Copy, Debug)]
enum Ime {
    Mark,
    Commit,
}

type Areas = Rc<RefCell<Vec<WeakEntity<TextArea>>>>;

struct Script {
    page: Page,
    theme: String,
    mode: Option<Mode>,
    size: Size<Pixels>,
    steps: Vec<Step>,
}

fn number(value: Option<&Value>, what: &str) -> Result<f32, String> {
    value
        .and_then(Value::as_f64)
        .map(|number| number as f32)
        .ok_or_else(|| format!("{what} is not a number"))
}

fn pair(value: Option<&Value>, what: &str) -> Result<(f32, f32), String> {
    let Some([x, y]) = value.and_then(Value::as_array).map(Vec::as_slice) else {
        return Err(format!("{what} is not [x, y]"));
    };
    Ok((number(Some(x), what)?, number(Some(y), what)?))
}

fn at(value: Option<&Value>) -> Result<Point<Pixels>, String> {
    let (x, y) = pair(value, "a point")?;
    Ok(point(px(x), px(y)))
}

fn button(value: Option<&Value>) -> Result<MouseButton, String> {
    match value.and_then(Value::as_str) {
        Some("left") => Ok(MouseButton::Left),
        Some("right") => Ok(MouseButton::Right),
        other => Err(format!("button {other:?} is not left or right")),
    }
}

fn whole(value: Option<&Value>, what: &str) -> Result<u64, String> {
    value
        .and_then(Value::as_u64)
        .ok_or_else(|| format!("{what} is not a whole number"))
}

fn shot_name(value: Option<&Value>) -> Result<String, String> {
    let name = value.and_then(Value::as_str).ok_or("a shot needs a name")?;
    let plain = !name.is_empty()
        && name
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_');
    match plain {
        true => Ok(name.to_owned()),
        false => Err(format!(
            "shot name {name:?} is not letters, digits, - and _"
        )),
    }
}

fn step(value: &Value) -> Result<Step, String> {
    let items = value
        .as_array()
        .ok_or_else(|| format!("step {value} is not an array"))?;
    let verb = items.first().and_then(Value::as_str).unwrap_or_default();
    let arg = |index: usize| items.get(index);
    Ok(match verb {
        "move" => Step::Move(at(arg(1))?),
        "down" => Step::Down(at(arg(1))?, button(arg(2))?),
        "up" => Step::Up(at(arg(1))?, button(arg(2))?),
        "drag" => {
            let count = usize::try_from(whole(arg(3), "drag steps")?)
                .map_err(|error| format!("drag steps: {error}"))?;
            Step::Drag(at(arg(1))?, at(arg(2))?, count.max(1))
        }
        "key" => {
            let source = arg(1)
                .and_then(Value::as_str)
                .ok_or("a key needs a string")?;
            Step::Key(Keystroke::parse(source).map_err(|error| format!("key {source}: {error}"))?)
        }
        "scroll" => Step::Scroll(at(arg(1))?, number(arg(2), "scroll dy")?),
        "wait" => Step::Wait(Duration::from_millis(whole(arg(1), "wait ms")?)),
        "shot" => Step::Shot(shot_name(arg(1))?),
        "ime" => {
            let kind = match arg(1).and_then(Value::as_str) {
                Some("mark") => Ime::Mark,
                Some("commit") => Ime::Commit,
                other => return Err(format!("ime {other:?} is not mark or commit")),
            };
            let text = arg(2)
                .and_then(Value::as_str)
                .ok_or("an ime step needs a string")?;
            Step::Ime(kind, text.to_owned())
        }
        other => return Err(format!("unknown step {other:?}\n{USAGE}")),
    })
}

fn parse_script(text: &str) -> Result<Script, String> {
    let root: Value = serde_json::from_str(text).map_err(|error| format!("script: {error}"))?;
    let field = |name: &str| root.get(name);
    let slug = field("page")
        .and_then(Value::as_str)
        .ok_or("the script names no page")?;
    let page = Page::parse(slug).ok_or_else(|| format!("no page {slug}"))?;
    let mode = field("mode")
        .and_then(Value::as_str)
        .map(themes::parse_mode)
        .transpose()?;
    let (w, h) = match field("size") {
        None => (WINDOW_WIDTH, WINDOW_HEIGHT),
        found => pair(found, "size")?,
    };
    let steps = field("steps")
        .and_then(Value::as_array)
        .ok_or("the script has no steps")?
        .iter()
        .map(step)
        .collect::<Result<_, _>>()?;
    Ok(Script {
        page,
        theme: field("theme")
            .and_then(Value::as_str)
            .unwrap_or(DEFAULT_THEME)
            .to_owned(),
        mode,
        size: size(px(w), px(h)),
        steps,
    })
}

#[derive(Clone, Copy)]
struct Rect {
    x0: f32,
    y0: f32,
    x1: f32,
    y1: f32,
}

impl Rect {
    fn clipped(bounds: Bounds<ScaledPixels>, mask: Bounds<ScaledPixels>) -> Option<Rect> {
        let rect = Rect {
            x0: bounds.origin.x.0.max(mask.origin.x.0),
            y0: bounds.origin.y.0.max(mask.origin.y.0),
            x1: (bounds.origin.x.0 + bounds.size.width.0).min(mask.origin.x.0 + mask.size.width.0),
            y1: (bounds.origin.y.0 + bounds.size.height.0)
                .min(mask.origin.y.0 + mask.size.height.0),
        };
        (rect.x1 > rect.x0 && rect.y1 > rect.y0).then_some(rect)
    }

    fn scaled(self, scale: f32) -> Rect {
        Rect {
            x0: self.x0 / scale,
            y0: self.y0 / scale,
            x1: self.x1 / scale,
            y1: self.y1 / scale,
        }
    }

    fn union(self, other: Rect) -> Rect {
        Rect {
            x0: self.x0.min(other.x0),
            y0: self.y0.min(other.y0),
            x1: self.x1.max(other.x1),
            y1: self.y1.max(other.y1),
        }
    }

    fn middle(self) -> f32 {
        (self.y0 + self.y1) / 2.0
    }

    fn shares_row(self, mark: Rect) -> bool {
        let overlap = self.y1.min(mark.y1) - self.y0.max(mark.y0);
        overlap >= (mark.y1 - mark.y0) * ROW_SHARE
    }
}

impl std::fmt::Display for Rect {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "x {:.1}..{:.1} y {:.1}..{:.1} ({:.1} x {:.1})",
            self.x0,
            self.x1,
            self.y0,
            self.y1,
            self.x1 - self.x0,
            self.y1 - self.y0
        )
    }
}

#[derive(Clone)]
enum Ink {
    Glyph {
        font: FontId,
        glyph: GlyphId,
        size: f32,
    },
    Svg(SharedString),
    Image,
}

type TileKey = (AtlasTextureId, u32);

struct Quad {
    rect: Rect,
    fill: Option<Hsla>,
    border: Option<Hsla>,
    border_width: f32,
}

#[derive(Default)]
struct Ledger {
    glyphs: HashMap<(FontId, GlyphId), String>,
    tiles: HashMap<TileKey, Ink>,
    quads: Vec<Quad>,
    underlines: Vec<(Rect, Hsla, f32)>,
    sprites: Vec<(Rect, TileKey)>,
}

impl Ledger {
    fn record(&mut self, scene: &Scene) {
        self.underlines = scene
            .underlines
            .iter()
            .filter_map(|line| {
                Some((
                    Rect::clipped(line.bounds, line.content_mask.bounds)?,
                    line.color.into(),
                    line.thickness.0,
                ))
            })
            .collect();
        self.quads = scene
            .quads
            .iter()
            .filter_map(|quad| {
                let widths = quad.border_widths;
                let border_width = widths
                    .top
                    .0
                    .max(widths.right.0)
                    .max(widths.bottom.0)
                    .max(widths.left.0);
                Some(Quad {
                    rect: Rect::clipped(quad.bounds, quad.content_mask.bounds)?,
                    fill: quad.background.as_solid(),
                    border: quad.border_color.as_solid().filter(|_| border_width > 0.0),
                    border_width,
                })
            })
            .collect();
        let mono = scene
            .monochrome_sprites
            .iter()
            .map(|s| (s.bounds, s.content_mask.bounds, s.tile));
        let sub = scene
            .subpixel_sprites
            .iter()
            .map(|s| (s.bounds, s.content_mask.bounds, s.tile));
        let poly = scene
            .polychrome_sprites
            .iter()
            .map(|s| (s.bounds, s.content_mask.bounds, s.tile));
        self.sprites = mono
            .chain(sub)
            .chain(poly)
            .filter_map(|(bounds, mask, tile)| {
                Some((
                    Rect::clipped(bounds, mask)?,
                    (tile.texture_id, tile.tile_id.0),
                ))
            })
            .collect();
    }
}

type Shared = Arc<Mutex<Ledger>>;

fn note(ledger: &Shared, write: impl FnOnce(&mut Ledger)) {
    write(&mut ledger.lock().unwrap_or_else(PoisonError::into_inner));
}

struct TextProbe {
    inner: CosmicTextSystem,
    ledger: Shared,
}

impl PlatformTextSystem for TextProbe {
    fn add_fonts(&self, fonts: Vec<Cow<'static, [u8]>>) -> gpui::Result<()> {
        self.inner.add_fonts(fonts)
    }
    fn all_font_names(&self) -> Vec<String> {
        self.inner.all_font_names()
    }
    fn font_id(&self, descriptor: &Font) -> gpui::Result<FontId> {
        self.inner.font_id(descriptor)
    }
    fn prewarm_fonts(&self, font_ids: &[FontId]) {
        self.inner.prewarm_fonts(font_ids);
    }
    fn font_metrics(&self, font_id: FontId) -> FontMetrics {
        self.inner.font_metrics(font_id)
    }
    fn typographic_bounds(&self, font_id: FontId, glyph_id: GlyphId) -> gpui::Result<Bounds<f32>> {
        self.inner.typographic_bounds(font_id, glyph_id)
    }
    fn advance(&self, font_id: FontId, glyph_id: GlyphId) -> gpui::Result<Size<f32>> {
        self.inner.advance(font_id, glyph_id)
    }
    fn glyph_for_char(&self, font_id: FontId, ch: char) -> Option<GlyphId> {
        self.inner.glyph_for_char(font_id, ch)
    }
    fn glyph_raster_bounds(
        &self,
        params: &RenderGlyphParams,
    ) -> gpui::Result<Bounds<DevicePixels>> {
        self.inner.glyph_raster_bounds(params)
    }
    fn rasterize_glyph(
        &self,
        params: &RenderGlyphParams,
        raster_bounds: Bounds<DevicePixels>,
    ) -> gpui::Result<(Size<DevicePixels>, Vec<u8>)> {
        self.inner.rasterize_glyph(params, raster_bounds)
    }
    fn layout_line(&self, text: &str, font_size: Pixels, runs: &[FontRun]) -> LineLayout {
        let layout = self.inner.layout_line(text, font_size, runs);
        let mut glyphs: Vec<(usize, FontId, GlyphId)> = layout
            .runs
            .iter()
            .flat_map(|run| {
                run.glyphs
                    .iter()
                    .map(move |glyph| (glyph.index, run.font_id, glyph.id))
            })
            .collect();
        glyphs.sort_by_key(|(index, _, _)| *index);
        note(&self.ledger, |ledger| {
            for (at, (start, font, glyph)) in glyphs.iter().enumerate() {
                let end = glyphs
                    .iter()
                    .skip(at + 1)
                    .map(|(index, _, _)| *index)
                    .find(|index| index > start)
                    .unwrap_or(text.len());
                if let Some(slice) = text.get(*start..end) {
                    ledger
                        .glyphs
                        .entry((*font, *glyph))
                        .or_insert_with(|| slice.to_owned());
                }
            }
        });
        layout
    }
    fn recommended_rendering_mode(&self, font_id: FontId, font_size: Pixels) -> TextRenderingMode {
        self.inner.recommended_rendering_mode(font_id, font_size)
    }
    fn glyph_dilation_for_color(&self, color: Hsla) -> u8 {
        self.inner.glyph_dilation_for_color(color)
    }
}

struct AtlasProbe {
    inner: Arc<dyn PlatformAtlas>,
    ledger: Shared,
}

impl PlatformAtlas for AtlasProbe {
    fn get_or_insert_with<'a>(
        &self,
        key: &AtlasKey,
        build: &mut dyn FnMut() -> gpui::Result<Option<(Size<DevicePixels>, Cow<'a, [u8]>)>>,
    ) -> gpui::Result<Option<AtlasTile>> {
        let tile = self.inner.get_or_insert_with(key, build)?;
        if let Some(tile) = tile {
            let ink = match key {
                AtlasKey::Glyph(params) => Ink::Glyph {
                    font: params.font_id,
                    glyph: params.glyph_id,
                    size: params.font_size.as_f32(),
                },
                AtlasKey::Svg(params) => Ink::Svg(params.path.clone()),
                AtlasKey::Image(_) => Ink::Image,
            };
            note(&self.ledger, |ledger| {
                ledger.tiles.insert((tile.texture_id, tile.tile_id.0), ink);
            });
        }
        Ok(tile)
    }
    fn remove(&self, key: &AtlasKey) {
        self.inner.remove(key);
    }
    fn contains(&self, key: &AtlasKey) -> bool {
        self.inner.contains(key)
    }
}

struct RenderProbe {
    inner: WgpuHeadlessRenderer,
    atlas: Arc<AtlasProbe>,
    ledger: Shared,
}

impl PlatformHeadlessRenderer for RenderProbe {
    fn render_scene_to_image(
        &mut self,
        scene: &Scene,
        size: Size<DevicePixels>,
    ) -> gpui::Result<RgbaImage> {
        note(&self.ledger, |ledger| ledger.record(scene));
        self.inner.render_scene_to_image(scene, size)
    }
    fn render_scene(&mut self, scene: &Scene, size: Size<DevicePixels>) -> gpui::Result<()> {
        self.inner.render_scene(scene, size)
    }
    fn sprite_atlas(&self) -> Arc<dyn PlatformAtlas> {
        self.atlas.clone()
    }
}

fn hex(color: Hsla) -> String {
    let rgba = hsla_to_rgba(color);
    let byte = |value: f32| (value.clamp(0.0, 1.0) * 255.0).round() as u8;
    format!(
        "#{:02x}{:02x}{:02x}{:02x}",
        byte(rgba.red),
        byte(rgba.green),
        byte(rgba.blue),
        byte(rgba.alpha)
    )
}

struct Mark {
    rect: Rect,
    size: f32,
    text: String,
}

fn texts(ledger: &Ledger, scale: f32) -> Vec<(Rect, String)> {
    let mut marks: Vec<Mark> = ledger
        .sprites
        .iter()
        .filter_map(|(rect, key)| match ledger.tiles.get(key)? {
            Ink::Glyph { font, glyph, size } => Some(Mark {
                rect: rect.scaled(scale),
                size: *size,
                text: ledger
                    .glyphs
                    .get(&(*font, *glyph))
                    .cloned()
                    .unwrap_or_else(|| "?".to_owned()),
            }),
            Ink::Svg(_) | Ink::Image => None,
        })
        .collect();
    marks.sort_by(|a, b| a.rect.middle().total_cmp(&b.rect.middle()));
    let mut rows: Vec<(Rect, f32, Vec<Mark>)> = Vec::new();
    for mark in marks {
        match rows
            .iter_mut()
            .rev()
            .find(|(band, size, _)| *size == mark.size && band.shares_row(mark.rect))
        {
            Some((band, _, members)) => {
                *band = band.union(mark.rect);
                members.push(mark);
            }
            None => rows.push((mark.rect, mark.size, vec![mark])),
        }
    }
    let mut runs = Vec::new();
    for (_, size, mut members) in rows {
        members.sort_by(|a, b| a.rect.x0.total_cmp(&b.rect.x0));
        let mut current: Option<(Rect, String)> = None;
        for mark in members {
            current = Some(match current.take() {
                Some((rect, mut text)) if mark.rect.x0 - rect.x1 <= size * RUN_BREAK => {
                    if mark.rect.x0 - rect.x1 > size * SPACE_GAP {
                        text.push(' ');
                    }
                    text.push_str(mark.text.trim());
                    (rect.union(mark.rect), text)
                }
                finished => {
                    runs.extend(finished);
                    (mark.rect, mark.text.trim().to_owned())
                }
            });
        }
        runs.extend(current);
    }
    runs.sort_by(|a, b| a.0.y0.total_cmp(&b.0.y0).then(a.0.x0.total_cmp(&b.0.x0)));
    runs
}

fn icons(ledger: &Ledger, scale: f32) -> Vec<String> {
    ledger
        .sprites
        .iter()
        .filter_map(|(rect, key)| match ledger.tiles.get(key)? {
            Ink::Svg(path) => Some(format!("icon {path} {}", rect.scaled(scale))),
            Ink::Image => Some(format!("image {}", rect.scaled(scale))),
            Ink::Glyph { .. } => None,
        })
        .collect()
}

fn quads(ledger: &Ledger, scale: f32) -> Vec<String> {
    let seen = |color: &Option<Hsla>| color.is_some_and(|color| color.alpha > VISIBLE);
    ledger
        .quads
        .iter()
        .filter(|quad| seen(&quad.fill) || seen(&quad.border))
        .map(|quad| {
            let fill = quad
                .fill
                .filter(|c| c.alpha > VISIBLE)
                .map_or("none".to_owned(), hex);
            let border = quad
                .border
                .filter(|c| c.alpha > VISIBLE)
                .map_or("none".to_owned(), |color| {
                    format!("{} {:.1}px", hex(color), quad.border_width / scale)
                });
            format!(
                "quad {} fill {fill} border {border}",
                quad.rect.scaled(scale)
            )
        })
        .collect()
}

fn labelled(window: &Window) -> Vec<String> {
    let Some(tree) = window.a11y_tree() else {
        return vec!["no accessibility tree was built".to_owned()];
    };
    tree.nodes
        .iter()
        .filter_map(|(id, node)| {
            let bounds = window.a11y_node_bounds(*id)?;
            let name = [node.author_id(), node.label(), node.value()]
                .into_iter()
                .flatten()
                .collect::<Vec<_>>()
                .join(" | ");
            let rect = Rect {
                x0: bounds.origin.x.as_f32(),
                y0: bounds.origin.y.as_f32(),
                x1: (bounds.origin.x + bounds.size.width).as_f32(),
                y1: (bounds.origin.y + bounds.size.height).as_f32(),
            };
            Some(format!("{:?} {name:?} {rect}", node.role()))
        })
        .collect()
}

fn grid(image: &RgbaImage, scale: f32) -> Vec<String> {
    let cell = (GRID * scale).max(1.0) as u32;
    let mut palette: Vec<([u8; 3], usize)> = Vec::new();
    let mut rows = Vec::new();
    for top in (0..image.height()).step_by(cell as usize) {
        let mut row = String::new();
        for left in (0..image.width()).step_by(cell as usize) {
            let mut counts: HashMap<[u8; 3], usize> = HashMap::new();
            for sy in 0..GRID_SAMPLES {
                for sx in 0..GRID_SAMPLES {
                    let x = left + cell * (2 * sx + 1) / (2 * GRID_SAMPLES);
                    let y = top + cell * (2 * sy + 1) / (2 * GRID_SAMPLES);
                    if let Some(pixel) = image.get_pixel_checked(x, y) {
                        let key = [pixel.0[0], pixel.0[1], pixel.0[2]].map(|c| c >> QUANT << QUANT);
                        *counts.entry(key).or_default() += 1;
                    }
                }
            }
            let dominant = counts
                .into_iter()
                .max_by_key(|(colour, count)| (*count, *colour))
                .map_or([0; 3], |(colour, _)| colour);
            let index = match palette.iter().position(|(colour, _)| *colour == dominant) {
                Some(index) => index,
                None => {
                    palette.push((dominant, 0));
                    palette.len() - 1
                }
            };
            if let Some(entry) = palette.get_mut(index) {
                entry.1 += 1;
            }
            row.push(LEGEND.chars().nth(index).unwrap_or('?'));
        }
        rows.push(format!("{:6.0} |{row}|", top as f32 / scale));
    }
    let legend = palette
        .iter()
        .zip(LEGEND.chars().chain(std::iter::repeat('?')))
        .map(|((colour, cells), mark)| {
            format!(
                "{mark} #{:02x}{:02x}{:02x} {cells} cells",
                colour[0], colour[1], colour[2]
            )
        });
    let mut out = vec![format!(
        "cell {GRID} px, dominant colour of {} samples, channels rounded down to {} steps",
        GRID_SAMPLES * GRID_SAMPLES,
        256 >> QUANT
    )];
    out.extend(legend);
    out.extend(rows);
    out
}

struct Pointer {
    at: Point<Pixels>,
    held: Option<MouseButton>,
}

fn dispatch(session: &mut Session, input: PlatformInput) -> Result<String, String> {
    let result = session.with(|window, cx| window.dispatch_event(input, cx))?;
    session.frame()?;
    Ok(match result.propagate {
        true => "unhandled".to_owned(),
        false => "handled".to_owned(),
    })
}

fn moved(
    session: &mut Session,
    pointer: &mut Pointer,
    at: Point<Pixels>,
) -> Result<String, String> {
    pointer.at = at;
    dispatch(
        session,
        PlatformInput::MouseMove(MouseMoveEvent {
            position: at,
            pressed_button: pointer.held,
            modifiers: Modifiers::none(),
        }),
    )
}

fn compose(
    areas: &Areas,
    kind: Ime,
    text: &str,
    window: &mut Window,
    cx: &mut App,
) -> Result<String, String> {
    let area = areas
        .borrow()
        .iter()
        .filter_map(WeakEntity::upgrade)
        .find(|area| area.read(cx).focus_handle(cx).is_focused(window))
        .ok_or("no text area has focus")?;
    Ok(area.update(cx, |area, cx| {
        match kind {
            Ime::Mark => {
                let caret = text.encode_utf16().count();
                area.replace_and_mark_text_in_range(None, text, Some(caret..caret), window, cx);
            }
            Ime::Commit => area.replace_text_in_range(None, text, window, cx),
        }
        let marked = area.marked_text_range(window, cx);
        format!(
            "ime {kind:?} {text:?}: text {:?} marked utf16 {marked:?}",
            area.text()
        )
    }))
}

struct Run<'a> {
    script: &'a Script,
    out: &'a Path,
    session: Session,
    ledger: Shared,
    pointer: Pointer,
    areas: Areas,
}

impl Run<'_> {
    fn perform(&mut self, index: usize, step: &Step) -> Result<(), String> {
        let session = &mut self.session;
        let pointer = &mut self.pointer;
        let line = match step {
            Step::Move(at) => format!("move {}", moved(session, pointer, *at)?),
            Step::Down(at, button) => {
                moved(session, pointer, *at)?;
                pointer.held = Some(*button);
                let result = dispatch(
                    session,
                    PlatformInput::MouseDown(MouseDownEvent {
                        button: *button,
                        position: *at,
                        modifiers: Modifiers::none(),
                        click_count: 1,
                        first_mouse: false,
                    }),
                )?;
                format!("down {button:?} {result}")
            }
            Step::Up(at, button) => {
                moved(session, pointer, *at)?;
                pointer.held = None;
                let result = dispatch(
                    session,
                    PlatformInput::MouseUp(MouseUpEvent {
                        button: *button,
                        position: *at,
                        modifiers: Modifiers::none(),
                        click_count: 1,
                    }),
                )?;
                format!("up {button:?} {result}")
            }
            Step::Drag(from, to, count) => {
                for at in 0..=*count {
                    let share = at as f32 / *count as f32;
                    let x = from.x.as_f32() + (to.x.as_f32() - from.x.as_f32()) * share;
                    let y = from.y.as_f32() + (to.y.as_f32() - from.y.as_f32()) * share;
                    moved(session, pointer, point(px(x), px(y)))?;
                }
                format!("drag {count} moves holding {:?}", pointer.held)
            }
            Step::Key(keystroke) => {
                let typed =
                    session.with(|window, cx| window.dispatch_keystroke(keystroke.clone(), cx))?;
                session.frame()?;
                let down = match typed {
                    true => "handled",
                    false => "unhandled",
                };
                dispatch(
                    session,
                    PlatformInput::KeyUp(KeyUpEvent {
                        keystroke: keystroke.clone(),
                    }),
                )?;
                format!("key {keystroke} {down}")
            }
            Step::Scroll(at, dy) => {
                moved(session, pointer, *at)?;
                let result = dispatch(
                    session,
                    PlatformInput::ScrollWheel(ScrollWheelEvent {
                        position: *at,
                        delta: ScrollDelta::Pixels(point(px(0.0), px(*dy))),
                        ..Default::default()
                    }),
                )?;
                format!("scroll {dy} {result}")
            }
            Step::Wait(span) => {
                session.advance(*span);
                session.settle(*span)?;
                format!("wait {} ms", span.as_millis())
            }
            Step::Shot(name) => return self.shot(index, name),
            Step::Ime(kind, text) => {
                let areas = &self.areas;
                let line = session.with(|window, cx| compose(areas, *kind, text, window, cx))??;
                session.frame()?;
                line
            }
        };
        println!(
            "step {index}: {line} at {:.0},{:.0}",
            self.pointer.at.x.as_f32(),
            self.pointer.at.y.as_f32()
        );
        Ok(())
    }

    fn shot(&mut self, index: usize, name: &str) -> Result<(), String> {
        let image = self.session.capture()?;
        let png = self.out.join(format!("{name}.png"));
        save(&image, &png)?;
        let scale = self.session.scale;
        let labelled = self.session.with(|window, _| labelled(window))?;
        let ledger = self.ledger.lock().unwrap_or_else(PoisonError::into_inner);
        let mut lines = vec![format!(
            "shot {name} after step {index} | page {} | window {:.0}x{:.0} at scale {scale} | pointer {:.0},{:.0} held {:?}",
            self.script.page.sheet().slug,
            self.script.size.width.as_f32(),
            self.script.size.height.as_f32(),
            self.pointer.at.x.as_f32(),
            self.pointer.at.y.as_f32(),
            self.pointer.held,
        )];
        let mut section = |title: &str, body: Vec<String>| {
            lines.push(format!("\n== {title} ({})", body.len()));
            lines.extend(body);
        };
        section(
            "text runs, rebuilt from painted glyph sprites, logical px",
            texts(&ledger, scale)
                .into_iter()
                .map(|(rect, text)| format!("{text:?} {rect}"))
                .collect(),
        );
        section("icons and images", icons(&ledger, scale));
        section(
            "accessible elements, the only element bounds gpui-ce exposes: role, author id | label | value, bounds",
            labelled,
        );
        section("painted boxes: fill and border", quads(&ledger, scale));
        section(
            "underlines: colour and thickness",
            ledger
                .underlines
                .iter()
                .map(|(rect, color, thickness)| {
                    format!(
                        "underline {} {} {:.1}px",
                        rect.scaled(scale),
                        hex(*color),
                        thickness / scale
                    )
                })
                .collect(),
        );
        section("fill grid from the png", grid(&image, scale));
        drop(ledger);
        let txt = self.out.join(format!("{name}.txt"));
        std::fs::write(&txt, lines.join("\n") + "\n")
            .map_err(|error| format!("{} was not written: {error}", txt.display()))?;
        println!("step {index}: shot wrote {}", png.display());
        println!("step {index}: shot wrote {}", txt.display());
        Ok(())
    }
}

#[expect(
    clippy::arc_with_non_send_sync,
    reason = "PlatformHeadlessRenderer::sprite_atlas returns Arc<dyn PlatformAtlas>, and gpui-ce declares PlatformAtlas without Send or Sync"
)]
fn run(script_path: &Path, out: &Path) -> Result<(), String> {
    let text = std::fs::read_to_string(script_path)
        .map_err(|error| format!("{} was not read: {error}", script_path.display()))?;
    let script = parse_script(&text)?;
    std::fs::create_dir_all(out)
        .map_err(|error| format!("{} was not made: {error}", out.display()))?;
    let ledger: Shared = Arc::default();
    let (text_system, renderer) = platform()?;
    let atlas = Arc::new(AtlasProbe {
        inner: renderer.sprite_atlas(),
        ledger: ledger.clone(),
    });
    let setup = Setup {
        page: script.page,
        theme: &script.theme,
        mode: script.mode,
        size: script.size,
    };
    let areas = Areas::default();
    let seen = areas.clone();
    let mut session = Session::open(
        &setup,
        Arc::new(TextProbe {
            inner: text_system,
            ledger: ledger.clone(),
        }),
        Box::new(RenderProbe {
            inner: renderer,
            atlas,
            ledger: ledger.clone(),
        }),
        move |cx| {
            cx.observe_new(move |_: &mut TextArea, _, cx| {
                seen.borrow_mut().push(cx.weak_entity());
            })
            .detach();
        },
    )?;
    session.with(|window, _| window.set_a11y_forced(true))?;
    session.frame()?;
    println!(
        "headless {:.0}x{:.0} at scale {}, page {}, theme {}",
        script.size.width.as_f32(),
        script.size.height.as_f32(),
        session.scale,
        script.page.sheet().slug,
        script.theme
    );
    let mut run = Run {
        script: &script,
        out,
        session,
        ledger,
        pointer: Pointer {
            at: point(px(0.0), px(0.0)),
            held: None,
        },
        areas,
    };
    for (index, step) in script.steps.iter().enumerate() {
        run.perform(index, step)?;
    }
    Ok(())
}

fn main() -> ExitCode {
    let args: Vec<String> = env::args().skip(1).collect();
    let [script, out] = args.as_slice() else {
        eprintln!("{USAGE}");
        return ExitCode::FAILURE;
    };
    match run(&PathBuf::from(script), &PathBuf::from(out)) {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("{error}");
            ExitCode::FAILURE
        }
    }
}
