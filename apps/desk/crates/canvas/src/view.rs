use draw_doc::Rect;
use gpui::{Bounds, Pixels, Point, Size, point, px};

pub const ZOOM_LEAST: f64 = 0.1;
pub const ZOOM_MOST: f64 = 8.0;
const FIT_MARGIN: f64 = 40.0;

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct View {
    left: f64,
    top: f64,
    zoom: f64,
}

impl View {
    pub fn new(left: f64, top: f64, zoom: f64) -> Self {
        View {
            left,
            top,
            zoom: zoom.clamp(ZOOM_LEAST, ZOOM_MOST),
        }
    }

    pub fn zoom(&self) -> f64 {
        self.zoom
    }

    pub fn fit(content: Rect, area: Size<Pixels>) -> Self {
        let (width, height) = (f64::from(area.width), f64::from(area.height));
        let room = |side: f64| (side - 2.0 * FIT_MARGIN).max(1.0);
        let spanned = |side: f64| side.max(1.0);
        let zoom = (room(width) / spanned(content.max_x - content.min_x))
            .min(room(height) / spanned(content.max_y - content.min_y))
            .clamp(ZOOM_LEAST, ZOOM_MOST);
        let middle_x = (content.min_x + content.max_x) / 2.0;
        let middle_y = (content.min_y + content.max_y) / 2.0;
        View::new(
            middle_x - width / 2.0 / zoom,
            middle_y - height / 2.0 / zoom,
            zoom,
        )
    }

    pub fn screen(&self, origin: Point<Pixels>, x: f64, y: f64) -> Point<Pixels> {
        point(
            origin.x + px(((x - self.left) * self.zoom) as f32),
            origin.y + px(((y - self.top) * self.zoom) as f32),
        )
    }

    pub fn scene(&self, origin: Point<Pixels>, at: Point<Pixels>) -> (f64, f64) {
        (
            self.left + f64::from(at.x - origin.x) / self.zoom,
            self.top + f64::from(at.y - origin.y) / self.zoom,
        )
    }

    pub fn length(&self, scene: f64) -> Pixels {
        px((scene * self.zoom) as f32)
    }

    pub fn panned(&self, by: Point<Pixels>) -> Self {
        View::new(
            self.left - f64::from(by.x) / self.zoom,
            self.top - f64::from(by.y) / self.zoom,
            self.zoom,
        )
    }

    pub fn zoomed(&self, origin: Point<Pixels>, at: Point<Pixels>, factor: f64) -> Self {
        let (x, y) = self.scene(origin, at);
        let zoom = (self.zoom * factor).clamp(ZOOM_LEAST, ZOOM_MOST);
        View::new(
            x - f64::from(at.x - origin.x) / zoom,
            y - f64::from(at.y - origin.y) / zoom,
            zoom,
        )
    }

    pub fn visible(&self, area: Bounds<Pixels>) -> Rect {
        let (min_x, min_y) = self.scene(area.origin, area.origin);
        let (max_x, max_y) = self.scene(area.origin, area.bottom_right());
        Rect {
            min_x,
            min_y,
            max_x,
            max_y,
        }
    }
}
