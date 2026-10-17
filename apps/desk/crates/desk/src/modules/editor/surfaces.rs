use std::fs;
use std::path::Path;
use std::time::Instant;

use desk_ui::components::palette::{Palette, PaletteDetail, PaletteEntry, PaletteItem};
use gpui::{Context, Entity, SharedString, Subscription, Task, Window};

use super::listing::Listing;
use super::{Editor, Hit, Open, Page, files_under, jump_once_laid_out};

const FIND_MOST: usize = 500;
const FIND_LARGEST: u64 = 1024 * 1024;
const BINARY_PROBE: usize = 8000;
const QUICK_ROWS: usize = 200;
const SHOWN_BEFORE: usize = 40;
const SHOWN_CHARS: usize = 160;
const GOTO_PROMPT: &str = "Go to line";
const QUICK_PROMPT: &str = "Open a file by name";
const QUICK_NONE: &str = "No matching files";
const QUICK_GROUP: &str = "Files";
const FIND_PROMPT: &str = "Find in files";
const FIND_NONE: &str = "No matches in the project";
const FIND_GROUP: &str = "Matches";

pub(super) struct Surfaces {
    goto: Entity<Palette>,
    quick: Entity<Palette>,
    find: Entity<Palette>,
    files: Vec<String>,
    filtered: Option<String>,
    sought: String,
    hits: Vec<Hit>,
    _listing: Option<Task<()>>,
    _searching: Option<Task<()>>,
    _heard: [Subscription; 2],
}

impl Surfaces {
    pub(super) fn new(window: &mut Window, cx: &mut Context<Editor>) -> Self {
        let worded = |placeholder: &'static str,
                      nothing: &'static str,
                      window: &mut Window,
                      cx: &mut Context<Editor>| {
            Palette::worded(Vec::new(), placeholder.into(), nothing.into(), window, cx)
        };
        let goto = worded(GOTO_PROMPT, GOTO_PROMPT, window, cx);
        let quick = worded(QUICK_PROMPT, QUICK_NONE, window, cx);
        let find = worded(FIND_PROMPT, FIND_NONE, window, cx);
        let submitted =
            cx.listener(|editor, query: &str, window, cx| editor.go_to_line(query, window, cx));
        let opened =
            cx.listener(|editor, id: &SharedString, window, cx| editor.quick_pick(id, window, cx));
        let found =
            cx.listener(|editor, id: &SharedString, window, cx| editor.find_pick(id, window, cx));
        goto.update(cx, |palette, _| palette.on_submit(submitted));
        quick.update(cx, |palette, _| palette.on_pick(opened));
        find.update(cx, |palette, _| palette.on_pick(found));
        Surfaces {
            _heard: [
                cx.observe(&quick, |editor, _, cx| editor.quick_filter(cx)),
                cx.observe(&find, |editor, _, cx| editor.find_query(cx)),
            ],
            goto,
            quick,
            find,
            files: Vec::new(),
            filtered: None,
            sought: String::new(),
            hits: Vec::new(),
            _listing: None,
            _searching: None,
        }
    }

    pub(super) fn palettes(&self) -> [Entity<Palette>; 3] {
        [self.goto.clone(), self.quick.clone(), self.find.clone()]
    }
}

impl Editor {
    pub(super) fn open_goto(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.surfaces
            .goto
            .update(cx, |palette, cx| palette.open(window, cx));
    }

    pub(super) fn open_find(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.surfaces
            .find
            .update(cx, |palette, cx| palette.open(window, cx));
    }

    pub(super) fn open_quick(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.surfaces
            .quick
            .update(cx, |palette, cx| palette.open(window, cx));
        let listing = self.folder.listing.clone();
        let walked = cx.background_executor().spawn(async move {
            let mut files: Vec<String> = files_under(&listing, |_| true)
                .iter()
                .filter_map(|path| listing.relative(path))
                .collect();
            files.sort();
            files
        });
        self.surfaces._listing = Some(cx.spawn(async move |this, cx| {
            let files = walked.await;
            let listed = this.update(cx, |editor, cx| {
                editor.surfaces.files = files;
                editor.surfaces.filtered = None;
                editor.quick_filter(cx);
            });
            if let Err(error) = listed {
                eprintln!("desk: editor closed before quick open listed its files: {error}");
            }
        }));
    }

    fn go_to_line(&mut self, query: &str, window: &mut Window, cx: &mut Context<Self>) {
        let Ok(line) = query.trim().parse::<usize>() else {
            return eprintln!("desk: editor goto refused {query:?}, not a line number");
        };
        let Some(Page { code: Ok(code), .. }) = self.pages.get(self.active) else {
            return eprintln!("desk: editor goto {line} with no file open");
        };
        let last = code.read(cx).buffer().line_count().max(1);
        jump_once_laid_out(code.clone(), (line.clamp(1, last) - 1, 0), window, cx);
        eprintln!("desk: editor goto {line}");
    }

    fn quick_filter(&mut self, cx: &mut Context<Self>) {
        let query = self.surfaces.quick.read(cx).query().trim().to_lowercase();
        if self.surfaces.filtered.as_ref() == Some(&query) {
            return;
        }
        let rows = quick_rows(&self.surfaces.files, &query);
        self.surfaces.filtered = Some(query);
        self.surfaces
            .quick
            .update(cx, |palette, cx| palette.replace(rows, cx));
    }

    fn quick_pick(&mut self, id: &SharedString, window: &mut Window, cx: &mut Context<Self>) {
        eprintln!("desk: editor quick open {id}");
        let full = self.folder.listing.root.join(id.as_ref());
        self.open_path(full, Open::Pinned, window, cx);
    }

    fn find_query(&mut self, cx: &mut Context<Self>) {
        let sought = self.surfaces.find.read(cx).query().trim().to_owned();
        if sought == self.surfaces.sought {
            return;
        }
        self.surfaces.sought = sought.clone();
        if sought.is_empty() {
            self.surfaces._searching = None;
            self.surfaces.hits.clear();
            return self
                .surfaces
                .find
                .update(cx, |palette, cx| palette.replace(Vec::new(), cx));
        }
        let listing = self.folder.listing.clone();
        let started = Instant::now();
        let searched = cx.background_executor().spawn({
            let sought = sought.clone();
            async move {
                let hits = search(&listing, &sought);
                let rows = find_rows(&listing, &hits);
                (hits, rows)
            }
        });
        self.surfaces._searching = Some(cx.spawn(async move |this, cx| {
            let (hits, rows) = searched.await;
            let shown = this.update(cx, |editor, cx| {
                if editor.surfaces.sought != sought {
                    return;
                }
                eprintln!(
                    "desk: editor find in files {sought} {} hits in {} ms",
                    hits.len(),
                    started.elapsed().as_millis()
                );
                editor.surfaces.hits = hits;
                editor
                    .surfaces
                    .find
                    .update(cx, |palette, cx| palette.replace(rows, cx));
            });
            if let Err(error) = shown {
                eprintln!("desk: editor closed before find in files answered: {error}");
            }
        }));
    }

    fn find_pick(&mut self, id: &SharedString, window: &mut Window, cx: &mut Context<Self>) {
        let Some(hit) = id
            .parse::<usize>()
            .ok()
            .and_then(|at| self.surfaces.hits.get(at))
        else {
            return eprintln!("desk: editor find in files picked {id}, no such hit");
        };
        let (path, line, col) = (hit.path.clone(), hit.line, hit.col);
        eprintln!(
            "desk: editor find in files open {}",
            self.shown(&path, line)
        );
        self.land(&path, line, col, window, cx);
    }
}

fn quick_rows(files: &[String], query: &str) -> Vec<PaletteEntry> {
    let mut found: Vec<(bool, &String)> = files
        .iter()
        .filter_map(|file| {
            let lower = file.to_lowercase();
            let name = lower.rsplit('/').next().unwrap_or_default();
            lower.contains(query).then(|| (!name.contains(query), file))
        })
        .collect();
    found.sort_by_key(|(elsewhere, file)| (*elsewhere, file.len()));
    found
        .into_iter()
        .take(QUICK_ROWS)
        .map(|(_, file)| {
            let path = SharedString::from(file.clone());
            let item = PaletteItem {
                id: path.clone(),
                label: path,
                group: QUICK_GROUP.into(),
                keys: None,
            };
            (item, None)
        })
        .collect()
}

fn search(listing: &Listing, sought: &str) -> Vec<Hit> {
    let wanted = sought.to_lowercase();
    let mut paths = files_under(listing, |path| {
        fs::metadata(path).is_ok_and(|meta| meta.len() <= FIND_LARGEST)
    });
    paths.sort();
    let mut hits = Vec::new();
    for path in paths {
        let Some(text) = readable(&path) else {
            continue;
        };
        for (line, row) in text.lines().enumerate() {
            let lower = row.to_lowercase();
            let Some(at) = lower.find(&wanted) else {
                continue;
            };
            let col = lower.get(..at).map_or(0, |before| before.chars().count());
            let text: String = row
                .chars()
                .skip(col.saturating_sub(SHOWN_BEFORE))
                .take(SHOWN_CHARS)
                .collect();
            hits.push(Hit {
                path: path.clone(),
                line,
                col,
                text: text.trim().to_owned(),
            });
            if hits.len() >= FIND_MOST {
                return hits;
            }
        }
    }
    hits
}

fn readable(path: &Path) -> Option<String> {
    let bytes = match fs::read(path) {
        Ok(bytes) => bytes,
        Err(error) => {
            eprintln!(
                "desk: editor find in files skipped {}: {error}",
                path.display()
            );
            return None;
        }
    };
    if bytes.iter().take(BINARY_PROBE).any(|byte| *byte == 0) {
        return None;
    }
    String::from_utf8(bytes).ok()
}

fn find_rows(listing: &Listing, hits: &[Hit]) -> Vec<PaletteEntry> {
    hits.iter()
        .enumerate()
        .map(|(at, hit)| {
            let path = listing
                .relative(&hit.path)
                .unwrap_or_else(|| hit.path.display().to_string());
            let item = PaletteItem {
                id: at.to_string().into(),
                label: format!("{path}:{}", hit.line + 1).into(),
                group: FIND_GROUP.into(),
                keys: None,
            };
            let detail = PaletteDetail {
                below: hit.text.clone().into(),
                ..PaletteDetail::default()
            };
            (item, Some(detail))
        })
        .collect()
}
