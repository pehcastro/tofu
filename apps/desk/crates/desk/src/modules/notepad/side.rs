use std::fs;
use std::path::{Path, PathBuf};

use desk_ui::components::form::TextInput;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::{MenuItem, context_menu};
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::size::{FONT_BODY, RADIUS_ROW, T1, T2};
use desk_ui::metrics::ICON_SMALL;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, ClickEvent, Context, Entity, EntityInputHandler, Focusable, KeyDownEvent,
    MouseButton, MouseDownEvent, Pixels, Point, Subscription, Window, div, prelude::*, px,
    relative,
};

use super::shelf::{Colour, Entry};
use super::{Asking, NEW_NOTE, Next, Notepad, one_name, write_file};

const NEW_FOLDER: &str = "New folder";
const RENAME: &str = "Rename";
const COLOUR: &str = "Colour";
const NO_COLOUR: &str = "No colour";
const MOVE_TO: &str = "Move to";
const REVEAL: &str = "Reveal in File Explorer";
const DELETE: &str = "Delete";
const NOTE_EXTENSION: &str = "md";
const LIST_WIDTH: f32 = 220.0;
const LIST_SHARE: f32 = 0.35;
const ROW_PAD: f32 = 8.0;
const INDENT: f32 = 14.0;

#[derive(Default)]
pub(super) struct Menu {
    spot: Option<PathBuf>,
    at: Option<Point<Pixels>>,
}

enum Name {
    Note(PathBuf),
    Folder(PathBuf),
    Rename(PathBuf),
}

pub(super) struct Naming {
    name: Name,
    input: Entity<TextInput>,
    _blurred: Subscription,
}

#[derive(Clone)]
enum Pick {
    NewNote(PathBuf),
    NewFolder(PathBuf),
    Rename(PathBuf),
    Paint(PathBuf, Option<Colour>),
    Move(PathBuf, PathBuf),
    Reveal(PathBuf),
    Delete(PathBuf),
}

fn file_name(path: &Path) -> String {
    path.file_name()
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_default()
}

fn moved(path: &Path, from: &Path, to: &Path) -> Option<PathBuf> {
    let rest = path.strip_prefix(from).ok()?;
    Some(match rest.as_os_str().is_empty() {
        true => to.to_path_buf(),
        false => to.join(rest),
    })
}

fn submenu(
    label: &'static str,
    mark: Glyph,
    children: Vec<(MenuItem, Pick)>,
    items: &mut Vec<MenuItem>,
    picks: &mut Vec<Option<Pick>>,
) {
    let (inner, chosen): (Vec<MenuItem>, Vec<Option<Pick>>) = children
        .into_iter()
        .map(|(item, pick)| (item, Some(pick)))
        .unzip();
    items.push(MenuItem::Submenu {
        label: label.into(),
        icon: Some(mark.into()),
        items: inner,
    });
    picks.push(None);
    picks.extend(chosen);
}

impl Notepad {
    fn picks(&self) -> (Vec<MenuItem>, Vec<Option<Pick>>) {
        let root = self.shelf.root().to_path_buf();
        let spot = self.menu.spot.clone();
        let folder = match &spot {
            Some(path) if path.is_dir() => path.clone(),
            Some(path) => path
                .parent()
                .map_or_else(|| root.clone(), Path::to_path_buf),
            None => root.clone(),
        };
        let mut items = vec![
            MenuItem::action(NEW_NOTE).icon(Glyph::FileAdd),
            MenuItem::action(NEW_FOLDER).icon(Glyph::FolderAdd),
            MenuItem::Separator,
        ];
        let mut picks = vec![
            Some(Pick::NewNote(folder.clone())),
            Some(Pick::NewFolder(folder)),
            None,
        ];
        let Some(spot) = spot else {
            items.push(MenuItem::action(REVEAL).icon(Glyph::Reveal));
            picks.push(Some(Pick::Reveal(root)));
            return (items, picks);
        };
        items.push(MenuItem::action(RENAME).icon(Glyph::Pencil));
        picks.push(Some(Pick::Rename(spot.clone())));
        let colours = std::iter::once((MenuItem::action(NO_COLOUR), None))
            .chain(Colour::ALL.map(|colour| (MenuItem::action(colour.label()), Some(colour))))
            .map(|(item, colour)| (item, Pick::Paint(spot.clone(), colour)))
            .collect();
        submenu(COLOUR, Glyph::Palette, colours, &mut items, &mut picks);
        let folders = self
            .shelf
            .entries()
            .iter()
            .filter(|entry| entry.folder)
            .map(|entry| entry.path.as_path());
        let targets: Vec<(MenuItem, Pick)> = std::iter::once(root.as_path())
            .chain(folders)
            .filter(|target| !target.starts_with(&spot) && spot.parent() != Some(*target))
            .map(|target| {
                let pick = Pick::Move(spot.clone(), target.to_path_buf());
                (MenuItem::action(self.shown(target)), pick)
            })
            .collect();
        if !targets.is_empty() {
            submenu(MOVE_TO, Glyph::Folder, targets, &mut items, &mut picks);
        }
        items.extend([
            MenuItem::action(REVEAL).icon(Glyph::Reveal),
            MenuItem::Separator,
            MenuItem::action(DELETE).icon(Glyph::Trash).danger(true),
        ]);
        picks.extend([
            Some(Pick::Reveal(spot.clone())),
            None,
            Some(Pick::Delete(spot)),
        ]);
        (items, picks)
    }

    fn pick(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        let (_, picks) = self.picks();
        let Some(Some(pick)) = picks.get(at).cloned() else {
            return eprintln!("desk: notepad menu has no item {at}");
        };
        let done = match pick {
            Pick::NewNote(folder) => {
                self.name(Name::Note(folder), window, cx);
                Ok(())
            }
            Pick::NewFolder(folder) => {
                self.name(Name::Folder(folder), window, cx);
                Ok(())
            }
            Pick::Rename(path) => {
                self.name(Name::Rename(path), window, cx);
                Ok(())
            }
            Pick::Paint(path, colour) => self.paint(&path, colour),
            Pick::Move(from, folder) => {
                let to = folder.join(file_name(&from));
                self.relocate(&from, to)
            }
            Pick::Reveal(path) => {
                eprintln!("desk: notepad reveals {}", path.display());
                cx.reveal_path(&path);
                Ok(())
            }
            Pick::Delete(path) => {
                eprintln!("desk: notepad asks before deleting {}", path.display());
                self.asking = Some(Asking::Delete(path));
                Ok(())
            }
        };
        if let Err(error) = done {
            self.fail(error);
        }
        cx.notify();
    }

    fn paint(&mut self, path: &Path, colour: Option<Colour>) -> Result<(), String> {
        let (sidecar, text) = self.shelf.paint(path, colour)?;
        write_file(self.plugin, &sidecar, &text)?;
        eprintln!(
            "desk: notepad coloured {} {}",
            path.display(),
            colour.map_or("none", Colour::label)
        );
        Ok(())
    }

    fn carry(&mut self, from: &Path, to: Option<&Path>) -> Result<(), String> {
        match self.shelf.carry(from, to)? {
            Some((sidecar, text)) => write_file(self.plugin, &sidecar, &text),
            None => Ok(()),
        }
    }

    fn relocate(&mut self, from: &Path, to: PathBuf) -> Result<(), String> {
        if to.exists() {
            return Err(format!("{} already exists", to.display()));
        }
        fs::rename(from, &to)
            .map_err(|error| format!("{} -> {}: {error}", from.display(), to.display()))?;
        eprintln!("desk: notepad moved {} -> {}", from.display(), to.display());
        let carried = self.carry(from, Some(to.as_path()));
        self.rendered = std::mem::take(&mut self.rendered)
            .into_iter()
            .map(|path| moved(&path, from, &to).unwrap_or(path))
            .collect();
        self.shut = std::mem::take(&mut self.shut)
            .into_iter()
            .map(|path| moved(&path, from, &to).unwrap_or(path))
            .collect();
        if let Some(page) = &mut self.page
            && let Some(open) = page.path.as_deref().and_then(|open| moved(open, from, &to))
        {
            page.path = Some(open);
        }
        self.list();
        self.remember();
        carried
    }

    pub(super) fn remove(&mut self, path: &Path, cx: &mut Context<Self>) {
        let removed = match path.is_dir() {
            true => fs::remove_dir_all(path),
            false => fs::remove_file(path),
        };
        if let Err(error) = removed {
            self.fail(format!("{}: {error}", path.display()));
            return cx.notify();
        }
        eprintln!("desk: notepad deleted {}", path.display());
        if let Err(error) = self.carry(path, None) {
            self.fail(error);
        }
        self.rendered.retain(|kept| !kept.starts_with(path));
        self.shut.retain(|kept| !kept.starts_with(path));
        let open = self
            .page
            .as_ref()
            .and_then(|page| page.path.as_deref())
            .is_some_and(|open| open.starts_with(path));
        self.list();
        match open {
            true => self.go(Next::Untitled, cx),
            false => {
                self.remember();
                cx.notify();
            }
        }
    }

    fn name(&mut self, name: Name, window: &mut Window, cx: &mut Context<Self>) {
        let (placeholder, typed) = match &name {
            Name::Note(_) => ("Note name", String::new()),
            Name::Folder(_) => ("Folder name", String::new()),
            Name::Rename(path) => ("New name", file_name(path)),
        };
        if let Name::Note(folder) | Name::Folder(folder) = &name {
            self.shut.remove(folder);
        }
        let input = TextInput::new(placeholder.into(), window, cx);
        input.update(cx, |input, cx| {
            input.replace_text_in_range(None, &typed, window, cx)
        });
        let focus = input.focus_handle(cx);
        window.focus(&focus, cx);
        let blurred = cx.on_blur(&focus, window, |notepad, _, cx| {
            if notepad.naming.take().is_some() {
                eprintln!("desk: notepad naming left");
                cx.notify();
            }
        });
        self.naming = Some(Naming {
            name,
            input,
            _blurred: blurred,
        });
        cx.notify();
    }

    fn name_key(&mut self, event: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        if self.naming.is_none() {
            return;
        }
        let enter = match event.keystroke.key.as_str() {
            "enter" => true,
            "escape" => false,
            _ => return,
        };
        cx.stop_propagation();
        let Some(naming) = self.naming.take() else {
            return;
        };
        cx.notify();
        if !enter {
            return eprintln!("desk: notepad naming cancelled");
        }
        let typed = naming.input.read(cx).text().trim().to_owned();
        if let Err(error) = self.commit(naming.name, &typed, cx) {
            self.fail(error);
        }
    }

    fn commit(&mut self, name: Name, typed: &str, cx: &mut Context<Self>) -> Result<(), String> {
        if !one_name(typed) {
            return Err(format!("{typed:?} is not a name"));
        }
        match name {
            Name::Note(folder) => {
                let mut path = folder.join(typed);
                if path.extension().is_none() {
                    path.set_extension(NOTE_EXTENSION);
                }
                if path.exists() {
                    return Err(format!("{} already exists", path.display()));
                }
                write_file(self.plugin, &path, "")?;
                eprintln!("desk: notepad new note {}", path.display());
                self.list();
                self.leave(Next::Open(path), cx);
            }
            Name::Folder(folder) => {
                let path = folder.join(typed);
                fs::create_dir(&path).map_err(|error| format!("{}: {error}", path.display()))?;
                eprintln!("desk: notepad new folder {}", path.display());
                self.list();
            }
            Name::Rename(from) => self.relocate(&from, from.with_file_name(typed))?,
        }
        Ok(())
    }

    fn fold(&mut self, folder: &Path, cx: &mut Context<Self>) {
        if !self.shut.remove(folder) {
            self.shut.insert(folder.to_path_buf());
        }
        cx.notify();
    }

    fn hidden(&self, entry: &Entry) -> bool {
        self.shut
            .iter()
            .any(|folder| entry.path != *folder && entry.path.starts_with(folder))
    }

    fn row(&self, at: usize, entry: &Entry, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let open = self.page.as_ref().and_then(|page| page.path.as_deref());
        let picked = open == Some(entry.path.as_path());
        let hover = theme.color(ColorToken::StateHover);
        let tint = self
            .shelf
            .colour(&entry.path)
            .map_or(theme.color(ColorToken::TextIcon), |colour| {
                theme.color(colour.token())
            });
        let shut = entry.folder && self.shut.contains(&entry.path);
        let mark = match entry.folder {
            true => Glyph::Folder,
            false => Glyph::File,
        };
        let (path, spot, folder) = (entry.path.clone(), entry.path.clone(), entry.folder);
        div()
            .id(("notepad-entry", at))
            .flex()
            .items_center()
            .gap_1p5()
            .pl(px(ROW_PAD + INDENT * entry.depth as f32))
            .pr_2()
            .py_1()
            .rounded(px(RADIUS_ROW))
            .cursor_pointer()
            .text_size(px(FONT_BODY))
            .text_color(ink(theme, if picked { T1 } else { T2 }))
            .when(picked, |row| row.bg(hover))
            .hover(move |style| style.bg(hover))
            .child(glyph(mark, ICON_SMALL, tint))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .truncate()
                    .child(file_name(&entry.path)),
            )
            .when(shut, |row| {
                row.child(div().text_color(ink(theme, T2)).child("+"))
            })
            .on_click(
                cx.listener(move |notepad, _: &ClickEvent, _, cx| match folder {
                    true => notepad.fold(&path, cx),
                    false => notepad.leave(Next::Open(path.clone()), cx),
                }),
            )
            .on_mouse_down(
                MouseButton::Right,
                cx.listener(move |notepad, event: &MouseDownEvent, _, cx| {
                    notepad.menu.spot = Some(spot.clone());
                    notepad.menu.at = Some(event.position);
                    cx.stop_propagation();
                    cx.notify();
                }),
            )
            .into_any_element()
    }

    fn rows(&self, theme: &Theme, cx: &mut Context<Self>) -> Vec<AnyElement> {
        let field = |input: &Entity<TextInput>, depth: usize| {
            div()
                .pl(px(ROW_PAD + INDENT * depth as f32))
                .pr_1()
                .py_0p5()
                .child(input.clone())
                .into_any_element()
        };
        let naming = self.naming.as_ref();
        let made_in = naming.and_then(|naming| match &naming.name {
            Name::Note(folder) | Name::Folder(folder) => Some((folder.as_path(), &naming.input)),
            Name::Rename(_) => None,
        });
        let mut rows = Vec::new();
        if let Some((folder, input)) = made_in
            && folder == self.shelf.root()
        {
            rows.push(field(input, 0));
        }
        for (at, entry) in self.shelf.entries().iter().enumerate() {
            if self.hidden(entry) {
                continue;
            }
            match naming {
                Some(Naming {
                    name: Name::Rename(path),
                    input,
                    ..
                }) if *path == entry.path => rows.push(field(input, entry.depth)),
                _ => rows.push(self.row(at, entry, theme, cx)),
            }
            if let Some((folder, input)) = made_in
                && folder == entry.path
            {
                rows.push(field(input, entry.depth.saturating_add(1)));
            }
        }
        rows
    }

    pub(super) fn side(&mut self, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let (items, _) = self.picks();
        let rows = self.rows(theme, cx);
        let list = div()
            .id("notepad-notes")
            .flex()
            .flex_col()
            .flex_1()
            .min_h_0()
            .p_1()
            .overflow_y_scroll()
            .on_mouse_down(
                MouseButton::Right,
                cx.listener(|notepad, _: &MouseDownEvent, _, cx| {
                    notepad.menu.spot = None;
                    cx.notify();
                }),
            )
            .on_key_down(cx.listener(Self::name_key))
            .children(rows);
        context_menu(items)
            .id("notepad-menu")
            .flex()
            .flex_col()
            .flex_none()
            .w(relative(LIST_SHARE))
            .max_w(px(LIST_WIDTH))
            .h_full()
            .bg(theme.color(ColorToken::CardsOuterFill))
            .border_r_1()
            .border_color(theme.color(ColorToken::Separator))
            .open_at(self.menu.at.take())
            .on_pick(cx.listener(|notepad, at: &usize, window, cx| notepad.pick(*at, window, cx)))
            .child(list)
            .into_any_element()
    }
}
