pub(crate) mod catalog;
mod preview;

use std::cell::Cell;
use std::ops::Range;
use std::rc::Rc;
use std::sync::Arc;

use desk_core::control::TELL_BADGE;
use desk_ui::components::card::{caption, header_button, inner_card, shell};
use desk_ui::components::chat::{Block, document, markdown};
use desk_ui::components::chip::{flat_chip, mono};
use desk_ui::components::color_picker::{self, ColorPicked, ColorPicker, to_hex};
use desk_ui::components::form::{TextArea, switch_bare};
use desk_ui::components::list::{HoverList, row};
use desk_ui::components::overlay::{Dismissed, MenuButton, MenuItem, context_menu, toast};
use desk_ui::components::paint::{ink, ring};
use desk_ui::components::scroll::{ScrollArea, Scrollbar, list_scrollbar, scrollbar};
use desk_ui::components::size::T3;
use desk_ui::effects::controls::{self, Dial, Settle, Unit};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{BorderToken, ColorToken, FlagToken, NumberToken, Theme, WordToken};
use gpui::{
    AnyElement, AnyView, App, AppContext, Bounds, ClickEvent, Context, Div, Entity, Focusable,
    ListAlignment, ListOffset, ListState, MouseButton, PathPromptOptions, Pixels, Render, Rgba,
    ScrollHandle, SharedString, StyleRefinement, Subscription, UniformListScrollHandle, WeakEntity,
    Window, anchored, canvas, div, list, prelude::*, px, rgb_to_hsla, uniform_list,
};
use serde_json::Value;

use crate::pick;
use crate::screens::frame;
use catalog::{Draft, Entry};

const BASE: &str = "tofu-glass";
const LIST_WIDTH: f32 = 280.0;
const TOKENS_WIDTH: f32 = 380.0;
const OTHER: &str = "other";
const PICKER_POPOVER: &str = "theme-picker-pop";
const SWATCH: f32 = 12.0;
const SWATCH_RING: f32 = 0.12;
const LABEL_WIDTH: f32 = 110.0;
const TOKEN_ROW: f32 = 24.0;
const PREVIEW_OVERDRAW: f32 = 0.0;
pub(super) const CARD_PAD: f32 = 12.0;
const CARD_GAP: f32 = 8.0;
const HIGHLIGHT_PATH: [&str; 3] = ["cards", "inner", "highlight"];

#[derive(Clone, Copy)]
struct Knob {
    id: &'static str,
    label: &'static str,
    path: [&'static str; 2],
    most: f32,
    step: f32,
    unit: Unit,
    json: fn(f32) -> Value,
}

const TINT: Knob = Knob {
    id: "theme-knob-tint",
    label: "tint",
    path: ["glass", "tint"],
    most: 1.0,
    step: 0.01,
    unit: Unit::Percent,
    json: |value| Value::from((f64::from(value) * 100.0).round() / 100.0),
};
const NUMBERS: [(&str, &str, NumberToken, &[u8]); 3] = [
    (
        "radius",
        "theme-outer-radius",
        NumberToken::CardsOuterRadius,
        &[0, 4, 8, 10, 12, 16, 20],
    ),
    (
        "gap to inner",
        "theme-outer-gap",
        NumberToken::CardsOuterGap,
        &[0, 1, 2, 3, 4, 6, 8],
    ),
    (
        "radius",
        "theme-inner-radius",
        NumberToken::CardsInnerRadius,
        &[0, 4, 8, 10, 11, 12, 16],
    ),
];

#[derive(Clone, Copy, PartialEq)]
enum Pane {
    Tokens,
    Json,
}

struct Editing {
    key: SharedString,
    path: &'static str,
    trigger: Rc<Cell<Bounds<Pixels>>>,
    picker: Entity<ColorPicker>,
    _watch: [Subscription; 2],
}

enum RowEdit {
    Renaming(String, Entity<TextArea>),
    Deleting(String),
}

#[derive(Clone, Copy)]
enum TokenRow {
    Group(&'static str),
    Token(ColorToken),
}

#[derive(Clone, Copy, PartialEq)]
enum Part {
    List,
    Shape,
    Preview,
    Tokens,
    Lit,
    Bars,
    Picker,
}

const SCROLLED: [Part; 3] = [Part::List, Part::Shape, Part::Preview];

impl Part {
    fn id(self) -> &'static str {
        match self {
            Part::List => "theme-list",
            Part::Shape => "theme-shape",
            Part::Preview => "theme-preview",
            Part::Tokens => "theme-tokens",
            Part::Lit => "theme-lit",
            Part::Bars => "theme-bars",
            Part::Picker => "theme-picker",
        }
    }
}

struct Area {
    screen: WeakEntity<ThemeScreen>,
    part: Part,
    _redrawn: Subscription,
}

impl Area {
    fn new(part: Part, cx: &mut Context<ThemeScreen>) -> Entity<Area> {
        let screen = cx.entity();
        cx.new(|cx| Area {
            screen: screen.downgrade(),
            part,
            _redrawn: cx.observe(&screen, |_, _, cx| cx.notify()),
        })
    }
}

impl Render for Area {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let part = self.part;
        desk_perf::span!(part.id());
        self.screen
            .update(cx, |screen, cx| screen.area(part, cx))
            .unwrap_or_else(|_| div())
    }
}

struct Areas {
    list: Entity<Area>,
    shape: Entity<Area>,
    preview: Entity<Area>,
    tokens: Entity<Area>,
    lit: Entity<Area>,
    bars: Entity<Area>,
    picker: Entity<Area>,
}

impl Areas {
    fn of(&self, part: Part) -> &Entity<Area> {
        match part {
            Part::List => &self.list,
            Part::Shape => &self.shape,
            Part::Preview => &self.preview,
            Part::Tokens => &self.tokens,
            Part::Lit => &self.lit,
            Part::Bars => &self.bars,
            Part::Picker => &self.picker,
        }
    }
}

#[derive(Clone)]
struct Scrolls {
    list: ScrollHandle,
    shape: ScrollHandle,
    preview: ListState,
    drawn: Rc<Cell<[Pixels; 3]>>,
}

impl Scrolls {
    fn now(&self) -> [Pixels; 3] {
        [
            self.list.offset().y,
            self.shape.offset().y,
            self.preview.scroll_px_offset_for_scrollbar().y,
        ]
    }

    fn noted(&self, part: Part) -> impl IntoElement + use<> {
        let scrolls = self.clone();
        canvas(
            move |_, _, _| {
                let mut drawn = scrolls.drawn.get();
                for ((scrolled, drawn), now) in SCROLLED.iter().zip(&mut drawn).zip(scrolls.now()) {
                    if *scrolled == part {
                        *drawn = now;
                    }
                }
                scrolls.drawn.set(drawn);
            },
            |_, (), _, _| {},
        )
        .absolute()
        .size_full()
    }
}

struct ThemeScreen {
    areas: Areas,
    entries: Vec<Entry>,
    active: String,
    pane: Pane,
    numbers: Vec<Entity<MenuButton>>,
    editing: Option<Editing>,
    row_edit: Option<RowEdit>,
    draft: Option<Draft>,
    unsaved: bool,
    preview: Rc<preview::Shown>,
    scrolls: Scrolls,
    preview_theme: Option<Arc<Theme>>,
    spots: Rc<preview::Spots>,
    rows: Rc<[TokenRow]>,
    token_scroll: UniformListScrollHandle,
    lit: Option<ColorToken>,
    json_view: Option<(SharedString, Rc<[Block]>)>,
    told: Option<SharedString>,
    _redrawn: Subscription,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(other) = board.filter(|board| *board != "ITHEME-1") {
        return Err(format!("the theme screen draws ITHEME-1, not {other}"));
    }
    let entries = catalog::read(&pick::folders()?)?;
    let active = pick::chosen()?.theme;
    Ok(cx
        .new(|cx| {
            let screen = cx.entity().downgrade();
            let numbers = NUMBERS
                .iter()
                .map(|(_, id, token, values)| {
                    let (id, token, values) = (*id, *token, *values);
                    let items = values
                        .iter()
                        .map(|value| MenuItem::Action {
                            label: value.to_string().into(),
                            keys: None,
                            icon: None,
                        })
                        .collect();
                    let menu = MenuButton::new(id.into(), items, cx);
                    let screen = screen.clone();
                    menu.update(cx, |menu, _| {
                        menu.trigger(move |_, theme| {
                            flat_chip(id, theme.number(token).to_string(), theme)
                        });
                        menu.on_pick(move |at, _, cx| {
                            let Some(value) = values.get(*at).copied() else {
                                return;
                            };
                            let landed = screen.update(cx, |screen: &mut ThemeScreen, cx| {
                                let path: Vec<&str> = token.path().split('.').collect();
                                screen.commit(&path, Value::from(value), cx);
                            });
                            if landed.is_err() {
                                eprintln!("desk: theme: the screen closed before the pick landed");
                            }
                        });
                    });
                    menu
                })
                .collect();
            let redrawn = cx.observe_global::<ActiveTheme>(|screen, cx| {
                if !screen.unsaved {
                    screen.draft = None;
                    cx.notify();
                }
            });
            ThemeScreen {
                areas: Areas {
                    list: Area::new(Part::List, cx),
                    shape: Area::new(Part::Shape, cx),
                    preview: Area::new(Part::Preview, cx),
                    tokens: Area::new(Part::Tokens, cx),
                    lit: Area::new(Part::Lit, cx),
                    bars: Area::new(Part::Bars, cx),
                    picker: Area::new(Part::Picker, cx),
                },
                entries,
                active,
                pane: Pane::Tokens,
                numbers,
                editing: None,
                row_edit: None,
                draft: None,
                unsaved: false,
                preview: Rc::new(preview::Shown::new()),
                scrolls: Scrolls {
                    list: ScrollHandle::new(),
                    shape: ScrollHandle::new(),
                    preview: ListState::new(
                        preview::ITEMS,
                        ListAlignment::Top,
                        px(PREVIEW_OVERDRAW),
                    ),
                    drawn: Rc::default(),
                },
                preview_theme: None,
                spots: Rc::default(),
                rows: token_rows(),
                token_scroll: UniformListScrollHandle::new(),
                lit: None,
                json_view: None,
                told: None,
                _redrawn: redrawn,
            }
        })
        .into())
}

impl ThemeScreen {
    fn tell(&mut self, outcome: Result<(), String>, cx: &mut Context<Self>) {
        self.told = outcome.err().map(|error| {
            eprintln!("desk: theme: {error}");
            error.into()
        });
        cx.notify();
    }

    fn shown(&self, cx: &App) -> Arc<Theme> {
        self.draft
            .as_ref()
            .map_or_else(|| ActiveTheme::theme(cx), |draft| draft.theme.clone())
    }

    fn chrome(&self, cx: &App) -> Arc<Theme> {
        match self.unsaved {
            true => ActiveTheme::theme(cx),
            false => self.shown(cx),
        }
    }

    fn area(&mut self, part: Part, cx: &mut Context<Self>) -> Div {
        let theme = match part {
            Part::Preview | Part::Lit => self.shown(cx),
            _ => self.chrome(cx),
        };
        let (body, scroll) = match part {
            Part::List => (self.themes(&theme, cx), &self.scrolls.list),
            Part::Shape => (self.picked(&theme, cx), &self.scrolls.shape),
            Part::Preview => return self.preview_pane(theme),
            Part::Tokens => return self.tokens(&theme, cx),
            Part::Lit => {
                let view = self.scrolls.preview.viewport_bounds();
                return preview::rings(&self.spots, self.lit, view, &theme);
            }
            Part::Bars => return self.bars(cx),
            Part::Picker => return self.picker(cx),
        };
        inner_card(&theme)
            .size_full()
            .child(
                div()
                    .id(part.id())
                    .flex_1()
                    .min_h_0()
                    .w_full()
                    .overflow_y_scroll()
                    .track_scroll(scroll)
                    .child(body.p(px(CARD_PAD))),
            )
            .child(self.scrolls.noted(part))
    }

    fn bars(&self, cx: &mut Context<Self>) -> Div {
        let drawn = self.scrolls.drawn.get();
        for ((part, drawn), now) in SCROLLED.iter().zip(drawn).zip(self.scrolls.now()) {
            if drawn != now {
                let area = self.areas.of(*part).clone();
                cx.defer(move |cx| Self::redraw(&area, cx));
            }
        }
        let placed = |view: Bounds<Pixels>, bar: Scrollbar| {
            anchored()
                .position(view.origin)
                .child(div().w(view.size.width).h(view.size.height).child(bar))
        };
        let scrolls = &self.scrolls;
        div()
            .size_full()
            .child(placed(
                scrolls.list.bounds(),
                scrollbar("theme-list-bar", &scrolls.list),
            ))
            .child(placed(
                scrolls.shape.bounds(),
                scrollbar("theme-shape-bar", &scrolls.shape),
            ))
            .child(placed(
                scrolls.preview.viewport_bounds(),
                list_scrollbar("theme-preview-bar", &scrolls.preview),
            ))
    }

    fn preview_pane(&mut self, theme: Arc<Theme>) -> Div {
        if !self
            .preview_theme
            .as_ref()
            .is_some_and(|drawn| Arc::ptr_eq(drawn, &theme))
        {
            self.scrolls.preview.remeasure();
            self.preview_theme = Some(theme.clone());
        }
        self.spots.clear();
        let shown = self.preview.clone();
        let spots = self.spots.clone();
        let painted = theme.clone();
        inner_card(&theme).size_full().child(
            div()
                .relative()
                .size_full()
                .child(
                    list(self.scrolls.preview.clone(), move |at, _, _| {
                        preview::item(at, &shown, &spots, &painted)
                    })
                    .size_full(),
                )
                .child(
                    div()
                        .id("theme-preview-still")
                        .absolute()
                        .inset_0()
                        .block_mouse_except_scroll(),
                )
                .child(self.scrolls.noted(Part::Preview)),
        )
    }

    fn redraw(area: &Entity<Area>, cx: &mut App) {
        area.update(cx, |_, cx| cx.notify());
    }

    fn choose(&mut self, file: String, cx: &mut Context<Self>) {
        self.editing = None;
        self.row_edit = None;
        self.draft = None;
        self.unsaved = false;
        let applied = self.switch(file, cx);
        self.tell(applied, cx);
    }

    fn reread(&mut self, cx: &mut App) -> Result<(), String> {
        self.entries = catalog::read(&pick::folders()?)?;
        cx.set_global(pick::Listing);
        Ok(())
    }

    fn switch(&mut self, file: String, cx: &mut App) -> Result<(), String> {
        self.reread(cx)?;
        if file != self.active {
            pick::start(&file, cx)?;
            self.active = file;
        }
        pick::set_mode(&self.active, None, cx)?;
        pick::save(&self.active, ActiveTheme::mode(cx))
    }

    fn entry(&self, file: &str) -> Result<&Entry, String> {
        self.entries
            .iter()
            .find(|entry| entry.file == file)
            .ok_or_else(|| format!("{file} is not a listed theme"))
    }

    fn added(&mut self, file: Result<String, String>, cx: &mut Context<Self>) {
        match file {
            Ok(file) => {
                eprintln!("desk: theme: {file}.json written to {}", shown_folder());
                self.choose(file, cx);
            }
            Err(error) => self.tell(Err(error), cx),
        }
    }

    fn duplicate(&mut self, cx: &mut Context<Self>) {
        let copied = self
            .entry(&self.active)
            .and_then(|entry| catalog::duplicate(&pick::person()?, entry));
        self.added(copied, cx);
    }

    fn create(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let made = self
            .entry(BASE)
            .and_then(|base| catalog::untitled(&pick::person()?, base));
        let made_one = made.is_ok();
        self.added(made, cx);
        if made_one {
            self.start_rename(self.active.clone(), window, cx);
        }
    }

    fn start_rename(&mut self, file: String, window: &mut Window, cx: &mut Context<Self>) {
        let Ok(entry) = self.entry(&file) else {
            return;
        };
        let placeholder = entry.label.clone();
        let screen = cx.entity().downgrade();
        let renamed = file.clone();
        let field = cx.new(|cx| {
            TextArea::new(placeholder, window, cx)
                .max_lines(1)
                .on_submit(move |name, _, cx| {
                    let name = name.to_owned();
                    let landed = screen.update(cx, |screen: &mut ThemeScreen, cx| {
                        screen.rename(&renamed, &name, cx)
                    });
                    if landed.is_err() {
                        eprintln!("desk: theme: the screen closed before the name landed");
                    }
                })
        });
        window.focus(&field.focus_handle(cx), cx);
        self.row_edit = Some(RowEdit::Renaming(file, field));
        cx.notify();
    }

    fn rename(&mut self, file: &str, name: &str, cx: &mut Context<Self>) {
        self.row_edit = None;
        if name.trim().is_empty() {
            return cx.notify();
        }
        let renamed = pick::person()
            .and_then(|dir| catalog::rename(&dir, file, name))
            .and_then(|renamed| match file == self.active {
                true => self.switch(renamed, cx),
                false => self.reread(cx),
            });
        self.tell(renamed, cx);
    }

    fn delete(&mut self, file: &str, cx: &mut Context<Self>) {
        self.row_edit = None;
        let deleted = pick::person()
            .and_then(|dir| catalog::delete(&dir, file))
            .and_then(|()| match file == self.active {
                true => self.switch(BASE.to_owned(), cx),
                false => self.reread(cx),
            });
        if deleted.is_ok() {
            eprintln!("desk: theme: {file}.json deleted from {}", shown_folder());
        }
        self.tell(deleted, cx);
    }

    fn row_menu(&mut self, file: &str, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        match at {
            0 => self.start_rename(file.to_owned(), window, cx),
            1 => {
                self.row_edit = Some(RowEdit::Deleting(file.to_owned()));
                cx.notify();
            }
            other => eprintln!("desk: theme: the row menu has no item {other}"),
        }
    }

    fn import(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let asked = cx.prompt_for_paths(PathPromptOptions {
            files: true,
            directories: false,
            multiple: false,
            prompt: Some("Import a theme".into()),
        });
        cx.spawn_in(window, async move |screen, cx| {
            let source = match asked.await {
                Ok(Ok(Some(mut picked))) => picked.pop(),
                Ok(Ok(None)) => None,
                Ok(Err(error)) => {
                    eprintln!("desk: theme: the file dialog failed: {error}");
                    None
                }
                Err(_) => {
                    eprintln!("desk: theme: the file dialog closed without an answer");
                    None
                }
            };
            let Some(source) = source else {
                return eprintln!("desk: theme: no file was picked to import");
            };
            let landed = screen.update(cx, |screen, cx| {
                let imported = pick::person().and_then(|folder| catalog::import(&folder, &source));
                screen.added(imported, cx)
            });
            if landed.is_err() {
                eprintln!("desk: theme: the screen closed before the import landed");
            }
        })
        .detach();
    }

    fn write(&mut self, path: &[&str], value: Value, cx: &mut App) -> Result<(), String> {
        let dir = pick::person()?;
        if !dir.join(format!("{}.json", self.active)).is_file() {
            catalog::adopt(&dir, self.entry(&self.active)?)?;
            catalog::set(&dir, &self.active, path, value)?;
            return self.switch(self.active.clone(), cx);
        }
        let text = catalog::set(&dir, &self.active, path, value)?;
        if let Some(entry) = self
            .entries
            .iter_mut()
            .find(|entry| entry.file == self.active)
        {
            entry.json = text;
        }
        Ok(())
    }

    fn try_on(
        &mut self,
        path: &[&str],
        value: Value,
        cx: &mut Context<Self>,
    ) -> Result<(), String> {
        let mode = ActiveTheme::mode(cx);
        let mut draft = match self.draft.take() {
            Some(draft) => draft,
            None => Draft::open(&pick::folders()?, self.entry(&self.active)?, mode)?,
        };
        let set = draft.set(path, value, mode);
        self.draft = Some(draft);
        set
    }

    fn moved(&mut self, path: &[&str], value: Value, cx: &mut Context<Self>) {
        self.unsaved = true;
        match self.try_on(path, value, cx) {
            Ok(()) => Self::redraw(&self.areas.preview, cx),
            Err(error) => self.tell(Err(error), cx),
        }
    }

    fn commit(&mut self, path: &[&str], value: Value, cx: &mut Context<Self>) {
        let shown = self.try_on(path, value.clone(), cx);
        self.unsaved = false;
        let written = self.write(path, value, cx);
        self.tell(shown.and(written), cx);
    }

    fn colored(&mut self, path: &str, picked: &ColorPicked, cx: &mut Context<Self>) {
        let mode = ActiveTheme::mode(cx).label();
        let at: Vec<&str> = std::iter::once(mode).chain(path.split('.')).collect();
        match picked {
            ColorPicked::Moving(color) => self.moved(&at, Value::String(to_hex(*color)), cx),
            ColorPicked::Settled(color) => self.commit(&at, Value::String(to_hex(*color)), cx),
        }
    }

    fn slid(&mut self, knob: Knob, value: f32, settle: Settle, cx: &mut Context<Self>) {
        let json = (knob.json)(value);
        match settle {
            Settle::Moving => self.moved(&knob.path, json, cx),
            Settle::Settled => self.commit(&knob.path, json, cx),
        }
        Self::redraw(&self.areas.shape, cx);
    }

    fn edit(
        &mut self,
        part: Part,
        key: SharedString,
        path: &'static str,
        color: Rgba,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let reopened = self
            .editing
            .as_ref()
            .is_some_and(|editing| editing.key == key);
        self.close(cx);
        if reopened {
            return;
        }
        let picker = cx.new(|cx| ColorPicker::new(rgb_to_hsla(color), window, cx));
        let picked = cx.subscribe(&picker, move |screen, _, picked, cx| {
            screen.colored(path, picked, cx)
        });
        let moved = cx.observe(self.areas.of(part), |screen, _, cx| {
            Self::redraw(&screen.areas.picker, cx)
        });
        self.editing = Some(Editing {
            key,
            path,
            trigger: Rc::default(),
            picker,
            _watch: [picked, moved],
        });
        cx.notify();
    }

    fn close(&mut self, cx: &mut Context<Self>) {
        let Some(editing) = self.editing.take() else {
            return;
        };
        if self.unsaved {
            let color = editing.picker.read(cx).color();
            self.colored(editing.path, &ColorPicked::Settled(color), cx);
        }
        cx.notify();
    }

    fn swatch(
        &self,
        part: Part,
        path: &'static str,
        color: Option<Rgba>,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let Some(color) = color else {
            return value(None, "none".into(), theme);
        };
        let key = SharedString::from(format!("{}.{path}", part.id()));
        let open = self.editing.as_ref().filter(|editing| editing.key == key);
        div()
            .id(SharedString::from(format!("swatch-{key}")))
            .relative()
            .cursor_pointer()
            .child(value(Some(color), catalog::hex(color), theme))
            .children(open.map(|editing| {
                let at = editing.trigger.clone();
                let picker = self.areas.picker.clone();
                canvas(
                    move |area, _, cx| {
                        if at.get() != area {
                            at.set(area);
                            let picker = picker.clone();
                            cx.defer(move |cx| Self::redraw(&picker, cx));
                        }
                    },
                    |_, (), _, _| {},
                )
                .absolute()
                .top_0()
                .left_0()
                .size_full()
            }))
            .on_click(cx.listener(move |screen, _: &ClickEvent, window, cx| {
                screen.edit(part, key.clone(), path, color, window, cx)
            }))
            .into_any_element()
    }

    fn picker(&self, cx: &mut Context<Self>) -> Div {
        let editing = self.editing.as_ref();
        let at = editing
            .map(|editing| editing.trigger.get())
            .unwrap_or_default();
        div().child(anchored().position(at.origin).child(color_picker::popover(
            PICKER_POPOVER,
            div().w(at.size.width).h(at.size.height),
            editing.map(|editing| editing.picker.clone()),
            cx.listener(|screen, _: &Dismissed, _, cx| screen.close(cx)),
        )))
    }

    fn slider(&self, knob: Knob, now: f32, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let weak = cx.weak_entity();
        let dial = Dial {
            label: knob.label,
            value: now,
            least: 0.0,
            most: knob.most,
            step: knob.step,
            unit: knob.unit,
        };
        controls::slider_tile(knob.id, dial, theme, move |value, settle, _, cx| {
            let landed = weak.update(cx, |screen, cx| screen.slid(knob, value, settle, cx));
            if let Err(error) = landed {
                eprintln!("desk: theme: the screen closed under a slider: {error}");
            }
        })
    }

    fn themes(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let rows: Vec<_> = self
            .entries
            .iter()
            .map(|entry| {
                let file = entry.file.clone();
                let (swatches, said) = match &entry.look {
                    Ok(look) => (
                        div()
                            .grid()
                            .grid_cols(2)
                            .gap(px(2.0))
                            .children(look.swatches.map(|color| swatch(color, theme))),
                        SharedString::from(look.mode),
                    ),
                    Err(problem) => (div(), problem.clone()),
                };
                let body = match &self.row_edit {
                    Some(RowEdit::Renaming(renamed, field)) if *renamed == entry.file => div()
                        .flex_1()
                        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                        .child(field.clone()),
                    Some(RowEdit::Deleting(doomed)) if *doomed == entry.file => {
                        self.confirm_delete(entry, theme, cx)
                    }
                    _ => div()
                        .flex()
                        .flex_col()
                        .flex_1()
                        .min_w_0()
                        .child(div().truncate().child(entry.label.clone()))
                        .child(frame::note(said, theme).truncate()),
                };
                let content = div()
                    .flex()
                    .items_center()
                    .gap_3()
                    .child(swatches.flex_none())
                    .child(body);
                let content = match entry.own {
                    true => {
                        let picked = file.clone();
                        context_menu(vec![
                            MenuItem::action("Rename"),
                            MenuItem::action("Delete").danger(true),
                        ])
                        .id(SharedString::from(format!("theme-menu-{}", entry.file)))
                        .on_pick(cx.listener(move |this, at: &usize, window, cx| {
                            this.row_menu(&picked, *at, window, cx)
                        }))
                        .flex_1()
                        .min_w_0()
                        .child(content)
                        .into_any_element()
                    }
                    false => content.flex_1().min_w_0().into_any_element(),
                };
                row(
                    SharedString::from(format!("theme-{}", entry.file)),
                    entry.file == self.active,
                    false,
                    theme,
                )
                .flex()
                .items_center()
                .py_1p5()
                .child(content)
                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    this.choose(file.clone(), cx);
                }))
            })
            .collect();
        let active = self
            .entries
            .iter()
            .position(|entry| entry.file == self.active);
        div()
            .flex()
            .flex_col()
            .gap_2()
            .child(heading(
                "Themes",
                format!("{} in the folder", self.entries.len()),
                theme,
            ))
            .child(HoverList::new("themes", theme).items(rows).selected(active))
    }

    fn confirm_delete(&self, entry: &Entry, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let doomed = entry.file.clone();
        div()
            .flex()
            .flex_col()
            .gap_1()
            .min_w_0()
            .child(frame::note(format!("Delete {}?", entry.label), theme))
            .child(
                div()
                    .flex()
                    .gap_1()
                    .child(
                        header_button("theme-delete-yes", "Delete", theme)
                            .text_color(theme.color(ColorToken::StatusDanger))
                            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                                cx.stop_propagation();
                                this.delete(&doomed, cx);
                            })),
                    )
                    .child(header_button("theme-delete-no", "Cancel", theme).on_click(
                        cx.listener(|this, _: &ClickEvent, _, cx| {
                            cx.stop_propagation();
                            this.row_edit = None;
                            cx.notify();
                        }),
                    )),
            )
    }

    fn picked(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let label = self.entry(&self.active).map_or_else(
            |_| SharedString::from(self.active.clone()),
            |entry| entry.label.clone(),
        );
        let filled = |token: ColorToken, cx: &mut Context<Self>| {
            self.swatch(
                Part::Shape,
                token.path(),
                Some(theme.color(token)),
                theme,
                cx,
            )
        };
        let border = |token: BorderToken, cx: &mut Context<Self>| {
            self.swatch(Part::Shape, token.path(), theme.border(token), theme, cx)
        };
        let shown = self.shown(cx);
        let lit = theme.flag(FlagToken::CardsInnerHighlight);
        let number = |at: usize| {
            let name = NUMBERS.get(at).map_or("", |(name, ..)| *name);
            shape(
                name,
                self.numbers.get(at).map_or_else(
                    || div().into_any_element(),
                    |menu| menu.clone().into_any_element(),
                ),
            )
        };
        div()
            .flex()
            .flex_col()
            .gap_2()
            .child(heading(
                label,
                "applied to this window as you pick".into(),
                theme,
            ))
            .child(caption("Glass", theme).pt_2())
            .child(controls::grid().child(self.slider(
                TINT,
                shown.number(NumberToken::GlassTint),
                theme,
                cx,
            )))
            .child(shape(
                "top light",
                switch_bare("theme-highlight", "Top light", lit, theme)
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.commit(&HIGHLIGHT_PATH, Value::Bool(!lit), cx)
                    }))
                    .into_any_element(),
            ))
            .child(caption("Outer card", theme).pt_2())
            .child(shape("fill", filled(ColorToken::CardsOuterFill, cx)))
            .child(shape("border", border(BorderToken::CardsOuter, cx)))
            .child(number(0))
            .child(number(1))
            .child(caption("Inner card", theme).pt_2())
            .child(shape("fill", filled(ColorToken::CardsInnerFill, cx)))
            .child(shape("border", border(BorderToken::CardsInner, cx)))
            .child(number(2))
    }

    fn json_blocks(&mut self) -> Rc<[Block]> {
        let json = self
            .entry(&self.active)
            .map(|entry| entry.json.clone())
            .unwrap_or_default();
        if let Some((shown, blocks)) = &self.json_view
            && *shown == json
        {
            return blocks.clone();
        }
        let blocks: Rc<[Block]> = markdown(&format!("```json\n{}\n```\n", json.trim_end())).into();
        self.json_view = Some((json, blocks.clone()));
        blocks
    }

    fn tokens(&mut self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let switch = frame::pills(
            "theme-pane",
            &[(Pane::Tokens, "Tokens"), (Pane::Json, "JSON")],
            self.pane,
            &frame::SHELL_PILLS,
            theme,
            cx,
            |this, pane| this.pane = pane,
        );
        let switch = div().flex().items_center().child(switch);
        let card = inner_card(theme).size_full();
        match self.pane {
            Pane::Tokens => {
                let base = self.token_scroll.0.borrow().base_handle.clone();
                card.child(
                    div()
                        .flex()
                        .flex_col()
                        .size_full()
                        .gap_2()
                        .py(px(CARD_PAD))
                        .child(switch.px(px(CARD_PAD)))
                        .child(
                            div()
                                .relative()
                                .flex_1()
                                .min_h_0()
                                .child(
                                    uniform_list(
                                        "theme-token-rows",
                                        self.rows.len(),
                                        cx.processor(Self::token_rows),
                                    )
                                    .track_scroll(&self.token_scroll)
                                    .size_full(),
                                )
                                .child(scrollbar("theme-token-bar", &base)),
                        ),
                )
            }
            Pane::Json => card.child(
                ScrollArea::new(Part::Tokens.id()).child(
                    div()
                        .flex()
                        .flex_col()
                        .gap_2()
                        .p(px(CARD_PAD))
                        .child(switch)
                        .child(document(
                            "theme-json",
                            &self.json_blocks(),
                            &[],
                            None,
                            theme,
                        )),
                ),
            ),
        }
    }

    fn token_rows(
        &mut self,
        range: Range<usize>,
        _: &mut Window,
        cx: &mut Context<Self>,
    ) -> Vec<AnyElement> {
        desk_perf::span!("theme-token-rows");
        let theme = self.chrome(cx);
        let rows = self.rows.clone();
        range
            .filter_map(|at| rows.get(at).copied())
            .map(|row| match row {
                TokenRow::Group(group) => caption(group, &theme)
                    .flex()
                    .items_end()
                    .h(px(TOKEN_ROW))
                    .px(px(CARD_PAD))
                    .into_any_element(),
                TokenRow::Token(token) => {
                    let color = theme.color(token);
                    shape(
                        preview::label(token),
                        self.swatch(Part::Tokens, token.path(), Some(color), &theme, cx),
                    )
                    .h(px(TOKEN_ROW))
                    .px(px(CARD_PAD))
                    .id(SharedString::from(format!("theme-token-{}", token.path())))
                    .on_hover(cx.listener(move |screen, hovered: &bool, _, cx| {
                        screen.light(token, *hovered, cx)
                    }))
                    .into_any_element()
                }
            })
            .collect()
    }

    fn light(&mut self, token: ColorToken, hovered: bool, cx: &mut Context<Self>) {
        let lit = match hovered {
            true => Some(token),
            false => self.lit.filter(|lit| *lit != token),
        };
        if lit == self.lit {
            return;
        }
        self.lit = lit;
        if let Some(token) = lit
            && !self
                .spots
                .shows(token, self.scrolls.preview.viewport_bounds())
            && let Some(at) = preview::item_of(token)
        {
            self.scrolls.preview.scroll_to(ListOffset {
                item_ix: at,
                offset_in_item: px(0.0),
            });
            Self::redraw(&self.areas.preview, cx);
            Self::redraw(&self.areas.bars, cx);
        }
        Self::redraw(&self.areas.lit, cx);
    }
}

fn token_rows() -> Rc<[TokenRow]> {
    let mut groups: Vec<(&'static str, Vec<ColorToken>)> = Vec::new();
    for token in ColorToken::ALL {
        let group = token
            .path()
            .split_once('.')
            .map_or(OTHER, |(group, _)| group);
        match groups.iter_mut().find(|(name, _)| *name == group) {
            Some((_, tokens)) => tokens.push(token),
            None => groups.push((group, vec![token])),
        }
    }
    groups
        .into_iter()
        .flat_map(|(group, tokens)| {
            std::iter::once(TokenRow::Group(group)).chain(tokens.into_iter().map(TokenRow::Token))
        })
        .collect()
}

fn shown_folder() -> String {
    pick::person_at().map_or_else(|error| error, |folder| folder.display().to_string())
}

pub(super) fn heading(title: impl Into<SharedString>, trailing: String, theme: &Theme) -> Div {
    div()
        .flex()
        .items_center()
        .gap_2()
        .child(caption(title, theme).flex_1())
        .child(frame::note(trailing, theme))
}

fn swatch(color: Rgba, theme: &Theme) -> Div {
    div()
        .flex_none()
        .size(px(SWATCH))
        .rounded(px(3.0))
        .bg(color)
        .shadow(vec![ring(ink(theme, SWATCH_RING))])
}

fn value(color: Option<Rgba>, said: String, theme: &Theme) -> AnyElement {
    div()
        .flex()
        .items_center()
        .gap_2()
        .children(color.map(|color| swatch(color, theme)))
        .child(
            div()
                .font_family(mono(theme))
                .text_size(px(12.0))
                .text_color(ink(theme, T3))
                .child(said),
        )
        .into_any_element()
}

fn shape(name: &str, value: AnyElement) -> Div {
    div()
        .flex()
        .items_center()
        .child(div().flex_none().w(px(LABEL_WIDTH)).child(name.to_owned()))
        .child(value)
}

impl Render for ThemeScreen {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = self.chrome(cx);
        let actions =
            div()
                .flex()
                .items_center()
                .gap_1()
                .child(header_button("theme-new", "New +", &theme).on_click(
                    cx.listener(|this, _: &ClickEvent, window, cx| this.create(window, cx)),
                ))
                .child(
                    header_button("theme-duplicate", "Duplicate as JSON", &theme)
                        .on_click(cx.listener(|this, _: &ClickEvent, _, cx| this.duplicate(cx))),
                )
                .child(header_button("theme-import", "Import", &theme).on_click(
                    cx.listener(|this, _: &ClickEvent, window, cx| this.import(window, cx)),
                ))
                .into_any_element();
        let told = self.told.clone().map(|message| {
            div()
                .absolute()
                .left_0()
                .right_0()
                .bottom_4()
                .flex()
                .justify_center()
                .child(toast(
                    message,
                    TELL_BADGE,
                    &theme,
                    cx.listener(|this, _: &ClickEvent, _, cx| {
                        this.told = None;
                        cx.notify();
                    }),
                ))
        });
        let top = div()
            .flex_1()
            .min_h_0()
            .flex()
            .gap(px(CARD_GAP))
            .child(
                self.areas
                    .list
                    .clone()
                    .cached(StyleRefinement::default().flex_none().w(px(LIST_WIDTH))),
            )
            .child(
                self.areas.shape.clone().cached(
                    StyleRefinement::default()
                        .flex_1()
                        .min_w_0()
                        .min_h_0()
                        .h_full(),
                ),
            );
        let left = div()
            .flex_1()
            .min_w_0()
            .min_h_0()
            .flex()
            .flex_col()
            .gap(px(CARD_GAP))
            .child(top)
            .child(
                self.areas
                    .preview
                    .clone()
                    .cached(StyleRefinement::default().flex_1().min_w_0().min_h_0()),
            );
        let body = div()
            .flex_1()
            .min_h_0()
            .flex()
            .gap(px(CARD_GAP))
            .font_family(theme.word(WordToken::ShapeFont))
            .child(left)
            .child(
                self.areas
                    .tokens
                    .clone()
                    .cached(StyleRefinement::default().flex_none().w(px(TOKENS_WIDTH))),
            );
        shell(frame::titled("theme", Some(actions)), &theme)
            .relative()
            .size_full()
            .min_w_0()
            .child(body)
            .child(
                self.areas
                    .bars
                    .clone()
                    .cached(StyleRefinement::default().absolute().size_full()),
            )
            .children(told)
            .child(
                self.areas
                    .lit
                    .clone()
                    .cached(StyleRefinement::default().absolute().size_full()),
            )
            .child(
                self.areas
                    .picker
                    .clone()
                    .cached(StyleRefinement::default().absolute().size_full()),
            )
    }
}
