use desk_terminal::Terminal;
use gpui::{App, AppContext, Focusable, TitlebarOptions, WindowBounds, WindowOptions, px, size};
use std::path::Path;

const TITLE: &str = "Tofu Terminal";
const WIDTH: f32 = 960.;
const HEIGHT: f32 = 600.;
const REPOSITORY_DEPTH: usize = 4;

fn main() {
    let manifest = Path::new(env!("CARGO_MANIFEST_DIR"));
    let Some(repository) = manifest.ancestors().nth(REPOSITORY_DEPTH) else {
        eprintln!("{} has no repository above it", manifest.display());
        return;
    };
    let repository = repository.to_path_buf();
    gpui_platform::application().run(move |cx: &mut App| {
        cx.on_window_closed(|cx, _| cx.quit()).detach();
        let options = WindowOptions {
            window_bounds: Some(WindowBounds::centered(size(px(WIDTH), px(HEIGHT)), cx)),
            titlebar: Some(TitlebarOptions {
                title: Some(TITLE.into()),
                ..TitlebarOptions::default()
            }),
            ..WindowOptions::default()
        };
        let opened = cx.open_window(options, |window, cx| {
            let terminal = cx.new(|cx| Terminal::new(&repository, cx));
            window.focus(&terminal.focus_handle(cx), cx);
            terminal
        });
        if let Err(error) = opened {
            eprintln!("could not open the terminal window: {error:#}");
            cx.quit();
        }
    });
}
