use desk_tiling::{Module, Preset, Rect, Stack, Workspace};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::ink;
use desk_ui::components::size::{FONT_TITLE, T1};
use desk_ui::components::tabs::Tab;
use desk_ui::components::tiling_board::{Host, SIDE_WIDTH, TilingBoard};
use desk_ui::theme::Theme;
use gpui::{
    AnyElement, Context, Div, FontWeight, KeyDownEvent, SharedString, Window, div, prelude::*, px,
};

use super::Book;
use super::kit::label;

const PRESETS: [(&str, Preset); 3] = [
    ("work", Preset::Work),
    ("editor", Preset::Editor),
    ("data", Preset::Data),
];
const DATA_PLUGIN: &str = "data-studio";
const FIRST_TOP: f32 = 160.0;
const BOTTOM_PAD: f32 = 16.0;
const SIDE_FROM: f32 = 960.0;
const EMPTY_SPAWNS: [Module; 4] = [
    Module::Chat,
    Module::Editor,
    Module::Terminal,
    Module::Shells,
];

fn glyph_of(module: &Module) -> Glyph {
    match module {
        Module::Chat | Module::Browser => Glyph::Chat,
        Module::SubAgents => Glyph::Agents,
        Module::FileEdits | Module::Editor => Glyph::File,
        Module::Shells | Module::Terminal => Glyph::Terminal,
        Module::SourceControl => Glyph::Trace,
        Module::Plugin(_) => Glyph::Attach,
    }
}

fn spawnable() -> Vec<Module> {
    let mut modules = Module::BUILT_IN.to_vec();
    modules.push(Module::Plugin(DATA_PLUGIN.to_owned()));
    modules
}

fn demo(stack: &Stack, solved: Rect, theme: &Theme, _: &mut Context<Book>) -> AnyElement {
    let name = stack
        .modules
        .get(stack.active)
        .map_or("", Module::name)
        .to_owned();
    div()
        .id(SharedString::from(format!("tiling-body-{}", stack.id.0)))
        .flex_1()
        .min_h_0()
        .flex()
        .flex_col()
        .items_center()
        .justify_center()
        .gap_1()
        .overflow_hidden()
        .child(
            div()
                .text_size(px(FONT_TITLE))
                .font_weight(FontWeight::SEMIBOLD)
                .text_color(ink(theme, T1))
                .child(name),
        )
        .child(label(format!("tile {}", stack.id.0), theme))
        .child(label(
            format!("{:.0} × {:.0} px", solved.w, solved.h),
            theme,
        ))
        .into_any_element()
}

pub(super) struct TilingPage {
    board: TilingBoard<Book>,
}

impl TilingPage {
    pub(super) fn new(_: &mut Context<Book>) -> Self {
        let workspaces = PRESETS
            .iter()
            .map(|(name, preset)| Workspace::new(*name, *preset))
            .collect();
        TilingPage {
            board: TilingBoard::new(
                workspaces,
                Host {
                    board: |book| &mut book.tiling.board,
                    body: Box::new(demo),
                    title: Box::new(|module, mark, _| Tab {
                        label: module.name().to_owned().into(),
                        icon: Some(glyph_of(module)),
                        count: None,
                        mark,
                    }),
                    subtitle: Box::new(|_, _| None),
                    spawnable: spawnable(),
                    spawns: EMPTY_SPAWNS.to_vec(),
                    settled: Box::new(|_, _, _| {}),
                    origin: (0.0, FIRST_TOP),
                    below: BOTTOM_PAD,
                },
            ),
        }
    }

    pub(super) fn key(&mut self, event: &KeyDownEvent) -> bool {
        self.board.key(event)
    }

    pub(super) fn render(
        &mut self,
        theme: &Theme,
        window: &mut Window,
        cx: &mut Context<Book>,
    ) -> Div {
        let wide = f32::from(window.viewport_size().width) >= SIDE_FROM + SIDE_WIDTH;
        let board = self.board.board(theme, window, cx);
        self.board
            .watch(div().flex().flex_col().gap_2().pt_2(), cx)
            .child(self.board.workspace_tabs(theme, cx))
            .child(self.board.toolbar(theme, cx))
            .child(
                div()
                    .flex()
                    .gap_3()
                    .child(board)
                    .when(wide, |row| row.child(self.board.side(theme))),
            )
    }
}
