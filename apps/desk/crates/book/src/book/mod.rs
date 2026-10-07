mod agents;
mod charts;
mod chat;
mod composer;
mod content;
mod controls;
mod decide;
mod diff;
mod editor;
mod empty;
mod file_edits;
mod focus;
mod foundations;
mod graph;
mod kit;
mod lists;
mod palette;
mod shell;
mod shells;
mod terminal;
mod tiling;
mod tips;

use std::time::Instant;

use desk_motion::{Presence, reduced_motion, set_reduced_motion, system_reduced_motion};
use desk_perf::Profiler;
use desk_ui::components::card::caption;
use desk_ui::components::chip::kbd;
use desk_ui::components::form::{segmented, switch};
use desk_ui::components::overlay::{ToastKind, Toaster};
use desk_ui::components::paint::{ink, ms};
use desk_ui::components::scroll::ScrollArea;
use desk_ui::components::size::{
    CAPTION_TEXT, DIM_TEXT, FONT_SMALL, FONT_TITLE, RADIUS_CHIP, RADIUS_ROW, T1, T2, T3,
};
use desk_ui::live::ActiveTheme;
use desk_ui::metrics::{SIDEBAR_WIDTH, TEXT};
use desk_ui::theme::{ColorToken, Mode, NumberToken, Theme};
use gpui::{
    App, ClickEvent, Context, Div, Entity, FocusHandle, FontWeight, KeyDownEvent,
    ModifiersChangedEvent, SharedString, Stateful, Window, deferred, div, prelude::*, px,
};

use crate::Launch;
use crate::catalog::{Group, Page, UNBUILT};
use crate::themes::{self, Choice};
use agents::AgentsPage;
use charts::ChartsPage;
use chat::{AskPage, ChatPage};
use composer::ComposerPage;
use content::ContentState;
use controls::ControlsState;
use decide::Crossfade;
use diff::DiffPage;
use editor::{HistoryPage, TreePage};
use file_edits::FileEditsPage;
use focus::FocusPage;
use foundations::FoundationsState;
use graph::GraphPage;
use kit::{MODELS, label};
use lists::ListsState;
use palette::PalettePage;
use shell::ShellPage;
use shells::ShellsPage;
use terminal::TerminalPage;
use tiling::TilingPage;

const META_WIDTH: f32 = 150.0;

pub struct Book {
    page: Crossfade<Page>,
    themes: Vec<Choice>,
    theme: usize,
    theme_error: Option<SharedString>,
    mode: Option<Mode>,
    focus: FocusHandle,
    toaster: Entity<Toaster>,
    foundations: FoundationsState,
    lists: ListsState,
    controls: ControlsState,
    focus_page: FocusPage,
    content: ContentState,
    composer: ComposerPage,
    shell: ShellPage,
    diff: DiffPage,
    chat: ChatPage,
    ask: AskPage,
    tree: TreePage,
    history: HistoryPage,
    shells: ShellsPage,
    terminal: TerminalPage,
    agents: AgentsPage,
    file_edits: FileEditsPage,
    charts: ChartsPage,
    graph: GraphPage,
    tiling: TilingPage,
    palette: PalettePage,
}

impl Book {
    pub fn new(launch: Launch, window: &mut Window, cx: &mut Context<Self>) -> Self {
        let focus = cx.focus_handle();
        focus.focus(window, cx);
        let theme = ActiveTheme::theme(cx);
        let presence = Presence::new(
            ms(&theme, NumberToken::MotionEnter),
            ms(&theme, NumberToken::MotionExit),
        );
        set_reduced_motion(system_reduced_motion(), cx);
        Book {
            page: Crossfade::new(launch.page),
            themes: launch.themes,
            theme: launch.theme,
            theme_error: None,
            mode: launch.mode,
            focus,
            toaster: Toaster::new(cx),
            foundations: FoundationsState::new(presence, window, cx),
            lists: ListsState::new(),
            controls: ControlsState::new(presence, window, cx),
            focus_page: FocusPage::new(cx),
            content: ContentState::new(cx),
            composer: ComposerPage::new(cx),
            shell: ShellPage::new(cx),
            diff: DiffPage::new(cx),
            chat: ChatPage::new(cx),
            ask: AskPage::new(cx),
            tree: TreePage::new(cx),
            history: HistoryPage::new(cx),
            shells: ShellsPage::new(cx),
            terminal: TerminalPage::new(cx),
            agents: AgentsPage::new(cx),
            file_edits: FileEditsPage::new(cx),
            charts: ChartsPage::new(cx),
            graph: GraphPage::new(cx),
            tiling: TilingPage::new(cx),
            palette: PalettePage::new(window, cx),
        }
    }

    fn key(&mut self, event: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let key = event.keystroke.key.as_str();
        if key == "f12" {
            Profiler::toggle_overlay(cx);
            return;
        }
        let page = self.page.shown();
        let used = match page {
            Page::Composer => self.composer.key(key),
            Page::Shell => self.shell.key(key),
            Page::Diff => self.diff.key(key),
            Page::ChatRows => self.chat.key(key),
            Page::AskBar => self.ask.key(key),
            Page::FileTree => self.tree.key(key),
            Page::History => self.history.key(key),
            Page::Shells => self.shells.key(key),
            Page::Agents => self.agents.key(key),
            Page::Charts => self.charts.key(key),
            Page::Delegation => self.graph.key(key),
            Page::Tiling => self.tiling.key(event),
            Page::Palette => self.palette.key(event, window, cx),
            Page::Focus => self.focus_page.key(event, window, cx),
            _ => self.content.key(page, key) || self.controls.key(page, key),
        };
        if used {
            cx.notify();
        }
    }

    fn clicked(
        what: &'static str,
        cx: &mut Context<Self>,
    ) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
        cx.listener(move |this, _: &ClickEvent, _, cx| {
            this.controls.clicks += 1;
            let clicks = this.controls.clicks;
            this.tell(format!("{what}: clicked, {clicks} clicks so far"), cx);
        })
    }

    fn tell(&mut self, text: impl Into<SharedString>, cx: &mut Context<Self>) {
        let text = text.into();
        self.toaster
            .update(cx, |toaster, cx| toaster.show(ToastKind::Info, text, cx));
    }

    fn picked_model(cx: &mut Context<Self>) -> impl Fn(&usize, &mut Window, &mut App) + 'static {
        cx.listener(|this, at: &usize, _, cx| {
            let picked = MODELS.get(*at).copied().unwrap_or_default();
            this.tell(format!("Picked {picked}"), cx);
        })
    }

    fn choose_theme(&mut self, at: usize, cx: &mut Context<Self>) {
        let Some(choice) = self.themes.get(at) else {
            return;
        };
        match themes::apply(choice, self.mode, cx) {
            Ok(()) => {
                self.theme = at;
                self.theme_error = None;
            }
            Err(error) => self.theme_error = Some(error.into()),
        }
        cx.notify();
    }

    fn choose_mode(&mut self, mode: Mode, cx: &mut Context<Self>) {
        let Some(choice) = self.themes.get(self.theme) else {
            return;
        };
        match themes::set_mode(&choice.name, mode, cx) {
            Ok(()) => {
                self.mode = Some(mode);
                self.theme_error = None;
            }
            Err(error) => self.theme_error = Some(error.into()),
        }
        cx.notify();
    }

    fn entry(&self, page: Page, theme: &Theme, cx: &mut Context<Self>) -> Stateful<Div> {
        let chosen = page == self.page.shown();
        let hover = theme.color(ColorToken::StateHover);
        div()
            .id(page.sheet().slug)
            .px_2()
            .py_1()
            .rounded(px(RADIUS_ROW))
            .cursor_pointer()
            .when(chosen, |entry| {
                entry
                    .bg(theme.color(ColorToken::StateActive))
                    .text_color(ink(theme, T1))
            })
            .when(!chosen, |entry| {
                entry
                    .text_color(ink(theme, T2))
                    .hover(move |style| style.bg(hover))
            })
            .child(page.sheet().title)
            .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                this.page.switch(page, Instant::now());
                cx.notify();
            }))
    }

    fn sidebar(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let mut groups = Vec::new();
        for group in Group::ALL {
            let mut column = div()
                .flex()
                .flex_col()
                .gap_0p5()
                .pt_3()
                .child(div().px_2().pb_1().child(caption(group.title(), theme)));
            for page in Page::ALL
                .into_iter()
                .filter(|page| page.sheet().group == group)
            {
                column = column.child(self.entry(page, theme, cx));
            }
            column = column.children(UNBUILT.iter().filter(|(of, _)| *of == group).map(
                |(_, name)| {
                    div()
                        .px_2()
                        .py_1()
                        .text_color(ink(theme, T3))
                        .child(format!("{name}, not built"))
                },
            ));
            groups.push(column);
        }
        div()
            .w(px(SIDEBAR_WIDTH))
            .flex_none()
            .flex()
            .flex_col()
            .child(ScrollArea::new("sidebar").child(div().p_2().children(groups)))
    }

    fn toolbar(&self, theme: &Theme, cx: &mut Context<Self>) -> Div {
        let hover = ink(theme, T1);
        let choices = self.themes.iter().enumerate().map(|(at, choice)| {
            let chosen = at == self.theme;
            div()
                .id(("theme", at))
                .px_2()
                .py_0p5()
                .rounded(px(RADIUS_CHIP))
                .cursor_pointer()
                .text_size(px(FONT_SMALL))
                .when(chosen, |item| {
                    item.bg(theme.color(ColorToken::SegmentedOn))
                        .text_color(ink(theme, T1))
                })
                .when(!chosen, |item| {
                    item.text_color(ink(theme, DIM_TEXT))
                        .hover(move |style| style.text_color(hover))
                })
                .child(choice.name.clone())
                .on_click(cx.listener(move |this, _: &ClickEvent, _, cx| {
                    this.choose_theme(at, cx);
                }))
        });
        div()
            .flex()
            .flex_none()
            .items_center()
            .gap_3()
            .px_4()
            .py_2()
            .child(caption("Theme", theme))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_1()
                    .p_0p5()
                    .rounded(px(RADIUS_ROW))
                    .bg(theme.color(ColorToken::SegmentedFill))
                    .children(choices),
            )
            .child(segmented(
                "mode",
                &Mode::ALL
                    .iter()
                    .map(|mode| mode.label())
                    .collect::<Vec<_>>(),
                Mode::ALL
                    .iter()
                    .position(|mode| *mode == ActiveTheme::mode(cx))
                    .unwrap_or_default(),
                theme,
                cx.listener(|this, at: &usize, _, cx| {
                    if let Some(mode) = Mode::ALL.get(*at) {
                        this.choose_mode(*mode, cx);
                    }
                }),
            ))
            .child(
                switch("reduced", "Reduced motion", reduced_motion(cx), theme).on_click(
                    cx.listener(|_, _: &ClickEvent, _, cx| {
                        set_reduced_motion(!reduced_motion(cx), cx);
                        cx.notify();
                    }),
                ),
            )
            .child(div().flex_1())
            .children(self.theme_error.clone().map(|error| {
                div()
                    .text_color(theme.color(ColorToken::StatusDanger))
                    .child(error)
            }))
            .child(kbd("F12", theme))
            .child(label("frame overlay", theme))
    }

    fn sheet(
        &mut self,
        page: Page,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Div {
        div()
            .flex()
            .flex_col()
            .gap_3()
            .when(page != Page::Tiling, |sheet| {
                sheet.child(Self::header(page, theme))
            })
            .child(self.body(page, theme, window, cx))
    }

    fn header(page: Page, theme: &Theme) -> Div {
        let sheet = page.sheet();
        let meta = |name: &'static str, value: &'static str| {
            div()
                .flex()
                .gap_2()
                .child(
                    div()
                        .w(px(META_WIDTH))
                        .flex_none()
                        .text_color(ink(theme, CAPTION_TEXT))
                        .child(name),
                )
                .child(div().flex_1().child(value))
        };
        div()
            .flex()
            .flex_col()
            .gap_1()
            .pt_2()
            .child(
                div()
                    .text_size(px(FONT_TITLE))
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(ink(theme, T1))
                    .child(sheet.title),
            )
            .child(div().text_color(ink(theme, T2)).child(sheet.about))
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap_0p5()
                    .pt_2()
                    .text_size(px(FONT_SMALL))
                    .child(meta("Boards", sheet.boards))
                    .child(meta("Source", sheet.source))
                    .child(meta("States drawn", sheet.states))
                    .when(!sheet.missing.is_empty(), |lines| {
                        lines.child(meta("Not in the component", sheet.missing))
                    }),
            )
    }

    fn body(
        &mut self,
        page: Page,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) -> Div {
        match page {
            Page::Type | Page::Icons | Page::Surfaces | Page::Motion => {
                self.foundations.render(page, theme, window, cx)
            }
            Page::Tabs | Page::Row | Page::Table | Page::Feed => self.lists.render(page, theme, cx),
            Page::Avatar
            | Page::Chip
            | Page::Button
            | Page::Input
            | Page::Segmented
            | Page::Switch => self.controls.render(page, theme, window, cx),
            Page::Popover
            | Page::Menu
            | Page::Dropdown
            | Page::ContextMenu
            | Page::Toast
            | Page::Sheet
            | Page::Drawer => self.content.render(page, theme, window, cx),
            Page::Focus => self.focus_page.render(theme, window, cx),
            Page::Tooltips => tips::tips(theme, window, cx),
            Page::Empty => empty::empty_page(theme, window, cx),
            Page::Composer => self.composer.render(theme, window, cx),
            Page::Shell => self.shell.render(theme, window, cx),
            Page::Diff => self.diff.render(theme, window, cx),
            Page::ChatRows => self.chat.render(theme, window, cx),
            Page::AskBar => self.ask.render(theme, window, cx),
            Page::FileTree => self.tree.render(theme, window, cx),
            Page::History => self.history.render(theme, window, cx),
            Page::Shells => self.shells.render(theme, window, cx),
            Page::Terminal => self.terminal.render(theme, window, cx),
            Page::Agents => self.agents.render(theme, window, cx),
            Page::FileEdits => self.file_edits.render(theme, window, cx),
            Page::Charts => self.charts.render(theme, window, cx),
            Page::Delegation => self.graph.render(theme, window, cx),
            Page::Tiling => self.tiling.render(theme, window, cx),
            Page::Palette => self.palette.render(theme, cx),
        }
    }
}

impl Render for Book {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = ActiveTheme::theme(cx);
        let now = Instant::now();
        let shown = self.page.shown();
        let entering = self.sheet(shown, &theme, window, cx);
        let leaving = self
            .page
            .leaving(now)
            .map(|page| self.sheet(page, &theme, window, cx));
        let staged = self
            .page
            .stage(now, reduced_motion(cx), window, entering, leaving);
        div()
            .id("book")
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::key))
            .on_modifiers_changed(cx.listener(|_, _: &ModifiersChangedEvent, _, cx| cx.notify()))
            .size_full()
            .relative()
            .flex()
            .bg(theme.color(ColorToken::SurfaceWindow))
            .text_size(px(TEXT))
            .text_color(theme.color(ColorToken::TextBase))
            .child(self.sidebar(&theme, cx))
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .flex()
                    .flex_col()
                    .child(self.toolbar(&theme, cx))
                    .child(
                        ScrollArea::new("page")
                            .child(div().px_4().pb_4().flex().flex_col().child(staged)),
                    ),
            )
            .child(deferred(self.toaster.clone()).priority(1))
            .children(desk_perf::overlay(cx))
    }
}
