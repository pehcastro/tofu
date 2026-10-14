use desk_ui::components::chat::{Block, lead, markdown};
use desk_ui::theme::Theme;
use gpui::{Div, div, prelude::*, px};

use super::kit::{block, label};

const COLUMN: f32 = 560.0;

const TABLES: [(&str, &str); 6] = [
    (
        "Narrow",
        "| Key | Value |\n| --- | --- |\n| mode | dark |\n| scale | 1.0 |",
    ),
    (
        "12 columns",
        "| a | b | c | d | e | f | g | h | i | j | k | l |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n| alpha one | bravo two | charlie three | delta four | echo five | foxtrot six | golf seven | hotel eight | india nine | juliet ten | kilo eleven | lima twelve |\n| 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 | 11 | 12 |",
    ),
    (
        "Long cell text",
        "| Step | What happens |\n| --- | --- |\n| plan | The lead reads the ticket, every file it names and every caller of what it changes, then writes the shape of the change before any code, so the code on top of it reads as the obvious thing to write. |\n| verify | The gate runs. |",
    ),
    (
        "Alignment row",
        "| Left | Center | Right |\n| :--- | :---: | ---: |\n| apples | 3 | 1.20 |\n| pears | 12 | 14.05 |",
    ),
    (
        "Inline code in cells",
        "| Command | Does |\n| --- | --- |\n| `cargo check -p desk_ui -j 4` | type checks one crate |\n| `rustfmt --edition 2024 --check` | formats nothing, reports |",
    ),
    (
        "Empty cells",
        "| Name | Owner | Due |\n| --- | --- | --- |\n| gaps |  | 10-14 |\n|  | desk |  |\n| fences | ui | |",
    ),
];

const PROSE: &str = "A reply mixes prose, tables and code, one gap apart.\n\n```go\nfunc main() {\n\tfmt.Println(\"go\")\n}\n```\n\nThe fence above names its language top right.";

pub struct ChatMarkdownPage {
    tables: Vec<(&'static str, Vec<Block>)>,
    prose: Vec<Block>,
}

impl ChatMarkdownPage {
    pub fn new() -> Self {
        ChatMarkdownPage {
            tables: TABLES
                .iter()
                .map(|(name, source)| (*name, markdown(source)))
                .collect(),
            prose: markdown(PROSE),
        }
    }

    pub fn render(&self, theme: &Theme) -> Div {
        let column = |id: &'static str, body: &[Block]| {
            div()
                .w(px(COLUMN))
                .min_w_0()
                .child(lead(id, "now", body, &[], theme))
        };
        div()
            .flex()
            .flex_col()
            .gap_4()
            .children(
                self.tables
                    .iter()
                    .map(|(name, body)| block(name, theme, column(name, body))),
            )
            .child(block(
                "Prose and a go fence",
                theme,
                div()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .child(label("block gap is GRID_GAP", theme))
                    .child(column("prose", &self.prose)),
            ))
    }
}
