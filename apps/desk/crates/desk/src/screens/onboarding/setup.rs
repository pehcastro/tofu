use std::sync::Arc;

use desk_core::protocol::{LoginKeyParams, LoginParams, Requirement, RequirementChoice, request};
use gpui::{
    ClickEvent, Context, Div, Entity, Focusable, FontWeight, Image, ImageFormat, Rgba,
    SharedString, Stateful, Window, div, img, prelude::*, px, relative,
};

use super::fixture::{
    CLASSIFIER_STEP, CLAUDE_SOURCE, CODEX_SOURCE, HINT, KEY_PROVIDERS, LLM_ROLE, MODEL_STEP, STEPS,
};
use super::paint::{MONO, T3, WARN, glyph, hex, medium, ringed, shadow, spacer, text, tint, white};
use super::{Onboarding, PROJECT_STEP, Sign, Slot};
use crate::project;
use crate::screens::intro::effects::glass;
use desk_ui::components::form::TextInput;

const CLAUDE: &[u8] = include_bytes!("../../../../../assets/intro/claude.svg");
const FOLDER: &[u8] = include_bytes!("../../../../../assets/intro/folder.svg");
const FOLDER_OPEN: &[u8] = include_bytes!("../../../../../assets/intro/folder-open.svg");
const TICK: &str = r#"<path d="M3.5 8.5l3 3 6-7"/>"#;
const KEY: &str = r#"<circle cx="5.5" cy="10.5" r="2.5"/><path d="M7.5 8.5l5-5M11 5l1.5 1.5"/>"#;
const ADD: Rgba = hex(0x7fd6a6);
const INK: Rgba = white(0.9);
const SHELL_SCRIM: u32 = 0x0a090e;
const SHELL_SHARE: f32 = 0.66;
const SHELL_LEAST: f32 = 400.0;
const STAGE_PAD: f32 = 16.0;
const ABOVE_SHELL: f32 = 1.0;
const BELOW_SHELL: f32 = 2.0;
const ASKING: &str = "asking tofu what is set up";

fn icon(bytes: &'static [u8]) -> gpui::Img {
    img(Arc::new(Image::from_bytes(
        ImageFormat::Svg,
        bytes.to_vec(),
    )))
    .flex_none()
    .size(px(16.0))
}

fn card() -> Div {
    div()
        .flex()
        .items_center()
        .gap(px(12.0))
        .px(px(14.0))
        .py(px(11.0))
        .rounded(px(10.0))
        .bg(tint(0x000000, 0.18))
}

fn caption(label: &str) -> Div {
    text(10.0, 10.0, white(0.45), label.to_uppercase()).font_weight(FontWeight::SEMIBOLD)
}

fn button(
    id: impl Into<gpui::ElementId>,
    label: impl Into<SharedString>,
    primary: bool,
    height: f32,
) -> Stateful<Div> {
    div()
        .id(id.into())
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .h(px(height))
        .px(px(if primary { 16.0 } else { 12.0 }))
        .rounded(px(8.0))
        .cursor_pointer()
        .text_size(px(13.0))
        .font_weight(FontWeight::MEDIUM)
        .when(primary, |b| b.bg(white(0.9)).text_color(hex(0x111111)))
        .when(!primary, |b| b.bg(white(0.07)))
        .child(label.into())
}

fn two_lines(title: impl Into<SharedString>, sub: Div) -> Div {
    div()
        .flex_1()
        .min_w_0()
        .flex()
        .flex_col()
        .line_height(px(19.0))
        .child(title.into())
        .child(sub.whitespace_normal())
}

fn badge(label: impl Into<SharedString>) -> Div {
    text(11.5, 16.0, hex(0x86e0b3), label)
        .px(px(9.0))
        .py(px(2.0))
        .rounded_full()
        .bg(tint(0x86e0b3, 0.12))
}

fn quiet(said: impl Into<SharedString>) -> Div {
    text(12.5, 18.0, white(T3), said).whitespace_normal()
}

fn refusal(sign: &Sign) -> Option<Div> {
    match sign {
        Sign::Refused(said) => Some(text(12.5, 18.0, WARN, said.clone()).whitespace_normal()),
        Sign::Idle | Sign::Waiting | Sign::Signed(_) => None,
    }
}

fn step<'a>(setup: &'a [Requirement], name: &str) -> Option<&'a Requirement> {
    setup.iter().find(|step| step.step == name)
}

fn done(step: Option<&Requirement>) -> Option<&str> {
    step.and_then(|step| step.done.as_deref())
        .filter(|done| !done.is_empty())
}

fn keyed(step: Option<&Requirement>) -> Vec<&RequirementChoice> {
    step.map(|step| {
        step.choices
            .iter()
            .filter(|choice| choice.key.is_some())
            .collect()
    })
    .unwrap_or_default()
}

fn provider(choice: &RequirementChoice) -> Option<&'static str> {
    let variable = choice.key.as_deref()?;
    KEY_PROVIDERS
        .iter()
        .find(|(known, _)| *known == variable)
        .map(|(_, provider)| *provider)
}

fn sessions(count: usize) -> String {
    match count {
        0 => "no sessions found".to_owned(),
        1 => "1 session found".to_owned(),
        count => format!("{count} sessions found"),
    }
}

impl Onboarding {
    fn steps(&self) -> Result<&[Requirement], &str> {
        match &self.setup {
            None => Err(ASKING),
            Some(Ok(steps)) => Ok(steps),
            Some(Err(reason)) => Err(reason),
        }
    }

    fn connected(&self) -> bool {
        let done_by_tofu = self
            .steps()
            .is_ok_and(|steps| done(step(steps, MODEL_STEP)).is_some());
        let signed = [&self.signs.claude, &self.signs.codex, &self.signs.key]
            .iter()
            .any(|sign| matches!(sign, Sign::Signed(_)));
        done_by_tofu || signed
    }

    fn sign_in(
        &mut self,
        slot: Slot,
        source: &'static str,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        self.waiting(slot, window, cx);
        let params = LoginParams {
            provider: source.to_owned(),
            role: LLM_ROLE.to_owned(),
        };
        eprintln!("desk: onboarding login.start {source}");
        self.ask::<request::LoginStart>(&params, cx, move |this, started, cx| {
            this.landed(slot, started.map(|started| started.account), cx);
        });
    }

    fn save_key(
        &mut self,
        slot: Slot,
        provider: &'static str,
        field: &Entity<TextInput>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let key = field.read(cx).text().trim().to_owned();
        if key.is_empty() {
            *self.sign(slot) = Sign::Refused("paste a key first".to_owned());
            return cx.notify();
        }
        self.waiting(slot, window, cx);
        let params = LoginKeyParams {
            provider: provider.to_owned(),
            key,
        };
        eprintln!("desk: onboarding login.key {provider}");
        self.ask::<request::LoginKey>(&params, cx, move |this, saved, cx| {
            this.landed(slot, saved.map(|saved| saved.note), cx);
        });
    }

    fn tabs(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        div()
            .h(px(36.0))
            .flex()
            .items_end()
            .gap(px(2.0))
            .pl(px(9.0))
            .pr(px(6.0))
            .pb(px(1.0))
            .children(STEPS.iter().enumerate().map(|(index, label)| {
                let on = index == self.step;
                div()
                    .id(("step", index))
                    .h(px(30.0))
                    .pl(px(10.0))
                    .pr(px(9.0))
                    .flex()
                    .items_center()
                    .gap(px(7.0))
                    .rounded(px(8.0))
                    .cursor_pointer()
                    .text_size(px(12.5))
                    .font_weight(FontWeight::MEDIUM)
                    .text_color(white(if on { 1.0 } else { 0.5 }))
                    .when(on, |tab| tab.bg(white(0.055)))
                    .on_click(cx.listener(move |this, _: &ClickEvent, window, cx| {
                        if index <= this.step {
                            this.go(index, window, cx);
                        }
                    }))
                    .child(text(11.0, 12.0, white(T3), (index + 1).to_string()).font_family(MONO))
                    .child(*label)
                    .when(index < self.step, |tab| {
                        tab.child(glyph(TICK, 13.0, ADD, scale))
                    })
            }))
            .child(spacer())
            .child(
                text(12.0, 12.0, white(T3), HINT)
                    .flex_shrink_1()
                    .min_w_0()
                    .truncate()
                    .pb(px(8.0)),
            )
    }

    fn signed_in(&self, slot: Slot, source: &'static str, cx: &mut Context<Self>) -> Div {
        let id = SharedString::from(format!("sign-in-{source}"));
        let ask = |label: &'static str, cx: &mut Context<Self>| {
            button(id.clone(), label, false, 28.0).on_click(cx.listener(
                move |this, _: &ClickEvent, window, cx| this.sign_in(slot, source, window, cx),
            ))
        };
        match self.shown(slot) {
            Sign::Idle => div().child(ask("Sign in", cx)),
            Sign::Waiting => div().child(quiet("waiting for the browser")),
            Sign::Signed(account) => div().child(badge(account.clone())),
            Sign::Refused(_) => div().child(ask("Try again", cx)),
        }
    }

    fn models(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        let steps = self.steps();
        let model = steps.ok().and_then(|steps| step(steps, MODEL_STEP));
        let status = match (steps, done(model)) {
            (Err(reason), _) => reason.to_owned(),
            (Ok(_), Some(done)) => format!("tofu has {done}"),
            (Ok(_), None) => "nothing connected yet".to_owned(),
        };
        let choices = keyed(model);
        let provider_at = choices.get(self.provider).copied();
        let mark = |fill: Rgba| {
            div()
                .flex()
                .flex_none()
                .items_center()
                .justify_center()
                .size(px(30.0))
                .rounded(px(9.0))
                .bg(fill)
        };
        let subscription = |mark: Div,
                            name: &'static str,
                            line: &'static str,
                            slot: Slot,
                            source: &'static str,
                            cx: &mut Context<Self>| {
            div()
                .flex()
                .flex_col()
                .gap(px(6.0))
                .child(
                    card()
                        .child(mark)
                        .child(two_lines(name, quiet(line)))
                        .child(self.signed_in(slot, source, cx)),
                )
                .children(refusal(self.shown(slot)).map(|said| said.pl(px(14.0))))
        };
        let key_badge = match &self.signs.key {
            Sign::Signed(note) => Some(badge(note.clone())),
            Sign::Idle | Sign::Waiting | Sign::Refused(_) => None,
        };
        div()
            .flex()
            .flex_col()
            .gap(px(14.0))
            .child(
                div()
                    .flex()
                    .items_end()
                    .gap(px(12.0))
                    .child(text(18.0, 23.0, INK, "How do you pay for models?").font_weight(FontWeight::SEMIBOLD))
                    .child(spacer())
                    .child(text(12.5, 23.0, white(T3), status).truncate()),
            )
            .child(caption("Subscriptions"))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(6.0))
                    .child(subscription(
                        mark(tint(0xd97757, 0.16)).child(icon(CLAUDE)),
                        "Claude",
                        "Pro or Max · signs in through your browser · claude-sub",
                        Slot::Claude,
                        CLAUDE_SOURCE,
                        cx,
                    ))
                    .child(subscription(
                        mark(white(0.08)).child(text(13.0, 13.0, INK, "C").font_weight(FontWeight::BOLD)),
                        "ChatGPT",
                        "Plus or Pro · signs in through your browser · codex-sub",
                        Slot::Codex,
                        CODEX_SOURCE,
                        cx,
                    )),
            )
            .child(caption("Keys"))
            .child(
                card()
                    .flex_col()
                    .items_stretch()
                    .gap(px(10.0))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap(px(12.0))
                            .child(mark(white(0.08)).child(glyph(KEY, 13.0, white(1.0), scale)))
                            .child(two_lines("An API key", quiet("paid per token, checked by tofu with one call")))
                            .children(key_badge)
                            .child(button("add-key", "Add a key", false, 28.0).on_click(cx.listener(|this, _: &ClickEvent, window, cx| {
                                this.key_open = !this.key_open;
                                if this.key_open {
                                    window.focus(&this.key_field.focus_handle(cx), cx);
                                }
                                cx.notify();
                            }))),
                    )
                    .when(self.key_open, |key| {
                        let body = div().flex().flex_col().gap(px(10.0)).pl(px(42.0));
                        let body = match provider_at.and_then(provider) {
                            None if choices.is_empty() => body.child(quiet(match steps {
                                Err(reason) => reason.to_owned(),
                                Ok(_) => "tofu offers no key for models here".to_owned(),
                            })),
                            None => body.child(quiet(format!(
                                "this desk does not know which provider stores {}",
                                provider_at.and_then(|choice| choice.key.as_deref()).unwrap_or_default()
                            ))),
                            Some(provider) => body
                                .child(
                                    div()
                                        .flex()
                                        .gap(px(2.0))
                                        .p(px(2.0))
                                        .rounded(px(9.0))
                                        .bg(white(0.05))
                                        .text_size(px(12.5))
                                        .w_auto()
                                        .self_start()
                                        .children(choices.iter().enumerate().map(|(index, choice)| {
                                            let on = index == self.provider;
                                            div()
                                                .id(("provider", index))
                                                .px(px(11.0))
                                                .py(px(4.0))
                                                .rounded(px(7.0))
                                                .cursor_pointer()
                                                .text_color(white(if on { 1.0 } else { 0.5 }))
                                                .when(on, |tab| tab.bg(white(0.12)))
                                                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                                                    this.provider = index;
                                                    cx.notify();
                                                }))
                                                .child(choice.label.clone())
                                        })),
                                )
                                .child(
                                    div()
                                        .flex()
                                        .items_center()
                                        .gap(px(8.0))
                                        .child(self.key_field.clone())
                                        .child(match self.signs.key {
                                            Sign::Waiting => div().child(quiet("checking")),
                                            Sign::Idle | Sign::Signed(_) | Sign::Refused(_) => div().child(
                                                button("save-key", "Check and save", true, 30.0).px(px(12.0)).on_click(cx.listener(
                                                    move |this, _: &ClickEvent, window, cx| {
                                                        let field = this.key_field.clone();
                                                        this.save_key(Slot::Key, provider, &field, window, cx);
                                                    },
                                                )),
                                            ),
                                        }),
                                )
                                .children(refusal(&self.signs.key))
                                .child(quiet("tofu checks it with one call and stores it; the desk never prints it")),
                        };
                        key.child(body)
                    }),
            )
    }

    fn classifiers(&self, cx: &mut Context<Self>) -> Div {
        let steps = self.steps();
        let classifier = steps.ok().and_then(|steps| step(steps, CLASSIFIER_STEP));
        let choices = keyed(classifier);
        let header = div()
            .flex()
            .flex_col()
            .child(
                div()
                    .flex()
                    .items_end()
                    .gap(px(12.0))
                    .child(text(18.0, 23.0, INK, "Pick a classifier").font_weight(FontWeight::SEMIBOLD))
                    .child(spacer())
                    .children(done(classifier).map(|done| badge(done.to_owned()))),
            )
            .child(
                div()
                    .mt(px(7.0))
                    .text_size(px(13.5))
                    .line_height(px(21.0))
                    .text_color(white(0.85))
                    .child("A small model tofu asks at fixed points: may this command run, is this output worth reading, is the work really done.")
                    .child("It answers in milliseconds, so the lead does not have to."),
            );
        let list = match (steps, choices.is_empty()) {
            (Err(reason), _) => quiet(reason.to_owned()),
            (Ok(_), true) => quiet("tofu offers no classifier choice here"),
            (Ok(_), false) => {
                div()
                    .flex()
                    .flex_col()
                    .gap(px(6.0))
                    .children(choices.iter().enumerate().map(|(index, choice)| {
                        let on = index == self.classifier;
                        let ring = |size: f32, fill: Rgba| {
                            div()
                                .flex()
                                .flex_none()
                                .items_center()
                                .justify_center()
                                .size(px(size))
                                .rounded_full()
                                .bg(fill)
                        };
                        let dot = if on {
                            ring(17.0, white(1.0))
                                .m(px(-1.5))
                                .child(ring(14.0, hex(0x17161c)).child(ring(8.0, white(1.0))))
                        } else {
                            div()
                                .flex_none()
                                .size(px(14.0))
                                .rounded_full()
                                .border(px(1.5))
                                .border_color(white(0.4))
                        };
                        let variable = choice.key.clone().unwrap_or_default();
                        let checked = provider(choice);
                        ringed(10.0, if on { 0.55 } else { 0.08 })
                            .flex()
                            .flex_col()
                            .bg(tint(0x000000, 0.18))
                            .when(on, |card| card.bg(white(0.06)))
                            .child(
                                div()
                                    .id(("classifier", index))
                                    .flex()
                                    .items_center()
                                    .gap(px(12.0))
                                    .px(px(14.0))
                                    .py(px(11.0))
                                    .cursor_pointer()
                                    .on_click(cx.listener(
                                        move |this, _: &ClickEvent, window, cx| {
                                            this.classifier = index;
                                            this.signs.classifier = Sign::Idle;
                                            window
                                                .focus(&this.classifier_field.focus_handle(cx), cx);
                                            cx.notify();
                                        },
                                    ))
                                    .child(dot)
                                    .child(two_lines(
                                        choice.label.clone(),
                                        text(12.5, 19.0, white(T3), variable.clone())
                                            .font_family(MONO),
                                    )),
                            )
                            .when(on, |card| {
                                let row = div()
                                    .flex()
                                    .flex_col()
                                    .gap(px(8.0))
                                    .pl(px(40.0))
                                    .pr(px(14.0))
                                    .pb(px(12.0));
                                let row = match checked {
                                None => row.child(quiet(format!(
                                    "this desk does not know which provider stores {variable}"
                                ))),
                                Some(provider) => row
                                    .child(
                                        div()
                                            .flex()
                                            .items_center()
                                            .gap(px(8.0))
                                            .child(self.classifier_field.clone())
                                            .child(match &self.signs.classifier {
                                                Sign::Waiting => div().child(quiet("checking")),
                                                Sign::Signed(note) => {
                                                    div().child(badge(note.clone()))
                                                }
                                                Sign::Idle | Sign::Refused(_) => div().child(
                                                    button("check", "Check", false, 30.0)
                                                        .on_click(cx.listener(
                                                        move |this, _: &ClickEvent, window, cx| {
                                                            let field =
                                                                this.classifier_field.clone();
                                                            this.save_key(
                                                                Slot::Classifier,
                                                                provider,
                                                                &field,
                                                                window,
                                                                cx,
                                                            );
                                                        },
                                                    )),
                                                ),
                                            }),
                                    )
                                    .children(refusal(&self.signs.classifier)),
                            };
                                card.child(row)
                            })
                    }))
            }
        };
        div()
            .flex()
            .flex_col()
            .gap(px(12.0))
            .child(header)
            .child(list)
    }

    fn found(&self, scale: f32) -> Option<Div> {
        let at = self.projects.picked?;
        let folder = self.projects.rows.get(at)?;
        let listed = self
            .projects
            .listed
            .iter()
            .find(|listed| listed.folder == *folder);
        let git =
            self.projects
                .head
                .as_ref()
                .map(|head| match (head.branch.as_str(), head.changed) {
                    ("", _) => "no git repository".to_owned(),
                    (branch, 0) => format!("git {branch} · clean"),
                    (branch, changed) => format!("git {branch} · {changed} changed"),
                });
        let sessions = listed.map(|listed| match (listed.missing, listed.sessions) {
            (true, _) => "the folder is missing".to_owned(),
            (false, Some(count)) => sessions(count),
            (false, None) => "tofu did not list its sessions".to_owned(),
        });
        let chips: Vec<String> = git.into_iter().chain(sessions).collect();
        Some(
            div()
                .flex()
                .flex_wrap()
                .gap(px(8.0))
                .children(chips.into_iter().map(|found| {
                    div()
                        .flex()
                        .items_center()
                        .gap(px(6.0))
                        .h(px(24.0))
                        .px(px(9.0))
                        .rounded(px(7.0))
                        .bg(white(0.07))
                        .child(glyph(TICK, 13.0, ADD, scale))
                        .child(medium(12.5, 12.5, white(0.85), found))
                })),
        )
    }

    fn projects(&self, scale: f32, cx: &mut Context<Self>) -> Div {
        div()
            .flex()
            .flex_col()
            .gap(px(12.0))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .child(text(18.0, 23.0, INK, "Pick a project").font_weight(FontWeight::SEMIBOLD))
                    .child(
                        text(13.5, 23.0, white(0.85), "A folder to work in. tofu reads it for git and past sessions; it never commits.")
                            .whitespace_normal()
                            .mt(px(6.0)),
                    ),
            )
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap(px(6.0))
                    .children(self.projects.rows.iter().enumerate().map(|(index, folder)| {
                        let on = self.projects.picked == Some(index);
                        let listed = self.projects.listed.iter().find(|listed| listed.folder == *folder);
                        let below = folder.display().to_string();
                        let below = match listed {
                            None => below,
                            Some(listed) if listed.missing => format!("{below} · missing"),
                            Some(listed) => [
                                Some(below),
                                listed.branch.clone(),
                                listed.sessions.map(|count| match count {
                                    1 => "1 session".to_owned(),
                                    count => format!("{count} sessions"),
                                }),
                            ]
                            .into_iter()
                            .flatten()
                            .collect::<Vec<_>>()
                            .join(" · "),
                        };
                        ringed(10.0, if on { 0.55 } else { 0.08 })
                            .id(("pick", index))
                            .cursor_pointer()
                            .flex()
                            .items_center()
                            .gap(px(12.0))
                            .px(px(14.0))
                            .py(px(11.0))
                            .bg(tint(0x000000, 0.18))
                            .when(on, |card| card.bg(white(0.06)))
                            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| this.pick(index, cx)))
                            .child(icon(FOLDER))
                            .child(two_lines(project::name(folder), text(11.5, 19.0, white(T3), below).font_family(MONO)))
                    }))
                    .child(
                        ringed(10.0, 0.08)
                            .id("open-folder")
                            .cursor_pointer()
                            .flex()
                            .items_center()
                            .gap(px(12.0))
                            .px(px(14.0))
                            .py(px(11.0))
                            .line_height(px(23.0))
                            .on_click(cx.listener(|this, _: &ClickEvent, window, cx| this.browse(window, cx)))
                            .child(icon(FOLDER_OPEN))
                            .child(div().text_color(white(0.85)).child("Open another folder")),
                    ),
            )
            .children(self.found(scale))
    }

    fn footer(&self, cx: &mut Context<Self>) -> Div {
        let blocked = self.step == 1 && !self.connected();
        let picked = self
            .projects
            .picked
            .and_then(|at| self.projects.rows.get(at))
            .map(|folder| project::name(folder));
        div()
            .flex()
            .items_center()
            .gap(px(10.0))
            .pt(px(6.0))
            .border_t_1()
            .border_color(white(0.05))
            .child(button("back", "Back", false, 30.0).on_click(cx.listener(
                |this, _: &ClickEvent, window, cx| this.go(this.step.saturating_sub(1), window, cx),
            )))
            .when(blocked, |row| {
                row.child(text(12.5, 18.0, white(T3), "connect one to continue"))
            })
            .child(spacer())
            .when(self.step == PROJECT_STEP, |row| {
                row.children(picked.map(|name| {
                    button("open", format!("Open {name}"), true, 30.0).on_click(
                        cx.listener(|this, _: &ClickEvent, window, cx| this.open_work(window, cx)),
                    )
                }))
            })
            .when(!blocked && self.step < PROJECT_STEP, |row| {
                row.child(
                    button("continue", "Continue", true, 30.0).on_click(cx.listener(
                        |this, _: &ClickEvent, window, cx| this.go(this.step + 1, window, cx),
                    )),
                )
            })
    }

    pub(super) fn setup(&self, scale: f32, cx: &mut Context<Self>) -> Stateful<Div> {
        let body = match self.step {
            1 => self.models(scale, cx),
            2 => self.classifiers(cx),
            _ => self.projects(scale, cx),
        };
        div()
            .id("setup")
            .absolute()
            .inset_0()
            .overflow_y_scroll()
            .flex()
            .flex_col()
            .items_center()
            .p(px(STAGE_PAD))
            .child(div().flex_grow(ABOVE_SHELL))
            .child(
                div()
                    .relative()
                    .flex_none()
                    .rounded(px(12.0))
                    .overflow_hidden()
                    .w(relative(SHELL_SHARE))
                    .min_w(px(SHELL_LEAST))
                    .max_w_full()
                    .flex()
                    .flex_col()
                    .px(px(3.0))
                    .pb(px(3.0))
                    .shadow(shadow(45.0, 30.0, 0.5))
                    .child(glass(12.0, SHELL_SCRIM, 0.76, 0.88))
                    .child(ringed(12.0, 0.1).absolute().inset_0())
                    .text_size(px(13.0))
                    .text_color(INK)
                    .child(self.tabs(scale, cx))
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .gap(px(18.0))
                            .min_h(px(360.0))
                            .pt(px(22.0))
                            .px(px(24.0))
                            .pb(px(16.0))
                            .rounded(px(11.0))
                            .child(body)
                            .child(div().flex_1())
                            .child(self.footer(cx)),
                    ),
            )
            .child(div().flex_grow(BELOW_SHELL))
    }
}
