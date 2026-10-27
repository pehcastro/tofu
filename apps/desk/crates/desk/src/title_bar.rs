use std::process::Command;
use std::sync::Arc;

use desk_core::control::Control;
use desk_ui::components::title_bar::{
    Github, GithubUser, Marks, Title, TitleBar, TitlePick, WindowKeys,
};
use gpui::{AnyElement, Context, Image, ImageFormat};

use crate::desk::Desk;

#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;
const AVATAR_FETCH_SECONDS: &str = "10";
const AVATAR_PIXELS: u32 = 96;
const CAT_ON_DARK: &[u8] = include_bytes!("../assets/brand/cat-on-dark.png");
const CAT_ON_LIGHT: &[u8] = include_bytes!("../assets/brand/cat-on-light.png");

pub fn marks() -> Marks {
    let mark = |bytes: &[u8]| Arc::new(Image::from_bytes(ImageFormat::Png, bytes.to_vec())).into();
    Marks {
        on_dark: mark(CAT_ON_DARK),
        on_light: mark(CAT_ON_LIGHT),
    }
}

pub struct GhAnswer {
    login: String,
    name: Option<String>,
    picture: Option<(ImageFormat, Vec<u8>)>,
}

pub fn github(answer: Option<GhAnswer>) -> Github {
    let Some(answer) = answer else {
        return Github::SignedOut;
    };
    Github::SignedIn(GithubUser {
        login: answer.login.into(),
        name: answer.name.map(Into::into),
        picture: answer
            .picture
            .map(|(format, bytes)| Arc::new(Image::from_bytes(format, bytes)).into()),
    })
}

fn hidden(program: &str) -> Command {
    let mut command = Command::new(program);
    #[cfg(windows)]
    std::os::windows::process::CommandExt::creation_flags(&mut command, CREATE_NO_WINDOW);
    command
}

fn picture_format(bytes: &[u8]) -> Option<ImageFormat> {
    match bytes {
        [0x89, b'P', b'N', b'G', ..] => Some(ImageFormat::Png),
        [0xFF, 0xD8, ..] => Some(ImageFormat::Jpeg),
        [b'G', b'I', b'F', b'8', ..] => Some(ImageFormat::Gif),
        _ => None,
    }
}

pub fn ask_github() -> Option<GhAnswer> {
    let asked = hidden("gh").args(["api", "user"]).output();
    let user = match asked {
        Ok(output) if output.status.success() => output.stdout,
        Ok(output) => {
            eprintln!(
                "desk: gh api user: {}",
                String::from_utf8_lossy(&output.stderr).trim()
            );
            return None;
        }
        Err(error) => {
            eprintln!("desk: gh api user did not run: {error}");
            return None;
        }
    };
    let user: serde_json::Value = match serde_json::from_slice(&user) {
        Ok(user) => user,
        Err(error) => {
            eprintln!("desk: gh api user answered something that is not json: {error}");
            return None;
        }
    };
    let text = |key: &str| {
        user.get(key)
            .and_then(serde_json::Value::as_str)
            .filter(|text| !text.is_empty())
            .map(str::to_owned)
    };
    let Some(login) = text("login") else {
        eprintln!("desk: gh api user has no login");
        return None;
    };
    let picture = text("avatar_url").and_then(|url| {
        let fetched = hidden("curl")
            .args(["-sfL", "--max-time", AVATAR_FETCH_SECONDS])
            .arg(format!(
                "{url}{}s={AVATAR_PIXELS}",
                if url.contains('?') { '&' } else { '?' }
            ))
            .output();
        match fetched {
            Ok(output) if output.status.success() => {
                let format = picture_format(&output.stdout);
                if format.is_none() {
                    eprintln!("desk: the avatar at {url} is not an image desk reads");
                }
                format.map(|format| (format, output.stdout))
            }
            Ok(output) => {
                eprintln!("desk: curl {url} exited {}", output.status);
                None
            }
            Err(error) => {
                eprintln!("desk: curl did not run: {error}");
                None
            }
        }
    });
    eprintln!(
        "desk: gh user {login}, picture {} bytes",
        picture.as_ref().map_or(0, |(_, bytes)| bytes.len())
    );
    Some(GhAnswer {
        login,
        name: text("name"),
        picture,
    })
}

pub fn title_bar(
    sidebar_open: bool,
    tabs_x: f32,
    github: Github,
    marks: &Marks,
    tabs: Option<AnyElement>,
    cx: &mut Context<Desk>,
) -> TitleBar {
    let title = Title {
        sidebar_open,
        palette_keys: Control::Palette.label().into(),
        github,
        keys: WindowKeys::Live,
        open: None,
        tabs_x,
    };
    TitleBar::new(
        "title-bar",
        title,
        tabs,
        cx.listener(|desk, pick: &TitlePick, window, cx| match pick {
            TitlePick::Sidebar => desk.toggle_sidebar(cx),
            TitlePick::Palette => desk.open_palette(window, cx),
            TitlePick::NewWorkspace => desk.tell(Control::NewWorkspace, cx),
            TitlePick::Theme => desk.toggle_theme(window, cx),
            TitlePick::Close => desk.close_window(window, cx),
        }),
    )
    .marks(marks.clone())
}
