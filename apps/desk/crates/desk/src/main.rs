#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
#[cfg(feature = "screen-work")]
mod control;
mod desk;
#[path = "screens/theme/pick.rs"]
mod pick;
mod project;
mod status_bar;
mod title_bar;
mod screens {
    #[cfg(feature = "screen-accounts")]
    pub mod accounts;
    #[cfg(feature = "screen-classifier")]
    pub mod classifier;
    #[cfg(feature = "screen-context")]
    pub mod context;
    #[cfg(feature = "screen-forks")]
    pub mod forks;
    #[cfg(any(
        feature = "screen-usage",
        feature = "screen-limits",
        feature = "screen-classifier",
        feature = "screen-library",
        feature = "screen-context",
        feature = "screen-session",
        feature = "screen-forks"
    ))]
    pub mod frame;
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
    #[cfg(feature = "plugin-draw")]
    pub mod draw;
    #[cfg(feature = "module-editor")]
    pub mod editor;
    #[cfg(any(feature = "module-file-edits", feature = "screen-work"))]
    pub mod file_edits;
    #[cfg(feature = "module-git")]
    pub mod git;
    #[cfg(feature = "plugin-notepad")]
    pub mod notepad;
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
    #[cfg(feature = "screen-work")]
    pub mod terminal;
}
#[cfg(feature = "screen-work")]
mod plugins;

use std::cell::Cell;
use std::env;
use std::path::PathBuf;
use std::process::ExitCode;
use std::rc::Rc;
use std::time::Instant;

use desk::{Desk, NoTofu, Open, Screen};
use desk_core::first_run::FirstRun;
use desk_ui::components::title_bar::focused_name;
use desk_ui::icon::Icon;
use gpui::{App, AppContext, Context, EmptyView, KeystrokeEvent, Window, WindowHandle};
#[cfg(feature = "module-editor")]
use modules::editor::{open_dir, open_file};
#[cfg(feature = "module-git")]
use modules::git::open_repo;
#[cfg(feature = "screen-settings")]
use screens::settings::install_plugin;

#[cfg(not(feature = "screen-settings"))]
fn install_plugin(
    _: &std::path::Path,
    _: &mut gpui::Window,
    _: &mut App,
) -> Result<gpui::AnyView, String> {
    Err(
        "--install-plugin needs the settings screen: rebuild with --features screen-settings"
            .to_owned(),
    )
}

#[cfg(not(feature = "module-editor"))]
fn open_file(
    _: &std::path::Path,
    _: Option<&str>,
    _: &mut gpui::Window,
    _: &mut App,
) -> Result<gpui::AnyView, String> {
    Err("--file needs the editor: rebuild with --features module-editor".to_owned())
}

#[cfg(not(feature = "module-editor"))]
fn open_dir(
    _: &std::path::Path,
    _: Option<&std::path::Path>,
    _: Option<&str>,
    _: &mut gpui::Window,
    _: &mut App,
) -> Result<gpui::AnyView, String> {
    Err("--dir needs the editor: rebuild with --features module-editor".to_owned())
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

const USAGE: &str = "usage: desk [<folder>] | [--screen <name> [--board <ID>] [--file <path>] [--dir <path>] [--repo <path>]] | [--install-plugin <folder>]\n\
--install-plugin installs the plugin in <folder> and opens Settings > Plugins on its consent sheet\n\
--file opens a file in the editor screen\n\
--dir lists a folder in the editor screen's file tree\n\
--repo opens a git repository in the git screen\n\
screens: work settings accounts theme library classifier usage limits context session forks intro onboarding platforms\n\
modules: chat subagents file-edits shells git editor browser data-studio";
const SCREENS: [&str; 15] = [
    "work",
    "settings",
    "editor",
    "accounts",
    "theme",
    "library",
    "classifier",
    "usage",
    "limits",
    "context",
    "session",
    "forks",
    "intro",
    "onboarding",
    "platforms",
];

const FIRST_RUN_VARIABLE: &str = "DESK_FIRST_RUN";
const TAB_WALK_LIMIT: usize = 512;
const FOCUS_TRACE: &str = "DESK_FOCUS_TRACE";
const NO_TOFU: &str = "no-tofu";
#[cfg(windows)]
const INSTALL: &str =
    "irm https://raw.githubusercontent.com/pehcastro/tofu/release/scripts/install.ps1 | iex";
#[cfg(not(windows))]
const INSTALL: &str =
    "curl -fsSL https://raw.githubusercontent.com/pehcastro/tofu/release/scripts/install.sh | sh";

enum Target {
    File(PathBuf),
    Dir(PathBuf, Option<PathBuf>),
    Repo(PathBuf),
    Plugin(PathBuf),
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
        "forks" => gated!("screen-forks", screens::forks::open),
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
        [folder] if !folder.starts_with("--") => Ok(Launch::Desk),
        [flag, folder] if flag == "--install-plugin" => Ok(Launch::Screen(ScreenLaunch {
            open: screen("settings")?,
            name: "settings".to_owned(),
            board: None,
            target: Some(Target::Plugin(PathBuf::from(folder))),
        })),
        [flag, name, rest @ ..] if flag == "--screen" => {
            let (mut board, mut file, mut dir, mut repo) = (None, None, None, None);
            for pair in rest.chunks(2) {
                match pair {
                    [flag, id] if flag == "--board" && board.is_none() => board = Some(id.clone()),
                    [flag, path] if flag == "--file" && name == "editor" && file.is_none() => {
                        file = Some(PathBuf::from(path))
                    }
                    [flag, path] if flag == "--dir" && name == "editor" && dir.is_none() => {
                        dir = Some(PathBuf::from(path))
                    }
                    [flag, path] if flag == "--repo" && name == "git" && repo.is_none() => {
                        repo = Some(PathBuf::from(path))
                    }
                    _ => return Err(USAGE.to_owned()),
                }
            }
            let target = match (dir, file, repo) {
                (Some(dir), file, _) => Some(Target::Dir(dir, file)),
                (None, Some(file), _) => Some(Target::File(file)),
                (None, None, repo) => repo.map(Target::Repo),
            };
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

fn forced() -> Result<Option<FirstRun>, String> {
    let Some(state) = env::var_os(FIRST_RUN_VARIABLE) else {
        return Ok(None);
    };
    match state.to_str() {
        Some("no-tofu") => Ok(Some(FirstRun::NoTofu {
            reason: format!("{FIRST_RUN_VARIABLE} forced it"),
        })),
        Some("no-account") => Ok(Some(FirstRun::NoAccount)),
        Some("no-project") => Ok(Some(FirstRun::NoProject)),
        Some("ready") => Ok(Some(FirstRun::Ready)),
        _ => Err(format!(
            "{FIRST_RUN_VARIABLE} is {}, not one of no-tofu no-account no-project ready",
            state.to_string_lossy()
        )),
    }
}

fn onboarding(
    board: Option<&str>,
    desk: &mut Desk,
    window: &mut Window,
    cx: &mut Context<Desk>,
) -> Result<String, String> {
    let view = screen("onboarding")?(board, window, cx)?;
    desk.route("onboarding", view, window, cx);
    Ok(format!("onboarding {}", board.unwrap_or("IONB-1")))
}

fn route(
    run: &FirstRun,
    desk: &mut Desk,
    window: &mut Window,
    cx: &mut Context<Desk>,
) -> Result<String, String> {
    match desk.open_screens().first() {
        Some(&"work") => {}
        Some(shown) => return Ok(format!("{shown}, kept because it changed first")),
        None => return Err("the desk shows no screen".to_owned()),
    }
    let tofu = project::tofu();
    let looked = match tofu.components().count() {
        1 => format!("{} on your PATH", tofu.display()),
        _ => tofu.display().to_string(),
    };
    let (title, line, command) = match run {
        FirstRun::Ready => return Ok("work".to_owned()),
        FirstRun::NoProject | FirstRun::UnknownStep { .. } => {
            return onboarding(None, desk, window, cx);
        }
        FirstRun::NoAccount => return onboarding(Some("S-ONB-1"), desk, window, cx),
        FirstRun::NoClassifier => return onboarding(Some("S-ONB-2"), desk, window, cx),
        FirstRun::NoTofu { reason } => (
            "tofu was not found",
            format!(
                "The desk looked for {looked} and could not start it: {reason}. Install tofu, then open the desk again."
            ),
            INSTALL,
        ),
        FirstRun::OldTofu { found } => (
            "This tofu is too old for the desk",
            format!(
                "The desk started {looked} and it answered: {found}. Update it, then open the desk again."
            ),
            "tofu update",
        ),
    };
    let panel = NoTofu {
        title: title.into(),
        line: line.into(),
        command: command.into(),
    };
    desk.route(NO_TOFU, cx.new(|_| panel).into(), window, cx);
    Ok(NO_TOFU.to_owned())
}

fn walk_focus(event: &KeystrokeEvent, trace: bool, window: &mut Window, cx: &mut App) {
    let stroke = &event.keystroke;
    let held = stroke.modifiers;
    if stroke.key != "tab"
        || event.action.is_some()
        || held.control
        || held.alt
        || held.platform
        || held.function
    {
        return;
    }
    let pressed = Instant::now();
    let start = window.focused(cx);
    if focused_name(window).is_none() {
        window.blur(cx);
    }
    for step in 1..=TAB_WALK_LIMIT {
        if held.shift {
            window.focus_prev(cx);
        } else {
            window.focus_next(cx);
        }
        if let Some(name) = focused_name(window) {
            if trace {
                trace_order(window, cx);
            }
            return window.on_next_frame(move |_, _| {
                eprintln!(
                    "desk: focus {name} in {:.2} ms after {step} steps",
                    pressed.elapsed().as_secs_f64() * 1000.0
                )
            });
        }
    }
    match start {
        Some(start) => window.focus(&start, cx),
        None => window.blur(cx),
    }
    eprintln!("desk: focus stays, no control on screen takes Tab");
}

fn trace_order(window: &mut Window, cx: &mut App) {
    let here = window.focused(cx);
    window.blur(cx);
    let mut seen = Vec::new();
    while seen.len() < TAB_WALK_LIMIT {
        window.focus_next(cx);
        let Some(focused) = window.focused(cx) else {
            break;
        };
        if seen.contains(&focused) {
            break;
        }
        let name = focused_name(window).unwrap_or_else(|| "unnamed".into());
        eprintln!("desk: focus order {} {name} {focused:?}", seen.len());
        seen.push(focused);
    }
    match here {
        Some(here) => window.focus(&here, cx),
        None => window.blur(cx),
    }
}

fn open_launch(launch: Launch, cx: &mut App) -> Result<WindowHandle<Desk>, String> {
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
            desk::board_client(),
            "work".to_owned(),
            None,
            None,
            screen("work")?,
        ),
        Launch::Screen(launch) => (
            format!("{} {}", desk::WINDOW_TITLE, launch.name),
            if launch.name == "chat" {
                desk::desk_client()
            } else {
                desk::board_client()
            },
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
                Some(Target::Dir(dir, file)) => {
                    open_dir(dir, file.as_deref(), board.as_deref(), window, cx)
                }
                Some(Target::Repo(path)) => open_repo(path, board.as_deref(), window, cx),
                Some(Target::Plugin(folder)) => install_plugin(folder, window, cx),
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
    window.map_err(|error| format!("tofu desk could not open its window: {error:#}"))
}

fn main() -> ExitCode {
    let started = Instant::now();
    let args: Vec<String> = env::args().skip(1).collect();
    let (launch, forced) = match parse(&args).and_then(|launch| Ok((launch, forced()?))) {
        Ok(parsed) => parsed,
        Err(error) => {
            eprintln!("{error}");
            return ExitCode::FAILURE;
        }
    };
    if let Err(error) = desk_core::home::Home::find().and_then(|home| home.migrate()) {
        eprintln!("desk: home: {error}");
        return ExitCode::FAILURE;
    }
    let explicit = match &launch {
        Launch::Desk => None,
        Launch::Screen(launch) => Some(launch.name.clone()),
    };
    let (told, heard) = flume::bounded(1);
    std::thread::spawn(move || {
        let started = Instant::now();
        let run = forced.unwrap_or_else(|| {
            let chosen = project::chosen().ok().flatten().map(|(folder, _)| folder);
            desk_core::first_run::detect(&project::tofu(), chosen.as_deref())
        });
        eprintln!(
            "desk: first run {run} detected in {} ms",
            started.elapsed().as_millis()
        );
        if told.send(run).is_err() {
            eprintln!("desk: first run answered after the desk closed");
        }
    });
    #[cfg(feature = "screen-work")]
    plugins::start();
    let mut assets = Icon::registry();
    if let Err(duplicates) = assets.extend(desk_ui::components::glyph::Glyph::entries()) {
        eprintln!("the desk has two assets under one path: {duplicates:?}");
        return ExitCode::FAILURE;
    }
    #[cfg(feature = "screen-work")]
    let (mut server, asked) = match control::start() {
        Ok((server, asked)) => (Some(server), Some(asked)),
        Err(error) => {
            eprintln!("desk: control is off: {error}");
            (None, None)
        }
    };
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
            let trace = env::var_os(FOCUS_TRACE).is_some();
            cx.observe_keystrokes(move |event, window, cx| walk_focus(event, trace, window, cx))
                .detach();
            #[cfg(feature = "screen-work")]
            cx.on_app_quit(move |_| {
                drop(server.take());
                async {}
            })
            .detach();
            let opened = pick::chosen()
                .and_then(|chosen| {
                    pick::start(&chosen.theme, cx)?;
                    pick::set_mode(&chosen.theme, chosen.mode, cx)
                })
                .and_then(|()| open_launch(launch, cx));
            let opened = match opened {
                Ok(opened) => opened,
                Err(error) => {
                    eprintln!("{error}");
                    failure.set(true);
                    cx.quit();
                    return;
                }
            };
            cx.spawn(async move |cx| {
                let Ok(run) = heard.recv_async().await else {
                    return eprintln!("desk: first run never answered");
                };
                let routed = match explicit {
                    Some(name) => Ok(format!("{name}, the screen argument")),
                    None => opened
                        .update(cx, |desk, window, cx| route(&run, desk, window, cx))
                        .map_err(|error| format!("{error:#}"))
                        .flatten(),
                };
                match routed {
                    Ok(screen) => eprintln!(
                        "desk: first run {run} routed to {screen} in {} ms",
                        started.elapsed().as_millis()
                    ),
                    Err(error) => eprintln!("desk: first run {run} was not routed: {error}"),
                }
            })
            .detach();
            let Some(window) = cx.windows().first().copied() else {
                return;
            };
            #[cfg(feature = "screen-work")]
            if let Some(asked) = asked {
                let screens = SCREENS
                    .into_iter()
                    .map(|name| (name, screen(name).is_ok()))
                    .collect();
                control::serve(asked, window, screens, cx);
            }
            let painted = window.update(cx, |_, window, _| {
                window.on_next_frame(move |_, _| {
                    eprintln!(
                        "desk: first paint after {} ms",
                        started.elapsed().as_millis()
                    );
                });
            });
            if let Err(error) = painted {
                eprintln!("desk: first paint is not timed: {error:#}");
            }
        });
    if failed.get() {
        ExitCode::FAILURE
    } else {
        ExitCode::SUCCESS
    }
}
