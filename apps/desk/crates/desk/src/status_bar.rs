use desk_core::control::Control;
use desk_ui::components::status_bar::{Status, StatusBar, StatusPick};
use desk_ui::theme::ThemeError;
use gpui::Context;

use crate::desk::Desk;

fn problem_line(problems: &[ThemeError]) -> Option<String> {
    let first = problems.first()?;
    Some(match problems.len() {
        1 => first.to_string(),
        count => format!("{first} (and {} more)", count - 1),
    })
}

pub fn status_bar(problems: &[ThemeError], cx: &mut Context<Desk>) -> StatusBar {
    let status = Status {
        problem: problem_line(problems).map(Into::into),
        ..Status::default()
    };
    StatusBar::new(
        "status-bar",
        status,
        cx.listener(|desk, pick: &StatusPick, _, cx| {
            let control = match pick {
                StatusPick::Branch => Control::Branch,
                StatusPick::Session => Control::Session,
                StatusPick::Context => Control::Context,
                StatusPick::Quota => Control::Quota,
                StatusPick::Classifier => Control::Classifier,
                StatusPick::Cron => Control::Cron,
            };
            desk.tell(control, cx)
        }),
    )
}
