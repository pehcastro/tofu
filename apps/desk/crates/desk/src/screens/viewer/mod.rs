use std::path::{Component, Path};
use std::sync::Arc;

use crate::screens::work::split::timed;
use desk_ui::components::media_view::{MediaControls, MediaView};
use desk_ui::components::tree::project_folder;
use gpui::{
    AnyElement, AnyView, App, AppContext, Entity, SharedString, StyleRefinement, prelude::*,
};

pub struct Viewer {
    media: Entity<MediaView>,
    controls: Entity<MediaControls>,
    name: SharedString,
}

impl Viewer {
    pub fn new(path: Arc<Path>, cx: &mut App) -> Self {
        let name = path.file_name().map_or_else(
            || path.display().to_string(),
            |name| name.to_string_lossy().into_owned(),
        );
        let media = cx.new(|cx| MediaView::new(path, cx));
        let controls = cx.new(|cx| MediaControls::new(media.clone(), cx));
        Viewer {
            media,
            controls,
            name: name.into(),
        }
    }

    pub fn shows(&self, path: &Path, cx: &App) -> bool {
        **self.media.read(cx).path() == *path
    }

    pub fn name(&self) -> SharedString {
        self.name.clone()
    }

    pub fn controls(&self) -> AnyElement {
        self.controls.clone().into_any_element()
    }

    pub fn media(&self) -> AnyView {
        self.media.clone().into()
    }

    pub fn body(&self) -> AnyElement {
        let media = AnyView::from(self.media.clone()).cached(
            StyleRefinement::default()
                .size_full()
                .min_h_0()
                .flex()
                .flex_col(),
        );
        timed("viewer media", media).into_any_element()
    }
}

pub fn resolve(path: &Path, cx: &App) -> Result<Arc<Path>, String> {
    if path.is_absolute() {
        return Ok(Arc::from(path));
    }
    if !path
        .components()
        .all(|part| matches!(part, Component::Normal(_) | Component::CurDir))
    {
        return Err(format!(
            "{} reaches outside the project, so it is not opened",
            path.display()
        ));
    }
    let root = match project_folder(cx) {
        Some(root) => root.to_path_buf(),
        None => std::env::current_dir()
            .map_err(|error| format!("the viewer has no working folder: {error}"))?,
    };
    Ok(Arc::from(root.join(path)))
}
