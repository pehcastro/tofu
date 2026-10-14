use std::sync::Arc;
use std::time::{Duration, Instant};

use desk_ui::effects::{EffectCache, EffectKey, EffectParams, Lookup, Source};
use gpui::{
    App, AppContext, Context, DevicePixels, Div, Entity, IntoElement, ObjectFit, Render,
    RenderImage, Size, StyledImage, Task, Window, canvas, div, img, linear_color_stop,
    linear_gradient, prelude::*, px,
};

use super::paint::{SANS, SURFACE, hex, tint, white};

const SETTLE: Duration = Duration::from_millis(150);
const GLASS_BLUR: f32 = 32.0;
const DOWNWARD: f32 = 180.0;

pub const STREET: Source = Source {
    name: "street",
    bytes: include_bytes!("../../../../../assets/intro/street.jpg"),
};
pub const ROOM: Source = Source {
    name: "room",
    bytes: include_bytes!("../../../../../assets/intro/room.jpg"),
};
pub const DUSK: Source = Source {
    name: "dusk",
    bytes: include_bytes!("../../../../../assets/intro/dusk.jpg"),
};
pub const WELCOME: Source = Source {
    name: "welcome",
    bytes: include_bytes!("../../../../../assets/intro/welcome.jpg"),
};

pub struct Backdrop {
    source: Source,
    params: EffectParams,
    cache: Entity<EffectCache>,
    size: Size<DevicePixels>,
    asked: Option<(EffectKey, Instant)>,
    shown: Option<Arc<RenderImage>>,
    settling: Option<Task<()>>,
}

pub fn backdrop(source: Source, params: EffectParams, cx: &mut App) -> Entity<Backdrop> {
    cx.new(|cx| {
        let cache = cx.new(|_| EffectCache::default());
        cx.observe(&cache, |backdrop: &mut Backdrop, _, cx| {
            backdrop.arrived(cx)
        })
        .detach();
        Backdrop {
            source,
            params,
            cache,
            size: Size::default(),
            asked: None,
            shown: None,
            settling: None,
        }
    })
}

impl Backdrop {
    fn resize(&mut self, size: Size<DevicePixels>, cx: &mut Context<Self>) {
        self.size = size;
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
        if width == 0 || height == 0 {
            return;
        }
        let key = EffectKey {
            source: self.source,
            width,
            height,
            params: self.params.clone(),
        };
        match self
            .cache
            .update(cx, |cache, cx| cache.image(key.clone(), cx))
        {
            Lookup::Pending => self.asked = Some((key, Instant::now())),
            found => {
                self.asked = None;
                self.show(found, cx);
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
                self.show(found, cx);
            }
        }
    }

    fn show(&mut self, found: Lookup, cx: &mut Context<Self>) {
        match found {
            Lookup::Pending => {}
            Lookup::Failed(error) => eprintln!("desk: intro effects {}: {error}", self.source.name),
            Lookup::Ready(image) => {
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
        let (this, held, scale) = (cx.entity(), self.size, window.scale_factor());
        div()
            .absolute()
            .inset_0()
            .children(self.shown.clone().map(|image| {
                img(image)
                    .absolute()
                    .inset_0()
                    .size_full()
                    .object_fit(ObjectFit::Cover)
            }))
            .child(
                canvas(
                    |_, _, _| {},
                    move |bounds, _, _, cx| {
                        let size = bounds.size.to_device_pixels(scale);
                        if size != held {
                            this.update(cx, |backdrop, cx| backdrop.resize(size, cx));
                        }
                    },
                )
                .absolute()
                .inset_0(),
            )
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

pub fn glass(radius: f32, scrim: u32, top: f32, bottom: f32) -> Div {
    div()
        .absolute()
        .inset_0()
        .rounded(px(radius))
        .backdrop_blur(px(GLASS_BLUR))
        .bg(linear_gradient(
            DOWNWARD,
            linear_color_stop(tint(scrim, top), 0.0),
            linear_color_stop(tint(scrim, bottom), 1.0),
        ))
}
