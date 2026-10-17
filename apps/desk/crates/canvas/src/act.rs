use std::path::{Component, Path, PathBuf};

use draw_doc::{DocError, Element, Landing, Op, clip, paste, unclip};
use gpui::{ClipboardEntry, ClipboardItem, Context, Image, Pixels, Point, Window};
use serde_json::{Value, json};

use crate::{Canvas, Changed, create, shape};

const PASTE_OFFSET: f64 = 16.0;
const IMAGE_LONGEST: f64 = 480.0;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Act {
    Cut,
    Copy,
    Paste,
    Duplicate,
    Front,
    Back,
    Group,
    Ungroup,
    Lock,
    Delete,
    SelectAll,
    Fit,
}

pub enum Ask {
    Menu { at: Point<Pixels>, shape: bool },
    Image(Image),
}

fn groups(element: &Element) -> Vec<String> {
    match element.extra.get("groupIds") {
        Some(Value::Array(list)) => list
            .iter()
            .filter_map(Value::as_str)
            .map(str::to_owned)
            .collect(),
        _ => Vec::new(),
    }
}

pub fn locked(element: &Element) -> bool {
    element.extra.get("locked").and_then(Value::as_bool) == Some(true)
}

impl Canvas {
    pub fn act(&mut self, act: Act, window: &mut Window, cx: &mut Context<Self>) {
        self.focus.focus(window, cx);
        let done = match act {
            Act::Copy => self.copy(cx),
            Act::Cut => self.copy(cx).map(|()| self.delete()),
            Act::Paste => self.paste_clipboard(window, cx),
            Act::Duplicate => self.duplicate(),
            Act::Front => self.restack(true),
            Act::Back => self.restack(false),
            Act::Group => self.group(),
            Act::Ungroup => self.regroup(|_, list| {
                list.pop();
            }),
            Act::Lock => self.lock(),
            Act::Delete => {
                self.delete();
                Ok(())
            }
            Act::SelectAll => {
                self.selected = self
                    .scene
                    .elements()
                    .iter()
                    .filter(|element| {
                        element.is_deleted != Some(true) && shape::container(element).is_none()
                    })
                    .map(|element| element.id.clone())
                    .collect();
                Ok(())
            }
            Act::Fit => {
                if let Some(content) = self.content() {
                    self.fit(content, cx);
                }
                Ok(())
            }
        };
        match done {
            Ok(()) => eprintln!("desk: draw: {act:?}, {} selected", self.selected.len()),
            Err(error) => self.fail(&error),
        }
        cx.emit(Changed);
        cx.notify();
    }

    pub fn opacity(&self) -> u8 {
        self.chosen()
            .next()
            .and_then(|element| shape::number(element, "opacity"))
            .map_or(self.style.opacity(), |percent| {
                percent.clamp(0.0, 100.0) as u8
            })
    }

    pub fn has_selection(&self) -> bool {
        !self.selected.is_empty()
    }

    pub fn selection_locked(&self) -> bool {
        self.has_selection() && self.chosen().all(locked)
    }

    pub fn selection_grouped(&self) -> bool {
        self.chosen().any(|element| !groups(element).is_empty())
    }

    pub fn set_folder(&mut self, folder: Option<PathBuf>) {
        self.folder = folder;
    }

    pub(crate) fn picture(&self, element: &Element) -> Option<PathBuf> {
        let file = element.extra.get("customData")?.get("file")?.as_str()?;
        let relative = Path::new(file);
        let plain = relative
            .components()
            .all(|part| matches!(part, Component::Normal(_)));
        plain.then(|| self.folder.as_ref().map(|folder| folder.join(relative)))?
    }

    pub(crate) fn grouped(&self, id: &str) -> Vec<String> {
        let Some(outer) = self.scene.get(id).and_then(|element| groups(element).pop()) else {
            return vec![id.to_owned()];
        };
        self.scene
            .elements()
            .iter()
            .filter(|element| {
                element.is_deleted != Some(true)
                    && shape::container(element).is_none()
                    && groups(element).contains(&outer)
            })
            .map(|element| element.id.clone())
            .collect()
    }

    fn landing(&self, window: &Window) -> Landing {
        let at = window.mouse_position();
        match self.bounds.contains(&at) {
            true => {
                let (x, y) = self.view.scene(self.bounds.origin, at);
                Landing::Pointer(x, y)
            }
            false => Landing::Offset(PASTE_OFFSET),
        }
    }

    fn copy(&mut self, cx: &mut Context<Self>) -> Result<(), DocError> {
        if self.selected.is_empty() {
            return Ok(());
        }
        let text = clip(&self.scene, &self.selected)?;
        cx.write_to_clipboard(ClipboardItem::new_string(text));
        eprintln!("desk: draw copied {} shapes", self.selected.len());
        Ok(())
    }

    fn paste_clipboard(&mut self, window: &Window, cx: &mut Context<Self>) -> Result<(), DocError> {
        let Some(item) = cx.read_from_clipboard() else {
            eprintln!("desk: draw: paste found an empty clipboard");
            return Ok(());
        };
        for entry in item.entries() {
            if let ClipboardEntry::String(text) = entry
                && let Some(elements) = unclip(text.text())?
            {
                return self.land(&elements, self.landing(window));
            }
        }
        let image = item.entries().iter().find_map(|entry| match entry {
            ClipboardEntry::Image(image) => Some(image.clone()),
            ClipboardEntry::String(_) | ClipboardEntry::ExternalPaths(_) => None,
        });
        match image {
            Some(image) => cx.emit(Ask::Image(image)),
            None => eprintln!("desk: draw: the clipboard holds no shapes and no image"),
        }
        Ok(())
    }

    fn land(&mut self, elements: &[Element], landing: Landing) -> Result<(), DocError> {
        let mint = &mut self.mint;
        let (ops, picked) = paste(&self.scene, elements, landing, || mint.id())?;
        self.scene.apply(ops)?;
        eprintln!("desk: draw pasted {} shapes at {landing:?}", picked.len());
        self.selected = picked;
        Ok(())
    }

    fn duplicate(&mut self) -> Result<(), DocError> {
        let Some(elements) = unclip(&clip(&self.scene, &self.selected)?)? else {
            return Ok(());
        };
        self.land(&elements, Landing::Offset(PASTE_OFFSET))
    }

    fn family(&self) -> Vec<String> {
        let mut ids: Vec<String> = Vec::new();
        for id in self.chosen().flat_map(|element| self.doomed(element)) {
            if !ids.contains(&id) {
                ids.push(id);
            }
        }
        ids
    }

    fn restack(&mut self, front: bool) -> Result<(), DocError> {
        let elements = self.scene.elements();
        let last = elements.len().saturating_sub(1);
        let mut moving: Vec<(usize, String)> = self
            .family()
            .into_iter()
            .filter_map(|id| Some((elements.iter().position(|e| e.id == id)?, id)))
            .collect();
        moving.sort_by_key(|(at, _)| *at);
        if !front {
            moving.reverse();
        }
        let to = if front { last } else { 0 };
        let ops = moving
            .into_iter()
            .map(|(_, id)| Op::Reorder { id, to })
            .collect();
        self.scene.apply(ops)
    }

    fn regroup(&mut self, change: impl Fn(&str, &mut Vec<String>)) -> Result<(), DocError> {
        let group = self.mint.id();
        let ops = self
            .family()
            .iter()
            .filter_map(|id| self.scene.get(id))
            .map(|element| {
                let mut list = groups(element);
                change(&group, &mut list);
                let mut changed = element.clone();
                changed.extra.insert("groupIds".to_owned(), json!(list));
                Op::Update(Box::new(changed))
            })
            .collect();
        self.scene.apply(ops)
    }

    fn group(&mut self) -> Result<(), DocError> {
        if self.selected.len() < 2 {
            return Ok(());
        }
        self.regroup(|group, list| list.push(group.to_owned()))
    }

    fn lock(&mut self) -> Result<(), DocError> {
        let lock = !self.selection_locked();
        let ops = self
            .family()
            .iter()
            .filter_map(|id| self.scene.get(id))
            .map(|element| {
                let mut changed = element.clone();
                changed.extra.insert("locked".to_owned(), json!(lock));
                Op::Update(Box::new(changed))
            })
            .collect();
        self.scene.apply(ops)
    }

    pub fn insert_image(
        &mut self,
        file: &str,
        (width, height): (f64, f64),
        cx: &mut Context<Self>,
    ) {
        let scale = (IMAGE_LONGEST / width.max(height).max(1.0)).min(1.0);
        let size = (width * scale, height * scale);
        let middle = self.view.scene(
            self.bounds.origin,
            self.pointer.unwrap_or(self.bounds.center()),
        );
        let corner = (middle.0 - size.0 / 2.0, middle.1 - size.1 / 2.0);
        let element = create::image(&mut self.mint, file, corner, size);
        let id = element.id.clone();
        let added = Op::Add {
            at: None,
            element: Box::new(element),
        };
        match self.scene.apply(vec![added]) {
            Ok(()) => {
                eprintln!("desk: draw image {id} {file} {:.0}x{:.0}", size.0, size.1);
                self.selected = vec![id];
            }
            Err(error) => self.fail(&error),
        }
        cx.emit(Changed);
        cx.notify();
    }
}
