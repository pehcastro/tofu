use desk_ui::components::color_picker::{self, ColorPicked, ColorPicker};
use desk_ui::components::overlay::{Align, Dismissed, Placement, Popover};
use desk_ui::components::paint::{ink, ring};
use desk_ui::components::size::{RADIUS_ROW, T2};
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Mode, Theme, WordToken};
use gpui::{
    App, ClickEvent, Context, Entity, Rgba, Subscription, Window, div, hsla_to_rgba, prelude::*,
    px, rgb_to_hsla,
};
use serde_json::Value;
use std::path::Path;

use super::{HEX_WIDTH, IntroSetup, PICKED_FILL, SMALL, SWATCH, THUMB_RING};
use crate::pick;
use crate::screens::theme::catalog;

pub const TOKENS: [(ColorToken, &str); 9] = [
    (ColorToken::IntroComposerFill, "Composer fill"),
    (ColorToken::IntroComposerBorder, "Composer border"),
    (ColorToken::IntroPlaceholder, "Placeholder"),
    (ColorToken::IntroIcon, "Icon"),
    (ColorToken::IntroModelText, "Model text"),
    (ColorToken::IntroSendFill, "Send fill"),
    (ColorToken::IntroSendIcon, "Send icon"),
    (ColorToken::IntroLink, "Link"),
    (ColorToken::IntroLinkHover, "Link hover"),
];

pub struct Editing {
    pub token: ColorToken,
    pub picker: Entity<ColorPicker>,
    pub live: Option<Rgba>,
    _picked: Subscription,
}

impl Editing {
    pub fn open(
        token: ColorToken,
        color: Rgba,
        window: &mut Window,
        cx: &mut Context<IntroSetup>,
    ) -> Editing {
        let picker = cx.new(|cx| ColorPicker::new(rgb_to_hsla(color), window, cx));
        let picked = cx.subscribe(&picker, |setup, _, picked, cx| setup.recoloured(picked, cx));
        Editing {
            token,
            picker,
            live: None,
            _picked: picked,
        }
    }
}

impl IntroSetup {
    pub(super) fn colour_row(
        &self,
        at: usize,
        (token, name): (ColorToken, &'static str),
        theme: &Theme,
        cx: &mut Context<Self>,
    ) -> Popover {
        let color = self.ink(theme, token);
        let picked = self
            .editing
            .as_ref()
            .filter(|editing| editing.token == token);
        let row = div()
            .id(("intro-colour", at))
            .flex()
            .items_center()
            .gap_2()
            .px_1()
            .py(px(5.0))
            .rounded(px(RADIUS_ROW))
            .cursor_pointer()
            .when(picked.is_some(), |row| row.bg(ink(theme, PICKED_FILL)))
            .on_click(cx.listener(move |this, _: &ClickEvent, window, cx| {
                this.pick_colour(token, window, cx)
            }))
            .child(div().flex_1().child(name))
            .child(
                div()
                    .size(px(SWATCH))
                    .rounded(px(RADIUS_ROW))
                    .bg(color)
                    .shadow(vec![ring(ink(theme, THUMB_RING))]),
            )
            .child(
                div()
                    .w(px(HEX_WIDTH))
                    .text_size(px(SMALL))
                    .font_family(theme.word(WordToken::ShapeMono))
                    .text_color(ink(theme, T2))
                    .child(catalog::hex(color)),
            );
        color_picker::popover(
            ("intro-colour-pop", at),
            row,
            picked.map(|editing| editing.picker.clone()),
            cx.listener(|this, _: &Dismissed, _, cx| {
                if this.editing.take().is_some() {
                    cx.notify();
                }
            }),
        )
        .placement(Placement {
            align: Align::End,
            ..Placement::below()
        })
    }

    fn recoloured(&mut self, picked: &ColorPicked, cx: &mut Context<Self>) {
        let Some(editing) = self.editing.as_mut() else {
            return;
        };
        let (color, settled) = match picked {
            ColorPicked::Moving(color) => (hsla_to_rgba(*color), false),
            ColorPicked::Settled(color) => (hsla_to_rgba(*color), true),
        };
        editing.live = Some(color);
        let token = editing.token;
        if settled && let Err(error) = save(token, color, cx) {
            self.told = Some(format!("The intro colour was not saved: {error}").into());
        }
        cx.notify();
    }
}

fn save(token: ColorToken, color: Rgba, cx: &mut App) -> Result<(), String> {
    let mode = ActiveTheme::mode(cx);
    let dir = pick::person()?;
    let active = pick::chosen()?.theme;
    if dir.join(format!("{active}.json")).is_file() {
        return write(&dir, &active, token, mode, color);
    }
    let entries = catalog::read(&pick::folders()?)?;
    let entry = entries
        .iter()
        .find(|entry| entry.file == active)
        .ok_or_else(|| format!("{active} is not a listed theme"))?;
    let copied = catalog::edited(&dir, entry)?;
    write(&dir, &copied, token, mode, color)?;
    eprintln!(
        "desk: intro setup saved {} into {copied}.json",
        token.path()
    );
    pick::start(&copied, cx)?;
    pick::set_mode(&copied, Some(mode), cx)?;
    pick::save(&copied, mode)
}

fn write(dir: &Path, file: &str, token: ColorToken, mode: Mode, color: Rgba) -> Result<(), String> {
    let path = |mode: Mode| -> Vec<&str> {
        std::iter::once(mode.label())
            .chain(token.path().split('.'))
            .collect()
    };
    let other = mode.other();
    let at = dir.join(format!("{file}.json"));
    let text =
        std::fs::read_to_string(&at).map_err(|error| format!("{}: {error}", at.display()))?;
    let json: Value = serde_json::from_str(&text).map_err(|error| format!("{file}: {error}"))?;
    let held = path(other)
        .iter()
        .try_fold(&json, |value, key| value.get(key))
        .is_some();
    if !held {
        let folders = pick::folders()?;
        let entries = catalog::read(&folders)?;
        let entry = entries
            .iter()
            .find(|entry| entry.file == file)
            .ok_or_else(|| format!("{file} is not a listed theme"))?;
        let current = catalog::Draft::open(&folders, entry, other)?
            .theme
            .color(token);
        catalog::set(
            dir,
            file,
            &path(other),
            Value::String(catalog::hex(current)),
        )?;
    }
    catalog::set(dir, file, &path(mode), Value::String(catalog::hex(color)))?;
    Ok(())
}
