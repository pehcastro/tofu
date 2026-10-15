use desk_control::{Call, ErrorCode};
use gpui::{AnyWindowHandle, App};

use super::change::Answered;

#[cfg(feature = "module-editor")]
pub use enabled::answer;

#[cfg(not(feature = "module-editor"))]
pub fn answer(_: &Call, _: AnyWindowHandle, _: &mut App) -> Answered {
    Err((
        ErrorCode::Refused,
        "this desk was built without the editor: rebuild with --features module-editor".to_owned(),
    ))
}

#[cfg(feature = "module-editor")]
mod enabled {
    use std::path::{Path, PathBuf};

    use desk_control::params::{
        FileClose, FileOpen, Find, MaybePath, PathRef, SelectionSet, TabClose, TabScope, TabStep,
        TextReplace,
    };
    use desk_control::replies::{
        Closed, Edited, FileOpened, FileSaved, FindHit, ListedFile, OpenFile, Ranges, Reopened,
        Selection, Tab, TabStepped, TileRef,
    };
    use desk_control::shapes::{self, Position, Range, Step};
    use desk_core::buffer::Buffer;
    use desk_ui::components::code_editor::CodeEditor;
    use desk_ui::components::tree::OpenProject;
    use gpui::{Context, Entity, Window, WindowHandle};

    use super::{Answered, AnyWindowHandle, App, Call, ErrorCode};
    use crate::control::change::json;
    use crate::control::dispatch::count;
    use crate::desk::Desk;
    use crate::modules::editor::{Editor, OpenTab, language};

    type Refused = (ErrorCode, String);

    const FIND_MOST: usize = 200;
    const FNV_OFFSET: u64 = 0xcbf2_9ce4_8422_2325;
    const FNV_PRIME: u64 = 0x0100_0000_01b3;

    pub fn answer(call: &Call, window: AnyWindowHandle, cx: &mut App) -> Answered {
        let desk = window.downcast::<Desk>().ok_or((
            ErrorCode::DeskNotRunning,
            "the desk window's root view is not the desk".to_owned(),
        ))?;
        match call {
            Call::FileOpen(open) => file_open(open, desk, cx),
            Call::EditorSelectionGet(MaybePath { path }) => {
                let selection = on_editor(desk, cx, |editor, _, cx| {
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
                    let (ranges, text) = selected(code.read(cx))?;
                    Ok(Selection {
                        path: open.display().to_string(),
                        ranges,
                        text,
                    })
                })?;
                json(&selection)
            }
            Call::EditorSelectionSet(SelectionSet { path, range }) => {
                let full = inside_project(path, cx)?;
                let ranges = on_editor(desk, cx, |editor, window, cx| {
                    let (Some(row), Some(col)) = (from_one(range.start.l), from_one(range.start.c))
                    else {
                        return Err((ErrorCode::BadParams, "line and col count from 1".to_owned()));
                    };
                    editor.land(&full, row, col, window, cx);
                    let code = code_shown(editor, &full)?;
                    let chars = chars_of(code.read(cx).buffer(), range)?;
                    code.update(cx, |code, cx| code.select(&[chars], cx));
                    Ok(selected(code.read(cx))?.0)
                })?;
                json(&Ranges { ranges })
            }
            Call::EditorTextReplace(TextReplace {
                path,
                range,
                text,
                expect_hash,
            }) => {
                let full = inside_project(path, cx)?;
                let edited = on_editor(desk, cx, |editor, window, cx| {
                    let open = editor
                        .open_tabs(cx)
                        .into_iter()
                        .find(|tab| tab.path == full)
                        .and_then(|tab| tab.code);
                    let code = match open {
                        Some(code) => code,
                        None => {
                            editor.land(&full, 0, 0, window, cx);
                            code_shown(editor, &full)?
                        }
                    };
                    let before = fnv(code.read(cx).buffer().text().as_bytes());
                    if let Some(expected) = expect_hash
                        && *expected != hash(before)
                    {
                        return Err((
                            ErrorCode::Conflict,
                            format!("{} is at {}, not {expected}", full.display(), hash(before)),
                        ));
                    }
                    let chars = chars_of(code.read(cx).buffer(), range)?;
                    code.update(cx, |code, cx| code.edit(chars, text, cx))
                        .map_err(|error| (ErrorCode::Refused, error))?;
                    Ok(Edited {
                        rev: fnv(code.read(cx).buffer().text().as_bytes()),
                        undo: hash(before),
                    })
                })?;
                json(&edited)
            }
            Call::EditorFind(Find {
                pattern,
                path,
                regex,
            }) => {
                if *regex {
                    return Err((
                        ErrorCode::BadParams,
                        "editor.find --regex is not built: the desk carries no regex engine"
                            .to_owned(),
                    ));
                }
                if pattern.is_empty() {
                    return Err((
                        ErrorCode::BadParams,
                        "editor.find needs a pattern".to_owned(),
                    ));
                }
                let only = path
                    .as_deref()
                    .map(|path| inside_project(path, cx))
                    .transpose()?;
                let hits = on_editor(desk, cx, |editor, _, cx| {
                    Ok(editor.find_text(pattern, only.as_deref(), FIND_MOST + 1, cx))
                })?;
                if hits.len() > FIND_MOST {
                    return Err((
                        ErrorCode::OverLimit,
                        format!(
                            "{pattern} is found in more than {FIND_MOST} places; narrow it with --path"
                        ),
                    ));
                }
                json(
                    &hits
                        .into_iter()
                        .map(|hit| FindHit {
                            path: hit.path.display().to_string(),
                            line: count(hit.line.saturating_add(1)),
                            col: count(hit.col.saturating_add(1)),
                            text: hit.text,
                        })
                        .collect::<Vec<_>>(),
                )
            }
            Call::FileSave(MaybePath { path }) => {
                let wanted = path
                    .as_deref()
                    .map(|path| full_path(path, cx))
                    .transpose()?;
                let saved = on_editor(desk, cx, |editor, _, cx| {
                    let focused = editor.focused();
                    let tab = editor
                        .open_tabs(cx)
                        .into_iter()
                        .find(|tab| match &wanted {
                            Some(full) => tab.path == *full,
                            None => (tab.tile, tab.index) == focused,
                        })
                        .ok_or_else(|| not_open(wanted.as_deref()))?;
                    let shown = tab.path.display().to_string();
                    if tab.dirty && tab.stale {
                        return Err((
                            ErrorCode::Conflict,
                            format!("{shown} changed on disk under unsaved edits"),
                        ));
                    }
                    let code = tab.code.as_ref().ok_or((
                        ErrorCode::Refused,
                        format!("{shown} did not open in the editor"),
                    ))?;
                    let text = code.read(cx).buffer().text();
                    code.update(cx, |code, cx| code.save(&tab.path, cx))
                        .map_err(|error| (ErrorCode::Refused, format!("{shown}: {error}")))?;
                    eprintln!(
                        "desk: editor saved {shown} {} lines",
                        code.read(cx).buffer().line_count()
                    );
                    Ok(FileSaved {
                        path: shown,
                        bytes: u64::try_from(text.len()).unwrap_or(u64::MAX),
                        hash: hash(fnv(text.as_bytes())),
                    })
                })?;
                json(&saved)
            }
            Call::FileClose(FileClose { path, discard }) => {
                let full = full_path(path, cx)?;
                on_editor(desk, cx, |editor, _, cx| {
                    let tab = editor
                        .open_tabs(cx)
                        .into_iter()
                        .find(|tab| tab.path == full)
                        .ok_or_else(|| not_open(Some(&full)))?;
                    close(editor, &tab, *discard, cx)
                })?;
                json(&Closed {
                    closed: true,
                    undo: None,
                })
            }
            Call::FileList(_) => {
                let tabs = on_editor(desk, cx, |editor, _, cx| Ok(editor.open_tabs(cx)))?;
                json(
                    &tabs
                        .iter()
                        .map(|tab| ListedFile {
                            path: tab.path.display().to_string(),
                            dirty: tab.dirty,
                            tile: tab.tile,
                            tab: count(tab.index),
                        })
                        .collect::<Vec<_>>(),
                )
            }
            Call::FileListOpen(_) => {
                let tabs = on_editor(desk, cx, |editor, _, cx| Ok(editor.open_tabs(cx)))?;
                let listed: Vec<OpenFile> = tabs
                    .iter()
                    .map(|tab| {
                        let code = tab.code.as_ref().map(|code| code.read(cx));
                        let caret = code.and_then(|code| {
                            let (_, head) = code.carets().next()?;
                            position(code.buffer(), head)
                        });
                        OpenFile {
                            path: tab.path.display().to_string(),
                            tile: tab.tile,
                            dirty: tab.dirty,
                            line: caret.as_ref().map_or(1, |caret| caret.l),
                            col: caret.as_ref().map_or(1, |caret| caret.c),
                            language: code.map_or_else(|| "none".to_owned(), language),
                        }
                    })
                    .collect();
                json(&listed)
            }
            Call::FileReveal(PathRef { path }) => {
                let full = inside_project(path, cx)?;
                let tile = on_editor(desk, cx, |editor, _, cx| {
                    if !editor.reveal_in_tree(&full, cx) {
                        return Err((
                            ErrorCode::NotFound,
                            format!("{} is outside the editor's folder", full.display()),
                        ));
                    }
                    Ok(editor.focused().0)
                })?;
                json(&TileRef { tile })
            }
            Call::TabList(TabScope { tile }) => {
                let tabs = on_editor(desk, cx, |editor, _, cx| Ok(editor.open_tabs(cx)))?;
                if let Some(wanted) = tile
                    && *wanted != 0
                    && !tabs.iter().any(|tab| tab.tile == *wanted)
                {
                    return Err((
                        ErrorCode::NoSuchTile,
                        format!("the editor has no pane {wanted}"),
                    ));
                }
                json(
                    &tabs
                        .iter()
                        .filter(|tab| tile.is_none_or(|wanted| wanted == tab.tile))
                        .map(listed_tab)
                        .collect::<Vec<_>>(),
                )
            }
            Call::TabStep(TabStep { step }) => {
                let (tile, active) = on_editor(desk, cx, |editor, window, cx| {
                    editor.step_page(matches!(step, Step::Next), window, cx);
                    Ok(editor.focused())
                })?;
                json(&TabStepped {
                    tile,
                    active: count(active),
                })
            }
            Call::TabClose(TabClose { tile, index }) => {
                on_editor(desk, cx, |editor, _, cx| {
                    let (focused, active) = editor.focused();
                    let tile = tile.unwrap_or(focused);
                    let index = match index {
                        Some(index) => usize::try_from(*index).ok(),
                        None => Some(active),
                    };
                    let tab = editor
                        .open_tabs(cx)
                        .into_iter()
                        .find(|tab| tab.tile == tile && Some(tab.index) == index)
                        .ok_or((
                            ErrorCode::NoSuchTile,
                            format!("the editor has no tab {index:?} in pane {tile}"),
                        ))?;
                    close(editor, &tab, false, cx)
                })?;
                json(&Closed {
                    closed: true,
                    undo: None,
                })
            }
            Call::TabReopen(_) => {
                let tab = on_editor(desk, cx, |editor, window, cx| {
                    let path = editor.reopen_page(window, cx).ok_or((
                        ErrorCode::NothingClosed,
                        "the editor has closed no file".to_owned(),
                    ))?;
                    editor
                        .open_tabs(cx)
                        .into_iter()
                        .find(|tab| tab.path == path)
                        .ok_or_else(|| not_open(Some(&path)))
                })?;
                json(&Reopened {
                    tab: listed_tab(&tab),
                })
            }
            _ => Err((
                ErrorCode::Refused,
                "the editor does not answer this verb".to_owned(),
            )),
        }
    }

    fn file_open(open: &FileOpen, desk: WindowHandle<Desk>, cx: &mut App) -> Answered {
        let FileOpen {
            path,
            line,
            col,
            tile,
            new_tab,
        } = open;
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
        let (Some(row), Some(column)) = (from_one(line), from_one(col)) else {
            return Err((ErrorCode::BadParams, "line and col count from 1".to_owned()));
        };
        let full = inside_project(path, cx)?;
        let size = std::fs::metadata(&full)
            .map_err(|error| (ErrorCode::NotFound, format!("{}: {error}", full.display())))?
            .len();
        let (caret, dirty) = desk
            .update(cx, |desk, window, cx| {
                desk.open_screen("editor", window, cx)
                    .map_err(|error| (ErrorCode::Refused, error))?;
                let editor = desk.editor().ok_or((
                    ErrorCode::Refused,
                    "the editor screen holds no editor".to_owned(),
                ))?;
                editor.update(cx, |editor, cx| {
                    editor.land(&full, row, column, window, cx);
                    let caret = caret_of(editor, &full, cx)?;
                    let dirty = editor
                        .active_code()
                        .is_some_and(|(_, code)| code.read(cx).buffer().is_dirty());
                    Ok::<_, Refused>((caret, dirty))
                })
            })
            .map_err(gone)??;
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

    fn on_editor<T>(
        desk: WindowHandle<Desk>,
        cx: &mut App,
        act: impl FnOnce(&mut Editor, &mut Window, &mut Context<Editor>) -> Result<T, Refused>,
    ) -> Result<T, Refused> {
        desk.update(cx, |desk, window, cx| {
            let editor = desk.editor().ok_or((
                ErrorCode::NotFound,
                "the editor screen is not open".to_owned(),
            ))?;
            editor.update(cx, |editor, cx| act(editor, window, cx))
        })
        .map_err(gone)?
    }

    fn gone(error: impl std::fmt::Display) -> Refused {
        (
            ErrorCode::DeskNotRunning,
            format!("the desk window is gone: {error:#}"),
        )
    }

    fn close(
        editor: &mut Editor,
        tab: &OpenTab,
        discard: bool,
        cx: &mut Context<Editor>,
    ) -> Result<(), Refused> {
        if tab.dirty && !discard {
            return Err((
                ErrorCode::NeedsApproval,
                format!(
                    "{} has unsaved edits: save it first, or close it with file.close --discard",
                    tab.path.display()
                ),
            ));
        }
        editor.close_page(&tab.path, cx);
        Ok(())
    }

    fn caret_of(editor: &Editor, full: &Path, cx: &App) -> Result<Position, Refused> {
        let code = code_shown(editor, full)?;
        let code = code.read(cx);
        code.carets()
            .next()
            .and_then(|(_, head)| position(code.buffer(), head))
            .ok_or((ErrorCode::Refused, "the editor has no caret".to_owned()))
    }

    fn code_shown(editor: &Editor, full: &Path) -> Result<Entity<CodeEditor>, Refused> {
        editor
            .active_code()
            .filter(|(open, _)| *open == full)
            .map(|(_, code)| code.clone())
            .ok_or_else(|| {
                (
                    ErrorCode::Refused,
                    format!("the editor did not open {}", full.display()),
                )
            })
    }

    fn selected(code: &CodeEditor) -> Result<(Vec<Range>, String), Refused> {
        let buffer = code.buffer();
        let past = |at: usize| {
            (
                ErrorCode::Refused,
                format!("caret {at} is past the end of the file"),
            )
        };
        let mut ranges = Vec::new();
        let mut text = Vec::new();
        for (anchor, head) in code.carets() {
            let (start, end) = (anchor.min(head), anchor.max(head));
            ranges.push(Range {
                start: position(buffer, start).ok_or_else(|| past(start))?,
                end: position(buffer, end).ok_or_else(|| past(end))?,
            });
            text.extend(buffer.rope().get_slice(start..end).map(String::from));
        }
        Ok((ranges, text.join("\n")))
    }

    fn chars_of(buffer: &Buffer, range: &Range) -> Result<std::ops::Range<usize>, Refused> {
        let char_at = |at: &Position| {
            let line = from_one(at.l)?;
            let col = from_one(at.c)?;
            let width = buffer.line(line)?.chars().count();
            (col <= width).then_some(())?;
            buffer.line_to_char(line).ok()?.checked_add(col)
        };
        let outside = |at: &Position| {
            (
                ErrorCode::BadParams,
                format!(
                    "{}:{} is not in the file; lines and columns count from 1",
                    at.l, at.c
                ),
            )
        };
        let start = char_at(&range.start).ok_or_else(|| outside(&range.start))?;
        let end = char_at(&range.end).ok_or_else(|| outside(&range.end))?;
        if start > end {
            return Err((
                ErrorCode::BadParams,
                "a range ends before it starts".to_owned(),
            ));
        }
        Ok(start..end)
    }

    fn listed_tab(tab: &OpenTab) -> Tab {
        Tab {
            tile: tab.tile,
            index: count(tab.index),
            module: shapes::Module::Editor,
            title: tab
                .path
                .file_name()
                .map(|name| name.to_string_lossy().into_owned())
                .unwrap_or_default(),
            active: tab.active,
            dirty: tab.dirty,
        }
    }

    fn not_open(path: Option<&Path>) -> Refused {
        (
            ErrorCode::NotFound,
            match path {
                Some(path) => format!("{} is not open in the editor", path.display()),
                None => "the editor has no file open".to_owned(),
            },
        )
    }

    fn full_path(path: &str, cx: &App) -> Result<PathBuf, Refused> {
        let root = match cx.try_global::<OpenProject>() {
            Some(OpenProject(folder)) => folder.clone(),
            None => std::env::current_dir().map_err(|error| {
                (
                    ErrorCode::NotFound,
                    format!("the desk has no project folder: {error}"),
                )
            })?,
        };
        std::path::absolute(root.join(path))
            .map_err(|error| (ErrorCode::BadParams, format!("{path}: {error}")))
    }

    fn inside_project(path: &str, cx: &App) -> Result<PathBuf, Refused> {
        let full = full_path(path, cx)?;
        let root = full_path("", cx)?;
        let missing =
            |error: std::io::Error| (ErrorCode::NotFound, format!("{}: {error}", full.display()));
        if !full
            .canonicalize()
            .map_err(missing)?
            .starts_with(root.canonicalize().map_err(missing)?)
        {
            return Err((
                ErrorCode::DeniedByConsent,
                format!(
                    "{} is outside the project {}",
                    full.display(),
                    root.display()
                ),
            ));
        }
        Ok(full)
    }

    fn position(buffer: &Buffer, char: usize) -> Option<Position> {
        let line = buffer.char_to_line(char).ok()?;
        let start = buffer.line_to_char(line).ok()?;
        Some(Position {
            l: u32::try_from(line.checked_add(1)?).ok()?,
            c: u32::try_from(char.checked_sub(start)?.checked_add(1)?).ok()?,
        })
    }

    fn from_one(at: u32) -> Option<usize> {
        usize::try_from(at.checked_sub(1)?).ok()
    }

    fn fnv(bytes: &[u8]) -> u64 {
        bytes.iter().fold(FNV_OFFSET, |hash, byte| {
            (hash ^ u64::from(*byte)).wrapping_mul(FNV_PRIME)
        })
    }

    fn hash(fnv: u64) -> String {
        format!("fnv1a64:{fnv:016x}")
    }

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
}
