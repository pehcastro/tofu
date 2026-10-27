use std::path::PathBuf;
use std::sync::Arc;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::overlay::{MenuButton, MenuItem};
use desk_ui::components::paint::{ink, tint};
use desk_ui::components::settings::{SettingRow, Source, setting_group};
use desk_ui::components::size::T3;
use desk_ui::components::tree::{IconPack, IconTheme, pick_pack, picked_pack};
use desk_ui::live::{self, ActiveTheme};
use desk_ui::theme::{ColorToken, Mode, Theme};
use gpui::{
    Context, Div, Entity, Image, Rgba, SharedString, Subscription, Task, Window, div, img,
    prelude::*, px,
};

use crate::desk::{Desk, Unpicked};

const THEME_ID: &str = "screen.theme";
const INTRO_SETUP_ID: &str = "screen.intro-setup";
const CARD_WIDTH: f32 = 168.0;
const CARD_GAP: f32 = 12.0;
const CARD_PAD: f32 = 10.0;
const CARD_RADIUS: f32 = 10.0;
const CARD_RING: f32 = 0.5;
const CARD_HOVER: f32 = 0.06;
const SWATCH: f32 = 18.0;
const SWATCH_GAP: f32 = 4.0;
const SWATCH_HEIGHT: f32 = 44.0;
const NAME_SIZE: f32 = 12.5;
const SECTION_GAP: f32 = 20.0;
const PREVIEW_FILES: [&str; 4] = ["index.ts", "main.rs", "README.md", "package.json"];
const PREVIEW_FOLDER: &str = "src";
const PREVIEW_COLUMNS: u16 = 3;
const PREVIEW_ICON: f32 = 16.0;
const PREVIEW_GAP: f32 = 6.0;
const PREVIEW_PAD: f32 = 6.0;
const PREVIEW_RADIUS: f32 = 8.0;
const PREVIEW_FILL: f32 = 0.25;
const PICKER_GAP: f32 = 12.0;
const SWATCHES: [ColorToken; 6] = [
    ColorToken::SurfaceWindow,
    ColorToken::CardsOuterFill,
    ColorToken::TextBase,
    ColorToken::StatusAccent,
    ColorToken::SyntaxKeyword,
    ColorToken::SyntaxString,
];

struct ThemeCard {
    file: String,
    name: SharedString,
    swatches: Vec<Rgba>,
    person: Option<PathBuf>,
}

enum Cards {
    Reading,
    Read(Vec<ThemeCard>),
    Failed(SharedString),
}

pub struct Appearance {
    cards: Cards,
    picked: String,
    problem: Option<SharedString>,
    preview: Option<(IconPack, Vec<Arc<Image>>)>,
    packs: Entity<MenuButton>,
    pending: Option<Reread>,
    reading: Option<Task<()>>,
    _changes: [Subscription; 2],
}

#[derive(Clone, Copy)]
enum Reread {
    CardsAndPick,
    Cards,
}

impl Appearance {
    pub fn new(_: &mut Window, cx: &mut Context<Self>) -> Self {
        let pack = picked_pack(cx);
        let packs = pack_menu(pack, cx);
        let mut appearance = Appearance {
            cards: Cards::Reading,
            picked: String::new(),
            problem: None,
            preview: None,
            packs,
            pending: None,
            reading: None,
            _changes: [
                cx.observe_global::<crate::pick::Listing>(|this, cx| {
                    this.read(Reread::CardsAndPick, cx);
                }),
                cx.observe_global::<ActiveTheme>(|this, cx| this.read(Reread::Cards, cx)),
            ],
        };
        appearance.read(Reread::CardsAndPick, cx);
        appearance
    }

    fn read(&mut self, reread: Reread, cx: &mut Context<Self>) {
        let reread = match self.pending {
            Some(Reread::CardsAndPick) => Reread::CardsAndPick,
            Some(Reread::Cards) | None => reread,
        };
        self.pending = Some(reread);
        let mode = ActiveTheme::mode(cx);
        let reading = cx.background_executor().spawn(async move {
            let chosen = match reread {
                Reread::CardsAndPick => Some(crate::pick::chosen()),
                Reread::Cards => None,
            };
            (read_cards(mode), chosen)
        });
        let task = cx.spawn(async move |this, cx| {
            let (cards, chosen) = reading.await;
            let landed = this.update(cx, |this, cx| {
                this.pending = None;
                this.cards = match cards {
                    Ok(cards) => Cards::Read(cards),
                    Err(problem) => Cards::Failed(problem.into()),
                };
                match chosen {
                    Some(Ok(chosen)) => this.picked = chosen.theme,
                    Some(Err(problem)) => {
                        eprintln!("desk: appearance reads no theme pick: {problem}");
                    }
                    None => {}
                }
                cx.notify();
            });
            if let Err(error) = landed {
                eprintln!("desk: appearance closed before its themes were read: {error}");
            }
        });
        self.reading = Some(task);
    }

    fn apply(&mut self, file: String, cx: &mut Context<Self>) {
        let mode = ActiveTheme::mode(cx);
        let applied = crate::pick::start(&file, cx)
            .and_then(|()| crate::pick::set_mode(&file, Some(mode), cx));
        #[cfg(feature = "screen-theme")]
        let applied = applied.and_then(|()| crate::pick::save(&file, ActiveTheme::mode(cx)));
        match applied {
            Ok(()) => {
                eprintln!("desk: appearance theme {} -> {file}", self.picked);
                self.picked = file;
                self.problem = None;
                self.packs = pack_menu(picked_pack(cx), cx);
            }
            Err(problem) => {
                eprintln!("desk: appearance theme not applied: {problem}");
                self.problem = Some(problem.into());
            }
        }
        cx.notify();
    }

    fn pick_pack_at(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(pack) = IconPack::ALL.get(at).copied() else {
            return eprintln!("desk: appearance has no file icon pack at {at}");
        };
        let person = match &self.cards {
            Cards::Read(cards) => cards
                .iter()
                .find(|card| card.file == self.picked)
                .and_then(|card| card.person.clone()),
            Cards::Reading | Cards::Failed(_) => None,
        };
        let saved = match person {
            Some(file) => live::set_icons(&file, pack)
                .map(|()| format!("into {}", file.display()))
                .map_err(|error| error.to_string()),
            None => pick_pack(pack, cx)
                .map(|()| "as the desk pick".to_owned())
                .map_err(|error| error.to_string()),
        };
        match saved {
            Ok(said) => {
                eprintln!("desk: appearance file icons {} saved {said}", pack.key());
                self.problem = None;
            }
            Err(problem) => {
                eprintln!(
                    "desk: appearance file icons {} not saved: {problem}",
                    pack.key()
                );
                self.problem = Some(problem.into());
            }
        }
        self.packs = pack_menu(pack, cx);
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
                eprintln!("desk: appearance shows no {} preview: {error}", pack.key());
                Vec::new()
            }
        };
        self.preview = Some((pack, images.clone()));
        images
    }

    fn cards(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let grid = div().flex().flex_wrap().gap(px(CARD_GAP));
        let cards = match &self.cards {
            Cards::Reading => return grid.text_color(ink(theme, T3)).child("Reading themes"),
            Cards::Failed(problem) => {
                return grid.text_color(ink(theme, T3)).child(problem.clone());
            }
            Cards::Read(cards) => cards,
        };
        let accent = theme.color(ColorToken::StatusAccent);
        let hover = tint(theme.color(ColorToken::TextBase), CARD_HOVER);
        grid.children(cards.iter().map(|card| {
            let in_use = card.file == self.picked;
            let file = card.file.clone();
            div()
                .id(SharedString::from(format!("theme-card-{}", card.file)))
                .w(px(CARD_WIDTH))
                .flex()
                .flex_col()
                .gap(px(CARD_PAD))
                .p(px(CARD_PAD))
                .rounded(px(CARD_RADIUS))
                .cursor_pointer()
                .bg(theme.color(ColorToken::CardsInnerFill))
                .border_1()
                .border_color(if in_use {
                    accent
                } else {
                    tint(theme.color(ColorToken::TextBase), CARD_HOVER)
                })
                .hover(move |style| style.bg(hover))
                .on_click(cx.listener(move |this, _, _, cx| this.apply(file.clone(), cx)))
                .child(
                    div()
                        .h(px(SWATCH_HEIGHT))
                        .flex()
                        .items_end()
                        .gap(px(SWATCH_GAP))
                        .p(px(SWATCH_GAP))
                        .rounded(px(CARD_RADIUS - CARD_PAD / 2.0))
                        .bg(card.swatches.first().copied().unwrap_or(accent))
                        .children(card.swatches.iter().skip(1).map(|color| {
                            div()
                                .size(px(SWATCH))
                                .rounded_full()
                                .border_1()
                                .border_color(tint(theme.color(ColorToken::TextBase), CARD_RING))
                                .bg(*color)
                        })),
                )
                .child(
                    div()
                        .flex()
                        .justify_between()
                        .text_size(px(NAME_SIZE))
                        .child(card.name.clone())
                        .when(in_use, |row| {
                            row.child(div().text_color(ink(theme, T3)).child("in use"))
                        }),
                )
        }))
    }

    fn icons(&mut self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let pack = picked_pack(cx);
        let grid = div()
            .flex_none()
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
        div()
            .flex()
            .items_center()
            .gap(px(PICKER_GAP))
            .child(grid)
            .child(self.packs.clone())
    }
}

impl Render for Appearance {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let manage = button(
            "appearance-manage",
            "Manage themes",
            Some(Glyph::Palette),
            ButtonKind::Plain,
            &theme,
        )
        .on_click(|_, window, cx| open_screen(THEME_ID, window, cx));
        let intro = button(
            "appearance-intro",
            "Manage",
            None,
            ButtonKind::Opener,
            &theme,
        )
        .on_click(|_, window, cx| open_screen(INTRO_SETUP_ID, window, cx));
        let rows = vec![
            SettingRow {
                id: "appearance-icons".into(),
                name: "File icons".into(),
                about:
                    "The icon pack the editor tree draws. A theme of yours keeps it in its file."
                        .into(),
                source: Source::Default,
                control: self.icons(&theme, cx).into_any_element(),
            },
            SettingRow {
                id: "appearance-intro-row".into(),
                name: "Intro".into(),
                about: "The screen the desk opens on.".into(),
                source: Source::Default,
                control: intro.into_any_element(),
            },
        ];
        div()
            .flex()
            .flex_col()
            .gap(px(SECTION_GAP))
            .child(div().flex().justify_end().child(manage))
            .children(self.problem.clone().map(|problem| {
                div()
                    .text_color(theme.color(ColorToken::StatusDanger))
                    .child(problem)
            }))
            .child(self.cards(&theme, cx))
            .child(setting_group("Theme parts", rows, &theme))
    }
}

fn pack_menu(pack: IconPack, cx: &mut Context<Appearance>) -> Entity<MenuButton> {
    let items = IconPack::ALL
        .iter()
        .map(|each| MenuItem::Action {
            label: each.label().into(),
            keys: (*each == pack).then(|| "in use".into()),
            icon: None,
        })
        .collect();
    let menu = MenuButton::new(pack.label().into(), items, cx);
    let me = cx.entity().downgrade();
    menu.update(cx, |menu, _| {
        menu.on_pick(move |at, _, cx| {
            let at = *at;
            if let Err(error) = me.update(cx, |this, cx| this.pick_pack_at(at, cx)) {
                eprintln!("desk: appearance closed before its icon pick landed: {error}");
            }
        });
    });
    menu
}

fn open_screen(id: &str, window: &mut Window, cx: &mut gpui::App) {
    let Some(Some(desk)) = window.root::<Desk>() else {
        return eprintln!("desk: appearance found no desk window for {id}");
    };
    let picked = desk.update(cx, |desk, cx| desk.pick(id, window, cx));
    match picked {
        Ok(()) => eprintln!("desk: appearance opened {id}"),
        Err(Unpicked::Off(why) | Unpicked::Failed(why)) => {
            eprintln!("desk: appearance cannot open {id}: {why}");
        }
    }
}

fn read_cards(mode: Mode) -> Result<Vec<ThemeCard>, String> {
    let person = crate::pick::person()?;
    let folders = crate::pick::folders()?;
    let mut cards: Vec<ThemeCard> = Vec::new();
    for folder in &folders {
        let entries =
            std::fs::read_dir(folder).map_err(|error| format!("{}: {error}", folder.display()))?;
        for path in entries.filter_map(Result::ok).map(|entry| entry.path()) {
            let Some(file) = path
                .extension()
                .filter(|ext| *ext == "json")
                .and_then(|_| path.file_stem()?.to_str())
                .map(str::to_owned)
            else {
                continue;
            };
            if cards.iter().any(|card| card.file == file) {
                continue;
            }
            let swatches = match live::preview(&folders, &file, mode) {
                Ok(built) => SWATCHES.iter().map(|token| built.color(*token)).collect(),
                Err(error) => {
                    eprintln!("desk: appearance shows {file} without swatches: {error}");
                    Vec::new()
                }
            };
            cards.push(ThemeCard {
                name: shown_name(&file).into(),
                person: (*folder == person).then(|| path.clone()),
                file,
                swatches,
            });
        }
    }
    cards.sort_by(|a, b| a.file.cmp(&b.file));
    Ok(cards)
}

fn shown_name(file: &str) -> String {
    file.split('-')
        .map(|word| {
            let mut letters = word.chars();
            letters
                .next()
                .map(|first| first.to_uppercase().chain(letters).collect::<String>())
                .unwrap_or_default()
        })
        .collect::<Vec<_>>()
        .join(" ")
}
