mod boards;
mod fixture;
mod kit;
mod parts;

use std::path::{Path, PathBuf};
use std::sync::Arc;

use desk_core::buffer::Buffer;
use desk_core::git::{Against, Git, GitBinary, Hunk};
use desk_core::syntax::{Language, Syntax};
use desk_ui::components::code::{GutterMark, LineMarks, Marks};
use desk_ui::components::code_editor::CodeEditor;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::ColorToken;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Entity, Render, SharedString, Subscription,
    Task, Window, prelude::*,
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
    launch(board, source, cx)
}

pub fn open_file(
    path: &Path,
    board: Option<&str>,
    _: &mut Window,
    cx: &mut App,
) -> Result<AnyView, String> {
    let source = Source::File(file_editor(path, cx), on_disk(path));
    launch(board, source, cx)
}

type Opened = Result<Entity<CodeEditor>, SharedString>;

enum Source {
    Fixture {
        notes: Opened,
        test: Opened,
        store: Opened,
    },
    File(Opened, OnDisk),
}

struct OnDisk {
    name: SharedString,
    folders: Vec<SharedString>,
    repo: Option<Repo>,
    dirty: bool,
    shut: bool,
}

struct Repo {
    git: Arc<GitBinary>,
    path: String,
}

fn named(part: &std::ffi::OsStr) -> SharedString {
    part.to_string_lossy().into_owned().into()
}

fn on_disk(path: &Path) -> OnDisk {
    let full = std::path::absolute(path).unwrap_or_else(|_| path.to_path_buf());
    let name = full.file_name().map(named).unwrap_or_default();
    let parent = full.parent().unwrap_or(&full);
    let inside = match GitBinary::open(parent) {
        Ok(git) => match full.strip_prefix(git.root()) {
            Ok(inside) => Some((inside.to_path_buf(), git)),
            Err(_) => {
                eprintln!(
                    "desk: editor found {} outside its repository {}",
                    full.display(),
                    git.root().display()
                );
                None
            }
        },
        Err(error) => {
            eprintln!("desk: editor shows no git marks: {error}");
            None
        }
    };
    let Some((inside, git)) = inside else {
        eprintln!(
            "desk: editor path {}, not in a git repository",
            full.display()
        );
        return OnDisk {
            name,
            folders: parent.file_name().map(named).into_iter().collect(),
            repo: None,
            dirty: false,
            shut: false,
        };
    };
    let parts: Vec<SharedString> = inside.iter().map(named).collect();
    let path = parts.join("/");
    eprintln!(
        "desk: editor path {path} in repository {}",
        git.root().display()
    );
    OnDisk {
        name,
        folders: parts
            .split_last()
            .map(|(_, folders)| folders.to_vec())
            .unwrap_or_default(),
        repo: Some(Repo {
            git: Arc::new(git),
            path,
        }),
        dirty: false,
        shut: false,
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

fn launch(board: Option<&str>, source: Source, cx: &mut App) -> Result<AnyView, String> {
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
    Ok(cx.new(|cx| editor.watched(cx)).into())
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
            _marking: None,
        }
    }

    fn watched(mut self, cx: &mut Context<Self>) -> Self {
        if let Source::File(Ok(code), _) = &self.source {
            self._watch = Some(cx.observe(code, Self::code_changed));
            self.remark(cx);
        }
        self
    }

    fn code_changed(&mut self, code: Entity<CodeEditor>, cx: &mut Context<Self>) {
        let dirty = code.read(cx).buffer().is_dirty();
        let Source::File(_, disk) = &mut self.source else {
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
        let Source::File(
            Ok(code),
            OnDisk {
                repo: Some(repo), ..
            },
        ) = &self.source
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
        if let Source::File(_, disk) = &mut self.source {
            disk.shut = true;
            cx.notify();
        }
    }

    fn opened(&self) -> &Opened {
        match (&self.source, self.file) {
            (Source::File(opened, _), _) => opened,
            (Source::Fixture { notes, .. }, File::Notes) => notes,
            (Source::Fixture { test, .. }, File::Test) => test,
            (Source::Fixture { store, .. }, File::Store) => store,
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
