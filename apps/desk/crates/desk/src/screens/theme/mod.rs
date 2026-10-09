use std::fs;

use desk_core::control::TELL_BADGE;
use desk_ui::components::card::{caption, inner_card};
use desk_ui::components::form::segmented;
use desk_ui::components::list::{HoverList, row};
use desk_ui::components::overlay::toast;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::Mode;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Render, SharedString, Window, div, prelude::*,
    px,
};

use crate::pick;
use crate::screens::frame;

const LIST_WIDTH: f32 = 300.0;
const MODES: [Mode; 2] = [Mode::Dark, Mode::Light];

struct Choice {
    file: String,
    label: SharedString,
}

struct ThemeScreen {
    choices: Vec<Choice>,
    active: String,
    told: Option<SharedString>,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(other) = board.filter(|board| *board != "ITHEME-1") {
        return Err(format!("the theme screen draws ITHEME-1, not {other}"));
    }
    let screen = ThemeScreen {
        choices: choices()?,
        active: pick::chosen()?.theme,
        told: None,
    };
    Ok(cx.new(|_| screen).into())
}

fn choices() -> Result<Vec<Choice>, String> {
    let dir = pick::folder();
    let unlisted =
        |error: std::io::Error| format!("the theme screen cannot list {}: {error}", dir.display());
    let mut found = Vec::new();
    for entry in fs::read_dir(&dir).map_err(unlisted)? {
        let path = entry.map_err(unlisted)?.path();
        let Some(file) = path
            .extension()
            .filter(|extension| *extension == "json")
            .and_then(|_| path.file_stem())
            .and_then(|stem| stem.to_str())
        else {
            continue;
        };
        let named = fs::read_to_string(&path)
            .ok()
            .and_then(|text| serde_json::from_str::<serde_json::Value>(&text).ok())
            .and_then(|theme| theme.get("name")?.as_str().map(str::to_owned));
        found.push(Choice {
            file: file.to_owned(),
            label: named.unwrap_or_else(|| file.to_owned()).into(),
        });
    }
    found.sort_by(|left, right| left.label.cmp(&right.label));
    Ok(found)
}

impl ThemeScreen {
    fn choose(&mut self, file: Option<String>, mode: Mode, cx: &mut Context<Self>) {
        self.told = self.apply(file, mode, cx).err().map(|error| {
            eprintln!("{error}");
            error.into()
        });
        cx.notify();
    }

    fn apply(&mut self, file: Option<String>, mode: Mode, cx: &mut App) -> Result<(), String> {
        if let Some(file) = file.filter(|file| *file != self.active) {
            pick::start(&file, cx)?;
            self.active = file;
        }
        pick::set_mode(&self.active, Some(mode), cx)?;
        pick::save(&self.active, ActiveTheme::mode(cx))
    }
}

impl Render for ThemeScreen {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let rows: Vec<_> = self
            .choices
            .iter()
            .map(|choice| {
                let file = choice.file.clone();
                row(
                    SharedString::from(choice.file.clone()),
                    choice.file == self.active,
                    false,
                    &theme,
                )
                .child(choice.label.clone())
                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    this.choose(Some(file.clone()), ActiveTheme::mode(cx), cx);
                }))
            })
            .collect();
        let active = self
            .choices
            .iter()
            .position(|choice| choice.file == self.active);
        let mode = match theme.mode() {
            Mode::Dark => 0,
            Mode::Light => 1,
        };
        let modes = segmented(
            "mode",
            &MODES.map(Mode::label),
            mode,
            &theme,
            cx.listener(|this, at: &usize, _, cx| {
                if let Some(mode) = MODES.get(*at) {
                    this.choose(None, *mode, cx);
                }
            }),
        );
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
        let body = div()
            .gap_3()
            .child(caption("Themes", &theme))
            .child(
                inner_card(&theme)
                    .flex_none()
                    .w(px(LIST_WIDTH))
                    .p_2()
                    .child(
                        HoverList::new("themes", &theme)
                            .items(rows)
                            .selected(active),
                    ),
            )
            .child(caption("Mode", &theme))
            .child(div().flex().child(modes));
        frame::window(&theme, body).children(told)
    }
}
