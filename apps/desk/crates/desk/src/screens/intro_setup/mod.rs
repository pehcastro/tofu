mod colours;
mod tune;

use std::path::Path;
use std::sync::Arc;

use desk_core::control::TELL_BADGE;
use desk_ui::components::card::{caption, inner_card, shell};
use desk_ui::components::form::switch;
use desk_ui::components::glyph::Glyph;
use desk_ui::components::logo::{Brand, logo};
use desk_ui::components::overlay::toast;
use desk_ui::components::paint::glyph;
use desk_ui::components::paint::{halo, ink, ring, tint};
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::size::{RADIUS_ROW, T1, T2, T3};
use desk_ui::components::skeleton::skeleton_block;
use desk_ui::effects::ascii::{AsciiChanged, AsciiPanel, Knob};
use desk_ui::effects::controls::Settle;
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::ICON_SMALL;
use desk_ui::theme::{ColorToken, Theme, WordToken};
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Div, Entity, Image, ImageFormat, ImageSource,
    ObjectFit, PathPromptOptions, Render, Rgba, ScrollHandle, SharedString, StyleRefinement,
    StyledImage, Subscription, WeakEntity, Window, div, img, prelude::*, px, relative,
};

use crate::screens::frame;
use crate::screens::intro::effects::{
    BUILT_IN, Backdrop, after_ascii, backdrop, remembered_params,
};
use crate::screens::intro::kept::{self, Fit};
use crate::screens::intro::remembered::{self, Effect, Remembered};
use crate::screens::intro::tuning::Tuning;
use crate::screens::settings::stored::Stored;
use tune::{Card, TunePanel, Tuned};

const HIDE_RESUME: &str = "intro.hide_resume";
const SIDE_WIDTH: f32 = 360.0;
const CARD_PAD: f32 = 12.0;
const CARD_GAP: f32 = 8.0;
const SMALL: f32 = 12.0;
const THUMB_WIDTH: f32 = 92.0;
const THUMB_HEIGHT: f32 = 56.0;
const THUMB_RADIUS: f32 = 9.0;
const THUMB_RING: f32 = 0.1;
const ADD_RING: f32 = 0.14;
const PICKED_GAP: f32 = 2.0;
const PICKED_RING: f32 = 3.5;
const ROW_LINE: f32 = 18.0;
const PREVIEW_RADIUS: f32 = 12.0;
const COMPOSER_LEFT: f32 = 0.19;
const COMPOSER_TOP: f32 = 0.46;
const COMPOSER_WIDTH: f32 = 0.62;
const COMPOSER_RADIUS: f32 = 15.0;
const COMPOSER_FILL_SHARE: f32 = 0.55;
const COMPOSER_HINT: f32 = 12.5;
const COMPOSER_META: f32 = 11.5;
const LOGO: f32 = 13.0;
const SEND: f32 = 22.0;
const PICKED_FILL: f32 = 0.07;
const SWATCH: f32 = 14.0;
const HEX_WIDTH: f32 = 76.0;
const COG: f32 = 22.0;
const MOTION: &str = "Slow motion";

fn effect_name(effect: Effect) -> &'static str {
    match effect {
        Effect::Sort => "Pixel sort",
        Effect::Ascii => "ASCII",
        Effect::Crt => "CRT",
        Effect::Dither => "Dithered fade",
        Effect::Fade => "Soft fade",
        Effect::Duotone => "Duotone",
        Effect::Halftone => "Halftone",
        Effect::Grain => "Film grain",
        Effect::Scan => "Scanlines",
        Effect::Glow => "Glow",
    }
}

fn effect_sub(effect: Effect) -> &'static str {
    match effect {
        Effect::Sort => "bright stretches of each row sorted dark to light",
        Effect::Ascii => "moving glyphs over a light field",
        Effect::Crt => "a curved tube with a phosphor mask and soft bloom",
        Effect::Dither => "the image dissolves into the window through ordered dots",
        Effect::Fade => "a plain gradient into the window color",
        Effect::Duotone => "grey, mapped between two theme colors",
        Effect::Halftone => "a dot screen rising from the middle",
        Effect::Grain => "fine noise over everything",
        Effect::Scan => "thin lines, strongest at the top",
        Effect::Glow => "a soft light rising from the bottom",
    }
}

struct Picture {
    name: String,
    shown: ImageSource,
}

struct Side {
    setup: WeakEntity<IntroSetup>,
    _redrawn: Subscription,
}

impl Side {
    fn new(cx: &mut Context<IntroSetup>) -> Entity<Side> {
        let setup = cx.entity();
        cx.new(|cx| Side {
            setup: setup.downgrade(),
            _redrawn: cx.observe(&setup, |_, _, cx| cx.notify()),
        })
    }
}

impl Render for Side {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        desk_perf::span!("intro-setup-side");
        self.setup
            .update(cx, |setup, cx| setup.side(cx))
            .unwrap_or_else(|_| div())
    }
}

struct IntroSetup {
    side: Entity<Side>,
    pictures: Vec<Picture>,
    adding: bool,
    backdrop: Entity<Backdrop>,
    fit: Fit,
    image: String,
    effects: Vec<Effect>,
    motion: bool,
    resume: bool,
    stored: Result<Stored, String>,
    kept: Result<Remembered, String>,
    told: Option<SharedString>,
    ascii: Entity<AsciiPanel>,
    tuning: Tuning,
    tunes: Vec<Entity<TunePanel>>,
    open: Vec<Card>,
    scrolls: [ScrollHandle; 2],
    colours_open: bool,
    editing: Option<colours::Editing>,
    _tuned: Subscription,
    _retuned: Vec<Subscription>,
}

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    if let Some(other) = board {
        return Err(format!(
            "the intro setup screen draws no board, not {other}"
        ));
    }
    let stored = Stored::load();
    let kept = remembered::read();
    if let Err(error) = &kept {
        eprintln!("desk: intro setup: {error}");
    }
    let start = kept.clone().unwrap_or_default();
    let pictures: Vec<Picture> = BUILT_IN
        .into_iter()
        .map(|source| Picture {
            name: source.name.to_owned(),
            shown: Arc::new(Image::from_bytes(ImageFormat::Jpeg, source.bytes.to_vec())).into(),
        })
        .collect();
    let fit = Fit::largest(window, cx);
    let image = start.image.unwrap_or_else(|| BUILT_IN[0].name.to_owned());
    let params = remembered_params(&start.effects, &start.tuning);
    let backdrop = backdrop(BUILT_IN[0], params.clone(), cx);
    backdrop.update(cx, |backdrop, cx| {
        backdrop.show_named(&image, params, fit, cx)
    });
    let shown = start
        .effects
        .contains(&Effect::Ascii)
        .then(|| start.ascii.clone());
    let after = after_ascii(&start.effects, &start.tuning);
    let speed = Knob::Speed.dial(&start.ascii);
    backdrop.update(cx, |backdrop, cx| {
        backdrop.round(PREVIEW_RADIUS);
        backdrop.ascii(shown, &after, start.motion, cx);
    });
    Ok(cx
        .new(|cx| {
            let ascii = cx.new(|cx| AsciiPanel::new(start.ascii, window, cx));
            let tuned = cx.subscribe(&ascii, |setup: &mut IntroSetup, _, changed, cx| {
                setup.tuned(changed, cx)
            });
            let tunes: Vec<_> = Effect::ALL
                .into_iter()
                .filter(|effect| *effect != Effect::Ascii)
                .map(Card::Effect)
                .chain([Card::Motion])
                .map(|card| cx.new(|_| TunePanel::new(start.tuning.clone(), card, speed)))
                .collect();
            let retuned = tunes
                .iter()
                .map(|tune| {
                    cx.subscribe(tune, |setup: &mut IntroSetup, _, tuned, cx| {
                        setup.retuned(tuned, cx)
                    })
                })
                .collect();
            let setup = IntroSetup {
                side: Side::new(cx),
                pictures,
                adding: false,
                backdrop,
                fit,
                image,
                effects: start.effects,
                motion: start.motion,
                resume: !stored
                    .as_ref()
                    .is_ok_and(|stored| stored.switch(HIDE_RESUME)),
                stored,
                kept,
                told: None,
                ascii,
                tuning: start.tuning,
                tunes,
                open: Vec::new(),
                scrolls: [ScrollHandle::new(), ScrollHandle::new()],
                colours_open: false,
                editing: None,
                _tuned: tuned,
                _retuned: retuned,
            };
            setup.gather(cx);
            setup
        })
        .into())
}

impl IntroSetup {
    fn on(&self, effect: Effect) -> bool {
        self.effects.contains(&effect)
    }

    fn fold(&mut self, card: Card, cx: &mut Context<Self>) {
        match self.open.iter().position(|open| *open == card) {
            Some(at) => {
                self.open.remove(at);
            }
            None => self.open.push(card),
        }
        cx.notify();
    }

    fn section(&self, card: Card, cx: &App) -> Option<AnyView> {
        if !self.open.contains(&card) {
            return None;
        }
        match card {
            Card::Effect(Effect::Ascii) => Some(self.ascii.clone().into()),
            Card::Effect(_) | Card::Motion => self
                .tunes
                .iter()
                .find(|tune| tune.read(cx).card() == card)
                .map(|tune| tune.clone().into()),
        }
    }

    fn tuned(&mut self, changed: &AsciiChanged, cx: &mut Context<Self>) {
        let speed = Knob::Speed.dial(self.ascii.read(cx).ascii());
        for tune in &self.tunes {
            tune.update(cx, |tune, cx| tune.speed(speed, cx));
        }
        match changed {
            AsciiChanged::Moving(_) => self.show_ascii(cx),
            AsciiChanged::Settled(_) => self.save(cx),
        }
    }

    fn retuned(&mut self, tuned: &Tuned, cx: &mut Context<Self>) {
        match *tuned {
            Tuned::Knob(effect, knob, value, settle) => {
                self.tuning.set_knob(effect, knob, value);
                self.settled(settle, cx);
            }
            Tuned::Hue(hue, color, settle) => {
                self.tuning.set_hue(hue, color);
                self.settled(settle, cx);
            }
            Tuned::Speed(value, settle) => self
                .ascii
                .update(cx, |ascii, cx| ascii.turned(Knob::Speed, value, settle, cx)),
        }
    }

    fn settled(&mut self, settle: Settle, cx: &mut Context<Self>) {
        match settle {
            Settle::Moving => self.redraw(cx),
            Settle::Settled => self.save(cx),
        }
    }

    fn show_ascii(&mut self, cx: &mut Context<Self>) {
        let shown = self
            .on(Effect::Ascii)
            .then(|| self.ascii.read(cx).ascii().clone());
        let after = after_ascii(&self.effects, &self.tuning);
        let motion = self.motion;
        self.backdrop
            .update(cx, |backdrop, cx| backdrop.ascii(shown, &after, motion, cx));
    }

    fn redraw(&mut self, cx: &mut Context<Self>) {
        self.show_ascii(cx);
        let params = remembered_params(&self.effects, &self.tuning);
        let (image, fit) = (self.image.clone(), self.fit);
        self.backdrop.update(cx, |backdrop, cx| {
            backdrop.show_named(&image, params, fit, cx)
        });
    }

    fn gather(&self, cx: &mut Context<Self>) {
        let fit = self.fit;
        cx.spawn(async move |setup, cx| {
            let gathered = cx
                .background_executor()
                .spawn(async move { kept::gather(fit) })
                .await;
            let landed = setup.update(cx, |setup, cx| {
                match gathered {
                    Ok(names) => {
                        for name in names {
                            setup.add_picture(name);
                        }
                    }
                    Err(error) => eprintln!("desk: intro setup lists no kept images: {error}"),
                }
                cx.notify();
            });
            if landed.is_err() {
                eprintln!("desk: intro setup closed before its images were listed");
            }
        })
        .detach();
    }

    fn add_picture(&mut self, name: String) {
        match remembered::kept_images() {
            Ok(folder) => {
                self.pictures.retain(|known| known.name != name);
                self.pictures.push(Picture {
                    shown: folder.join(kept::thumb(&name)).into(),
                    name,
                });
            }
            Err(error) => eprintln!("desk: intro setup shows no {name}: {error}"),
        }
    }

    fn save(&mut self, cx: &mut Context<Self>) {
        self.redraw(cx);
        let (image, effects, motion) = (self.image.clone(), self.effects.clone(), self.motion);
        let ascii = self.ascii.read(cx).ascii().clone();
        let tuning = self.tuning.clone();
        cx.background_executor()
            .spawn(async move {
                let saved = remembered::update(|kept| {
                    kept.image = Some(image);
                    kept.effects = effects;
                    kept.motion = motion;
                    kept.ascii = ascii;
                    kept.tuning = tuning;
                });
                if let Err(error) = saved {
                    eprintln!("desk: intro setup was not saved: {error}");
                }
            })
            .detach();
    }

    fn pick_image(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let asked = cx.prompt_for_paths(PathPromptOptions {
            files: true,
            directories: false,
            multiple: false,
            prompt: Some("Add an intro image".into()),
        });
        cx.spawn_in(window, async move |setup, cx| {
            let source = match asked.await {
                Ok(Ok(Some(mut picked))) => picked.pop(),
                Ok(Ok(None)) => None,
                Ok(Err(error)) => {
                    eprintln!("desk: intro setup: the file dialog failed: {error}");
                    None
                }
                Err(_) => {
                    eprintln!("desk: intro setup: the file dialog closed without an answer");
                    None
                }
            };
            let Some(source) = source else {
                return eprintln!("desk: intro setup: no image was picked");
            };
            let Ok(fit) = setup.update(cx, |setup, cx| {
                setup.adding = true;
                cx.notify();
                setup.fit
            }) else {
                return eprintln!("desk: intro setup closed before the image was kept");
            };
            let copied = cx
                .background_executor()
                .spawn(async move { copy_in(&source, fit) })
                .await;
            let landed = setup.update(cx, |setup, cx| {
                setup.adding = false;
                match copied {
                    Ok(name) => {
                        eprintln!("desk: intro setup added {name}");
                        setup.image = name.clone();
                        setup.add_picture(name);
                        setup.save(cx);
                    }
                    Err(error) => setup.told = Some(error.into()),
                }
                cx.notify();
            });
            if landed.is_err() {
                eprintln!("desk: intro setup closed before the image landed");
            }
        })
        .detach();
    }

    fn images(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let thumbs: Vec<_> = self
            .pictures
            .iter()
            .enumerate()
            .map(|(at, shown)| {
                let picked = shown.name == self.image;
                let name = shown.name.clone();
                div()
                    .id(("image", at))
                    .flex_none()
                    .w(px(THUMB_WIDTH))
                    .h(px(THUMB_HEIGHT))
                    .rounded(px(THUMB_RADIUS))
                    .cursor_pointer()
                    .shadow(if picked {
                        vec![
                            halo(theme.color(ColorToken::AvatarRing), PICKED_GAP),
                            halo(theme.color(ColorToken::TextStrong), PICKED_RING),
                        ]
                    } else {
                        vec![ring(ink(theme, THUMB_RING))]
                    })
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.image = name.clone();
                        this.save(cx);
                        cx.notify();
                    }))
                    .child(picture(shown).size_full().rounded(px(THUMB_RADIUS)))
            })
            .collect();
        let add = div()
            .id("add-image")
            .flex_none()
            .flex()
            .items_center()
            .justify_center()
            .size(px(THUMB_HEIGHT))
            .rounded(px(THUMB_RADIUS))
            .shadow(vec![ring(ink(theme, ADD_RING))])
            .text_color(ink(theme, T2))
            .cursor_pointer()
            .on_click(cx.listener(|this, _: &ClickEvent, window, cx| this.pick_image(window, cx)))
            .child("+");
        div()
            .flex()
            .flex_col()
            .gap_2()
            .child(caption("Images", theme))
            .child(
                div()
                    .flex()
                    .flex_wrap()
                    .gap_2()
                    .children(thumbs)
                    .when(self.adding, |row| {
                        row.child(
                            div()
                                .flex_none()
                                .w(px(THUMB_WIDTH))
                                .child(skeleton_block(THUMB_HEIGHT, theme)),
                        )
                    })
                    .child(add),
            )
    }

    fn effects(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let mut list = div()
            .flex()
            .flex_col()
            .gap(px(2.0))
            .child(caption("Effects, applied in order", theme).pb_1p5());
        for (at, effect) in Effect::ALL.into_iter().enumerate() {
            let card = Card::Effect(effect);
            list = list
                .child(switch_row(
                    ("effect", at),
                    effect_name(effect),
                    effect_sub(effect),
                    self.on(effect),
                    Some(self.cog(card, theme, cx)),
                    theme,
                    act(cx, move |this, cx| this.flip(effect, cx)),
                ))
                .children(self.section(card, cx));
        }
        list.child(switch_row(
            "motion",
            MOTION,
            "effects drift; off when the system asks for less motion",
            self.motion,
            Some(self.cog(Card::Motion, theme, cx)),
            theme,
            act(cx, |this, cx| {
                this.motion = !this.motion;
                this.save(cx);
                cx.notify();
            }),
        ))
        .children(self.section(Card::Motion, cx))
        .child(switch_row(
            "resume",
            "Resume button",
            "under the composer, with the project's sessions",
            self.resume,
            None,
            theme,
            act(cx, |this, cx| this.flip_resume(cx)),
        ))
        .child(self.colours(theme, cx))
    }

    fn ink(&self, theme: &Theme, token: ColorToken) -> Rgba {
        self.editing
            .as_ref()
            .filter(|editing| editing.token == token)
            .and_then(|editing| editing.live)
            .unwrap_or_else(|| theme.color(token))
    }

    fn colours(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let open = self.colours_open;
        let header = div()
            .id("intro-colours")
            .flex()
            .items_center()
            .gap_2()
            .px_1()
            .py(px(7.0))
            .cursor_pointer()
            .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                this.colours_open = !this.colours_open;
                this.editing = None;
                cx.notify();
            }))
            .child(
                div()
                    .flex_1()
                    .line_height(px(ROW_LINE))
                    .child("Intro colours"),
            )
            .child(glyph(
                Glyph::Chevron,
                ICON_SMALL,
                ink(theme, if open { T1 } else { T3 }),
            ));
        let rows = colours::TOKENS
            .into_iter()
            .enumerate()
            .filter(|_| open)
            .map(|(at, entry)| self.colour_row(at, entry, theme, cx));
        div().flex().flex_col().child(header).children(rows)
    }

    fn pick_colour(&mut self, token: ColorToken, window: &mut Window, cx: &mut Context<Self>) {
        let again = self
            .editing
            .as_ref()
            .is_some_and(|editing| editing.token == token);
        self.editing = None;
        if !again {
            let color = ActiveTheme::theme(cx).color(token);
            self.editing = Some(colours::Editing::open(token, color, window, cx));
        }
        cx.notify();
    }

    fn cog(&self, card: Card, theme: &Theme, cx: &mut Context<Self>) -> gpui::Stateful<Div> {
        let open = self.open.contains(&card);
        div()
            .id(SharedString::from(format!("cog-{card:?}")))
            .flex()
            .flex_none()
            .items_center()
            .justify_center()
            .size(px(COG))
            .rounded(px(RADIUS_ROW))
            .cursor_pointer()
            .when(open, |cog| cog.bg(ink(theme, PICKED_FILL)))
            .child(glyph(
                Glyph::Settings,
                ICON_SMALL,
                ink(theme, if open { T1 } else { T3 }),
            ))
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                cx.stop_propagation();
                this.fold(card, cx)
            }))
    }

    fn flip(&mut self, effect: Effect, cx: &mut Context<Self>) {
        let on = !self.on(effect);
        self.effects = Effect::ALL
            .into_iter()
            .filter(|known| {
                if *known == effect {
                    on
                } else {
                    self.on(*known)
                }
            })
            .collect();
        self.save(cx);
        cx.notify();
    }

    fn flip_resume(&mut self, cx: &mut Context<Self>) {
        let resume = !self.resume;
        let saved = self
            .stored
            .as_mut()
            .map_err(|error| error.clone())
            .and_then(|stored| stored.set_switch(HIDE_RESUME, !resume));
        match saved {
            Ok(()) => {
                eprintln!("desk: intro setup Resume button {resume}");
                self.resume = resume;
            }
            Err(error) => {
                self.told = Some(format!("The Resume button was not saved: {error}").into())
            }
        }
        cx.notify();
    }

    fn composer(&self, theme: &Theme) -> Div {
        let kept = self.kept.as_ref().ok();
        let project = kept
            .and_then(|kept| kept.project.as_ref())
            .and_then(|project| project.file_name())
            .map(|name| name.to_string_lossy().into_owned());
        let hint = match (&self.kept, project) {
            (Err(error), _) => format!("intro.json could not be read: {error}"),
            (Ok(_), Some(project)) => format!("Do anything in {project}"),
            (Ok(_), None) => "No project opened from the intro yet".to_owned(),
        };
        let model = kept.and_then(|kept| kept.model.clone());
        let brand = model
            .as_deref()
            .and_then(|model| model.split_once('/'))
            .and_then(|(source, _)| Brand::for_source(source));
        let resume = kept
            .filter(|kept| kept.session.is_some() && self.resume)
            .map(|kept| match &kept.session_name {
                Some(name) => format!("Resume \u{b7} {name}"),
                None => "Resume".to_owned(),
            });
        let composer = self.ink(theme, ColorToken::IntroComposerFill);
        let fill = Rgba {
            alpha: composer.alpha * COMPOSER_FILL_SHARE,
            ..composer
        };
        div()
            .absolute()
            .left(relative(COMPOSER_LEFT))
            .top(relative(COMPOSER_TOP))
            .w(relative(COMPOSER_WIDTH))
            .flex()
            .flex_col()
            .gap_2()
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(14.0))
                    .py(px(11.0))
                    .px(px(13.0))
                    .rounded(px(COMPOSER_RADIUS))
                    .bg(fill)
                    .shadow(vec![ring(self.ink(theme, ColorToken::IntroComposerBorder))])
                    .child(
                        div()
                            .text_size(px(COMPOSER_HINT))
                            .text_color(self.ink(theme, ColorToken::IntroPlaceholder))
                            .child(hint),
                    )
                    .child(
                        div()
                            .flex()
                            .justify_end()
                            .items_center()
                            .gap_2()
                            .text_size(px(COMPOSER_META))
                            .text_color(self.ink(theme, ColorToken::IntroModelText))
                            .children(brand.map(|brand| logo(brand, px(LOGO), theme)))
                            .child(model.unwrap_or_else(|| "no model picked yet".to_owned()))
                            .child(
                                div()
                                    .flex()
                                    .items_center()
                                    .justify_center()
                                    .size(px(SEND))
                                    .rounded_full()
                                    .bg(self.ink(theme, ColorToken::IntroSendFill))
                                    .child(glyph(
                                        Glyph::Send,
                                        ICON_SMALL,
                                        self.ink(theme, ColorToken::IntroSendIcon),
                                    )),
                            ),
                    ),
            )
            .when_some(resume, |composer, resume| {
                composer.child(
                    div()
                        .flex()
                        .justify_end()
                        .text_size(px(COMPOSER_META))
                        .text_color(self.ink(theme, ColorToken::IntroLink))
                        .child(resume),
                )
            })
    }

    fn side(&self, cx: &mut Context<Self>) -> Div {
        let theme = ActiveTheme::theme(cx);
        div()
            .size_full()
            .flex()
            .flex_col()
            .gap(px(CARD_GAP))
            .child(
                area(
                    "intro-setup-images",
                    &self.scrolls[0],
                    &theme,
                    self.images(&theme, cx),
                )
                .flex_none(),
            )
            .child(
                area(
                    "intro-setup-effects",
                    &self.scrolls[1],
                    &theme,
                    self.effects(&theme, cx),
                )
                .flex_1(),
            )
    }

    fn preview(&self, theme: &Theme) -> Div {
        div()
            .flex_1()
            .relative()
            .rounded(px(PREVIEW_RADIUS))
            .overflow_hidden()
            .bg(tint(theme.color(ColorToken::SurfaceWindow), 1.0))
            .child(self.backdrop.clone())
            .child(self.composer(theme))
    }
}

fn picture(shown: &Picture) -> gpui::Img {
    img(shown.shown.clone()).object_fit(ObjectFit::Cover)
}

fn copy_in(source: &Path, fit: Fit) -> Result<String, String> {
    if !kept::pickable(source) {
        return Err(format!(
            "{} is not a png, jpg or webp image",
            source.display()
        ));
    }
    kept::keep(source, fit)
}

fn switch_row(
    id: impl Into<gpui::ElementId>,
    name: &'static str,
    sub: &'static str,
    on: bool,
    cog: Option<gpui::Stateful<Div>>,
    theme: &Theme,
    flip: Flip,
) -> gpui::Stateful<Div> {
    let keyed = flip.clone();
    div()
        .id(id)
        .flex()
        .items_center()
        .gap_2p5()
        .px_1()
        .py(px(7.0))
        .child(
            div()
                .flex_1()
                .min_w_0()
                .flex()
                .flex_col()
                .line_height(px(ROW_LINE))
                .child(name)
                .child(
                    div()
                        .text_size(px(SMALL))
                        .text_color(ink(theme, T3))
                        .child(sub),
                ),
        )
        .children(cog)
        .child(
            switch("switch", "", on, theme)
                .on_click(move |_, window, cx| flip(window, cx))
                .on_key_down(move |event, window, cx| {
                    if matches!(event.keystroke.key.as_str(), "space" | "enter") {
                        cx.stop_propagation();
                        keyed(window, cx);
                    }
                }),
        )
}

type Flip = std::rc::Rc<dyn Fn(&mut Window, &mut App)>;

fn act(
    cx: &Context<IntroSetup>,
    change: impl Fn(&mut IntroSetup, &mut Context<IntroSetup>) + 'static,
) -> Flip {
    let setup = cx.weak_entity();
    std::rc::Rc::new(move |_, cx| {
        if let Err(error) = setup.update(cx, |this, cx| change(this, cx)) {
            eprintln!("desk: intro setup closed under a switch: {error}");
        }
    })
}

fn area(id: &'static str, scroll: &ScrollHandle, theme: &Theme, body: Div) -> Div {
    inner_card(theme).child(
        ScrollArea::new(id)
            .track(scroll)
            .child(body.p(px(CARD_PAD))),
    )
}

impl Render for IntroSetup {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
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
        let side = self.side.clone().cached(
            StyleRefinement::default()
                .flex_none()
                .w(px(SIDE_WIDTH))
                .min_h_0(),
        );
        let preview = inner_card(&theme)
            .flex_1()
            .min_w_0()
            .p(px(CARD_PAD))
            .flex()
            .flex_col()
            .gap_2()
            .child(caption("Preview", &theme))
            .child(self.preview(&theme));
        let preview = div()
            .flex_1()
            .min_w_0()
            .min_h_0()
            .flex()
            .flex_col()
            .gap(px(CARD_GAP))
            .child(preview);
        let body = div()
            .flex_1()
            .min_h_0()
            .flex()
            .gap(px(CARD_GAP))
            .font_family(theme.word(WordToken::ShapeFont))
            .child(side)
            .child(preview);
        shell(frame::titled("intro-setup", None), &theme)
            .relative()
            .size_full()
            .min_w_0()
            .child(body)
            .children(told)
    }
}
