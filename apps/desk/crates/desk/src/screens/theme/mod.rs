#[cfg(not(any(feature = "screen-settings", feature = "screen-accounts")))]
#[path = "../settings/board.rs"]
mod board;
#[cfg(feature = "screen-accounts")]
use crate::screens::accounts::board;
#[cfg(all(feature = "screen-settings", not(feature = "screen-accounts")))]
use crate::screens::settings::board;
mod fixture;

use board::segment;

use std::sync::Arc;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::{caption, inner_card, outer_card};
use desk_ui::components::chip::{chip, mono};
use desk_ui::components::paint::{ink, ring, tint};
use desk_ui::components::size::{T2, T3};
use desk_ui::theme::{ColorToken, Theme, build, parse_layer};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, FontWeight, Render, Rgba,
    SharedString, Window, div, prelude::*, px, relative,
};

use fixture::{
    AGENTS, DUPLICATE_TELL, GIT, GLASS, IMPORT_TELL, Knob, PALETTES, Palette, THEMES_NOTE,
    TOKEN_TELL, WORKERS,
};

const BODY_TEXT: f32 = 14.0;
const SHELL_HEADER: f32 = 28.0;
const TITLE: f32 = 19.0;
const DESC: f32 = 13.0;
const SMALL: f32 = 12.0;
const MONO_SMALL: f32 = 11.5;
const LIST_WIDTH: f32 = 300.0;
const TOKENS_WIDTH: f32 = 420.0;
const ROW_FILL: f32 = 0.18;
const ROW_RADIUS: f32 = 11.0;
const ROW_LINE: f32 = 18.0;
const NAME_TEXT: f32 = 13.5;
const PICKED_RING: f32 = 0.6;
const PICKED_RING_WIDTH: f32 = 1.5;
const RING: f32 = 0.08;
const SWATCH: f32 = 14.0;
const SWATCH_GAP: f32 = 3.0;
const SWATCH_RADIUS: f32 = 4.0;
const SWATCH_RING: f32 = 0.18;
const TOKEN_SWATCH: f32 = 16.0;
const TOKEN_SWATCH_RADIUS: f32 = 5.0;
const TOKEN_ROW: f32 = 30.0;
const KNOB_NAME: f32 = 96.0;
const SEG_FILL: f32 = 0.05;
const SEG_HEADER_FILL: f32 = 0.04;
const SEG_HEADER_LINE: f32 = 1.3;
const SEG_ON: f32 = 0.12;
const NOTE_LINE: f32 = 22.0;
const SMALL_LINE: f32 = 17.0;
const PREVIEW_HEIGHT: f32 = 180.0;
const AVATAR: f32 = 18.0;
const AVATAR_TEXT: f32 = 9.0;
const AVATAR_FILL: f32 = 0.16;
const JSON_INK: f32 = 0.75;
const JSON_LINE: f32 = 19.0;
const RESET_HEIGHT: f32 = 26.0;
const TAB_BADGE: f32 = 11.0;

#[derive(Clone, Copy, PartialEq, Eq)]
enum View {
    Tokens,
    Json,
}

struct ThemeScreen {
    palette: usize,
    knobs: [usize; 7],
    view: View,
    theme: Arc<Theme>,
    told: Option<SharedString>,
}

pub fn open(board: Option<&str>, _: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    board::load_fonts(cx)?;
    match board {
        None | Some("ITHEME-1") => {
            let knobs = [0; 7];
            let theme = Arc::new(styled(&PALETTES[0], &knobs)?);
            Ok(cx
                .new(|_| ThemeScreen {
                    palette: 0,
                    knobs,
                    view: View::Tokens,
                    theme,
                    told: None,
                })
                .into())
        }
        Some(other) => Err(format!("the theme screen draws ITHEME-1, not {other}")),
    }
}

fn styled(palette: &Palette, knobs: &[usize; 7]) -> Result<Theme, String> {
    let mut problems = Vec::new();
    let mut patch: Vec<String> = Vec::new();
    for (knob, at) in Knob::ALL.iter().zip(knobs) {
        let option = knob.options().get(*at).copied().unwrap_or("theme");
        patch.extend(
            knob.tokens(option)
                .iter()
                .map(|(path, value)| format!("\"{path}\": {value}")),
        );
    }
    if palette.file.is_none() && palette.key != "glass" {
        let colors = [
            ("surface.window", palette.window),
            ("cards.outer.fill", palette.shell),
            ("cards.inner.fill", palette.inner),
            ("cards.inner.fill_end", palette.inner),
            ("text.base", palette.text),
            ("text.strong", palette.text),
            ("text.muted", palette.muted),
            ("status.accent", palette.accent),
            ("status.live", palette.live),
            ("status.warn", palette.warn),
            ("status.danger", palette.danger),
        ];
        let unset: Vec<String> = colors
            .iter()
            .filter(|(path, _)| {
                !patch
                    .iter()
                    .any(|set| set.starts_with(&format!("\"{path}\"")))
            })
            .map(|(path, value)| format!("\"{path}\": \"{}\"", hex(css(value))))
            .collect();
        patch.extend(unset);
    }
    let mut texts = vec![("theme screen", format!("{{{}}}", patch.join(", ")))];
    texts.extend(palette.file.map(|file| (palette.key, file.to_owned())));
    texts.push(("tofu-glass.json", GLASS.to_owned()));
    let layers = texts
        .iter()
        .map(|(name, text)| parse_layer(name, text, &mut problems).map(|parsed| parsed.layer))
        .collect::<Result<Vec<_>, _>>()
        .map_err(|error| error.to_string())?;
    let theme = build(&layers, &mut problems).map_err(|error| error.to_string())?;
    match problems.first() {
        Some(problem) => Err(problem.to_string()),
        None => Ok(theme),
    }
}

fn css(value: &str) -> Rgba {
    let channel = |text: &str| text.trim().parse::<f32>().unwrap_or(0.0);
    if let Some(body) = value
        .strip_prefix("rgba(")
        .and_then(|rest| rest.strip_suffix(')'))
    {
        let parts: Vec<f32> = body.split(',').map(channel).collect();
        if let [r, g, b, a] = parts[..] {
            return Rgba::new(r / 255.0, g / 255.0, b / 255.0, a);
        }
    }
    let digits = value.trim_start_matches('#');
    let word = u32::from_str_radix(digits, 16).unwrap_or(0);
    gpui::rgba(if digits.len() == 6 {
        word << 8 | 0xff
    } else {
        word
    })
}

fn hex(color: Rgba) -> String {
    let byte = |share: f32| (share.clamp(0.0, 1.0) * 255.0).round() as u8;
    format!(
        "#{:02x}{:02x}{:02x}{:02x}",
        byte(color.color.red),
        byte(color.color.green),
        byte(color.color.blue),
        byte(color.alpha)
    )
}

fn swatch(color: &str, side: f32, radius: f32) -> Div {
    div()
        .flex_none()
        .size(px(side))
        .rounded(px(radius))
        .bg(css(color))
        .shadow(vec![ring(Rgba::new(1.0, 1.0, 1.0, SWATCH_RING))])
}

fn small(text: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .text_size(px(SMALL))
        .text_color(ink(theme, T3))
        .child(text.into())
}

fn header(left: impl IntoElement, right: impl IntoElement) -> Div {
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap_1p5()
        .h(px(SHELL_HEADER))
        .pl(px(9.0))
        .pr_1p5()
        .child(div().flex_1().flex().child(left))
        .child(right)
}

impl ThemeScreen {
    fn restyle(&mut self, cx: &mut Context<Self>) {
        match PALETTES
            .get(self.palette)
            .map(|palette| styled(palette, &self.knobs))
        {
            Some(Ok(theme)) => self.theme = Arc::new(theme),
            Some(Err(error)) => self.told = Some(error.into()),
            None => {}
        }
        cx.notify();
    }

    fn tell(
        message: &'static str,
        cx: &mut Context<Self>,
    ) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
        cx.listener(move |this, _: &ClickEvent, _, cx| {
            this.told = Some(message.into());
            cx.notify();
        })
    }

    fn themes(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let rows: Vec<_> = PALETTES
            .iter()
            .enumerate()
            .map(|(at, palette)| {
                let picked = at == self.palette;
                div()
                    .id(palette.key)
                    .flex()
                    .items_center()
                    .gap_3()
                    .py_2p5()
                    .px_3()
                    .rounded(px(ROW_RADIUS))
                    .cursor_pointer()
                    .relative()
                    .bg(tint(theme.color(ColorToken::Shadow), ROW_FILL))
                    .child(
                        div()
                            .absolute()
                            .inset_0()
                            .rounded(px(ROW_RADIUS))
                            .border(px(if picked { PICKED_RING_WIDTH } else { 1.0 }))
                            .border_color(ink(theme, if picked { PICKED_RING } else { RING })),
                    )
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.palette = at;
                        this.restyle(cx);
                    }))
                    .child(
                        div()
                            .flex()
                            .flex_wrap()
                            .w(px(SWATCH * 2.0 + SWATCH_GAP))
                            .gap(px(SWATCH_GAP))
                            .children(
                                [palette.shell, palette.inner, palette.accent, palette.live]
                                    .map(|color| swatch(color, SWATCH, SWATCH_RADIUS)),
                            ),
                    )
                    .child(
                        div()
                            .flex_1()
                            .line_height(px(ROW_LINE))
                            .child(div().text_size(px(NAME_TEXT)).child(palette.name))
                            .child(small(palette.mode, theme)),
                    )
            })
            .collect();
        outer_card(theme)
            .flex_col()
            .w(px(LIST_WIDTH))
            .flex_none()
            .child(header(caption("Themes", theme), small("5 built in", theme)))
            .child(
                inner_card(theme).p_2().gap_1p5().children(rows).child(
                    small(THEMES_NOTE, theme)
                        .py_2()
                        .px_1()
                        .line_height(px(SMALL_LINE)),
                ),
            )
    }

    fn knobs(&self, theme: &Theme, cx: &mut Context<Self>) -> Vec<AnyElement> {
        let row = |at: usize, knob: Knob, cx: &mut Context<Self>| {
            let chosen = self.knobs.get(at).copied().unwrap_or(0);
            div()
                .flex()
                .items_center()
                .gap_2p5()
                .min_h(px(TOKEN_ROW))
                .text_size(px(DESC))
                .child(
                    div()
                        .w(px(KNOB_NAME))
                        .flex_none()
                        .text_color(ink(theme, T2))
                        .child(knob.name()),
                )
                .child(
                    div()
                        .id(("knob", at))
                        .flex()
                        .flex_wrap()
                        .gap(px(2.0))
                        .p(px(2.0))
                        .rounded(px(8.0))
                        .bg(ink(theme, SEG_FILL))
                        .children(knob.options().iter().enumerate().map(|(index, option)| {
                            segment(option, index == chosen, theme)
                                .id((
                                    SharedString::from(format!("{}-{option}", knob.word(option))),
                                    at,
                                ))
                                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                                    if let Some(knob) = this.knobs.get_mut(at) {
                                        *knob = index;
                                        this.restyle(cx);
                                    }
                                }))
                        })),
                )
        };
        [("Outer card", 0..4), ("Inner card", 4..7)]
            .into_iter()
            .map(|(label, range)| {
                let rows: Vec<_> = range
                    .filter_map(|at| Knob::ALL.get(at).map(|knob| row(at, *knob, cx)))
                    .collect();
                div()
                    .flex()
                    .flex_col()
                    .gap_1p5()
                    .child(caption(label, theme))
                    .children(rows)
                    .into_any_element()
            })
            .collect()
    }

    fn preview(theme: &Theme) -> Div {
        let workers = WORKERS.iter().map(|worker| {
            let color = css(worker.color);
            div()
                .flex()
                .items_center()
                .gap_2p5()
                .child(
                    div()
                        .flex()
                        .flex_none()
                        .items_center()
                        .justify_center()
                        .size(px(AVATAR))
                        .rounded_full()
                        .bg(tint(color, AVATAR_FILL))
                        .text_color(color)
                        .text_size(px(AVATAR_TEXT))
                        .font_weight(FontWeight::SEMIBOLD)
                        .when(!worker.waiting, |avatar| {
                            avatar.shadow(vec![desk_ui::components::paint::halo(color, 1.0)])
                        })
                        .child(worker.letter),
                )
                .child(div().text_color(color).child(worker.name))
                .child(div().flex_1().text_color(ink(theme, T2)).child(worker.task))
                .child(small(worker.when, theme))
        });
        let dots = [
            (ColorToken::StatusLive, "live"),
            (ColorToken::StatusWarn, "warn"),
            (ColorToken::StatusDanger, "danger"),
            (ColorToken::StatusAccent, "accent"),
        ];
        outer_card(theme)
            .flex_col()
            .flex_none()
            .h(px(PREVIEW_HEIGHT))
            .child(
                div()
                    .flex()
                    .flex_none()
                    .items_center()
                    .gap(px(2.0))
                    .h(px(36.0))
                    .pl(px(4.0))
                    .text_size(px(DESC))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap_1p5()
                            .h(px(30.0))
                            .px_3()
                            .rounded(px(9.0))
                            .bg(theme.color(ColorToken::TabsFill))
                            .child("Sub-agents")
                            .child(
                                div()
                                    .px_1p5()
                                    .rounded_full()
                                    .bg(ink(theme, SEG_ON))
                                    .text_size(px(TAB_BADGE))
                                    .line_height(relative(1.4))
                                    .child("18 live"),
                            ),
                    )
                    .child(div().px_3().text_color(ink(theme, T2)).child("File edits")),
            )
            .child(
                inner_card(theme)
                    .overflow_hidden()
                    .py_3()
                    .px(px(14.0))
                    .gap_2()
                    .text_size(px(DESC))
                    .children(workers)
                    .child(
                        div()
                            .flex()
                            .gap_2()
                            .mt_1()
                            .children(dots.map(|(token, label)| {
                                chip(label, None, theme)
                                    .flex_row_reverse()
                                    .justify_end()
                                    .child(div().text_color(theme.color(token)).child("\u{25cf}"))
                            })),
                    ),
            )
    }

    fn middle(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let palette = PALETTES.get(self.palette).unwrap_or(&PALETTES[0]);
        let knobs = self.knobs(theme, cx);
        outer_card(theme)
            .flex_col()
            .flex_1()
            .child(header(caption(palette.name, theme), small("applied to this window as you pick", theme)))
            .child(
                inner_card(theme)
                    .py(px(18.0))
                    .px_5()
                    .gap_4()
                    .child(
                        div()
                            .text_size(px(BODY_TEXT))
                            .line_height(px(NOTE_LINE))
                            .text_color(ink(theme, T2))
                            .child(palette.note),
                    )
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .gap(px(14.0))
                            .children(knobs)
                            .child(
                                div()
                                    .flex()
                                    .items_center()
                                    .gap_2p5()
                                    .child(
                                        small("changes apply to every tile in this window; theme keeps what the theme says", theme)
                                            .flex_1()
                                            .min_w_0()
                                            .line_height(px(NOTE_LINE)),
                                    )
                                    .child(
                                        button("reset-cards", "Back to the theme", None, ButtonKind::Plain, theme)
                                            .h(px(RESET_HEIGHT))
                                            .text_size(px(SMALL))
                                            .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                                this.knobs = [0; 7];
                                                this.restyle(cx);
                                            })),
                                    ),
                            ),
                    )
                    .child(caption("Preview", theme))
                    .child(Self::preview(theme)),
            )
    }

    fn tokens(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let palette = PALETTES.get(self.palette).unwrap_or(&PALETTES[0]);
        let inner = if palette.double {
            palette.inner
        } else {
            palette.shell
        };
        let groups: [(&str, Vec<(&str, &str)>); 5] = [
            (
                "surface",
                vec![
                    ("window", palette.window),
                    ("shell", palette.shell),
                    ("inner", inner),
                ],
            ),
            (
                "text",
                vec![("text", palette.text), ("muted", palette.muted)],
            ),
            (
                "status",
                vec![
                    ("accent", palette.accent),
                    ("live", palette.live),
                    ("warn", palette.warn),
                    ("danger", palette.danger),
                ],
            ),
            ("git", GIT.to_vec()),
            ("agents", AGENTS.to_vec()),
        ];
        let view_switch = |view: View, label: &'static str, cx: &mut Context<Self>| {
            segment(label, self.view == view, theme)
                .px(px(10.0))
                .line_height(relative(SEG_HEADER_LINE))
                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    this.view = view;
                    cx.notify();
                }))
        };
        let switch = div()
            .flex()
            .gap(px(2.0))
            .p(px(2.0))
            .rounded(px(8.0))
            .bg(ink(theme, SEG_HEADER_FILL))
            .child(view_switch(View::Tokens, "Tokens", cx))
            .child(view_switch(View::Json, "JSON", cx));
        let body: Div = match self.view {
            View::Tokens => {
                div()
                    .py_1p5()
                    .px_1()
                    .children(groups.into_iter().flat_map(|(label, items)| {
                        let rows: Vec<AnyElement> = items
                            .into_iter()
                            .map(|(name, value)| {
                                div()
                                    .id(SharedString::from(format!("token-{label}-{name}")))
                                    .flex()
                                    .flex_none()
                                    .items_center()
                                    .gap_2p5()
                                    .h(px(TOKEN_ROW))
                                    .px_3()
                                    .rounded(px(8.0))
                                    .text_size(px(DESC))
                                    .cursor_pointer()
                                    .on_click(Self::tell(TOKEN_TELL, cx))
                                    .child(swatch(value, TOKEN_SWATCH, TOKEN_SWATCH_RADIUS))
                                    .child(div().flex_1().child(name))
                                    .child(
                                        small(value, theme)
                                            .font_family(mono(theme))
                                            .text_size(px(MONO_SMALL)),
                                    )
                                    .into_any_element()
                            })
                            .collect();
                        std::iter::once(
                            caption(label, theme)
                                .pt_3()
                                .px_3()
                                .pb_1p5()
                                .into_any_element(),
                        )
                        .chain(rows)
                    }))
            }
            View::Json => div()
                .py(px(14.0))
                .px_4()
                .font_family(mono(theme))
                .text_size(px(SMALL))
                .line_height(px(JSON_LINE))
                .text_color(ink(theme, JSON_INK))
                .children(json(palette, &self.knobs).lines().map(|line| {
                    div()
                        .whitespace_nowrap()
                        .child(SharedString::from(line.to_owned()))
                })),
        };
        outer_card(theme)
            .flex_col()
            .w(px(TOKENS_WIDTH))
            .flex_none()
            .child(header(
                switch,
                small("~/.tofu/themes/", theme)
                    .font_family(mono(theme))
                    .text_size(px(MONO_SMALL)),
            ))
            .child(inner_card(theme).child(body))
    }
}

fn json(palette: &Palette, knobs: &[usize; 7]) -> String {
    let word = |at: usize| {
        Knob::ALL
            .get(at)
            .and_then(|knob| {
                knob.options()
                    .get(knobs.get(at).copied().unwrap_or(0))
                    .map(|option| knob.word(option))
            })
            .unwrap_or("theme")
    };
    let inner = if palette.double {
        palette.inner
    } else {
        "@surface.shell"
    };
    format!(
        "{{\n  \"name\": \"{name}\",\n  \"extends\": \"tofu-glass\",\n  \"mode\": \"{mode}\",\n  \"surface\": {{\n    \"window\": \"{window}\",\n    \"shell\": \"{shell}\",\n    \"inner\": \"{inner}\",\n    \"doubleSurface\": {double}\n  }},\n  \"cards\": {{\n    \"outer\": {{ \"fill\": \"{o0}\", \"border\": \"{o1}\", \"radius\": \"{o2}\", \"gap\": \"{o3}\" }},\n    \"inner\": {{ \"fill\": \"{i0}\", \"border\": \"{i1}\", \"radius\": \"{i2}\" }}\n  }},\n  \"text\": {{ \"base\": \"{text}\", \"muted\": \"{muted}\" }},\n  \"status\": {{ \"accent\": \"{accent}\", \"live\": \"{live}\", \"warn\": \"{warn}\", \"danger\": \"{danger}\" }},\n  \"shape\": {{ \"radius\": {radius}, \"blur\": {blur}, \"density\": \"comfortable\" }},\n  \"agents\": {{ \"go-dev\": \"#79c0ff\", \"ts-dev\": \"#b9a6ea\", \"explore\": \"#8fd0aa\" }}\n}}",
        name = palette.name,
        mode = palette.mode,
        window = palette.window,
        shell = palette.shell,
        double = palette.double,
        o0 = word(0),
        o1 = word(1),
        o2 = word(2),
        o3 = word(3),
        i0 = word(4),
        i1 = word(5),
        i2 = word(6),
        text = palette.text,
        muted = palette.muted,
        accent = palette.accent,
        live = palette.live,
        warn = palette.warn,
        danger = palette.danger,
        radius = palette.radius,
        blur = palette.blur,
    )
}

impl Render for ThemeScreen {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Arc::clone(&self.theme);
        let theme = theme.as_ref();
        let title = div()
            .flex()
            .flex_none()
            .items_center()
            .gap_2p5()
            .px_1()
            .child(
                div()
                    .text_size(px(TITLE))
                    .line_height(relative(1.0))
                    .font_weight(FontWeight::SEMIBOLD)
                    .child("Theme"),
            )
            .child(
                small("every color, surface and shape is a token in a JSON file; pick one and the whole window follows", theme)
                    .text_size(px(DESC)),
            )
            .child(div().flex_1())
            .child(button("duplicate", "Duplicate as JSON", None, ButtonKind::Plain, theme).on_click(Self::tell(DUPLICATE_TELL, cx)))
            .child(button("import", "Import", None, ButtonKind::Plain, theme).on_click(Self::tell(IMPORT_TELL, cx)));
        let body = div()
            .flex_1()
            .min_h_0()
            .flex()
            .flex_col()
            .gap_2()
            .mb_2()
            .pt_1()
            .px_1()
            .child(title)
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .gap_2()
                    .child(self.themes(theme, cx))
                    .child(self.middle(theme, cx))
                    .child(self.tokens(theme, cx)),
            );
        board::root(
            theme,
            body,
            self.told.clone(),
            cx.listener(|this, _: &ClickEvent, _, cx| {
                this.told = None;
                cx.notify();
            }),
        )
    }
}
