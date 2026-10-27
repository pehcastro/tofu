use std::cell::Cell;
use std::fs;
use std::path::Path;
use std::rc::Rc;
use std::sync::Arc;
use std::time::Duration;

use gpui::{
    Action, App, Asset, Bounds, ClickEvent, ClipboardItem, Context, Corners, CursorStyle,
    DevicePixels, Entity, Image, ImageAssetLoader, ImageCacheError, ImageFormat, MouseButton,
    MouseDownEvent, MouseMoveEvent, MouseUpEvent, ParsedSvg, Pixels, Point, Render, RenderImage,
    Resource, SMOOTH_SVG_SCALE_FACTOR, ScrollWheelEvent, SharedString, Size, Subscription,
    SvgRenderer, SvgSize, Task, WeakEntity, Window, canvas, div, point, prelude::*, px, size,
};
use image::imageops::{FilterType, resize};
use image::{Frame, ImageBuffer, ImageReader, Rgba};

use crate::component::control;
use crate::components::card::{HEADER_CONTROL, header_action, header_button};
use crate::components::empty::empty_state;
use crate::components::glyph::Glyph;
use crate::components::paint::{glyph, ink};
use crate::components::size::{FONT_SMALL, HEADER, RADIUS_CHIP_SMALL, T2};
use crate::icon::Icon;
use crate::live::ActiveTheme;
use crate::metrics::ICON_SMALL;
use crate::motion::reduced_motion;
use crate::theme::ColorToken;

const MEDIA_EXTENSIONS: [&str; 8] = ["png", "jpg", "jpeg", "gif", "webp", "bmp", "ico", "svg"];
const FOOTER_PAD: f32 = 10.0;
const FACT_GAP: f32 = 14.0;
const CONTROL_GAP: f32 = 2.0;
const FIT_PAD: f32 = 16.0;
const ZOOM_STEP: f32 = 1.25;
const ZOOM_LEAST: f32 = 0.02;
const ZOOM_MOST: f32 = 32.0;
const WHEEL_LINE: f32 = 20.0;
const WHEEL_PIXELS: f32 = 240.0;
const LEVEL_LEAST: u32 = 1024;
const CHECK_CELL: u32 = 8;
const CHECK_TILE: u32 = 256;
const CHECK_DARK: [u8; 4] = [128, 128, 128, 16];
const CHECK_LIGHT: [u8; 4] = [128, 128, 128, 40];
const FASTEST_FRAME: Duration = Duration::from_millis(20);
const SLOW_FRAME: Duration = Duration::from_millis(100);
const KIB: f64 = 1024.0;
const RASTER_MOST: f32 = 4096.0;
const BUCKETS_PER_OCTAVE: f32 = 4.0;
const STILL: &str = "animated, still frame";
const ANIMATIONS: [&[u8]; 3] = [b"<animate", b"<set ", b"@keyframes"];

#[derive(Clone, Debug, PartialEq, Action)]
#[action(namespace = media, no_json)]
pub struct OpenMedia {
    pub path: Arc<Path>,
}

pub fn shows(path: &Path) -> bool {
    path.extension()
        .and_then(|extension| extension.to_str())
        .is_some_and(|extension| {
            MEDIA_EXTENSIONS
                .iter()
                .any(|known| extension.eq_ignore_ascii_case(known))
        })
}

#[derive(Clone, Copy, PartialEq)]
enum Zoom {
    Fit,
    Scale(f32),
}

enum Read {
    Reading,
    Ready(Decoded),
    Failed(SharedString),
}

struct Decoded {
    levels: Vec<Arc<RenderImage>>,
    checker: Arc<RenderImage>,
    pixels: Size<f32>,
    facts: Vec<SharedString>,
    copied: Option<ImageFormat>,
    vector: Option<Vector>,
}

struct Vector {
    parsed: Arc<ParsedSvg>,
    renderer: SvgRenderer,
    raster: f32,
}

struct Sharp {
    view: WeakEntity<MediaView>,
    raster: f32,
    pending: Option<f32>,
}

pub struct MediaView {
    path: Arc<Path>,
    read: Read,
    zoom: Zoom,
    pan: Point<Pixels>,
    grab: Option<Point<Pixels>>,
    stage: Rc<Cell<Bounds<Pixels>>>,
    frame: usize,
    painted: Rc<Cell<bool>>,
    ticking: Option<Task<()>>,
    copying: Option<Task<()>>,
    sharpening: Option<(f32, Task<()>)>,
    _reading: Task<()>,
}

impl MediaView {
    pub fn new(path: Arc<Path>, cx: &mut Context<Self>) -> Self {
        let read = path.clone();
        let decoding = match svg(&path) {
            true => {
                let renderer = cx.svg_renderer();
                cx.background_executor()
                    .spawn(async move { traced(&read, renderer) })
            }
            false => {
                let decode = ImageAssetLoader::load(Resource::Path(path.clone()), cx);
                cx.background_executor()
                    .spawn(async move { decoded(&read, decode.await) })
            }
        };
        let shown = path.clone();
        let reading = cx.spawn(async move |view, cx| {
            let read = decoding.await;
            if let Err(error) = view.update(cx, |view, cx| view.landed(read, cx)) {
                eprintln!(
                    "desk: media view closed before {} decoded: {error}",
                    shown.display()
                );
            }
        });
        cx.on_release(|view, cx| view.release(cx)).detach();
        Self {
            path,
            read: Read::Reading,
            zoom: Zoom::Fit,
            pan: Point::default(),
            grab: None,
            stage: Rc::default(),
            frame: 0,
            painted: Rc::default(),
            ticking: None,
            copying: None,
            sharpening: None,
            _reading: reading,
        }
    }

    pub fn path(&self) -> &Arc<Path> {
        &self.path
    }

    fn shown_zoom(&self) -> Option<Zoom> {
        match self.read {
            Read::Ready(_) => Some(self.zoom),
            Read::Reading | Read::Failed(_) => None,
        }
    }

    fn landed(&mut self, read: Result<Decoded, String>, cx: &mut Context<Self>) {
        let shown = self.path.display();
        self.read = match read {
            Ok(decoded) => {
                eprintln!(
                    "desk: media {shown} decoded: {}, {} levels",
                    decoded.facts.join(", "),
                    decoded.levels.len()
                );
                Read::Ready(decoded)
            }
            Err(why) => {
                eprintln!("desk: media {shown} not shown: {why}");
                Read::Failed(why.into())
            }
        };
        cx.notify();
    }

    fn release(&mut self, cx: &mut App) {
        if let Read::Ready(decoded) = std::mem::replace(&mut self.read, Read::Reading) {
            for image in decoded.levels.into_iter().chain([decoded.checker]) {
                cx.drop_image(image, None);
            }
        }
    }

    fn tick(&mut self, cx: &mut Context<Self>) {
        let Read::Ready(decoded) = &self.read else {
            return;
        };
        let Some(full) = decoded.levels.first() else {
            return;
        };
        let frames = full.frame_count();
        if frames < 2 || self.ticking.is_some() || reduced_motion(cx) {
            return;
        }
        let delay = Duration::from(full.delay(self.frame));
        let delay = if delay < FASTEST_FRAME {
            SLOW_FRAME
        } else {
            delay
        };
        let painted = self.painted.clone();
        self.ticking = Some(cx.spawn(async move |view, cx| {
            cx.background_executor().timer(delay).await;
            let stepped = view.update(cx, |view, cx| {
                view.ticking = None;
                if painted.replace(false) {
                    view.frame = (view.frame + 1) % frames;
                    cx.notify();
                }
            });
            if let Err(error) = stepped {
                eprintln!("desk: media frame stepped after its view closed: {error}");
            }
        }));
    }

    fn sharpen(&mut self, wanted: f32, cx: &mut Context<Self>) {
        let Read::Ready(decoded) = &self.read else {
            return;
        };
        let Some(vector) = &decoded.vector else {
            return;
        };
        let pending = self.sharpening.as_ref().map(|(raster, _)| *raster);
        if wanted == vector.raster || pending == Some(wanted) {
            return;
        }
        let (parsed, renderer) = (vector.parsed.clone(), vector.renderer.clone());
        let wide = DevicePixels((decoded.pixels.width * wanted).round().max(1.0) as i32);
        let rastered = cx.background_executor().spawn(async move {
            renderer
                .render_parsed(&parsed, SvgSize::Size(size(wide, wide)))
                .map(leveled)
                .map_err(|error| error.to_string())
        });
        let shown = self.path.clone();
        let task = cx.spawn(async move |view, cx| {
            let rastered = rastered.await;
            let landed = view.update(cx, |view, cx| view.sharpened(wanted, rastered, cx));
            if let Err(error) = landed {
                eprintln!(
                    "desk: media view closed before {} rastered: {error}",
                    shown.display()
                );
            }
        });
        self.sharpening = Some((wanted, task));
    }

    fn sharpened(
        &mut self,
        raster: f32,
        rastered: Result<Vec<Arc<RenderImage>>, String>,
        cx: &mut Context<Self>,
    ) {
        self.sharpening = None;
        let shown = self.path.display();
        let Read::Ready(decoded) = &mut self.read else {
            return;
        };
        let Some(vector) = &mut decoded.vector else {
            return;
        };
        let levels = match rastered {
            Ok(levels) => levels,
            Err(why) => return eprintln!("desk: media {shown} not rastered at {raster}x: {why}"),
        };
        eprintln!(
            "desk: media {shown} rastered at {raster}x, {} levels",
            levels.len()
        );
        vector.raster = raster;
        for old in std::mem::replace(&mut decoded.levels, levels) {
            cx.drop_image(old, None);
        }
        cx.notify();
    }

    fn scale_now(&self, window: &Window) -> Option<f32> {
        let Read::Ready(decoded) = &self.read else {
            return None;
        };
        let stage = self.stage.get().size;
        Some(scale(
            self.zoom,
            stage,
            decoded.pixels,
            decoded.fit_most(),
            window.scale_factor(),
        ))
    }

    fn zoom_at(
        &mut self,
        to: f32,
        anchor: Option<Point<Pixels>>,
        window: &Window,
        cx: &mut Context<Self>,
    ) {
        let (Read::Ready(decoded), Some(before)) = (&self.read, self.scale_now(window)) else {
            return;
        };
        let (stage, ws) = (self.stage.get(), window.scale_factor());
        let after = to.clamp(ZOOM_LEAST, ZOOM_MOST);
        let centre = stage.center();
        let anchor = anchor.unwrap_or(centre);
        let pan = held(self.pan, stage.size, drawn(decoded.pixels, before, ws));
        let ratio = after / before;
        let next = point(
            anchor.x - centre.x - (anchor.x - centre.x - pan.x) * ratio,
            anchor.y - centre.y - (anchor.y - centre.y - pan.y) * ratio,
        );
        self.pan = held(next, stage.size, drawn(decoded.pixels, after, ws));
        self.zoom = Zoom::Scale(after);
        cx.notify();
    }

    fn step(&mut self, factor: f32, window: &Window, cx: &mut Context<Self>) {
        if let Some(now) = self.scale_now(window) {
            self.zoom_at(now * factor, None, window, cx);
        }
    }

    fn toggle(&mut self, anchor: Option<Point<Pixels>>, window: &Window, cx: &mut Context<Self>) {
        match self.zoom {
            Zoom::Fit => self.zoom_at(1.0, anchor, window, cx),
            Zoom::Scale(_) => {
                self.zoom = Zoom::Fit;
                self.pan = Point::default();
                cx.notify();
            }
        }
    }

    fn pressed(&mut self, event: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        if event.click_count == 2 {
            return self.toggle(Some(event.position), window, cx);
        }
        self.grab = Some(event.position);
        cx.notify();
    }

    fn moved(&mut self, event: &MouseMoveEvent, window: &mut Window, cx: &mut Context<Self>) {
        let Some(from) = self.grab else {
            return;
        };
        if event.pressed_button != Some(MouseButton::Left) {
            self.grab = None;
            return cx.notify();
        }
        let (Read::Ready(decoded), Some(now)) = (&self.read, self.scale_now(window)) else {
            return;
        };
        let stage = self.stage.get().size;
        let shown = drawn(decoded.pixels, now, window.scale_factor());
        let pan = held(self.pan, stage, shown) + (event.position - from);
        self.grab = Some(event.position);
        let pan = held(pan, stage, shown);
        if pan != self.pan {
            self.pan = pan;
            cx.notify();
        }
    }

    fn released(&mut self, _: &MouseUpEvent, _: &mut Window, cx: &mut Context<Self>) {
        if self.grab.take().is_some() {
            cx.notify();
        }
    }

    fn wheeled(&mut self, event: &ScrollWheelEvent, window: &mut Window, cx: &mut Context<Self>) {
        let Some(now) = self.scale_now(window) else {
            return;
        };
        let delta = event.delta.pixel_delta(px(WHEEL_LINE)).y.as_f32();
        if delta == 0.0 {
            return;
        }
        let factor = (delta / WHEEL_PIXELS).exp();
        self.zoom_at(now * factor, Some(event.position), window, cx);
    }

    fn copy(&mut self, cx: &mut Context<Self>) {
        let Read::Ready(decoded) = &self.read else {
            return;
        };
        let Some(format) = decoded.copied else {
            return eprintln!(
                "desk: media {} has no format the clipboard takes",
                self.path.display()
            );
        };
        let path = self.path.clone();
        let read = path.clone();
        let bytes = cx
            .background_executor()
            .spawn(async move { fs::read(&read) });
        self.copying = Some(cx.spawn(async move |_, cx| match bytes.await {
            Ok(bytes) => {
                let item = ClipboardItem::new_image(&Image::from_bytes(format, bytes));
                cx.update(|cx| cx.write_to_clipboard(item));
                eprintln!("desk: media {} copied", path.display());
            }
            Err(error) => eprintln!("desk: media {} not copied: {error}", path.display()),
        }));
    }
}

impl Render for MediaView {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.tick(cx);
        let theme = ActiveTheme::theme(cx);
        let decoded = match &self.read {
            Read::Reading => return div().size_full().into_any_element(),
            Read::Failed(why) => {
                return empty_state(
                    "media-failed",
                    "Could not show the file",
                    Some(why.clone()),
                    &[],
                    &[],
                    &theme,
                    |_, _, _| {},
                )
                .size_full()
                .into_any_element();
            }
            Read::Ready(decoded) => decoded,
        };
        let ws = window.scale_factor();
        let stage_size = self.stage.get().size;
        let now = scale(
            self.zoom,
            stage_size,
            decoded.pixels,
            decoded.fit_most(),
            ws,
        );
        let shown = drawn(decoded.pixels, now, ws);
        let pannable = shown.width > stage_size.width || shown.height > stage_size.height;
        let cursor = match (self.grab, pannable) {
            (Some(_), true) => CursorStyle::ClosedHand,
            (None, true) => CursorStyle::OpenHand,
            (_, false) => CursorStyle::Arrow,
        };
        let painting = Painting {
            levels: decoded.levels.clone(),
            checker: decoded.checker.clone(),
            pixels: decoded.pixels,
            most: decoded.fit_most(),
            zoom: self.zoom,
            pan: self.pan,
            frame: self.frame,
        };
        let sharp = decoded.vector.as_ref().map(|vector| Sharp {
            view: cx.weak_entity(),
            raster: vector.raster,
            pending: self.sharpening.as_ref().map(|(raster, _)| *raster),
        });
        let (stage, painted, pixels) = (self.stage.clone(), self.painted.clone(), decoded.pixels);
        let surface = canvas(
            move |bounds, _, _| stage.set(bounds),
            move |bounds, (), window, cx| {
                painted.set(true);
                let now = painting.paint(bounds, window);
                let Some(Sharp {
                    view,
                    raster,
                    pending,
                }) = sharp
                else {
                    return;
                };
                let wanted = bucket(now, pixels);
                if wanted == raster || pending == Some(wanted) {
                    return;
                }
                cx.defer(move |cx| {
                    if let Err(error) = view.update(cx, |view, cx| view.sharpen(wanted, cx)) {
                        eprintln!("desk: media view closed before it sharpened: {error}");
                    }
                });
            },
        )
        .absolute()
        .size_full();
        let footer = div()
            .flex()
            .flex_none()
            .items_center()
            .gap(px(FACT_GAP))
            .h(px(HEADER))
            .px(px(FOOTER_PAD))
            .border_t_1()
            .border_color(theme.color(ColorToken::Separator))
            .text_size(px(FONT_SMALL))
            .text_color(ink(&theme, T2))
            .children(decoded.facts.iter().cloned());
        div()
            .size_full()
            .flex()
            .flex_col()
            .child(
                div()
                    .relative()
                    .flex_1()
                    .min_h_0()
                    .overflow_hidden()
                    .cursor(cursor)
                    .on_mouse_down(MouseButton::Left, cx.listener(Self::pressed))
                    .on_mouse_move(cx.listener(Self::moved))
                    .on_mouse_up(MouseButton::Left, cx.listener(Self::released))
                    .on_mouse_up_out(MouseButton::Left, cx.listener(Self::released))
                    .on_scroll_wheel(cx.listener(Self::wheeled))
                    .child(surface),
            )
            .child(footer)
            .into_any_element()
    }
}

pub struct MediaControls {
    media: Entity<MediaView>,
    zoom: Option<Zoom>,
    _watch: Subscription,
}

impl MediaControls {
    pub fn new(media: Entity<MediaView>, cx: &mut Context<Self>) -> Self {
        let watch = cx.observe(&media, |controls, media, cx| {
            let zoom = media.read(cx).shown_zoom();
            if zoom != controls.zoom {
                controls.zoom = zoom;
                cx.notify();
            }
        });
        Self {
            zoom: media.read(cx).shown_zoom(),
            media,
            _watch: watch,
        }
    }
}

impl Render for MediaControls {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let Some(zoom) = self.zoom else {
            return div().into_any_element();
        };
        let theme = ActiveTheme::theme(cx);
        let label: SharedString = match zoom {
            Zoom::Fit => "Fit".into(),
            Zoom::Scale(scale) => format!("{:.0}%", scale * 100.0).into(),
        };
        let media = self.media.clone();
        let on = move |change: fn(&mut MediaView, &mut Window, &mut Context<MediaView>)| {
            let media = media.clone();
            move |_: &ClickEvent, window: &mut Window, cx: &mut App| {
                media.update(cx, |view, cx| change(view, window, cx));
            }
        };
        let copy = control("media-copy", "Copy image", &theme)
            .size(px(HEADER_CONTROL))
            .rounded(px(RADIUS_CHIP_SMALL))
            .child(glyph(
                Glyph::Copy,
                ICON_SMALL,
                theme.color(ColorToken::TextIcon),
            ))
            .on_click(on(|view, _, cx| view.copy(cx)));
        let out = header_action("media-zoom-out", Icon::Minimize, "Zoom out", &theme).on_click(on(
            |view, window, cx| view.step(1.0 / ZOOM_STEP, window, cx),
        ));
        let fit = header_button("media-fit", label, &theme)
            .on_click(on(|view, window, cx| view.toggle(None, window, cx)));
        let into = header_action("media-zoom-in", Icon::Plus, "Zoom in", &theme)
            .on_click(on(|view, window, cx| view.step(ZOOM_STEP, window, cx)));
        div()
            .flex()
            .items_center()
            .gap(px(CONTROL_GAP))
            .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .child(copy)
            .child(out)
            .child(fit)
            .child(into)
            .into_any_element()
    }
}

struct Painting {
    levels: Vec<Arc<RenderImage>>,
    checker: Arc<RenderImage>,
    pixels: Size<f32>,
    most: f32,
    zoom: Zoom,
    pan: Point<Pixels>,
    frame: usize,
}

impl Painting {
    fn paint(self, stage: Bounds<Pixels>, window: &mut Window) -> f32 {
        let ws = window.scale_factor();
        let now = scale(self.zoom, stage.size, self.pixels, self.most, ws);
        let shown = drawn(self.pixels, now, ws);
        let pan = held(self.pan, stage.size, shown);
        let half = point(shown.width / 2.0, shown.height / 2.0);
        let image = Bounds::new(stage.center() - half + pan, shown);
        let visible = stage.intersect(&image);
        if visible.size.width <= px(0.0) || visible.size.height <= px(0.0) {
            return now;
        }
        let tile = CHECK_TILE as f32 / ws;
        let snap = |at: Pixels| (at.as_f32() * ws).round() / ws;
        let (left, top) = (snap(stage.origin.x), snap(stage.origin.y));
        let first = |from: f32, edge: Pixels| from + tile * ((edge.as_f32() - from) / tile).floor();
        let mut y = first(top, visible.top());
        while y < visible.bottom().as_f32() {
            let mut x = first(left, visible.left());
            while x < visible.right().as_f32() {
                let cell = Bounds::new(point(px(x), px(y)), size(px(tile), px(tile)));
                if let Err(error) = window.paint_image(
                    visible,
                    cell,
                    Corners::default(),
                    self.checker.clone(),
                    0,
                    false,
                ) {
                    eprintln!("desk: media checker not painted: {error}");
                    return now;
                }
                x += tile;
            }
            y += tile;
        }
        let wanted = self.pixels.width * now;
        let Some(level) = self
            .levels
            .iter()
            .rev()
            .find(|level| level.size(0).width.0 as f32 >= wanted)
            .or(self.levels.first())
        else {
            return now;
        };
        let frame = self.frame.min(level.frame_count().saturating_sub(1));
        if let Err(error) = window.paint_image(
            stage,
            image,
            Corners::default(),
            level.clone(),
            frame,
            false,
        ) {
            eprintln!("desk: media image not painted: {error}");
        }
        now
    }
}

impl Decoded {
    fn fit_most(&self) -> f32 {
        match self.vector {
            Some(_) => ZOOM_MOST,
            None => 1.0,
        }
    }
}

fn scale(zoom: Zoom, stage: Size<Pixels>, pixels: Size<f32>, most: f32, ws: f32) -> f32 {
    match zoom {
        Zoom::Scale(scale) => scale,
        Zoom::Fit => {
            let room =
                |side: Pixels, of: f32| (side.as_f32() - 2.0 * FIT_PAD).max(1.0) * ws / of.max(1.0);
            room(stage.width, pixels.width)
                .min(room(stage.height, pixels.height))
                .min(most)
        }
    }
}

fn bucket(now: f32, pixels: Size<f32>) -> f32 {
    let most = RASTER_MOST / pixels.width.max(pixels.height).max(1.0);
    ((now.log2() * BUCKETS_PER_OCTAVE).ceil() / BUCKETS_PER_OCTAVE)
        .exp2()
        .min(most)
}

fn drawn(pixels: Size<f32>, scale: f32, ws: f32) -> Size<Pixels> {
    size(
        px(pixels.width * scale / ws),
        px(pixels.height * scale / ws),
    )
}

fn held(pan: Point<Pixels>, stage: Size<Pixels>, shown: Size<Pixels>) -> Point<Pixels> {
    let room = |shown: Pixels, stage: Pixels| ((shown - stage).as_f32() / 2.0).max(0.0);
    let (x, y) = (
        room(shown.width, stage.width),
        room(shown.height, stage.height),
    );
    point(
        px(pan.x.as_f32().clamp(-x, x)),
        px(pan.y.as_f32().clamp(-y, y)),
    )
}

fn decoded(
    path: &Path,
    image: Result<Arc<RenderImage>, ImageCacheError>,
) -> Result<Decoded, String> {
    let image = image.map_err(|error| error.to_string())?;
    let bytes = fs::metadata(path)
        .map_err(|error| format!("{}: {error}", path.display()))?
        .len();
    let coded = ImageReader::open(path)
        .and_then(ImageReader::with_guessed_format)
        .map_err(|error| format!("{}: {error}", path.display()))?
        .format();
    let dense = image.size(0);
    let smooth = match coded {
        Some(_) => 1.0,
        None => SMOOTH_SVG_SCALE_FACTOR,
    };
    let pixels = size(
        dense.width.0 as f32 / smooth,
        dense.height.0 as f32 / smooth,
    );
    let (named, copied) = match coded {
        Some(coded) => named(coded),
        None => ("SVG".into(), Some(ImageFormat::Svg)),
    };
    let mut facts: Vec<SharedString> = vec![
        format!("{} × {}", pixels.width.round(), pixels.height.round()).into(),
        weight(bytes).into(),
        named,
    ];
    if image.frame_count() > 1 {
        facts.push(format!("{} frames", image.frame_count()).into());
    }
    Ok(Decoded {
        levels: leveled(image),
        checker: Arc::new(checker()),
        pixels,
        facts,
        copied,
        vector: None,
    })
}

fn traced(path: &Path, renderer: SvgRenderer) -> Result<Decoded, String> {
    let failed = |error: &dyn std::fmt::Display| format!("{}: {error}", path.display());
    let bytes = fs::read(path).map_err(|error| failed(&error))?;
    let parsed = renderer.parse_svg(&bytes).map_err(|error| failed(&error))?;
    let image = renderer
        .render_parsed(&parsed, 1.0)
        .map_err(|error| failed(&error))?;
    let dense = image.size(0);
    let pixels = size(
        dense.width.0 as f32 / SMOOTH_SVG_SCALE_FACTOR,
        dense.height.0 as f32 / SMOOTH_SVG_SCALE_FACTOR,
    );
    let mut facts: Vec<SharedString> = vec![
        format!("{} × {}", pixels.width.round(), pixels.height.round()).into(),
        weight(bytes.len() as u64).into(),
        "SVG".into(),
    ];
    if ANIMATIONS
        .iter()
        .any(|mark| bytes.windows(mark.len()).any(|window| window == *mark))
    {
        facts.push(STILL.into());
    }
    Ok(Decoded {
        levels: leveled(image),
        checker: Arc::new(checker()),
        pixels,
        facts,
        copied: Some(ImageFormat::Svg),
        vector: Some(Vector {
            parsed: Arc::new(parsed),
            renderer,
            raster: SMOOTH_SVG_SCALE_FACTOR,
        }),
    })
}

pub fn svg(path: &Path) -> bool {
    path.extension()
        .is_some_and(|extension| extension.eq_ignore_ascii_case("svg"))
}

fn leveled(image: Arc<RenderImage>) -> Vec<Arc<RenderImage>> {
    let mut levels = vec![image];
    while let Some(half) = levels.last().and_then(|last| halved(last)) {
        levels.push(Arc::new(half));
    }
    levels
}

fn named(coded: image::ImageFormat) -> (SharedString, Option<ImageFormat>) {
    match coded {
        image::ImageFormat::Png => ("PNG".into(), Some(ImageFormat::Png)),
        image::ImageFormat::Jpeg => ("JPEG".into(), Some(ImageFormat::Jpeg)),
        image::ImageFormat::Gif => ("GIF".into(), Some(ImageFormat::Gif)),
        image::ImageFormat::WebP => ("WebP".into(), Some(ImageFormat::Webp)),
        image::ImageFormat::Bmp => ("BMP".into(), Some(ImageFormat::Bmp)),
        image::ImageFormat::Ico => ("ICO".into(), Some(ImageFormat::Ico)),
        image::ImageFormat::Tiff => ("TIFF".into(), Some(ImageFormat::Tiff)),
        other => (
            other
                .extensions_str()
                .first()
                .map_or_else(|| "image".to_owned(), |name| name.to_ascii_uppercase())
                .into(),
            None,
        ),
    }
}

pub fn weight(bytes: u64) -> String {
    let counted = bytes as f64;
    if counted < KIB {
        format!("{bytes} B")
    } else if counted < KIB * KIB {
        format!("{:.1} KB", counted / KIB)
    } else {
        format!("{:.1} MB", counted / (KIB * KIB))
    }
}

fn halved(image: &RenderImage) -> Option<RenderImage> {
    if image.frame_count() != 1 {
        return None;
    }
    let dense = image.size(0);
    let width = u32::try_from(dense.width.0).ok()?;
    let height = u32::try_from(dense.height.0).ok()?;
    if width.max(height) <= LEVEL_LEAST {
        return None;
    }
    let source = ImageBuffer::<Rgba<u8>, &[u8]>::from_raw(width, height, image.as_bytes(0)?)?;
    let half = resize(
        &source,
        (width / 2).max(1),
        (height / 2).max(1),
        FilterType::Triangle,
    );
    Some(RenderImage::new(vec![Frame::new(half)]))
}

fn checker() -> RenderImage {
    let tile = ImageBuffer::from_fn(CHECK_TILE, CHECK_TILE, |x, y| {
        match (x / CHECK_CELL + y / CHECK_CELL) % 2 {
            0 => Rgba(CHECK_DARK),
            _ => Rgba(CHECK_LIGHT),
        }
    });
    RenderImage::new(vec![Frame::new(tile)])
}
