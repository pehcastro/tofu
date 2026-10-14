use std::collections::HashMap;
use std::hash::{Hash, Hasher};
use std::sync::Arc;

use gpui::{Context, RenderImage};
use image::ImageError;

use super::{EffectParams, render, to_render_image};

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
}

impl EffectCache {
    pub fn get(&self, key: &EffectKey) -> Option<Lookup> {
        self.slots.get(key).cloned()
    }

    pub fn insert(&mut self, key: EffectKey, slot: Lookup) {
        self.slots
            .retain(|held, _| held.source != key.source || held.params != key.params);
        self.slots.insert(key, slot);
    }

    pub fn image(&mut self, key: EffectKey, cx: &mut Context<Self>) -> Lookup {
        if let Some(found) = self.get(&key) {
            return found;
        }
        self.insert(key.clone(), Lookup::Pending);
        cx.spawn(async move |this, cx| {
            let job = key.clone();
            let rendered = cx
                .background_executor()
                .spawn(async move { render(job.source.bytes, job.width, job.height, &job.params) })
                .await;
            let slot = match rendered {
                Ok(image) => Lookup::Ready(Arc::new(to_render_image(image))),
                Err(error) => Lookup::Failed(Arc::new(error)),
            };
            let told = this.update(cx, |cache, cx| {
                if cache.slots.contains_key(&key) {
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
