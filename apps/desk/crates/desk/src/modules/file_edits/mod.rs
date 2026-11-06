use super::chat::{Trace, mention};
use super::replayed::{self, Replayed};

use std::path::{Component, Path, PathBuf};
use std::rc::Rc;

use desk_core::model::{Session, Store};
use desk_core::protocol::{FileEdit as Edit, FileEditOp, HunkLineKind};
use desk_ui::components::diff::{FileChange, FileDiff};
use desk_ui::components::file_edits::{EditedFile, FileEdit, RowPick, file_edits, file_history};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::sheet::Drawer;
use desk_ui::components::tree::project_folder;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, Context, Entity, IntoElement, Render, SharedString, Subscription,
    Window, div, prelude::*,
};

const BOARD: &str = "36-agents";
const TILE_WIDTH: f32 = 513.0;
#[cfg(feature = "module-editor")]
const EDITOR_SCREEN: &str = "editor";

pub struct FileEdits {
    store: Entity<Store>,
    files: Vec<EditedFile>,
    mentions: Vec<Option<Trace>>,
    opened: usize,
    drawer: bool,
    _watch: Subscription,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if board.is_some_and(|board| board != BOARD) {
        return Err(format!(
            "the file edits module replays {BOARD}, not {board:?}"
        ));
    }
    replayed::fonts(cx)?;
    let store = cx.new(|_| Store::default());
    let module = mount(store.clone(), cx);
    let counted = module.clone();
    Replayed::open(
        store,
        module.into(),
        (Glyph::File, "File edits", TILE_WIDTH),
        move |cx| {
            let line = counted.update(cx, |module, cx| module.counted(cx));
            eprintln!(
                "desk: file edits from the store: {line}; cassette: {} file.edit events",
                cassette_edits()
            );
        },
        cx,
    )
}

pub fn mount(store: Entity<Store>, cx: &mut App) -> Entity<FileEdits> {
    cx.new(|cx: &mut Context<FileEdits>| FileEdits {
        _watch: cx.observe(&store, |module, _, cx| {
            module.rebuild(cx);
            eprintln!("desk: tile file edits rebuilt {} files", module.files.len());
            cx.notify();
        }),
        store,
        files: Vec::new(),
        mentions: Vec::new(),
        opened: 0,
        drawer: false,
    })
}

fn patch(edit: &Edit) -> String {
    let mut text = String::new();
    for hunk in &edit.hunks {
        let without = |skip: HunkLineKind| hunk.lines.iter().filter(|l| l.kind != skip).count();
        text += &format!(
            "@@ -{},{} +{},{} @@\n",
            hunk.old_start,
            without(HunkLineKind::Added),
            hunk.new_start,
            without(HunkLineKind::Removed)
        );
        for line in &hunk.lines {
            let sign = match line.kind {
                HunkLineKind::Added => '+',
                HunkLineKind::Removed => '-',
                HunkLineKind::Context | HunkLineKind::Unknown(_) => ' ',
            };
            text += &format!("{sign}{}\n", line.text);
        }
    }
    text
}

fn first_changed_line(edit: &Edit) -> usize {
    edit.hunks.first().map_or(1, |hunk| {
        let offset = hunk
            .lines
            .iter()
            .filter(|line| line.kind != HunkLineKind::Removed)
            .position(|line| line.kind == HunkLineKind::Added)
            .unwrap_or_default();
        usize::try_from(hunk.new_start)
            .unwrap_or_default()
            .max(1)
            .saturating_add(offset)
    })
}

fn shown(session: &Session, edit: &Edit) -> Result<FileEdit, String> {
    let change = match &edit.op {
        FileEditOp::Create => FileChange::Added,
        FileEditOp::Modify => FileChange::Modified,
        FileEditOp::Unknown(op) => return Err(format!("{}: unknown op {op}", edit.path)),
    };
    let diff = FileDiff::parse(edit.path.clone(), change, &patch(edit), |_| Vec::new())
        .map_err(|error| format!("{}: {error:?}", edit.path))?;
    let agent = edit.agent.as_ref().map(|id| {
        session.agents.get(id).map_or_else(
            || SharedString::from(id.clone()),
            |agent| format!("{} {}", agent.kind, agent.number).into(),
        )
    });
    Ok(FileEdit {
        agent,
        at: shown_at(session, edit).into(),
        line: first_changed_line(edit),
        diff: Rc::new(diff),
    })
}

fn shown_at(session: &Session, edit: &Edit) -> String {
    session
        .tools
        .get(&edit.item)
        .and_then(|tool| tool.started_at.as_deref()?.get(11..19))
        .map_or_else(|| format!("seq {}", edit.seq), str::to_owned)
}

fn files(session: &Session) -> Vec<(EditedFile, Option<Trace>)> {
    let mut files: Vec<(i64, EditedFile, Option<Trace>)> = session
        .files
        .iter()
        .filter(|(path, _)| !path.is_empty())
        .map(|(path, edits)| {
            let shown: Vec<FileEdit> = edits
                .iter()
                .filter_map(|edit| {
                    shown(session, edit)
                        .map_err(|error| eprintln!("desk: file edits: {error}"))
                        .ok()
                })
                .collect();
            let mut by: Vec<SharedString> = Vec::new();
            for agent in shown.iter().rev().filter_map(|edit| edit.agent.clone()) {
                if !by.contains(&agent) {
                    by.push(agent);
                }
            }
            let newest = edits.iter().map(|edit| edit.seq).max().unwrap_or_default();
            let mention = edits
                .iter()
                .filter(|edit| edit.r#ref.is_some())
                .max_by_key(|edit| edit.seq)
                .and_then(|edit| {
                    Some(Trace {
                        token: edit.r#ref.clone()?,
                        glyph: Glyph::File,
                        label: path.clone(),
                        detail: format!("edited at {}", shown_at(session, edit)),
                    })
                });
            let file = EditedFile {
                path: path.clone().into(),
                edits: shown,
                by,
            };
            (newest, file, mention)
        })
        .collect();
    files.sort_by_key(|(newest, ..)| std::cmp::Reverse(*newest));
    files
        .into_iter()
        .map(|(_, file, mention)| (file, mention))
        .collect()
}

fn on_disk(path: &str, cx: &App) -> Result<PathBuf, String> {
    let path = Path::new(path);
    if path.is_absolute() {
        return Ok(path.to_path_buf());
    }
    if !path
        .components()
        .all(|part| matches!(part, Component::Normal(_)))
    {
        return Err(format!("{} leaves the project", path.display()));
    }
    let root = match project_folder(cx) {
        Some(root) => root.to_path_buf(),
        None => std::env::current_dir()
            .map_err(|error| format!("no project folder for {}: {error}", path.display()))?,
    };
    Ok(root.join(path))
}

#[cfg(feature = "module-editor")]
fn open_in_editor(path: &str, line: usize, window: &mut Window, cx: &mut App) {
    let full = match on_disk(path, cx) {
        Ok(full) => full,
        Err(error) => return eprintln!("desk: file edits cannot open the editor: {error}"),
    };
    let Some(Some(desk)) = window.root::<crate::desk::Desk>() else {
        return eprintln!("desk: file edits is not inside the desk, so it cannot open the editor");
    };
    eprintln!("desk: file edits opens {} at line {line}", full.display());
    window.defer(cx, move |window, cx| {
        let opened = desk.update(cx, |desk, cx| {
            desk.open_screen(EDITOR_SCREEN, window, cx)?;
            let editor = desk
                .editor()
                .ok_or_else(|| "the editor screen holds no editor".to_owned())?;
            editor.update(cx, |editor, cx| {
                editor.land(&full, line.saturating_sub(1), 0, window, cx);
            });
            Ok::<(), String>(())
        });
        if let Err(error) = opened {
            eprintln!("desk: file edits could not open the editor: {error}");
        }
    });
}

#[cfg(not(feature = "module-editor"))]
fn open_in_editor(path: &str, line: usize, _: &mut Window, cx: &mut App) {
    match on_disk(path, cx) {
        Ok(full) => eprintln!(
            "desk: {}:{line} needs the editor: rebuild with --features module-editor",
            full.display()
        ),
        Err(error) => eprintln!("desk: file edits cannot open the editor: {error}"),
    }
}

impl FileEdits {
    pub fn count(&self) -> usize {
        self.files.len()
    }

    pub fn reveal(&mut self, path: &str, cx: &mut Context<Self>) -> bool {
        let Some(at) = self.files.iter().position(|file| file.path == path) else {
            return false;
        };
        self.opened = at;
        self.drawer = true;
        cx.notify();
        true
    }

    fn rebuild(&mut self, cx: &mut Context<Self>) {
        (self.files, self.mentions) = self
            .store
            .read(cx)
            .open_session()
            .map_or_else(Vec::new, files)
            .into_iter()
            .unzip();
    }

    pub fn counted(&mut self, cx: &mut Context<Self>) -> String {
        self.rebuild(cx);
        let stored: usize = self
            .store
            .read(cx)
            .open_session()
            .map_or(0, |s| s.files.values().map(Vec::len).sum());
        let shown: usize = self.files.iter().map(|file| file.edits.len()).sum();
        format!("{} files, {stored} edits, {shown} shown", self.files.len())
    }
}

fn cassette_edits() -> usize {
    include_str!("../../../../../cassettes/36-agents.cassette")
        .lines()
        .filter(|line| line.contains("\"method\": \"file.edit\""))
        .count()
}

impl Render for FileEdits {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let (opener, closer) = (cx.weak_entity(), cx.weak_entity());
        let picked: Vec<(SharedString, usize)> = self
            .files
            .iter()
            .map(|file| {
                let line = file.edits.last().map_or(1, |edit| edit.line);
                (file.path.clone(), line)
            })
            .collect();
        let pick = move |at: usize, pick: RowPick, window: &mut Window, cx: &mut App| match pick {
            RowPick::Editor => {
                if let Some((path, line)) = picked.get(at) {
                    open_in_editor(path, *line, window, cx);
                }
            }
            RowPick::Detail => {
                opener
                    .update(cx, |module, cx| {
                        module.opened = at;
                        module.drawer = true;
                        cx.notify();
                    })
                    .ok();
            }
        };
        let trace = self.mentions.get(self.opened).cloned().flatten();
        let history = self.files.get(self.opened).map(|file| {
            let path = file.path.clone();
            let lines: Vec<usize> = file.edits.iter().map(|edit| edit.line).collect();
            let on_mention = move |window: &mut Window, cx: &mut App| match &trace {
                Some(trace) => mention(trace.clone(), window, cx),
                None => eprintln!("desk: file edits has no mention token for this file"),
            };
            let on_open = move |ix: usize, window: &mut Window, cx: &mut App| {
                if let Some(line) = lines.get(ix) {
                    open_in_editor(&path, *line, window, cx);
                }
            };
            file_history(
                ("file-history", self.opened),
                file,
                &theme,
                on_mention,
                on_open,
            )
        });
        div()
            .relative()
            .size_full()
            .child(file_edits("file-edits", &self.files, &theme, pick))
            .child(
                Drawer::new("file-edits-drawer")
                    .open(self.drawer && history.is_some())
                    .on_close(move |_, cx| {
                        closer
                            .update(cx, |module, cx| {
                                module.drawer = false;
                                cx.notify();
                            })
                            .ok();
                    })
                    .child(
                        div()
                            .id("file-history-scroll")
                            .size_full()
                            .overflow_y_scroll()
                            .children(history),
                    ),
            )
    }
}
