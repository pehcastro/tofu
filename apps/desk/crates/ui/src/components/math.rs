use std::rc::Rc;
use std::sync::Once;
use std::time::Instant;

use gpui::{
    AnyElement, App, Bounds, ElementId, FontStyle, FontWeight, Hsla, PathBuilder, Pixels, Point,
    Rgba, Size, TextRun, Window, canvas, div, font, point, prelude::*, px, rgb_to_hsla, size,
};
use ratex_layout::{LayoutOptions, to_display_list};
use ratex_types::{Color, DisplayItem, DisplayList, MathStyle, PathCommand};

const FONT_FILES: [&str; 19] = [
    "AMS-Regular",
    "Caligraphic-Bold",
    "Caligraphic-Regular",
    "Fraktur-Bold",
    "Fraktur-Regular",
    "Main-Bold",
    "Main-BoldItalic",
    "Main-Italic",
    "Main-Regular",
    "Math-BoldItalic",
    "Math-Italic",
    "SansSerif-Bold",
    "SansSerif-Italic",
    "SansSerif-Regular",
    "Script-Regular",
    "Size1-Regular",
    "Size2-Regular",
    "Size3-Regular",
    "Size4-Regular",
];
const STROKE_EM: f32 = 0.04;
const DISPLAY_PAD_Y: f32 = 4.0;
const DISPLAY_SCALE: f32 = 1.2;

static FONTS: Once = Once::new();

pub struct Math {
    pub tex: String,
    list: DisplayList,
}

pub fn layout(tex: &str, display: bool) -> Option<Rc<Math>> {
    let started = Instant::now();
    let nodes = match ratex_parser::parse(tex) {
        Ok(nodes) => nodes,
        Err(error) => {
            eprintln!("desk: math failed: {error}");
            return None;
        }
    };
    let options = LayoutOptions {
        style: match display {
            true => MathStyle::Display,
            false => MathStyle::Text,
        },
        ..LayoutOptions::default()
    };
    let list = to_display_list(&ratex_layout::layout(&nodes, &options));
    eprintln!(
        "desk: math {} chars in {} us",
        tex.chars().count(),
        started.elapsed().as_micros()
    );
    Some(Rc::new(Math {
        tex: tex.to_owned(),
        list,
    }))
}

impl Math {
    pub fn width(&self, em: Pixels) -> Pixels {
        em * self.list.width as f32
    }

    pub fn ascent(&self, em: Pixels) -> Pixels {
        em * self.list.height as f32
    }

    pub fn size(&self, em: Pixels) -> Size<Pixels> {
        size(
            self.width(em),
            em * (self.list.height + self.list.depth) as f32,
        )
    }

    pub fn paint(
        &self,
        top_left: Point<Pixels>,
        em: Pixels,
        ink: Hsla,
        window: &mut Window,
        cx: &App,
    ) {
        FONTS.call_once(|| {
            let fonts = FONT_FILES
                .iter()
                .filter_map(|name| ratex_katex_fonts::ttf_bytes(&format!("KaTeX_{name}.ttf")))
                .collect();
            if let Err(error) = cx.text_system().add_fonts(fonts) {
                eprintln!("desk: math failed: fonts not added: {error}");
            }
        });
        let device = window.scale_factor();
        let at = |x: f64, y: f64| point(top_left.x + em * x as f32, top_left.y + em * y as f32);
        let tint = |color: &Color| match *color == Color::BLACK {
            true => ink,
            false => rgb_to_hsla(Rgba::new(color.r, color.g, color.b, color.a)),
        };
        for item in &self.list.items {
            match item {
                DisplayItem::GlyphPath {
                    x,
                    y,
                    scale,
                    font: face,
                    char_code,
                    color,
                } => glyph(
                    at(*x, *y),
                    em * *scale as f32,
                    face,
                    *char_code,
                    tint(color),
                    window,
                ),
                DisplayItem::Line {
                    x,
                    y,
                    width,
                    thickness,
                    color,
                    ..
                } => window.paint_quad(gpui::fill(
                    rule(
                        at(*x, y - thickness / 2.0),
                        em * *width as f32,
                        em * *thickness as f32,
                        device,
                    ),
                    tint(color),
                )),
                DisplayItem::Rect {
                    x,
                    y,
                    width,
                    height,
                    color,
                } => window.paint_quad(gpui::fill(
                    rule(at(*x, *y), em * *width as f32, em * *height as f32, device),
                    tint(color),
                )),
                DisplayItem::Path {
                    x,
                    y,
                    commands,
                    fill,
                    color,
                } => {
                    let mut builder = match fill {
                        true => PathBuilder::fill(),
                        false => PathBuilder::stroke(em * STROKE_EM),
                    };
                    let to = |px: &f64, py: &f64| at(x + px, y + py);
                    for command in commands {
                        match command {
                            PathCommand::MoveTo { x, y } => builder.move_to(to(x, y)),
                            PathCommand::LineTo { x, y } => builder.line_to(to(x, y)),
                            PathCommand::QuadTo { x1, y1, x, y } => {
                                builder.curve_to(to(x, y), to(x1, y1))
                            }
                            PathCommand::CubicTo {
                                x1,
                                y1,
                                x2,
                                y2,
                                x,
                                y,
                            } => builder.cubic_bezier_to(to(x, y), to(x1, y1), to(x2, y2)),
                            PathCommand::Close => builder.close(),
                        }
                    }
                    match builder.build() {
                        Ok(path) => window.paint_path(path, tint(color)),
                        Err(error) => eprintln!("desk: math failed: path not built: {error}"),
                    }
                }
            }
        }
    }
}

fn rule(top_left: Point<Pixels>, width: Pixels, height: Pixels, device: f32) -> Bounds<Pixels> {
    let snap = |v: Pixels| px((f32::from(v) * device).round() / device);
    let tall = px((f32::from(height) * device).round().max(1.0) / device);
    Bounds::new(point(top_left.x, snap(top_left.y)), size(width, tall))
}

fn glyph(
    baseline: Point<Pixels>,
    font_size: Pixels,
    face: &str,
    code: u32,
    color: Hsla,
    window: &mut Window,
) {
    let Some(id) = ratex_font::FontId::parse(face) else {
        eprintln!("desk: math failed: unknown font {face}");
        return;
    };
    let ch = ratex_font::katex_ttf_glyph_char(id, code);
    let (family, style) = face.split_once('-').unwrap_or((face, "Regular"));
    let mut katex = font(format!("KaTeX_{family}"));
    if style.contains("Bold") {
        katex.weight = FontWeight::BOLD;
    }
    if style.contains("Italic") {
        katex.style = FontStyle::Italic;
    }
    let text = ch.to_string();
    let run = TextRun {
        len: text.len(),
        font: katex,
        color,
        ..TextRun::default()
    };
    let line = window
        .text_system()
        .layout_line(&text, font_size, &[run], None);
    for shaped in &line.runs {
        for g in &shaped.glyphs {
            let origin = point(baseline.x + g.position.x, baseline.y);
            if let Err(error) = window.paint_glyph(origin, shaped.font_id, g.id, font_size, color) {
                eprintln!("desk: math failed: glyph not painted: {error}");
            }
        }
    }
}

pub fn display(id: impl Into<ElementId>, math: Rc<Math>, body: Pixels, ink: Hsla) -> AnyElement {
    let em = body * DISPLAY_SCALE;
    let drawn = math.size(em);
    div()
        .id(id)
        .overflow_x_scroll()
        .restrict_scroll_to_axis()
        .py(px(DISPLAY_PAD_Y))
        .child(
            div()
                .flex_none()
                .min_w_full()
                .w(drawn.width)
                .flex()
                .justify_center()
                .child(
                    canvas(
                        |_, _, _| {},
                        move |bounds, (), window, cx| {
                            math.paint(bounds.origin, em, ink, window, cx)
                        },
                    )
                    .flex_none()
                    .w(drawn.width)
                    .h(drawn.height),
                ),
        )
        .into_any_element()
}
