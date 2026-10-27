use std::collections::HashMap;
use std::ffi::{OsStr, OsString};
use std::fs;
use std::future::Future;
use std::hash::{DefaultHasher, Hash, Hasher};
use std::io::ErrorKind;
use std::panic::Location;
use std::path::{Component, Path, PathBuf};
use std::sync::{Arc, Condvar, Mutex};
use std::thread;
use std::time::{Duration, Instant, SystemTime};

use desk_motion::tokens::{PANEL_IN_MS, PANEL_OUT_MS};
use desk_motion::{Phase, Presence, reduced_motion};
use gpui::{
    AnyElement, App, AppContext, Asset, AsyncApp, AvailableSpace, Bounds, ClickEvent, Context,
    DevicePixels, DispatchPhase, Div, Element, ElementId, Entity, EntityId, FontWeight, Global,
    GlobalElementId, ImageAssetLoader, ImageCacheError, InspectorElementId, LayoutId,
    MouseMoveEvent, ObjectFit, Pixels, Point, Position, RenderImage, Resource,
    SMOOTH_SVG_SCALE_FACTOR, SharedString, Style, SvgSize, Task, Window, WindowId, deferred, div,
    img, point, prelude::*, px, size,
};

use crate::components::button::{ButtonKind, button};
use crate::components::linkmeta::{self, Lookup};
use crate::components::media_view::{self, OpenMedia};
use crate::components::overlay::popover;
use crate::components::paint::ink;
use crate::components::size::{FONT_SMALL, T1, T2, T3};
use crate::components::tree::OpenProject;
use crate::live::ActiveTheme;
use crate::theme::Theme;

const OPEN_AFTER: Duration = Duration::from_millis(400);
const EDGE: f32 = 8.0;
const GAP: f32 = 6.0;
const IMAGE_HEIGHT: f32 = 120.0;
const DESCRIPTION_LINES: usize = 3;
const THUMB_WIDTH: f32 = 360.0;
const THUMB_HEIGHT: f32 = 280.0;
const THUMB_UPSCALE: f32 = 4.0;
const THUMB_RADIUS: f32 = 6.0;
const EXPAND: &str = "Open full size";
const CITED_REPO_MARK: &str = ".git";
const CITED_SKIPPED_NAMES: [&str; 2] = [CITED_REPO_MARK, "node_modules"];
const CITED_SKIPPED_PREFIXES: [&str; 2] = ["target", "appdata-"];
const CITED_WORKERS: usize = 4;

struct Queue {
    dirs: Vec<PathBuf>,
    busy: usize,
}

struct Walk {
    queue: Mutex<Queue>,
    moved: Condvar,
}

type Names = HashMap<OsString, Vec<Arc<Path>>>;

type CiteKey = (PathBuf, Arc<Path>);

enum Cite {
    Checking(Instant),
    Indexing(Instant),
    Settled(Option<Arc<Path>>),
}

#[derive(Default)]
struct Cited {
    answers: HashMap<CiteKey, Cite>,
    hosts: HashMap<CiteKey, Vec<EntityId>>,
    indexes: HashMap<PathBuf, Option<Arc<Names>>>,
}

impl Global for Cited {}

fn project_root(cx: &App) -> Result<PathBuf, String> {
    match cx.try_global::<OpenProject>() {
        Some(OpenProject(root)) => Ok(root.clone()),
        None => {
            std::env::current_dir().map_err(|error| format!("no project folder to search: {error}"))
        }
    }
}

pub fn confirmed(asked: &Arc<Path>, window: &Window, cx: &App) -> Option<Arc<Path>> {
    let root = match project_root(cx) {
        Ok(root) => root,
        Err(why) => {
            eprintln!("desk: cited {}: {why}", asked.display());
            return None;
        }
    };
    let key = (root, asked.clone());
    let view = window.current_view();
    let cited = cx.try_global::<Cited>();
    if let Some(Cite::Settled(one)) = cited.and_then(|cited| cited.answers.get(&key)) {
        return one.clone();
    }
    let waiting = cited
        .and_then(|cited| cited.hosts.get(&key))
        .is_some_and(|hosts| hosts.contains(&view));
    if !waiting {
        let later = cx.to_async();
        cx.foreground_executor()
            .spawn(async move { later.update(|cx| await_answer(key, view, cx)) })
            .detach();
    }
    None
}

fn await_answer(key: CiteKey, view: EntityId, cx: &mut App) {
    let cited = cx.default_global::<Cited>();
    if let Some(Cite::Settled(_)) = cited.answers.get(&key) {
        return cx.notify(view);
    }
    let hosts = cited.hosts.entry(key.clone()).or_default();
    if !hosts.contains(&view) {
        hosts.push(view);
    }
    if !cited.answers.contains_key(&key) {
        want(key, cx);
    }
}

fn want(key: CiteKey, cx: &mut App) {
    let safe = key.1.is_absolute()
        || key
            .1
            .components()
            .all(|part| matches!(part, Component::Normal(_) | Component::CurDir));
    if !safe {
        return settle(key, None, cx);
    }
    cx.default_global::<Cited>()
        .answers
        .insert(key.clone(), Cite::Checking(Instant::now()));
    warm(&key.0, cx);
    let full = key.0.join(&key.1);
    let check = cx
        .background_executor()
        .spawn(async move { full.is_file() });
    cx.spawn(async move |cx: &mut AsyncApp| {
        let written = check.await;
        cx.update(|cx| checked(key, written, cx));
    })
    .detach();
}

fn skipped(name: &OsStr) -> bool {
    name.to_str().is_some_and(|name| {
        CITED_SKIPPED_NAMES.contains(&name)
            || CITED_SKIPPED_PREFIXES
                .iter()
                .any(|prefix| name.starts_with(prefix))
    })
}

fn index(root: &Path) -> Names {
    let walk = Walk {
        queue: Mutex::new(Queue {
            dirs: vec![root.to_path_buf()],
            busy: 0,
        }),
        moved: Condvar::new(),
    };
    let parts: Vec<Names> = thread::scope(|scope| {
        let workers: Vec<_> = (0..CITED_WORKERS)
            .map(|_| scope.spawn(|| crawl(root, &walk)))
            .collect();
        workers
            .into_iter()
            .filter_map(|worker| match worker.join() {
                Ok(part) => Some(part),
                Err(_) => {
                    eprintln!("desk: cited index worker of {} panicked", root.display());
                    None
                }
            })
            .collect()
    });
    let mut names = Names::new();
    for (name, paths) in parts.into_iter().flatten() {
        names.entry(name).or_default().extend(paths);
    }
    names
}

fn crawl(root: &Path, walk: &Walk) -> Names {
    let mut names = Names::new();
    let mut below: Vec<PathBuf> = Vec::new();
    let mut holding = false;
    loop {
        let Ok(mut queue) = walk.queue.lock() else {
            return names;
        };
        let pushed = !below.is_empty();
        queue.dirs.append(&mut below);
        if holding {
            queue.busy = queue.busy.saturating_sub(1);
        }
        if pushed || queue.busy == 0 {
            walk.moved.notify_all();
        }
        let dir = loop {
            if let Some(dir) = queue.dirs.pop() {
                queue.busy = queue.busy.saturating_add(1);
                break dir;
            }
            if queue.busy == 0 {
                return names;
            }
            queue = match walk.moved.wait(queue) {
                Ok(queue) => queue,
                Err(_) => return names,
            };
        };
        drop(queue);
        holding = true;
        let Ok(entries) = fs::read_dir(&dir) else {
            continue;
        };
        let first_below = below.len();
        let mut files: Vec<(OsString, Arc<Path>)> = Vec::new();
        let mut cloned = false;
        for entry in entries.flatten() {
            let Ok(kind) = entry.file_type() else {
                continue;
            };
            let name = entry.file_name();
            cloned |= name == CITED_REPO_MARK && dir != root;
            if kind.is_dir() && !skipped(&name) {
                below.push(entry.path());
            }
            if kind.is_file()
                && let Ok(inside) = entry.path().strip_prefix(root)
            {
                files.push((name, Arc::from(inside)));
            }
        }
        if cloned {
            below.truncate(first_below);
            continue;
        }
        for (name, inside) in files {
            names.entry(name).or_default().push(inside);
        }
    }
}

fn warm(root: &Path, cx: &mut App) {
    let indexes = &mut cx.default_global::<Cited>().indexes;
    if indexes.contains_key(root) {
        return;
    }
    indexes.insert(root.to_path_buf(), None);
    let at = root.to_path_buf();
    let walk = cx.background_executor().spawn(async move {
        let started = Instant::now();
        let names = index(&at);
        (at, names, started.elapsed())
    });
    cx.spawn(async move |cx: &mut AsyncApp| {
        let (root, names, took) = walk.await;
        eprintln!(
            "desk: cited index of {} built, {} files in {} ms",
            root.display(),
            names.values().map(Vec::len).sum::<usize>(),
            took.as_millis()
        );
        cx.update(|cx| indexed(root, Arc::new(names), cx));
    })
    .detach();
}

fn lookup(root: &Path, names: &Names, asked: &Path) -> Option<Arc<Path>> {
    let hits: Vec<&Arc<Path>> = asked
        .file_name()
        .and_then(|name| names.get(name))
        .into_iter()
        .flatten()
        .filter(|path| path.ends_with(asked))
        .collect();
    match hits.as_slice() {
        [one] => Some(Arc::from(root.join(one))),
        _ => {
            eprintln!(
                "desk: cited {} is plain text: {} matches",
                asked.display(),
                hits.len()
            );
            None
        }
    }
}

fn settle(key: CiteKey, one: Option<Arc<Path>>, cx: &mut App) {
    let cited = cx.default_global::<Cited>();
    if let Some(Cite::Checking(started) | Cite::Indexing(started)) = cited.answers.get(&key) {
        eprintln!(
            "desk: cited {} resolved in {} ms, {}",
            key.1.display(),
            started.elapsed().as_millis(),
            match one.is_some() {
                true => "one file",
                false => "not one file",
            }
        );
    }
    let hosts = cited.hosts.remove(&key).unwrap_or_default();
    cited.answers.insert(key, Cite::Settled(one));
    for host in hosts {
        cx.notify(host);
    }
}

fn indexed(root: PathBuf, names: Arc<Names>, cx: &mut App) {
    let cited = cx.default_global::<Cited>();
    let waiting: Vec<CiteKey> = cited
        .answers
        .iter()
        .filter(|(key, cite)| key.0 == root && matches!(cite, Cite::Indexing(_)))
        .map(|(key, _)| key.clone())
        .collect();
    cited.indexes.insert(root, Some(names.clone()));
    for key in waiting {
        let one = lookup(&key.0, &names, &key.1);
        settle(key, one, cx);
    }
}

fn checked(key: CiteKey, written: bool, cx: &mut App) {
    if written {
        let full = Arc::from(key.0.join(&key.1));
        return settle(key, Some(full), cx);
    }
    if key.1.is_absolute() {
        return settle(key, None, cx);
    }
    let cited = cx.default_global::<Cited>();
    let names = cited.indexes.get(&key.0).cloned().flatten();
    match names {
        Some(names) => {
            let one = lookup(&key.0, &names, &key.1);
            settle(key, one, cx);
        }
        None => {
            if let Some(cite) = cited.answers.get_mut(&key)
                && let Cite::Checking(started) = *cite
            {
                *cite = Cite::Indexing(started);
            }
        }
    }
}

#[derive(Clone, PartialEq)]
pub enum Subject {
    Link(SharedString),
    Image(u32, Option<Arc<Path>>),
    File(Arc<Path>),
}

#[derive(Clone)]
struct Target {
    key: u64,
    subject: Subject,
    line: Bounds<Pixels>,
}

impl Target {
    fn same(&self, other: &Target) -> bool {
        self.key == other.key && self.subject == other.subject
    }
}

struct Card {
    hovered: Option<Target>,
    shown: Option<Target>,
    presence: Presence,
    since: Instant,
    painted: bool,
    placed: Option<Bounds<Pixels>>,
    hosts: Vec<EntityId>,
    timer: Option<Task<()>>,
}

impl Card {
    fn aim(&mut self, target: Target, cx: &mut Context<Self>) {
        if self
            .hovered
            .as_ref()
            .is_some_and(|hovered| hovered.same(&target))
        {
            return;
        }
        self.hovered = Some(target.clone());
        self.since = Instant::now();
        self.painted = false;
        if let Subject::Link(url) = &target.subject {
            let card = cx.entity().downgrade();
            linkmeta::fetch(
                url,
                move |cx| {
                    if let Err(error) = card.update(cx, |card, cx| card.notify_hosts(cx)) {
                        eprintln!("desk: linkmeta answered a closed card: {error}");
                    }
                },
                cx,
            );
        }
        self.timer = Some(cx.spawn(async move |this, cx| {
            cx.background_executor().timer(OPEN_AFTER).await;
            if let Err(error) = this.update(cx, |card, cx| card.open(cx)) {
                eprintln!("desk: hovercard opened after its window closed: {error}");
            }
        }));
    }

    fn left_link(&mut self) {
        self.hovered = None;
        self.timer = None;
    }

    fn open(&mut self, cx: &mut Context<Self>) {
        self.shown = self.hovered.clone();
        self.placed = None;
        self.presence.set_open(true, Instant::now());
        self.notify_hosts(cx);
    }

    fn dismiss(&mut self, cx: &mut Context<Self>) {
        self.left_link();
        self.presence.set_open(false, Instant::now());
        self.notify_hosts(cx);
    }

    fn travel(&mut self, at: Point<Pixels>, cx: &mut Context<Self>) {
        let open = matches!(
            self.presence.phase(Instant::now()),
            Phase::Opening | Phase::Open
        );
        let over = |line: Option<&Target>| line.is_some_and(|target| target.line.contains(&at));
        let on_card = self
            .placed
            .is_some_and(|placed| placed.dilate(px(GAP)).contains(&at));
        if open && !on_card && !over(self.shown.as_ref()) && !over(self.hovered.as_ref()) {
            self.dismiss(cx);
        }
    }

    fn notify_hosts(&self, cx: &mut Context<Self>) {
        for host in &self.hosts {
            AppContext::notify(cx, *host);
        }
    }

    fn painted(&mut self, subject: &Subject, settled: Instant) {
        if self.painted || self.shown.as_ref().map(|shown| &shown.subject) != Some(subject) {
            return;
        }
        self.painted = true;
        let source = match settled <= self.since {
            true => "cached",
            false => "fetched",
        };
        let said = match subject {
            Subject::Link(url) => format!("linkmeta {url}"),
            Subject::Image(n, _) => format!("image {n}"),
            Subject::File(path) => format!("file {}", path.display()),
        };
        eprintln!(
            "desk: hovercard {said} painted after {} ms ({source})",
            self.since.elapsed().as_millis()
        );
    }
}

struct Loaded {
    image: Arc<RenderImage>,
    bytes: u64,
    modified: SystemTime,
    settled: Instant,
}

enum Decoded {}

impl Asset for Decoded {
    type Source = Arc<Path>;
    type Output = Result<Arc<Loaded>, ImageCacheError>;

    fn load(path: Arc<Path>, cx: &mut App) -> impl Future<Output = Self::Output> + Send + 'static {
        let asked = Instant::now();
        let decode = ImageAssetLoader::load(Resource::Path(path.clone()), cx);
        async move {
            let image = decode.await;
            let on = thread::current();
            eprintln!(
                "desk: hovercard image {} decoded in {} ms on thread {:?} ({}), {}",
                path.display(),
                asked.elapsed().as_millis(),
                on.id(),
                on.name().unwrap_or("unnamed"),
                match &image {
                    Ok(_) => "ok".to_owned(),
                    Err(error) => error.to_string(),
                }
            );
            let meta = fs::metadata(&path).map_err(|error| ImageCacheError::Io(Arc::new(error)))?;
            Ok(Arc::new(Loaded {
                image: image?,
                bytes: meta.len(),
                modified: meta
                    .modified()
                    .map_err(|error| ImageCacheError::Io(Arc::new(error)))?,
                settled: Instant::now(),
            }))
        }
    }
}

#[derive(Clone, Hash)]
struct SvgAt {
    path: Arc<Path>,
    modified: SystemTime,
    wide: i32,
}

enum Traced {}

impl Asset for Traced {
    type Source = SvgAt;
    type Output = Result<Arc<RenderImage>, ImageCacheError>;

    fn load(at: SvgAt, cx: &mut App) -> impl Future<Output = Self::Output> + Send + 'static {
        let renderer = cx.svg_renderer();
        async move {
            let bytes = fs::read(&at.path).map_err(|error| ImageCacheError::Io(Arc::new(error)))?;
            let parsed = renderer.parse_svg(&bytes)?;
            let wide = DevicePixels(at.wide);
            let image = renderer.render_parsed(&parsed, SvgSize::Size(size(wide, wide)))?;
            eprintln!(
                "desk: hovercard svg {} rastered {} px wide",
                at.path.display(),
                at.wide
            );
            Ok(image)
        }
    }
}

fn picture(
    said: &str,
    path: &Arc<Path>,
    card: &Entity<Card>,
    now: Instant,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> (Div, Option<Instant>) {
    let small = |text: String| {
        div()
            .w(px(THUMB_WIDTH))
            .text_size(px(FONT_SMALL))
            .text_color(ink(theme, T3))
            .child(text)
    };
    match window.use_asset::<Decoded>(path, cx) {
        None => (div().w(px(THUMB_WIDTH)).h(px(THUMB_HEIGHT)), None),
        Some(Ok(loaded)) => (
            thumbnail(path, &loaded, card, theme, window, cx),
            Some(loaded.settled),
        ),
        Some(Err(ImageCacheError::Io(error))) if error.kind() == ErrorKind::NotFound => (
            small(format!("{said}: file not found at {}", path.display())),
            Some(now),
        ),
        Some(Err(error)) => (
            small(format!("{said}: {error} at {}", path.display())),
            Some(now),
        ),
    }
}

fn thumbnail(
    path: &Arc<Path>,
    loaded: &Loaded,
    card: &Entity<Card>,
    theme: &Theme,
    window: &mut Window,
    cx: &mut App,
) -> Div {
    let pixels = loaded.image.size(0);
    let (width, height) = (pixels.width.0.max(1) as f32, pixels.height.0.max(1) as f32);
    let fit = (THUMB_WIDTH / width)
        .min(THUMB_HEIGHT / height)
        .min(THUMB_UPSCALE);
    let (drawn_width, drawn_height) = ((width * fit).round(), (height * fit).round());
    let vector = media_view::svg(path);
    let own = match vector {
        true => SMOOTH_SVG_SCALE_FACTOR,
        false => 1.0,
    };
    let sharp = vector
        .then(|| SvgAt {
            path: path.clone(),
            modified: loaded.modified,
            wide: (drawn_width * window.scale_factor()).round().max(1.0) as i32,
        })
        .and_then(|at| window.use_asset::<Traced>(&at, cx))
        .and_then(|traced| match traced {
            Ok(image) => Some(image),
            Err(error) => {
                eprintln!(
                    "desk: hovercard svg {} not rastered: {error}",
                    path.display()
                );
                None
            }
        });
    let image = sharp.unwrap_or_else(|| loaded.image.clone());
    let name = path.file_name().map_or_else(
        || path.display().to_string(),
        |name| name.to_string_lossy().into_owned(),
    );
    let facts = format!(
        "{} × {}  {}",
        (width / own).round(),
        (height / own).round(),
        media_view::weight(loaded.bytes)
    );
    let open = |path: Arc<Path>, card: Entity<Card>| {
        move |_: &ClickEvent, window: &mut Window, cx: &mut App| {
            card.update(cx, |card, cx| card.dismiss(cx));
            window.dispatch_action(Box::new(OpenMedia { path: path.clone() }), cx);
        }
    };
    div()
        .flex()
        .flex_col()
        .gap_2()
        .child(
            div()
                .id("link-card-thumbnail")
                .flex()
                .justify_center()
                .cursor_pointer()
                .on_click(open(path.clone(), card.clone()))
                .child(
                    img(image)
                        .w(px(drawn_width))
                        .h(px(drawn_height))
                        .rounded(px(THUMB_RADIUS)),
                ),
        )
        .child(
            div()
                .flex()
                .items_center()
                .gap_2()
                .w(px(THUMB_WIDTH))
                .text_size(px(FONT_SMALL))
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .text_color(ink(theme, T1))
                        .truncate()
                        .child(name),
                )
                .child(div().flex_none().text_color(ink(theme, T3)).child(facts))
                .child(
                    button("link-card-expand", EXPAND, None, ButtonKind::Text, theme)
                        .on_click(open(path.clone(), card.clone())),
                ),
        )
}

#[derive(Default)]
struct Cards(HashMap<WindowId, Entity<Card>>);

impl Global for Cards {}

fn card(window: &Window, cx: &mut App) -> Entity<Card> {
    let id = window.window_handle().window_id();
    if let Some(found) = cx.default_global::<Cards>().0.get(&id).cloned() {
        return found;
    }
    let made = cx.new(|_| Card {
        hovered: None,
        shown: None,
        presence: Presence::new(PANEL_IN_MS, PANEL_OUT_MS),
        since: Instant::now(),
        painted: false,
        placed: None,
        hosts: Vec::new(),
        timer: None,
    });
    cx.default_global::<Cards>().0.insert(id, made.clone());
    made
}

pub fn hover(key: u64, hovered: Option<(Subject, Bounds<Pixels>)>, window: &Window, cx: &mut App) {
    card(window, cx).update(cx, |card, cx| match hovered {
        Some((subject, line)) => card.aim(Target { key, subject, line }, cx),
        None => card.left_link(),
    });
}

#[derive(IntoElement)]
pub struct LinkCard {
    key: u64,
}

pub fn link_card(key: u64) -> LinkCard {
    LinkCard { key }
}

impl RenderOnce for LinkCard {
    fn render(self, window: &mut Window, cx: &mut App) -> impl IntoElement {
        let unindexed = cx
            .try_global::<OpenProject>()
            .map(|OpenProject(root)| root)
            .filter(|root| {
                !cx.try_global::<Cited>()
                    .is_some_and(|cited| cited.indexes.contains_key(*root))
            })
            .cloned();
        if let Some(root) = unindexed {
            warm(&root, cx);
        }
        let card = card(window, cx);
        let view = window.current_view();
        let now = Instant::now();
        let reduced = reduced_motion(cx);
        let shown = card.update(cx, |card, _| {
            if !card.hosts.contains(&view) {
                card.hosts.push(view);
            }
            let phase = card.presence.phase(now);
            let alpha = match reduced {
                true => f32::from(u8::from(phase != Phase::Closing)),
                false => card.presence.progress(now),
            };
            card.shown
                .clone()
                .filter(|shown| shown.key == self.key && phase != Phase::Closed && alpha > 0.0)
                .map(|shown| (shown, alpha, phase))
        });
        let Some((target, alpha, phase)) = shown else {
            return div().into_any_element();
        };
        if matches!(phase, Phase::Opening | Phase::Closing) && !reduced {
            window.request_animation_frame();
        }
        let theme = ActiveTheme::theme(cx);
        let small = |text: SharedString, tone: f32| {
            div()
                .text_size(px(FONT_SMALL))
                .text_color(ink(&theme, tone))
                .truncate()
                .child(text)
        };
        let mut body = popover(&theme).id("link-card").opacity(alpha);
        let url = match &target.subject {
            Subject::Link(url) => url.clone(),
            Subject::Image(n, None) => {
                body = body.child(small(format!("Image {n}, not loaded").into(), T3));
                return placed(card, target, Some(now), body);
            }
            Subject::Image(n, Some(path)) => {
                let said = format!("Image {n}");
                let (shown, settled) = picture(&said, path, &card, now, &theme, window, cx);
                return placed(card, target, settled, body.child(shown));
            }
            Subject::File(path) => {
                let name = path.file_name().map_or_else(
                    || path.display().to_string(),
                    |name| name.to_string_lossy().into_owned(),
                );
                let (shown, settled) = picture(&name, path, &card, now, &theme, window, cx);
                let inside = project_root(cx)
                    .ok()
                    .and_then(|root| path.strip_prefix(root).ok().map(Path::to_path_buf))
                    .unwrap_or_else(|| path.to_path_buf());
                body = body
                    .child(shown)
                    .child(small(inside.display().to_string().into(), T3));
                return placed(card, target, settled, body);
            }
        };
        let (lookup, settled) = linkmeta::cached(&url, cx).unwrap_or((Lookup::Pending, now));
        let host: SharedString = linkmeta::host(&url).to_owned().into();
        if let Lookup::Found(meta) = &lookup {
            body = body
                .children(meta.image.clone().map(|image| {
                    img(image)
                        .w_full()
                        .h(px(IMAGE_HEIGHT))
                        .object_fit(ObjectFit::Cover)
                }))
                .children(meta.title.clone().map(|title| {
                    div()
                        .font_weight(FontWeight::SEMIBOLD)
                        .text_color(ink(&theme, T1))
                        .child(title)
                }))
                .children(meta.description.clone().map(|description| {
                    div()
                        .text_size(px(FONT_SMALL))
                        .text_color(ink(&theme, T2))
                        .line_clamp(DESCRIPTION_LINES)
                        .child(description)
                }));
        }
        body = body.child(small(host, T3));
        if !matches!(lookup, Lookup::Found(_)) {
            body = body.child(small(url, T3));
        }
        let settled = (!matches!(lookup, Lookup::Pending)).then_some(settled);
        placed(card, target, settled, body)
    }
}

fn placed(
    card: Entity<Card>,
    target: Target,
    settled: Option<Instant>,
    body: impl IntoElement,
) -> AnyElement {
    deferred(Placed {
        card,
        subject: target.subject,
        line: target.line,
        settled,
        panel: body.into_any_element(),
    })
    .priority(1)
    .into_any_element()
}

struct Placed {
    card: Entity<Card>,
    subject: Subject,
    line: Bounds<Pixels>,
    settled: Option<Instant>,
    panel: AnyElement,
}

impl IntoElement for Placed {
    type Element = Self;

    fn into_element(self) -> Self {
        self
    }
}

impl Element for Placed {
    type RequestLayoutState = ();
    type PrepaintState = ();

    fn id(&self) -> Option<ElementId> {
        None
    }

    fn source_location(&self) -> Option<&'static Location<'static>> {
        None
    }

    fn request_layout(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        window: &mut Window,
        cx: &mut App,
    ) -> (LayoutId, ()) {
        let style = Style {
            position: Position::Absolute,
            ..Style::default()
        };
        (window.request_layout(style, [], cx), ())
    }

    fn prepaint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) {
        let room = size(AvailableSpace::MaxContent, AvailableSpace::MaxContent);
        let measured = self.panel.layout_as_root(room, window, cx);
        let viewport = window.viewport_size();
        let (gap, edge) = (px(GAP), px(EDGE));
        let below = self.line.bottom() + gap;
        let above = self.line.top() - gap - measured.height;
        let room_below = viewport.height - edge - self.line.bottom();
        let room_above = self.line.top() - edge;
        let y = match below + measured.height <= viewport.height - edge
            || (above < edge && room_below >= room_above)
        {
            true => below,
            false => above,
        };
        let widest = (viewport.width - measured.width - edge).max(edge);
        let x = self.line.left().clamp(edge, widest);
        let origin = point(x.round(), y.round());
        self.panel.prepaint_at(origin, window, cx);
        self.card.update(cx, |card, _| {
            card.placed = Some(Bounds::new(origin, measured));
        });
    }

    fn paint(
        &mut self,
        _: Option<&GlobalElementId>,
        _: Option<&InspectorElementId>,
        _: Bounds<Pixels>,
        _: &mut (),
        _: &mut (),
        window: &mut Window,
        cx: &mut App,
    ) {
        self.panel.paint(window, cx);
        if let Some(settled) = self.settled {
            self.card
                .update(cx, |card, _| card.painted(&self.subject, settled));
        }
        let card = self.card.clone();
        window.on_mouse_event(move |event: &MouseMoveEvent, phase, _, cx| {
            if phase == DispatchPhase::Bubble {
                card.update(cx, |card, cx| card.travel(event.position, cx));
            }
        });
    }
}

pub fn link_key(text: &str, piece: usize) -> u64 {
    let mut hasher = DefaultHasher::new();
    (text, piece).hash(&mut hasher);
    hasher.finish()
}
