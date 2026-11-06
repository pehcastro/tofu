use std::sync::Arc;
use std::time::{Duration, Instant};

use desk_ui::components::title_bar::{Shade, captions_span};
use desk_ui::effects::ascii::{Ascii, GradientAscii};
use desk_ui::effects::{EffectCache, EffectKey, EffectParams, Lookup, Source, Stage};
use desk_ui::glass::frosted;
use desk_ui::theme::Theme;
use gpui::{
    App, AppContext, Context, DevicePixels, Div, Entity, Global, IntoElement, ObjectFit, Render,
    RenderImage, Rgba, Size, StyledImage, Task, Window, canvas, div, img, linear_color_stop,
    linear_gradient, prelude::*, px,
};

use super::kept::{self, Fit};
use super::paint::{MONO, SANS, SURFACE, hex, white};
use super::remembered::Effect;
use super::tuning::Tuning;

const SETTLE: Duration = Duration::from_millis(150);
const DOWNWARD: f32 = 180.0;

pub const STREET: Source = Source {
    name: "street.jpg",
    bytes: include_bytes!("../../../../../assets/intro/street.jpg"),
};
pub const ROOM: Source = Source {
    name: "room.jpg",
    bytes: include_bytes!("../../../../../assets/intro/room.jpg"),
};
pub const DUSK: Source = Source {
    name: "dusk.jpg",
    bytes: include_bytes!("../../../../../assets/intro/dusk.jpg"),
};
pub const BUILT_IN: [Source; 3] = [STREET, ROOM, DUSK];

pub fn remembered_params(effects: &[Effect], tuning: &Tuning) -> EffectParams {
    EffectParams {
        stages: effects
            .iter()
            .filter_map(|effect| tuning.stage(*effect))
            .collect(),
    }
}

pub fn after_ascii(effects: &[Effect], tuning: &Tuning) -> Vec<Stage> {
    let after = effects
        .iter()
        .position(|effect| *effect == Effect::Ascii)
        .and_then(|at| effects.get(at + 1..))
        .unwrap_or_default();
    remembered_params(after, tuning).stages
}
pub const WELCOME: Source = Source {
    name: "welcome",
    bytes: include_bytes!("../../../../../assets/intro/welcome.jpg"),
};

pub struct Backdrop {
    source: Source,
    params: EffectParams,
    cache: Entity<EffectCache>,
    size: Size<DevicePixels>,
    under_captions: Size<u32>,
    shade: Shade,
    asked: Option<(EffectKey, Instant)>,
    shown: Option<Arc<RenderImage>>,
    settling: Option<Task<()>>,
    corner: f32,
    ascii: Option<Entity<GradientAscii>>,
    loading: Option<(String, Task<()>)>,
}

struct Shared(Entity<EffectCache>);

impl Global for Shared {}

fn shared(cx: &mut App) -> Entity<EffectCache> {
    if let Some(shared) = cx.try_global::<Shared>() {
        return shared.0.clone();
    }
    let cache = cx.new(|_| EffectCache::default());
    cx.set_global(Shared(cache.clone()));
    cache
}

pub fn backdrop(source: Source, params: EffectParams, cx: &mut App) -> Entity<Backdrop> {
    let cache = shared(cx);
    cx.new(|cx| {
        cx.observe(&cache, |backdrop: &mut Backdrop, _, cx| {
            backdrop.arrived(cx)
        })
        .detach();
        Backdrop {
            source,
            params,
            cache,
            size: Size::default(),
            under_captions: Size::default(),
            shade: Shade::Dark,
            asked: None,
            shown: None,
            settling: None,
            corner: 0.0,
            ascii: None,
            loading: None,
        }
    })
}

impl Backdrop {
    pub fn shade(&self) -> Shade {
        self.shade
    }

    pub fn round(&mut self, corner: f32) {
        self.corner = corner;
    }

    pub fn ascii(
        &mut self,
        ascii: Option<Ascii>,
        after: &[Stage],
        motion: bool,
        cx: &mut Context<Self>,
    ) {
        cx.notify();
        let Some(ascii) = ascii else {
            self.ascii = None;
            return;
        };
        match &self.ascii {
            Some(view) => view.update(cx, |view, cx| view.set(ascii, after, motion, cx)),
            None => {
                self.ascii = Some(cx.new(|_| GradientAscii::new(ascii, after, MONO.into(), motion)))
            }
        }
    }

    pub fn show_named(
        &mut self,
        name: &str,
        params: EffectParams,
        fit: Fit,
        cx: &mut Context<Self>,
    ) {
        if let Some(source) = kept::known(name, cx) {
            return self.set(source, params, cx);
        }
        self.params = params;
        if self
            .loading
            .as_ref()
            .is_some_and(|(asked, _)| asked == name)
        {
            return;
        }
        let asked = name.to_owned();
        let task = cx.spawn(async move |this, cx| {
            let job = asked.clone();
            let read = cx
                .background_executor()
                .spawn(async move { kept::read(&job, fit) })
                .await;
            let landed = this.update(cx, |backdrop, cx| {
                backdrop.loading = None;
                backdrop.source = match read {
                    Ok(source) => {
                        kept::remember(&asked, source, cx);
                        source
                    }
                    Err(error) => {
                        eprintln!("desk: intro image falls back to street.jpg: {error}");
                        STREET
                    }
                };
                backdrop.ask(cx);
            });
            if let Err(error) = landed {
                eprintln!("desk: intro backdrop closed before {asked} was read: {error}");
            }
        });
        self.loading = Some((name.to_owned(), task));
    }

    pub fn set(&mut self, source: Source, params: EffectParams, cx: &mut Context<Self>) {
        let was_loading = self.loading.take().is_some();
        if !was_loading && self.source == source && self.params == params {
            return;
        }
        self.source = source;
        self.params = params;
        self.ask(cx);
    }

    fn resize(&mut self, size: Size<DevicePixels>, scale: f32, cx: &mut Context<Self>) {
        self.size = size;
        self.under_captions = captions_span(scale);
        if self.shown.is_none() {
            self.ask(cx);
            return;
        }
        self.settling = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(SETTLE).await;
            if let Err(error) = this.update(cx, |backdrop, cx| backdrop.ask(cx)) {
                eprintln!("desk: intro effects: the backdrop closed while settling: {error}");
            }
        }));
    }

    fn ask(&mut self, cx: &mut Context<Self>) {
        let (width, height) = (u32::from(self.size.width), u32::from(self.size.height));
        if width == 0 || height == 0 || self.loading.is_some() {
            return;
        }
        let key = EffectKey {
            source: self.source,
            width,
            height,
            params: self.params.clone(),
        };
        let under_captions = self.under_captions;
        match self
            .cache
            .update(cx, |cache, cx| cache.image(key.clone(), under_captions, cx))
        {
            Lookup::Pending => self.asked = Some((key, Instant::now())),
            found => {
                self.asked = None;
                self.show(&key, found, cx);
            }
        }
    }

    fn arrived(&mut self, cx: &mut Context<Self>) {
        let Some((key, at)) = self.asked.take() else {
            return;
        };
        let found = self.cache.read(cx).get(&key);
        match found {
            None | Some(Lookup::Pending) => self.asked = Some((key, at)),
            Some(found) => {
                let ms = at.elapsed().as_secs_f64() * 1000.0;
                eprintln!(
                    "desk: intro effects {}x{} in {ms:.1} ms",
                    key.width, key.height
                );
                self.show(&key, found, cx);
            }
        }
    }

    fn show(&mut self, key: &EffectKey, found: Lookup, cx: &mut Context<Self>) {
        match found {
            Lookup::Pending => {}
            Lookup::Failed(error) => eprintln!("desk: intro effects {}: {error}", self.source.name),
            Lookup::Ready(image) => {
                if let Some(shade) = self.cache.read(cx).shade(key) {
                    self.shade = shade;
                }
                match self.shown.replace(image.clone()) {
                    Some(old) if !Arc::ptr_eq(&old, &image) => cx.drop_image(old, None),
                    _ => {}
                }
                cx.notify();
            }
        }
    }
}

impl Render for Backdrop {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let (this, held, scale, corner) = (
            cx.entity(),
            self.size,
            window.scale_factor(),
            px(self.corner),
        );
        let underneath = self
            .ascii
            .as_ref()
            .is_none_or(|ascii| ascii.read(cx).underneath());
        div()
            .absolute()
            .inset_0()
            .children(self.shown.clone().filter(|_| underneath).map(|image| {
                img(image)
                    .absolute()
                    .inset_0()
                    .size_full()
                    .rounded(corner)
                    .object_fit(ObjectFit::Cover)
            }))
            .child(
                canvas(
                    |_, _, _| {},
                    move |bounds, _, _, cx| {
                        let size = bounds.size.to_device_pixels(scale);
                        if size != held {
                            this.update(cx, |backdrop, cx| backdrop.resize(size, scale, cx));
                        }
                    },
                )
                .absolute()
                .inset_0(),
            )
            .children(self.ascii.clone())
    }
}

pub fn hero(backdrop: &Entity<Backdrop>) -> Div {
    div()
        .flex_1()
        .min_h_0()
        .w_full()
        .relative()
        .overflow_hidden()
        .bg(hex(SURFACE))
        .font_family(SANS)
        .text_size(px(14.0))
        .line_height(px(23.0))
        .text_color(white(0.9))
        .child(backdrop.clone())
}

pub fn glass(radius: f32, blur: f32, scrim: u32, top: f32, bottom: f32) -> Div {
    scrim_over(radius, hex(scrim), top, bottom)
        .when(blur > 0.0, |glass| glass.backdrop_blur(px(blur)))
}

pub fn glass_over(radius: f32, theme: &Theme, fill: Rgba, top: f32, bottom: f32) -> Div {
    frosted(scrim_over(radius, fill, top, bottom), theme)
}

fn scrim_over(radius: f32, fill: Rgba, top: f32, bottom: f32) -> Div {
    let faded = |share: f32| Rgba {
        alpha: fill.alpha * share,
        ..fill
    };
    div()
        .absolute()
        .inset_0()
        .rounded(px(radius))
        .bg(linear_gradient(
            DOWNWARD,
            linear_color_stop(faded(top), 0.0),
            linear_color_stop(faded(bottom), 1.0),
        ))
}
