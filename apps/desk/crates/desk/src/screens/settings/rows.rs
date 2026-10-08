use std::collections::HashMap;

use desk_tiling::SHORTCUTS;
use desk_ui::components::card::{Header, inner_card, shell};
use desk_ui::components::chip::{chip, mono};
use desk_ui::components::form::{input, segmented, switch_bare};
use desk_ui::components::list::{HoverList, group_header, row};
use desk_ui::components::paint::tint;
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::settings::{
    SettingRow, Source, key_binding, page_title, setting_group, setting_group_clickable,
};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{ClickEvent, Context, Div, Render, SharedString, Window, div, prelude::*, px, relative};

use super::board::root;
use super::fixture::{Control, KEYS_GROUP, NAV, PageId, Scope, Setting};

const NAV_WIDTH: f32 = 247.0;
const NAV_LEAST: f32 = 150.0;
const NAV_SHARE: f32 = 0.3;
const NAV_TEXT: f32 = 13.5;
const NAV_RULE: f32 = 0.35;
const CONTENT_PAD_X: f32 = 28.0;
const CONTENT_PAD_Y: f32 = 20.0;
const VALUE_TEXT: f32 = 12.0;
const SHELL_BOTTOM: f32 = 8.0;

pub struct Rows {
    page: PageId,
    scope: Scope,
    flipped: HashMap<&'static str, bool>,
    shown: Option<PageId>,
}

impl Rows {
    pub fn new() -> Self {
        Rows {
            page: PageId::Turn,
            scope: Scope::Project,
            flipped: HashMap::new(),
            shown: None,
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

    fn row(&self, setting: &'static Setting, theme: &Theme, cx: &mut Context<Self>) -> SettingRow {
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
        &self,
        theme: &Theme,
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
        self.page
            .page()
            .groups
            .iter()
            .map(|(label, settings)| {
                let rows = settings.iter().map(|setting| self.row(setting, theme, cx));
                (*label, rows.collect())
            })
            .collect()
    }
}

impl Render for Rows {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let chosen = Scope::ALL
            .iter()
            .position(|scope| *scope == self.scope)
            .unwrap_or(0);
        let scope = segmented(
            "scope",
            &Scope::LABELS,
            chosen,
            &theme,
            cx.listener(|this, at: &usize, _, cx| {
                if let Some(scope) = Scope::ALL.get(*at) {
                    this.scope = *scope;
                    cx.notify();
                }
            }),
        );
        let page = self.page.page();
        let groups = self.groups(&theme, cx);
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
        let body = shell(
            Header::Title(None, "Settings".into(), Some(scope.into_any_element())),
            &theme,
        )
        .flex_1()
        .mb(px(SHELL_BOTTOM))
        .child(
            inner_card(&theme)
                .flex_row()
                .child(self.nav(&theme, cx))
                .child(content),
        );
        root(&theme, body, None, |_, _, _| {})
    }
}
