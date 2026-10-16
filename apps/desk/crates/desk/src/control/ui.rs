use std::io::ErrorKind;
use std::path::{Component, Path};

use desk_control::ErrorCode::{self, *};
use desk_control::params::{
    ClipboardSet, MaybeId, NotifyShow, PaletteList, PaletteRun, PathRef, StatusClick, ThemeSet,
};
use desk_control::replies::{
    Ack, Clipboard, Created, Notification, PaletteEntry, StatusControl, StatusOpened, Theme,
};
use desk_control::{Call, shapes};
use desk_core::home::Home;
use desk_ui::components::status_bar::controls;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::Mode;
use gpui::{AnyWindowHandle, App, ClipboardItem, Context, Window};
use serde_json::{Value, json as value};

use super::change::{Answered, json};
use crate::desk::{Desk, Unpicked};
use crate::pick;

const CLIPBOARD_KEY: &str = "control.clipboard";
const SETTINGS_FILE: &str = "settings.json";
const BRANCH: &str = "branch";

type Refusal = (ErrorCode, String);

pub fn answer(call: &Call, window: AnyWindowHandle, cx: &mut App) -> Option<Answered> {
    Some(match call {
        Call::PaletteList(PaletteList { query }) => with_desk(window, cx, |desk, _, cx| {
            let query = query.as_deref().map(str::to_lowercase);
            json(
                &desk
                    .palette_entries(cx)
                    .iter()
                    .filter(|(item, _)| {
                        query
                            .as_ref()
                            .is_none_or(|query| item.label.to_lowercase().contains(query))
                    })
                    .map(|(item, _)| PaletteEntry {
                        id: item.id.to_string(),
                        title: item.label.to_string(),
                        source: item.group.to_string(),
                        args_schema: Value::Null,
                    })
                    .collect::<Vec<_>>(),
            )
        }),
        Call::PaletteRun(run) => palette_run(run, window, cx),
        Call::MenuInvoke(PathRef { path }) => Err((
            NotFound,
            format!(
                "menu.invoke {path}: no desk menu has a path model yet; the screen tab menu, the cron menu and the context menus pick an item by its position"
            ),
        )),
        Call::StatusbarGet(_) => with_desk(window, cx, |desk, _, cx| json(&listed(desk, cx))),
        Call::StatusbarClick(StatusClick { control }) => with_desk(window, cx, |desk, _, cx| {
            if control == BRANCH {
                desk.open_branch(true, cx);
                return json(&StatusOpened {
                    opened: BRANCH.to_owned(),
                });
            }
            let names: Vec<String> = listed(desk, cx)
                .into_iter()
                .map(|shown| shown.control)
                .collect();
            Err(match names.contains(control) {
                true => (
                    Refused,
                    format!(
                        "statusbar.click {control}: only {BRANCH} opens from a control call, {control} keeps its open state inside the status bar"
                    ),
                ),
                false => (
                    NotFound,
                    format!(
                        "the status bar has no control {control}: it shows {}",
                        names.join(", ")
                    ),
                ),
            })
        }),
        Call::NotifyList(_) => with_desk(window, cx, |desk, _, _| {
            json(
                &desk
                    .shown_toast()
                    .map(|toast| Notification {
                        id: toast.id.to_string(),
                        title: toast.title.to_string(),
                        body: toast.body.to_string(),
                        source: toast.source.to_owned(),
                        at: toast.at.clone(),
                    })
                    .into_iter()
                    .collect::<Vec<_>>(),
            )
        }),
        Call::NotifyDismiss(MaybeId { id }) => {
            let unknown = || {
                (
                    NotFound,
                    format!("no toast {} shows", id.as_deref().unwrap_or("at all")),
                )
            };
            id.as_deref()
                .map(str::parse::<u64>)
                .transpose()
                .map_err(|_| unknown())
                .and_then(|wanted| {
                    with_desk(window, cx, |desk, _, cx| {
                        match desk.dismiss_toast(wanted, cx) {
                            true => json(&Ack { ok: true }),
                            false => Err(unknown()),
                        }
                    })
                })
        }
        Call::NotifyShow(NotifyShow { title, body, tone }) => match tone {
            Some(tone) => Err((
                BadParams,
                format!("notify.show has no tone {tone}: desk toasts carry no tone yet"),
            )),
            None => with_desk(window, cx, |desk, _, cx| {
                let id = desk.notify(title.clone().into(), body.clone().into(), cx);
                json(&Created { id: id.to_string() })
            }),
        },
        Call::ThemeGet(_) => pick::chosen()
            .map_err(|error| (Refused, error))
            .and_then(|chosen| themed(chosen.theme, cx)),
        Call::ThemeSet(set) => theme_set(set, cx),
        Call::ClipboardGet(_) => consented().and_then(|()| {
            json(&Clipboard {
                text: cx
                    .read_from_clipboard()
                    .and_then(|item| item.text())
                    .unwrap_or_default(),
            })
        }),
        Call::ClipboardSet(ClipboardSet { text }) => consented().and_then(|()| {
            cx.write_to_clipboard(ClipboardItem::new_string(text.clone()));
            json(&Clipboard { text: text.clone() })
        }),
        _ => return None,
    })
}

fn with_desk(
    window: AnyWindowHandle,
    cx: &mut App,
    run: impl FnOnce(&mut Desk, &mut Window, &mut Context<Desk>) -> Answered,
) -> Answered {
    let gone = |error: String| (DeskNotRunning, format!("the desk window is gone: {error}"));
    window
        .downcast::<Desk>()
        .ok_or_else(|| gone("its root view is not the desk".to_owned()))?
        .update(cx, run)
        .map_err(|error| gone(format!("{error:#}")))?
}

fn listed(desk: &Desk, cx: &App) -> Vec<StatusControl> {
    controls(&desk.status(cx))
        .into_iter()
        .map(|(label, text)| StatusControl {
            control: label.to_lowercase(),
            label: text.to_string(),
            tooltip: label.to_owned(),
        })
        .collect()
}

fn palette_run(run: &PaletteRun, window: AnyWindowHandle, cx: &mut App) -> Answered {
    if run.args.is_some() {
        return Err((
            BadParams,
            "palette.run takes no --args: no palette entry has arguments".to_owned(),
        ));
    }
    with_desk(window, cx, |desk, window, cx| {
        if !desk
            .palette_entries(cx)
            .iter()
            .any(|(item, _)| run.id == *item.id)
        {
            return Err((
                UnknownCommand,
                format!(
                    "the palette has no entry {}; tofu-desk palette.list names them",
                    run.id
                ),
            ));
        }
        desk.pick(&run.id, window, cx)
            .map_err(|unpicked| match unpicked {
                Unpicked::Off(why) => (DeniedByConsent, why),
                Unpicked::Failed(why) => (Refused, why),
            })?;
        Ok(value!({"ran": run.id, "screen": desk.screen()}))
    })
}

fn theme_set(set: &ThemeSet, cx: &mut App) -> Answered {
    let named = set
        .name
        .as_deref()
        .and_then(|name| Mode::ALL.iter().copied().find(|mode| mode.label() == name));
    let (file, mode) = match (named, set.mode) {
        (Some(_), Some(_)) => {
            return Err((
                BadParams,
                "theme.set takes the mode once, as the name or as --mode".to_owned(),
            ));
        }
        (Some(mode), None) => (None, Some(mode)),
        (None, mode) => (set.name.clone(), mode.map(local)),
    };
    let current = pick::chosen().map_err(|error| (Refused, error))?.theme;
    let theme = match file {
        Some(file) if file != current => {
            let single = matches!(
                Path::new(&file).components().collect::<Vec<_>>()[..],
                [Component::Normal(_)]
            );
            if !single || !pick::folder().join(format!("{file}.json")).is_file() {
                return Err((
                    NotFound,
                    format!(
                        "no theme {file} in {}; the names are light, dark or a theme file there",
                        pick::folder().display()
                    ),
                ));
            }
            pick::start(&file, cx).map_err(|error| (NotFound, error))?;
            file
        }
        _ => current,
    };
    pick::set_mode(&theme, mode, cx).map_err(|error| (NotFound, error))?;
    #[cfg(feature = "screen-theme")]
    pick::save(&theme, ActiveTheme::mode(cx)).map_err(|error| (Refused, error))?;
    themed(theme, cx)
}

fn themed(theme: String, cx: &App) -> Answered {
    json(&Theme {
        theme,
        mode: match ActiveTheme::mode(cx) {
            Mode::Light => shapes::Mode::Light,
            Mode::Dark => shapes::Mode::Dark,
        },
    })
}

fn local(mode: shapes::Mode) -> Mode {
    match mode {
        shapes::Mode::Light => Mode::Light,
        shapes::Mode::Dark => Mode::Dark,
    }
}

fn consented() -> Result<(), Refusal> {
    let denied = |why: String| (DeniedByConsent, why);
    let path = Home::find()
        .map_err(|error| denied(format!("the desk settings have no home: {error}")))?
        .config
        .join(SETTINGS_FILE);
    let on = match std::fs::read_to_string(&path) {
        Ok(text) => {
            serde_json::from_str::<Value>(&text)
                .map_err(|error| denied(format!("{}: {error}", path.display())))?
                .get(CLIPBOARD_KEY)
                == Some(&Value::Bool(true))
        }
        Err(error) if error.kind() == ErrorKind::NotFound => false,
        Err(error) => return Err(denied(format!("{}: {error}", path.display()))),
    };
    match on {
        true => Ok(()),
        false => Err(denied(format!(
            "the clipboard is off for control calls: set {CLIPBOARD_KEY} to true in {}",
            path.display()
        ))),
    }
}
