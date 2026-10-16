mod shelf;
mod side;

use std::collections::BTreeSet;
use std::fs::{self, OpenOptions};
use std::io::ErrorKind;
use std::path::{Component, Path, PathBuf};
use std::time::{Duration, Instant};

use desk_core::home::Home;
use desk_ui::component::{control, icon};
use desk_ui::components::chat::{self, Block};
use desk_ui::components::code_editor::CodeEditor;
use desk_ui::components::empty::{EmptyAction, empty_state};
use desk_ui::components::form::segmented;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::{glyph, ink};
use desk_ui::components::sheet::Sheet;
use desk_ui::components::size::{FONT_BODY, FONT_SMALL, NUMBER_COLUMN, RADIUS_CHIP, T1};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::icon::Icon;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::ICON_SMALL;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, Entity, FocusHandle, Focusable,
    Global, KeyDownEvent, PathPromptOptions, Render, ScrollHandle, SharedString, Stateful,
    Subscription, Task, WeakEntity, Window, canvas, div, prelude::*, px,
};
use serde_json::{Map, Value};

use desk_plugin::Capability;

use crate::plugins::{Plugin, consent};
use shelf::Shelf;
use side::{Menu, Naming};

const NOTES: &str = "notes";
const DRAFT: &str = "untitled.md";
const OPEN_NOTE: &str = "open";
const SETTINGS_FILE: &str = "settings.json";
const ASK_KEY: &str = "notepad.ask_on_close";
const SAVE_IDLE: Duration = Duration::from_millis(700);
const MODES: [&str; 2] = ["Raw", "Rendered"];
const TOGGLE_KEY: &str = "e";
const NEW_NOTE: &str = "New note";
const OPEN_FILE: &str = "Open file...";
const NOTES_FOLDER: &str = "Open the notes folder";
const SIDEBAR: &str = "Show or hide the notes";
const NUMBERS: &str = "Show or hide line numbers";
const CLOSE: &str = "Close note";
const UNTITLED: &str = "Untitled";
const UNSAVED_BODY: &str = "Your changes are lost if you close without saving.";
const TOOL: f32 = 26.0;

pub fn mount(plugin: &'static Plugin, cx: &mut App) -> AnyView {
    let started = Instant::now();
    cx.new(|cx| Notepad::new(plugin, started, cx)).into()
}

#[derive(Default)]
struct Link {
    live: Option<WeakEntity<Notepad>>,
    untitled: bool,
}

impl Global for Link {}

pub fn untitled(cx: &mut App) {
    let live = cx
        .try_global::<Link>()
        .and_then(|link| link.live.as_ref()?.upgrade());
    match live {
        Some(notepad) => notepad.update(cx, |notepad, cx| notepad.leave(Next::Untitled, cx)),
        None => cx.default_global::<Link>().untitled = true,
    }
}

struct Page {
    path: Option<PathBuf>,
    editor: Entity<CodeEditor>,
    saved: String,
    seen: String,
    dirty: bool,
    view: View,
    scroll: ScrollHandle,
    _edited: Subscription,
}

enum View {
    Raw,
    Rendered(Vec<Block>),
}

enum Next {
    Untitled,
    Open(PathBuf),
}

enum Asking {
    Leave(Next),
    Delete(PathBuf),
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
    shelf: Shelf,
    shut: BTreeSet<PathBuf>,
    rendered: BTreeSet<PathBuf>,
    focus: FocusHandle,
    page: Option<Page>,
    naming: Option<Naming>,
    menu: Menu,
    asking: Option<Asking>,
    asks: bool,
    sidebar: bool,
    numbers: bool,
    focus_pending: bool,
    opened: Option<Instant>,
    problem: Option<SharedString>,
    saving: Option<Task<()>>,
}

fn write_file(plugin: &Plugin, path: &Path, text: &str) -> Result<(), String> {
    let what = format!("write {}", path.display());
    consent::check(&plugin.id, &plugin.folder()?, Capability::FsWrite, &what)?;
    let started = Instant::now();
    fs::write(path, text).map_err(|error| format!("{}: {error}", path.display()))?;
    eprintln!(
        "desk: notepad saved {} in {} ms",
        path.display(),
        started.elapsed().as_millis()
    );
    Ok(())
}

fn free_note(notes: &Path) -> Result<PathBuf, String> {
    let created = (1..=u32::MAX)
        .map(|at| notes.join(format!("note-{at}.md")))
        .find_map(
            |path| match OpenOptions::new().write(true).create_new(true).open(&path) {
                Ok(_) => Some(Ok(path)),
                Err(error) if error.kind() == ErrorKind::AlreadyExists => None,
                Err(error) => Some(Err(format!("{}: {error}", path.display()))),
            },
        );
    created.unwrap_or_else(|| Err(format!("{} has no free note name", notes.display())))
}

fn asks_on_close() -> bool {
    let read = Home::find()
        .map_err(|error| error.to_string())
        .and_then(|home| {
            let path = home.config.join(SETTINGS_FILE);
            match fs::read_to_string(&path) {
                Ok(text) => serde_json::from_str::<Map<String, Value>>(&text)
                    .map_err(|error| format!("{}: {error}", path.display()))
                    .map(|values| values.get(ASK_KEY).cloned()),
                Err(error) if error.kind() == ErrorKind::NotFound => Ok(None),
                Err(error) => Err(format!("{}: {error}", path.display())),
            }
        });
    match read {
        Ok(None) => true,
        Ok(Some(Value::Bool(asks))) => asks,
        Ok(Some(other)) => {
            eprintln!("desk: notepad asks before closing, {ASK_KEY} holds {other}");
            true
        }
        Err(error) => {
            eprintln!("desk: notepad asks before closing, the settings did not read: {error}");
            true
        }
    }
}

fn tool(
    label: &'static str,
    mark: impl IntoElement,
    on: bool,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Stateful<Div> {
    let face = control(SharedString::from(format!("notepad-{label}")), label, theme)
        .size(px(TOOL))
        .rounded(px(RADIUS_CHIP))
        .when(on, |face| face.bg(theme.color(ColorToken::StateActive)))
        .child(mark);
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

fn answered(
    cx: &mut Context<Notepad>,
    act: fn(&mut Notepad, &mut Context<Notepad>),
) -> impl Fn(&mut Window, &mut App) + 'static {
    let notepad = cx.weak_entity();
    move |_, cx| {
        if let Err(error) = notepad.update(cx, act) {
            eprintln!("desk: notepad is gone before its alert answered: {error}");
        }
    }
}

fn one_name(name: &str) -> bool {
    let mut parts = Path::new(name).components();
    matches!(
        (parts.next(), parts.next()),
        (Some(Component::Normal(_)), None)
    )
}

impl Notepad {
    fn new(plugin: &'static Plugin, started: Instant, cx: &mut Context<Self>) -> Self {
        let folder = plugin.folder().and_then(|folder| {
            let notes = folder.join(NOTES);
            fs::create_dir_all(&notes).map_err(|error| format!("{}: {error}", notes.display()))?;
            Ok(folder)
        });
        let notes = folder
            .as_ref()
            .map(|folder| folder.join(NOTES))
            .unwrap_or_default();
        cx.on_release(|notepad, cx| notepad.keep(cx)).detach();
        cx.on_app_quit(|notepad, cx| {
            notepad.keep(cx);
            async {}
        })
        .detach();
        let mut notepad = Notepad {
            plugin,
            folder,
            shelf: Shelf::new(notes),
            shut: BTreeSet::new(),
            rendered: BTreeSet::new(),
            focus: cx.focus_handle(),
            page: None,
            naming: None,
            menu: Menu::default(),
            asking: None,
            asks: asks_on_close(),
            sidebar: true,
            numbers: true,
            focus_pending: true,
            opened: Some(started),
            problem: None,
            saving: None,
        };
        notepad.list();
        notepad.restore(cx);
        let weak = cx.weak_entity();
        let link = cx.default_global::<Link>();
        link.live = Some(weak);
        if std::mem::take(&mut link.untitled) {
            notepad.leave(Next::Untitled, cx);
        }
        notepad
    }

    fn draft(&self) -> Option<PathBuf> {
        self.folder.as_ref().ok().map(|folder| folder.join(DRAFT))
    }

    fn fail(&mut self, problem: String) {
        eprintln!("desk: notepad: {problem}");
        self.problem = Some(problem.into());
    }

    fn list(&mut self) {
        if self.folder.is_err() {
            return;
        }
        if let Err(error) = self.shelf.reload() {
            self.fail(error);
        }
    }

    fn restore(&mut self, cx: &mut Context<Self>) {
        let Ok(folder) = &self.folder else {
            return;
        };
        let remembered = folder.join(OPEN_NOTE);
        let state = match fs::read_to_string(&remembered) {
            Ok(state) => state,
            Err(error) if error.kind() == ErrorKind::NotFound => String::new(),
            Err(error) => {
                self.fail(format!("{}: {error}", remembered.display()));
                String::new()
            }
        };
        let mut lines = state.lines().map(str::trim);
        let open = lines.next().unwrap_or_default();
        self.rendered = lines
            .filter(|line| !line.is_empty())
            .map(PathBuf::from)
            .collect();
        match Path::new(open).is_file() {
            true => self.load(Some(PathBuf::from(open)), cx),
            false if open.is_empty() => self.load(None, cx),
            false => {
                eprintln!("desk: notepad: {open} is gone, so an untitled note opens");
                self.load(None, cx);
            }
        }
    }

    fn remember(&mut self) {
        let Ok(folder) = &self.folder else {
            return;
        };
        let remembered = folder.join(OPEN_NOTE);
        let open = self
            .page
            .as_ref()
            .and_then(|page| page.path.as_deref())
            .unwrap_or(Path::new(""));
        let state: Vec<String> = std::iter::once(open)
            .chain(self.rendered.iter().map(PathBuf::as_path))
            .map(|path| path.to_string_lossy().into_owned())
            .collect();
        if let Err(error) = fs::write(&remembered, state.join("\n")) {
            self.fail(format!("{}: {error}", remembered.display()));
        }
    }

    fn clear_draft(&mut self) {
        let Some(draft) = self.draft() else {
            return;
        };
        if let Err(error) = fs::write(&draft, "") {
            self.fail(format!("{}: {error}", draft.display()));
        }
    }

    fn load(&mut self, path: Option<PathBuf>, cx: &mut Context<Self>) {
        self.saving = None;
        let Some(draft) = self.draft() else {
            return;
        };
        let shown = path.clone().unwrap_or(draft);
        if path.is_none()
            && let Err(error) = OpenOptions::new().create(true).append(true).open(&shown)
        {
            self.fail(format!("{}: {error}", shown.display()));
            return cx.notify();
        }
        let editor = match CodeEditor::open(&shown, cx) {
            Ok(editor) => editor,
            Err(error) => {
                self.fail(error);
                return cx.notify();
            }
        };
        let text = editor.buffer().text();
        let saved = match path {
            Some(_) => text.clone(),
            None => String::new(),
        };
        let notepad = cx.weak_entity();
        let editor = cx.new(|cx| {
            let mut editor = editor.on_save(move |buffer, _, cx| {
                let text = buffer.text();
                notepad
                    .update(cx, |notepad, cx| notepad.save(text, cx))
                    .map_err(|error| error.to_string())
                    .and_then(|saved| saved)
            });
            editor.colour_rest(cx);
            editor
        });
        let edited = cx.observe(&editor, |notepad: &mut Notepad, _, cx| notepad.edited(cx));
        eprintln!("desk: notepad opened {}", shown.display());
        let view = match path
            .as_ref()
            .is_some_and(|path| self.rendered.contains(path))
        {
            true => rendered(&editor, cx),
            false => View::Raw,
        };
        self.page = Some(Page {
            path,
            editor,
            dirty: text != saved,
            saved,
            seen: text,
            view,
            scroll: ScrollHandle::new(),
            _edited: edited,
        });
        self.problem = None;
        self.focus_pending = true;
        self.remember();
        cx.notify();
    }

    fn store(&mut self, text: String) -> Result<bool, String> {
        let plugin = self.plugin;
        let notes = self.shelf.root().to_path_buf();
        let Some(page) = &mut self.page else {
            return Ok(false);
        };
        if text == page.saved {
            return Ok(false);
        }
        let named = page.path.is_none();
        let path = match &page.path {
            Some(path) => path.clone(),
            None => free_note(&notes)?,
        };
        write_file(plugin, &path, &text)?;
        page.path = Some(path);
        page.dirty = text != page.seen;
        page.saved = text;
        Ok(named)
    }

    fn save(&mut self, text: String, cx: &mut Context<Self>) -> Result<(), String> {
        if self.store(text)? {
            self.clear_draft();
            self.list();
            self.remember();
        }
        cx.notify();
        Ok(())
    }

    fn save_open(&mut self, cx: &mut Context<Self>) -> Result<(), String> {
        let Some(page) = &self.page else {
            return Ok(());
        };
        let text = page.editor.read(cx).buffer().text();
        self.save(text, cx)
    }

    fn keep(&mut self, cx: &App) {
        self.saving = None;
        let Some(page) = &self.page else {
            return;
        };
        let text = page.editor.read(cx).buffer().text();
        let kept = match (page.path.is_some(), self.draft()) {
            (true, _) => self.store(text).map(|_| ()),
            (false, Some(draft)) => {
                fs::write(&draft, text).map_err(|error| format!("{}: {error}", draft.display()))
            }
            (false, None) => Ok(()),
        };
        if let Err(error) = kept {
            self.fail(error);
        }
    }

    fn edited(&mut self, cx: &mut Context<Self>) {
        let asks = self.asks;
        let Some(page) = &mut self.page else {
            return;
        };
        let buffer = page.editor.read(cx).buffer();
        if *buffer.rope() == page.seen {
            return;
        }
        page.seen = buffer.text();
        let dirty = page.seen != page.saved;
        if dirty != page.dirty {
            page.dirty = dirty;
            cx.notify();
        }
        if asks || !dirty {
            return;
        }
        self.saving = Some(cx.spawn(async move |notepad, cx| {
            cx.background_executor().timer(SAVE_IDLE).await;
            let saved = notepad.update(cx, |notepad, cx| {
                if let Err(error) = notepad.save_open(cx) {
                    notepad.fail(error);
                }
            });
            if saved.is_err() {
                eprintln!("desk: notepad closed before its idle save");
            }
        }));
    }

    fn leave(&mut self, next: Next, cx: &mut Context<Self>) {
        let open = self.page.as_ref().and_then(|page| page.path.as_ref());
        if let (Next::Open(path), Some(open)) = (&next, open)
            && path == open
        {
            return;
        }
        let dirty = self.page.as_ref().is_some_and(|page| page.dirty);
        if dirty && self.asks {
            eprintln!("desk: notepad asks before closing {}", self.title());
            self.asking = Some(Asking::Leave(next));
            return cx.notify();
        }
        if dirty && let Err(error) = self.save_open(cx) {
            self.fail(error);
            return cx.notify();
        }
        self.go(next, cx);
    }

    fn go(&mut self, next: Next, cx: &mut Context<Self>) {
        match next {
            Next::Untitled => {
                self.clear_draft();
                self.load(None, cx);
            }
            Next::Open(path) => self.load(Some(path), cx),
        }
    }

    fn save_and_leave(&mut self, cx: &mut Context<Self>) {
        let Some(Asking::Leave(next)) = self.asking.take() else {
            return;
        };
        match self.save_open(cx) {
            Ok(()) => self.go(next, cx),
            Err(error) => {
                self.fail(error);
                cx.notify();
            }
        }
    }

    fn discard_and_leave(&mut self, cx: &mut Context<Self>) {
        let Some(Asking::Leave(next)) = self.asking.take() else {
            return;
        };
        eprintln!("desk: notepad closes {} without saving", self.title());
        if self.page.as_ref().is_some_and(|page| page.path.is_none()) {
            self.clear_draft();
        }
        self.go(next, cx);
    }

    fn keep_open(&mut self, cx: &mut Context<Self>) {
        self.asking = None;
        cx.notify();
    }

    fn delete_confirmed(&mut self, cx: &mut Context<Self>) {
        let Some(Asking::Delete(path)) = self.asking.take() else {
            return;
        };
        self.remove(&path, cx);
    }

    fn shown(&self, path: &Path) -> String {
        match path.strip_prefix(self.shelf.root()) {
            Ok(rest) if rest.as_os_str().is_empty() => "Notes".to_owned(),
            Ok(rest) => rest
                .components()
                .map(|part| part.as_os_str().to_string_lossy())
                .collect::<Vec<_>>()
                .join("/"),
            Err(_) => path.display().to_string(),
        }
    }

    fn title(&self) -> String {
        match self.page.as_ref().map(|page| page.path.as_deref()) {
            Some(Some(path)) => self.shown(path),
            Some(None) => UNTITLED.to_owned(),
            None => "No note open".to_owned(),
        }
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
                self.rendered.extend(page.path.clone());
                window.focus(&self.focus, cx);
            }
            false => {
                page.view = View::Raw;
                if let Some(path) = &page.path {
                    self.rendered.remove(path);
                }
                window.focus(&page.editor.focus_handle(cx), cx);
            }
        }
        self.remember();
        cx.notify();
    }

    fn toggle_key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let keys = &event.keystroke;
        if self.asking.is_some() && keys.key == "escape" {
            cx.stop_propagation();
            return self.keep_open(cx);
        }
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
                .update(cx, |notepad, cx| notepad.leave(Next::Open(path), cx))
                .is_err()
            {
                eprintln!("desk: notepad closed before the pick landed");
            }
        })
        .detach();
    }

    fn open_notes(&mut self, cx: &mut Context<Self>) {
        let notes = self.shelf.root();
        eprintln!(
            "desk: notepad shows {} in the file manager",
            notes.display()
        );
        cx.open_with_system(notes);
    }

    fn header(&self, theme: &Theme, window: &mut Window, cx: &mut Context<Self>) -> AnyElement {
        let dirty = self.page.as_ref().is_some_and(|page| page.dirty);
        let title = match dirty {
            true => format!("\u{2022} {}", self.title()),
            false => self.title(),
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
        let mark = theme.color(ColorToken::TextIcon);
        let sidebar = tool(
            SIDEBAR,
            icon(Icon::Sidebar, ICON_SMALL, mark),
            self.sidebar,
            theme,
            window,
            cx,
        )
        .on_click(cx.listener(|notepad, _: &ClickEvent, _, cx| {
            notepad.sidebar = !notepad.sidebar;
            eprintln!("desk: notepad sidebar shown {}", notepad.sidebar);
            cx.notify();
        }));
        let new = tool(
            NEW_NOTE,
            glyph(Glyph::FileAdd, ICON_SMALL, mark),
            false,
            theme,
            window,
            cx,
        )
        .on_click(cx.listener(|notepad, _: &ClickEvent, _, cx| notepad.leave(Next::Untitled, cx)));
        let open = tool(
            OPEN_FILE,
            glyph(Glyph::File, ICON_SMALL, mark),
            false,
            theme,
            window,
            cx,
        )
        .on_click(cx.listener(|notepad, _: &ClickEvent, window, cx| notepad.open_file(window, cx)));
        let notes = tool(
            NOTES_FOLDER,
            glyph(Glyph::Folder, ICON_SMALL, mark),
            false,
            theme,
            window,
            cx,
        )
        .on_click(cx.listener(|notepad, _: &ClickEvent, _, cx| notepad.open_notes(cx)));
        let numbers = tool(
            NUMBERS,
            div().text_size(px(FONT_SMALL)).text_color(mark).child("#"),
            self.numbers,
            theme,
            window,
            cx,
        )
        .on_click(cx.listener(|notepad, _: &ClickEvent, _, cx| {
            notepad.numbers = !notepad.numbers;
            eprintln!("desk: notepad line numbers shown {}", notepad.numbers);
            cx.notify();
        }));
        let close = tool(
            CLOSE,
            icon(Icon::Close, ICON_SMALL, mark),
            false,
            theme,
            window,
            cx,
        )
        .on_click(cx.listener(|notepad, _: &ClickEvent, _, cx| notepad.leave(Next::Untitled, cx)));
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap_1()
            .px_2()
            .py_1()
            .min_w_0()
            .bg(theme.color(ColorToken::CardsOuterFill))
            .border_b_1()
            .border_color(theme.color(ColorToken::Separator))
            .child(sidebar)
            .child(new)
            .child(open)
            .child(notes)
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .px_1()
                    .truncate()
                    .text_size(px(FONT_BODY))
                    .text_color(ink(theme, T1))
                    .child(title),
            )
            .child(numbers)
            .child(close)
            .child(modes.flex_none())
            .into_any_element()
    }

    fn empty(
        title: &'static str,
        line: String,
        labels: &[&'static str],
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let actions: Vec<EmptyAction> = labels
            .iter()
            .map(|label| EmptyAction {
                label: (*label).into(),
                glyph: None,
                keys: None,
            })
            .collect();
        let this = cx.weak_entity();
        let state = empty_state(
            "notepad-empty",
            title,
            Some(line.into()),
            &actions,
            &[],
            theme,
            move |at, window, cx| {
                let picked = this.update(cx, |notepad, cx| match at {
                    0 => notepad.go(Next::Untitled, cx),
                    _ => notepad.open_file(window, cx),
                });
                if picked.is_err() {
                    eprintln!("desk: notepad is gone");
                }
            },
        );
        div()
            .flex()
            .flex_1()
            .min_h_0()
            .items_center()
            .justify_center()
            .child(state)
            .into_any_element()
    }

    fn body(&self, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let surface = theme.color(ColorToken::CardsInnerFill);
        match (&self.folder, &self.page) {
            (Err(error), _) => Self::empty("Notepad has no folder", error.clone(), &[], theme, cx),
            (Ok(_), None) => Self::empty(
                "No note open",
                "Start a new note or open any file.".to_owned(),
                &[NEW_NOTE, OPEN_FILE],
                theme,
                cx,
            ),
            (
                Ok(_),
                Some(Page {
                    view: View::Rendered(blocks),
                    scroll,
                    ..
                }),
            ) => div()
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
            (Ok(_), Some(Page { editor, .. })) => div()
                .relative()
                .flex_1()
                .min_h_0()
                .child(editor.clone())
                .when(!self.numbers, |body| {
                    body.child(
                        div()
                            .absolute()
                            .top_0()
                            .left_0()
                            .h_full()
                            .w(px(NUMBER_COLUMN))
                            .bg(surface)
                            .occlude(),
                    )
                })
                .into_any_element(),
        }
    }

    fn alerts(&self, cx: &mut Context<Self>) -> [AnyElement; 2] {
        let leaving = matches!(self.asking, Some(Asking::Leave(_)));
        let deleting = match &self.asking {
            Some(Asking::Delete(path)) => Some((self.shown(path), path.is_dir())),
            _ => None,
        };
        let unsaved = Sheet::new(
            "notepad-unsaved",
            format!("Save changes to {}?", self.title()),
        )
        .modal()
        .open(leaving)
        .confirm("Save")
        .on_confirm(answered(cx, Self::save_and_leave))
        .aside("Don't save", answered(cx, Self::discard_and_leave))
        .on_cancel(answered(cx, Self::keep_open))
        .child(div().truncate().child(UNSAVED_BODY));
        let gone = match deleting {
            Some((_, true)) => "The folder and every note in it are deleted for good.",
            _ => "The note is deleted for good.",
        };
        let delete = Sheet::new(
            "notepad-delete",
            format!(
                "Delete {}?",
                deleting.map(|(name, _)| name).unwrap_or_default()
            ),
        )
        .modal()
        .open(matches!(self.asking, Some(Asking::Delete(_))))
        .confirm("Delete")
        .on_confirm(answered(cx, Self::delete_confirmed))
        .on_cancel(answered(cx, Self::keep_open))
        .child(div().truncate().child(gone));
        [unsaved.into_any_element(), delete.into_any_element()]
    }
}

impl Render for Notepad {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let raw = match &self.page {
            Some(Page {
                view: View::Raw,
                editor,
                ..
            }) => Some(editor.focus_handle(cx)),
            _ => None,
        };
        if std::mem::take(&mut self.focus_pending) {
            window.focus(raw.as_ref().unwrap_or(&self.focus), cx);
        }
        let opened = self.opened.take().filter(|_| raw.is_some()).map(|started| {
            canvas(
                |_, _, _| {},
                move |_, (), _, _| {
                    eprintln!(
                        "desk: notepad open to caret {} ms",
                        started.elapsed().as_millis()
                    );
                },
            )
            .absolute()
            .size_0()
        });
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
            .bg(theme.color(ColorToken::CardsInnerFill))
            .children(problem)
            .child(self.body(&theme, cx));
        let side = self.sidebar.then(|| self.side(&theme, cx));
        let header = self.header(&theme, window, cx);
        div()
            .size_full()
            .relative()
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
                    .children(side)
                    .child(main),
            )
            .children(opened)
            .children(self.alerts(cx))
    }
}

impl Focusable for Notepad {
    fn focus_handle(&self, cx: &App) -> FocusHandle {
        match &self.page {
            Some(Page {
                view: View::Raw,
                editor,
                ..
            }) => editor.focus_handle(cx),
            _ => self.focus.clone(),
        }
    }
}
