use std::collections::HashMap;
use std::io::Read;
use std::process::{Command, Stdio};
use std::sync::Arc;
use std::thread;
use std::time::Instant;

use gpui::{App, Global, Image, ImageFormat};

const PAGE_CAP: usize = 262_144;
const CHUNK: usize = 8192;
const HEAD_END: &[u8] = b"</head>";
const IMAGE_CAP: &str = "1048576";
const TIMEOUT_SECONDS: &str = "3";
const FETCHED_SCHEMES: [&str; 2] = ["https://", "http://"];
#[cfg(windows)]
const CREATE_NO_WINDOW: u32 = 0x0800_0000;

#[derive(Clone)]
pub struct Meta {
    pub title: Option<String>,
    pub description: Option<String>,
    pub image: Option<Arc<Image>>,
}

#[derive(Clone)]
pub enum Lookup {
    Pending,
    Found(Meta),
    Failed,
}

#[derive(Default)]
struct Cache(HashMap<String, (Lookup, Instant)>);

impl Global for Cache {}

pub fn cached(url: &str, cx: &App) -> Option<(Lookup, Instant)> {
    cx.try_global::<Cache>()
        .and_then(|cache| cache.0.get(url).cloned())
}

fn remember(url: String, lookup: Lookup, cx: &mut App) {
    cx.default_global::<Cache>()
        .0
        .insert(url, (lookup, Instant::now()));
}

pub fn fetch(url: &str, then: impl FnOnce(&mut App) + 'static, cx: &mut App) {
    if cached(url, cx).is_some() {
        return;
    }
    let lower = url.to_ascii_lowercase();
    if !FETCHED_SCHEMES
        .iter()
        .any(|scheme| lower.starts_with(scheme))
    {
        eprintln!("desk: linkmeta {url} failed: only http and https are fetched");
        return remember(url.to_owned(), Lookup::Failed, cx);
    }
    remember(url.to_owned(), Lookup::Pending, cx);
    let (sender, answer) = flume::bounded(1);
    let asked = url.to_owned();
    thread::spawn(move || {
        if sender.send(read_page(&asked)).is_err() {
            eprintln!("desk: linkmeta {asked} answered after the window closed");
        }
    });
    let url = url.to_owned();
    cx.spawn(async move |cx| {
        let looked = match answer.recv_async().await {
            Ok(Ok(meta)) => Lookup::Found(meta),
            Ok(Err(reason)) => {
                eprintln!("desk: linkmeta {url} failed: {reason}");
                Lookup::Failed
            }
            Err(error) => {
                eprintln!("desk: linkmeta {url} failed: {error}");
                Lookup::Failed
            }
        };
        cx.update(|cx| {
            remember(url, looked, cx);
            then(cx);
        });
    })
    .detach();
}

fn curl_command(url: &str) -> Command {
    let mut command = Command::new("curl");
    command.args(["-sSL", "--max-time", TIMEOUT_SECONDS, url]);
    #[cfg(windows)]
    std::os::windows::process::CommandExt::creation_flags(&mut command, CREATE_NO_WINDOW);
    command
}

fn read_head(url: &str) -> Result<Vec<u8>, String> {
    let mut child = curl_command(url)
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|error| format!("curl did not run: {error}"))?;
    let Some(mut stdout) = child.stdout.take() else {
        return Err("curl started without its stdout".to_owned());
    };
    let mut page = Vec::new();
    let mut chunk = [0; CHUNK];
    let cut = loop {
        let read = stdout
            .read(&mut chunk)
            .map_err(|error| format!("reading curl: {error}"))?;
        let Some(fresh) = chunk.get(..read).filter(|fresh| !fresh.is_empty()) else {
            break false;
        };
        let from = page.len().saturating_sub(HEAD_END.len());
        page.extend_from_slice(fresh);
        let ended = page.get(from..).is_some_and(|tail| {
            tail.windows(HEAD_END.len())
                .any(|window| window.eq_ignore_ascii_case(HEAD_END))
        });
        if ended || page.len() >= PAGE_CAP {
            break true;
        }
    };
    if cut {
        page.truncate(PAGE_CAP);
        if let Err(error) = child.kill() {
            eprintln!("desk: linkmeta {url} curl not stopped: {error}");
        }
        if let Err(error) = child.wait() {
            eprintln!("desk: linkmeta {url} curl not reaped: {error}");
        }
        return Ok(page);
    }
    let output = child
        .wait_with_output()
        .map_err(|error| format!("curl did not finish: {error}"))?;
    match output.status.success() {
        true => Ok(page),
        false => Err(format!(
            "curl exited {}: {}",
            output.status,
            String::from_utf8_lossy(&output.stderr).trim()
        )),
    }
}

fn read_image(url: &str) -> Result<Vec<u8>, String> {
    let output = curl_command(url)
        .args(["--max-filesize", IMAGE_CAP])
        .output()
        .map_err(|error| format!("curl did not run: {error}"))?;
    match output.status.success() {
        true => Ok(output.stdout),
        false => Err(format!(
            "curl exited {}: {}",
            output.status,
            String::from_utf8_lossy(&output.stderr).trim()
        )),
    }
}

fn read_page(url: &str) -> Result<Meta, String> {
    let page = String::from_utf8_lossy(&read_head(url)?).into_owned();
    let tags = meta_tags(&page);
    let tag = |keys: &[&str]| {
        keys.iter()
            .find_map(|key| tags.get(*key))
            .map(|text| decode(text))
            .filter(|text| !text.is_empty())
    };
    let title = tag(&["og:title"]).or_else(|| title_tag(&page));
    let description = tag(&["og:description", "description"]);
    let image = tag(&["og:image"]).and_then(|found| {
        let address = absolute(url, &found);
        let bytes = read_image(&address)
            .map_err(|reason| eprintln!("desk: linkmeta image {address} failed: {reason}"))
            .ok()?;
        let Some(format) = image_format(&bytes) else {
            eprintln!("desk: linkmeta image {address} failed: not an image desk reads");
            return None;
        };
        Some(Arc::new(Image::from_bytes(format, bytes)))
    });
    if title.is_none() && description.is_none() && image.is_none() {
        return Err("the page has no title, description or image".to_owned());
    }
    Ok(Meta {
        title,
        description,
        image,
    })
}

fn title_tag(page: &str) -> Option<String> {
    let lower = page.to_ascii_lowercase();
    let open = lower.find("<title")?;
    let start = open + lower.get(open..)?.find('>')? + 1;
    let end = start + lower.get(start..)?.find("</title")?;
    let title = decode(page.get(start..end)?.trim());
    (!title.is_empty()).then_some(title)
}

fn meta_tags(page: &str) -> HashMap<String, String> {
    let lower = page.to_ascii_lowercase();
    let mut tags = HashMap::new();
    let mut from = 0;
    while let Some(found) = lower.get(from..).and_then(|rest| rest.find("<meta")) {
        let start = from + found;
        let Some(end) = lower.get(start..).and_then(|rest| rest.find('>')) else {
            break;
        };
        let tag = page.get(start..start + end).unwrap_or_default();
        from = start + end;
        let key = attribute(tag, "property").or_else(|| attribute(tag, "name"));
        if let (Some(key), Some(content)) = (key, attribute(tag, "content")) {
            tags.entry(key.to_ascii_lowercase()).or_insert(content);
        }
    }
    tags
}

fn attribute(tag: &str, name: &str) -> Option<String> {
    let lower = tag.to_ascii_lowercase();
    let mut from = 0;
    while let Some(found) = lower.get(from..).and_then(|rest| rest.find(name)) {
        let at = from + found;
        from = at + name.len();
        let before = lower.get(..at)?.chars().next_back()?;
        let after = lower.get(from..)?.trim_start();
        if !before.is_whitespace() || !after.starts_with('=') {
            continue;
        }
        let value = after.get(1..)?.trim_start();
        let offset = tag.len() - value.len();
        let quote = value.chars().next()?;
        let text = tag.get(offset..)?;
        return match quote {
            '"' | '\'' => text.get(1..)?.split(quote).next().map(str::to_owned),
            _ => text
                .split(|c: char| c.is_whitespace() || c == '/')
                .next()
                .map(str::to_owned),
        };
    }
    None
}

fn decode(text: &str) -> String {
    [
        ("&quot;", "\""),
        ("&#39;", "'"),
        ("&#x27;", "'"),
        ("&apos;", "'"),
        ("&lt;", "<"),
        ("&gt;", ">"),
        ("&nbsp;", " "),
        ("&amp;", "&"),
    ]
    .iter()
    .fold(text.trim().to_owned(), |text, (entity, plain)| {
        text.replace(entity, plain)
    })
}

fn absolute(page: &str, found: &str) -> String {
    if found.starts_with("http://") || found.starts_with("https://") {
        return found.to_owned();
    }
    let scheme_end = page.find("://").map_or(0, |at| at + 3);
    let host_end = page
        .get(scheme_end..)
        .and_then(|rest| rest.find('/'))
        .map_or(page.len(), |at| scheme_end + at);
    match found.strip_prefix("//") {
        Some(rest) => format!("{}{rest}", page.get(..scheme_end).unwrap_or("https://")),
        None => format!(
            "{}/{}",
            page.get(..host_end).unwrap_or(page),
            found.trim_start_matches('/')
        ),
    }
}

fn image_format(bytes: &[u8]) -> Option<ImageFormat> {
    match bytes {
        [0x89, b'P', b'N', b'G', ..] => Some(ImageFormat::Png),
        [0xFF, 0xD8, ..] => Some(ImageFormat::Jpeg),
        [b'G', b'I', b'F', b'8', ..] => Some(ImageFormat::Gif),
        [
            b'R',
            b'I',
            b'F',
            b'F',
            _,
            _,
            _,
            _,
            b'W',
            b'E',
            b'B',
            b'P',
            ..,
        ] => Some(ImageFormat::Webp),
        _ => None,
    }
}

pub fn host(url: &str) -> &str {
    let rest = url.split_once("://").map_or(url, |(_, rest)| rest);
    rest.split(['/', '?', '#']).next().unwrap_or(rest)
}
