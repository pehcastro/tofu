use std::cell::Cell;
use std::collections::{HashMap, HashSet};
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::time::Instant;

use desk_core::protocol::boardy::InitRequest;
use desk_core::protocol::request::{BoardyInit, QueryModels, QuerySettings, SettingsSet};
use desk_core::protocol::{
    self, ModelsQuery, NoParams, Request, SettingValue, SettingValueKind, SettingsReport,
    SettingsSetParams,
};
use desk_core::query::{Answer, QueryError, Read};
use desk_core::settings::{self as wire, Scope, Settings, Value};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{header_button, header_control};
use desk_ui::components::chip::{Tone, flat_chip};
use desk_ui::components::form::{FieldWidth, TextInput, switch_bare};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::list::{HoverList, group_header, row};
use desk_ui::components::logo::{Brand, logo};
use desk_ui::components::overlay::{MenuButton, MenuIcon, MenuItem, menu};
use desk_ui::components::paint::{glyph, ink, ms, presented, tint};
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::settings::{
    Badge, OnScope, SettingRow, SettingSection, Source, page_title, setting_section,
    setting_section_clickable,
};
use desk_ui::components::size::{
    CHIP_PAD, FIELD, FIELD_WIDTH, FONT_SMALL, HOVER, RADIUS_BADGE, T3,
};
use desk_ui::components::tooltip::{Edge, tooltip};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::ICON_SMALL;
use desk_ui::motion::{Phase, Presence, reduced_motion};
use desk_ui::theme::{ColorToken, NumberToken, Theme};
use gpui::{
    AnyElement, AnyWindowHandle, ClickEvent, Context, Div, Entity, EntityId, EntityInputHandler,
    Focusable, FontWeight, KeyDownEvent, MouseDownEvent, Render, SharedString, Stateful,
    StyleRefinement, Subscription, Transformation, WeakEntity, Window, canvas, deferred, div,
    prelude::*, px, radians, relative,
};

use super::appearance::Appearance;
use super::fixture::{
    DESK_NAV, DeskPage, FIXED_SETTINGS, Fixed, MODEL_SETTINGS, Models, PLUGINS_GENERAL,
    PLUGINS_NAV, PageId, Setting, TERMINAL_ONLY, TOFU_EVERYWHERE, TOFU_NAV,
};
use super::keys::KeysPage;
use super::plugins::PluginsPage;
use super::stored::Stored;
use super::terminal::TerminalPage;
use crate::desk::{Desk, Unpicked};
use crate::modules::chat::{Chat, SettingsMoved};
use crate::plugins;
use crate::screens::frame;
use crate::screens::work::split::{self, Spans, rooted, timed};

const NAV_WIDTH: f32 = 247.0;
const NAV_LEAST: f32 = 150.0;
const NAV_SHARE: f32 = 0.3;
const NAV_TEXT: f32 = 13.5;
const NAV_RULE: f32 = 0.35;
const CONTENT_PAD_X: f32 = 28.0;
const CONTENT_PAD_Y: f32 = 20.0;
const MENU_DROP: f32 = 34.0;
const PICKER_MENU: &str = "settings-picker";
const IN_USE: &str = "in use";
const SCOPE_PICKER: &str = "settings-scope";
const SCOPE_TIP: &str = "settings-scope-tip";
const NAV_ICON: f32 = 14.0;
const NAV_ICON_GAP: f32 = 8.0;
const SCOPES: [(Scope, &str); 2] = [(Scope::Project, "Project"), (Scope::Global, "Global")];
const SYNC_GAP: f32 = 8.0;
const SECONDS_PER_MINUTE: u64 = 60;
const MINUTES_PER_HOUR: u64 = 60;
const NUMBER_FIELD: f32 = 72.0;
const TEXT_LEAST: f32 = 120.0;
const MODEL_DEFAULT: &str = "Model default";
const WAITING: &str = "Waiting for tofu";
const SEARCH: &str = "Search settings";
const NAV_DIM: f32 = 0.35;
const STEM_LEAST: usize = 5;
const STEM_SLACK: usize = 2;
const SHOWN_MOST: usize = 24;
const CONTINUITY: [&str; 2] = ["continueOnSubscription", "subscriptionOrder"];
const NONE: &str = "None";
const ORDER_KEY: &str = "subscriptionOrder";
const ORDER_LOGO: f32 = 14.0;
const ORDER_ARROW: f32 = 12.0;
const ORDER_BUTTON: f32 = 22.0;
const ORDER_DIM: f32 = 0.35;
const RESTART: &str = "Needs restart";
const ROLES_PAGE: &str = "Models & roles";
const ROLES: &str = "Roles";
const TIERS: [(&str, &str); 4] = [
    ("modelTier.genius", "Genius"),
    ("modelTier.smart", "Smart"),
    ("modelTier.worker", "Worker"),
    ("modelTier.dumb", "Dumb"),
];
const PROJECTS: &str = "Projects";
const PROJECT_MANAGEMENT: &str = "Project management";
const MANAGED_KEY: &str = "projectManagement";
const ENABLED_ROW: &str = "projectManagement.enabled";
const ENABLED: &str = "Enabled";
const TOOL: &str = "Tool";
const OFF: &str = "off";
const CREATE_BOARD: &str = "Create a board";
const BOARDY_SCREEN: &str = "screen.boardy";
const BOARD_KEY_MOST: usize = 10;
const BRANDS: [(&str, &str); 5] = [
    ("boardy", "Boardy"),
    ("jira", "Jira"),
    ("linear", "Linear"),
    ("powershell", "PowerShell"),
    ("github", "GitHub"),
];
const PICKER_LEAST: f32 = 120.0;
const CHIP_GAP: f32 = 6.0;
const SOURCE_GAP: f32 = 6.0;
const SOURCE_TEXT: f32 = 11.5;
const NO_TOFU: &str = "Settings reads tofu's settings from the tofu the work screen runs, and no work screen is open here.";

pub struct Rows {
    page: PageId,
    scope: Scope,
    shown: Option<PageId>,
    menu: Presence,
    menu_for: Option<SharedString>,
    unfolded: HashSet<&'static str>,
    described: HashMap<String, Described>,
    models: Option<ModelsQuery>,
    model_menus: HashMap<String, ModelMenu>,
    scope_menu: Option<Entity<MenuButton>>,
    synced: Option<Instant>,
    appearance: Option<Entity<Appearance>>,
    custom: HashSet<&'static str>,
    tofu: Option<(WeakEntity<Chat>, EntityId)>,
    answer: Answer<Settings>,
    switching: HashMap<String, bool>,
    asked: HashMap<String, String>,
    problems: HashMap<String, SharedString>,
    board_opening: Option<Subscription>,
    fields: HashMap<String, Field>,
    shells: Option<Entity<TerminalPage>>,
    keys: Option<Entity<KeysPage>>,
    plugins: Option<(Entity<PluginsPage>, Subscription)>,
    stored: Result<Stored, String>,
    opened: Option<Instant>,
    showing: Rc<Cell<Option<Instant>>>,
    search: Option<(Entity<TextInput>, Subscription)>,
    query: String,
    typed: Option<(Instant, Spans)>,
    index: Vec<Entry>,
}

struct Entry {
    page: PageId,
    hit: Hit,
    text: String,
    stems: Vec<String>,
}

impl Entry {
    fn new(page: PageId, hit: Hit, texts: &[&str], key: &str) -> Self {
        let mut stems: Vec<String> = Vec::new();
        for letter in key.chars() {
            match stems.last_mut() {
                Some(stem) if !letter.is_uppercase() => stem.push(letter),
                _ => stems.push(letter.to_lowercase().collect()),
            }
        }
        stems.retain(|stem| stem.chars().count() >= STEM_LEAST);
        Entry {
            page,
            hit,
            text: texts.join("\n").to_lowercase(),
            stems,
        }
    }

    fn matches(&self, query: &str) -> bool {
        self.text.contains(query)
            || self.stems.iter().any(|stem| {
                query.starts_with(stem.as_str()) && query.len() <= stem.len() + STEM_SLACK
            })
    }
}

#[derive(Clone)]
enum Hit {
    Tofu(wire::Setting),
    Desk(&'static Setting),
    Plugin(&'static plugins::Plugin),
}

type Badged = (SettingRow, Option<Badge>);

struct ModelMenu {
    button: Entity<MenuButton>,
    key: String,
    source: Option<String>,
    choices: Vec<Option<String>>,
}

struct Described {
    label: SharedString,
    about: SharedString,
    list: bool,
    restart: bool,
    reason: Option<SharedString>,
}

impl Described {
    fn new(setting: &SettingValue) -> Self {
        let label = match setting.label.trim() {
            "" => sentence_case(&setting.key),
            label => label.to_owned(),
        };
        let about = match setting.description.trim() {
            "" => SharedString::default(),
            said => sentence(&capitalized(said)),
        };
        Described {
            label: label.into(),
            about,
            list: matches!(setting.kind, SettingValueKind::List),
            restart: setting.restart == Some(true),
            reason: setting
                .reason
                .as_deref()
                .map(str::trim)
                .filter(|said| !said.is_empty())
                .map(|said| sentence(&capitalized(said))),
        }
    }
}

struct Field {
    input: Entity<TextInput>,
    seen: Option<String>,
    _blur: Subscription,
}

impl Rows {
    pub fn new(theme: &Theme) -> Self {
        let mut rows = Rows {
            page: PageId::Desk(DeskPage::Appearance),
            scope: Scope::Project,
            shown: None,
            menu: Presence::new(
                ms(theme, NumberToken::MotionEnter),
                ms(theme, NumberToken::MotionExit),
            ),
            menu_for: None,
            unfolded: HashSet::from([DESK_NAV.0]),
            described: HashMap::new(),
            models: None,
            model_menus: HashMap::new(),
            scope_menu: None,
            synced: None,
            appearance: None,
            custom: HashSet::new(),
            tofu: None,
            answer: Answer::default(),
            switching: HashMap::new(),
            asked: HashMap::new(),
            problems: HashMap::new(),
            board_opening: None,
            fields: HashMap::new(),
            shells: None,
            keys: None,
            plugins: None,
            stored: Stored::load(),
            opened: None,
            showing: Rc::new(Cell::new(None)),
            search: None,
            query: String::new(),
            typed: None,
            index: Vec::new(),
        };
        rows.reindex();
        rows
    }

    pub fn showing(&mut self) {
        self.showing.set(Some(Instant::now()));
    }

    pub fn install_plugin(&mut self, folder: PathBuf, cx: &mut Context<Self>) {
        self.go(PageId::Plugins(None));
        self.plugins_page(cx)
            .update(cx, |page, cx| page.install_from(folder, cx));
        self.reindex();
    }

    fn plugins_page(&mut self, cx: &mut Context<Self>) -> Entity<PluginsPage> {
        if let Some((page, _)) = &self.plugins {
            return page.clone();
        }
        let page = cx.new(|_| PluginsPage::new());
        let watched = cx.observe(&page, |_, _, cx| cx.notify());
        self.plugins = Some((page.clone(), watched));
        page
    }

    pub fn read_from(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        if self
            .tofu
            .as_ref()
            .is_some_and(|(_, id)| *id == chat.entity_id())
        {
            return;
        }
        self.tofu = Some((chat.downgrade(), chat.entity_id()));
        self.answer.want();
        self.reread(cx);
    }

    pub fn reread(&mut self, cx: &mut Context<Self>) {
        self.answer.again();
        self.ask(cx);
        self.send::<QueryModels>(NoParams {}, cx, |rows, reply, cx| match reply {
            Ok(models) => {
                eprintln!("desk: settings read {} models", models.models.len());
                rows.models = Some(models);
                cx.notify();
            }
            Err(error) => eprintln!("desk: settings reads no models: {error}"),
        });
        cx.notify();
    }

    fn send<R: Request>(
        &self,
        params: R::Params,
        cx: &mut Context<Self>,
        then: impl FnOnce(&mut Self, Result<R::Result, String>, &mut Context<Self>) + 'static,
    ) where
        R::Result: 'static,
    {
        let chat = self.tofu.as_ref().and_then(|(chat, _)| chat.upgrade());
        let (sent, reply) = flume::bounded(1);
        match chat {
            Some(chat) => chat.update(cx, |chat, cx| {
                chat.request::<R>(&params, cx, move |_, reply, _| {
                    sent.send(reply).ok();
                });
            }),
            None => {
                sent.send(Err("the work screen that ran tofu is closed".to_owned()))
                    .ok();
            }
        }
        cx.spawn(async move |rows, cx| {
            let Ok(reply) = reply.recv_async().await else {
                return;
            };
            if let Err(error) = rows.update(cx, |rows, cx| then(rows, reply, cx)) {
                eprintln!(
                    "desk: settings closed before {} answered: {error}",
                    R::METHOD
                );
            }
        })
        .detach();
    }

    fn ask(&mut self, cx: &mut Context<Self>) {
        if !self.answer.take_due() {
            return;
        }
        eprintln!("desk: settings asks {}", QuerySettings::METHOD);
        self.send::<QuerySettings>(NoParams {}, cx, Self::landed);
    }

    fn landed(&mut self, reply: Result<SettingsReport, String>, cx: &mut Context<Self>) {
        if let Ok(report) = &reply {
            let unlabelled: Vec<&str> = report
                .settings
                .iter()
                .filter(|setting| setting.label.trim().is_empty())
                .map(|setting| setting.key.as_str())
                .collect();
            if !unlabelled.is_empty() {
                eprintln!(
                    "desk: settings: tofu sent no label for {}",
                    unlabelled.join(", ")
                );
            }
            self.described = report
                .settings
                .iter()
                .map(|setting| (setting.key.clone(), Described::new(setting)))
                .collect();
        }
        let read = reply
            .and_then(|report| Settings::try_from(report).map_err(|error| error.to_string()))
            .map(Read::now)
            .map_err(|reason| QueryError {
                method: QuerySettings::METHOD,
                reason,
            });
        match &read {
            Ok(read) => eprintln!(
                "desk: settings read {} categories at {}: {}",
                read.value.categories.len(),
                read.at,
                read.value
                    .categories
                    .iter()
                    .map(|category| format!("{} {}", category.name, category.settings.len()))
                    .collect::<Vec<_>>()
                    .join(", ")
            ),
            Err(error) => eprintln!("desk: settings: {error}"),
        }
        if let Ok(read) = &read {
            self.synced = Some(Instant::now());
            for (key, wanted) in self.asked.drain() {
                let Some(kept) = read.value.get(&key).map(|setting| setting.value.wire()) else {
                    continue;
                };
                if kept == wanted {
                    continue;
                }
                eprintln!("desk: settings {key} asked {wanted}, tofu keeps {kept}");
                if self
                    .described
                    .get(&key)
                    .is_none_or(|said| said.reason.is_none())
                {
                    self.problems.insert(
                        key,
                        format!("Tofu wrote {wanted} but keeps {kept} in force here.").into(),
                    );
                }
            }
        }
        self.switching.clear();
        self.answer.answered(read);
        self.reindex();
        cx.notify();
    }

    fn write(&mut self, key: String, value: String, cx: &mut Context<Self>) {
        self.write_at(key, value, self.scope, cx);
    }

    fn write_at(&mut self, key: String, value: String, scope: Scope, cx: &mut Context<Self>) {
        eprintln!("desk: settings set {key} {value} in {}", scope.wire());
        self.problems.remove(&key);
        self.asked.insert(key.clone(), value.clone());
        let params = SettingsSetParams {
            key: key.clone(),
            value,
            scope: Some(scope.wire().to_owned().into()),
        };
        self.send::<SettingsSet>(params, cx, move |rows, reply, cx| {
            rows.written(key, reply, cx)
        });
        cx.notify();
    }

    fn written(
        &mut self,
        key: String,
        reply: Result<protocol::SettingsSet, String>,
        cx: &mut Context<Self>,
    ) {
        match reply.err() {
            Some(problem) => {
                eprintln!("desk: settings {key} refused: {problem}");
                self.switching.remove(&key);
                self.asked.remove(&key);
                if let Some(field) = self.fields.get_mut(&key) {
                    field.seen = None;
                }
                self.problems.insert(key, sentence(&problem));
            }
            None => eprintln!("desk: settings {key} written"),
        }
        self.answer.again();
        self.ask(cx);
        cx.notify();
    }

    fn clicked(&mut self, page: &PageId, key: &str, window: &mut Window, cx: &mut Context<Self>) {
        match page {
            PageId::Tofu(_) => self.flip_tofu(key, cx),
            PageId::Desk(_) => self.flip_desk(key, cx),
            PageId::Plugins(_) => {
                self.go(page.clone());
                self.clear_search(window, cx);
                cx.notify();
            }
        }
    }

    fn go(&mut self, page: PageId) {
        self.unfolded.insert(nav_group(&page));
        self.page = page;
    }

    fn fold(&mut self, group: &'static str, cx: &mut Context<Self>) {
        let open = !self.unfolded.remove(group);
        if open {
            self.unfolded.insert(group);
        }
        eprintln!("desk: settings nav {group} open {open}");
        cx.notify();
    }

    fn flip_enabled(&mut self, cx: &mut Context<Self>) {
        let read = self.answer.read.as_ref();
        let Some(Value::Choice(picked, choices)) = read
            .and_then(|read| read.value.get(MANAGED_KEY))
            .map(|setting| &setting.value)
        else {
            return;
        };
        let on = self
            .switching
            .get(ENABLED_ROW)
            .copied()
            .unwrap_or(picked.as_str() != OFF);
        let Some(tool) = choices.iter().find(|choice| choice.as_str() != OFF) else {
            return;
        };
        let value = match on {
            true => OFF.to_owned(),
            false => tool.clone(),
        };
        self.switching.insert(ENABLED_ROW.to_owned(), !on);
        self.write(MANAGED_KEY.to_owned(), value, cx);
    }

    fn create_board(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let chat = self.tofu.as_ref().and_then(|(chat, _)| chat.upgrade());
        let Some(Some(desk)) = window.root::<Desk>() else {
            return eprintln!("desk: settings found no desk window to open boardy in");
        };
        let named = chat.as_ref().and_then(|chat| {
            let folder = Path::new(chat.read(cx).project()).file_name()?;
            Some(folder.to_string_lossy().into_owned())
        });
        let Some(name) = named else {
            self.problems.insert(
                MANAGED_KEY.to_owned(),
                sentence("tofu has not said which project folder it runs in"),
            );
            return cx.notify();
        };
        let key: String = name
            .chars()
            .filter(char::is_ascii_alphanumeric)
            .map(|letter| letter.to_ascii_uppercase())
            .take(BOARD_KEY_MOST)
            .collect();
        eprintln!("desk: settings boardy init {key} named {name}");
        self.problems.remove(MANAGED_KEY);
        let params = InitRequest {
            hooks: Vec::new(),
            key,
            mode: None,
            name,
        };
        let handle = window.window_handle();
        self.send::<BoardyInit>(params, cx, move |rows, reply, cx| match reply {
            Ok(_) => {
                eprintln!("desk: settings boardy init made a board");
                rows.open_board_when_on(&desk, handle, cx);
                if let Some(chat) = chat {
                    chat.update(cx, |_, cx| cx.emit(SettingsMoved));
                }
            }
            Err(problem) => {
                eprintln!("desk: settings boardy init refused: {problem}");
                rows.problems
                    .insert(MANAGED_KEY.to_owned(), sentence(&problem));
                cx.notify();
            }
        });
    }

    fn open_board_when_on(
        &mut self,
        desk: &Entity<Desk>,
        handle: AnyWindowHandle,
        cx: &mut Context<Self>,
    ) {
        let rows = cx.weak_entity();
        self.board_opening = Some(cx.observe(desk, move |_, desk, cx| {
            let rows = rows.clone();
            cx.defer(move |cx| {
                let picked = handle.update(cx, |_, window, cx| {
                    desk.update(cx, |desk, cx| desk.pick(BOARDY_SCREEN, window, cx))
                });
                match picked {
                    Ok(Err(Unpicked::Off(_))) => return,
                    Ok(Ok(())) => eprintln!("desk: settings opened boardy"),
                    Ok(Err(Unpicked::Failed(why))) => {
                        eprintln!("desk: settings cannot open boardy: {why}")
                    }
                    Err(error) => eprintln!("desk: settings lost its window: {error}"),
                }
                if let Err(error) = rows.update(cx, |rows, _| rows.board_opening = None) {
                    eprintln!("desk: settings closed before boardy opened: {error}");
                }
            });
        }));
    }

    fn flip_tofu(&mut self, key: &str, cx: &mut Context<Self>) {
        if key == ENABLED_ROW {
            return self.flip_enabled(cx);
        }
        let on = self.switching.get(key).copied().or_else(|| {
            let read = self.answer.read.as_ref()?;
            match read.value.get(key)?.value {
                Value::Switch(on) => Some(on),
                Value::Number(_) | Value::Text(_) | Value::Choice(..) => None,
            }
        });
        let Some(on) = on else {
            return;
        };
        self.switching.insert(key.to_owned(), !on);
        self.write(key.to_owned(), (!on).to_string(), cx)
    }

    fn flip_desk(&mut self, key: &str, cx: &mut Context<Self>) {
        let found = desk_settings().find(|setting| setting.key == key);
        let Some(setting) = found else {
            return;
        };
        self.flip_stored(setting.key);
        cx.notify();
    }

    fn flip_stored(&mut self, key: &'static str) {
        let flipped = self
            .stored
            .as_mut()
            .map_err(|e| e.clone())
            .and_then(|stored| {
                let on = !stored.switch(key);
                stored.set_switch(key, on)?;
                eprintln!(
                    "desk: settings {key} {on} written to {}",
                    stored.path().display()
                );
                Ok(())
            });
        match flipped {
            Ok(()) => {
                self.problems.remove(key);
            }
            Err(problem) => {
                eprintln!("desk: settings {key} not written: {problem}");
                self.problems.insert(key.to_owned(), sentence(&problem));
            }
        }
    }

    fn commit(&mut self, key: &str, cx: &mut Context<Self>) {
        let Some(field) = self.fields.get(key) else {
            return;
        };
        let text = field.input.read(cx).text().to_owned();
        if field.seen.as_deref() == Some(text.as_str()) {
            return;
        }
        self.write(key.to_owned(), text, cx);
    }

    fn field(
        &mut self,
        key: &str,
        value: &Value,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Entity<TextInput> {
        let shown = value.wire();
        let width = match value {
            Value::Number(_) => FieldWidth::Fixed(NUMBER_FIELD),
            Value::Switch(_) | Value::Text(_) | Value::Choice(..) => FieldWidth::Fit {
                least: TEXT_LEAST,
                most: FIELD_WIDTH,
            },
        };
        let field = self.fields.entry(key.to_owned()).or_insert_with(|| {
            let input = TextInput::new(SharedString::default(), window, cx);
            input.update(cx, |input, _| input.sized(width));
            let named = key.to_owned();
            let blur = cx.on_blur(&input.focus_handle(cx), window, move |rows, _, cx| {
                rows.commit(&named, cx)
            });
            Field {
                input,
                seen: None,
                _blur: blur,
            }
        });
        if field.seen.as_deref() != Some(shown.as_str()) {
            let all = field.input.read(cx).text().encode_utf16().count();
            field.input.update(cx, |input, cx| {
                input.replace_text_in_range(Some(0..all), &shown, window, cx)
            });
            field.seen = Some(shown);
        }
        field.input.clone()
    }

    fn tofu_row(
        &mut self,
        setting: &wire::Setting,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Badged {
        let key = setting.key.clone();
        let (name, about, list, restart) = match self.described.get(&key) {
            Some(said) => (
                said.label.clone(),
                said.about.clone(),
                said.list,
                said.restart,
            ),
            None => (
                sentence_case(&key).into(),
                SharedString::default(),
                false,
                false,
            ),
        };
        let models = MODEL_SETTINGS
            .iter()
            .find(|(named, _)| *named == key)
            .map(|(_, models)| *models);
        let fixed = FIXED_SETTINGS.iter().find(|(named, _)| *named == key);
        let control = match (&setting.value, models, fixed) {
            (Value::Text(text) | Value::Choice(text, _), _, _)
                if key == ORDER_KEY && self.models.is_some() =>
            {
                self.order_control(text, theme, cx)
            }
            (Value::Text(text), Some(models), _) if self.models.is_some() => {
                self.model_control(&key, text, models, cx)
            }
            (Value::Text(text), _, Some((named, fixed))) => {
                self.fixed_control(named, fixed, text, &setting.value, window, cx)
            }
            (value, _, _) => self.plain_control(&key, value, list, theme, window, cx),
        };
        let row = SettingRow {
            id: key.clone().into(),
            name,
            about: self.problems.get(&key).cloned().unwrap_or(about),
            source: badge(setting.source),
            control,
        };
        let restart = restart.then(|| Badge {
            text: RESTART.into(),
            tone: Tone::Warn,
        });
        (row, restart)
    }

    fn plain_control(
        &mut self,
        key: &str,
        value: &Value,
        list: bool,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let key = key.to_owned();
        let control = match value {
            Value::Switch(on) => {
                let on = self.switching.get(&key).copied().unwrap_or(*on);
                let flipped = key.clone();
                switch_bare(SharedString::from(key.clone()), key.clone(), on, theme)
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        cx.stop_propagation();
                        this.flip_tofu(&flipped, cx);
                    }))
                    .into_any_element()
            }
            Value::Choice(picked, choices) if !list => {
                let items = choices
                    .iter()
                    .map(|each| choice(&shown_choice(each), each == picked))
                    .collect();
                let (written, choices) = (key.clone(), choices.clone());
                self.picker(
                    key.clone().into(),
                    shown_choice(picked).into(),
                    items,
                    window,
                    cx,
                    move |this, at, cx| {
                        if let Some(choice) = choices.get(at) {
                            this.write(written.clone(), choice.clone(), cx);
                        }
                    },
                )
                .into_any_element()
            }
            Value::Number(_) | Value::Text(_) | Value::Choice(..) => {
                self.text_field(&key, value, window, cx)
            }
        };
        column(control).into_any_element()
    }

    fn text_field(
        &mut self,
        key: &str,
        value: &Value,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let input = self.field(key, value, window, cx);
        let entered = key.to_owned();
        div()
            .on_key_down(cx.listener(move |this, event: &KeyDownEvent, _, cx| {
                if event.keystroke.key == "enter" {
                    cx.stop_propagation();
                    this.commit(&entered, cx);
                }
            }))
            .child(input)
            .into_any_element()
    }

    fn fixed_control(
        &mut self,
        key: &'static str,
        fixed: &'static Fixed,
        text: &str,
        value: &Value,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let known = fixed
            .values
            .iter()
            .find(|(wire, _)| *wire == text)
            .filter(|_| !self.custom.contains(key));
        let items = fixed
            .values
            .iter()
            .map(|(wire, label)| {
                choice(
                    &shown_choice(label),
                    known.is_some_and(|(at, _)| at == wire),
                )
            })
            .chain([choice(fixed.custom, known.is_none())])
            .collect();
        let shown = known.map_or(fixed.custom, |(_, label)| label);
        let picker = self.picker(
            key.into(),
            shown_choice(shown).into(),
            items,
            window,
            cx,
            move |this, at, cx| match fixed.values.get(at) {
                Some((wire, _)) => {
                    this.custom.remove(key);
                    this.write(key.to_owned(), (*wire).to_owned(), cx);
                }
                None => {
                    this.custom.insert(key);
                    cx.notify();
                }
            },
        );
        let field = known
            .is_none()
            .then(|| column(self.text_field(key, value, window, cx)));
        stack(None, column(picker))
            .children(field)
            .into_any_element()
    }

    fn model_control(
        &mut self,
        key: &str,
        text: &str,
        models: Models,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let Some(query) = &self.models else {
            return div().into_any_element();
        };
        let subscriptions = query.subscriptions.clone();
        let groups = model_groups(query);
        let shown = |picked: &str| match picked {
            "" => NONE.to_owned(),
            picked => picked.to_owned(),
        };
        if models == Models::OnePerSubscription {
            let picked: Vec<&str> = text.split(',').map(str::trim).collect();
            let controls: Vec<Div> = subscriptions
                .iter()
                .map(|source| {
                    let mine = picked
                        .iter()
                        .find(|slug| slug_source(slug) == source)
                        .copied()
                        .unwrap_or_default();
                    let inside = groups
                        .iter()
                        .filter(|(owner, _)| owner == source)
                        .cloned()
                        .collect();
                    let button = self.model_menu(
                        format!("{key}/{source}"),
                        key,
                        Some(source.clone()),
                        inside,
                        shown(mine),
                        cx,
                    );
                    stack(Some(source.clone().into()), column(button))
                })
                .collect();
            return div()
                .flex()
                .flex_col()
                .gap(px(SOURCE_GAP))
                .children(controls)
                .into_any_element();
        }
        let wanted = |source: &String| match models {
            Models::Any => true,
            Models::Keyed => !subscriptions.contains(source),
            Models::Subscriptions | Models::OnePerSubscription => subscriptions.contains(source),
        };
        let inside = groups
            .into_iter()
            .filter(|(source, _)| wanted(source))
            .collect();
        column(self.model_menu(key.to_owned(), key, None, inside, shown(text), cx))
            .into_any_element()
    }

    fn order_control(&mut self, text: &str, theme: &Theme, cx: &mut Context<Self>) -> AnyElement {
        let Some(query) = &self.models else {
            return div().into_any_element();
        };
        let order = saved_order(text, &query.subscriptions);
        let last = order.len().saturating_sub(1);
        let lines = order.iter().enumerate().map(|(at, source)| {
            let up = order_button(("order-up", at), at > 0, std::f32::consts::PI, theme).when(
                at > 0,
                |button| {
                    let moved = swapped(&order, at - 1);
                    button.on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.write(ORDER_KEY.to_owned(), moved.clone(), cx)
                    }))
                },
            );
            let down =
                order_button(("order-down", at), at < last, 0.0, theme).when(at < last, |button| {
                    let moved = swapped(&order, at);
                    button.on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.write(ORDER_KEY.to_owned(), moved.clone(), cx)
                    }))
                });
            div()
                .w(px(FIELD_WIDTH))
                .h(px(FIELD))
                .flex()
                .items_center()
                .gap(px(SOURCE_GAP))
                .children(Brand::for_source(source).map(|brand| logo(brand, px(ORDER_LOGO), theme)))
                .child(div().flex_1().min_w_0().truncate().child(source.clone()))
                .child(up)
                .child(down)
        });
        div()
            .flex()
            .flex_col()
            .gap(px(SOURCE_GAP))
            .children(lines)
            .into_any_element()
    }

    fn model_menu(
        &mut self,
        id: String,
        key: &str,
        source: Option<String>,
        groups: Vec<(String, Vec<String>)>,
        label: String,
        cx: &mut Context<Self>,
    ) -> Entity<MenuButton> {
        let flat = source.is_some();
        let mut items = vec![MenuItem::action(NONE)];
        let mut choices = vec![Some(String::new())];
        for (owner, ids) in groups {
            let slugs: Vec<String> = ids.iter().map(|model| format!("{owner}/{model}")).collect();
            if flat {
                items.extend(slugs.iter().cloned().map(MenuItem::action));
                choices.extend(slugs.into_iter().map(Some));
                continue;
            }
            choices.push(None);
            choices.extend(slugs.into_iter().map(Some));
            items.push(MenuItem::Submenu {
                label: owner.clone().into(),
                icon: Brand::for_source(&owner).map(MenuIcon::from),
                items: ids.into_iter().map(MenuItem::action).collect(),
            });
        }
        let weak = cx.entity().downgrade();
        let picked = id.clone();
        let made = self.model_menus.entry(id.clone()).or_insert_with(|| {
            let button = MenuButton::new(label.clone().into(), Vec::new(), cx);
            button.update(cx, |button, _| {
                button.on_pick(move |at, _, cx| {
                    if let Err(error) =
                        weak.update(cx, |rows, cx| rows.pick_model(&picked, *at, cx))
                    {
                        eprintln!("desk: settings closed before a model was picked: {error}");
                    }
                })
            });
            ModelMenu {
                button,
                key: key.to_owned(),
                source,
                choices: Vec::new(),
            }
        });
        made.choices = choices;
        let button = made.button.clone();
        let chip = SharedString::from(format!("{id}-pick"));
        button.update(cx, |button, cx| {
            button.items(items, cx);
            button.trigger(move |_, theme| {
                flat_chip(chip.clone(), label.clone(), theme)
                    .w(px(FIELD_WIDTH))
                    .h(px(FIELD))
                    .justify_between()
            });
        });
        button
    }

    fn pick_model(&mut self, id: &str, at: usize, cx: &mut Context<Self>) {
        let Some(menu) = self.model_menus.get(id) else {
            return;
        };
        let Some(Some(slug)) = menu.choices.get(at).cloned() else {
            return;
        };
        let key = menu.key.clone();
        let value = match &menu.source {
            None => slug,
            Some(source) => {
                let current = self
                    .answer
                    .read
                    .as_ref()
                    .and_then(|read| read.value.get(&key))
                    .map(|setting| setting.value.wire())
                    .unwrap_or_default();
                let order = self
                    .models
                    .as_ref()
                    .map(|models| models.subscriptions.clone())
                    .unwrap_or_default();
                one_per_source(&current, source, &slug, &order)
            }
        };
        self.write(key, value, cx);
    }

    fn search_box(&mut self, window: &mut Window, cx: &mut Context<Self>) -> Entity<TextInput> {
        if let Some((input, _)) = &self.search {
            return input.clone();
        }
        let input = TextInput::new(SEARCH.into(), window, cx);
        input.update(cx, |input, _| input.sized(FieldWidth::Fill));
        let changed = cx.observe(&input, |rows, input, cx| {
            let query = input.read(cx).text().trim().to_lowercase();
            if query != rows.query {
                rows.query = query;
                rows.typed = Some((Instant::now(), split::watch(cx)));
                cx.notify();
            }
        });
        self.search = Some((input.clone(), changed));
        input
    }

    fn clear_search(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some((input, _)) = &self.search else {
            return;
        };
        let all = input.read(cx).text().encode_utf16().count();
        input.update(cx, |input, cx| {
            input.replace_text_in_range(Some(0..all), "", window, cx)
        });
    }

    fn reindex(&mut self) {
        let started = Instant::now();
        let tofu = self
            .answer
            .read
            .iter()
            .flat_map(|read| read.value.categories.iter())
            .filter(|category| !TERMINAL_ONLY.contains(&category.name.as_str()))
            .flat_map(|category| {
                category.settings.iter().map(|setting| {
                    let mut texts = vec![setting.key.as_str()];
                    if let Some(said) = self.described.get(&setting.key) {
                        texts.extend([said.label.as_ref(), said.about.as_ref()]);
                    }
                    if let Value::Choice(picked, choices) = &setting.value {
                        texts.push(picked);
                        texts.extend(choices.iter().map(String::as_str));
                    }
                    let page = match tier_name(&setting.key) {
                        Some(_) => ROLES_PAGE,
                        None => category.name.as_str(),
                    };
                    Entry::new(
                        PageId::Tofu(page.to_owned().into()),
                        Hit::Tofu(setting.clone()),
                        &texts,
                        &setting.key,
                    )
                })
            });
        let desk = DESK_NAV.1.iter().flat_map(|page| {
            page.page()
                .groups
                .iter()
                .flat_map(|group| group.settings.iter())
                .map(|setting| {
                    Entry::new(
                        PageId::Desk(*page),
                        Hit::Desk(setting),
                        &[setting.key, setting.name, setting.desc],
                        setting.key,
                    )
                })
        });
        let installed = plugins::all().map(|plugin| {
            Entry::new(
                PageId::Plugins(Some(plugin.id.clone().into())),
                Hit::Plugin(plugin),
                &[&plugin.id, &plugin.name],
                &plugin.id,
            )
        });
        self.index = desk.chain(tofu).chain(installed).collect();
        eprintln!(
            "desk: settings: search index {} rows in {:.2} ms",
            self.index.len(),
            started.elapsed().as_secs_f64() * 1000.0
        );
    }

    fn hits(&self) -> Vec<(PageId, Vec<Hit>)> {
        let mut found: Vec<(PageId, Vec<Hit>)> = Vec::new();
        for entry in self.index.iter().filter(|entry| entry.matches(&self.query)) {
            match found.last_mut() {
                Some((page, hits)) if *page == entry.page => hits.push(entry.hit.clone()),
                _ => found.push((entry.page.clone(), vec![entry.hit.clone()])),
            }
        }
        found
    }

    fn search_page(
        &mut self,
        found: Vec<(PageId, Vec<Hit>)>,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        if found.is_empty() {
            let said = format!("No setting matches \"{}\".", self.query);
            return page_title(SEARCH, Some(said.into()), theme).into_any_element();
        }
        let total: usize = found.iter().map(|(_, hits)| hits.len()).sum();
        let mut left = SHOWN_MOST;
        let mut shown: Vec<(PageId, Vec<Hit>)> = Vec::new();
        for (page, mut hits) in found {
            if left == 0 {
                break;
            }
            hits.truncate(left);
            left -= hits.len();
            shown.push((page, hits));
        }
        let groups: Vec<(PageId, Vec<Badged>)> = shown
            .into_iter()
            .map(|(page, hits)| {
                let rows = hits
                    .into_iter()
                    .map(|hit| match hit {
                        Hit::Tofu(setting) => self.tofu_row(&setting, theme, window, cx),
                        Hit::Desk(setting) => (self.desk_row(setting, theme, cx), None),
                        Hit::Plugin(plugin) => (
                            SettingRow {
                                id: plugin.id.clone().into(),
                                name: plugin.name.clone().into(),
                                about: plugin.id.clone().into(),
                                source: Source::Default,
                                control: div().into_any_element(),
                            },
                            None,
                        ),
                    })
                    .collect();
                (page, rows)
            })
            .collect();
        div()
            .children(groups.into_iter().map(|(page, rows)| {
                let tofu = matches!(page, PageId::Tofu(_));
                setting_section_clickable(
                    SettingSection {
                        title: page_name(&page),
                        about: None,
                        rows,
                    },
                    cx.listener(move |this, key: &SharedString, window, cx| {
                        this.clicked(&page, key, window, cx)
                    }),
                    tofu.then(|| self.on_scope(cx)),
                    theme,
                )
            }))
            .children((total > SHOWN_MOST).then(|| {
                frame::note(
                    format!(
                        "{} more match \"{}\". Type more to narrow.",
                        total - SHOWN_MOST,
                        self.query
                    ),
                    theme,
                )
            }))
            .into_any_element()
    }

    fn nav(
        &mut self,
        lit: Option<&[PageId]>,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Div {
        let mut list = HoverList::new("settings-nav", theme);
        let mut at = 0;
        let mut selected = None;
        let tofu: Vec<PageId> = self
            .answer
            .read
            .iter()
            .flat_map(|read| read.value.categories.iter())
            .filter(|category| !TERMINAL_ONLY.contains(&category.name.as_str()))
            .map(|category| PageId::Tofu(category.name.clone().into()))
            .collect();
        let desk: Vec<PageId> = DESK_NAV.1.iter().copied().map(PageId::Desk).collect();
        let installed: Vec<PageId> = [PageId::Plugins(None)]
            .into_iter()
            .chain(plugins::all().map(|plugin| PageId::Plugins(Some(plugin.id.clone().into()))))
            .collect();
        for (label, pages) in [
            (DESK_NAV.0, desk),
            (TOFU_NAV, tofu),
            (PLUGINS_NAV, installed),
        ] {
            let open = self.unfolded.contains(label)
                || lit.is_some_and(|lit| pages.iter().any(|page| lit.contains(page)));
            list = list.inert(
                group_header(label, label, pages.len(), open, theme)
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| this.fold(label, cx))),
            );
            for page in pages.into_iter().filter(|_| open) {
                at += 1;
                if page == self.page {
                    selected = Some(at);
                }
                let name = page_name(&page);
                let dim = lit.is_some_and(|lit| !lit.contains(&page));
                list = list.item(
                    row(name.clone(), page == self.page, false, theme)
                        .when(dim, |row| row.opacity(NAV_DIM))
                        .gap(px(NAV_ICON_GAP))
                        .child(glyph(page_glyph(&page), NAV_ICON, ink(theme, T3)))
                        .child(name)
                        .on_click(cx.listener(move |this, _: &ClickEvent, window, cx| {
                            if this.page != page && matches!(page, PageId::Plugins(None)) {
                                this.opened = Some(Instant::now());
                            }
                            this.go(page.clone());
                            this.clear_search(window, cx);
                            cx.notify();
                        })),
                );
            }
            at += 1;
        }
        div()
            .size_full()
            .flex()
            .flex_col()
            .py_2p5()
            .px_2()
            .border_r_1()
            .border_color(tint(theme.color(ColorToken::Shadow), NAV_RULE))
            .text_size(px(NAV_TEXT))
            .child(
                div()
                    .id("settings-search")
                    .w_full()
                    .mb_2p5()
                    .on_key_down(cx.listener(|this, event: &KeyDownEvent, window, cx| {
                        if event.keystroke.key == "escape" && !this.query.is_empty() {
                            cx.stop_propagation();
                            this.clear_search(window, cx);
                        }
                    }))
                    .child(self.search_box(window, cx)),
            )
            .child(list.selected(selected))
    }

    fn set_menu(&mut self, key: &SharedString, open: bool, cx: &mut Context<Self>) {
        if open {
            self.menu_for = Some(key.clone());
        }
        self.menu.set_open(open, Instant::now());
        eprintln!("desk: settings picker {key} open {open}");
        cx.notify();
    }

    fn picker(
        &mut self,
        key: SharedString,
        shown: SharedString,
        items: Vec<MenuItem>,
        window: &mut Window,
        cx: &mut Context<Self>,
        pick: impl Fn(&mut Self, usize, &mut Context<Self>) + 'static,
    ) -> Div {
        let theme = ActiveTheme::theme(cx);
        let now = Instant::now();
        let mine = self.menu_for.as_ref() == Some(&key);
        let phase = self.menu.phase(now);
        if mine && matches!(phase, Phase::Opening | Phase::Closing) {
            window.request_animation_frame();
        }
        let open = mine && matches!(phase, Phase::Opening | Phase::Open);
        let width = fitted(
            items
                .iter()
                .filter_map(|item| match item {
                    MenuItem::Action { label, .. } => Some(label.as_ref()),
                    _ => None,
                })
                .chain([shown.as_ref()]),
            window,
        );
        let picked = key.clone();
        let list = (mine && self.menu.mounted(now)).then(|| {
            let list = div().child(menu(
                PICKER_MENU,
                &items,
                &theme,
                cx.listener(move |this, at: &usize, _, cx| {
                    cx.stop_propagation();
                    this.set_menu(&picked, false, cx);
                    pick(this, *at, cx);
                }),
            ));
            div()
                .absolute()
                .right_0()
                .mt(px(MENU_DROP))
                .child(presented(list, self.menu.progress(now), reduced_motion(cx)))
        });
        let (closed, toggled) = (key.clone(), key.clone());
        div()
            .relative()
            .flex_none()
            .w(width)
            .on_mouse_down_out(cx.listener(move |this, _: &MouseDownEvent, _, cx| {
                if open {
                    this.set_menu(&closed, false, cx);
                }
            }))
            .child(
                flat_chip(SharedString::from(format!("{key}-pick")), shown, &theme)
                    .w_full()
                    .h(px(FIELD))
                    .justify_between()
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        cx.stop_propagation();
                        this.set_menu(&toggled, !open, cx);
                    })),
            )
            .children(list.map(|list| deferred(list).priority(1)))
    }

    fn desk_row(
        &mut self,
        setting: &'static Setting,
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> SettingRow {
        let on = self
            .stored
            .as_ref()
            .is_ok_and(|stored| stored.switch(setting.key));
        let control = column(switch_bare(setting.key, setting.name, on, theme).on_click(
            cx.listener(move |this, _: &ClickEvent, _, cx| {
                cx.stop_propagation();
                this.flip_desk(setting.key, cx);
            }),
        ))
        .into_any_element();
        let about = self
            .problems
            .get(setting.key)
            .cloned()
            .unwrap_or_else(|| setting.desc.into());
        SettingRow {
            id: setting.key.into(),
            name: setting.name.into(),
            about,
            source: Source::Default,
            control,
        }
    }

    fn desk_page(
        &mut self,
        page: DeskPage,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Div {
        let shown = page.page();
        let title = page_title(
            shown.title,
            Some(shown.desc)
                .filter(|desc| !desc.is_empty())
                .map(Into::into),
            theme,
        );
        if page == DeskPage::Terminal {
            let shells = self
                .shells
                .get_or_insert_with(|| cx.new(|_| TerminalPage::new()))
                .clone();
            return div().child(title).child(shells);
        }
        if page == DeskPage::Keys {
            let keys = self
                .keys
                .get_or_insert_with(|| cx.new(|cx| KeysPage::new(window, cx)))
                .clone();
            return div().child(title).child(keys);
        }
        if page == DeskPage::Appearance {
            let appearance = self
                .appearance
                .get_or_insert_with(|| cx.new(|cx| Appearance::new(window, cx)))
                .clone();
            return div().child(title).child(appearance);
        }
        let sections: Vec<SettingSection> = shown
            .groups
            .iter()
            .map(|group| SettingSection {
                title: group.title.into(),
                about: Some(group.about.into()),
                rows: group
                    .settings
                    .iter()
                    .map(|setting| (self.desk_row(setting, theme, cx), None))
                    .collect(),
            })
            .collect();
        self.log_page(
            page.name(),
            sections.iter().map(|section| section.rows.len()).sum(),
        );
        div().child(title).children(
            sections
                .into_iter()
                .map(|section| setting_section(section, None, theme)),
        )
    }

    fn tofu_page(
        &mut self,
        name: &str,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let title = shown_page(name);
        if self.tofu.is_none() {
            return page_title(title, Some(NO_TOFU.into()), theme).into_any_element();
        }
        let Some(read) = self.answer.read.clone() else {
            let reading = match &self.answer.failed {
                Some(error) => frame::note(error.to_string(), theme).truncate(),
                None => frame::note(WAITING, theme),
            };
            return div()
                .child(page_title(title, None, theme))
                .child(reading)
                .into_any_element();
        };
        let settings = &read.value;
        let Some(category) = settings
            .categories
            .iter()
            .find(|category| category.name == name)
        else {
            return page_title(
                title,
                Some(format!("tofu reports no {title} settings").into()),
                theme,
            )
            .into_any_element();
        };
        let one_subscription = self
            .tofu
            .as_ref()
            .and_then(|(chat, _)| chat.upgrade())
            .is_none_or(|chat| chat.read(cx).signed_subscriptions() < 2);
        let rows: Vec<Badged> = match name {
            PROJECTS => self.project_rows(&category.settings, theme, window, cx),
            _ => category
                .settings
                .iter()
                .filter(|setting| {
                    !(one_subscription && CONTINUITY.contains(&setting.key.as_str()))
                        && tier_name(&setting.key).is_none()
                })
                .map(|setting| self.tofu_row(setting, theme, window, cx))
                .collect(),
        };
        let roles: Vec<Badged> = match name {
            ROLES_PAGE => TIERS
                .iter()
                .filter_map(|(key, label)| Some((settings.get(key)?, *label)))
                .map(|(setting, label)| {
                    let (mut row, restart) = self.tofu_row(setting, theme, window, cx);
                    row.name = label.into();
                    (row, restart)
                })
                .collect(),
            _ => Vec::new(),
        };
        self.log_page(name, rows.len() + roles.len());
        let roles = (!roles.is_empty()).then(|| {
            setting_section(
                SettingSection {
                    title: ROLES.into(),
                    about: Some(TOFU_EVERYWHERE.into()),
                    rows: roles,
                },
                Some(self.on_scope(cx)),
                theme,
            )
        });
        div()
            .child(page_title(title, None, theme))
            .children(
                self.answer
                    .failed
                    .as_ref()
                    .map(|error| frame::note(format!("the last read failed: {error}"), theme)),
            )
            .child(setting_section(
                SettingSection {
                    title: title.into(),
                    about: Some(TOFU_EVERYWHERE.into()),
                    rows,
                },
                Some(self.on_scope(cx)),
                theme,
            ))
            .children(roles)
            .into_any_element()
    }

    fn project_rows(
        &mut self,
        settings: &[wire::Setting],
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Vec<Badged> {
        let managed = settings.iter().find(|setting| setting.key == MANAGED_KEY);
        let Some((managed, Value::Choice(picked, choices))) =
            managed.map(|setting| (setting, &setting.value))
        else {
            return settings
                .iter()
                .map(|setting| self.tofu_row(setting, theme, window, cx))
                .collect();
        };
        let on = self
            .switching
            .get(ENABLED_ROW)
            .copied()
            .unwrap_or(picked.as_str() != OFF);
        let switch = switch_bare(ENABLED_ROW, ENABLED, on, theme).on_click(cx.listener(
            |this, _: &ClickEvent, _, cx| {
                cx.stop_propagation();
                this.flip_enabled(cx);
            },
        ));
        let reason = self
            .described
            .get(MANAGED_KEY)
            .and_then(|said| said.reason.clone())
            .filter(|_| picked.as_str() == OFF);
        let create = reason.is_some().then(|| {
            button(
                "settings-create-board",
                CREATE_BOARD,
                Some(Glyph::FolderAdd),
                ButtonKind::Plain,
                theme,
            )
            .on_click(cx.listener(|this, _: &ClickEvent, window, cx| {
                cx.stop_propagation();
                this.create_board(window, cx);
            }))
        });
        let enabled = SettingRow {
            id: ENABLED_ROW.into(),
            name: ENABLED.into(),
            about: self
                .problems
                .get(MANAGED_KEY)
                .cloned()
                .or(reason)
                .unwrap_or_default(),
            source: badge(managed.source),
            control: column(
                div()
                    .flex()
                    .items_center()
                    .gap(px(SOURCE_GAP))
                    .children(create)
                    .child(switch),
            )
            .into_any_element(),
        };
        let mut rows = vec![(enabled, None)];
        if !on {
            return rows;
        }
        let tools: Vec<String> = choices
            .iter()
            .filter(|choice| choice.as_str() != OFF)
            .cloned()
            .collect();
        let tool = wire::Setting {
            value: Value::Choice(picked.clone(), tools),
            ..managed.clone()
        };
        let (mut row, restart) = self.tofu_row(&tool, theme, window, cx);
        row.name = TOOL.into();
        rows.push((row, restart));
        rows.extend(
            settings
                .iter()
                .filter(|setting| setting.key != MANAGED_KEY)
                .map(|setting| self.tofu_row(setting, theme, window, cx)),
        );
        rows
    }

    fn on_scope(&self, cx: &mut Context<Self>) -> OnScope {
        let rows = cx.entity().downgrade();
        Rc::new(move |id, source, _, cx| {
            let scope = match source {
                Source::Global => Scope::Global,
                Source::Project | Source::Default => Scope::Project,
            };
            if let Err(error) = rows.update(cx, |rows, cx| rows.rescope(id, scope, cx)) {
                eprintln!("desk: settings closed before {id} was rescoped: {error}");
            }
        })
    }

    fn rescope(&mut self, id: &str, scope: Scope, cx: &mut Context<Self>) {
        let key = match id {
            ENABLED_ROW => MANAGED_KEY,
            key => key,
        };
        let value = self
            .answer
            .read
            .as_ref()
            .and_then(|read| read.value.get(key))
            .map(|setting| setting.value.wire());
        let Some(value) = value else {
            return eprintln!("desk: settings has no {key} to rescope");
        };
        self.write_at(key.to_owned(), value, scope, cx);
    }

    fn scope_control(
        &mut self,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        let file = self
            .answer
            .read
            .as_ref()
            .map(|read| read.value.file(self.scope).to_owned());
        let label = SCOPES
            .iter()
            .find(|(scope, _)| *scope == self.scope)
            .map_or("", |(_, label)| label);
        let button = match &self.scope_menu {
            Some(button) => button.clone(),
            None => {
                let button = MenuButton::new(label.into(), Vec::new(), cx);
                let weak = cx.entity().downgrade();
                button.update(cx, |button, _| {
                    button.on_pick(move |at, _, cx| {
                        let Some((scope, _)) = SCOPES.get(*at) else {
                            return;
                        };
                        let picked = weak.update(cx, |rows, cx| {
                            rows.scope = *scope;
                            eprintln!("desk: settings changes go to {}", scope.wire());
                            cx.notify();
                        });
                        if let Err(error) = picked {
                            eprintln!("desk: settings closed before its scope was picked: {error}");
                        }
                    })
                });
                self.scope_menu = Some(button.clone());
                button
            }
        };
        let items = SCOPES
            .iter()
            .map(|(scope, label)| choice(label, *scope == self.scope))
            .collect();
        button.update(cx, |button, cx| {
            button.items(items, cx);
            button.trigger(move |_, theme| header_button(SCOPE_PICKER, label, theme));
        });
        let control = header_control(button).id(SCOPE_TIP);
        match file {
            Some(file) => {
                tooltip(SCOPE_TIP, control, Edge::Frame, file, theme, window, cx).into_any_element()
            }
            None => control.into_any_element(),
        }
    }

    fn log_page(&mut self, name: &str, count: usize) {
        if self.shown.as_ref() != Some(&self.page) {
            self.shown = Some(self.page.clone());
            eprintln!("desk: settings page {name} shows {count} rows");
        }
    }
}

fn column(control: impl IntoElement) -> Div {
    div()
        .flex_none()
        .w(px(FIELD_WIDTH))
        .h(px(FIELD))
        .flex()
        .items_center()
        .justify_end()
        .child(control)
}

fn stack(label: Option<SharedString>, control: Div) -> Div {
    div()
        .flex()
        .flex_col()
        .items_end()
        .gap(px(SOURCE_GAP))
        .children(label.map(|label| {
            div()
                .w(px(FIELD_WIDTH))
                .text_size(px(SOURCE_TEXT))
                .child(label)
        }))
        .child(control)
}

fn model_groups(models: &ModelsQuery) -> Vec<(String, Vec<String>)> {
    let usable: Vec<(&str, &str)> = models
        .models
        .iter()
        .filter(|model| model.kind == "llm" && model.r#use != "excluded")
        .filter_map(|model| Some((model.slug.split_once('/')?.0, model.id.as_str())))
        .collect();
    let mut groups: Vec<(String, Vec<String>)> = models
        .subscriptions
        .iter()
        .map(|source| (source.clone(), Vec::new()))
        .collect();
    for (source, model) in usable {
        match groups.iter_mut().find(|(owner, _)| owner == source) {
            Some((_, ids)) => ids.push(model.to_owned()),
            None => groups.push((source.to_owned(), vec![model.to_owned()])),
        }
    }
    groups.retain(|(_, ids)| !ids.is_empty());
    groups
}

fn slug_source(slug: &str) -> &str {
    slug.split_once('/').map_or(slug, |(source, _)| source)
}

fn one_per_source(current: &str, source: &str, slug: &str, order: &[String]) -> String {
    let mut kept: Vec<&str> = current
        .split(',')
        .map(str::trim)
        .filter(|each| !each.is_empty() && slug_source(each) != source)
        .chain(Some(slug).filter(|slug| !slug.is_empty()))
        .collect();
    kept.sort_by_key(|each| {
        order
            .iter()
            .position(|known| known == slug_source(each))
            .unwrap_or(order.len())
    });
    kept.join(",")
}

fn saved_order(text: &str, subscriptions: &[String]) -> Vec<String> {
    let mut order: Vec<String> = text
        .split(',')
        .map(str::trim)
        .filter(|source| subscriptions.iter().any(|known| known == source))
        .map(str::to_owned)
        .collect();
    let mut seen = HashSet::new();
    order.retain(|source| seen.insert(source.clone()));
    for source in subscriptions {
        if !order.contains(source) {
            order.push(source.clone());
        }
    }
    order
}

fn swapped(order: &[String], at: usize) -> String {
    let mut moved = order.to_vec();
    moved.swap(at, at + 1);
    moved.join(",")
}

fn order_button(id: (&'static str, usize), live: bool, turn: f32, theme: &Theme) -> Stateful<Div> {
    let hover = ink(theme, HOVER);
    let arrow = glyph(Glyph::Chevron, ORDER_ARROW, ink(theme, T3))
        .with_transformation(Transformation::rotate(radians(turn)));
    div()
        .id(id)
        .flex_none()
        .size(px(ORDER_BUTTON))
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(RADIUS_BADGE))
        .when(live, |button| {
            button.cursor_pointer().hover(move |style| style.bg(hover))
        })
        .when(!live, |button| button.opacity(ORDER_DIM))
        .child(arrow)
}

fn choice(label: &str, in_use: bool) -> MenuItem {
    MenuItem::Action {
        label: label.to_owned().into(),
        keys: in_use.then(|| IN_USE.into()),
        icon: None,
    }
}

fn nav_group(page: &PageId) -> &'static str {
    match page {
        PageId::Desk(_) => DESK_NAV.0,
        PageId::Tofu(_) => TOFU_NAV,
        PageId::Plugins(_) => PLUGINS_NAV,
    }
}

fn page_glyph(page: &PageId) -> Glyph {
    match page {
        PageId::Desk(DeskPage::Appearance) => Glyph::Palette,
        PageId::Desk(DeskPage::Keys) => Glyph::Label,
        PageId::Desk(DeskPage::Editor) => Glyph::Code,
        PageId::Desk(DeskPage::Terminal) => Glyph::Terminal,
        PageId::Tofu(name) => match name.as_ref() {
            "Models & roles" => Glyph::Brain,
            "Interaction" => Glyph::Chat,
            "Context" => Glyph::Book,
            "Files" => Glyph::File,
            "Shell" => Glyph::Terminal,
            "Turn" => Glyph::Agents,
            "Projects" => Glyph::Folder,
            "Browser" => Glyph::Globe,
            _ => Glyph::Settings,
        },
        PageId::Plugins(None) => Glyph::Settings,
        PageId::Plugins(Some(_)) => Glyph::Sparkle,
    }
}

fn capitalized(said: &str) -> String {
    let mut letters = said.chars();
    letters
        .next()
        .map(|first| first.to_uppercase().chain(letters).collect())
        .unwrap_or_default()
}

fn sentence_case(key: &str) -> String {
    let letters: Vec<char> = key.chars().collect();
    let mut words: Vec<String> = Vec::new();
    for (at, letter) in letters.iter().copied().enumerate() {
        let before = at.checked_sub(1).and_then(|was| letters.get(was)).copied();
        let after = letters.get(at + 1).copied();
        let breaks = letter.is_uppercase()
            && before.is_some_and(|was| {
                was.is_lowercase() || (was.is_uppercase() && after.is_some_and(char::is_lowercase))
            });
        match words.last_mut() {
            _ if !letter.is_alphanumeric() => words.push(String::new()),
            Some(word) if !breaks => word.push(letter),
            _ => words.push(letter.to_string()),
        }
    }
    let said: Vec<String> = words
        .into_iter()
        .filter(|word| !word.is_empty())
        .map(
            |word| match word.chars().count() > 1 && word.chars().all(char::is_uppercase) {
                true => word,
                false => word.to_lowercase(),
            },
        )
        .collect();
    capitalized(&said.join(" "))
}

fn desk_settings() -> impl Iterator<Item = &'static Setting> {
    DESK_NAV
        .1
        .iter()
        .flat_map(|page| page.page().groups.iter())
        .flat_map(|group| group.settings.iter())
}

fn page_name(page: &PageId) -> SharedString {
    match page {
        PageId::Desk(page) => page.name().into(),
        PageId::Tofu(name) => shown_page(name).into(),
        PageId::Plugins(None) => PLUGINS_GENERAL.into(),
        PageId::Plugins(Some(id)) => {
            plugins::find(id).map_or_else(|| id.clone(), |plugin| plugin.name.clone().into())
        }
    }
}

fn shown_page(name: &str) -> &str {
    match name {
        PROJECTS => PROJECT_MANAGEMENT,
        name => name,
    }
}

fn tier_name(key: &str) -> Option<&'static str> {
    TIERS
        .iter()
        .find(|(tier, _)| *tier == key)
        .map(|(_, label)| *label)
}

fn synced_ago(at: Instant) -> String {
    let minutes = at.elapsed().as_secs() / SECONDS_PER_MINUTE;
    match minutes {
        0 => "Synced just now".to_owned(),
        minutes if minutes < MINUTES_PER_HOUR => format!("Synced {minutes}m ago"),
        minutes => format!("Synced {}h ago", minutes / MINUTES_PER_HOUR),
    }
}

fn shown_choice(value: &str) -> String {
    if value.is_empty() {
        return MODEL_DEFAULT.to_owned();
    }
    let mut said = capitalized(value);
    for (brand, cased) in BRANDS {
        if let Some(at) = said.to_ascii_lowercase().find(brand) {
            said.replace_range(at..at + brand.len(), cased);
        }
    }
    said
}

fn fitted<'a>(labels: impl Iterator<Item = &'a str>, window: &Window) -> gpui::Pixels {
    let mut style = window.text_style();
    style.font_weight = FontWeight::MEDIUM;
    let size = px(FONT_SMALL);
    let widest = labels
        .map(|label| {
            let run = style.to_run(label.len());
            window
                .text_system()
                .layout_line(label, size, &[run], None)
                .width
        })
        .fold(px(0.0), gpui::Pixels::max);
    let chrome = px(2.0 * CHIP_PAD + CHIP_GAP + ICON_SMALL);
    (widest + chrome).clamp(px(PICKER_LEAST), px(FIELD_WIDTH))
}

fn badge(source: wire::Source) -> Source {
    match source {
        wire::Source::Default => Source::Default,
        wire::Source::Global => Source::Global,
        wire::Source::Project => Source::Project,
    }
}

fn sentence(problem: &str) -> SharedString {
    let said = problem.trim();
    let stop = if said.ends_with(['.', '!', '?']) {
        ""
    } else {
        "."
    };
    format!("{said}{stop}").into()
}

impl Render for Rows {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let scope = matches!(self.page, PageId::Tofu(_)).then(|| {
            div()
                .flex()
                .items_center()
                .gap(px(SYNC_GAP))
                .children(self.synced.map(|at| {
                    div()
                        .text_size(px(FONT_SMALL))
                        .text_color(ink(&theme, T3))
                        .child(synced_ago(at))
                }))
                .child(self.scope_control(&theme, window, cx))
                .into_any_element()
        });
        let found = (!self.query.is_empty()).then(|| self.hits());
        let lit: Option<Vec<PageId>> = found
            .as_ref()
            .map(|found| found.iter().map(|(page, _)| page.clone()).collect());
        let count: usize = found.iter().flatten().map(|(_, hits)| hits.len()).sum();
        let building = Instant::now();
        let nav = self.nav(lit.as_deref(), &theme, window, cx);
        let page_body = match (found, self.page.clone()) {
            (Some(found), _) => self.search_page(found, &theme, window, cx),
            (None, PageId::Desk(page)) => {
                self.desk_page(page, &theme, window, cx).into_any_element()
            }
            (None, PageId::Tofu(name)) => self.tofu_page(&name, &theme, window, cx),
            (None, PageId::Plugins(shown)) => {
                let page = self.plugins_page(cx);
                let opened = self.opened.take();
                page.update(cx, |page, _| page.show(shown, opened));
                page.into_any_element()
            }
        };
        if let Some((typed, spans)) = self.typed.take() {
            let query = self.query.clone();
            let waited = building.duration_since(typed).as_secs_f64() * 1000.0;
            let built = building.elapsed().as_secs_f64() * 1000.0;
            window.on_next_frame(move |_, cx| {
                let shown = typed.elapsed().as_secs_f64() * 1000.0;
                split::unwatch(cx);
                eprintln!(
                    "desk: settings: search \"{query}\" {count} matches in {shown:.2} ms (waited {waited:.2}, rows built {built:.2})"
                );
                eprintln!(
                    "desk: settings: search \"{query}\" frame ms after the keystroke: rows built {:.2}, {}, next frame {shown:.2}",
                    waited + built,
                    split::told(&spans, typed)
                );
            });
        }
        let sheet = self
            .plugins
            .as_ref()
            .map(|(page, _)| page.clone())
            .and_then(|page| page.update(cx, |page, cx| page.consent_sheet(cx)));
        let content = div().size_full().flex().flex_col().child(
            ScrollArea::new("settings-content").child(
                div()
                    .px(px(CONTENT_PAD_X))
                    .py(px(CONTENT_PAD_Y))
                    .flex()
                    .flex_col()
                    .child(page_body),
            ),
        );
        let showing = Rc::clone(&self.showing);
        let first_frame = canvas(
            |_, _, _| {},
            move |_, (), _, _| {
                if let Some(at) = showing.take() {
                    eprintln!(
                        "desk: settings: first frame {:.1}",
                        at.elapsed().as_secs_f64() * 1000.0
                    );
                }
            },
        )
        .absolute()
        .size_0();
        frame::tile(
            frame::titled("settings", scope),
            &theme,
            div()
                .flex_1()
                .min_h_0()
                .flex()
                .relative()
                .child(timed(
                    "settings nav",
                    rooted(
                        "settings nav",
                        StyleRefinement::default()
                            .w(relative(NAV_SHARE))
                            .min_w(px(NAV_LEAST))
                            .max_w(px(NAV_WIDTH))
                            .flex_none(),
                        nav,
                    ),
                ))
                .child(timed(
                    "settings page",
                    rooted(
                        "settings page",
                        StyleRefinement::default().flex_1().min_w_0(),
                        content,
                    ),
                ))
                .children(sheet)
                .child(first_frame),
        )
    }
}
