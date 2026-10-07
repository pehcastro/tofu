use std::rc::Rc;

use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::diff::{FileChange, FileDiff, PatchError};
use desk_ui::components::file_edits::{EditedFile, FileEdit, file_edits, file_history};
use desk_ui::components::glyph::Glyph;
use desk_ui::theme::Theme;
use gpui::{App, Context, Div, SharedString, Window, div, prelude::*, px};

use super::Book;
use super::kit::label;

const TILE_WIDTH: f32 = 513.0;
const TILE_HEIGHT: f32 = 460.0;
const HISTORY_WIDTH: f32 = 495.0;
const OPENED_AT_START: usize = 2;

const STORE_EDITS: [(&str, &str, &str); 4] = [
    (
        "go-dev 2",
        "14m ago",
        "@@ -8,6 +8,7 @@ type Store interface
 type Store interface {
 \tAll() ([]Note, error)
 \tAdd(title string) (Note, error)
+\tCount() (int, error)
 \tRemove(id int64) error
 \tSearch(q string) ([]Note, error)
 }",
    ),
    (
        "go-dev 5",
        "9m ago",
        "@@ -8,7 +8,7 @@ type Store interface
 type Store interface {
 \tAll() ([]Note, error)
 \tAdd(title string) (Note, error)
 \tCount() (int, error)
-\tRemove(id int64) error
+\tDelete(id int64) error
 \tSearch(q string) ([]Note, error)
 }",
    ),
    (
        "go-dev 1",
        "4m ago",
        "@@ -30,16 +30,16 @@ func Open(cfg Config) (Store, error)
 func Open(cfg Config) (Store, error) {
-\tif cfg.JSON != \"\" {
-\t\treturn OpenJSON(cfg.JSON)
-\t}
+\tif cfg.Path == \"\" {
+\t\treturn nil, ErrNoPath
+\t}
 \tdb, err := sql.Open(\"sqlite\", cfg.Path)
 \tif err != nil {
 \t\treturn nil, err
 \t}
 \tif _, err := db.Exec(walPragma); err != nil {
 \t\treturn nil, err
 \t}
 \tif err := migrate(db); err != nil {
 \t\treturn nil, err
 \t}
-\treturn &SQLite{db: db}, nil
+\treturn &SQLite{db: db, clock: cfg.Clock}, nil
 }",
    ),
    (
        "go-dev 7",
        "2m ago",
        "@@ -12,3 +12,4 @@ type Store interface
 \tCount() (int, error)
 \tSearch(q string) ([]Note, error)
+\tRestore(id int64) error
 }",
    ),
];

const ONE_EDIT_FILES: [(&str, FileChange, &str, &str, &str); 11] = [
    (
        "store/sqlite.go",
        FileChange::Added,
        "go-dev 1",
        "3m ago",
        "@@ -0,0 +1,5 @@
+package store
+
+type SQLite struct {
+\tdb *sql.DB
+}",
    ),
    (
        "store/json.go",
        FileChange::Deleted,
        "go-dev 8",
        "6m ago",
        "@@ -1,5 +0,0 @@
-package store
-
-type JSON struct {
-\tpath string
-}",
    ),
    (
        "web/Header.tsx",
        FileChange::Modified,
        "ts-dev 1",
        "1m ago",
        "@@ -33,1 +33,1 @@ export function Header
-      <Badge count={count} />
+      {count > 0 && <Badge count={count} />}",
    ),
    (
        "notes/notes_test.go",
        FileChange::Modified,
        "go-dev 2",
        "9m ago",
        "@@ -40,1 +40,4 @@ func TestDelete
 \tn.Delete(2)
+\tif got, _ := n.Count(); got != 2 {
+\t\tt.Fatalf(\"want 2, got %d\", got)
+\t}",
    ),
    (
        "web/NoteList.tsx",
        FileChange::Modified,
        "ts-dev 3",
        "5m ago",
        "@@ -12,2 +12,2 @@ export function NoteList
-  const notes = useNotes()
+  const { data: notes } = useQuery(notesQuery)
   return <List items={notes} />",
    ),
    (
        "notes/notes.go",
        FileChange::Modified,
        "go-dev 1",
        "2m ago",
        "@@ -20,1 +20,2 @@ func (n *Notes) Delete
 \treturn n.store.Delete(id)
+\tn.undo.Push(id)",
    ),
    (
        "notes/search.go",
        FileChange::Modified,
        "go-dev 2",
        "5m ago",
        "@@ -9,1 +9,1 @@ func Search
-\treturn strings.Contains(title, q)
+\treturn strings.Contains(strings.ToLower(title), strings.ToLower(q))",
    ),
    (
        "store/undo.go",
        FileChange::Modified,
        "go-dev 3",
        "8m ago",
        "@@ -1,1 +1,3 @@
 package store
+
+type Snapshot struct{ ID int64 }",
    ),
    (
        "store/migrate.go",
        FileChange::Added,
        "go-dev 4",
        "11m ago",
        "@@ -0,0 +1,2 @@
+package store
+const softDelete = \"ALTER TABLE notes ADD deleted_at INTEGER\"",
    ),
    (
        "web/count.ts",
        FileChange::Modified,
        "ts-dev 1",
        "26m ago",
        "@@ -3,1 +3,1 @@ export const countQuery
-  staleTime: 30_000,
+  staleTime: 0,",
    ),
    (
        "web/Empty.tsx",
        FileChange::Added,
        "ts-dev 2",
        "29m ago",
        "@@ -0,0 +1,3 @@
+export function Empty() {
+  return <p>No notes yet</p>
+}",
    ),
];

fn edit(
    path: &str,
    change: FileChange,
    agent: &str,
    at: &str,
    patch: &str,
) -> Result<FileEdit, PatchError> {
    Ok(FileEdit {
        agent: Some(SharedString::from(agent.to_owned())),
        at: SharedString::from(at.to_owned()),
        diff: Rc::new(FileDiff::parse(path.to_owned(), change, patch, |_| {
            Vec::new()
        })?),
    })
}

fn store_file() -> Result<EditedFile, PatchError> {
    let path = "store/store.go";
    let edits = STORE_EDITS
        .iter()
        .map(|(agent, at, patch)| edit(path, FileChange::Modified, agent, at, patch))
        .collect::<Result<Vec<_>, _>>()?;
    let mut by: Vec<SharedString> = Vec::new();
    for agent in edits.iter().rev().filter_map(|edit| edit.agent.clone()) {
        if !by.contains(&agent) {
            by.push(agent);
        }
    }
    Ok(EditedFile {
        path: path.into(),
        edits,
        by,
    })
}

fn files() -> Result<Vec<EditedFile>, PatchError> {
    let mut files = ONE_EDIT_FILES
        .iter()
        .map(|(path, change, agent, at, patch)| {
            Ok(EditedFile {
                path: SharedString::from(path.to_owned()),
                edits: vec![edit(path, change.clone(), agent, at, patch)?],
                by: vec![SharedString::from(agent.to_owned())],
            })
        })
        .collect::<Result<Vec<_>, PatchError>>()?;
    files.insert(OPENED_AT_START, store_file()?);
    Ok(files)
}

pub(super) struct FileEditsPage {
    files: Result<Vec<EditedFile>, PatchError>,
    opened: usize,
}

impl FileEditsPage {
    pub(super) fn new(_cx: &mut Context<Book>) -> Self {
        FileEditsPage {
            files: files(),
            opened: OPENED_AT_START,
        }
    }

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, cx: &mut Context<Book>) -> Div {
        let files = match &self.files {
            Ok(files) => files,
            Err(problem) => return div().child(label(format!("sample patch: {problem}"), theme)),
        };
        let book = cx.weak_entity();
        let open = move |ix: usize, _: &mut Window, cx: &mut App| {
            if let Some(book) = book.upgrade() {
                book.update(cx, |book, cx| {
                    book.file_edits.opened = ix;
                    cx.notify();
                });
            }
        };
        let tile = shell(
            Header::Title(Some(Glyph::File), "File edits".into(), None),
            theme,
        )
        .w(px(TILE_WIDTH))
        .h(px(TILE_HEIGHT))
        .child(inner_card(theme).child(file_edits("file-edits", files, theme, open)));
        let history = files.get(self.opened).map(|file| {
            div().w(px(HISTORY_WIDTH)).flex_none().child(file_history(
                ("file-history", self.opened),
                file,
                theme,
            ))
        });
        div()
            .flex()
            .flex_col()
            .gap_2()
            .child(label(
                "IWY-4, the File edits tab: one row per file the session edited. A row opens that file's history beside it, newest edit first, each a folding DiffCard with three lines of context.",
                theme,
            ))
            .child(
                div()
                    .flex()
                    .items_start()
                    .gap_3()
                    .child(tile)
                    .children(history),
            )
    }
}
