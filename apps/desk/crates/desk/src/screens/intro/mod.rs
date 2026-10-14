pub mod effects;
mod fixture;
pub mod paint;

use std::path::{Path, PathBuf};
use std::time::{Instant, SystemTime};

use desk_core::model::{self, Store};
use desk_core::protocol::{
    AccountStatusState, Accounts, ModelPick, NoParams, Role, TurnSendParams, request,
};
use desk_core::sessions::SessionRow;
use desk_ui::components::form::TextArea;
use desk_ui::effects::EffectParams;
use gpui::{
    AnyElement, AnyView, App, AppContext, ClickEvent, Context, Div, Entity, FontWeight,
    IntoElement, Render, SharedString, Stateful, Subscription, Window, div, prelude::*, px,
    relative,
};

use crate::modules::chat::{self, Chat, Listed};
use crate::project;
use effects::{Backdrop, DUSK, ROOM, STREET, backdrop, glass, hero};
use fixture::{
    CHECKOUT, TELL_ACCOUNT, TELL_ALL_SESSIONS, TELL_ATTACH, TELL_BRANCH, TELL_CHECKOUT,
    TELL_FOLDER, TELL_MODEL,
};
use paint::{
    BRANCH, DOWN, FOLDER, LIVE, MONO, PLUS, POP, SEND, SURFACE, T3, WARN, glyph, hex, load_fonts,
    medium, ringed, shadow, spacer, text, tint, white,
};

const CARD_SCRIM: u32 = 0x18171e;
const COMPOSER_SHARE: f32 = 0.56;
const COMPOSER_LEAST: f32 = 340.0;
const STAGE_PAD: f32 = 16.0;
const ABOVE_COMPOSER: f32 = 5.6;
const BELOW_COMPOSER: f32 = 4.4;
const LINK_INK: f32 = 0.72;
const CHEVRON_INK: f32 = 0.43;
const RESUME_ROWS: usize = 5;
const READING_ACCOUNT: &str = "Reading accounts";
const NO_ACCOUNT: &str = "No account";
const READING_MODEL: &str = "Reading model";
const NO_MODEL: &str = "No model";
const NO_BRANCH: &str = "No branch";
const NO_SESSIONS: &str = "no sessions yet";

#[derive(Clone, Copy, PartialEq, Eq)]
enum Menu {
    Closed,
    Projects,
    Resume,
}

enum Stage {
    Home,
    Opening { text: String, at: Instant },
    Chat,
}

struct Tofu {
    chat: Entity<Chat>,
    area: Entity<TextArea>,
    _watch: [Subscription; 2],
}

struct Intro {
    backdrop: Entity<Backdrop>,
    folders: Vec<PathBuf>,
    project: usize,
    branch: SharedString,
    account: SharedString,
    model: SharedString,
    effort: Option<SharedString>,
    rows: Vec<SessionRow>,
    ready: bool,
    stage: Stage,
    tofu: Tofu,
    menu: Menu,
    told: Option<SharedString>,
    _delta: Option<Subscription>,
}

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    load_fonts(cx)?;
    let (source, menu) = match board {
        None | Some("IHOME-1") => (STREET, Menu::Closed),
        Some("IHOME-2") => (ROOM, Menu::Closed),
        Some("IHOME-3") => (DUSK, Menu::Closed),
        Some("S-HOME-1") => (STREET, Menu::Resume),
        Some("S-HOME-2") => (STREET, Menu::Projects),
        Some(other) => {
            return Err(format!(
                "the intro draws IHOME-1 to 3, S-HOME-1 and S-HOME-2, not {other}"
            ));
        }
    };
    let launched = project::launch()?;
    let recents = project::recents().unwrap_or_else(|error| {
        eprintln!("desk: intro recents: {error}");
        Vec::new()
    });
    let folders: Vec<PathBuf> = std::iter::once(launched.clone())
        .chain(recents.into_iter().filter(|folder| *folder != launched))
        .collect();
    let backdrop = backdrop(source, EffectParams::default(), cx);
    Ok(cx
        .new(|cx| {
            let tofu = Tofu::start(&launched, window, cx);
            Intro {
                backdrop,
                folders,
                project: 0,
                branch: SharedString::default(),
                account: READING_ACCOUNT.into(),
                model: READING_MODEL.into(),
                effort: None,
                rows: Vec::new(),
                ready: false,
                stage: Stage::Home,
                tofu,
                menu,
                told: None,
                _delta: None,
            }
        })
        .into())
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
                .on_submit(submit)
        });
        let watch = [
            cx.subscribe(&chat, |intro, chat, _: &Listed, cx| intro.listed(&chat, cx)),
            cx.observe_in(&chat, window, |intro, chat, window, cx| {
                intro.opened(&chat, window, cx)
            }),
        ];
        let read = folder.to_owned();
        cx.spawn(async move |intro, cx| {
            let head = cx
                .background_executor()
                .spawn(async move { project::head(read) })
                .await;
            let told = intro.update(cx, |intro, cx| {
                if intro.folder() == head.folder {
                    intro.branch = head.branch.into();
                    cx.notify();
                }
            });
            if let Err(error) = told {
                eprintln!("desk: intro closed before its branch was read: {error}");
            }
        })
        .detach();
        Tofu {
            chat,
            area,
            _watch: watch,
        }
    }
}

fn account_said(accounts: &Accounts) -> Option<String> {
    let subscribed = || {
        accounts
            .subscriptions
            .iter()
            .filter(|source| source.role == Role::Llm)
            .flat_map(|source| {
                source
                    .accounts
                    .iter()
                    .map(move |account| (&source.source, account))
            })
    };
    let serving = subscribed()
        .find(|(_, account)| matches!(account.state, AccountStatusState::InUse))
        .or_else(|| subscribed().next())
        .map(|(source, account)| format!("{source} · {}", account.account));
    serving.or_else(|| {
        accounts
            .keys
            .iter()
            .find(|key| key.role == Role::Llm)
            .map(|key| format!("{} · key", key.provider))
    })
}

fn model_said(pick: &ModelPick) -> Option<String> {
    let model = pick.model.as_deref()?;
    Some(match pick.wire.as_deref() {
        Some(wire) => format!("{wire}/{model}"),
        None => model.to_owned(),
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

fn link(id: &'static str, icon: &str, label: impl Into<SharedString>, scale: f32) -> Stateful<Div> {
    div()
        .id(id)
        .flex()
        .items_center()
        .gap(px(6.0))
        .cursor_pointer()
        .text_color(white(LINK_INK))
        .child(glyph(icon, 13.0, white(LINK_INK), scale))
        .child(label.into())
        .child(glyph(DOWN, 13.0, white(CHEVRON_INK), scale))
}

fn caption(label: impl Into<SharedString>) -> Div {
    text(10.0, 10.0, white(0.45), label.into().to_uppercase())
        .font_weight(FontWeight::SEMIBOLD)
        .px(px(10.0))
        .py(px(6.0))
}

fn row(id: impl Into<gpui::ElementId>) -> Stateful<Div> {
    div()
        .id(id.into())
        .flex()
        .items_center()
        .gap(px(10.0))
        .px(px(10.0))
        .py(px(7.0))
        .rounded(px(8.0))
        .cursor_pointer()
}

fn divider() -> Div {
    div().h(px(1.0)).mx(px(6.0)).my(px(4.0)).bg(white(0.07))
}

fn pop(width: f32) -> Div {
    ringed(12.0, 0.12)
        .absolute()
        .right_0()
        .w(px(width))
        .max_w_full()
        .p(px(6.0))
        .bg(POP)
        .shadow(shadow(25.0, 22.0, 0.6))
        .text_size(px(13.0))
        .flex()
        .flex_col()
}

impl Intro {
    fn folder(&self) -> &Path {
        &self.folders[self.project]
    }

    fn connect(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let folder = self.folder().to_owned();
        self.tofu = Tofu::start(&folder, window, cx);
        self.branch = SharedString::default();
        self.account = READING_ACCOUNT.into();
        self.model = READING_MODEL.into();
        self.effort = None;
        self.rows.clear();
        self.ready = false;
    }

    fn listed(&mut self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        let now = SystemTime::now();
        self.rows = chat.read(cx).rows().to_vec();
        self.rows.sort_by_key(|row| {
            let since = row.since(now);
            (since.is_none(), since)
        });
        if !self.ready {
            self.ready = true;
            self.ask(chat, cx);
            if let Stage::Opening { .. } = self.stage {
                chat.update(cx, |chat, cx| chat.open_session(None, cx));
            }
        }
        cx.notify();
    }

    fn ask(&self, chat: &Entity<Chat>, cx: &mut Context<Self>) {
        let intro = cx.weak_entity();
        chat.update(cx, |chat, cx| {
            let told = intro.clone();
            chat.request::<request::QueryAccounts>(&NoParams {}, cx, move |_, reply, cx| {
                let said = reply
                    .map(|accounts| account_said(&accounts))
                    .unwrap_or_else(|error| {
                        eprintln!("desk: intro query.accounts: {error}");
                        None
                    });
                cx.defer(move |cx| {
                    let shown = told.update(cx, |intro, cx| {
                        intro.account = said.map_or(NO_ACCOUNT.into(), Into::into);
                        cx.notify();
                    });
                    if let Err(error) = shown {
                        eprintln!("desk: intro closed before query.accounts answered: {error}");
                    }
                });
            });
            chat.request::<request::SessionState>(&NoParams {}, cx, move |_, reply, cx| {
                let pick = reply.map(|state| state.pick).unwrap_or_else(|error| {
                    eprintln!("desk: intro session.state: {error}");
                    ModelPick::default()
                });
                cx.defer(move |cx| {
                    let shown = intro.update(cx, |intro, cx| {
                        intro.model = model_said(&pick).map_or(NO_MODEL.into(), Into::into);
                        intro.effort = pick.effort.map(Into::into);
                        cx.notify();
                    });
                    if let Err(error) = shown {
                        eprintln!("desk: intro closed before session.state answered: {error}");
                    }
                });
            });
        });
    }

    fn submit(&mut self, text: &str, cx: &mut Context<Self>) {
        let text = text.trim();
        if text.is_empty() || !matches!(self.stage, Stage::Home) {
            return;
        }
        self.tofu.area.update(cx, |area, cx| area.clear(cx));
        self.stage = Stage::Opening {
            text: text.to_owned(),
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
        let params = TurnSendParams {
            session,
            text,
            ..TurnSendParams::default()
        };
        chat.update(cx, |chat, cx| {
            chat.request::<request::TurnSend>(&params, cx, |_, sent, _| match sent {
                Ok(sent) => eprintln!("desk: intro turn {} sent", sent.turn),
                Err(error) => eprintln!("desk: intro turn.send: {error}"),
            });
        });
        cx.notify();
    }

    fn tell(
        message: &'static str,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.told = Some(message.into());
            this.menu = Menu::Closed;
            cx.notify();
        }
    }

    fn toggle(menu: Menu) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.menu = if this.menu == menu {
                Menu::Closed
            } else {
                menu
            };
            cx.notify();
        }
    }

    fn card(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        div()
            .relative()
            .rounded(px(18.0))
            .shadow(shadow(70.0, 24.0, 0.4))
            .child(glass(18.0, CARD_SCRIM, 0.45, 0.65))
            .pt(px(14.0))
            .pr(px(12.0))
            .pb(px(10.0))
            .pl(px(16.0))
            .flex()
            .flex_col()
            .gap(px(20.0))
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
                            .on_click(cx.listener(Self::tell(TELL_ATTACH)))
                            .child(glyph(PLUS, 16.0, white(0.45), scale)),
                    )
                    .child(spacer())
                    .child(
                        div()
                            .id("model")
                            .flex()
                            .items_center()
                            .gap(px(6.0))
                            .h(px(24.0))
                            .px(px(9.0))
                            .cursor_pointer()
                            .on_click(cx.listener(Self::tell(TELL_MODEL)))
                            .child(medium(12.5, 12.5, white(0.85), self.model.clone()))
                            .children(
                                self.effort
                                    .clone()
                                    .map(|effort| medium(12.5, 12.5, white(T3), effort)),
                            )
                            .child(glyph(DOWN, 13.0, white(0.85 * 0.6), scale)),
                    )
                    .child(
                        div()
                            .id("send")
                            .flex()
                            .items_center()
                            .justify_center()
                            .size(px(30.0))
                            .rounded(px(15.0))
                            .cursor_pointer()
                            .bg(white(0.92))
                            .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
                                let text = this.tofu.area.read(cx).text();
                                this.submit(&text, cx);
                            }))
                            .child(glyph(SEND, 16.0, hex(0x111111), scale)),
                    ),
            )
            .child(ringed(18.0, 0.16).absolute().inset_0())
    }

    fn resume(&self, cx: &mut Context<Self>) -> Div {
        let now = SystemTime::now();
        let name = project::name(self.folder());
        pop(432.0)
            .top(relative(1.0))
            .mt(px(6.0))
            .child(caption(format!("Sessions in {name}")))
            .when(self.rows.is_empty(), |pop| {
                pop.child(
                    div()
                        .px(px(10.0))
                        .py(px(7.0))
                        .text_color(white(T3))
                        .child(format!("No sessions in {name} yet")),
                )
            })
            .children(
                self.rows
                    .iter()
                    .take(RESUME_ROWS)
                    .enumerate()
                    .map(|(index, session)| {
                        let dot = match (session.running, session.expired) {
                            (true, _) => LIVE,
                            (false, true) => WARN,
                            (false, false) => white(0.3),
                        };
                        let id = session.id.clone();
                        row(("session", index))
                            .items_start()
                            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                                this.stage = Stage::Chat;
                                this.menu = Menu::Closed;
                                let id = id.clone();
                                this.tofu
                                    .chat
                                    .update(cx, |chat, cx| chat.open_session(Some(id), cx));
                                cx.notify();
                            }))
                            .child(
                                div()
                                    .flex_none()
                                    .size(px(6.0))
                                    .mt(px(7.0))
                                    .rounded(px(3.0))
                                    .bg(dot),
                            )
                            .child(
                                div()
                                    .flex_1()
                                    .min_w_0()
                                    .line_height(px(18.0))
                                    .flex()
                                    .flex_col()
                                    .child(
                                        div()
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
                                            .child(text(
                                                11.5,
                                                18.0,
                                                white(T3),
                                                session.age(now).unwrap_or_default(),
                                            )),
                                    )
                                    .child(
                                        div()
                                            .text_size(px(12.0))
                                            .text_color(white(T3))
                                            .overflow_hidden()
                                            .whitespace_nowrap()
                                            .text_ellipsis()
                                            .child(session.task.clone().unwrap_or_default()),
                                    ),
                            )
                    }),
            )
            .child(divider())
            .child(
                row("all-sessions")
                    .text_color(white(0.6))
                    .on_click(cx.listener(Self::tell(TELL_ALL_SESSIONS)))
                    .child("All sessions")
                    .child(spacer())
                    .child(
                        text(11.0, 16.0, white(0.5), "ctrl r")
                            .font_family(MONO)
                            .font_weight(FontWeight::MEDIUM)
                            .px(px(6.0))
                            .py(px(1.0))
                            .rounded(px(5.0))
                            .bg(white(0.08)),
                    ),
            )
    }

    fn projects(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        pop(332.0)
            .top(px(26.0))
            .child(caption("Recent projects"))
            .children(self.folders.iter().enumerate().map(|(index, folder)| {
                let name = project::name(folder);
                let initial: String = name.chars().take(1).collect();
                row(("project", index))
                    .when(index == self.project, |row| row.bg(white(0.08)))
                    .on_click(cx.listener(move |this, _: &ClickEvent, window, cx| {
                        this.menu = Menu::Closed;
                        if this.project != index {
                            this.project = index;
                            this.connect(window, cx);
                        }
                        cx.notify();
                    }))
                    .child(
                        div()
                            .flex()
                            .flex_none()
                            .items_center()
                            .justify_center()
                            .size(px(22.0))
                            .rounded(px(7.0))
                            .bg(white(0.1))
                            .child(
                                text(11.0, 11.0, white(0.9), initial)
                                    .font_weight(FontWeight::SEMIBOLD),
                            ),
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
                                text(11.0, 16.0, white(T3), folder.display().to_string())
                                    .font_family(MONO),
                            ),
                    )
            }))
            .child(divider())
            .child(
                row("open-folder")
                    .on_click(cx.listener(Self::tell(TELL_FOLDER)))
                    .child(glyph(FOLDER, 16.0, white(0.9), scale))
                    .child("Open a folder"),
            )
    }

    fn home(&mut self, window: &mut Window, cx: &mut Context<Self>) -> AnyElement {
        let scale = window.scale_factor();
        let name = project::name(self.folder());
        let latest: SharedString = self
            .rows
            .first()
            .map_or(NO_SESSIONS.into(), |row| row.title().to_owned().into());
        let branch = match self.branch.is_empty() {
            true => NO_BRANCH.into(),
            false => self.branch.clone(),
        };
        let column = div()
            .relative()
            .flex_none()
            .w(relative(COMPOSER_SHARE))
            .min_w(px(COMPOSER_LEAST))
            .max_w_full()
            .flex()
            .flex_col()
            .gap(px(10.0))
            .text_size(px(12.5))
            .child(
                div()
                    .flex()
                    .justify_end()
                    .gap(px(16.0))
                    .child(
                        link("account", r#"<rect x="2.5" y="4" width="11" height="8" rx="1.5"/><path d="M6 14h4"/>"#, self.account.clone(), scale)
                            .on_click(cx.listener(Self::tell(TELL_ACCOUNT))),
                    )
                    .child(
                        link("project", FOLDER, name, scale)
                            .on_click(cx.listener(Self::toggle(Menu::Projects))),
                    ),
            )
            .child(self.card(scale, cx))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap(px(16.0))
                    .px(px(6.0))
                    .child(
                        link("checkout", FOLDER, CHECKOUT, scale)
                            .on_click(cx.listener(Self::tell(TELL_CHECKOUT))),
                    )
                    .child(
                        link("branch", BRANCH, branch, scale)
                            .on_click(cx.listener(Self::tell(TELL_BRANCH))),
                    )
                    .child(spacer())
                    .child(
                        div()
                            .id("resume")
                            .flex()
                            .min_w_0()
                            .items_center()
                            .gap(px(6.0))
                            .cursor_pointer()
                            .text_color(white(LINK_INK))
                            .on_click(cx.listener(Self::toggle(Menu::Resume)))
                            .child(glyph(
                                r#"<circle cx="8" cy="8" r="5.5"/><path d="M8 5v3l2 1.5"/>"#,
                                13.0,
                                white(LINK_INK),
                                scale,
                            ))
                            .child("Resume")
                            .child(
                                div()
                                    .min_w_0()
                                    .truncate()
                                    .text_color(white(T3))
                                    .child(format!("· {latest}")),
                            )
                            .child(glyph(DOWN, 13.0, white(CHEVRON_INK), scale)),
                    ),
            )
            .when(self.menu == Menu::Resume, |column| column.child(self.resume(cx)))
            .when(self.menu == Menu::Projects, |column| {
                column.child(self.projects(scale, cx))
            });
        let stage = div()
            .absolute()
            .inset_0()
            .flex()
            .flex_col()
            .items_center()
            .px(px(STAGE_PAD))
            .child(div().flex_grow(ABOVE_COMPOSER))
            .child(column)
            .child(div().flex_grow(BELOW_COMPOSER));
        hero(&self.backdrop)
            .child(stage)
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
}

impl Render for Intro {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        match self.stage {
            Stage::Home => self.home(window, cx),
            Stage::Opening { .. } | Stage::Chat => div()
                .size_full()
                .bg(hex(SURFACE))
                .child(self.tofu.chat.clone())
                .into_any_element(),
        }
    }
}
