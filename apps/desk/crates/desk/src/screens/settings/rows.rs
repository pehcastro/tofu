use std::cell::Cell;
use std::collections::HashMap;
use std::path::PathBuf;
use std::rc::Rc;
use std::sync::Arc;
use std::time::Instant;

use desk_core::protocol::request::{QuerySettings, SettingsSet};
use desk_core::protocol::{self, NoParams, Request, SettingsReport, SettingsSetParams};
use desk_core::query::{Answer, QueryError, Read};
use desk_core::settings::{self as wire, Settings, Value};
use desk_ui::components::chip::flat_chip;
use desk_ui::components::form::{TextInput, switch_bare};
use desk_ui::components::list::{HoverList, group_header, row};
use desk_ui::components::overlay::{MenuItem, menu};
use desk_ui::components::paint::{ink, ms, presented, tint};
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::settings::{SettingRow, Source, page_title, setting_group_clickable};
use desk_ui::components::size::T3;
use desk_ui::components::tree::{IconPack, IconTheme, pick_pack, picked_pack};
use desk_ui::live::ActiveTheme;
use desk_ui::motion::{Phase, Presence, reduced_motion};
use desk_ui::theme::{ColorToken, NumberToken, Theme};
use gpui::{
    AnyElement, ClickEvent, Context, Div, Entity, EntityId, EntityInputHandler, Focusable, Image,
    KeyDownEvent, MouseDownEvent, Render, SharedString, StyleRefinement, Subscription, WeakEntity,
    Window, canvas, deferred, div, img, prelude::*, px, relative,
};

use super::fixture::{
    Control, DESK_NAV, DeskPage, FIRST_TOFU_PAGE, PLUGINS_GENERAL, PLUGINS_NAV, PageId, Scope,
    Setting, TOFU_NAV, TOFU_PAGES,
};
use super::keys::KeysPage;
use super::plugins::PluginsPage;
use super::stored::Stored;
use super::terminal::TerminalPage;
use crate::modules::chat::Chat;
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
const PREVIEW_FILES: [&str; 4] = ["index.ts", "main.rs", "README.md", "package.json"];
const PREVIEW_FOLDER: &str = "src";
const PREVIEW_COLUMNS: u16 = 3;
const PREVIEW_ICON: f32 = 16.0;
const PREVIEW_GAP: f32 = 6.0;
const PREVIEW_PAD: f32 = 6.0;
const PREVIEW_RADIUS: f32 = 8.0;
const PREVIEW_FILL: f32 = 0.25;
const PICKER_GAP: f32 = 12.0;
const SELECT_WIDTH: f32 = 132.0;
const MENU_DROP: f32 = 32.0;
const PROBLEM_TEXT: f32 = 12.0;
const PROBLEM_GAP: f32 = 4.0;
const WAITING: &str = "Waiting for tofu";
const CHOICE_GAP: f32 = 2.0;
const CHOICE_PAD: f32 = 2.0;
const CHOICE_RADIUS: f32 = 8.0;
const CHOICE_FILL: f32 = 0.04;
const CHOICE_ON: f32 = 0.12;
const CHOICE_PILL_X: f32 = 10.0;
const CHOICE_PILL_Y: f32 = 3.0;
const CHOICE_PILL_RADIUS: f32 = 6.0;
const SEARCH: &str = "Search settings";
const NAV_DIM: f32 = 0.35;
const STEM_LEAST: usize = 5;
const STEM_SLACK: usize = 2;
const SHOWN_MOST: usize = 24;
const NO_TOFU: &str = "Settings reads tofu's settings from the tofu the work screen runs, and no work screen is open here.";

pub struct Rows {
    page: PageId,
    scope: Scope,
    flipped: HashMap<&'static str, bool>,
    shown: Option<PageId>,
    packs: Presence,
    preview: Option<(IconPack, Vec<Arc<Image>>)>,
    tofu: Option<(WeakEntity<Chat>, EntityId)>,
    answer: Answer<Settings>,
    switching: HashMap<String, bool>,
    problems: HashMap<String, SharedString>,
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

struct Field {
    input: Entity<TextInput>,
    seen: Option<String>,
    _blur: Subscription,
}

impl Rows {
    pub fn new(theme: &Theme) -> Self {
        let mut rows = Rows {
            page: PageId::Tofu(FIRST_TOFU_PAGE.into()),
            scope: Scope::Project,
            flipped: HashMap::new(),
            shown: None,
            packs: Presence::new(
                ms(theme, NumberToken::MotionEnter),
                ms(theme, NumberToken::MotionExit),
            ),
            preview: None,
            tofu: None,
            answer: Answer::default(),
            switching: HashMap::new(),
            problems: HashMap::new(),
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
        self.page = PageId::Plugins(None);
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
        self.switching.clear();
        self.answer.answered(read);
        self.reindex();
        cx.notify();
    }

    fn write(&mut self, key: String, value: String, cx: &mut Context<Self>) {
        let scope = self.scope.wire();
        eprintln!("desk: settings set {key} {value} in {}", scope.wire());
        self.problems.remove(&key);
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
                self.page = page.clone();
                self.clear_search(window, cx);
                cx.notify();
            }
        }
    }

    fn flip_tofu(&mut self, key: &str, cx: &mut Context<Self>) {
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
        let key = setting.key;
        match setting.control {
            Control::Switch(start) => {
                let now = self.flipped.get(key).copied().unwrap_or(start);
                self.flipped.insert(key, !now);
                eprintln!("desk: settings {key} {now} -> {}", !now);
            }
            Control::Stored => self.flip_stored(key),
            Control::Pack => return,
        }
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
        let field = self.fields.entry(key.to_owned()).or_insert_with(|| {
            let input = TextInput::new(SharedString::default(), window, cx);
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
    ) -> SettingRow {
        let key = setting.key.clone();
        let control = match &setting.value {
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
            Value::Choice(picked, choices) => div()
                .flex()
                .flex_none()
                .gap(px(CHOICE_GAP))
                .p(px(CHOICE_PAD))
                .rounded(px(CHOICE_RADIUS))
                .bg(ink(theme, CHOICE_FILL))
                .text_size(px(PROBLEM_TEXT))
                .children(choices.iter().map(|choice| {
                    let on = choice == picked;
                    let (written, value) = (key.clone(), choice.clone());
                    div()
                        .id(SharedString::from(format!("{key}-{choice}")))
                        .px(px(CHOICE_PILL_X))
                        .py(px(CHOICE_PILL_Y))
                        .rounded(px(CHOICE_PILL_RADIUS))
                        .cursor_pointer()
                        .when(on, |pill| {
                            pill.bg(ink(theme, CHOICE_ON))
                                .text_color(theme.color(ColorToken::TextStrong))
                        })
                        .when(!on, |pill| pill.text_color(ink(theme, T3)))
                        .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                            cx.stop_propagation();
                            this.write(written.clone(), value.clone(), cx);
                        }))
                        .child(choice.clone())
                }))
                .into_any_element(),
            Value::Number(_) | Value::Text(_) => {
                let input = self.field(&key, &setting.value, window, cx);
                let entered = key.clone();
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
        };
        let problem = self.problems.get(&key).cloned();
        SettingRow {
            id: key.clone().into(),
            name: key.into(),
            about: SharedString::default(),
            source: badge(setting.source),
            control: div()
                .flex()
                .flex_col()
                .items_end()
                .gap(px(PROBLEM_GAP))
                .child(control)
                .children(problem.map(|problem| {
                    div()
                        .text_size(px(PROBLEM_TEXT))
                        .text_color(theme.color(ColorToken::StatusWarn))
                        .child(problem)
                }))
                .into_any_element(),
        }
    }

    fn search_box(&mut self, window: &mut Window, cx: &mut Context<Self>) -> Entity<TextInput> {
        if let Some((input, _)) = &self.search {
            return input.clone();
        }
        let input = TextInput::new(SEARCH.into(), window, cx);
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
            .flat_map(|category| {
                category.settings.iter().map(|setting| {
                    let mut texts = vec![setting.key.as_str()];
                    if let Value::Choice(picked, choices) = &setting.value {
                        texts.push(picked);
                        texts.extend(choices.iter().map(String::as_str));
                    }
                    Entry::new(
                        PageId::Tofu(category.name.clone().into()),
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
                .flat_map(|(_, settings)| settings.iter())
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
        self.index = tofu.chain(desk).chain(installed).collect();
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
        let groups: Vec<(PageId, Vec<SettingRow>)> = shown
            .into_iter()
            .map(|(page, hits)| {
                let rows = hits
                    .into_iter()
                    .map(|hit| match hit {
                        Hit::Tofu(setting) => self.tofu_row(&setting, theme, window, cx),
                        Hit::Desk(setting) => self.desk_row(setting, theme, window, cx),
                        Hit::Plugin(plugin) => SettingRow {
                            id: plugin.id.clone().into(),
                            name: plugin.name.clone().into(),
                            about: plugin.id.clone().into(),
                            source: Source::Default,
                            control: div().into_any_element(),
                        },
                    })
                    .collect();
                (page, rows)
            })
            .collect();
        div()
            .children(groups.into_iter().map(|(page, rows)| {
                setting_group_clickable(
                    page_name(&page),
                    rows,
                    cx.listener(move |this, key: &SharedString, window, cx| {
                        this.clicked(&page, key, window, cx)
                    }),
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
        let tofu: Vec<PageId> = TOFU_PAGES
            .iter()
            .map(|name| PageId::Tofu((*name).into()))
            .collect();
        let desk: Vec<PageId> = DESK_NAV.1.iter().copied().map(PageId::Desk).collect();
        let installed: Vec<PageId> = [PageId::Plugins(None)]
            .into_iter()
            .chain(plugins::all().map(|plugin| PageId::Plugins(Some(plugin.id.clone().into()))))
            .collect();
        for (label, pages) in [
            (TOFU_NAV, tofu),
            (DESK_NAV.0, desk),
            (PLUGINS_NAV, installed),
        ] {
            list = list.inert(group_header(label, label, pages.len(), true, theme));
            for page in pages {
                at += 1;
                if page == self.page {
                    selected = Some(at);
                }
                let name = page_name(&page);
                let dim = lit.is_some_and(|lit| !lit.contains(&page));
                list = list.item(
                    row(name.clone(), page == self.page, false, theme)
                        .when(dim, |row| row.opacity(NAV_DIM))
                        .child(name)
                        .on_click(cx.listener(move |this, _: &ClickEvent, window, cx| {
                            if this.page != page && matches!(page, PageId::Plugins(None)) {
                                this.opened = Some(Instant::now());
                            }
                            this.page = page.clone();
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

    fn pick(&mut self, at: usize, cx: &mut Context<Self>) {
        self.packs.set_open(false, Instant::now());
        cx.notify();
        let Some(pack) = IconPack::ALL.get(at).copied() else {
            return eprintln!("desk: settings has no file icon pack at {at}");
        };
        let was = picked_pack(cx);
        match pick_pack(pack, cx) {
            Ok(()) => eprintln!("desk: settings file icons {} -> {}", was.key(), pack.key()),
            Err(error) => eprintln!(
                "desk: settings file icons {} for this run only: {error}",
                pack.key()
            ),
        }
    }

    fn set_packs(&mut self, open: bool, cx: &mut Context<Self>) {
        self.packs.set_open(open, Instant::now());
        eprintln!("desk: settings file icon packs open {open}");
        cx.notify();
    }

    fn preview(&mut self, pack: IconPack) -> Vec<Arc<Image>> {
        if let Some((shown, images)) = &self.preview
            && *shown == pack
        {
            return images.clone();
        }
        let images = match IconTheme::pack(pack) {
            Ok(icons) => PREVIEW_FILES
                .iter()
                .map(|name| icons.file_image(name))
                .chain([false, true].map(|open| icons.folder_image(PREVIEW_FOLDER, open)))
                .collect(),
            Err(error) => {
                eprintln!("desk: settings shows no {} preview: {error}", pack.key());
                Vec::new()
            }
        };
        self.preview = Some((pack, images.clone()));
        images
    }

    fn pack_picker(&mut self, theme: &Theme, window: &mut Window, cx: &mut Context<Self>) -> Div {
        let pack = picked_pack(cx);
        let grid = div()
            .grid()
            .grid_cols(PREVIEW_COLUMNS)
            .gap(px(PREVIEW_GAP))
            .p(px(PREVIEW_PAD))
            .rounded(px(PREVIEW_RADIUS))
            .bg(tint(theme.color(ColorToken::Shadow), PREVIEW_FILL))
            .children(
                self.preview(pack)
                    .into_iter()
                    .map(|image| img(image).flex_none().size(px(PREVIEW_ICON))),
            );
        let items: Vec<MenuItem> = IconPack::ALL
            .iter()
            .map(|each| MenuItem::Action {
                label: each.label().into(),
                keys: (*each == pack).then(|| "in use".into()),
                icon: None,
            })
            .collect();
        let list = div().child(menu(
            "file-icon-packs",
            &items,
            theme,
            cx.listener(|this, at: &usize, _, cx| {
                cx.stop_propagation();
                this.pick(*at, cx);
            }),
        ));
        let now = Instant::now();
        if matches!(self.packs.phase(now), Phase::Opening | Phase::Closing) {
            window.request_animation_frame();
        }
        let shown = self.packs.mounted(now).then(|| {
            div()
                .absolute()
                .right_0()
                .mt(px(MENU_DROP))
                .child(presented(
                    list,
                    self.packs.progress(now),
                    reduced_motion(cx),
                ))
        });
        let open = matches!(self.packs.phase(now), Phase::Opening | Phase::Open);
        let select = div()
            .relative()
            .on_mouse_down_out(cx.listener(move |this, _: &MouseDownEvent, _, cx| {
                if open {
                    this.set_packs(false, cx);
                }
            }))
            .child(
                flat_chip("file-icon-pick", pack.label(), theme)
                    .min_w(px(SELECT_WIDTH))
                    .justify_between()
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        cx.stop_propagation();
                        this.set_packs(!open, cx);
                    })),
            )
            .children(shown.map(|list| deferred(list).priority(1)));
        div()
            .flex()
            .items_center()
            .gap(px(PICKER_GAP))
            .child(grid)
            .child(select)
    }

    fn desk_row(
        &mut self,
        setting: &'static Setting,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> SettingRow {
        let control = match setting.control {
            Control::Switch(start) => {
                let on = self.flipped.get(setting.key).copied().unwrap_or(start);
                switch_bare(setting.key, setting.name, on, theme)
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        cx.stop_propagation();
                        this.flip_desk(setting.key, cx);
                    }))
                    .into_any_element()
            }
            Control::Stored => {
                let on = self
                    .stored
                    .as_ref()
                    .is_ok_and(|stored| stored.switch(setting.key));
                switch_bare(setting.key, setting.name, on, theme)
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        cx.stop_propagation();
                        this.flip_desk(setting.key, cx);
                    }))
                    .into_any_element()
            }
            Control::Pack => self.pack_picker(theme, window, cx).into_any_element(),
        };
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
        let groups: Vec<(&str, Vec<SettingRow>)> = shown
            .groups
            .iter()
            .map(|(label, settings)| {
                let rows = settings
                    .iter()
                    .map(|setting| self.desk_row(setting, theme, window, cx));
                (*label, rows.collect())
            })
            .collect();
        self.log_page(page.name(), groups.iter().map(|(_, rows)| rows.len()).sum());
        div()
            .child(title)
            .children(groups.into_iter().map(|(label, rows)| {
                setting_group_clickable(
                    label,
                    rows,
                    cx.listener(|this, key: &SharedString, _, cx| this.flip_desk(key, cx)),
                    theme,
                )
            }))
    }

    fn tofu_page(
        &mut self,
        name: &str,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> AnyElement {
        if self.tofu.is_none() {
            return page_title(name.to_owned(), Some(NO_TOFU.into()), theme).into_any_element();
        }
        let Some(read) = self.answer.read.clone() else {
            let reading = match &self.answer.failed {
                Some(error) => frame::note(error.to_string(), theme).truncate(),
                None => frame::note(WAITING, theme),
            };
            return div()
                .child(page_title(name.to_owned(), None, theme))
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
                name.to_owned(),
                Some(format!("tofu reports no {name} settings").into()),
                theme,
            )
            .into_any_element();
        };
        let rows: Vec<SettingRow> = category
            .settings
            .iter()
            .map(|setting| self.tofu_row(setting, theme, window, cx))
            .collect();
        self.log_page(name, rows.len());
        let scope = self.scope.wire();
        let about = format!(
            "Read at {}. Changes go to {}.",
            read.at,
            settings.file(scope)
        );
        div()
            .child(page_title(name.to_owned(), Some(about.into()), theme))
            .children(
                self.answer
                    .failed
                    .as_ref()
                    .map(|error| frame::note(format!("the last read failed: {error}"), theme)),
            )
            .child(setting_group_clickable(
                format!("{} keys", rows.len()),
                rows,
                cx.listener(|this, key: &SharedString, _, cx| this.flip_tofu(key, cx)),
                theme,
            ))
            .into_any_element()
    }

    fn log_page(&mut self, name: &str, count: usize) {
        if self.shown.as_ref() != Some(&self.page) {
            self.shown = Some(self.page.clone());
            eprintln!("desk: settings page {name} shows {count} rows");
        }
    }
}

fn desk_settings() -> impl Iterator<Item = &'static Setting> {
    DESK_NAV
        .1
        .iter()
        .flat_map(|page| page.page().groups.iter())
        .flat_map(|(_, settings)| settings.iter())
}

fn page_name(page: &PageId) -> SharedString {
    match page {
        PageId::Desk(page) => page.name().into(),
        PageId::Tofu(name) => name.clone(),
        PageId::Plugins(None) => PLUGINS_GENERAL.into(),
        PageId::Plugins(Some(id)) => {
            plugins::find(id).map_or_else(|| id.clone(), |plugin| plugin.name.clone().into())
        }
    }
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
        let scope = frame::tile_tabs(
            "settings-scope",
            &[
                (Scope::Everywhere, "Everywhere"),
                (Scope::Project, "This project"),
            ],
            self.scope,
            &theme,
            cx,
            |rows, scope, _| rows.scope = scope,
        );
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
            frame::tabbed("settings", scope, None),
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
