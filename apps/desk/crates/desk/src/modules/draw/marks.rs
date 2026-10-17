use desk_canvas::{Dash, Fill, Pick, Route, Size, Tool};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::paint::glyph;
use gpui::{
    AnyElement, BorderStyle, Bounds, Corners, Edges, PathBuilder, Rgba, canvas, point, prelude::*,
    px, quad, size, transparent_black,
};

pub const ICON_BOX: f32 = 18.0;
const LINE: f32 = 1.5;

type Stroke = &'static [(f32, f32)];

#[derive(Clone, Copy)]
pub enum Mark {
    Glyph(Glyph),
    Lines(&'static [Stroke], f32),
    Ring(f32, f32, f32),
}

const SQUARE: Stroke = &[
    (3.0, 3.0),
    (15.0, 3.0),
    (15.0, 15.0),
    (3.0, 15.0),
    (3.0, 3.0),
];
const CORNERS: [Stroke; 4] = [
    &[(2.0, 6.0), (2.0, 2.0), (6.0, 2.0)],
    &[(12.0, 2.0), (16.0, 2.0), (16.0, 6.0)],
    &[(16.0, 12.0), (16.0, 16.0), (12.0, 16.0)],
    &[(6.0, 16.0), (2.0, 16.0), (2.0, 12.0)],
];

const SELECT: Mark = Mark::Lines(
    &[&[
        (5.0, 3.0),
        (5.0, 15.0),
        (8.5, 11.5),
        (13.0, 11.5),
        (5.0, 3.0),
    ]],
    LINE,
);
const HAND: Mark = Mark::Lines(
    &[
        &[(7.0, 9.0), (7.0, 4.5)],
        &[(9.5, 8.0), (9.5, 3.0)],
        &[(12.0, 8.0), (12.0, 4.0)],
        &[(14.5, 10.0), (14.5, 6.5)],
        &[(7.0, 11.0), (4.5, 8.5)],
        &[
            (7.0, 9.0),
            (7.0, 13.0),
            (9.0, 16.0),
            (13.0, 16.0),
            (14.5, 13.0),
            (14.5, 10.0),
        ],
    ],
    LINE,
);
const RECTANGLE: Mark = Mark::Lines(
    &[&[
        (3.0, 4.5),
        (15.0, 4.5),
        (15.0, 13.5),
        (3.0, 13.5),
        (3.0, 4.5),
    ]],
    LINE,
);
const ARROW: Mark = Mark::Lines(
    &[
        &[(3.0, 15.0), (15.0, 3.0)],
        &[(8.5, 3.0), (15.0, 3.0), (15.0, 9.5)],
    ],
    LINE,
);
const STRAIGHT: Mark = Mark::Lines(&[&[(3.0, 15.0), (15.0, 3.0)]], LINE);
const TEXT: Mark = Mark::Lines(
    &[
        &[(4.0, 4.0), (14.0, 4.0)],
        &[(9.0, 4.0), (9.0, 15.0)],
        &[(7.0, 15.0), (11.0, 15.0)],
    ],
    LINE,
);
const ERASER: Mark = Mark::Lines(
    &[
        &[
            (7.0, 15.0),
            (2.5, 10.5),
            (10.0, 3.0),
            (15.5, 8.5),
            (9.0, 15.0),
            (7.0, 15.0),
        ],
        &[(5.5, 7.5), (11.5, 13.5)],
        &[(9.0, 15.0), (15.5, 15.0)],
    ],
    LINE,
);
const FRAME: Mark = Mark::Lines(
    &[
        &[(5.0, 2.5), (5.0, 15.5)],
        &[(13.0, 2.5), (13.0, 15.5)],
        &[(2.5, 5.0), (15.5, 5.0)],
        &[(2.5, 13.0), (15.5, 13.0)],
    ],
    LINE,
);
const NOTE: Mark = Mark::Lines(
    &[&[
        (3.0, 3.0),
        (15.0, 3.0),
        (15.0, 11.0),
        (11.0, 15.0),
        (3.0, 15.0),
        (3.0, 3.0),
    ]],
    LINE,
);
const LASER: Mark = Mark::Lines(
    &[
        &[(3.0, 15.0), (10.0, 8.0)],
        &[(12.0, 3.0), (12.0, 5.0)],
        &[(15.0, 6.0), (13.0, 6.0)],
        &[(14.5, 3.5), (13.5, 4.5)],
    ],
    LINE,
);

pub const FRONT: Mark = Mark::Lines(
    &[
        &[
            (7.0, 3.0),
            (15.0, 3.0),
            (15.0, 11.0),
            (7.0, 11.0),
            (7.0, 3.0),
        ],
        &[
            (5.0, 7.0),
            (3.0, 7.0),
            (3.0, 15.0),
            (11.0, 15.0),
            (11.0, 13.0),
        ],
    ],
    LINE,
);
pub const BACK: Mark = Mark::Lines(
    &[
        &[
            (3.0, 7.0),
            (11.0, 7.0),
            (11.0, 15.0),
            (3.0, 15.0),
            (3.0, 7.0),
        ],
        &[
            (7.0, 5.0),
            (7.0, 3.0),
            (15.0, 3.0),
            (15.0, 11.0),
            (13.0, 11.0),
        ],
    ],
    LINE,
);
pub const GROUP: Mark = Mark::Lines(
    &[
        CORNERS[0],
        CORNERS[1],
        CORNERS[2],
        CORNERS[3],
        &[
            (6.0, 6.0),
            (12.0, 6.0),
            (12.0, 12.0),
            (6.0, 12.0),
            (6.0, 6.0),
        ],
    ],
    LINE,
);
pub const UNGROUP: Mark = Mark::Lines(
    &[
        &[(2.0, 2.0), (8.0, 2.0), (8.0, 8.0), (2.0, 8.0), (2.0, 2.0)],
        &[
            (10.0, 10.0),
            (16.0, 10.0),
            (16.0, 16.0),
            (10.0, 16.0),
            (10.0, 10.0),
        ],
    ],
    LINE,
);
pub const FIT: Mark = Mark::Lines(&CORNERS, LINE);
pub const ALL: Mark = SELECT;

pub fn of_tool(tool: Tool) -> Mark {
    match tool {
        Tool::Select => SELECT,
        Tool::Hand => HAND,
        Tool::Rectangle => RECTANGLE,
        Tool::Ellipse => Mark::Ring(3.0, 3.0, 12.0),
        Tool::Arrow => ARROW,
        Tool::Line => STRAIGHT,
        Tool::Pencil => Mark::Glyph(Glyph::Pencil),
        Tool::Text => TEXT,
        Tool::Eraser => ERASER,
        Tool::Frame => FRAME,
        Tool::Note => NOTE,
        Tool::Laser => LASER,
    }
}

pub fn of_pick(pick: Pick) -> Mark {
    match pick {
        Pick::Color(_) | Pick::Tint(_) | Pick::Opacity(_) => Mark::Lines(&[SQUARE], LINE),
        Pick::Fill(Fill::None) => Mark::Lines(&[SQUARE, &[(3.0, 15.0), (15.0, 3.0)]], LINE),
        Pick::Fill(Fill::Semi) => Mark::Lines(
            &[
                SQUARE,
                &[(3.0, 11.0), (15.0, 11.0)],
                &[(3.0, 13.0), (15.0, 13.0)],
            ],
            LINE,
        ),
        Pick::Fill(Fill::Solid) => Mark::Lines(
            &[
                SQUARE,
                &[(4.0, 5.0), (14.0, 5.0)],
                &[(4.0, 7.0), (14.0, 7.0)],
                &[(4.0, 9.0), (14.0, 9.0)],
                &[(4.0, 11.0), (14.0, 11.0)],
                &[(4.0, 13.0), (14.0, 13.0)],
            ],
            LINE,
        ),
        Pick::Fill(Fill::Pattern) => Mark::Lines(
            &[
                SQUARE,
                &[(3.0, 9.0), (9.0, 3.0)],
                &[(3.0, 15.0), (15.0, 3.0)],
                &[(9.0, 15.0), (15.0, 9.0)],
            ],
            LINE,
        ),
        Pick::Dash(Dash::Draw) => Mark::Lines(
            &[&[
                (2.0, 11.0),
                (5.0, 7.0),
                (8.0, 11.0),
                (11.0, 7.0),
                (14.0, 11.0),
                (16.0, 8.0),
            ]],
            LINE,
        ),
        Pick::Dash(Dash::Dashed) => Mark::Lines(
            &[
                &[(2.0, 9.0), (6.0, 9.0)],
                &[(8.0, 9.0), (12.0, 9.0)],
                &[(14.0, 9.0), (16.0, 9.0)],
            ],
            LINE,
        ),
        Pick::Dash(Dash::Dotted) => Mark::Lines(
            &[
                &[(3.0, 9.0), (4.0, 9.0)],
                &[(7.0, 9.0), (8.0, 9.0)],
                &[(11.0, 9.0), (12.0, 9.0)],
                &[(15.0, 9.0), (16.0, 9.0)],
            ],
            LINE,
        ),
        Pick::Dash(Dash::Solid) => Mark::Lines(&[&[(2.0, 9.0), (16.0, 9.0)]], LINE),
        Pick::Size(Size::S) => Mark::Lines(&[&[(4.0, 14.0), (14.0, 4.0)]], 1.0),
        Pick::Size(Size::M) => Mark::Lines(&[&[(4.0, 14.0), (14.0, 4.0)]], 2.0),
        Pick::Size(Size::L) => Mark::Lines(&[&[(4.0, 14.0), (14.0, 4.0)]], 3.0),
        Pick::Size(Size::Xl) => Mark::Lines(&[&[(4.0, 14.0), (14.0, 4.0)]], 4.5),
        Pick::Route(Route::Curve) => Mark::Lines(
            &[&[
                (3.0, 15.0),
                (4.5, 10.0),
                (7.0, 6.5),
                (10.5, 4.0),
                (15.0, 3.0),
            ]],
            LINE,
        ),
        Pick::Route(Route::Elbow) => Mark::Lines(
            &[&[(3.0, 15.0), (3.0, 9.0), (15.0, 9.0), (15.0, 3.0)]],
            LINE,
        ),
    }
}

pub fn paint(mark: Mark, color: Rgba) -> AnyElement {
    let lines = match mark {
        Mark::Glyph(mark) => return glyph(mark, ICON_BOX, color).into_any_element(),
        Mark::Lines(strokes, width) => Some((strokes, width)),
        Mark::Ring(..) => None,
    };
    canvas(
        |_, _, _| (),
        move |bounds, (), window, _| {
            let at = |(x, y): (f32, f32)| point(bounds.origin.x + px(x), bounds.origin.y + px(y));
            let Some((strokes, width)) = lines else {
                if let Mark::Ring(left, top, side) = mark {
                    window.paint_quad(quad(
                        Bounds::new(at((left, top)), size(px(side), px(side))),
                        Corners::all(px(side / 2.0)),
                        transparent_black(),
                        Edges::all(px(LINE)),
                        color,
                        BorderStyle::Solid,
                    ));
                }
                return;
            };
            for stroke in strokes {
                let mut path = PathBuilder::stroke(px(width));
                if let Some((first, rest)) = stroke.split_first() {
                    path.move_to(at(*first));
                    for next in rest {
                        path.line_to(at(*next));
                    }
                }
                if let Ok(path) = path.build() {
                    window.paint_path(path, color);
                }
            }
        },
    )
    .size(px(ICON_BOX))
    .into_any_element()
}
