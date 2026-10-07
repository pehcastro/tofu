mod boards;
mod fixture;
mod kit;
mod parts;

use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use desk_core::buffer::Buffer;
use desk_core::git::{Against, Git, GitBinary, Hunk, Mark, State};
use desk_core::syntax::{Language, Syntax};
use desk_ui::components::chip::GitStatus;
use desk_ui::components::code::{GutterMark, LineMarks, Marks};
use desk_ui::components::code_editor::CodeEditor;
use desk_ui::components::tree::{FileTree, IconTheme, TreeEvent, TreeNode};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::ColorToken;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Entity, Focusable, Render, SharedString,
    Subscription, Task, Window, prelude::*,
};

use boards::{cursor_marks, notes_marks, store_marks};
use fixture::{COUNT_TEST, Line, NOTES, STORE};

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let scale = window.scale_factor();
    let source = Source::Fixture {
        notes: fixture_editor(NOTES, notes_marks(scale), cx),
        test: fixture_editor(COUNT_TEST, cursor_marks(), cx),
        store: fixture_editor(STORE, store_marks(), cx),
    };
    launch(board, source, window, cx)
}

pub fn open_file(
    path: &Path,
    board: Option<&str>,
    window: &mut Window,
    cx: &mut App,
) -> Result<AnyView, String> {
    let full = absolute(path);
    let git = repository(full.parent().unwrap_or(&full));
    let file = (file_editor(&full, cx), on_disk(full, git));
    launch(
        board,
        Source::Disk {
            file: Some(file),
            folder: None,
        },
        window,
        cx,
    )
}

pub fn open_dir(
    dir: &Path,
    file: Option<&Path>,
    board: Option<&str>,
    window: &mut Window,
    cx: &mut App,
) -> Result<AnyView, String> {
    let root = absolute(dir);
    if !root.is_dir() {
        return Err(format!("--dir {} is not a folder", root.display()));
    }
    let icons = IconTheme::material().map_err(|error| error.to_string())?;
    let git = repository(&root);
    let listing = Listing {
        states: git
            .as_deref()
            .map(|git| states(git, &root))
            .unwrap_or_default(),
        root,
    };
    let tree = FileTree::new("editor-files", listing.children(""), &icons).badges();
    let folder = Folder {
        tree: cx.new(|_| tree),
        listing,
        icons,
        git,
    };
    let file = file.map(absolute).map(|full| {
        let git = folder.git.clone();
        (file_editor(&full, cx), on_disk(full, git))
    });
    launch(
        board,
        Source::Disk {
            file,
            folder: Some(Box::new(folder)),
        },
        window,
        cx,
    )
}

type Opened = Result<Entity<CodeEditor>, SharedString>;

enum Source {
    Fixture {
        notes: Opened,
        test: Opened,
        store: Opened,
    },
    Disk {
        file: Option<(Opened, OnDisk)>,
        folder: Option<Box<Folder>>,
    },
}

struct OnDisk {
    path: PathBuf,
    name: SharedString,
    folders: Vec<SharedString>,
    repo: Option<Repo>,
    dirty: bool,
}

struct Repo {
    git: Arc<GitBinary>,
    path: String,
}

struct Folder {
    listing: Listing,
    icons: IconTheme,
    git: Option<Arc<GitBinary>>,
    tree: Entity<FileTree>,
}

struct Listing {
    root: PathBuf,
    states: Vec<(String, GitStatus)>,
}

impl Listing {
    fn children(&self, folder: &str) -> Vec<TreeNode> {
        let dir = self.root.join(folder);
        let read = match fs::read_dir(&dir) {
            Ok(read) => read,
            Err(error) => {
                eprintln!("desk: editor could not list {}: {error}", dir.display());
                return Vec::new();
            }
        };
        let mut found: Vec<(bool, String)> = read
            .filter_map(|entry| match entry {
                Ok(entry) => Some((
                    entry.path().is_dir(),
                    entry.file_name().to_string_lossy().into_owned(),
                )),
                Err(error) => {
                    eprintln!(
                        "desk: editor skipped an entry of {}: {error}",
                        dir.display()
                    );
                    None
                }
            })
            .filter(|(_, name)| name != ".git")
            .collect();
        found.sort_by_cached_key(|(is_folder, name)| (!is_folder, name.to_lowercase()));
        eprintln!("desk: editor listed {} entries in /{folder}", found.len());
        found
            .into_iter()
            .map(|(is_folder, name)| {
                let path = match folder {
                    "" => name.clone(),
                    folder => format!("{folder}/{name}"),
                };
                match is_folder {
                    true => {
                        TreeNode::unread(name, self.own(&format!("{path}/")), self.inside(&path))
                    }
                    false => TreeNode::file(name, self.own(&path)),
                }
            })
            .collect()
    }

    fn own(&self, key: &str) -> Option<GitStatus> {
        self.states
            .iter()
            .find(|(path, _)| {
                path == key || (path.ends_with('/') && key.starts_with(path.as_str()))
            })
            .map(|(_, git)| *git)
    }

    fn inside(&self, folder: &str) -> Vec<GitStatus> {
        let prefix = format!("{folder}/");
        self.states
            .iter()
            .filter(|(path, _)| path.len() > prefix.len() && path.starts_with(&prefix))
            .map(|(_, git)| *git)
            .collect()
    }

    fn relative(&self, path: &Path) -> Option<SharedString> {
        let inside = path.strip_prefix(&self.root).ok()?;
        let parts: Vec<SharedString> = inside.iter().map(named).collect();
        Some(parts.join("/").into())
    }
}

fn git_status(state: &State) -> Option<GitStatus> {
    match state {
        State::Untracked => Some(GitStatus::Untracked),
        State::Ignored => Some(GitStatus::Ignored),
        State::Conflicted(_) => Some(GitStatus::Conflict),
        State::Renamed { .. } => Some(GitStatus::Modified),
        State::Tracked {
            index: Mark::Deleted,
            ..
        }
        | State::Tracked {
            worktree: Mark::Deleted,
            ..
        } => Some(GitStatus::Deleted),
        State::Tracked {
            index: Mark::Added, ..
        } => Some(GitStatus::Added),
        State::Tracked {
            index: Mark::Unmodified,
            worktree: Mark::Unmodified,
        } => None,
        State::Tracked { .. } => Some(GitStatus::Modified),
    }
}

fn states(git: &GitBinary, root: &Path) -> Vec<(String, GitStatus)> {
    let Ok(inside) = root.strip_prefix(git.root()) else {
        eprintln!(
            "desk: editor found {} outside its repository {}",
            root.display(),
            git.root().display()
        );
        return Vec::new();
    };
    let prefix: String = inside
        .iter()
        .map(|part| format!("{}/", part.to_string_lossy()))
        .collect();
    let entries = match git.status() {
        Ok(entries) => entries,
        Err(error) => {
            eprintln!("desk: editor shows no git badges: {error}");
            return Vec::new();
        }
    };
    let states: Vec<(String, GitStatus)> = entries
        .into_iter()
        .filter_map(|entry| {
            let path = entry.path.strip_prefix(prefix.as_str())?.to_owned();
            Some((path, git_status(&entry.state)?))
        })
        .collect();
    let shown: Vec<String> = states
        .iter()
        .map(|(path, git)| format!("{path} {git:?}"))
        .collect();
    eprintln!(
        "desk: editor git status in {}: {}",
        root.display(),
        shown.join(", ")
    );
    states
}

fn named(part: &std::ffi::OsStr) -> SharedString {
    part.to_string_lossy().into_owned().into()
}

fn absolute(path: &Path) -> PathBuf {
    std::path::absolute(path).unwrap_or_else(|_| path.to_path_buf())
}

fn repository(dir: &Path) -> Option<Arc<GitBinary>> {
    match GitBinary::open(dir) {
        Ok(git) => Some(Arc::new(git)),
        Err(error) => {
            eprintln!("desk: editor shows no git marks: {error}");
            None
        }
    }
}

fn on_disk(full: PathBuf, git: Option<Arc<GitBinary>>) -> OnDisk {
    let name = full.file_name().map(named).unwrap_or_default();
    let inside = git.and_then(|git| match full.strip_prefix(git.root()) {
        Ok(inside) => Some((inside.to_path_buf(), git)),
        Err(_) => {
            eprintln!(
                "desk: editor found {} outside its repository {}",
                full.display(),
                git.root().display()
            );
            None
        }
    });
    let Some((inside, git)) = inside else {
        eprintln!(
            "desk: editor path {}, not in a git repository",
            full.display()
        );
        let folders = full
            .parent()
            .and_then(Path::file_name)
            .map(named)
            .into_iter()
            .collect();
        return OnDisk {
            path: full,
            name,
            folders,
            repo: None,
            dirty: false,
        };
    };
    let parts: Vec<SharedString> = inside.iter().map(named).collect();
    let path = parts.join("/");
    eprintln!(
        "desk: editor path {path} in repository {}",
        git.root().display()
    );
    OnDisk {
        path: full,
        name,
        folders: parts
            .split_last()
            .map(|(_, folders)| folders.to_vec())
            .unwrap_or_default(),
        repo: Some(Repo { git, path }),
        dirty: false,
    }
}

fn git_marks(hunks: &[Hunk]) -> Marks {
    hunks
        .iter()
        .flat_map(|hunk| {
            let (start, count) = (hunk.new.start as usize, hunk.new.count as usize);
            let (lines, gutter) = match (hunk.old.count, count) {
                (_, 0) => (
                    start.max(1)..start.max(1) + 1,
                    GutterMark::Removed(ColorToken::GitDeleted),
                ),
                (0, _) => (
                    start..start + count,
                    GutterMark::Changed(ColorToken::GitAdded),
                ),
                _ => (
                    start..start + count,
                    GutterMark::Changed(ColorToken::GitModified),
                ),
            };
            lines.map(move |line| {
                let mark = LineMarks {
                    gutter: Some(gutter),
                    ..LineMarks::default()
                };
                (line.saturating_sub(1), mark)
            })
        })
        .collect()
}

fn described(marks: &Marks) -> String {
    let lines: Vec<String> = marks
        .iter()
        .map(|(row, mark)| match mark.gutter {
            Some(GutterMark::Changed(ColorToken::GitAdded)) => format!("{} added", row + 1),
            Some(GutterMark::Removed(_)) => format!("{} removed below", row + 1),
            _ => format!("{} modified", row + 1),
        })
        .collect();
    lines.join(", ")
}

fn fixture_editor(lines: &[Line], marks: Marks, cx: &mut App) -> Opened {
    let text: Vec<String> = lines
        .iter()
        .map(|line| line.runs.iter().map(|(_, body)| *body).collect())
        .collect();
    let buffer = Buffer::from_text(&text.join("\n"));
    let syntax = Syntax::new(Language::Go, &buffer).map_err(|error| error.to_string())?;
    let mut editor = CodeEditor::new(buffer, syntax, cx);
    editor.set_marks(marks);
    Ok(cx.new(|_| editor))
}

fn file_editor(path: &Path, cx: &mut App) -> Opened {
    let shown = path.display();
    let editor = match CodeEditor::open(path, cx) {
        Ok(editor) => editor,
        Err(error) => {
            eprintln!("desk: editor could not open {shown}: {error}");
            return Err(error.into());
        }
    };
    let language = Language::from_path(path)
        .map(|language| format!("{language:?}"))
        .unwrap_or_default();
    eprintln!(
        "desk: editor opened {shown} {language} {} lines",
        editor.buffer().line_count()
    );
    let path = PathBuf::from(path);
    let editor = editor.on_save(move |buffer, _, _| {
        let shown = path.display();
        match buffer.save(&path) {
            Ok(()) => {
                eprintln!("desk: editor saved {shown} {} lines", buffer.line_count());
                Ok(())
            }
            Err(error) => {
                eprintln!("desk: editor could not save {shown}: {error}");
                Err(format!("{shown}: {error}"))
            }
        }
    });
    Ok(cx.new(|_| editor))
}

fn launch(
    board: Option<&str>,
    source: Source,
    window: &mut Window,
    cx: &mut App,
) -> Result<AnyView, String> {
    kit::load_fonts(cx)?;
    let editor = match board {
        None | Some("IEDITOR-1") => Editor::new(Board::Edit, source),
        Some("IEDITOR-2") => Editor::new(Board::Changes, source),
        Some("IEDITOR-3") => Editor::new(Board::Split, source),
        Some("S-EDIT-1") => Editor {
            prefs: true,
            trace: true,
            filtering: true,
            ..Editor::new(Board::Edit, source)
        },
        Some("S-EDIT-2") => Editor {
            mode: Mode::Who,
            ..Editor::new(Board::Changes, source)
        },
        Some(other) => {
            return Err(format!(
                "the editor draws IEDITOR-1, IEDITOR-2, IEDITOR-3, S-EDIT-1 and S-EDIT-2, not {other}"
            ));
        }
    };
    Ok(cx.new(|cx| editor.watched(window, cx)).into())
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Board {
    Edit,
    Changes,
    Split,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum File {
    Notes,
    Test,
    Store,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Mode {
    Edit,
    Changes,
    Who,
}

pub struct Editor {
    board: Board,
    file: File,
    test_open: bool,
    notes_open: bool,
    trace: bool,
    filtering: bool,
    prefs: bool,
    mode: Mode,
    menu: bool,
    told: Option<SharedString>,
    source: Source,
    _watch: Option<Subscription>,
    _tree: Option<Subscription>,
    _marking: Option<Task<()>>,
}

impl Editor {
    fn new(board: Board, source: Source) -> Self {
        Editor {
            board,
            file: File::Notes,
            test_open: true,
            notes_open: true,
            trace: false,
            filtering: false,
            prefs: false,
            mode: Mode::Changes,
            menu: board == Board::Split,
            told: None,
            source,
            _watch: None,
            _tree: None,
            _marking: None,
        }
    }

    fn watched(mut self, window: &mut Window, cx: &mut Context<Self>) -> Self {
        if let Source::Disk {
            folder: Some(folder),
            ..
        } = &self.source
        {
            self._tree = Some(cx.subscribe_in(&folder.tree, window, Self::tree_event));
        }
        self.watch(cx);
        self.follow(cx);
        self
    }

    fn watch(&mut self, cx: &mut Context<Self>) {
        self._watch = None;
        if let Source::Disk {
            file: Some((Ok(code), _)),
            ..
        } = &self.source
        {
            self._watch = Some(cx.observe(code, Self::code_changed));
            self.remark(cx);
        }
    }

    fn follow(&self, cx: &mut Context<Self>) {
        let Source::Disk {
            file: Some((_, disk)),
            folder: Some(folder),
        } = &self.source
        else {
            return;
        };
        if let Some(path) = folder.listing.relative(&disk.path) {
            folder.tree.update(cx, |tree, cx| tree.select(path, cx));
        }
    }

    fn tree_event(
        &mut self,
        _: &Entity<FileTree>,
        event: &TreeEvent,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let Source::Disk {
            file,
            folder: Some(folder),
        } = &self.source
        else {
            return;
        };
        match event {
            TreeEvent::Unfolded(path) => {
                let children = folder.listing.children(path);
                let loaded = folder
                    .tree
                    .update(cx, |tree, _| tree.load(path, children, &folder.icons));
                if !loaded {
                    eprintln!("desk: editor tree has no folder {path}");
                }
            }
            TreeEvent::Opened(path) => {
                let target = folder.listing.root.join(path.as_str());
                match file {
                    Some((_, disk)) if disk.path == target => {}
                    Some((_, disk)) if disk.dirty => {
                        eprintln!("desk: editor kept {}: unsaved changes", disk.path.display());
                        self.follow(cx);
                    }
                    _ => {
                        let git = folder.git.clone();
                        let opened = (file_editor(&target, cx), on_disk(target, git));
                        if let Source::Disk { file, .. } = &mut self.source {
                            *file = Some(opened);
                        }
                        self.watch(cx);
                        cx.notify();
                    }
                }
                if let Source::Disk {
                    file: Some((Ok(code), _)),
                    ..
                } = &self.source
                {
                    window.focus(&code.focus_handle(cx), cx);
                }
            }
        }
    }

    fn code_changed(&mut self, code: Entity<CodeEditor>, cx: &mut Context<Self>) {
        let dirty = code.read(cx).buffer().is_dirty();
        let Source::Disk {
            file: Some((_, disk)),
            ..
        } = &mut self.source
        else {
            return;
        };
        if disk.dirty == dirty {
            return;
        }
        disk.dirty = dirty;
        if !dirty {
            self.remark(cx);
        }
        cx.notify();
    }

    fn remark(&mut self, cx: &mut Context<Self>) {
        let Source::Disk {
            file:
                Some((
                    Ok(code),
                    OnDisk {
                        repo: Some(repo), ..
                    },
                )),
            ..
        } = &self.source
        else {
            return;
        };
        let (code, git, path) = (code.downgrade(), repo.git.clone(), repo.path.clone());
        let diff = cx
            .background_executor()
            .spawn(async move { git.diff(&path, Against::Index).map(|hunks| (path, hunks)) });
        self._marking = Some(cx.spawn(async move |_, cx| {
            let (path, hunks) = match diff.await {
                Ok(found) => found,
                Err(error) => {
                    eprintln!("desk: editor could not read git marks: {error}");
                    return;
                }
            };
            let marks = git_marks(&hunks);
            let shown = described(&marks);
            let applied = code.update(cx, |code, cx| {
                let clean = !code.buffer().is_dirty();
                if clean {
                    code.set_marks(marks);
                    cx.notify();
                }
                clean
            });
            match applied {
                Ok(true) => eprintln!(
                    "desk: editor git marks for {path}, {} hunks: {shown}",
                    hunks.len()
                ),
                Ok(false) => eprintln!("desk: editor dropped git marks for {path}, edited since"),
                Err(error) => eprintln!("desk: editor is gone before its git marks: {error}"),
            }
        }));
    }

    fn shut_file(&mut self, cx: &mut Context<Self>) {
        if let Source::Disk { file, .. } = &mut self.source {
            *file = None;
            self._watch = None;
            cx.notify();
        }
    }

    fn opened(&self) -> Option<&Opened> {
        match (&self.source, self.file) {
            (Source::Disk { file, .. }, _) => file.as_ref().map(|(opened, _)| opened),
            (Source::Fixture { notes, .. }, File::Notes) => Some(notes),
            (Source::Fixture { test, .. }, File::Test) => Some(test),
            (Source::Fixture { store, .. }, File::Store) => Some(store),
        }
    }

    fn tell(
        message: &'static str,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.told = Some(message.into());
            cx.notify();
        }
    }

    fn update(
        change: impl Fn(&mut Self) + 'static,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            change(this);
            cx.notify();
        }
    }
}

impl Render for Editor {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let theme = ActiveTheme::theme(cx);
        let body = match self.board {
            Board::Edit => self.edit_board(scale, cx),
            Board::Changes => self.changes_board(scale, cx),
            Board::Split => self.split_board(scale, cx),
        };
        kit::frame(
            &theme,
            body,
            self.told.clone(),
            cx.listener(|this, _: &ClickEvent, _, cx| {
                this.told = None;
                cx.notify();
            }),
        )
    }
}
