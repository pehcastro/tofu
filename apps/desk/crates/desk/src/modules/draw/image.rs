use std::fs;
use std::path::Path;

use desk_canvas::Canvas;
use gpui::{Context, Image, ImageFormat, PathPromptOptions, Pixels, Point, Window};

use super::{Draw, write};

const IMAGES: &str = "images";

fn extension(format: ImageFormat) -> &'static str {
    match format {
        ImageFormat::Png => "png",
        ImageFormat::Jpeg => "jpg",
        ImageFormat::Webp => "webp",
        ImageFormat::Gif => "gif",
        ImageFormat::Svg => "svg",
        ImageFormat::Bmp => "bmp",
        ImageFormat::Tiff => "tiff",
        ImageFormat::Ico => "ico",
        ImageFormat::Pnm => "pnm",
    }
}

fn format_of(path: &Path) -> Option<ImageFormat> {
    let extension = path.extension()?.to_str()?.to_ascii_lowercase();
    Some(match extension.as_str() {
        "png" => ImageFormat::Png,
        "jpg" | "jpeg" => ImageFormat::Jpeg,
        "webp" => ImageFormat::Webp,
        "gif" => ImageFormat::Gif,
        "svg" => ImageFormat::Svg,
        "bmp" => ImageFormat::Bmp,
        "tif" | "tiff" => ImageFormat::Tiff,
        "ico" => ImageFormat::Ico,
        "pbm" | "ppm" | "pgm" | "pnm" => ImageFormat::Pnm,
        _ => return None,
    })
}

impl Draw {
    pub(super) fn paste_image(&mut self, image: &Image, at: Point<Pixels>, cx: &mut Context<Self>) {
        self.land_image(
            image,
            |canvas, file, size, cx| canvas.insert_image_at_pointer(file, size, at, cx),
            cx,
        );
    }

    fn land_image(
        &mut self,
        image: &Image,
        insert: impl FnOnce(&mut Canvas, &str, (f64, f64), &mut Context<Canvas>),
        cx: &mut Context<Self>,
    ) {
        if let Err(error) = self.keep_image(image, insert, cx) {
            self.fail(format!("image paste: {error}"));
            cx.notify();
        }
    }

    fn keep_image(
        &mut self,
        image: &Image,
        insert: impl FnOnce(&mut Canvas, &str, (f64, f64), &mut Context<Canvas>),
        cx: &mut Context<Self>,
    ) -> Result<(), String> {
        let folder = self.folder.clone()?;
        let stored = format!("{:016x}.{}", image.id, extension(image.format));
        let path = folder.join(IMAGES).join(&stored);
        let file = format!("{IMAGES}/{stored}");
        let decoded = image
            .to_image_data(cx.svg_renderer())
            .map_err(|error| format!("the image does not decode: {error}"))?;
        let pixels = decoded.size(0);
        if !path.exists() {
            write(self.plugin, &path, &image.bytes)?;
        }
        let size = (
            f64::from(pixels.width.0.max(1)),
            f64::from(pixels.height.0.max(1)),
        );
        self.canvas
            .update(cx, |canvas, cx| insert(canvas, &file, size, cx));
        self.save(cx);
        Ok(())
    }

    pub(super) fn insert_image_from_file(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        eprintln!("desk: draw insert image: the file dialog opens");
        let asked = cx.prompt_for_paths(PathPromptOptions {
            files: true,
            directories: false,
            multiple: false,
            prompt: Some("Insert image".into()),
        });
        cx.spawn_in(window, async move |draw, cx| {
            let path = match asked.await {
                Ok(Ok(Some(mut picked))) => picked.pop(),
                Ok(Ok(None)) => None,
                Ok(Err(error)) => {
                    eprintln!("desk: draw: the file dialog failed: {error}");
                    None
                }
                Err(_) => {
                    eprintln!("desk: draw: the file dialog closed without an answer");
                    None
                }
            };
            let Some(path) = path else {
                return eprintln!("desk: draw: no image was picked");
            };
            let Some(format) = format_of(&path) else {
                return eprintln!("desk: draw: {} is not an image", path.display());
            };
            eprintln!("desk: draw insert image {}", path.display());
            let read = cx.background_executor().spawn({
                let path = path.clone();
                async move { fs::read(path) }
            });
            let landed = match read.await {
                Ok(bytes) => draw.update(cx, |draw, cx| {
                    draw.land_image(
                        &Image::from_bytes(format, bytes),
                        Canvas::insert_image_at_view_centre,
                        cx,
                    )
                }),
                Err(error) => draw.update(cx, |draw, cx| {
                    draw.fail(format!("{}: {error}", path.display()));
                    cx.notify();
                }),
            };
            if landed.is_err() {
                eprintln!("desk: draw closed before the image landed");
            }
        })
        .detach();
    }
}
