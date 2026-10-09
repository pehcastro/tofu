use std::borrow::Cow;

use gpui::SharedString;

use crate::components::size::{RING_STROKE, SPIN_SIZES};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Glyph {
    Pin,
    Lock,
    Check,
    Trace,
    Chevron,
    Attach,
    Send,
    File,
    Terminal,
    Chat,
    Agents,
    Pencil,
    Window,
    Cron,
    Talk,
    Code,
    Bug,
    Rocket,
    Beaker,
    Book,
    Globe,
    Database,
    Star,
    Sparkle,
    Folder,
    Settings,
    Person,
    Palette,
    Branch,
    BarChart,
    Brain,
    Gauge,
}

impl Glyph {
    const ALL: [Glyph; 32] = [
        Glyph::Pin,
        Glyph::Lock,
        Glyph::Check,
        Glyph::Trace,
        Glyph::Chevron,
        Glyph::Attach,
        Glyph::Send,
        Glyph::File,
        Glyph::Terminal,
        Glyph::Chat,
        Glyph::Agents,
        Glyph::Pencil,
        Glyph::Window,
        Glyph::Cron,
        Glyph::Talk,
        Glyph::Code,
        Glyph::Bug,
        Glyph::Rocket,
        Glyph::Beaker,
        Glyph::Book,
        Glyph::Globe,
        Glyph::Database,
        Glyph::Star,
        Glyph::Sparkle,
        Glyph::Folder,
        Glyph::Settings,
        Glyph::Person,
        Glyph::Palette,
        Glyph::Branch,
        Glyph::BarChart,
        Glyph::Brain,
        Glyph::Gauge,
    ];

    pub const WORKSPACE: [(&'static str, Glyph); 12] = [
        ("chat", Glyph::Talk),
        ("code", Glyph::Code),
        ("terminal", Glyph::Terminal),
        ("bug", Glyph::Bug),
        ("rocket", Glyph::Rocket),
        ("beaker", Glyph::Beaker),
        ("book", Glyph::Book),
        ("globe", Glyph::Globe),
        ("database", Glyph::Database),
        ("star", Glyph::Star),
        ("sparkle", Glyph::Sparkle),
        ("folder", Glyph::Folder),
    ];

    pub fn workspace(name: &str) -> Option<Glyph> {
        Glyph::WORKSPACE
            .iter()
            .find(|(known, _)| *known == name)
            .map(|(_, glyph)| *glyph)
    }

    pub fn path(self) -> &'static str {
        match self {
            Glyph::Pin => "glyphs/pin.svg",
            Glyph::Lock => "glyphs/lock.svg",
            Glyph::Check => "glyphs/check.svg",
            Glyph::Trace => "glyphs/trace.svg",
            Glyph::Chevron => "glyphs/chevron.svg",
            Glyph::Attach => "glyphs/attach.svg",
            Glyph::Send => "glyphs/send.svg",
            Glyph::File => "glyphs/file.svg",
            Glyph::Terminal => "glyphs/terminal.svg",
            Glyph::Chat => "glyphs/chat.svg",
            Glyph::Agents => "glyphs/agents.svg",
            Glyph::Pencil => "glyphs/pencil.svg",
            Glyph::Window => "glyphs/window.svg",
            Glyph::Cron => "glyphs/cron.svg",
            Glyph::Talk => "glyphs/talk.svg",
            Glyph::Code => "glyphs/code.svg",
            Glyph::Bug => "glyphs/bug.svg",
            Glyph::Rocket => "glyphs/rocket.svg",
            Glyph::Beaker => "glyphs/beaker.svg",
            Glyph::Book => "glyphs/book.svg",
            Glyph::Globe => "glyphs/globe.svg",
            Glyph::Database => "glyphs/database.svg",
            Glyph::Star => "glyphs/star.svg",
            Glyph::Sparkle => "glyphs/sparkle.svg",
            Glyph::Folder => "glyphs/folder.svg",
            Glyph::Settings => "glyphs/settings.svg",
            Glyph::Person => "glyphs/person.svg",
            Glyph::Palette => "glyphs/palette.svg",
            Glyph::Branch => "glyphs/branch.svg",
            Glyph::BarChart => "glyphs/bar-chart.svg",
            Glyph::Brain => "glyphs/brain.svg",
            Glyph::Gauge => "glyphs/gauge.svg",
        }
    }

    fn svg(self) -> &'static str {
        match self {
            Glyph::Pin => include_str!("../../assets/icons/pin.svg"),
            Glyph::Lock => include_str!("../../assets/icons/lock.svg"),
            Glyph::Check => include_str!("../../assets/icons/checkmark.svg"),
            Glyph::Trace => include_str!("../../assets/icons/pulse.svg"),
            Glyph::Chevron => include_str!("../../assets/icons/chevron-down.svg"),
            Glyph::Attach => include_str!("../../assets/icons/attach.svg"),
            Glyph::Send => include_str!("../../assets/icons/arrow-up.svg"),
            Glyph::File => include_str!("../../assets/icons/file.svg"),
            Glyph::Terminal => include_str!("../../assets/icons/terminal.svg"),
            Glyph::Chat => include_str!("../../assets/icons/comment.svg"),
            Glyph::Agents => include_str!("../../assets/icons/person-multiple.svg"),
            Glyph::Pencil => include_str!("../../assets/icons/pencil.svg"),
            Glyph::Window => include_str!("../../assets/icons/window.svg"),
            Glyph::Cron => include_str!("../../assets/icons/clock-repeat.svg"),
            Glyph::Talk => include_str!("../../assets/icons/chat.svg"),
            Glyph::Code => include_str!("../../assets/icons/code.svg"),
            Glyph::Bug => include_str!("../../assets/icons/bug.svg"),
            Glyph::Rocket => include_str!("../../assets/icons/rocket.svg"),
            Glyph::Beaker => include_str!("../../assets/icons/beaker.svg"),
            Glyph::Book => include_str!("../../assets/icons/book.svg"),
            Glyph::Globe => include_str!("../../assets/icons/globe.svg"),
            Glyph::Database => include_str!("../../assets/icons/database.svg"),
            Glyph::Star => include_str!("../../assets/icons/star.svg"),
            Glyph::Sparkle => include_str!("../../assets/icons/sparkle.svg"),
            Glyph::Folder => include_str!("../../assets/icons/folder.svg"),
            Glyph::Settings => include_str!("../../assets/icons/settings.svg"),
            Glyph::Person => include_str!("../../assets/icons/person.svg"),
            Glyph::Palette => include_str!("../../assets/icons/color-palette.svg"),
            Glyph::Branch => include_str!("../../assets/icons/branch-fork.svg"),
            Glyph::BarChart => include_str!("../../assets/icons/bar-chart.svg"),
            Glyph::Brain => include_str!("../../assets/icons/brain.svg"),
            Glyph::Gauge => include_str!("../../assets/icons/gauge.svg"),
        }
    }

    pub fn entries() -> Vec<(String, Cow<'static, [u8]>)> {
        let glyphs = Glyph::ALL.iter().map(|glyph| {
            (
                glyph.path().to_owned(),
                Cow::Borrowed(glyph.svg().as_bytes()),
            )
        });
        let marks = SPIN_SIZES.map(|size| {
            let size = f32::from(size);
            (
                spinner_path(size).to_string(),
                Cow::Owned(spinner_svg(size).into_bytes()),
            )
        });
        glyphs.chain(marks).collect()
    }
}

pub const MARK_BLEED: f32 = 1.0;

pub fn spinner_path(size: f32) -> SharedString {
    let size = size
        .round()
        .clamp(f32::from(*SPIN_SIZES.start()), f32::from(*SPIN_SIZES.end()));
    format!("glyphs/arc-{size}.svg").into()
}

fn spinner_svg(size: f32) -> String {
    let radius = (size - RING_STROKE) / 2.0;
    let center = size / 2.0 + MARK_BLEED;
    let canvas = size + 2.0 * MARK_BLEED;
    let reach = radius * std::f32::consts::FRAC_1_SQRT_2;
    let (left, right, top) = (center - reach, center + reach, center - reach);
    format!(
        r##"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {canvas} {canvas}" fill="none" stroke="#000" stroke-width="{RING_STROKE}"><path d="M{left} {top}A{radius} {radius} 0 0 1 {right} {top}"/></svg>"##
    )
}
