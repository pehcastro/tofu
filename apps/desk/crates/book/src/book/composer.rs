use std::time::Duration;

use desk_ui::components::button::{ButtonKind, button};
use desk_ui::components::card::inner_card;
use desk_ui::components::composer::{
    Composer, ComposerVariant, HomeComposer, HomeProject, HomeSession, SessionStatus,
};
use desk_ui::components::form::TextArea;
use desk_ui::components::overlay::{Dropdown, DropdownTrigger};
use desk_ui::theme::{ColorToken, Theme};
use gpui::{
    ClickEvent, Context, Div, Entity, Result, SharedString, Task, Window, div, prelude::*, px,
};

use super::Book;
use super::kit::{block, efforts, models, named, spread};

const HOME_ASK: &str = "Do anything in tofu";
const HOME_WIDTH: f32 = 740.0;
const HOME_GAP: f32 = 10.0;
const HOME_ACCOUNT: &str = "claude-sub · personal";
const HOME_MODEL: &str = "claude-sub/claude-opus-5";
const HOME_EFFORT: &str = "high";
const HOME_CURRENT: usize = 1;
const ASK: &str = "Ask the lead, Enter sends, Enter during a turn queues";
const AREA_LINES: usize = 8;
const COMPOSER_WIDTH: f32 = 560.0;
const TURN: Duration = Duration::from_secs(3);
const PHASE: &str = "running 2 sub-agents";
const ATTACHED: [&str; 2] = ["internal/turn/loop.go", "loop_test.go"];
const TRACED: [&str; 2] = ["turn 14", "go-dev / TOFU-612"];

fn shared(names: &[&str]) -> Vec<SharedString> {
    names.iter().map(|&name| SharedString::from(name)).collect()
}

fn session(name: &str, prompt: &str, when: &str, status: SessionStatus) -> HomeSession {
    HomeSession {
        name: SharedString::from(name.to_owned()),
        prompt: SharedString::from(prompt.to_owned()),
        when: SharedString::from(when.to_owned()),
        status,
    }
}

fn project(
    [name, path, when]: [&str; 3],
    branches: [&str; 2],
    worktree: [&str; 2],
    sessions: Vec<HomeSession>,
) -> HomeProject {
    let [tree, tree_path] = worktree.map(|part| SharedString::from(part.to_owned()));
    HomeProject {
        name: SharedString::from(name.to_owned()),
        path: SharedString::from(path.to_owned()),
        when: SharedString::from(when.to_owned()),
        branches: shared(&branches),
        folders: vec![
            (
                SharedString::from(name.to_owned()),
                SharedString::from(path.to_owned()),
            ),
            (tree, tree_path),
        ],
        sessions,
    }
}

fn home_projects() -> Vec<HomeProject> {
    vec![
        project(
            ["notes-app", "F:/code/notes-app", "now"],
            ["main", "develop"],
            ["notes-app-wt-sqlite", "F:/code/notes-app-wt-sqlite"],
            vec![
                session(
                    "clear-sable-eagle",
                    "Port the store to SQLite and keep the API the same",
                    "22m ago",
                    SessionStatus::Running,
                ),
                session(
                    "quiet-amber-heron",
                    "Check the badge after each reseed, every 10m",
                    "1h ago",
                    SessionStatus::Running,
                ),
                session(
                    "tidy-ochre-wren",
                    "Why is All sorted by id and not created_at?",
                    "1d ago",
                    SessionStatus::Finished,
                ),
            ],
        ),
        project(
            ["tofu", "F:/localhost/ephem-sh/tofu", "2h ago"],
            ["develop", "main"],
            ["tofu-wt-bench", "F:/localhost/ephem-sh/tofu-wt-bench"],
            vec![
                session(
                    "keen-brass-mole",
                    "Run the bench and report the slowest step",
                    "2h ago",
                    SessionStatus::Finished,
                ),
                session(
                    "crisp-azure-swift",
                    "explain this repository to me",
                    "3d ago",
                    SessionStatus::Stopped,
                ),
                session(
                    "vivid-sable-egret",
                    "Fork keeps the recent tail word for word",
                    "5d ago",
                    SessionStatus::Finished,
                ),
            ],
        ),
        project(
            ["bob", "F:/localhost/ephem-sh/bob", "1w ago"],
            ["main", "develop"],
            ["bob-wt-cli", "F:/localhost/ephem-sh/bob-wt-cli"],
            vec![session(
                "fond-sandy-mink",
                "Scaffold the CLI and its first verb",
                "1w ago",
                SessionStatus::Finished,
            )],
        ),
    ]
}

fn send(book: &mut Book, text: &str, cx: &mut Context<Book>) {
    let text = text.trim();
    if text.is_empty() {
        return;
    }
    let page = &mut book.composer;
    let message = SharedString::from(text.to_owned());
    if let Some(area) = &page.area {
        area.update(cx, |area, cx| area.clear(cx));
    }
    if page.turn.is_some() {
        page.queued.push(message);
    } else {
        page.sent.push(message);
        start(page, cx);
    }
    cx.notify();
}

fn start(page: &mut ComposerPage, cx: &mut Context<Book>) {
    page.turn = Some(cx.spawn(async move |this, cx| {
        cx.background_executor().timer(TURN).await;
        this.update(cx, |book, cx| {
            let page = &mut book.composer;
            page.turn = None;
            if !page.queued.is_empty() {
                let next = page.queued.remove(0);
                page.sent.push(next);
                start(page, cx);
            }
            cx.notify();
        })
    }));
}

fn home_send(book: &mut Book, text: &str, cx: &mut Context<Book>) {
    let text = text.trim();
    if text.is_empty() {
        return;
    }
    let page = &mut book.composer;
    page.home_sent.push(SharedString::from(text.to_owned()));
    if let Some(area) = &page.home_area {
        area.update(cx, |area, cx| area.clear(cx));
    }
    cx.notify();
}

fn drop_at(list: &mut Vec<SharedString>, at: usize) {
    if at < list.len() {
        list.remove(at);
    }
}

pub(super) struct ComposerPage {
    home_area: Option<Entity<TextArea>>,
    home: Option<Entity<HomeComposer>>,
    home_sent: Vec<SharedString>,
    area: Option<Entity<TextArea>>,
    sent: Vec<SharedString>,
    queued: Vec<SharedString>,
    turn: Option<Task<Result<()>>>,
    variant: ComposerVariant,
    attachments: Vec<SharedString>,
    traces: Vec<SharedString>,
    pickers: [Entity<Dropdown>; 2],
}

impl ComposerPage {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        ComposerPage {
            home_area: None,
            home: None,
            home_sent: Vec::new(),
            area: None,
            sent: Vec::new(),
            queued: Vec::new(),
            turn: None,
            variant: ComposerVariant::default(),
            attachments: shared(&ATTACHED),
            traces: shared(&TRACED),
            pickers: [models(), efforts()].map(|items| {
                let picker = Dropdown::new(items, cx);
                picker.update(cx, |picker, _| picker.trigger(DropdownTrigger::Flat));
                picker
            }),
        }
    }

    pub(super) fn key(&mut self, _: &str) -> bool {
        false
    }

    fn home(&mut self, window: &mut Window, cx: &mut Context<Book>) -> Entity<HomeComposer> {
        if let Some(home) = &self.home {
            return home.clone();
        }
        let submit = cx.listener(|this, text: &str, _, cx| home_send(this, text, cx));
        let area = cx.new(|cx| {
            TextArea::new(HOME_ASK.into(), window, cx)
                .bare()
                .on_submit(submit)
        });
        let home = HomeComposer::new(
            area.clone(),
            HOME_ACCOUNT.into(),
            HOME_MODEL.into(),
            HOME_EFFORT.into(),
            home_projects(),
            HOME_CURRENT,
            cx,
        );
        let book = cx.entity().downgrade();
        home.update(cx, |home, _| {
            home.on_send(move |text, _, cx| {
                if let Some(book) = book.upgrade() {
                    book.update(cx, |book, cx| home_send(book, &text, cx));
                }
            });
        });
        self.home_area = Some(area);
        self.home = Some(home.clone());
        home
    }

    pub(super) fn render(
        &mut self,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> Div {
        let area = self
            .area
            .get_or_insert_with(|| {
                let submit = cx.listener(|this, text: &str, _, cx| send(this, text, cx));
                cx.new(|cx| {
                    TextArea::new(ASK.into(), window, cx)
                        .bare()
                        .max_lines(AREA_LINES)
                        .on_submit(submit)
                })
            })
            .clone();
        let home = self.home(window, cx);
        let home_sent = self
            .home_sent
            .iter()
            .map(|message| inner_card(theme).px_3().py_2().child(message.clone()));
        let variants = ComposerVariant::ALL
            .iter()
            .enumerate()
            .map(|(at, &variant)| {
                let kind = if variant == self.variant {
                    ButtonKind::Primary
                } else {
                    ButtonKind::Plain
                };
                let pick = button(("composer-type", at), variant.label(), None, kind, theme)
                    .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                        this.composer.variant = variant;
                        cx.notify();
                    }));
                named(format!("composer type {}", at + 1), theme, pick)
            });
        let stop = cx.listener(|this, _: &(), _, cx| {
            this.composer.turn = None;
            this.composer.queued.clear();
            cx.notify();
        });
        let attach = cx.listener(|this, _: &(), _, cx| {
            let next = this.composer.attachments.len() + 1;
            this.composer
                .attachments
                .push(format!("notes-{next}.md").into());
            cx.notify();
        });
        let remove = cx.listener(|this, at: &usize, _, cx| {
            drop_at(&mut this.composer.attachments, *at);
            cx.notify();
        });
        let untrace = cx.listener(|this, at: &usize, _, cx| {
            drop_at(&mut this.composer.traces, *at);
            cx.notify();
        });
        let unqueue = cx.listener(|this, at: &usize, _, cx| {
            drop_at(&mut this.composer.queued, *at);
            cx.notify();
        });
        let restore = button(
            "attachments-reset",
            "Restore files and traces",
            None,
            ButtonKind::Plain,
            theme,
        )
        .on_click(cx.listener(|this, _: &ClickEvent, _, cx| {
            this.composer.attachments = shared(&ATTACHED);
            this.composer.traces = shared(&TRACED);
            cx.notify();
        }));
        let [model, effort] = self.pickers.clone();
        let composer = Composer::new("composer", area)
            .variant(self.variant)
            .busy(self.turn.is_some())
            .phase(PHASE)
            .attachments(self.attachments.clone())
            .traces(self.traces.clone())
            .queued(self.queued.clone())
            .model(model)
            .effort(effort)
            .on_send(cx.listener(|this, text: &str, _, cx| send(this, text, cx)))
            .on_stop(move |window, cx| stop(&(), window, cx))
            .on_attach(move |window, cx| attach(&(), window, cx))
            .on_remove(move |at, window, cx| remove(&at, window, cx))
            .on_remove_trace(move |at, window, cx| untrace(&at, window, cx))
            .on_unqueue(move |at, window, cx| unqueue(&at, window, cx));
        let sent = self
            .sent
            .iter()
            .map(|message| inner_card(theme).px_3().py_2().child(message.clone()));
        let home = block(
            "IHOME-1 first screen: one input that grows, account, project, checkout, branch and Resume each open a menu, Enter adds a line beneath",
            theme,
            div()
                .p_6()
                .flex()
                .justify_center()
                .bg(theme.color(ColorToken::SurfaceWindow))
                .child(
                    div()
                        .w(px(HOME_WIDTH))
                        .flex()
                        .flex_col()
                        .gap(px(HOME_GAP))
                        .child(home)
                        .children(home_sent),
                ),
        );
        let tile = block(
            "ICHAT-1 idle and ICHAT-5 in a turn: Enter or Send starts a 3 s turn, Enter during it queues, Stop ends it and drops the queue",
            theme,
            div()
                .w(px(COMPOSER_WIDTH))
                .flex()
                .flex_col()
                .gap_2()
                .children(sent)
                .child(composer)
                .child(spread(theme).children(variants))
                .child(div().flex().child(restore)),
        );
        div().flex().flex_col().gap_6().child(home).child(tile)
    }
}
