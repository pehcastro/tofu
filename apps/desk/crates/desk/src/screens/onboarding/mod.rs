mod fixture;
mod setup;

use std::path::{Path, PathBuf};
use std::time::Instant;
use std::{env, thread};

use desk_core::bridge::{Bridge, BridgeError, serve_command};
use desk_core::protocol::{NoParams, PROTOCOL, Request, Requirement, request};
use desk_ui::components::form::TextInput;
use desk_ui::effects::EffectParams;
use gpui::{
    AnyView, App, AppContext, ClickEvent, Context, Entity, FontWeight, IntoElement, Render,
    SharedString, Window, div, prelude::*, px,
};

use super::intro::effects::{Backdrop, WELCOME, backdrop, hero};
use super::intro::paint;
use crate::project::{self, Head, Listed};
use fixture::{IMPORT, KEY_PLACEHOLDER, START, TAGLINE, TELL_IMPORT, TITLE};
use paint::{hex, load_fonts, shadow, white};

const CLIENT: &str = "tofu-desk-onboarding";
const STAGE_PAD: f32 = 16.0;
const ABOVE_WELCOME: f32 = 7.0;
const BELOW_WELCOME: f32 = 3.0;
const PROJECT_STEP: usize = 3;

enum Link {
    Starting,
    Ready(Bridge),
    Down(String),
}

enum Sign {
    Idle,
    Waiting,
    Signed(String),
    Refused(String),
}

#[derive(Clone, Copy)]
enum Slot {
    Claude,
    Codex,
    Key,
    Classifier,
}

struct Signs {
    claude: Sign,
    codex: Sign,
    key: Sign,
    classifier: Sign,
}

struct Projects {
    rows: Vec<PathBuf>,
    listed: Vec<Listed>,
    picked: Option<usize>,
    head: Option<Head>,
}

struct Onboarding {
    backdrop: Entity<Backdrop>,
    step: usize,
    link: Link,
    setup: Option<Result<Vec<Requirement>, String>>,
    signs: Signs,
    key_open: bool,
    key_field: Entity<TextInput>,
    classifier_field: Entity<TextInput>,
    provider: usize,
    classifier: usize,
    projects: Projects,
    told: Option<SharedString>,
}

pub fn open(board: Option<&str>, window: &mut Window, cx: &mut App) -> Result<AnyView, String> {
    load_fonts(cx)?;
    let step = match board {
        None | Some("IONB-1") => 0,
        Some("S-ONB-1") => 1,
        Some("S-ONB-2") => 2,
        Some("S-ONB-3") => PROJECT_STEP,
        Some(other) => {
            return Err(format!(
                "the onboarding draws IONB-1 and S-ONB-1 to 3, not {other}"
            ));
        }
    };
    let backdrop = backdrop(WELCOME, EffectParams::default(), cx);
    let key_field = TextInput::new(KEY_PLACEHOLDER.into(), window, cx);
    let classifier_field = TextInput::new(KEY_PLACEHOLDER.into(), window, cx);
    let view = cx.new(|cx| {
        let mut onboarding = Onboarding {
            backdrop,
            step: 0,
            link: Link::Starting,
            setup: None,
            signs: Signs {
                claude: Sign::Idle,
                codex: Sign::Idle,
                key: Sign::Idle,
                classifier: Sign::Idle,
            },
            key_open: false,
            key_field,
            classifier_field,
            provider: 0,
            classifier: 0,
            projects: Projects {
                rows: Vec::new(),
                listed: Vec::new(),
                picked: None,
                head: None,
            },
            told: None,
        };
        onboarding.connect(cx);
        onboarding.go(step, window, cx);
        onboarding
    });
    Ok(view.into())
}

fn said(error: BridgeError) -> String {
    match error {
        BridgeError::Refused(refusal) => refusal.message,
        other => other.to_string(),
    }
}

impl Onboarding {
    fn connect(&mut self, cx: &mut Context<Self>) {
        let (sender, opened) = flume::bounded(1);
        thread::spawn(move || {
            let bridge = Bridge::open(
                serve_command(&project::tofu(), Path::new(".")),
                CLIENT,
                PROTOCOL,
            );
            sender.send(bridge).ok();
        });
        cx.spawn(async move |this, cx| {
            let link = match opened.recv_async().await {
                Ok(Ok((bridge, _))) => Link::Ready(bridge),
                Ok(Err(error)) => Link::Down(error.to_string()),
                Err(_) => Link::Down("tofu did not start".to_owned()),
            };
            let landed = this.update(cx, |this, cx| {
                if let Link::Down(reason) = &link {
                    eprintln!("desk: onboarding: tofu is not running: {reason}");
                }
                this.link = link;
                this.check(cx);
            });
            if let Err(error) = landed {
                eprintln!("desk: onboarding closed before tofu started: {error}");
            }
        })
        .detach();
    }

    fn ask<R: Request>(
        &mut self,
        params: &R::Params,
        cx: &mut Context<Self>,
        then: impl FnOnce(&mut Self, Result<R::Result, String>, &mut Context<Self>) + 'static,
    ) where
        R::Result: 'static,
    {
        let pending = match &self.link {
            Link::Ready(bridge) => bridge.request::<R>(params).map_err(said),
            Link::Starting => Err("tofu is still starting; try again in a moment".to_owned()),
            Link::Down(reason) => Err(reason.clone()),
        };
        let pending = match pending {
            Ok(pending) => pending,
            Err(reason) => return then(self, Err(reason), cx),
        };
        cx.spawn(async move |this, cx| {
            let reply = pending.reply().await.map_err(said);
            let landed = this.update(cx, |this, cx| {
                then(this, reply, cx);
                cx.notify();
            });
            if let Err(error) = landed {
                eprintln!(
                    "desk: onboarding closed before {} answered: {error}",
                    R::METHOD
                );
            }
        })
        .detach();
    }

    fn check(&mut self, cx: &mut Context<Self>) {
        self.ask::<request::SetupCheck>(&NoParams {}, cx, |this, setup, _| {
            if let Err(reason) = &setup {
                eprintln!("desk: onboarding: setup.check: {reason}");
            }
            this.setup = Some(setup.map(|setup| setup.steps));
        });
    }

    fn shown(&self, slot: Slot) -> &Sign {
        match slot {
            Slot::Claude => &self.signs.claude,
            Slot::Codex => &self.signs.codex,
            Slot::Key => &self.signs.key,
            Slot::Classifier => &self.signs.classifier,
        }
    }

    fn sign(&mut self, slot: Slot) -> &mut Sign {
        match slot {
            Slot::Claude => &mut self.signs.claude,
            Slot::Codex => &mut self.signs.codex,
            Slot::Key => &mut self.signs.key,
            Slot::Classifier => &mut self.signs.classifier,
        }
    }

    fn landed(&mut self, slot: Slot, answer: Result<String, String>, cx: &mut Context<Self>) {
        let signed = answer.is_ok();
        *self.sign(slot) = match answer {
            Ok(said) => Sign::Signed(said),
            Err(refusal) => Sign::Refused(refusal),
        };
        if signed {
            self.check(cx);
        }
    }

    fn waiting(&mut self, slot: Slot, window: &mut Window, cx: &mut Context<Self>) {
        let clicked = Instant::now();
        *self.sign(slot) = Sign::Waiting;
        cx.notify();
        window.on_next_frame(move |_, _| {
            eprintln!(
                "desk: onboarding waiting {} ms",
                clicked.elapsed().as_millis()
            );
        });
    }

    fn go(&mut self, step: usize, window: &mut Window, cx: &mut Context<Self>) {
        self.step = step;
        if step == PROJECT_STEP && self.projects.rows.is_empty() {
            self.list(window, cx);
        }
        cx.notify();
    }

    fn list(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let started = Instant::now();
        let current = env::current_dir().map_err(|error| error.to_string());
        let launched = project::launched().map(|(folder, _)| folder);
        let recents = project::recents();
        let mut rows: Vec<PathBuf> = Vec::new();
        let found = [
            current.map(|folder| vec![folder]),
            launched.map(|folder| vec![folder]),
            recents,
        ];
        for folders in found {
            match folders {
                Ok(folders) => folders.into_iter().for_each(|folder| {
                    if !rows.contains(&folder) {
                        rows.push(folder);
                    }
                }),
                Err(error) => eprintln!("desk: onboarding: {error}"),
            }
        }
        self.projects.rows = rows.clone();
        let count = rows.len();
        window.on_next_frame(move |_, _| {
            eprintln!(
                "desk: onboarding project {} ms",
                started.elapsed().as_millis()
            );
            eprintln!("desk: onboarding project painted {count} rows");
        });
        self.read_listed(rows, started, cx);
    }

    fn read_listed(&mut self, folders: Vec<PathBuf>, started: Instant, cx: &mut Context<Self>) {
        let (sender, arrived) = flume::unbounded();
        for folder in folders {
            let sender = sender.clone();
            thread::spawn(move || sender.send(project::listed(folder)).ok());
        }
        drop(sender);
        cx.spawn(async move |this, cx| {
            let mut read = 0;
            while let Ok(listed) = arrived.recv_async().await {
                read += usize::from(listed.sessions.is_some());
                let landed = this.update(cx, |this, cx| {
                    this.projects.listed.push(listed);
                    cx.notify();
                });
                if let Err(error) = landed {
                    return eprintln!(
                        "desk: onboarding closed before its projects were read: {error}"
                    );
                }
            }
            eprintln!(
                "desk: onboarding project-filled {} ms",
                started.elapsed().as_millis()
            );
            eprintln!("desk: onboarding project sessions read in {read} folders");
        })
        .detach();
    }

    fn pick(&mut self, at: usize, cx: &mut Context<Self>) {
        self.projects.picked = Some(at);
        self.projects.head = None;
        let Some(folder) = self.projects.rows.get(at).cloned() else {
            return;
        };
        cx.spawn(async move |this, cx| {
            let head = cx
                .background_executor()
                .spawn(async move { project::head(folder) })
                .await;
            let landed = this.update(cx, |this, cx| {
                let still = this
                    .projects
                    .picked
                    .and_then(|at| this.projects.rows.get(at))
                    .is_some_and(|picked| *picked == head.folder);
                if still {
                    this.projects.head = Some(head);
                    cx.notify();
                }
            });
            if let Err(error) = landed {
                eprintln!("desk: onboarding closed before git answered: {error}");
            }
        })
        .detach();
        cx.notify();
    }

    fn add_folder(&mut self, folder: PathBuf, cx: &mut Context<Self>) {
        let at = match self.projects.rows.iter().position(|row| *row == folder) {
            Some(at) => at,
            None => {
                self.projects.rows.insert(0, folder.clone());
                self.projects.picked = None;
                self.read_listed(vec![folder], Instant::now(), cx);
                0
            }
        };
        self.pick(at, cx);
    }

    fn browse(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let asked = cx.prompt_for_paths(gpui::PathPromptOptions {
            files: false,
            directories: true,
            multiple: false,
            prompt: Some("Open project".into()),
        });
        cx.spawn_in(window, async move |this, cx| {
            let folder = match asked.await {
                Ok(Ok(Some(mut picked))) => picked.pop(),
                Ok(Ok(None)) => None,
                Ok(Err(error)) => {
                    eprintln!("desk: onboarding: the folder dialog failed: {error}");
                    None
                }
                Err(_) => {
                    eprintln!("desk: onboarding: the folder dialog closed without an answer");
                    None
                }
            };
            let Some(folder) = folder else {
                return eprintln!("desk: onboarding: no folder was picked");
            };
            if let Err(error) = this.update(cx, |this, cx| this.add_folder(folder, cx)) {
                eprintln!("desk: onboarding closed before the folder was picked: {error}");
            }
        })
        .detach();
    }

    fn open_work(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(folder) = self
            .projects
            .picked
            .and_then(|at| self.projects.rows.get(at))
            .cloned()
        else {
            return;
        };
        if let Err(error) = project::remember(&folder) {
            self.told = Some(error.into());
            return cx.notify();
        }
        eprintln!("desk: onboarding remembered {}", folder.display());
        let Some(Some(desk)) = window.root::<crate::desk::Desk>() else {
            self.told =
                Some("the onboarding is not inside the desk, so it cannot open work".into());
            return cx.notify();
        };
        let me = cx.entity().downgrade();
        window.defer(cx, move |window, cx| {
            let opened = desk.update(cx, |desk, cx| desk.open_screen("work", window, cx));
            if let Err(error) = opened {
                eprintln!("desk: onboarding: {error}");
                if let Err(gone) = me.update(cx, |this, cx| {
                    this.told = Some(error.into());
                    cx.notify();
                }) {
                    eprintln!("desk: onboarding closed before work refused: {gone}");
                }
            }
        });
    }

    fn tell(
        message: &'static str,
    ) -> impl Fn(&mut Self, &ClickEvent, &mut Window, &mut Context<Self>) {
        move |this, _, _, cx| {
            this.told = Some(message.into());
            cx.notify();
        }
    }

    fn welcome(cx: &mut Context<Self>) -> gpui::Div {
        div()
            .absolute()
            .inset_0()
            .flex()
            .flex_col()
            .items_center()
            .px(px(STAGE_PAD))
            .gap(px(18.0))
            .text_center()
            .child(div().flex_grow(ABOVE_WELCOME))
            .child(
                div()
                    .text_size(px(96.0))
                    .line_height(px(96.0))
                    .font_weight(FontWeight::BOLD)
                    .text_color(white(1.0))
                    .child(TITLE),
            )
            .child(
                div()
                    .max_w(px(560.0))
                    .text_size(px(17.0))
                    .line_height(px(26.0))
                    .text_color(white(0.78))
                    .child(TAGLINE),
            )
            .child(
                div()
                    .id("start")
                    .mt(px(10.0))
                    .h(px(42.0))
                    .px(px(26.0))
                    .flex()
                    .items_center()
                    .rounded(px(21.0))
                    .bg(white(0.92))
                    .shadow(shadow(40.0, 10.0, 0.35))
                    .text_color(hex(0x111111))
                    .text_size(px(14.0))
                    .font_weight(FontWeight::SEMIBOLD)
                    .cursor_pointer()
                    .on_click(
                        cx.listener(|this, _: &ClickEvent, window, cx| this.go(1, window, cx)),
                    )
                    .child(START),
            )
            .child(
                div()
                    .id("import")
                    .text_size(px(12.5))
                    .text_color(white(0.55))
                    .cursor_pointer()
                    .on_click(cx.listener(Self::tell(TELL_IMPORT)))
                    .child(IMPORT),
            )
            .child(div().flex_grow(BELOW_WELCOME))
    }
}

impl Render for Onboarding {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let scale = window.scale_factor();
        let over = if self.step == 0 {
            Self::welcome(cx).into_any_element()
        } else {
            self.setup(scale, cx).into_any_element()
        };
        hero(&self.backdrop)
            .child(over)
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
    }
}
