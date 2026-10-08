mod book;
mod catalog;
mod data;
mod themes;

use std::cell::Cell;
use std::env;
use std::process::ExitCode;
use std::rc::Rc;

use desk_perf::{ALLOCATOR, CountingAllocator, Profiler};
use desk_ui::components::glyph::Glyph;
use desk_ui::icon::Icon;
use desk_ui::metrics::{WINDOW_HEIGHT, WINDOW_MIN_HEIGHT, WINDOW_MIN_WIDTH, WINDOW_WIDTH};
use desk_ui::theme::Mode;
use gpui::{App, AppContext, TitlebarOptions, WindowBounds, WindowOptions, px, size};

use book::Book;
use catalog::Page;
use themes::{Choice, DEFAULT_THEME};

#[global_allocator]
static GLOBAL: &CountingAllocator = &ALLOCATOR;

const TITLE: &str = "Tofu Desk Book";
const USAGE: &str = "usage: book [--page <name>] [--theme <name>] [--mode light|dark]";

pub struct Launch {
    pub page: Page,
    pub theme: usize,
    pub themes: Vec<Choice>,
    pub mode: Option<Mode>,
}

fn parse(args: &[String], themes: Vec<Choice>) -> Result<Launch, String> {
    let mut page = Page::Button;
    let mut theme_name = DEFAULT_THEME;
    let mut mode = None;
    let mut rest = args;
    while let [flag, value, tail @ ..] = rest {
        match flag.as_str() {
            "--page" => {
                page = Page::parse(value).ok_or_else(|| {
                    let known: Vec<&str> = Page::ALL.iter().map(|page| page.sheet().slug).collect();
                    format!("unknown page {value}; pages: {}", known.join(" "))
                })?;
            }
            "--theme" => theme_name = value,
            "--mode" => mode = Some(themes::parse_mode(value)?),
            _ => return Err(USAGE.to_owned()),
        }
        rest = tail;
    }
    if !rest.is_empty() {
        return Err(USAGE.to_owned());
    }
    let theme = themes
        .iter()
        .position(|choice| choice.name == theme_name)
        .ok_or_else(|| {
            let known: Vec<&str> = themes.iter().map(|choice| choice.name.as_ref()).collect();
            format!("unknown theme {theme_name}; themes: {}", known.join(" "))
        })?;
    Ok(Launch {
        page,
        theme,
        themes,
        mode,
    })
}

fn window_options(cx: &App) -> WindowOptions {
    let mut options = WindowOptions::new()
        .window_bounds(Some(WindowBounds::centered(
            size(px(WINDOW_WIDTH), px(WINDOW_HEIGHT)),
            cx,
        )))
        .window_min_size(Some(size(px(WINDOW_MIN_WIDTH), px(WINDOW_MIN_HEIGHT))))
        .titlebar(Some(TitlebarOptions {
            title: Some(TITLE.into()),
            appears_transparent: false,
            traffic_light_position: None,
        }));
    #[cfg(target_os = "windows")]
    {
        options.windows_window_background = gpui::WindowsWindowBackground::Transparent;
    }
    options
}

fn open(launch: Launch, cx: &mut App) -> Result<(), String> {
    let choice = launch
        .themes
        .get(launch.theme)
        .ok_or_else(|| USAGE.to_owned())?;
    themes::apply(choice, launch.mode, cx)?;
    let window = cx
        .open_window(window_options(cx), |window, cx| {
            cx.new(|cx| Book::new(launch, window, cx))
        })
        .map_err(|error| format!("the book could not open its window: {error:#}"))?;
    window
        .update(cx, |_, window, _| window.activate())
        .map_err(|error| format!("the book could not raise its window: {error:#}"))?;
    cx.activate(true);
    Ok(())
}

fn main() -> ExitCode {
    let args: Vec<String> = env::args().skip(1).collect();
    let launch = match themes::discover().and_then(|themes| parse(&args, themes)) {
        Ok(launch) => launch,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::FAILURE;
        }
    };
    let mut assets = Icon::registry();
    if let Err(duplicates) = assets.extend(Glyph::entries()) {
        eprintln!("the book has two assets under one path: {duplicates:?}");
        return ExitCode::FAILURE;
    }
    let failed = Rc::new(Cell::new(false));
    let failure = Rc::clone(&failed);
    gpui_platform::application()
        .with_assets(assets)
        .run(move |cx: &mut App| {
            cx.on_window_closed(|cx, _| {
                if cx.windows().is_empty() {
                    cx.quit();
                }
            })
            .detach();
            Profiler::install(None, cx);
            if let Err(error) = open(launch, cx) {
                eprintln!("{error}");
                failure.set(true);
                cx.quit();
            }
        });
    if failed.get() {
        ExitCode::FAILURE
    } else {
        ExitCode::SUCCESS
    }
}
