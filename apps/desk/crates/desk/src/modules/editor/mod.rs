mod boards;
mod fixture;
mod kit;
mod parts;

use std::path::{Path, PathBuf};

use desk_core::buffer::Buffer;
use desk_core::syntax::{Language, Syntax};
use desk_ui::components::code_editor::CodeEditor;
use desk_ui::live::ActiveTheme;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Entity, Render, SharedString, Window, prelude::*,
};

use fixture::{COUNT_TEST, Line, NOTES, STORE};

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    let source = Source::Fixture {
        notes: fixture_editor(NOTES, cx),
        test: fixture_editor(COUNT_TEST, cx),
        store: fixture_editor(STORE, cx),
    };
    launch(board, source, cx)
}

pub fn open_file(
    path: &Path,
    board: Option<&str>,
    _: &mut Window,
    cx: &mut App,
) -> Result<AnyView, String> {
    let source = Source::File(file_editor(path, cx));
    launch(board, source, cx)
}

type Opened = Result<Entity<CodeEditor>, SharedString>;

enum Source {
    Fixture {
        notes: Opened,
        test: Opened,
        store: Opened,
    },
    File(Opened),
}

fn fixture_editor(lines: &[Line], cx: &mut App) -> Opened {
    let text: Vec<String> = lines
        .iter()
        .map(|line| line.runs.iter().map(|(_, body)| *body).collect())
        .collect();
    let buffer = Buffer::from_text(&text.join("\n"));
    let syntax = Syntax::new(Language::Go, &buffer).map_err(|error| error.to_string())?;
    Ok(cx.new(|cx| CodeEditor::new(buffer, syntax, cx)))
}

fn file_editor(path: &Path, cx: &mut App) -> Opened {
    let shown = path.display();
    let loaded = Language::from_path(path)
        .ok_or_else(|| format!("desk_core has no syntax for {shown}"))
        .and_then(|language| {
            let buffer = Buffer::load(path).map_err(|error| error.to_string())?;
            let syntax = Syntax::new(language, &buffer).map_err(|error| error.to_string())?;
            Ok((language, buffer, syntax))
        });
    let (language, buffer, syntax) = match loaded {
        Ok(loaded) => loaded,
        Err(error) => {
            eprintln!("desk: editor could not open {shown}: {error}");
            return Err(error.into());
        }
    };
    eprintln!(
        "desk: editor opened {shown} {language:?} {} lines",
        buffer.line_count()
    );
    let path = PathBuf::from(path);
    Ok(cx.new(|cx| {
        CodeEditor::new(buffer, syntax, cx).on_save(move |buffer, _, _| {
            let shown = path.display();
            match buffer.save(&path) {
                Ok(()) => eprintln!("desk: editor saved {shown} {} lines", buffer.line_count()),
                Err(error) => eprintln!("desk: editor could not save {shown}: {error}"),
            }
        })
    }))
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
    Ok(cx.new(|_| editor).into())
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
        }
    }

    fn opened(&self) -> &Opened {
        match (&self.source, self.file) {
            (Source::File(opened), _) => opened,
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
