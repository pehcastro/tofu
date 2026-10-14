use desk_canvas::{Canvas, Framing, View};
use desk_ui::components::button::{ButtonKind, button};
use desk_ui::theme::{ColorToken, Theme};
use draw_doc::Scene;
use gpui::{
    AppContext, ClickEvent, Context, Div, Entity, SharedString, StyleRefinement, Window, div,
    prelude::*, px,
};
use serde_json::{Value, json};

use super::Book;
use super::kit::label;

const FIXTURE: &str = concat!(
    env!("CARGO_MANIFEST_DIR"),
    "/../draw_doc/fixtures/real-liarsdice.excalidraw"
);
const CANVAS_HEIGHT: f32 = 640.0;
const BENCH_SHAPES: usize = 10_000;
const COLUMNS: usize = 100;
const PITCH: (f64, f64) = (160.0, 120.0);
const SHAPE: (f64, f64) = (120.0, 80.0);
const KINDS: [&str; 5] = ["rectangle", "ellipse", "diamond", "text", "arrow"];

fn fixture() -> Result<Scene, String> {
    let text = std::fs::read_to_string(FIXTURE).map_err(|error| format!("{FIXTURE}: {error}"))?;
    Scene::read(&text).map_err(|error| format!("{FIXTURE}: {error}"))
}

fn grid(count: usize) -> Result<Scene, String> {
    let elements: Vec<Value> = (0..count)
        .map(|at| {
            let x = (at % COLUMNS) as f64 * PITCH.0;
            let y = (at / COLUMNS) as f64 * PITCH.1;
            let kind = KINDS.get(at % KINDS.len()).copied().unwrap_or("rectangle");
            let mut element = json!({
                "id": format!("bench-{at}"),
                "type": kind,
                "x": x,
                "y": y,
                "width": SHAPE.0,
                "height": SHAPE.1,
                "strokeColor": "#1e1e1e",
                "backgroundColor": "transparent",
                "strokeWidth": 1,
                "roundness": null,
            });
            match kind {
                "text" => {
                    element["text"] = json!(format!("shape {at}"));
                    element["fontSize"] = json!(20);
                }
                "arrow" => {
                    element["points"] = json!([[0.0, 0.0], [SHAPE.0, SHAPE.1]]);
                    element["endArrowhead"] = json!("arrow");
                }
                _ => {}
            }
            element
        })
        .collect();
    let file =
        json!({ "type": "excalidraw", "version": 2, "source": "desk-book", "elements": elements });
    Scene::read(&file.to_string()).map_err(|error| format!("bench grid: {error}"))
}

pub(super) struct DrawPage {
    canvas: Entity<Canvas>,
    failure: Option<SharedString>,
}

impl DrawPage {
    pub(super) fn new(cx: &mut Context<Book>) -> Self {
        let (scene, failure) = match fixture() {
            Ok(scene) => (scene, None),
            Err(error) => (Scene::new(), Some(error.into())),
        };
        DrawPage {
            canvas: cx.new(|cx| Canvas::new(scene, cx)),
            failure,
        }
    }

    fn load(&mut self, scene: Result<Scene, String>, framing: Framing, cx: &mut Context<Book>) {
        match scene {
            Ok(scene) => {
                self.failure = None;
                self.canvas
                    .update(cx, |canvas, cx| canvas.show(scene, framing, cx));
            }
            Err(error) => self.failure = Some(error.into()),
        }
        cx.notify();
    }

    pub(super) fn render(&mut self, theme: &Theme, _: &mut Window, cx: &mut Context<Book>) -> Div {
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(label(
                "Wheel pans, Ctrl+wheel zooms at the pointer, drag empty space to pan. Click selects, drag moves, corner handles resize, double-click edits text and Enter commits. Ctrl+Z undoes, Ctrl+Y redoes.",
                theme,
            ))
            .child(
                div()
                    .flex()
                    .gap_2()
                    .child(
                        button("draw-fixture", "Liarsdice", None, ButtonKind::Plain, theme)
                            .on_click(cx.listener(|book, _: &ClickEvent, _, cx| {
                                book.draw.load(fixture(), Framing::Fit, cx)
                            })),
                    )
                    .child(
                        button("draw-bench", "10,000 shapes", None, ButtonKind::Plain, theme)
                            .on_click(cx.listener(|book, _: &ClickEvent, _, cx| {
                                let at = Framing::At(View::new(0.0, 0.0, 1.0));
                                book.draw.load(grid(BENCH_SHAPES), at, cx)
                            })),
                    ),
            )
            .children(self.failure.clone().map(|failure| {
                div()
                    .text_color(theme.color(ColorToken::StatusDanger))
                    .child(failure)
            }))
            .child(
                div()
                    .w_full()
                    .h(px(CANVAS_HEIGHT))
                    .border_1()
                    .border_color(theme.color(ColorToken::Separator))
                    .overflow_hidden()
                    .child(
                        self.canvas
                            .clone()
                            .cached(StyleRefinement::default().size_full()),
                    ),
            )
    }
}
