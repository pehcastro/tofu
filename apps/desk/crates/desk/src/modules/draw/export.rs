use std::path::PathBuf;

use gpui::{App, Bounds, Context, DevicePixels, Pixels, Window, point, px, size};

use super::{Draw, EXPORT_MOST_PX, EXPORT_PAD, Export, Menu, Note, name, permit, write};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Format {
    Png,
    Svg,
    Mermaid,
    Html,
}

impl Format {
    pub const ALL: [Format; 4] = [Format::Png, Format::Svg, Format::Mermaid, Format::Html];

    pub fn label(self) -> &'static str {
        match self {
            Format::Png => "PNG",
            Format::Svg => "SVG",
            Format::Mermaid => "Mermaid",
            Format::Html => "HTML",
        }
    }

    fn extension(self) -> &'static str {
        match self {
            Format::Png => "png",
            Format::Svg => "svg",
            Format::Mermaid => "mmd",
            Format::Html => "html",
        }
    }
}

impl Draw {
    pub(super) fn export_as(&mut self, format: Format, cx: &mut Context<Self>) {
        self.menu = Menu::Closed;
        if format == Format::Png {
            self.export = Export::Asked;
            return cx.notify();
        }
        self.note = Some(match self.export_text(format, cx) {
            Ok(path) => Note::Done(format!("Exported {}", path.display()).into()),
            Err(error) => {
                eprintln!("desk: draw: export {}: {error}", format.label());
                Note::Problem(format!("Export failed: {error}").into())
            }
        });
        cx.notify();
    }

    fn export_text(&self, format: Format, cx: &App) -> Result<PathBuf, String> {
        let board = self.board.as_ref().ok_or("no board is open to export")?;
        let path = board.with_extension(format.extension());
        let canvas = self.canvas.read(cx);
        let (scene, ground) = (canvas.scene(), canvas.backdrop().color.as_deref());
        let text = match format {
            Format::Svg => draw_doc::svg(scene, ground),
            Format::Html => draw_doc::html(scene, &name(board), ground),
            Format::Mermaid => draw_doc::mermaid(scene).map_err(|error| error.to_string())?,
            Format::Png => return Err("a PNG is captured from the screen, not written".into()),
        };
        write(self.plugin, &path, text.as_bytes())?;
        eprintln!(
            "desk: draw exported {} {} bytes",
            path.display(),
            text.len()
        );
        Ok(path)
    }

    fn export_png(&self, window: &mut Window, cx: &App) -> Result<PathBuf, String> {
        let path = self
            .board
            .as_ref()
            .ok_or("no board is open to export")?
            .with_extension(Format::Png.extension());
        let drawn = self
            .canvas
            .read(cx)
            .drawn(px(EXPORT_PAD))
            .ok_or("nothing is drawn in view to export")?;
        permit(self.plugin, &path)?;
        let scale = window.scale_factor();
        let device = |logical: Pixels| DevicePixels((f32::from(logical) * scale).round() as i32);
        let region = Bounds::new(
            point(device(drawn.origin.x), device(drawn.origin.y)),
            size(device(drawn.size.width), device(drawn.size.height)),
        );
        let (width, height) = desk_perf::grab_region(window, &path, region, EXPORT_MOST_PX)
            .map_err(|error| error.to_string())?;
        eprintln!("desk: draw exported {} {width}x{height}", path.display());
        Ok(path)
    }

    pub(super) fn hide_for_export(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.export = Export::Hidden;
        let this = cx.weak_entity();
        window.on_next_frame(move |window, cx| {
            let done = this.update(cx, |draw, cx| {
                draw.export = Export::Idle;
                draw.note = Some(match draw.export_png(window, cx) {
                    Ok(path) => Note::Done(format!("Exported {}", path.display()).into()),
                    Err(error) => {
                        eprintln!("desk: draw: export: {error}");
                        Note::Problem(format!("Export failed: {error}").into())
                    }
                });
                cx.notify();
            });
            if let Err(error) = done {
                eprintln!("desk: draw: export: the draw tile is gone: {error}");
            }
        });
    }
}
