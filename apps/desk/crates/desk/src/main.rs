mod desk;
mod status_bar;
mod title_bar;

use desk_ui::icon::Icon;
use gpui::{App, AppContext};

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
            if let Err(error) = cx.open_window(desk::window_options(cx), |_, cx| {
                cx.new(|_| desk::Desk::new())
            }) {
                eprintln!("tofu desk could not open its window: {error:#}");
                cx.quit();
            }
        });
}
