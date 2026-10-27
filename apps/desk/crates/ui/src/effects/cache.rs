use std::collections::HashMap;
use std::hash::{Hash, Hasher};
use std::sync::Arc;

use gpui::{Context, RenderImage, Size};
use image::{ImageError, RgbaImage};

use super::{EffectParams, apply, cover, to_render_image};
use crate::components::title_bar::Shade;

const LUMA_WEIGHTS: [u64; 3] = [2126, 7152, 722];
const LUMA_SCALE: u64 = 10_000;
const MID_LUMA: u64 = 128;

#[derive(Clone, Copy, Debug)]
pub struct Source {
    pub name: &'static str,
    pub bytes: &'static [u8],
}

impl PartialEq for Source {
    fn eq(&self, other: &Self) -> bool {
        self.name == other.name
    }
}

impl Eq for Source {}

impl Hash for Source {
    fn hash<H: Hasher>(&self, state: &mut H) {
        self.name.hash(state);
    }
}

#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct EffectKey {
    pub source: Source,
    pub width: u32,
    pub height: u32,
    pub params: EffectParams,
}

#[derive(Clone)]
pub enum Lookup {
    Pending,
    Ready(Arc<RenderImage>),
    Failed(Arc<ImageError>),
}

#[derive(Default)]
pub struct EffectCache {
    slots: HashMap<EffectKey, Lookup>,
    shades: HashMap<EffectKey, Shade>,
    decoded: HashMap<&'static str, Arc<RgbaImage>>,
}

impl EffectCache {
    pub fn get(&self, key: &EffectKey) -> Option<Lookup> {
        self.slots.get(key).cloned()
    }

    pub fn shade(&self, key: &EffectKey) -> Option<Shade> {
        self.shades.get(key).copied()
    }

    pub fn insert(&mut self, key: EffectKey, slot: Lookup) {
        let Self { slots, shades, .. } = self;
        slots.retain(|held, slot| {
            held.source != key.source
                || held.params != key.params
                || matches!(slot, Lookup::Pending)
        });
        shades.retain(|held, _| slots.contains_key(held));
        slots.insert(key, slot);
    }

    pub fn image(
        &mut self,
        key: EffectKey,
        under_captions: Size<u32>,
        cx: &mut Context<Self>,
    ) -> Lookup {
        if let Some(found) = self.get(&key) {
            return found;
        }
        self.insert(key.clone(), Lookup::Pending);
        let decoded = self.decoded.get(key.source.name).cloned();
        cx.spawn(async move |this, cx| {
            let job = key.clone();
            let rendered = cx
                .background_executor()
                .spawn(async move { render(decoded, &job, under_captions) })
                .await;
            let (source, shade, slot) = match rendered {
                Ok((source, image, shade)) => (
                    Some(source),
                    shade,
                    Lookup::Ready(Arc::new(to_render_image(image))),
                ),
                Err(error) => (None, None, Lookup::Failed(Arc::new(error))),
            };
            let told = this.update(cx, |cache, cx| {
                if let Some(source) = source {
                    cache.decoded.insert(key.source.name, source);
                }
                if cache.slots.contains_key(&key) {
                    if let Some(shade) = shade {
                        cache.shades.insert(key.clone(), shade);
                    }
                    cache.insert(key, slot);
                    cx.notify();
                }
            });
            if let Err(error) = told {
                eprintln!("desk: an effect finished after its cache closed: {error}");
            }
        })
        .detach();
        Lookup::Pending
    }
}

fn render(
    decoded: Option<Arc<RgbaImage>>,
    key: &EffectKey,
    under_captions: Size<u32>,
) -> Result<(Arc<RgbaImage>, RgbaImage, Option<Shade>), ImageError> {
    let source = match decoded {
        Some(source) => source,
        None => {
            let source = Arc::new(image::load_from_memory(key.source.bytes)?.into_rgba8());
            eprintln!("desk: intro decoded {}", key.source.name);
            source
        }
    };
    let mut image = cover(&source, key.width, key.height)?;
    apply(&mut image, key.width, key.height, &key.params);
    let shade = top_right_shade(&image, under_captions);
    Ok((source, image, shade))
}

fn top_right_shade(image: &RgbaImage, span: Size<u32>) -> Option<Shade> {
    let (width, height) = image.dimensions();
    let left = width.saturating_sub(span.width);
    let bottom = height.min(span.height);
    let (mut sum, mut count) = (0u64, 0u64);
    for y in 0..bottom {
        for x in left..width {
            let [r, g, b, _] = image.get_pixel(x, y).0;
            let [wr, wg, wb] = LUMA_WEIGHTS;
            sum += u64::from(r) * wr + u64::from(g) * wg + u64::from(b) * wb;
            count += 1;
        }
    }
    let mean = sum.checked_div(count * LUMA_SCALE)?;
    Some(match mean >= MID_LUMA {
        true => Shade::Light,
        false => Shade::Dark,
    })
}
