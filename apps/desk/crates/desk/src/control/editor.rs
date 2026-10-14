use desk_control::{Call, ErrorCode};
use gpui::{AnyWindowHandle, App};

use super::change::Answered;

#[cfg(not(feature = "module-editor"))]
pub fn answer(_: &Call, _: AnyWindowHandle, _: &mut App) -> Answered {
    Err((
        ErrorCode::Refused,
        "this desk was built without the editor: rebuild with --features module-editor".to_owned(),
    ))
}

#[cfg(feature = "module-editor")]
pub fn answer(call: &Call, window: AnyWindowHandle, cx: &mut App) -> Answered {
    use desk_control::params::{FileOpen, MaybePath};
    use desk_control::replies::{FileOpened, Selection};
    use desk_control::shapes::{Position, Range};
    use desk_core::buffer::Buffer;
    use desk_ui::components::tree::OpenProject;
    use std::path::Path;

    use super::change::json;
    use crate::desk::Desk;

    let at = |buffer: &Buffer, char: usize| -> Option<Position> {
        let line = buffer.char_to_line(char).ok()?;
        let start = buffer.line_to_char(line).ok()?;
        Some(Position {
            l: u32::try_from(line.checked_add(1)?).ok()?,
            c: u32::try_from(char.checked_sub(start)?.checked_add(1)?).ok()?,
        })
    };
    let desk = window.downcast::<Desk>().ok_or((
        ErrorCode::DeskNotRunning,
        "the desk window's root view is not the desk".to_owned(),
    ))?;
    let gone = |error: String| {
        (
            ErrorCode::DeskNotRunning,
            format!("the desk window is gone: {error}"),
        )
    };
    match call {
        Call::FileOpen(FileOpen {
            path,
            line,
            col,
            tile,
            new_tab,
        }) => {
            if tile.is_some() || *new_tab {
                return Err((
                    ErrorCode::BadParams,
                    "file.open opens in the editor screen; --tile and --new-tab are not built yet"
                        .to_owned(),
                ));
            }
            let (path, spot) = spot(path);
            let line = line.or(spot.first().copied()).unwrap_or(1);
            let col = col.or(spot.get(1).copied()).unwrap_or(1);
            let from_one = |at: u32| usize::try_from(at.checked_sub(1)?).ok();
            let (Some(row), Some(column)) = (from_one(line), from_one(col)) else {
                return Err((ErrorCode::BadParams, "line and col count from 1".to_owned()));
            };
            let root = match cx.try_global::<OpenProject>() {
                Some(OpenProject(folder)) => folder.clone(),
                None => std::env::current_dir().map_err(|error| {
                    (
                        ErrorCode::NotFound,
                        format!("the desk has no project folder: {error}"),
                    )
                })?,
            };
            let full = std::path::absolute(root.join(path))
                .map_err(|error| (ErrorCode::BadParams, format!("{path}: {error}")))?;
            let missing = |error: std::io::Error| {
                (ErrorCode::NotFound, format!("{}: {error}", full.display()))
            };
            let inside = full
                .canonicalize()
                .map_err(missing)?
                .starts_with(root.canonicalize().map_err(missing)?);
            if !inside {
                return Err((
                    ErrorCode::DeniedByConsent,
                    format!(
                        "{} is outside the project {}",
                        full.display(),
                        root.display()
                    ),
                ));
            }
            let size = std::fs::metadata(&full).map_err(missing)?.len();
            let (caret, dirty) = desk
                .update(cx, |desk, window, cx| {
                    desk.open_screen("editor", window, cx)?;
                    let editor = desk
                        .editor()
                        .ok_or("the editor screen holds no editor".to_owned())?;
                    editor.update(cx, |editor, cx| {
                        editor.land(&full, row, column, window, cx);
                        let (_, code) = editor
                            .active_code()
                            .filter(|(open, _)| *open == full.as_path())
                            .ok_or(format!("the editor did not open {}", full.display()))?;
                        let code = code.read(cx);
                        let caret = code
                            .carets()
                            .next()
                            .and_then(|(_, head)| at(code.buffer(), head))
                            .ok_or("the editor has no caret".to_owned())?;
                        Ok((caret, code.buffer().is_dirty()))
                    })
                })
                .map_err(|error| gone(format!("{error:#}")))?
                .map_err(|error: String| (ErrorCode::Refused, error))?;
            json(&FileOpened {
                tile: 0,
                tab: 0,
                path: full.display().to_string(),
                line: caret.l,
                col: caret.c,
                size,
                dirty,
            })
        }
        Call::EditorSelectionGet(MaybePath { path }) => {
            let (open, ranges, text) = desk
                .update(cx, |desk, _, cx| {
                    let editor = desk.editor().ok_or((
                        ErrorCode::NotFound,
                        "the editor screen is not open".to_owned(),
                    ))?;
                    let editor = editor.read(cx);
                    let (open, code) = editor.active_code().ok_or((
                        ErrorCode::NotFound,
                        "the editor has no file open".to_owned(),
                    ))?;
                    if let Some(wanted) = path.as_deref()
                        && !open.ends_with(Path::new(wanted))
                    {
                        return Err((
                            ErrorCode::NotFound,
                            format!(
                                "{wanted} is not the file the editor shows, {}",
                                open.display()
                            ),
                        ));
                    }
                    let code = code.read(cx);
                    let buffer = code.buffer();
                    let mut ranges = Vec::new();
                    let mut text = Vec::new();
                    for (anchor, head) in code.carets() {
                        let (start, end) = (anchor.min(head), anchor.max(head));
                        ranges.push(Range {
                            start: at(buffer, start).ok_or((
                                ErrorCode::Refused,
                                format!("caret {start} is past the end of the file"),
                            ))?,
                            end: at(buffer, end).ok_or((
                                ErrorCode::Refused,
                                format!("caret {end} is past the end of the file"),
                            ))?,
                        });
                        text.extend(buffer.rope().get_slice(start..end).map(String::from));
                    }
                    Ok((open.to_path_buf(), ranges, text.join("\n")))
                })
                .map_err(|error| gone(format!("{error:#}")))??;
            json(&Selection {
                path: open.display().to_string(),
                ranges,
                text,
            })
        }
        _ => Err((
            ErrorCode::Refused,
            "the editor answers file.open and editor.selection.get only".to_owned(),
        )),
    }
}

#[cfg(feature = "module-editor")]
fn spot(path: &str) -> (&str, Vec<u32>) {
    let mut head = path;
    let mut numbers = Vec::new();
    while numbers.len() < 2 {
        let Some((before, number)) = head
            .rsplit_once(':')
            .and_then(|(before, tail)| Some((before, tail.parse().ok()?)))
        else {
            break;
        };
        numbers.insert(0, number);
        head = before;
    }
    (head, numbers)
}
