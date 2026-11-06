use std::hash::{DefaultHasher, Hash, Hasher};
use std::io::Cursor;
use std::ops::Range;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::sync::Arc;
use std::{fs, process};

use desk_core::home::Home;
use desk_core::model::{Session, Tool};
use desk_core::protocol::command::Command;
use desk_core::protocol::{
    AttachedImage, MentionParams, MentionResolvedOutcome, NoParams, request,
};
use desk_ui::components::chat::Object;
use desk_ui::components::composer::{
    MentionGroup, MentionMenu, MentionRow, TraceChip, image_chips, image_token, image_tokens,
};
use desk_ui::components::glyph::Glyph;
use desk_ui::components::hovercard::Subject;
use gpui::{
    App, ClipboardEntry, Context, Entity, EntityInputHandler, Focusable, Global, Image,
    KeyDownEvent, SharedString, Task, WeakEntity, Window,
};

use super::Chat;
use super::cards::card;
use super::items::brief;

const MENU_FILES: usize = 8;
const MENU_TRACES: usize = 6;
const MENU_COMMANDS: usize = 10;
const NOT_FOUND: &str = "not found: tofu has no recorded item with this id";
const AMBIGUOUS: &str = "ambiguous: more than one recorded item ends in this id";
const ASKING: &str = "asking tofu what this points to";
const SAVING: &str = "an image is still being saved";
const IMAGES: &str = "images";
const HOST_READS: [&str; 5] = ["png", "jpg", "jpeg", "gif", "webp"];
const CONVERTED: [&str; 1] = ["bmp"];

pub(super) struct Picture {
    n: usize,
    saved: Saved,
}

enum Saved {
    Saving { _task: Task<()> },
    Done(PathBuf),
}

enum Source {
    Pasted(Image),
    File(PathBuf),
}

#[derive(Clone, PartialEq)]
pub struct Trace {
    pub token: String,
    pub glyph: Glyph,
    pub label: String,
    pub detail: String,
}

pub(super) struct Mention {
    trace: Trace,
    state: Resolve,
}

enum Resolve {
    Asking,
    Found,
    Broken(SharedString),
}

#[derive(Clone)]
enum Pick {
    File(String),
    Trace(Trace),
    Command(Command),
}

#[derive(Clone, Copy, PartialEq)]
enum Trigger {
    At,
    Slash,
}

pub(super) struct Menu {
    trigger: Trigger,
    start: usize,
    end: usize,
    query: String,
    at: usize,
    rows: Vec<Pick>,
}

pub(super) enum List<T> {
    Unlisted,
    Listing,
    Listed(Rc<Vec<T>>),
    Failed,
}

struct Mentioner(WeakEntity<Chat>);

impl Global for Mentioner {}

pub(super) fn aim(chat: &Entity<Chat>, cx: &mut App) {
    cx.set_global(Mentioner(chat.downgrade()));
    let pictured = chat.downgrade();
    let picture = move |token: &str, cx: &App| {
        let (_, n) = image_tokens(token).into_iter().next()?;
        let path = pictured.upgrade()?.read(cx).picture_path(n).map(Arc::from);
        Some(Subject::Image(u32::try_from(n).ok()?, path))
    };
    let area = chat.read(cx).area.clone();
    area.update(cx, |area, cx| area.inline_chips(image_chips, picture, cx));
}

pub fn mention(trace: Trace, window: &mut Window, cx: &mut App) {
    let Some(chat) = cx
        .try_global::<Mentioner>()
        .and_then(|aimed| aimed.0.upgrade())
    else {
        return eprintln!("desk: {} mentioned with no chat to take it", trace.token);
    };
    chat.update(cx, |chat, cx| {
        chat.add_mention(trace, cx);
        chat.area.focus_handle(cx).focus(window, cx);
    });
}

fn typed_token(text: &str) -> Option<(usize, &str)> {
    text.match_indices('[').find_map(|(open, _)| {
        let rest = text.get(open..)?;
        let token = rest.get(..=rest.find(']')?)?;
        let (kind, id) = token
            .strip_prefix('[')?
            .strip_suffix(']')?
            .split_once('#')?;
        let word = |part: &str| {
            !part.is_empty()
                && part
                    .chars()
                    .all(|letter| letter.is_ascii_alphanumeric() || letter == '-')
        };
        let named = kind.starts_with(|letter: char| letter.is_ascii_lowercase());
        (named && word(kind) && word(id)).then_some((open, token))
    })
}

fn units(text: &str) -> usize {
    text.encode_utf16().count()
}

fn prefix(text: &str, caret: usize) -> &str {
    let mut counted = 0;
    let end = text
        .char_indices()
        .find(|(_, letter)| {
            let past = counted >= caret;
            counted += letter.len_utf16();
            past
        })
        .map_or(text.len(), |(at, _)| at);
    text.get(..end).unwrap_or(text)
}

pub(super) fn tool_glyph(name: &str) -> Glyph {
    match name {
        "edit" | "write" => Glyph::Pencil,
        "bash" | "shell" => Glyph::Terminal,
        "read" | "typecheck" => Glyph::File,
        "search" | "symbols" => Glyph::Search,
        "glob" => Glyph::Folder,
        "fetch" | "web_search" => Glyph::Globe,
        "message" => Glyph::Arrow,
        "spawn" | "subagents" => Glyph::Agents,
        "ask_person" => Glyph::Person,
        "plan" => Glyph::Check,
        "test" => Glyph::Beaker,
        "remember" => Glyph::Brain,
        "cron" => Glyph::Cron,
        name if name.starts_with("ticket_") => Glyph::Inbox,
        name if name.starts_with("browser_") => Glyph::Window,
        _ => Glyph::Code,
    }
}

fn first_line(text: &str) -> String {
    text.lines().next().unwrap_or_default().to_owned()
}

fn words(tool: &Tool) -> String {
    let card = card(tool);
    let objects = card.objects.iter().map(Object::text);
    let said: Vec<&str> = tool
        .agent
        .as_deref()
        .into_iter()
        .chain([card.verb.as_ref()])
        .chain(objects)
        .chain(card.meta.as_deref())
        .collect();
    first_line(&said.join(" "))
}

fn session_traces(session: &Session) -> Vec<Trace> {
    let mut found: Vec<(Option<&str>, Trace)> = Vec::new();
    for (id, agent) in &session.agents {
        if let Some(token) = &agent.mention {
            let summary = match brief(agent.report.as_deref().unwrap_or_default()) {
                "" => first_line(&agent.task),
                said => said.to_owned(),
            };
            let trace = Trace {
                token: token.clone(),
                glyph: Glyph::Agents,
                label: id.clone(),
                detail: summary,
            };
            found.push((agent.started_at.as_deref(), trace));
        }
    }
    for tool in session.tools.values() {
        if let Some(token) = &tool.mention {
            let trace = Trace {
                token: token.clone(),
                glyph: tool_glyph(&tool.name),
                label: tool.name.clone(),
                detail: words(tool),
            };
            found.push((tool.started_at.as_deref(), trace));
        }
    }
    for (name, shell) in &session.shells {
        if let Some(token) = &shell.mention {
            let trace = Trace {
                token: token.clone(),
                glyph: Glyph::Terminal,
                label: name.clone(),
                detail: first_line(&shell.command),
            };
            found.push((shell.started_at.as_deref(), trace));
        }
    }
    for (path, edits) in &session.files {
        for edit in edits {
            if let Some(token) = &edit.r#ref {
                let started = session
                    .tools
                    .get(&edit.item)
                    .and_then(|tool| tool.started_at.as_deref());
                let trace = Trace {
                    token: token.clone(),
                    glyph: Glyph::Pencil,
                    label: path.clone(),
                    detail: "edited".to_owned(),
                };
                found.push((started, trace));
            }
        }
    }
    found.sort_by(|(left, _), (right, _)| right.cmp(left));
    let mut traces: Vec<Trace> = Vec::new();
    for (_, trace) in found {
        if !traces.iter().any(|known| known.token == trace.token) {
            traces.push(trace);
        }
    }
    traces
}

fn ranked<'a>(files: &'a [String], query: &str) -> Vec<&'a String> {
    let mut hits: Vec<(u8, usize, &String)> = files
        .iter()
        .filter_map(|path| {
            let lower = path.to_lowercase();
            let name = lower.rsplit('/').next().unwrap_or(&lower);
            let rank = match (name.starts_with(query), name.contains(query)) {
                (true, _) => 0,
                (false, true) => 1,
                (false, false) if lower.contains(query) => 2,
                (false, false) => return None,
            };
            Some((rank, path.len(), path))
        })
        .collect();
    hits.sort();
    hits.into_iter()
        .take(MENU_FILES)
        .map(|(_, _, path)| path)
        .collect()
}

fn commanded<'a>(commands: &'a [Command], query: &str) -> Vec<&'a Command> {
    let mut hits: Vec<(bool, String, &Command)> = commands
        .iter()
        .map(|command| (command.name.trim_start_matches('/').to_lowercase(), command))
        .filter(|(name, _)| name.contains(query))
        .map(|(name, command)| (!name.starts_with(query), name, command))
        .collect();
    hits.sort_by(|left, right| (left.0, &left.1).cmp(&(right.0, &right.1)));
    hits.into_iter()
        .take(MENU_COMMANDS)
        .map(|(_, _, command)| command)
        .collect()
}

fn extension(path: &Path) -> String {
    path.extension()
        .map(|extension| extension.to_string_lossy().to_ascii_lowercase())
        .unwrap_or_default()
}

fn is_image(path: &Path) -> bool {
    let extension = extension(path);
    HOST_READS.contains(&extension.as_str()) || CONVERTED.contains(&extension.as_str())
}

fn store(source: Source, folder: &Path) -> Result<PathBuf, String> {
    let bytes = match source {
        Source::File(path) if HOST_READS.contains(&extension(&path).as_str()) => {
            return match fs::metadata(&path) {
                Ok(_) => Ok(path),
                Err(error) => Err(format!("{}: {error}", path.display())),
            };
        }
        Source::File(path) => {
            fs::read(&path).map_err(|error| format!("{}: {error}", path.display()))?
        }
        Source::Pasted(image) => image.bytes,
    };
    let decoded = image::load_from_memory(&bytes)
        .map_err(|error| format!("the image does not decode: {error}"))?;
    let mut png = Vec::new();
    decoded
        .write_to(&mut Cursor::new(&mut png), image::ImageFormat::Png)
        .map_err(|error| format!("the image does not encode to png: {error}"))?;
    let mut hasher = DefaultHasher::new();
    png.hash(&mut hasher);
    let path = folder.join(format!("{:016x}.png", hasher.finish()));
    fs::create_dir_all(folder).map_err(|error| format!("{}: {error}", folder.display()))?;
    fs::write(&path, png).map_err(|error| format!("{}: {error}", path.display()))?;
    Ok(path)
}

fn git_files(root: &Path) -> Result<Vec<String>, String> {
    let mut command = process::Command::new("git");
    command
        .args([
            "ls-files",
            "--cached",
            "--others",
            "--exclude-standard",
            "-z",
        ])
        .current_dir(root);
    #[cfg(windows)]
    std::os::windows::process::CommandExt::creation_flags(&mut command, super::CREATE_NO_WINDOW);
    let output = command
        .output()
        .map_err(|error| format!("git ls-files in {}: {error}", root.display()))?;
    if !output.status.success() {
        return Err(format!(
            "git ls-files in {} said {}",
            root.display(),
            String::from_utf8_lossy(&output.stderr).trim()
        ));
    }
    Ok(String::from_utf8_lossy(&output.stdout)
        .split('\0')
        .filter(|path| !path.is_empty())
        .map(str::to_owned)
        .collect())
}

impl Chat {
    pub(super) fn add_mention(&mut self, trace: Trace, cx: &mut Context<Self>) {
        if self
            .mentions
            .iter()
            .any(|known| known.trace.token == trace.token)
        {
            return;
        }
        let token = trace.token.clone();
        let params = MentionParams {
            r#ref: token.clone(),
            session: self.open_id().map(str::to_owned),
        };
        self.mentions.push(Mention {
            trace,
            state: Resolve::Asking,
        });
        cx.notify();
        self.request::<request::MentionResolve>(&params, cx, move |chat, reply, cx| {
            let state = match reply {
                Ok(answer) => {
                    eprintln!(
                        "desk: mention.resolve {} answered {} item {} speaker {}",
                        answer.r#ref,
                        String::from(answer.outcome.clone()),
                        answer.item.as_deref().unwrap_or("none"),
                        answer.speaker.as_deref().unwrap_or("none")
                    );
                    match answer.outcome {
                        MentionResolvedOutcome::Item => Resolve::Found,
                        MentionResolvedOutcome::NotFound => Resolve::Broken(NOT_FOUND.into()),
                        MentionResolvedOutcome::Ambiguous => Resolve::Broken(AMBIGUOUS.into()),
                        MentionResolvedOutcome::Unknown(said) => {
                            Resolve::Broken(format!("tofu answered {said}").into())
                        }
                    }
                }
                Err(error) => {
                    eprintln!("desk: mention.resolve {token} failed: {error}");
                    Resolve::Broken(error.into())
                }
            };
            if let Some(known) = chat
                .mentions
                .iter_mut()
                .find(|known| known.trace.token == token)
            {
                known.state = state;
            }
            cx.notify();
        });
    }

    pub(super) fn remove_mention(&mut self, at: usize, cx: &mut Context<Self>) {
        if at < self.mentions.len() {
            self.mentions.remove(at);
            cx.notify();
        }
    }

    pub(super) fn chips(&self) -> Vec<TraceChip> {
        self.mentions
            .iter()
            .map(|known| TraceChip {
                glyph: known.trace.glyph,
                label: known.trace.label.clone().into(),
                broken: match &known.state {
                    Resolve::Asking | Resolve::Found => None,
                    Resolve::Broken(why) => Some(why.clone()),
                },
            })
            .collect()
    }

    pub(super) fn held(&self) -> Option<&'static str> {
        let saving = self
            .pictures
            .iter()
            .any(|picture| matches!(picture.saved, Saved::Saving { .. }));
        if saving {
            return Some(SAVING);
        }
        self.mentions.iter().find_map(|known| match known.state {
            Resolve::Asking => Some(ASKING),
            Resolve::Broken(_) => Some("a mention does not resolve, so the send is held"),
            Resolve::Found => None,
        })
    }

    pub(super) fn picture_path(&self, n: usize) -> Option<&Path> {
        self.pictures
            .iter()
            .find(|picture| picture.n == n)
            .and_then(|picture| match &picture.saved {
                Saved::Done(path) => Some(path.as_path()),
                Saved::Saving { .. } => None,
            })
    }

    pub(super) fn numbered(&self, text: &str) -> (String, Vec<AttachedImage>) {
        let (mut sent, mut images, mut from) = (String::new(), Vec::new(), 0);
        for (range, n) in image_tokens(text) {
            let Some(path) = self.picture_path(n) else {
                continue;
            };
            sent.push_str(text.get(from..range.start).unwrap_or_default());
            images.push(AttachedImage {
                path: path.to_string_lossy().into_owned(),
            });
            sent.push_str(&image_token(images.len()));
            from = range.end;
        }
        sent.push_str(text.get(from..).unwrap_or_default());
        (sent, images)
    }

    fn insert(&mut self, text: &str, window: &mut Window, cx: &mut Context<Self>) {
        self.area.update(cx, |area, cx| {
            area.replace_text_in_range(None, text, window, cx)
        });
    }

    fn paste_image(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        let Some(item) = cx.read_from_clipboard() else {
            return false;
        };
        let entries = item.entries();
        if entries
            .iter()
            .any(|entry| matches!(entry, ClipboardEntry::String(_)))
        {
            return false;
        }
        for entry in entries {
            match entry {
                ClipboardEntry::Image(image) => {
                    eprintln!(
                        "desk: chat paste image {:?} {} bytes",
                        image.format,
                        image.bytes.len()
                    );
                    self.attach_image(Source::Pasted(image.clone()), window, cx);
                    return true;
                }
                ClipboardEntry::ExternalPaths(paths) => {
                    self.attach_paths(paths.paths(), window, cx);
                    return true;
                }
                ClipboardEntry::String(_) => {}
            }
        }
        false
    }

    pub(super) fn attach_paths(
        &mut self,
        paths: &[PathBuf],
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let root = PathBuf::from(&self.project);
        for path in paths {
            if is_image(path) {
                self.attach_image(Source::File(path.clone()), window, cx);
                continue;
            }
            let shown = path
                .strip_prefix(&root)
                .unwrap_or(path)
                .to_string_lossy()
                .replace('\\', "/");
            eprintln!("desk: chat attach {shown}");
            self.insert(&format!("@{shown} "), window, cx);
        }
    }

    fn attach_image(&mut self, source: Source, window: &mut Window, cx: &mut Context<Self>) {
        let folder = match Home::find() {
            Ok(home) => home.state.join(IMAGES),
            Err(error) => return self.fail(format!("the image was not attached: {error}"), cx),
        };
        let n = self
            .pictures
            .iter()
            .map(|picture| picture.n)
            .max()
            .unwrap_or(0)
            + 1;
        self.insert(&image_token(n), window, cx);
        let saving = cx.spawn_in(window, async move |chat, cx| {
            let saved = cx
                .background_executor()
                .spawn(async move { store(source, &folder) })
                .await;
            if let Err(error) =
                chat.update_in(cx, |chat, window, cx| chat.saved(n, saved, window, cx))
            {
                eprintln!("desk: the chat closed before image {n} was saved: {error}");
            }
        });
        self.pictures.push(Picture {
            n,
            saved: Saved::Saving { _task: saving },
        });
        cx.notify();
    }

    fn saved(
        &mut self,
        n: usize,
        saved: Result<PathBuf, String>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let path = match saved {
            Ok(path) => path,
            Err(error) => {
                self.pictures.retain(|picture| picture.n != n);
                let text = self.area.read(cx).text();
                if let Some((range, _)) = image_tokens(&text).into_iter().find(|(_, at)| *at == n) {
                    self.erase(&text, range, window, cx);
                }
                return self.fail(format!("image {n} was not attached: {error}"), cx);
            }
        };
        eprintln!("desk: chat image {n} saved at {}", path.display());
        if let Some(picture) = self.pictures.iter_mut().find(|picture| picture.n == n) {
            picture.saved = Saved::Done(path);
        }
        cx.notify();
    }

    fn erase(
        &mut self,
        text: &str,
        range: Range<usize>,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        let start = units(text.get(..range.start).unwrap_or_default());
        let end = start + units(text.get(range).unwrap_or_default());
        self.area.update(cx, |area, cx| {
            area.replace_text_in_range(Some(start..end), "", window, cx)
        });
    }

    pub(super) fn take_tokens(&mut self) -> Vec<String> {
        self.mentions
            .drain(..)
            .map(|known| known.trace.token)
            .collect()
    }

    pub(super) fn mention_menu(&self) -> Option<MentionMenu> {
        let menu = self.menu.as_ref()?;
        let row = |pick: &Pick| match pick {
            Pick::File(path) => MentionRow {
                glyph: Glyph::File,
                label: path.clone().into(),
                detail: SharedString::default(),
            },
            Pick::Trace(trace) => MentionRow {
                glyph: trace.glyph,
                label: trace.label.clone().into(),
                detail: trace.detail.clone().into(),
            },
            Pick::Command(command) => MentionRow {
                glyph: Glyph::Code,
                label: format!("/{}", command.name.trim_start_matches('/')).into(),
                detail: first_line(&command.summary).into(),
            },
        };
        let group = |title: &str, kept: fn(&Pick) -> bool| MentionGroup {
            title: title.to_owned().into(),
            rows: menu
                .rows
                .iter()
                .filter(|pick| kept(pick))
                .map(row)
                .collect(),
        };
        Some(MentionMenu {
            groups: vec![
                group("Files", |pick| matches!(pick, Pick::File(_))),
                group("Traces", |pick| matches!(pick, Pick::Trace(_))),
                group("Commands", |pick| matches!(pick, Pick::Command(_))),
            ],
            at: menu.at,
        })
    }

    pub(super) fn typed(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let text = self.area.read(cx).text();
        let present = image_tokens(&text);
        self.pictures.retain(|picture| {
            let kept = present.iter().any(|(_, n)| *n == picture.n);
            if !kept {
                eprintln!("desk: chat image {} removed", picture.n);
            }
            kept
        });
        if let Some((open, token)) = typed_token(&text) {
            let start = units(text.get(..open).unwrap_or_default());
            let range = start..start + units(token);
            let trace = Trace {
                token: token.to_owned(),
                glyph: Glyph::At,
                label: token.trim_matches(['[', ']']).to_owned(),
                detail: String::new(),
            };
            self.area.update(cx, |area, cx| {
                area.replace_text_in_range(Some(range), "", window, cx)
            });
            return self.add_mention(trace, cx);
        }
        let caret = self
            .area
            .update(cx, |area, cx| area.selected_text_range(false, window, cx))
            .filter(|selection| selection.range.is_empty())
            .map(|selection| selection.range.end);
        let before = caret.map(|caret| prefix(&text, caret));
        let slash = |before: &str| {
            let query = before.strip_prefix('/')?;
            (!query.contains(char::is_whitespace))
                .then(|| (Trigger::Slash, 0, 1 + units(query), query.to_lowercase()))
        };
        let mentioned = |before: &str| {
            let at = before.rfind('@')?;
            let head = before.get(..at)?;
            let query = before.get(at + 1..)?;
            let free = head.chars().next_back().is_none_or(char::is_whitespace);
            (free && !query.contains(char::is_whitespace)).then(|| {
                let start = units(head);
                (
                    Trigger::At,
                    start,
                    start + 1 + units(query),
                    query.to_lowercase(),
                )
            })
        };
        let opened = before.and_then(|before| slash(before).or_else(|| mentioned(before)));
        if opened.is_none() {
            self.dismissed = None;
        }
        let Some((trigger, start, end, query)) =
            opened.filter(|(_, start, ..)| self.dismissed != Some(*start))
        else {
            if self.menu.take().is_some() {
                cx.notify();
            }
            return;
        };
        if self.dismissed.is_some_and(|dismissed| dismissed != start) {
            self.dismissed = None;
        }
        if self
            .menu
            .as_ref()
            .is_none_or(|menu| menu.start != start || menu.trigger != trigger)
        {
            let key = match trigger {
                Trigger::At => '@',
                Trigger::Slash => '/',
            };
            eprintln!("desk: the {key} menu opens at {start} for {query:?}");
        }
        let at = self
            .menu
            .as_ref()
            .filter(|menu| menu.start == start && menu.query == query)
            .map_or(0, |menu| menu.at);
        self.menu = Some(Menu {
            trigger,
            start,
            end,
            query,
            at,
            rows: Vec::new(),
        });
        self.fill(cx);
    }

    fn fill(&mut self, cx: &mut Context<Self>) {
        let Some(trigger) = self.menu.as_ref().map(|menu| menu.trigger) else {
            return;
        };
        match trigger {
            Trigger::At if matches!(self.files, List::Unlisted) => self.list_files(cx),
            Trigger::Slash if matches!(self.commands, List::Unlisted) => self.list_commands(cx),
            Trigger::At | Trigger::Slash => {}
        }
        let traces = match trigger {
            Trigger::At => self
                .store
                .read(cx)
                .open_session()
                .map_or_else(Vec::new, session_traces),
            Trigger::Slash => Vec::new(),
        };
        let Some(menu) = &mut self.menu else {
            return;
        };
        let query = menu.query.as_str();
        menu.rows = match (trigger, &self.files, &self.commands) {
            (Trigger::Slash, _, List::Listed(commands)) => commanded(commands, query)
                .into_iter()
                .map(|command| Pick::Command(command.clone()))
                .collect(),
            (Trigger::Slash, ..) => Vec::new(),
            (Trigger::At, files, _) => {
                let files = match files {
                    List::Listed(files) => ranked(files, query),
                    List::Unlisted | List::Listing | List::Failed => Vec::new(),
                };
                let traces = traces
                    .into_iter()
                    .filter(|trace| {
                        trace.label.to_lowercase().contains(query)
                            || trace.detail.to_lowercase().contains(query)
                    })
                    .take(MENU_TRACES)
                    .map(Pick::Trace);
                files
                    .into_iter()
                    .map(|path| Pick::File(path.clone()))
                    .chain(traces)
                    .collect()
            }
        };
        menu.at = menu.at.min(menu.rows.len().saturating_sub(1));
        cx.notify();
    }

    fn list_commands(&mut self, cx: &mut Context<Self>) {
        self.commands = List::Listing;
        self.request::<request::QueryCommands>(&NoParams {}, cx, |chat, reply, cx| {
            chat.commands = match reply {
                Ok(listed) => {
                    eprintln!(
                        "desk: the / menu lists {} commands tofu reports",
                        listed.commands.len()
                    );
                    List::Listed(Rc::new(listed.commands))
                }
                Err(error) => {
                    eprintln!("desk: the / menu has no commands: {error}");
                    List::Failed
                }
            };
            chat.fill(cx);
        });
    }

    fn list_files(&mut self, cx: &mut Context<Self>) {
        self.files = List::Listing;
        let root = std::path::PathBuf::from(&self.project);
        self._files = Some(cx.spawn(async move |this, cx| {
            let listed = cx
                .background_executor()
                .spawn(async move { git_files(&root) })
                .await;
            this.update(cx, |chat, cx| {
                chat.files = match listed {
                    Ok(files) => {
                        eprintln!("desk: the @ menu lists {} project files", files.len());
                        List::Listed(Rc::new(files))
                    }
                    Err(error) => {
                        eprintln!("desk: the @ menu has no files: {error}");
                        List::Failed
                    }
                };
                chat.fill(cx);
            })
            .ok();
        }));
    }

    pub(super) fn pick(&mut self, at: usize, window: &mut Window, cx: &mut Context<Self>) {
        let Some(menu) = self.menu.take() else {
            return;
        };
        let Some(pick) = menu.rows.get(at).cloned() else {
            return;
        };
        let typed = match &pick {
            Pick::File(path) => format!("@{path} "),
            Pick::Trace(_) => String::new(),
            Pick::Command(command) => format!("/{} ", command.name.trim_start_matches('/')),
        };
        self.area.update(cx, |area, cx| {
            area.replace_text_in_range(Some(menu.start..menu.end), &typed, window, cx)
        });
        if let Pick::Trace(trace) = pick {
            self.add_mention(trace, cx);
        }
        self.area.focus_handle(cx).focus(window, cx);
        cx.notify();
    }

    pub(super) fn mention_key(
        &mut self,
        event: &KeyDownEvent,
        window: &mut Window,
        cx: &mut Context<Self>,
    ) {
        if !self.area.focus_handle(cx).is_focused(window) {
            return;
        }
        let keys = &event.keystroke;
        let pasting = keys.key == "v" && keys.modifiers.secondary() && !keys.modifiers.shift;
        if pasting && self.paste_image(window, cx) {
            return cx.stop_propagation();
        }
        let key = event.keystroke.key.as_str();
        if let Some(menu) = &mut self.menu {
            let count = menu.rows.len();
            match key {
                "down" if count > 0 => menu.at = (menu.at + 1) % count,
                "up" if count > 0 => menu.at = (menu.at + count - 1) % count,
                "enter" | "tab" if count > 0 => {
                    let at = menu.at;
                    self.pick(at, window, cx);
                }
                "escape" => {
                    self.dismissed = Some(menu.start);
                    self.menu = None;
                }
                _ => return,
            }
            cx.stop_propagation();
            return cx.notify();
        }
        if key != "backspace" || self.mentions.is_empty() {
            return;
        }
        let at_start = self
            .area
            .update(cx, |area, cx| area.selected_text_range(false, window, cx))
            .is_some_and(|selection| selection.range == (0..0));
        if at_start {
            self.mentions.pop();
            cx.stop_propagation();
            cx.notify();
        }
    }
}
