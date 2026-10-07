mod desk;
mod status_bar;
mod title_bar;
mod screens {
    #[cfg(feature = "screen-accounts")]
    pub mod accounts;
    #[cfg(feature = "screen-classifier")]
    pub mod classifier;
    #[cfg(feature = "screen-context")]
    pub mod context;
    #[cfg(feature = "screen-intro")]
    pub mod intro;
    #[cfg(feature = "screen-library")]
    pub mod library;
    #[cfg(feature = "screen-limits")]
    pub mod limits;
    #[cfg(feature = "screen-onboarding")]
    pub mod onboarding;
    #[cfg(feature = "screen-platforms")]
    pub mod platforms;
    #[cfg(feature = "screen-session")]
    pub mod session;
    #[cfg(feature = "screen-settings")]
    pub mod settings;
    #[cfg(feature = "screen-theme")]
    pub mod theme;
    #[cfg(feature = "screen-usage")]
    pub mod usage;
    #[cfg(feature = "screen-work")]
    pub mod work;
}
mod modules {
    #[cfg(feature = "module-browser")]
    pub mod browser;
    pub mod chat;
    #[cfg(feature = "module-data-studio")]
    pub mod data_studio;
    #[cfg(feature = "module-editor")]
    pub mod editor;
    #[cfg(feature = "module-file-edits")]
    pub mod file_edits;
    #[cfg(feature = "module-git")]
    pub mod git;
    #[cfg(feature = "module-shells")]
    pub mod shells;
    #[cfg(feature = "module-subagents")]
    pub mod subagents;
}

use std::cell::Cell;
use std::env;
use std::path::PathBuf;
use std::process::ExitCode;
use std::rc::Rc;

use desk_ui::icon::Icon;
use desk_ui::live::{self, THEME_VARIABLE};
use gpui::{AnyView, App, AppContext, Window};

const DEFAULT_THEME: &str = "tofu-glass";
const USAGE: &str = "usage: desk [--screen <name> [--board <ID>]]\n\
screens: work settings accounts theme library classifier usage limits context session intro onboarding platforms\n\
modules: chat subagents file-edits shells git editor browser data-studio";

type Open = fn(Option<&str>, &mut Window, &mut App) -> Result<AnyView, String>;

struct ScreenLaunch {
    name: String,
    board: Option<String>,
    open: Open,
}

enum Launch {
    Desk,
    Screen(ScreenLaunch),
}

macro_rules! gated {
    ($feature:literal, $open:path) => {{
        #[cfg(feature = $feature)]
        let open: Option<Open> = Some($open);
        #[cfg(not(feature = $feature))]
        let open: Option<Open> = None;
        ($feature, open)
    }};
}

fn screen(name: &str) -> Result<Open, String> {
    let (feature, open) = match name {
        "work" => gated!("screen-work", screens::work::open),
        "settings" => gated!("screen-settings", screens::settings::open),
        "accounts" => gated!("screen-accounts", screens::accounts::open),
        "theme" => gated!("screen-theme", screens::theme::open),
        "library" => gated!("screen-library", screens::library::open),
        "classifier" => gated!("screen-classifier", screens::classifier::open),
        "usage" => gated!("screen-usage", screens::usage::open),
        "limits" => gated!("screen-limits", screens::limits::open),
        "context" => gated!("screen-context", screens::context::open),
        "session" => gated!("screen-session", screens::session::open),
        "intro" => gated!("screen-intro", screens::intro::open),
        "onboarding" => gated!("screen-onboarding", screens::onboarding::open),
        "chat" => ("chat", Some(modules::chat::open as Open)),
        "subagents" => gated!("module-subagents", modules::subagents::open),
        "file-edits" => gated!("module-file-edits", modules::file_edits::open),
        "shells" => gated!("module-shells", modules::shells::open),
        "git" => gated!("module-git", modules::git::open),
        "editor" => gated!("module-editor", modules::editor::open),
        "browser" => gated!("module-browser", modules::browser::open),
        "data-studio" => gated!("module-data-studio", modules::data_studio::open),
        "platforms" => gated!("screen-platforms", screens::platforms::open),
        _ => return Err(format!("unknown screen {name}\n{USAGE}")),
    };
    open.ok_or_else(|| {
        format!("screen {name} is not in this build: rebuild with --features {feature}")
    })
}

fn parse(args: &[String]) -> Result<Launch, String> {
    match args {
        [] => Ok(Launch::Desk),
        [flag, name, rest @ ..] if flag == "--screen" => {
            let board = match rest {
                [] => None,
                [flag, id] if flag == "--board" => Some(id.clone()),
                _ => return Err(USAGE.to_owned()),
            };
            Ok(Launch::Screen(ScreenLaunch {
                open: screen(name)?,
                name: name.clone(),
                board,
            }))
        }
        _ => Err(USAGE.to_owned()),
    }
}

fn open_launch(launch: Launch, cx: &mut App) -> Result<(), String> {
    let opened = match launch {
        Launch::Desk => cx
            .open_window(
                desk::window_options(desk::WINDOW_TITLE.into(), desk::desk_client(), cx),
                |window, cx| {
                    let chat = modules::chat::live(window, cx);
                    cx.new(|_| desk::Desk::new(chat))
                },
            )
            .map(drop),
        Launch::Screen(launch) => {
            let title = format!("{} {}", desk::WINDOW_TITLE, launch.name);
            let mut refused = None;
            let window = cx.open_window(
                desk::window_options(title.into(), desk::board_client(), cx),
                |window, cx| {
                    let view = (launch.open)(launch.board.as_deref(), window, cx)
                        .map_err(|error| refused = Some(error))
                        .ok();
                    cx.new(|_| desk::ScreenRoot(view))
                },
            );
            if let Some(error) = refused {
                return Err(error);
            }
            window.map(drop)
        }
    };
    opened.map_err(|error| format!("tofu desk could not open its window: {error:#}"))
}

fn main() -> ExitCode {
    let args: Vec<String> = env::args().skip(1).collect();
    let launch = match parse(&args) {
        Ok(launch) => launch,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::FAILURE;
        }
    };
    let failed = Rc::new(Cell::new(false));
    let failure = Rc::clone(&failed);
    gpui_platform::application()
        .with_assets(Icon::registry())
        .run(move |cx: &mut App| {
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
            let opened = live::start(themes, chosen, cx)
                .map_err(|error| format!("tofu desk has no theme to draw with: {error}"))
                .and_then(|()| open_launch(launch, cx));
            if let Err(error) = opened {
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
