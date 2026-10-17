use std::borrow::Cow;

use gpui::SharedString;

use crate::components::priority_icon::Priority;
use crate::components::size::{RING_STROKE, SPIN_SIZES};
use crate::components::status_icon::TicketStatus;

macro_rules! stroked {
    ($body:literal) => {
        concat!(
            r#"<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5">"#,
            $body,
            "</svg>"
        )
    };
}

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
    FileAdd,
    FolderAdd,
    Reveal,
    Cut,
    Copy,
    Duplicate,
    Paste,
    Undo,
    Redo,
    Link,
    Relative,
    ExpandAll,
    CollapseAll,
    Trash,
    Inbox,
    Filter,
    Display,
    Ellipsis,
    Label,
    Milestone,
    Sprint,
    Epic,
    Attention,
    Catchup,
    Image,
    Sun,
    Moon,
}

impl Glyph {
    const ALL: [Glyph; 59] = [
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
        Glyph::FileAdd,
        Glyph::FolderAdd,
        Glyph::Reveal,
        Glyph::Cut,
        Glyph::Copy,
        Glyph::Duplicate,
        Glyph::Paste,
        Glyph::Undo,
        Glyph::Redo,
        Glyph::Link,
        Glyph::Relative,
        Glyph::ExpandAll,
        Glyph::CollapseAll,
        Glyph::Trash,
        Glyph::Inbox,
        Glyph::Filter,
        Glyph::Display,
        Glyph::Ellipsis,
        Glyph::Label,
        Glyph::Milestone,
        Glyph::Sprint,
        Glyph::Epic,
        Glyph::Attention,
        Glyph::Catchup,
        Glyph::Image,
        Glyph::Sun,
        Glyph::Moon,
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
            Glyph::FileAdd => "glyphs/file-add.svg",
            Glyph::FolderAdd => "glyphs/folder-add.svg",
            Glyph::Reveal => "glyphs/reveal.svg",
            Glyph::Cut => "glyphs/cut.svg",
            Glyph::Copy => "glyphs/copy.svg",
            Glyph::Duplicate => "glyphs/duplicate.svg",
            Glyph::Paste => "glyphs/paste.svg",
            Glyph::Undo => "glyphs/undo.svg",
            Glyph::Redo => "glyphs/redo.svg",
            Glyph::Link => "glyphs/link.svg",
            Glyph::Relative => "glyphs/relative.svg",
            Glyph::ExpandAll => "glyphs/expand-all.svg",
            Glyph::CollapseAll => "glyphs/collapse-all.svg",
            Glyph::Trash => "glyphs/trash.svg",
            Glyph::Inbox => "glyphs/inbox.svg",
            Glyph::Filter => "glyphs/filter.svg",
            Glyph::Display => "glyphs/display.svg",
            Glyph::Ellipsis => "glyphs/ellipsis.svg",
            Glyph::Label => "glyphs/label.svg",
            Glyph::Milestone => "glyphs/milestone.svg",
            Glyph::Sprint => "glyphs/sprint.svg",
            Glyph::Epic => "glyphs/epic.svg",
            Glyph::Attention => "glyphs/attention.svg",
            Glyph::Catchup => "glyphs/catchup.svg",
            Glyph::Image => "glyphs/image.svg",
            Glyph::Sun => "glyphs/sun.svg",
            Glyph::Moon => "glyphs/moon.svg",
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
            Glyph::FileAdd => stroked!(
                r#"<path d="M14 3H7.5A2.5 2.5 0 0 0 5 5.5v13A2.5 2.5 0 0 0 7.5 21h9a2.5 2.5 0 0 0 2.5-2.5V8z"/><path d="M14 3v5h5M12 11v6M9 14h6"/>"#
            ),
            Glyph::FolderAdd => stroked!(
                r#"<path d="M3 7.5A2.5 2.5 0 0 1 5.5 5h3.6l2 2.5h7.4A2.5 2.5 0 0 1 21 10v7.5a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 17.5z"/><path d="M12 11v6M9 14h6"/>"#
            ),
            Glyph::Reveal => stroked!(
                r#"<path d="M13 4h7v7M20 4l-9 9"/><path d="M18 14v4.5a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 4 18.5v-11A1.5 1.5 0 0 1 5.5 6H10"/>"#
            ),
            Glyph::Cut => stroked!(
                r#"<circle cx="6" cy="18" r="3"/><circle cx="18" cy="18" r="3"/><path d="M8.1 15.9 19 4M15.9 15.9 5 4"/>"#
            ),
            Glyph::Copy => stroked!(
                r#"<rect x="8" y="8" width="12" height="12" rx="2.5"/><path d="M16 8V6.5A2.5 2.5 0 0 0 13.5 4h-7A2.5 2.5 0 0 0 4 6.5v7A2.5 2.5 0 0 0 6.5 16H8"/>"#
            ),
            Glyph::Duplicate => stroked!(
                r#"<rect x="8" y="8" width="12" height="12" rx="2.5"/><path d="M16 8V6.5A2.5 2.5 0 0 0 13.5 4h-7A2.5 2.5 0 0 0 4 6.5v7A2.5 2.5 0 0 0 6.5 16H8M14 11v6M11 14h6"/>"#
            ),
            Glyph::Paste => stroked!(
                r#"<path d="M9 4.5H7.5A2.5 2.5 0 0 0 5 7v11.5A2.5 2.5 0 0 0 7.5 21h9a2.5 2.5 0 0 0 2.5-2.5V7a2.5 2.5 0 0 0-2.5-2.5H15"/><rect x="9" y="3" width="6" height="3.5" rx="1"/>"#
            ),
            Glyph::Undo => {
                stroked!(r#"<path d="M9 14 4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 0 11H11"/>"#)
            }
            Glyph::Redo => {
                stroked!(r#"<path d="m15 14 5-5-5-5"/><path d="M20 9H9.5a5.5 5.5 0 0 0 0 11H13"/>"#)
            }
            Glyph::Link => stroked!(
                r#"<path d="M10 13.5a4 4 0 0 0 5.7.3l3-3a4 4 0 0 0-5.7-5.7l-1.6 1.6"/><path d="M14 10.5a4 4 0 0 0-5.7-.3l-3 3a4 4 0 0 0 5.7 5.7l1.6-1.6"/>"#
            ),
            Glyph::Relative => {
                stroked!(r#"<path d="M5 4v7a4 4 0 0 0 4 4h11"/><path d="m15 10 5 5-5 5"/>"#)
            }
            Glyph::ExpandAll => stroked!(r#"<path d="m7 15 5 5 5-5M7 9l5-5 5 5"/>"#),
            Glyph::CollapseAll => stroked!(r#"<path d="m7 20 5-5 5 5M7 4l5 5 5-5"/>"#),
            Glyph::Trash => stroked!(
                r#"<path d="M4 7h16M10 11v6M14 11v6"/><path d="M6 7l1 11.5A2.5 2.5 0 0 0 9.5 21h5a2.5 2.5 0 0 0 2.5-2.5L18 7"/><path d="M9 7V4.5A1.5 1.5 0 0 1 10.5 3h3A1.5 1.5 0 0 1 15 4.5V7"/>"#
            ),
            Glyph::Inbox => stroked!(
                r#"<path d="M4 13.5V18a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4.5"/><path d="M4 13.5 6.4 5.5A2 2 0 0 1 8.3 4h7.4a2 2 0 0 1 1.9 1.5l2.4 8h-4.5l-1 2.5h-5l-1-2.5z"/>"#
            ),
            Glyph::Filter => stroked!(r#"<path d="M4 7h16M7 12h10M10 17h4"/>"#),
            Glyph::Display => stroked!(
                r#"<path d="M4 8h8M18 8h2M4 16h2M12 16h8"/><circle cx="15" cy="8" r="2.5"/><circle cx="9" cy="16" r="2.5"/>"#
            ),
            Glyph::Ellipsis => stroked!(
                r#"<circle cx="6" cy="12" r="1.25" fill="currentColor"/><circle cx="12" cy="12" r="1.25" fill="currentColor"/><circle cx="18" cy="12" r="1.25" fill="currentColor"/>"#
            ),
            Glyph::Label => stroked!(
                r#"<path d="M3.5 11.6V5.5a2 2 0 0 1 2-2h6.1a2 2 0 0 1 1.4.6l7.2 7.2a2 2 0 0 1 0 2.8l-6.1 6.1a2 2 0 0 1-2.8 0l-7.2-7.2a2 2 0 0 1-.6-1.4z"/><circle cx="8.5" cy="8.5" r="1.5"/>"#
            ),
            Glyph::Milestone => stroked!(
                r#"<path d="M12 3.5 20.5 12 12 20.5 3.5 12z"/><path d="M12 9 15 12 12 15 9 12z" fill="currentColor"/>"#
            ),
            Glyph::Sprint => stroked!(
                r#"<path d="M19.5 12a7.5 7.5 0 0 1-13.4 4.6M4.5 12a7.5 7.5 0 0 1 13.4-4.6"/><path d="M18.5 3.5v4h-4M5.5 20.5v-4h4"/>"#
            ),
            Glyph::Epic => stroked!(
                r#"<path d="M12 3 20 7.5v9L12 21l-8-4.5v-9z"/><path d="M4 7.5 12 12l8-4.5M12 12v9"/>"#
            ),
            Glyph::Attention => stroked!(
                r#"<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5v5"/><circle cx="12" cy="16" r="1" fill="currentColor"/>"#
            ),
            Glyph::Catchup => stroked!(
                r#"<path d="M4.5 12a7.5 7.5 0 1 0 2.2-5.3"/><path d="M4.5 3.5v3.7h3.7"/><path d="M12 8v4l2.5 2.5"/>"#
            ),
            Glyph::Image => stroked!(
                r#"<rect x="3.5" y="4.5" width="17" height="15" rx="2.5"/><circle cx="9" cy="10" r="1.75"/><path d="m20.5 16-4.5-4.5L6 19.5"/>"#
            ),
            Glyph::Sun => stroked!(
                r#"<circle cx="12" cy="12" r="4"/><path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M5.3 18.7l1.4-1.4M17.3 6.7l1.4-1.4"/>"#
            ),
            Glyph::Moon => stroked!(r#"<path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z"/>"#),
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
        glyphs
            .chain(marks)
            .chain(TicketStatus::entries())
            .chain(Priority::entries())
            .collect()
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
