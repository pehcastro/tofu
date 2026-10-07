use std::borrow::Cow;
use std::cell::RefCell;
use std::path::Path;
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};

use desk_ui::components::glyph::Glyph;
use desk_ui::icon::Icon;
use desk_ui::theme::Mode;
use gpui::{
    AnyWindowHandle, AppContext, AssetRegistry, AssetSource, HeadlessAppContext, Pixels,
    PlatformHeadlessRenderer, PlatformTextSystem, SharedString, Size,
};
use gpui_wgpu::{CosmicTextSystem, WgpuHeadlessRenderer};
use image::RgbaImage;

use crate::Launch;
use crate::book::Book;
use crate::catalog::Page;
use crate::themes;

const FONT_FALLBACK: &str = "Segoe UI";
const ICONS: [Icon; 10] = [
    Icon::Sidebar,
    Icon::Plus,
    Icon::Search,
    Icon::Bell,
    Icon::Minimize,
    Icon::Maximize,
    Icon::Restore,
    Icon::Close,
    Icon::Branch,
    Icon::Arrow,
];
pub const SETTLE: Duration = Duration::from_millis(700);
const FRAME: Duration = Duration::from_millis(8);

struct Assets {
    registry: AssetRegistry,
    paths: Vec<SharedString>,
}

impl AssetSource for Assets {
    fn load(&self, path: &str) -> gpui::Result<Option<Cow<'static, [u8]>>> {
        Ok(self
            .registry
            .load(path)
            .map(|data| Cow::Owned(data.into_owned())))
    }

    fn list(&self, _: &str) -> gpui::Result<Vec<SharedString>> {
        Ok(self.paths.clone())
    }
}

fn assets() -> Result<Assets, String> {
    let glyphs = Glyph::entries();
    let mut paths: Vec<SharedString> = ICONS.iter().map(|icon| icon.path().into()).collect();
    paths.extend(glyphs.iter().map(|(path, _)| path.clone().into()));
    let mut registry = Icon::registry();
    registry
        .extend(glyphs)
        .map_err(|duplicates| format!("two assets under one path: {duplicates:?}"))?;
    Ok(Assets { registry, paths })
}

pub fn platform() -> Result<(CosmicTextSystem, WgpuHeadlessRenderer), String> {
    let renderer = WgpuHeadlessRenderer::new()
        .map_err(|error| format!("the headless renderer did not start: {error:#}"))?;
    Ok((CosmicTextSystem::new(FONT_FALLBACK), renderer))
}

pub struct Setup<'a> {
    pub page: Page,
    pub theme: &'a str,
    pub mode: Option<Mode>,
    pub size: Size<Pixels>,
}

pub struct Session {
    pub cx: HeadlessAppContext,
    pub window: AnyWindowHandle,
    pub scale: f32,
}

impl Session {
    pub fn open(
        setup: &Setup,
        text: Arc<dyn PlatformTextSystem>,
        renderer: Box<dyn PlatformHeadlessRenderer>,
    ) -> Result<Session, String> {
        let themes = themes::discover()?;
        let theme = themes
            .iter()
            .position(|choice| choice.name == setup.theme)
            .ok_or_else(|| format!("unknown theme {}", setup.theme))?;
        let renderer = RefCell::new(Some(renderer));
        let mut cx = HeadlessAppContext::with_platform(text, Arc::new(assets()?), move || {
            renderer.borrow_mut().take()
        });
        let mode = setup.mode;
        cx.update(|cx| {
            themes
                .get(theme)
                .map(|choice| themes::apply(choice, mode, cx))
        })
        .ok_or_else(|| format!("no theme at {theme}"))??;
        let launch = Launch {
            page: setup.page,
            theme,
            themes,
            mode,
        };
        let handle = cx
            .open_window(setup.size, |window, cx| {
                cx.new(|cx| Book::new(launch, window, cx))
            })
            .map_err(|error| format!("the headless window did not open: {error:#}"))?;
        let mut session = Session {
            cx,
            window: handle.into(),
            scale: 1.0,
        };
        session.scale = session.with(|window, _| window.scale_factor())?;
        session.settle(SETTLE)?;
        Ok(session)
    }

    pub fn with<R>(
        &mut self,
        f: impl FnOnce(&mut gpui::Window, &mut gpui::App) -> R,
    ) -> Result<R, String> {
        self.cx
            .update_window(self.window, |_, window, cx| f(window, cx))
            .map_err(|error| format!("the window is gone: {error:#}"))
    }

    pub fn frame(&mut self) -> Result<(), String> {
        self.cx.run_until_parked();
        self.with(|window, cx| window.draw(cx).clear(cx))
    }

    pub fn settle(&mut self, span: Duration) -> Result<(), String> {
        let end = Instant::now() + span;
        while Instant::now() < end {
            self.frame()?;
            thread::sleep(FRAME);
        }
        self.frame()
    }

    pub fn advance(&mut self, span: Duration) {
        self.cx.advance_clock(span);
        self.cx.run_until_parked();
    }

    pub fn capture(&mut self) -> Result<RgbaImage, String> {
        self.frame()?;
        self.cx
            .capture_screenshot(self.window)
            .map_err(|error| format!("capture failed: {error:#}"))
    }
}

pub fn save(image: &RgbaImage, file: &Path) -> Result<(), String> {
    image
        .save(file)
        .map_err(|error| format!("{} was not written: {error}", file.display()))
}
