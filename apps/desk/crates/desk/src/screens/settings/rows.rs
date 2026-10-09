use std::collections::HashMap;
use std::sync::Arc;
use std::time::Instant;

use desk_core::protocol::request::{QuerySettings, SettingsSet};
use desk_core::protocol::{NoParams, Request, SettingsReport, SettingsSetParams, VerbResult};
use desk_core::query::{Answer, QueryError, Read};
use desk_core::settings::{self as wire, Settings, Value, refusal};
use desk_tiling::SHORTCUTS;
use desk_ui::components::chip::flat_chip;
use desk_ui::components::form::{TextInput, input, switch_bare};
use desk_ui::components::list::{HoverList, group_header, row};
use desk_ui::components::overlay::{MenuItem, menu};
use desk_ui::components::paint::{ms, presented, tint};
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::settings::{
    SettingRow, Source, key_binding, page_title, setting_group, setting_group_clickable,
};
use desk_ui::components::skeleton::skeleton_lines;
use desk_ui::components::tree::{IconPack, IconTheme, pick_pack, picked_pack};
use desk_ui::live::ActiveTheme;
use desk_ui::motion::{Phase, Presence, reduced_motion};
use desk_ui::theme::{ColorToken, NumberToken, Theme};
use gpui::{
    AnyElement, ClickEvent, Context, Div, Entity, EntityId, EntityInputHandler, Focusable, Image,
    KeyDownEvent, MouseDownEvent, Render, SharedString, Subscription, WeakEntity, Window, deferred,
    div, img, prelude::*, px, relative,
};

use super::fixture::{
    Control, DESK_NAV, DeskPage, FIRST_TOFU_PAGE, KEYS_GROUP, PageId, Scope, Setting, TOFU_NAV,
};
use crate::modules::chat::Chat;
use crate::screens::frame;

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
const READING_LINES: usize = 8;
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
}

struct Field {
    input: Entity<TextInput>,
    seen: Option<String>,
    _blur: Subscription,
}

impl Rows {
    pub fn new(theme: &Theme) -> Self {
        Rows {
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
        }
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
        if let (PageId::Tofu(name), Some(read)) = (&self.page, &self.answer.read)
            && !read
                .value
                .categories
                .iter()
                .any(|category| category.name == **name)
            && let Some(first) = read.value.categories.first()
        {
            self.page = PageId::Tofu(first.name.clone().into());
        }
        cx.notify();
    }

    fn write(&mut self, key: String, value: String, cx: &mut Context<Self>) {
        let scope = self.scope.wire();
        eprintln!("desk: settings set {key} {value} in {}", scope.wire());
        self.problems.remove(&key);
        let params = SettingsSetParams {
            key: key.clone(),
            value,
            scope: Some(scope.wire().to_owned()),
        };
        self.send::<SettingsSet>(params, cx, move |rows, reply, cx| {
            rows.written(key, reply, cx)
        });
        cx.notify();
    }

    fn written(&mut self, key: String, reply: Result<VerbResult, String>, cx: &mut Context<Self>) {
        match reply.map_or_else(Some, |result| refusal(&result)) {
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

    fn flip(&mut self, key: &str, cx: &mut Context<Self>) {
        if let PageId::Tofu(_) = self.page {
            let on = self.switching.get(key).copied().or_else(|| {
                let read = self.answer.read.as_ref()?;
                match read.value.get(key)?.value {
                    Value::Switch(on) => Some(on),
                    Value::Number(_) | Value::Text(_) => None,
                }
            });
            let Some(on) = on else {
                return;
            };
            self.switching.insert(key.to_owned(), !on);
            return self.write(key.to_owned(), (!on).to_string(), cx);
        }
        let PageId::Desk(page) = self.page else {
            return;
        };
        let found = page
            .page()
            .groups
            .iter()
            .flat_map(|(_, settings)| settings.iter())
            .find_map(|setting| match setting.control {
                Control::Switch(start) if setting.key == key => Some((setting.key, start)),
                Control::Switch(_) | Control::Pack => None,
            });
        let Some((key, start)) = found else {
            return;
        };
        let now = self.flipped.get(key).copied().unwrap_or(start);
        self.flipped.insert(key, !now);
        eprintln!("desk: settings {key} {now} -> {}", !now);
        cx.notify();
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
                        this.flip(&flipped, cx);
                    }))
                    .into_any_element()
            }
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

    fn nav(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let mut list = HoverList::new("settings-nav", theme);
        let mut at = 0;
        let mut selected = None;
        let tofu: Vec<PageId> = self
            .answer
            .read
            .iter()
            .flat_map(|read| &read.value.categories)
            .map(|category| PageId::Tofu(category.name.clone().into()))
            .collect();
        let desk: Vec<PageId> = DESK_NAV.1.iter().copied().map(PageId::Desk).collect();
        for (label, pages) in [(TOFU_NAV, tofu), (DESK_NAV.0, desk)] {
            list = list.inert(group_header(label, label, pages.len(), true, theme));
            for page in pages {
                at += 1;
                if page == self.page {
                    selected = Some(at);
                }
                let name = page_name(&page);
                list = list.item(
                    row(name.clone(), page == self.page, false, theme)
                        .child(name)
                        .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                            this.page = page.clone();
                            cx.notify();
                        })),
                );
            }
            at += 1;
        }
        div()
            .w(relative(NAV_SHARE))
            .min_w(px(NAV_LEAST))
            .max_w(px(NAV_WIDTH))
            .flex_none()
            .flex()
            .flex_col()
            .py_2p5()
            .px_2()
            .border_r_1()
            .border_color(tint(theme.color(ColorToken::Shadow), NAV_RULE))
            .text_size(px(NAV_TEXT))
            .child(
                input(
                    "settings-search",
                    "Search settings",
                    SharedString::default(),
                    false,
                    theme,
                )
                .w_full()
                .mb_2p5(),
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
                        this.flip(setting.key, cx);
                    }))
                    .into_any_element()
            }
            Control::Pack => self.pack_picker(theme, window, cx).into_any_element(),
        };
        SettingRow {
            id: setting.key.into(),
            name: setting.name.into(),
            about: setting.desc.into(),
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
        if page == DeskPage::Keys {
            let keys = SHORTCUTS.iter().map(|shortcut| SettingRow {
                id: shortcut.label.into(),
                name: shortcut.label.into(),
                about: SharedString::default(),
                source: Source::Default,
                control: key_binding(
                    shortcut.keys.split(' ').map(SharedString::from).collect(),
                    None,
                    theme,
                )
                .into_any_element(),
            });
            self.log_page(page.name(), keys.len());
            return div()
                .child(title)
                .child(setting_group(KEYS_GROUP, keys.collect(), theme));
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
                    cx.listener(|this, key: &SharedString, _, cx| this.flip(key, cx)),
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
                Some(error) => frame::note(error.to_string(), theme),
                None => skeleton_lines("settings-reading", READING_LINES, theme, cx),
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
                cx.listener(|this, key: &SharedString, _, cx| this.flip(key, cx)),
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

fn page_name(page: &PageId) -> SharedString {
    match page {
        PageId::Desk(page) => page.name().into(),
        PageId::Tofu(name) => name.clone(),
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
        let page_body = match self.page.clone() {
            PageId::Desk(page) => self.desk_page(page, &theme, window, cx).into_any_element(),
            PageId::Tofu(name) => self.tofu_page(&name, &theme, window, cx),
        };
        let content = div().flex_1().min_w_0().flex().flex_col().child(
            ScrollArea::new("settings-content").child(
                div()
                    .px(px(CONTENT_PAD_X))
                    .py(px(CONTENT_PAD_Y))
                    .flex()
                    .flex_col()
                    .child(page_body),
            ),
        );
        frame::tile(
            frame::tabbed("settings", scope, None),
            &theme,
            div()
                .flex_1()
                .min_h_0()
                .flex()
                .child(self.nav(&theme, cx))
                .child(content),
        )
    }
}
