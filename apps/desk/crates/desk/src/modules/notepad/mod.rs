use std::cell::RefCell;
use std::collections::BTreeSet;
use std::fs::{self, OpenOptions};
use std::io::ErrorKind;
use std::path::{Component, Path, PathBuf};
use std::rc::Rc;
use std::time::{Duration, Instant};

use desk_core::buffer::Buffer;
use desk_ui::component::control;
use desk_ui::components::chat::{self, Block};
use desk_ui::components::code_editor::CodeEditor;
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::form::{TextInput, segmented};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::size::{FONT_BODY, FONT_SMALL, RADIUS_CHIP, RADIUS_ROW, T1, T2};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::ICON_SMALL;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, Entity, EntityInputHandler,
    FocusHandle, Focusable, KeyDownEvent, PathPromptOptions, Render, ScrollHandle, SharedString,
    Stateful, Subscription, Task, Window, div, prelude::*, px, relative,
};

use desk_plugin::Capability;

use crate::plugins::{Plugin, consent};

const NOTES: &str = "notes";
const OPEN_NOTE: &str = "open";
const SAVE_IDLE: Duration = Duration::from_millis(700);
const LIST_WIDTH: f32 = 200.0;
const MODES: [&str; 2] = ["Raw", "Rendered"];
const TOGGLE_KEY: &str = "e";
const NEW_NOTE: &str = "New note";
const OPEN_FILE: &str = "Open file...";
const RENAME: &str = "Rename";
const DELETE: &str = "Delete";
const ACTIONS: [&str; 2] = [NEW_NOTE, OPEN_FILE];
const TOOL: f32 = 26.0;
const LIST_SHARE: f32 = 0.35;

pub fn mount(plugin: &'static Plugin, cx: &mut App) -> AnyView {
    cx.new(|cx| Notepad::new(plugin, cx)).into()
}

struct Page {
    path: PathBuf,
    editor: Entity<CodeEditor>,
    saved: Rc<RefCell<String>>,
    seen: String,
    view: View,
    scroll: ScrollHandle,
    _edited: Subscription,
}

enum View {
    Raw,
    Rendered(Vec<Block>),
}

fn rendered(editor: &Entity<CodeEditor>, cx: &App) -> View {
    let started = Instant::now();
    let text = editor.read(cx).buffer().text();
    let blocks = chat::markdown(&text);
    eprintln!(
        "desk: notepad rendered {} bytes in {} ms",
        text.len(),
        started.elapsed().as_millis()
    );
    View::Rendered(blocks)
}

pub struct Notepad {
    plugin: &'static Plugin,
    folder: Result<PathBuf, String>,
    notes: Vec<PathBuf>,
    rendered: BTreeSet<PathBuf>,
    focus: FocusHandle,
    page: Option<Page>,
    renaming: Option<Entity<TextInput>>,
    problem: Option<SharedString>,
    saving: Option<Task<()>>,
}

fn write(
    plugin: &Plugin,
    path: &Path,
    buffer: &Buffer,
    saved: &RefCell<String>,
) -> Result<(), String> {
    if *buffer.rope() == *saved.borrow() {
        return Ok(());
    }
    let what = format!("write {}", path.display());
    consent::check(&plugin.id, &plugin.folder()?, Capability::FsWrite, &what)?;
    let started = Instant::now();
    let text = buffer.text();
    fs::write(path, &text).map_err(|error| format!("{}: {error}", path.display()))?;
    eprintln!(
        "desk: notepad saved {} in {} ms",
        path.display(),
        started.elapsed().as_millis()
    );
    *saved.borrow_mut() = text;
    Ok(())
}

fn tool(
    label: &'static str,
    mark: Glyph,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Stateful<Div> {
    let face = control(SharedString::from(format!("notepad-{label}")), label, theme)
        .size(px(TOOL))
        .rounded(px(RADIUS_CHIP))
        .child(glyph(mark, ICON_SMALL, theme.color(ColorToken::TextIcon)));
    tooltip(
        SharedString::from(format!("notepad-{label}-tip")),
        face,
        Edge::Frame,
        label,
        theme,
        window,
        cx,
    )
}

fn one_name(name: &str) -> bool {
    let mut parts = Path::new(name).components();
    matches!(
        (parts.next(), parts.next()),
        (Some(Component::Normal(_)), None)
    )
}

impl Notepad {
    fn new(plugin: &'static Plugin, cx: &mut Context<Self>) -> Self {
        let folder = plugin.folder().and_then(|folder| {
            let notes = folder.join(NOTES);
            fs::create_dir_all(&notes).map_err(|error| format!("{}: {error}", notes.display()))?;
            Ok(folder)
        });
        cx.on_release(|notepad, cx| notepad.save_now(cx)).detach();
        cx.on_app_quit(|notepad, cx| {
            notepad.save_now(cx);
            async {}
        })
        .detach();
        let mut notepad = Notepad {
            plugin,
            folder,
            notes: Vec::new(),
            rendered: BTreeSet::new(),
            focus: cx.focus_handle(),
            page: None,
            renaming: None,
            problem: None,
            saving: None,
        };
        notepad.list();
        notepad.restore(cx);
        notepad
    }

    fn notes_folder(&self) -> Option<PathBuf> {
        self.folder.as_ref().ok().map(|folder| folder.join(NOTES))
    }

    fn fail(&mut self, problem: String) {
        eprintln!("desk: notepad: {problem}");
        self.problem = Some(problem.into());
    }

    fn list(&mut self) {
        let Some(notes) = self.notes_folder() else {
            return;
        };
        match fs::read_dir(&notes) {
            Ok(entries) => {
                self.notes = entries
                    .filter_map(Result::ok)
                    .map(|entry| entry.path())
                    .filter(|path| path.is_file())
                    .collect();
                self.notes.sort();
            }
            Err(error) => self.fail(format!("{}: {error}", notes.display())),
        }
    }

    fn restore(&mut self, cx: &mut Context<Self>) {
        let Ok(folder) = &self.folder else {
            return;
        };
        let remembered = folder.join(OPEN_NOTE);
        let state = match fs::read_to_string(&remembered) {
            Ok(state) => state,
            Err(error) if error.kind() == ErrorKind::NotFound => return,
            Err(error) => return self.fail(format!("{}: {error}", remembered.display())),
        };
        let mut lines = state.lines().map(str::trim);
        let open = lines.next().unwrap_or_default();
        self.rendered = lines
            .filter(|line| !line.is_empty())
            .map(PathBuf::from)
            .collect();
        match Path::new(open).is_file() {
            true => self.show(PathBuf::from(open), cx),
            false if open.is_empty() => {}
            false => eprintln!("desk: notepad: {open} is gone, so nothing opens"),
        }
    }

    fn remember(&mut self) {
        let Ok(folder) = &self.folder else {
            return;
        };
        let remembered = folder.join(OPEN_NOTE);
        let open = self.page.as_ref().map_or(Path::new(""), |page| &page.path);
        let state: Vec<String> = std::iter::once(open)
            .chain(self.rendered.iter().map(PathBuf::as_path))
            .map(|path| path.to_string_lossy().into_owned())
            .collect();
        if let Err(error) = fs::write(&remembered, state.join("\n")) {
            self.fail(format!("{}: {error}", remembered.display()));
        }
    }

    fn show(&mut self, path: PathBuf, cx: &mut Context<Self>) {
        self.save_now(cx);
        self.renaming = None;
        let editor = match CodeEditor::plain(&path, cx) {
            Ok(editor) => editor,
            Err(error) => {
                self.fail(error);
                return cx.notify();
            }
        };
        let text = editor.buffer().text();
        let saved = Rc::new(RefCell::new(text.clone()));
        let (plugin, target, written) = (self.plugin, path.clone(), saved.clone());
        let editor = cx
            .new(|_| editor.on_save(move |buffer, _, _| write(plugin, &target, buffer, &written)));
        let edited = cx.observe(&editor, |notepad: &mut Notepad, _, cx| notepad.edited(cx));
        eprintln!("desk: notepad opened {}", path.display());
        let view = match self.rendered.contains(&path) {
            true => rendered(&editor, cx),
            false => View::Raw,
        };
        self.page = Some(Page {
            path,
            editor,
            saved,
            seen: text,
            view,
            scroll: ScrollHandle::new(),
            _edited: edited,
        });
        self.problem = None;
        self.remember();
        cx.notify();
    }

    fn save_now(&mut self, cx: &App) {
        self.saving = None;
        let Some(page) = &self.page else {
            return;
        };
        let written = write(
            self.plugin,
            &page.path,
            page.editor.read(cx).buffer(),
            &page.saved,
        );
        if let Err(error) = written {
            self.fail(error);
        }
    }

    fn edited(&mut self, cx: &mut Context<Self>) {
        let Some(page) = &mut self.page else {
            return;
        };
        let buffer = page.editor.read(cx).buffer();
        if *buffer.rope() == page.seen {
            return;
        }
        page.seen = buffer.text();
        self.saving = Some(cx.spawn(async move |notepad, cx| {
            cx.background_executor().timer(SAVE_IDLE).await;
            if notepad
                .update(cx, |notepad, cx| notepad.save_now(cx))
                .is_err()
            {
                eprintln!("desk: notepad closed before its idle save");
            }
        }));
    }

    fn set_view(&mut self, to_rendered: bool, window: &mut Window, cx: &mut Context<Self>) {
        let Some(page) = &mut self.page else {
            return;
        };
        if matches!(page.view, View::Rendered(_)) == to_rendered {
            return;
        }
        match to_rendered {
            true => {
                page.view = rendered(&page.editor, cx);
                self.rendered.insert(page.path.clone());
                window.focus(&self.focus, cx);
            }
            false => {
                page.view = View::Raw;
                self.rendered.remove(&page.path);
                window.focus(&page.editor.focus_handle(cx), cx);
            }
        }
        self.remember();
        cx.notify();
    }

    fn toggle_key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let keys = &event.keystroke;
        let Some(page) = &self.page else {
            return;
        };
        if !keys.modifiers.control || keys.key != TOGGLE_KEY {
            return;
        }
        cx.stop_propagation();
        let to_rendered = matches!(page.view, View::Raw);
        self.set_view(to_rendered, window, cx);
    }

    fn new_note(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(notes) = self.notes_folder() else {
            return;
        };
        let created = (1..=u32::MAX)
            .map(|at| notes.join(format!("note-{at}.md")))
            .find_map(
                |path| match OpenOptions::new().write(true).create_new(true).open(&path) {
                    Ok(_) => Some(Ok(path)),
                    Err(error) if error.kind() == ErrorKind::AlreadyExists => None,
                    Err(error) => Some(Err(format!("{}: {error}", path.display()))),
                },
            );
        let path = match created {
            Some(Ok(path)) => path,
            Some(Err(error)) => return self.fail(error),
            None => return self.fail(format!("{} has no free note name", notes.display())),
        };
        eprintln!("desk: notepad new note {}", path.display());
        self.list();
        self.show(path, cx);
        if let Some(page) = &self.page {
            window.focus(&page.editor.focus_handle(cx), cx);
        }
    }

    fn open_file(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let asked = cx.prompt_for_paths(PathPromptOptions {
            files: true,
            directories: false,
            multiple: false,
            prompt: Some("Open in Notepad".into()),
        });
        cx.spawn_in(window, async move |notepad, cx| {
            let path = match asked.await {
                Ok(Ok(Some(mut picked))) => picked.pop(),
                Ok(Ok(None)) => None,
                Ok(Err(error)) => {
                    eprintln!("desk: notepad: the file dialog failed: {error}");
                    None
                }
                Err(_) => {
                    eprintln!("desk: notepad: the file dialog closed without an answer");
                    None
                }
            };
            let Some(path) = path else {
                return eprintln!("desk: notepad: no file was picked");
            };
            if notepad
                .update(cx, |notepad, cx| notepad.show(path, cx))
                .is_err()
            {
                eprintln!("desk: notepad closed before the pick landed");
            }
        })
        .detach();
    }

    fn is_note(&self, path: &Path) -> bool {
        self.notes_folder().as_deref() == path.parent()
    }

    fn start_rename(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(page) = &self.page else {
            return;
        };
        let name = page
            .path
            .file_name()
            .map(|name| name.to_string_lossy().into_owned())
            .unwrap_or_default();
        let input = TextInput::new("Note name".into(), window, cx);
        input.update(cx, |input, cx| {
            input.replace_text_in_range(None, &name, window, cx)
        });
        window.focus(&input.focus_handle(cx), cx);
        self.renaming = Some(input);
        cx.notify();
    }

    fn rename_key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let enter = match event.keystroke.key.as_str() {
            "enter" => true,
            "escape" => false,
            _ => return,
        };
        cx.stop_propagation();
        let Some(input) = self.renaming.take() else {
            return;
        };
        cx.notify();
        let name = input.read(cx).text().trim().to_owned();
        let Some(from) = self.page.as_ref().map(|page| page.path.clone()) else {
            return;
        };
        if !enter {
            return eprintln!("desk: notepad: rename of {} cancelled", from.display());
        }
        if !one_name(&name) {
            return self.fail(format!("{name:?} is not a file name"));
        }
        let to = from.with_file_name(&name);
        if to.exists() {
            return self.fail(format!("{} already exists", to.display()));
        }
        self.save_now(cx);
        if let Err(error) = fs::rename(&from, &to) {
            return self.fail(format!("{} -> {}: {error}", from.display(), to.display()));
        }
        eprintln!(
            "desk: notepad renamed {} -> {}",
            from.display(),
            to.display()
        );
        if self.rendered.remove(&from) {
            self.rendered.insert(to.clone());
        }
        self.page = None;
        self.list();
        self.show(to, cx);
        match &self.page {
            Some(Page {
                view: View::Raw,
                editor,
                ..
            }) => window.focus(&editor.focus_handle(cx), cx),
            _ => window.focus(&self.focus, cx),
        }
    }

    fn delete(&mut self, cx: &mut Context<Self>) {
        let Some(page) = self.page.take() else {
            return;
        };
        self.saving = None;
        self.renaming = None;
        match fs::remove_file(&page.path) {
            Ok(()) => eprintln!("desk: notepad deleted {}", page.path.display()),
            Err(error) => self.fail(format!("{}: {error}", page.path.display())),
        }
        self.rendered.remove(&page.path);
        self.list();
        self.remember();
        cx.notify();
    }

    fn rows(&self, theme: &Theme, cx: &mut Context<Self>) -> Vec<AnyElement> {
        let open = self.page.as_ref().map(|page| page.path.as_path());
        let hover = theme.color(ColorToken::StateHover);
        self.notes
            .iter()
            .enumerate()
            .map(|(at, path)| {
                let name = path
                    .file_name()
                    .map(|name| name.to_string_lossy().into_owned())
                    .unwrap_or_default();
                let picked = path.clone();
                div()
                    .id(("notepad-note", at))
                    .px_2()
                    .py_1()
                    .rounded(px(RADIUS_ROW))
                    .truncate()
                    .cursor_pointer()
                    .text_size(px(FONT_BODY))
                    .text_color(ink(theme, if open == Some(path) { T1 } else { T2 }))
                    .when(open == Some(path), |row| row.bg(hover))
                    .hover(move |style| style.bg(hover))
                    .on_click(cx.listener(move |notepad, _: &ClickEvent, _, cx| {
                        notepad.show(picked.clone(), cx)
                    }))
                    .child(name)
                    .into_any_element()
            })
            .collect()
    }

    fn header(&self, theme: &Theme, window: &mut Window, cx: &mut Context<Self>) -> AnyElement {
        let title: SharedString = match &self.page {
            Some(page) if self.is_note(&page.path) => page
                .path
                .file_name()
                .map(|name| name.to_string_lossy().into_owned())
                .unwrap_or_default()
                .into(),
            Some(page) => page.path.display().to_string().into(),
            None => "No note open".into(),
        };
        let note = self
            .page
            .as_ref()
            .is_some_and(|page| self.is_note(&page.path));
        let name = match &self.renaming {
            Some(input) => div()
                .flex_1()
                .min_w_0()
                .overflow_hidden()
                .on_key_down(cx.listener(Self::rename_key))
                .child(input.clone())
                .into_any_element(),
            None => div()
                .flex_1()
                .min_w_0()
                .truncate()
                .text_size(px(FONT_BODY))
                .text_color(ink(theme, T1))
                .child(title)
                .into_any_element(),
        };
        let selected = match self.page.as_ref().map(|page| &page.view) {
            Some(View::Rendered(_)) => 1,
            _ => 0,
        };
        let this = cx.weak_entity();
        let modes = segmented(
            "notepad-mode",
            &MODES,
            selected,
            theme,
            move |at, window, cx| {
                if this
                    .update(cx, |notepad, cx| notepad.set_view(*at == 1, window, cx))
                    .is_err()
                {
                    eprintln!("desk: notepad is gone");
                }
            },
        );
        let new = tool(NEW_NOTE, Glyph::FileAdd, theme, window, cx).on_click(
            cx.listener(|notepad, _: &ClickEvent, window, cx| notepad.new_note(window, cx)),
        );
        let open = tool(OPEN_FILE, Glyph::Folder, theme, window, cx).on_click(
            cx.listener(|notepad, _: &ClickEvent, window, cx| notepad.open_file(window, cx)),
        );
        let edits = (note && self.renaming.is_none()).then(|| {
            [
                tool(RENAME, Glyph::Pencil, theme, window, cx).on_click(cx.listener(
                    |notepad, _: &ClickEvent, window, cx| notepad.start_rename(window, cx),
                )),
                tool(DELETE, Glyph::Trash, theme, window, cx)
                    .on_click(cx.listener(|notepad, _: &ClickEvent, _, cx| notepad.delete(cx))),
            ]
        });
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap_1()
            .px_2()
            .py_1()
            .min_w_0()
            .child(new)
            .child(open)
            .child(name)
            .children(edits.into_iter().flatten())
            .child(modes.flex_none())
            .into_any_element()
    }

    fn body(&self, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        if let Err(error) = &self.folder {
            return empty_state(
                "notepad-no-folder",
                "Notepad has no folder",
                Some(error.clone().into()),
                &[],
                &[],
                theme,
                |_, _, _| {},
            )
            .into_any_element();
        }
        match &self.page {
            Some(Page {
                view: View::Raw,
                editor,
                ..
            }) => div()
                .flex_1()
                .min_h_0()
                .child(editor.clone())
                .into_any_element(),
            Some(Page {
                view: View::Rendered(blocks),
                scroll,
                ..
            }) => div()
                .id("notepad-rendered")
                .flex_1()
                .min_h_0()
                .min_w_0()
                .p_3()
                .overflow_y_scroll()
                .track_scroll(scroll)
                .on_mouse_down(
                    gpui::MouseButton::Left,
                    cx.listener(|notepad, _, window, cx| window.focus(&notepad.focus, cx)),
                )
                .child(chat::document("notepad-document", blocks, &[], theme))
                .into_any_element(),
            None => {
                let actions: Vec<EmptyAction> = ACTIONS
                    .iter()
                    .map(|label| EmptyAction {
                        label: (*label).into(),
                        glyph: None,
                        keys: None,
                    })
                    .collect();
                let this = cx.weak_entity();
                empty_state(
                    "notepad-empty",
                    "No note open",
                    Some("Pick a note, start a new one, or open any file.".into()),
                    &actions,
                    &[],
                    theme,
                    move |at, window, cx| {
                        let picked = this.update(cx, |notepad, cx| match at {
                            0 => notepad.new_note(window, cx),
                            _ => notepad.open_file(window, cx),
                        });
                        if picked.is_err() {
                            eprintln!("desk: notepad is gone");
                        }
                    },
                )
                .into_any_element()
            }
        }
    }
}

impl Render for Notepad {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let list = div()
            .id("notepad-notes")
            .flex()
            .flex_col()
            .flex_none()
            .w(relative(LIST_SHARE))
            .max_w(px(LIST_WIDTH))
            .h_full()
            .p_1()
            .overflow_y_scroll()
            .children(self.rows(&theme, cx));
        let problem = self.problem.clone().map(|problem| {
            div()
                .px_2()
                .truncate()
                .text_size(px(FONT_SMALL))
                .text_color(theme.color(ColorToken::StatusDanger))
                .child(problem)
        });
        let main = div()
            .flex()
            .flex_col()
            .flex_1()
            .min_w_0()
            .h_full()
            .children(problem)
            .child(self.body(&theme, cx));
        let header = self.header(&theme, window, cx);
        div()
            .size_full()
            .flex()
            .flex_col()
            .min_w_0()
            .track_focus(&self.focus)
            .capture_key_down(cx.listener(Self::toggle_key))
            .child(header)
            .child(
                div()
                    .flex()
                    .flex_1()
                    .min_h_0()
                    .min_w_0()
                    .child(list)
                    .child(main),
            )
    }
}
