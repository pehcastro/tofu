pub mod effects;
pub mod kept;
pub mod paint;
pub mod remembered;
pub mod tuning;

use std::path::{Path, PathBuf};
use std::time::{Instant, SystemTime};

use desk_core::model::{self, Store};
use desk_core::protocol::{
    AccountStatusState, Accounts, ModelPick, ModelsQuery, NoParams, Request, Role,
    SessionListParams, SessionSetParams, TurnSendParams, request,
};
use desk_core::sessions::SessionRow;
use desk_ui::components::form::TextArea;
use desk_ui::components::logo::{Brand, logo};
use desk_ui::components::overlay::{
    Align, MenuButton, MenuIcon, MenuItem, Placement, Popover, Side,
};
use desk_ui::components::paint::ink;
use desk_ui::components::size::{T1 as INK_T1, T3 as INK_T3};
use desk_ui::effects::EffectParams;
use desk_ui::live::ActiveTheme;
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, Entity, FontWeight,
    IntoElement, MouseButton, MouseDownEvent, PathPromptOptions, Render, SharedString, Stateful,
    StyleRefinement, Subscription, WeakEntity, Window, div, prelude::*, px,
};

use crate::modules::chat::{self, Chat, Listed};
use crate::project;
use effects::{
    Backdrop, DUSK, ROOM, STREET, after_ascii, backdrop, glass_over, hero, remembered_params,
};
use paint::{
    BRANCH, DOWN, FOLDER, LIVE, MONO, PLUS, SEND, SURFACE, T3, WARN, glyph, hex, load_fonts,
    medium, shadow, spacer, text, tint, white,
};
use remembered::Remembered;

const COMPOSER_SHARE: f32 = 0.56;
const COMPOSER_LEAST: f32 = 340.0;
const STAGE_PAD: f32 = 16.0;
const ABOVE_COMPOSER: f32 = 5.6;
const BELOW_COMPOSER: f32 = 4.4;
const RESUME_ROWS: usize = 3;
const NEWEST: i64 = RESUME_ROWS as i64 + 1;
const LOADING: &str = "Loading sessions";
const MENU_TALLEST: f32 = 460.0;
const MENU_SHARE: f32 = 0.55;
const HOVER_INK: f32 = 0.08;
const RULE_INK: f32 = 0.1;
const PICK_MODEL: &str = "Pick a model";
const NO_SESSIONS: &str = "no sessions yet";
const ATTACH: &str = "Attach";
const OPEN: &str = "Open";
const HIDE_RESUME: &str = "intro.hide_resume";
const DESK_SETTINGS: &str = "settings.json";

#[derive(Clone, Copy, PartialEq, Eq)]
enum Menu {
    Closed,
    Projects,
    Resume,
}

#[derive(Clone, Copy)]
enum Sessions {
    Waiting(Instant),
    Asked(Instant),
    Landed,
}

enum Stage {
    Home,
    Opening { text: String, at: Instant },
    Chat,
    Resuming(String),
}

struct Tofu {
    chat: Entity<Chat>,
    area: Entity<TextArea>,
    _watch: [Subscription; 2],
}

#[derive(Default)]
struct Repo {
    branch: String,
    changed: usize,
    branches: Vec<String>,
}

pub struct Intro {
    backdrop: Entity<Backdrop>,
    front: Entity<Front>,
    folders: Vec<PathBuf>,
    project: usize,
    repo: Repo,
    kept: Remembered,
    effort: Option<SharedString>,
    models: Entity<MenuButton>,
    choices: Vec<Option<(String, String)>>,
    listing: Option<ModelsQuery>,
    emails: Vec<(String, String)>,
    branches: Entity<MenuButton>,
    dressed: Option<(String, String, f32)>,
    attached: Vec<String>,
    rows: Vec<SessionRow>,
    sessions: Sessions,
    all: bool,
    ready: bool,
    resume: bool,
    stage: Stage,
    tofu: Tofu,
    menu: Menu,
    told: Option<SharedString>,
    _delta: Option<Subscription>,
}

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    load_fonts(cx)?;
    let kept = remembered::read().unwrap_or_else(|error| {
        eprintln!("desk: intro remembers nothing: {error}");
        Remembered::default()
    });
    let (source, params, menu) = match board {
        None => (
            STREET,
            remembered_params(&kept.effects, &kept.tuning),
            Menu::Closed,
        ),
        Some("IHOME-1") => (STREET, EffectParams::default(), Menu::Closed),
        Some("IHOME-2") => (ROOM, EffectParams::default(), Menu::Closed),
        Some("IHOME-3") => (DUSK, EffectParams::default(), Menu::Closed),
        Some("S-HOME-1") => (STREET, EffectParams::default(), Menu::Resume),
        Some("S-HOME-2") => (STREET, EffectParams::default(), Menu::Projects),
        Some(other) => {
            return Err(format!(
                "the intro draws IHOME-1 to 3, S-HOME-1 and S-HOME-2, not {other}"
            ));
        }
    };
    let (launched, from) = intro_project(kept.project.as_deref())?;
    eprintln!("desk: intro project {} from {from}", launched.display());
    let recents = project::recents().unwrap_or_else(|error| {
        eprintln!("desk: intro recents: {error}");
        Vec::new()
    });
    let folders: Vec<PathBuf> = std::iter::once(launched.clone())
        .chain(recents.into_iter().filter(|folder| *folder != launched))
        .collect();
    let resume = resume_on().unwrap_or_else(|error| {
        eprintln!("desk: intro reads the Resume button as on: {error}");
        true
    });
    let backdrop = backdrop(source, params.clone(), cx);
    if let (None, Some(image)) = (board, kept.image.as_deref()) {
        let fit = kept::Fit::largest(window, cx);
        backdrop.update(cx, |backdrop, cx| {
            backdrop.show_named(image, params, fit, cx)
        });
    }
    let ascii = (board.is_none() && kept.effects.contains(&remembered::Effect::Ascii))
        .then(|| kept.ascii.clone());
    let (motion, after) = (kept.motion, after_ascii(&kept.effects, &kept.tuning));
    backdrop.update(cx, |backdrop, cx| backdrop.ascii(ascii, &after, motion, cx));
    Ok(cx
        .new(|cx| {
            let tofu = Tofu::start(&launched, window, cx);
            let models = MenuButton::new(PICK_MODEL.into(), Vec::new(), cx);
            let branches = MenuButton::new(SharedString::default(), Vec::new(), cx);
            let picked = cx.weak_entity();
            models.update(cx, |button, _| {
                button.placement(Placement {
                    side: Side::Top,
                    align: Align::End,
                    ..Placement::below()
                });
                button.on_pick(move |at, _, cx| {
                    if let Err(error) = picked.update(cx, |intro, cx| intro.choose(*at, cx)) {
                        eprintln!("desk: the intro closed before a model was picked: {error}");
                    }
                });
            });
            let switched = cx.weak_entity();
            branches.update(cx, |button, _| {
                button.on_pick(move |at, _, cx| {
                    if let Err(error) = switched.update(cx, |intro, cx| intro.switch(*at, cx)) {
                        eprintln!("desk: the intro closed before a branch was picked: {error}");
                    }
                });
            });
            let mut intro = Intro {
                backdrop,
                front: Front::new(cx),
                folders,
                project: 0,
                repo: Repo::default(),
                kept: Remembered {
                    project: Some(launched.clone()),
                    ..kept
                },
                effort: None,
                models,
                choices: Vec::new(),
                listing: None,
                emails: Vec::new(),
                branches,
                dressed: None,
                attached: Vec::new(),
                rows: Vec::new(),
                sessions: Sessions::Waiting(Instant::now()),
                all: false,
                ready: false,
                resume,
                stage: Stage::Home,
                tofu,
                menu,
                told: None,
                _delta: None,
            };
            intro.read_git(cx);
            intro
        })
        .into())
}

fn resume_on() -> Result<bool, String> {
    let home = desk_core::home::Home::find().map_err(|error| error.to_string())?;
    let path = home.config.join(DESK_SETTINGS);
    let text = match std::fs::read_to_string(&path) {
        Ok(text) => text,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(true),
        Err(error) => return Err(format!("{}: {error}", path.display())),
    };
    let values: serde_json::Map<String, serde_json::Value> =
        serde_json::from_str(&text).map_err(|error| format!("{}: {error}", path.display()))?;
    match values.get(HIDE_RESUME) {
        None => Ok(true),
        Some(serde_json::Value::Bool(hidden)) => Ok(!hidden),
        Some(other) => Err(format!(
            "{}: {HIDE_RESUME} holds {other}, not a switch",
            path.display()
        )),
    }
}

fn intro_project(last: Option<&Path>) -> Result<(PathBuf, &'static str), String> {
    let last = last.filter(|folder| folder.is_dir());
    match (project::chosen()?, last) {
        (Some((_, "the most recent project")) | None, Some(last)) => {
            Ok((last.to_owned(), "intro.json"))
        }
        (Some(found), _) => Ok(found),
        (None, None) => std::env::current_dir()
            .map(|folder| (folder, "cwd"))
            .map_err(|error| format!("the intro cannot read the folder it runs in: {error}")),
    }
}

impl Tofu {
    fn start(folder: &Path, window: &mut Window, cx: &mut Context<Intro>) -> Tofu {
        let store = cx.new(|_| Store::default());
        let chat = chat::live(folder.to_owned(), store, window, cx);
        let placeholder = format!("Do anything in {}", project::name(folder));
        let submit = cx.listener(|intro, text: &str, _, cx| intro.submit(text, cx));
        let area = cx.new(|cx| {
            TextArea::new(placeholder.into(), window, cx)
                .bare()
                .placeholder_ink(ColorToken::IntroPlaceholder)
                .on_submit(submit)
        });
        let watch = [
            cx.subscribe(&chat, |intro, chat, _: &Listed, cx| intro.listed(&chat, cx)),
            cx.observe_in(&chat, window, |intro, chat, window, cx| {
                intro.ask_newest(&chat, cx);
                intro.opened(&chat, window, cx)
            }),
        ];
        Tofu {
            chat,
            area,
            _watch: watch,
        }
    }
}

fn emails(accounts: &Accounts) -> Vec<(String, String)> {
    accounts
        .subscriptions
        .iter()
        .filter(|source| source.role == Role::Llm)
        .filter_map(|source| {
            let account = source
                .accounts
                .iter()
                .find(|account| matches!(account.state, AccountStatusState::InUse))
                .or_else(|| source.accounts.first())?;
            Some((source.source.clone(), account.account.clone()))
        })
        .collect()
}

fn model_items(
    models: &ModelsQuery,
    emails: &[(String, String)],
) -> (Vec<MenuItem>, Vec<Option<(String, String)>>) {
    let usable: Vec<(&str, &str)> = models
        .models
        .iter()
        .filter(|model| model.kind == "llm" && model.r#use != "excluded")
        .filter_map(|model| Some((model.slug.split_once('/')?.0, model.id.as_str())))
        .collect();
    let mut sources: Vec<&str> = Vec::new();
    for source in models
        .subscriptions
        .iter()
        .map(String::as_str)
        .chain(usable.iter().map(|(source, _)| *source))
    {
        if !sources.contains(&source) {
            sources.push(source);
        }
    }
    let mut items = Vec::new();
    let mut choices = Vec::new();
    for source in sources {
        let inside: Vec<&str> = usable
            .iter()
            .filter(|(owner, _)| *owner == source)
            .map(|(_, model)| *model)
            .collect();
        if inside.is_empty() {
            continue;
        }
        let account = emails.iter().find(|(owner, _)| owner == source);
        choices.push(None);
        choices.extend(account.map(|_| None));
        choices.extend(
            inside
                .iter()
                .map(|model| Some((source.to_owned(), (*model).to_owned()))),
        );
        items.push(MenuItem::Submenu {
            label: source.to_owned().into(),
            icon: Brand::for_source(source).map(MenuIcon::from),
            items: account
                .map(|(_, email)| MenuItem::Caption(email.clone().into()))
                .into_iter()
                .chain(inside.into_iter().map(MenuItem::action))
                .collect(),
        });
    }
    (items, choices)
}

fn slug(pick: &ModelPick) -> Option<String> {
    let model = pick.model.as_deref()?;
    Some(match pick.wire.as_deref() {
        Some(wire) if !model.contains('/') => format!("{wire}/{model}"),
        _ => model.to_owned(),
    })
}

fn replied(store: &Store, session: &str) -> bool {
    store.sessions.get(session).is_some_and(|session| {
        session.messages.iter().any(|message| {
            matches!(message.role, model::Role::Assistant)
                && message.agent.is_none()
                && !message.text.is_empty()
        })
    })
}

fn ask_tofu<R: Request>(
    chat: &Entity<Chat>,
    params: &R::Params,
    cx: &mut Context<Intro>,
    land: impl FnOnce(&mut Intro, R::Result, &mut Context<Intro>) + 'static,
) where
    R::Result: 'static,
{
    let intro = cx.weak_entity();
    chat.update(cx, |chat, cx| {
        chat.request::<R>(params, cx, move |_, reply, cx| {
            let reply = match reply {
                Ok(reply) => reply,
                Err(error) => return eprintln!("desk: intro {}: {error}", R::METHOD),
            };
            cx.defer(move |cx| {
                let landed = intro.update(cx, |intro, cx| {
                    land(intro, reply, cx);
                    cx.notify();
                });
                if let Err(error) = landed {
                    eprintln!("desk: intro closed before {} answered: {error}", R::METHOD);
                }
            });
        });
    });
}

fn newest_first(mut rows: Vec<SessionRow>) -> Vec<SessionRow> {
    let now = SystemTime::now();
    rows.sort_by_key(|row| {
        let since = row.since(now);
        (since.is_none(), since)
    });
    rows
}

fn link(
    id: &'static str,
    icon: &str,
    label: impl Into<SharedString>,
    scale: f32,
    theme: &Theme,
) -> Stateful<Div> {
    let ink = theme.color(ColorToken::IntroLink);
    let hover = theme.color(ColorToken::IntroLinkHover);
    div()
        .id(id)
        .flex()
        .items_center()
        .gap(px(6.0))
        .cursor_pointer()
        .text_color(ink)
        .hover(move |style| style.text_color(hover))
        .child(glyph(icon, 13.0, ink, scale))
        .child(label.into())
        .child(glyph(DOWN, 13.0, ink, scale))
}

fn caption(label: impl Into<SharedString>, theme: &Theme) -> Div {
    div()
        .text_size(px(10.0))
        .line_height(px(10.0))
        .text_color(ink(theme, INK_T3))
        .font_weight(FontWeight::SEMIBOLD)
        .px(px(10.0))
        .py(px(6.0))
        .child(label.into().to_uppercase())
}

fn row(id: impl Into<gpui::ElementId>, theme: &Theme) -> Stateful<Div> {
    let hover = ink(theme, HOVER_INK);
    div()
        .id(id.into())
        .flex()
        .items_center()
        .gap(px(10.0))
        .px(px(10.0))
        .py(px(7.0))
        .rounded(px(8.0))
        .cursor_pointer()
        .text_color(ink(theme, INK_T1))
        .hover(move |style| style.bg(hover))
}

fn divider(theme: &Theme) -> Div {
    div()
        .h(px(1.0))
        .mx(px(6.0))
        .my(px(4.0))
        .bg(ink(theme, RULE_INK))
}

fn scroller(id: &'static str, window: &Window) -> Stateful<Div> {
    let tallest = (window.viewport_size().height * MENU_SHARE).min(px(MENU_TALLEST));
    div()
        .id(id)
        .flex()
        .flex_col()
        .max_h(tallest)
        .overflow_y_scroll()
        .text_size(px(13.0))
}

impl Intro {
    fn folder(&self) -> &Path {
        &self.folders[self.project]
    }

    fn keep(&self, cx: &mut Context<Self>) {
        let (project, session, session_name, model) = (
            self.kept.project.clone(),
            self.kept.session.clone(),
            self.kept.session_name.clone(),
            self.kept.model.clone(),
        );
        cx.background_executor()
            .spawn(async move {
                let kept = remembered::update(|fresh| {
                    fresh.project = project;
                    fresh.session = session;
                    fresh.session_name = session_name;
                    fresh.model = model;
                });
                if let Err(error) = kept {
                    eprintln!("desk: intro could not remember its setup: {error}");
                }
            })
            .detach();
    }

    fn go_to(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        self.menu = Menu::Closed;
        if self.project == at {
            return cx.notify();
        }
        self.project = at;
        let folder = self.folder().to_owned();
        if let Err(error) = project::remember(&folder) {
            eprintln!("desk: intro recents: {error}");
        }
        self.kept.project = Some(folder.clone());
        self.kept.session = None;
        self.kept.session_name = None;
        self.keep(cx);
        self.tofu = Tofu::start(&folder, window, cx);
        self.repo = Repo::default();
        self.dressed = None;
        self.effort = None;
        self.attached.clear();
        self.rows.clear();
        self.sessions = Sessions::Waiting(Instant::now());
        self.all = false;
        self.ready = false;
        self.read_git(cx);
        cx.notify();
    }

    fn read_git(&mut self, cx: &mut Context<Self>) {
        let folder = self.folder().to_owned();
        cx.spawn(async move |intro, cx| {
            let (head, branches) = cx
                .background_executor()
                .spawn(async move {
                    let branches = project::branches(&folder).unwrap_or_else(|error| {
                        eprintln!("desk: intro branches in {}: {error}", folder.display());
                        Vec::new()
                    });
                    (project::head(folder), branches)
                })
                .await;
            let landed = intro.update(cx, |intro, cx| {
                if intro.folder() != head.folder {
                    return;
                }
                intro.repo = Repo {
                    branch: head.branch,
                    changed: head.changed,
                    branches,
                };
                intro.dress_branches(cx);
                cx.notify();
            });
            if let Err(error) = landed {
                eprintln!("desk: intro closed before its branches were read: {error}");
            }
        })
        .detach();
    }

    fn dress_branches(&mut self, cx: &mut Context<Self>) {
        let names = self.repo.branches.iter().cloned().map(SharedString::from);
        let items: Vec<MenuItem> = match self.repo.changed {
            0 => names.map(MenuItem::action).collect(),
            changed => std::iter::once(MenuItem::Caption(
                format!("{changed} changed files: commit or stash to switch").into(),
            ))
            .chain(names.map(|label| MenuItem::Disabled { label, icon: None }))
            .collect(),
        };
        self.branches
            .update(cx, |button, cx| button.items(items, cx));
    }

    fn switch(&mut self, at: usize, cx: &mut Context<Self>) {
        if self.repo.changed > 0 {
            return;
        }
        let Some(branch) = self.repo.branches.get(at).cloned() else {
            return;
        };
        if branch == self.repo.branch {
            return;
        }
        let folder = self.folder().to_owned();
        cx.spawn(async move |intro, cx| {
            let target = branch.clone();
            let switched = cx
                .background_executor()
                .spawn(async move { project::switch(&folder, &target) })
                .await;
            let landed = intro.update(cx, |intro, cx| {
                match switched {
                    Ok(()) => eprintln!("desk: intro switched to {branch}"),
                    Err(error) => intro.told = Some(format!("git switch {branch}: {error}").into()),
                }
                intro.read_git(cx);
                cx.notify();
            });
            if let Err(error) = landed {
                eprintln!("desk: intro closed before git switch answered: {error}");
            }
        })
        .detach();
    }

    fn choose(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(Some((source, model))) = self.choices.get(at) else {
            return;
        };
        let picked = format!("{source}/{model}");
        eprintln!("desk: intro model picked {picked}");
        self.kept.model = Some(picked);
        self.keep(cx);
        cx.notify();
    }

    fn dress_models(&mut self, cx: &mut Context<Self>) {
        let Some(listing) = &self.listing else {
            return;
        };
        let (items, choices) = model_items(listing, &self.emails);
        self.choices = choices;
        if self.kept.model.is_none() {
            self.kept.model = listing.defaults.first().cloned();
        }
        self.models.update(cx, |button, cx| button.items(items, cx));
    }

    fn dress(&mut self, scale: f32, cx: &mut Context<Self>) {
        let model = self
            .kept
            .model
            .clone()
            .unwrap_or_else(|| PICK_MODEL.to_owned());
        let branch = self.repo.branch.clone();
        let key = (model.clone(), branch.clone(), scale);
        if self.dressed.as_ref() == Some(&key) {
            return;
        }
        self.dressed = Some(key);
        let brand = model
            .split_once('/')
            .and_then(|(source, _)| Brand::for_source(source));
        let effort = self.effort.clone();
        self.models.update(cx, |button, cx| {
            button.trigger(move |_, theme| {
                div()
                    .id("intro-model")
                    .flex()
                    .items_center()
                    .gap(px(6.0))
                    .h(px(24.0))
                    .px(px(9.0))
                    .cursor_pointer()
                    .children(brand.map(|brand| logo(brand, px(14.0), theme)))
                    .child(medium(
                        12.5,
                        12.5,
                        theme.color(ColorToken::IntroModelText),
                        model.clone(),
                    ))
                    .children(
                        effort
                            .clone()
                            .map(|effort| medium(12.5, 12.5, white(T3), effort)),
                    )
                    .child(glyph(DOWN, 13.0, white(0.85 * 0.6), scale))
            });
            cx.notify();
        });
        self.branches.update(cx, |button, cx| {
            button.trigger(move |_, theme| {
                link("intro-branch", BRANCH, branch.clone(), scale, theme)
            });
            cx.notify();
        });
    }

    pub fn backdrop(&self) -> &Entity<Backdrop> {
        &self.backdrop
    }

    pub fn left(&self) -> bool {
        !matches!(self.stage, Stage::Home)
    }

    pub fn chat(&self) -> &Entity<Chat> {
        &self.tofu.chat
    }

    pub fn resuming(&self) -> Option<(PathBuf, String)> {
        match &self.stage {
            Stage::Resuming(session) => Some((self.folder().to_owned(), session.clone())),
            Stage::Home | Stage::Opening { .. } | Stage::Chat => None,
        }
    }

    fn latest(&self) -> Option<&SessionRow> {
        let kept = self.kept.session.as_deref();
        self.rows
            .iter()
            .find(|row| Some(row.id.as_str()) == kept)
            .or_else(|| self.rows.first())
    }

    fn ask_newest(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        let Sessions::Waiting(at) = self.sessions else {
            return;
        };
        if chat.read(cx).project().is_empty() {
            return;
        }
        self.sessions = Sessions::Asked(at);
        let from = chat.entity_id();
        let params = SessionListParams {
            limit: Some(NEWEST),
            ..SessionListParams::default()
        };
        ask_tofu::<request::SessionList>(chat, &params, cx, move |intro, listed, _| {
            if intro.tofu.chat.entity_id() != from || intro.ready {
                return;
            }
            intro.rows = newest_first(listed.sessions);
            intro.landed();
        });
    }

    fn landed(&mut self) {
        let (Sessions::Waiting(at) | Sessions::Asked(at)) = self.sessions else {
            return;
        };
        self.sessions = Sessions::Landed;
        eprintln!(
            "desk: intro sessions {} in {} ms",
            self.rows.len(),
            at.elapsed().as_millis()
        );
    }

    fn listed(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        self.rows = newest_first(chat.read(cx).rows().to_vec());
        self.landed();
        let named = self
            .rows
            .iter()
            .find(|row| Some(&row.id) == self.kept.session.as_ref())
            .and_then(|row| row.name.clone())
            .filter(|name| !name.is_empty());
        if named.is_some() && named != self.kept.session_name {
            self.kept.session_name = named;
            self.keep(cx);
        }
        if !self.ready {
            self.ready = true;
            match (self.resume, self.latest()) {
                (false, _) => {
                    eprintln!("desk: intro resume row off by the Resume button setting")
                }
                (true, None) => eprintln!(
                    "desk: intro resume has no session in {}",
                    self.folder().display()
                ),
                (true, Some(row)) => eprintln!(
                    "desk: intro resume target {} {} in {}",
                    row.id,
                    row.title(),
                    self.folder().display()
                ),
            }
            self.ask(chat, cx);
            if let Stage::Opening { .. } = self.stage {
                chat.update(cx, |chat, cx| chat.open_session(None, cx));
            }
        }
        cx.notify();
    }

    fn ask(&self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        ask_tofu::<request::QueryAccounts>(chat, &NoParams {}, cx, |intro, accounts, cx| {
            intro.emails = emails(&accounts);
            intro.dress_models(cx);
        });
        ask_tofu::<request::QueryModels>(chat, &NoParams {}, cx, |intro, models, cx| {
            eprintln!("desk: intro query.models {} rows", models.models.len());
            intro.listing = Some(models);
            intro.dress_models(cx);
        });
        ask_tofu::<request::SessionState>(chat, &NoParams {}, cx, |intro, state, _| {
            intro.effort = state.pick.effort.clone().map(Into::into);
            if intro.kept.model.is_none() {
                intro.kept.model = slug(&state.pick);
            }
            intro.dressed = None;
        });
    }

    fn submit(&mut self, text: &str, cx: &mut Context<Self>) {
        let text = text.trim();
        if text.is_empty() || !matches!(self.stage, Stage::Home) {
            return;
        }
        let mut words: Vec<String> = self
            .attached
            .drain(..)
            .map(|path| format!("@{path}"))
            .collect();
        words.push(text.to_owned());
        self.tofu.area.update(cx, |area, cx| area.clear(cx));
        self.stage = Stage::Opening {
            text: words.join(" "),
            at: Instant::now(),
        };
        self.menu = Menu::Closed;
        if self.ready {
            self.tofu
                .chat
                .update(cx, |chat, cx| chat.open_session(None, cx));
        }
        cx.notify();
    }

    fn opened(&mut self, chat: &Entity<Chat>, window: &mut Window, cx: &mut Context<Self>) {
        let Stage::Opening { .. } = self.stage else {
            return;
        };
        let Some(session) = chat.read(cx).open_id().map(str::to_owned) else {
            return;
        };
        let Stage::Opening { text, at } = std::mem::replace(&mut self.stage, Stage::Chat) else {
            return;
        };
        self.kept.session = Some(session.clone());
        self.kept.session_name = None;
        self.keep(cx);
        let store = chat.read(cx).store().clone();
        let waiting = session.clone();
        self._delta = Some(
            cx.observe_in(&store, window, move |intro, store, window, cx| {
                if !replied(store.read(cx), &waiting) {
                    return;
                }
                intro._delta = None;
                window.on_next_frame(move |_, _| {
                    eprintln!(
                        "desk: intro turn first delta {} ms",
                        at.elapsed().as_millis()
                    );
                });
            }),
        );
        let set = self
            .kept
            .model
            .as_deref()
            .and_then(|slug| slug.split_once('/'))
            .map(|(wire, model)| SessionSetParams {
                wire: Some(wire.to_owned()),
                model: Some(model.to_owned()),
                ..SessionSetParams::default()
            });
        let params = TurnSendParams {
            session,
            text,
            ..TurnSendParams::default()
        };
        chat.update(cx, |chat, cx| {
            if let Some(set) = set {
                chat.request::<request::SessionSet>(&set, cx, |_, reply, _| match reply {
                    Ok(ack) => eprintln!("desk: intro session.set answered ok {}", ack.ok),
                    Err(error) => eprintln!("desk: intro session.set: {error}"),
                });
            }
            chat.request::<request::TurnSend>(&params, cx, |_, sent, _| match sent {
                Ok(sent) => eprintln!("desk: intro turn {} sent", sent.turn),
                Err(error) => eprintln!("desk: intro turn.send: {error}"),
            });
        });
        cx.notify();
    }

    fn attach(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let asked = cx.prompt_for_paths(PathPromptOptions {
            files: true,
            directories: false,
            multiple: true,
            prompt: Some(ATTACH.into()),
        });
        cx.spawn_in(window, async move |intro, cx| {
            let picked = match asked.await {
                Ok(Ok(Some(picked))) => picked,
                Ok(Ok(None)) => return eprintln!("desk: intro attach: no file was picked"),
                Ok(Err(error)) => return eprintln!("desk: intro attach: {error}"),
                Err(_) => return eprintln!("desk: intro attach: the dialog closed unanswered"),
            };
            let landed = intro.update(cx, |intro, cx| {
                let root = intro.folder().to_owned();
                for path in picked {
                    let shown = path
                        .strip_prefix(&root)
                        .unwrap_or(&path)
                        .to_string_lossy()
                        .replace('\\', "/");
                    if !intro.attached.contains(&shown) {
                        intro.attached.push(shown);
                    }
                }
                cx.notify();
            });
            if landed.is_err() {
                eprintln!("desk: the intro closed before the attached file landed");
            }
        })
        .detach();
    }

    fn open_folder(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.menu = Menu::Closed;
        let asked = cx.prompt_for_paths(PathPromptOptions {
            files: false,
            directories: true,
            multiple: false,
            prompt: Some(OPEN.into()),
        });
        cx.spawn_in(window, async move |intro, cx| {
            let folder = match asked.await {
                Ok(Ok(Some(picked))) => picked.into_iter().next(),
                Ok(Ok(None)) => None,
                Ok(Err(error)) => return eprintln!("desk: intro open folder: {error}"),
                Err(_) => {
                    return eprintln!("desk: intro open folder: the dialog closed unanswered");
                }
            };
            let Some(folder) = folder else {
                return eprintln!("desk: intro open folder: no folder was picked");
            };
            let landed = intro.update_in(cx, |intro, window, cx| {
                let at = match intro.folders.iter().position(|known| *known == folder) {
                    Some(at) => at,
                    None => {
                        intro.folders.push(folder);
                        intro.folders.len() - 1
                    }
                };
                intro.go_to(at, window, cx);
            });
            if let Err(error) = landed {
                eprintln!("desk: the intro closed before the folder opened: {error}");
            }
        })
        .detach();
    }

    fn toggle(menu: Menu) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.menu = if this.menu == menu {
                Menu::Closed
            } else {
                menu
            };
            this.all = false;
            cx.notify();
        }
    }

    fn card(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let theme = ActiveTheme::theme(cx);
        div()
            .relative()
            .rounded(px(18.0))
            .shadow(shadow(70.0, 24.0, 0.4))
            .child(glass_over(
                18.0,
                &theme,
                theme.color(ColorToken::IntroComposerFill),
                0.45,
                0.65,
            ))
            .pt(px(14.0))
            .pr(px(12.0))
            .pb(px(10.0))
            .pl(px(16.0))
            .flex()
            .flex_col()
            .gap(px(20.0))
            .when(!self.attached.is_empty(), |card| {
                card.child(div().flex().flex_wrap().gap(px(6.0)).children(
                    self.attached.iter().enumerate().map(|(at, path)| {
                        div()
                            .id(("attached", at))
                            .flex()
                            .items_center()
                            .gap(px(6.0))
                            .px(px(8.0))
                            .py(px(2.0))
                            .rounded(px(6.0))
                            .bg(white(0.08))
                            .cursor_pointer()
                            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                                if at < this.attached.len() {
                                    this.attached.remove(at);
                                    cx.notify();
                                }
                            }))
                            .child(text(12.0, 16.0, white(0.8), path.clone()).font_family(MONO))
                            .child(text(12.0, 16.0, white(0.45), "×"))
                    }),
                ))
            })
            .child(
                div()
                    .text_size(px(15.0))
                    .line_height(px(22.0))
                    .text_color(white(0.92))
                    .child(self.tofu.area.clone()),
            )
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(8.0))
                    .text_size(px(12.5))
                    .child(
                        div()
                            .id("attach")
                            .flex()
                            .items_center()
                            .justify_center()
                            .size(px(26.0))
                            .rounded(px(7.0))
                            .cursor_pointer()
                            .on_click(cx.listener(|this, _: &ClickEvent, window, cx| {
                                this.attach(window, cx)
                            }))
                            .child(glyph(PLUS, 16.0, theme.color(ColorToken::IntroIcon), scale)),
                    )
                    .child(spacer())
                    .child(self.models.clone())
                    .child(
                        div()
                            .id("send")
                            .flex()
                            .items_center()
                            .justify_center()
                            .size(px(30.0))
                            .rounded(px(15.0))
                            .cursor_pointer()
                            .bg(theme.color(ColorToken::IntroSendFill))
                            .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                let text = this.tofu.area.read(cx).text();
                                this.submit(&text, cx);
                            }))
                            .child(glyph(
                                SEND,
                                16.0,
                                theme.color(ColorToken::IntroSendIcon),
                                scale,
                            )),
                    ),
            )
            .child(
                div()
                    .absolute()
                    .inset_0()
                    .rounded(px(18.0))
                    .border_1()
                    .border_color(theme.color(ColorToken::IntroComposerBorder)),
            )
    }

    fn resume_list(&self, theme: &Theme, window: &Window, cx: &mut Context<Self>) -> Div {
        let now = SystemTime::now();
        let name = project::name(self.folder());
        let shown = match self.all {
            true => self.rows.len(),
            false => RESUME_ROWS,
        };
        let faint = ink(theme, INK_T3);
        div()
            .flex()
            .flex_col()
            .child(caption(format!("Sessions in {name}"), theme))
            .when(self.rows.is_empty(), |list| {
                let said = match self.sessions {
                    Sessions::Waiting(_) | Sessions::Asked(_) => LOADING.to_owned(),
                    Sessions::Landed => format!("No sessions in {name} yet"),
                };
                list.child(div().px(px(10.0)).py(px(7.0)).text_color(faint).child(said))
            })
            .child(
                scroller("intro-sessions", window).children(
                    self.rows
                        .iter()
                        .take(shown)
                        .enumerate()
                        .map(|(index, session)| {
                            let dot = match (session.running, session.expired) {
                                (true, _) => LIVE,
                                (false, true) => WARN,
                                (false, false) => white(0.3),
                            };
                            let id = session.id.clone();
                            let named = session.name.clone().filter(|name| !name.is_empty());
                            row(("session", index), theme)
                                .items_center()
                                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                                    this.kept.session = Some(id.clone());
                                    this.kept.session_name = named.clone();
                                    this.keep(cx);
                                    this.stage = Stage::Resuming(id.clone());
                                    this.menu = Menu::Closed;
                                    cx.notify();
                                }))
                                .child(
                                    div()
                                        .flex_none()
                                        .size(px(6.0))
                                        .rounded(px(3.0))
                                        .bg(dot),
                                )
                                .child(
                                    div()
                                        .flex_1()
                                        .min_w_0()
                                        .line_height(px(18.0))
                                        .flex()
                                        .items_center()
                                        .gap(px(7.0))
                                        .child(session.title().to_owned())
                                        .when(index == 0, |line| {
                                            line.child(
                                                text(10.5, 15.0, hex(0xc4d4f6), "Latest")
                                                    .px(px(6.0))
                                                    .py(px(1.0))
                                                    .rounded(px(5.0))
                                                    .bg(tint(0x9db8f0, 0.16)),
                                            )
                                        })
                                        .child(spacer())
                                        .child(
                                            div()
                                                .flex_none()
                                                .text_size(px(11.5))
                                                .text_color(faint)
                                                .child(session.age(now).unwrap_or_default()),
                                        ),
                                )
                        }),
                ),
            )
            .when(self.rows.len() > RESUME_ROWS && !self.all, |list| {
                list.child(
                    row("all-sessions", theme)
                        .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                            this.all = true;
                            cx.notify();
                        }))
                        .child(match self.ready {
                            true => format!("All sessions ({})", self.rows.len()),
                            false => "All sessions".to_owned(),
                        }),
                )
            })
    }

    fn project_list(&self, theme: &Theme, window: &Window, cx: &mut Context<Self>) -> Div {
        let faint = ink(theme, INK_T3);
        let picked = ink(theme, HOVER_INK);
        div()
            .flex()
            .flex_col()
            .child(caption("Recent projects", theme))
            .child(scroller("intro-projects", window).children(
                self.folders.iter().enumerate().map(|(index, folder)| {
                    let name = project::name(folder);
                    let initial: String = name.chars().take(1).collect();
                    row(("project", index), theme)
                        .when(index == self.project, |row| row.bg(picked))
                        .on_click(cx.listener(move |this, _: &ClickEvent, window, cx| {
                            this.go_to(index, window, cx)
                        }))
                        .child(
                            div()
                                .flex()
                                .flex_none()
                                .items_center()
                                .justify_center()
                                .size(px(22.0))
                                .rounded(px(7.0))
                                .bg(picked)
                                .text_size(px(11.0))
                                .font_weight(FontWeight::SEMIBOLD)
                                .child(initial),
                        )
                        .child(
                            div()
                                .flex_1()
                                .min_w_0()
                                .flex()
                                .flex_col()
                                .line_height(px(16.0))
                                .child(name)
                                .child(
                                    div()
                                        .text_size(px(11.0))
                                        .text_color(faint)
                                        .font_family(MONO)
                                        .truncate()
                                        .child(folder.display().to_string()),
                                ),
                        )
                }),
            ))
            .child(divider(theme))
            .child(
                row("open-folder", theme)
                    .on_click(
                        cx.listener(|this, _: &ClickEvent, window, cx| {
                            this.open_folder(window, cx)
                        }),
                    )
                    .child("Open a folder"),
            )
    }

    fn home(&mut self, window: &mut Window, cx: &mut Context<Self>) -> AnyElement {
        let scale = window.scale_factor();
        self.dress(scale, cx);
        hero(&self.backdrop)
            .child(
                self.front
                    .clone()
                    .cached(StyleRefinement::default().absolute().inset_0()),
            )
            .when(self.menu != Menu::Closed, |hero| {
                hero.child(
                    div()
                        .id("intro-menu-shade")
                        .absolute()
                        .inset_0()
                        .occlude()
                        .on_mouse_down(
                            MouseButton::Left,
                            cx.listener(|this, _: &MouseDownEvent, _, cx| {
                                this.menu = Menu::Closed;
                                cx.notify();
                            }),
                        ),
                )
            })
            .children(self.told.clone().map(|message| {
                paint::toast(
                    message,
                    scale,
                    cx.listener(|this, _: &ClickEvent, _, cx| {
                        this.told = None;
                        cx.notify();
                    }),
                )
            }))
            .into_any_element()
    }

    fn front(&mut self, window: &mut Window, cx: &mut Context<Self>) -> Div {
        let scale = window.scale_factor();
        let theme = ActiveTheme::theme(cx);
        let name = project::name(self.folder());
        let latest: SharedString = match (self.latest(), self.sessions) {
            (Some(row), _) => row.title().to_owned().into(),
            (None, Sessions::Waiting(_) | Sessions::Asked(_)) => LOADING.into(),
            (None, Sessions::Landed) => NO_SESSIONS.into(),
        };
        let below = Placement {
            side: Side::Bottom,
            align: Align::End,
            ..Placement::below()
        };
        let projects = Popover::new(
            "intro-projects-menu",
            link("project", FOLDER, name, scale, &theme)
                .on_click(cx.listener(Self::toggle(Menu::Projects))),
        )
        .open(self.menu == Menu::Projects)
        .placement(below)
        .width(332.0)
        .children((self.menu == Menu::Projects).then(|| self.project_list(&theme, window, cx)));
        let link_ink = theme.color(ColorToken::IntroLink);
        let link_hover = theme.color(ColorToken::IntroLinkHover);
        let resume = self.resume.then(|| {
            let trigger = div()
                .id("resume")
                .flex()
                .min_w_0()
                .items_center()
                .gap(px(6.0))
                .cursor_pointer()
                .text_color(link_ink)
                .hover(move |style| style.text_color(link_hover))
                .on_click(cx.listener(Self::toggle(Menu::Resume)))
                .child(glyph(
                    r#"<circle cx="8" cy="8" r="5.5"/><path d="M8 5v3l2 1.5"/>"#,
                    13.0,
                    link_ink,
                    scale,
                ))
                .child("Resume")
                .child(div().min_w_0().truncate().child(format!("· {latest}")))
                .child(glyph(DOWN, 13.0, link_ink, scale));
            Popover::new("intro-resume-menu", trigger)
                .open(self.menu == Menu::Resume)
                .placement(below)
                .width(320.0)
                .children((self.menu == Menu::Resume).then(|| self.resume_list(&theme, window, cx)))
        });
        let column = div()
            .relative()
            .flex_none()
            .w(gpui::relative(COMPOSER_SHARE))
            .min_w(px(COMPOSER_LEAST))
            .max_w_full()
            .flex()
            .flex_col()
            .gap(px(10.0))
            .text_size(px(12.5))
            .child(div().flex().justify_end().child(projects))
            .child(self.card(scale, cx))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(16.0))
                    .px(px(6.0))
                    .when(!self.repo.branches.is_empty(), |links| {
                        links.child(self.branches.clone())
                    })
                    .child(spacer())
                    .children(resume),
            );
        div()
            .size_full()
            .flex()
            .flex_col()
            .items_center()
            .px(px(STAGE_PAD))
            .child(div().flex_grow(ABOVE_COMPOSER))
            .child(column)
            .child(div().flex_grow(BELOW_COMPOSER))
    }
}

struct Front {
    intro: WeakEntity<Intro>,
    _redrawn: Subscription,
}

impl Front {
    fn new(cx: &mut Context<Intro>) -> Entity<Front> {
        let intro = cx.entity();
        cx.new(|cx| Front {
            intro: intro.downgrade(),
            _redrawn: cx.observe(&intro, |_, _, cx| cx.notify()),
        })
    }
}

impl Render for Front {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        desk_perf::span!("intro-front");
        self.intro
            .update(cx, |intro, cx| intro.front(window, cx))
            .unwrap_or_else(|_| div())
    }
}

impl Render for Intro {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        match self.stage {
            Stage::Home => self.home(window, cx),
            Stage::Opening { .. } | Stage::Chat | Stage::Resuming(_) => div()
                .size_full()
                .bg(hex(SURFACE))
                .child(self.tofu.chat.clone())
                .into_any_element(),
        }
    }
}
