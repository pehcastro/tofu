mod desk;
#[path = "screens/theme/pick.rs"]
mod pick;
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
    #[cfg(any(feature = "module-file-edits", feature = "screen-work"))]
    pub mod file_edits;
    #[cfg(feature = "module-git")]
    pub mod git;
    #[cfg(any(
        feature = "module-subagents",
        feature = "module-file-edits",
        feature = "module-shells",
        feature = "screen-work"
    ))]
    pub mod replayed;
    #[cfg(any(feature = "module-shells", feature = "screen-work"))]
    pub mod shells;
    #[cfg(any(feature = "module-subagents", feature = "screen-work"))]
    pub mod subagents;
}

use std::cell::Cell;
use std::env;
use std::path::PathBuf;
use std::process::ExitCode;
use std::rc::Rc;

use desk::{Open, Screen};
use desk_ui::icon::Icon;
use gpui::{App, AppContext, EmptyView};
#[cfg(feature = "module-editor")]
use modules::editor::open_file;
#[cfg(feature = "module-git")]
use modules::git::open_repo;

#[cfg(not(feature = "module-editor"))]
fn open_file(
    _: &std::path::Path,
    _: Option<&str>,
    _: &mut gpui::Window,
    _: &mut App,
) -> Result<gpui::AnyView, String> {
    Err("--file needs the editor: rebuild with --features module-editor".to_owned())
}

#[cfg(not(feature = "module-git"))]
fn open_repo(
    _: &std::path::Path,
    _: Option<&str>,
    _: &mut gpui::Window,
    _: &mut App,
) -> Result<gpui::AnyView, String> {
    Err("--repo needs the git module: rebuild with --features module-git".to_owned())
}

const USAGE: &str = "usage: desk [--screen <name> [--board <ID>] [--file <path>] [--repo <path>]]\n\
--file opens a file in the editor screen\n\
--repo opens a git repository in the git screen\n\
screens: work settings accounts theme library classifier usage limits context session intro onboarding platforms\n\
modules: chat subagents file-edits shells git editor browser data-studio";
const SCREENS: [&str; 13] = [
    "work",
    "settings",
    "accounts",
    "theme",
    "library",
    "classifier",
    "usage",
    "limits",
    "context",
    "session",
    "intro",
    "onboarding",
    "platforms",
];

enum Target {
    File(PathBuf),
    Repo(PathBuf),
}

struct ScreenLaunch {
    name: String,
    board: Option<String>,
    target: Option<Target>,
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
            let (mut board, mut target) = (None, None);
            for pair in rest.chunks(2) {
                match pair {
                    [flag, id] if flag == "--board" && board.is_none() => board = Some(id.clone()),
                    [flag, path] if flag == "--file" && name == "editor" && target.is_none() => {
                        target = Some(Target::File(PathBuf::from(path)))
                    }
                    [flag, path] if flag == "--repo" && name == "git" && target.is_none() => {
                        target = Some(Target::Repo(PathBuf::from(path)))
                    }
                    _ => return Err(USAGE.to_owned()),
                }
            }
            Ok(Launch::Screen(ScreenLaunch {
                open: screen(name)?,
                name: name.clone(),
                board,
                target,
            }))
        }
        _ => Err(USAGE.to_owned()),
    }
}

fn open_launch(launch: Launch, cx: &mut App) -> Result<(), String> {
    let screens: Vec<Screen> = SCREENS
        .into_iter()
        .filter_map(|name| {
            Some(Screen {
                name,
                open: screen(name).ok()?,
            })
        })
        .collect();
    let (title, client, name, board, target, open): (String, _, String, _, _, Open) = match launch {
        Launch::Desk => (
            desk::WINDOW_TITLE.to_owned(),
            desk::desk_client(),
            "chat".to_owned(),
            None,
            None,
            |_, window, cx| {
                let store = cx.new(|_| desk_core::model::Store::default());
                Ok(modules::chat::live(store, window, cx).into())
            },
        ),
        Launch::Screen(launch) => (
            format!("{} {}", desk::WINDOW_TITLE, launch.name),
            desk::board_client(),
            launch.name,
            launch.board,
            launch.target,
            launch.open,
        ),
    };
    let mut refused = None;
    let window = cx.open_window(
        desk::window_options(title.into(), client, cx),
        |window, cx| {
            let opened = match &target {
                Some(Target::File(path)) => open_file(path, board.as_deref(), window, cx),
                Some(Target::Repo(path)) => open_repo(path, board.as_deref(), window, cx),
                None => open(board.as_deref(), window, cx),
            };
            let view = opened.unwrap_or_else(|error| {
                refused = Some(error);
                cx.new(|_| EmptyView).into()
            });
            cx.new(|cx| desk::Desk::new(screens, name.into(), view, window, cx))
        },
    );
    if let Some(error) = refused {
        return Err(error);
    }
    window
        .map(drop)
        .map_err(|error| format!("tofu desk could not open its window: {error:#}"))
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
            let opened = pick::chosen()
                .and_then(|chosen| {
                    pick::start(&chosen.theme, cx)?;
                    pick::set_mode(&chosen.theme, chosen.mode, cx)
                })
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
