mod desk;
mod status_bar;
mod title_bar;

use std::env;
use std::path::PathBuf;

use desk_ui::icon::Icon;
use desk_ui::live::{self, THEME_VARIABLE};
use gpui::{App, AppContext};

const DEFAULT_THEME: &str = "tofu-glass";

fn main() {
    gpui_platform::application()
        .with_assets(Icon::registry())
        .run(|cx: &mut App| {
            cx.on_window_closed(|cx, _| {
                if cx.windows().is_empty() {
                    cx.quit();
                }
            })
            .detach();
            let themes = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../themes");
            let chosen = env::var_os(THEME_VARIABLE).map_or_else(
                || DEFAULT_THEME.to_owned(),
                |name| name.to_string_lossy().into_owned(),
            );
            if let Err(error) = live::start(themes, chosen, cx) {
                eprintln!("tofu desk has no theme to draw with: {error}");
                cx.quit();
                return;
            }
            if let Err(error) = cx.open_window(desk::window_options(cx), |_, cx| {
                cx.new(|_| desk::Desk::new())
            }) {
                eprintln!("tofu desk could not open its window: {error:#}");
                cx.quit();
            }
        });
}
