use std::collections::HashMap;
use std::sync::Arc;
use std::time::Instant;

use desk_tiling::SHORTCUTS;
use desk_ui::components::chip::{chip, flat_chip, mono};
use desk_ui::components::form::{input, switch_bare};
use desk_ui::components::list::{HoverList, group_header, row};
use desk_ui::components::overlay::{MenuItem, menu};
use desk_ui::components::paint::{ms, presented, tint};
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::settings::{
    SettingRow, Source, key_binding, page_title, setting_group, setting_group_clickable,
};
use desk_ui::components::tree::{IconPack, IconTheme, pick_pack, picked_pack};
use desk_ui::live::ActiveTheme;
use desk_ui::motion::{Phase, Presence, reduced_motion};
use desk_ui::theme::{ColorToken, NumberToken, Theme};
use gpui::{
    ClickEvent, Context, Div, Image, MouseDownEvent, Render, SharedString, Window, deferred, div,
    img, prelude::*, px, relative,
};

use super::fixture::{Control, KEYS_GROUP, NAV, PageId, Scope, Setting};
use crate::screens::frame;

const NAV_WIDTH: f32 = 247.0;
const NAV_LEAST: f32 = 150.0;
const NAV_SHARE: f32 = 0.3;
const NAV_TEXT: f32 = 13.5;
const NAV_RULE: f32 = 0.35;
const CONTENT_PAD_X: f32 = 28.0;
const CONTENT_PAD_Y: f32 = 20.0;
const VALUE_TEXT: f32 = 12.0;
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

pub struct Rows {
    page: PageId,
    scope: Scope,
    flipped: HashMap<&'static str, bool>,
    shown: Option<PageId>,
    packs: Presence,
    preview: Option<(IconPack, Vec<Arc<Image>>)>,
}

impl Rows {
    pub fn new(theme: &Theme) -> Self {
        Rows {
            page: PageId::Turn,
            scope: Scope::Project,
            flipped: HashMap::new(),
            shown: None,
            packs: Presence::new(
                ms(theme, NumberToken::MotionEnter),
                ms(theme, NumberToken::MotionExit),
            ),
            preview: None,
        }
    }

    fn nav(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let mut list = HoverList::new("settings-nav", theme);
        let mut at = 0;
        let mut selected = None;
        for (label, pages) in NAV {
            list = list.inert(group_header(label, label, pages.len(), true, theme));
            for page in pages.iter().copied() {
                at += 1;
                if page == self.page {
                    selected = Some(at);
                }
                list = list.item(
                    row(page.name(), page == self.page, false, theme)
                        .child(page.name())
                        .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                            this.page = page;
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

    fn flip(&mut self, key: &str, cx: &mut Context<Self>) {
        let found = self
            .page
            .page()
            .groups
            .iter()
            .flat_map(|(_, settings)| settings.iter())
            .find_map(|setting| match setting.control {
                Control::Switch(start) if setting.key == key => Some((setting.key, start)),
                _ => None,
            });
        let Some((key, start)) = found else {
            return;
        };
        let now = self.flipped.get(key).copied().unwrap_or(start);
        self.flipped.insert(key, !now);
        eprintln!("desk: settings {key} {now} -> {}", !now);
        cx.notify();
    }

    fn row(
        &mut self,
        setting: &'static Setting,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> SettingRow {
        let flipped = self.flipped.get(setting.key).copied();
        let control = match setting.control {
            Control::Switch(start) => {
                switch_bare(setting.key, setting.name, flipped.unwrap_or(start), theme)
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        cx.stop_propagation();
                        this.flip(setting.key, cx);
                    }))
                    .into_any_element()
            }
            Control::Value(value) => chip(value, None, theme)
                .font_family(mono(theme))
                .text_size(px(VALUE_TEXT))
                .into_any_element(),
            Control::Pack => self.pack_picker(theme, window, cx).into_any_element(),
        };
        SettingRow {
            id: setting.key.into(),
            name: setting.name.into(),
            about: setting.desc.into(),
            source: flipped.map_or(setting.source, |_| self.scope.source()),
            control,
        }
    }

    fn groups(
        &mut self,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Vec<(&'static str, Vec<SettingRow>)> {
        if self.page == PageId::Keys {
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
            return vec![(KEYS_GROUP, keys.collect())];
        }
        let page = self.page.page();
        page.groups
            .iter()
            .map(|(label, settings)| {
                let rows = settings
                    .iter()
                    .map(|setting| self.row(setting, theme, window, cx));
                (*label, rows.collect())
            })
            .collect()
    }
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
        let page = self.page.page();
        let groups = self.groups(&theme, window, cx);
        let clickable = self.page != PageId::Keys;
        if self.shown != Some(self.page) {
            self.shown = Some(self.page);
            let count: usize = groups.iter().map(|(_, rows)| rows.len()).sum();
            eprintln!("desk: settings page {} shows {count} rows", page.title);
        }
        let page_body = div()
            .px(px(CONTENT_PAD_X))
            .py(px(CONTENT_PAD_Y))
            .flex()
            .flex_col()
            .child(page_title(
                page.title,
                Some(page.desc)
                    .filter(|desc| !desc.is_empty())
                    .map(Into::into),
                &theme,
            ))
            .children(groups.into_iter().map(|(label, rows)| {
                if clickable {
                    setting_group_clickable(
                        label,
                        rows,
                        cx.listener(|this, key: &SharedString, _, cx| this.flip(key, cx)),
                        &theme,
                    )
                } else {
                    setting_group(label, rows, &theme)
                }
            }));
        let content = div()
            .flex_1()
            .min_w_0()
            .flex()
            .flex_col()
            .child(ScrollArea::new("settings-content").child(page_body));
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
