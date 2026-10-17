use std::path::{Component, Path, PathBuf};
use std::sync::Arc;

const IMAGES: [&str; 5] = ["png", "jpg", "jpeg", "gif", "webp"];
const CUT: [char; 10] = ['`', '(', ')', '[', ']', ',', ';', '"', '\'', '*'];
const TRAILING: [char; 4] = ['.', ':', '!', '?'];
const LONGEST_EXTENSION: usize = 5;

#[derive(Clone, Debug, PartialEq)]
pub(super) enum To {
    Ticket(String),
    File(PathBuf),
    Folder(PathBuf),
    Image(Arc<Path>),
}

#[derive(Clone, Debug, PartialEq)]
pub(super) struct Ref {
    pub label: String,
    pub to: To,
}

pub(super) fn refs(
    text: &str,
    own: &str,
    boards: &[&str],
    root: &Path,
    owns: &[String],
) -> Vec<Ref> {
    let mut found: Vec<Ref> = Vec::new();
    for token in text.split(|c: char| c.is_whitespace() || CUT.contains(&c)) {
        let token = bare(token);
        if token.is_empty() || token == own || found.iter().any(|held| held.label == token) {
            continue;
        }
        let to = match ticket(token, boards) {
            true => Some(To::Ticket(token.to_owned())),
            false => target(token, root, owns),
        };
        found.extend(to.map(|to| Ref {
            label: token.to_owned(),
            to,
        }));
    }
    found
}

pub(super) fn target(token: &str, root: &Path, owns: &[String]) -> Option<To> {
    resolve(token, root, owns).map(shaped)
}

fn resolve(token: &str, root: &Path, owns: &[String]) -> Option<PathBuf> {
    let asked = Path::new(token);
    if !normal(asked) || !(token.contains(['/', '\\']) || named_file(asked)) {
        return None;
    }
    let tail = format!("/{token}");
    std::iter::once(root.join(asked))
        .chain(owns.iter().filter_map(|glob| {
            let dir = glob.trim_end_matches(['*', '/']);
            let held = Path::new(dir);
            match normal(held) {
                false => None,
                true if glob.ends_with(&tail) => Some(root.join(held)),
                true if dir.len() < glob.len() => Some(root.join(held).join(asked)),
                true => None,
            }
        }))
        .find(|path| path.exists())
}

fn bare(mut token: &str) -> &str {
    loop {
        token = token.trim_end_matches(TRAILING);
        match token.rsplit_once(':') {
            Some((head, line)) if !line.is_empty() && line.bytes().all(|b| b.is_ascii_digit()) => {
                token = head;
            }
            _ => return token,
        }
    }
}

fn ticket(token: &str, boards: &[&str]) -> bool {
    token.split_once('-').is_some_and(|(board, number)| {
        boards.contains(&board)
            && number
                .split('-')
                .all(|part| !part.is_empty() && part.bytes().all(|b| b.is_ascii_digit()))
    })
}

fn normal(path: &Path) -> bool {
    path.components().next().is_some()
        && path
            .components()
            .all(|part| matches!(part, Component::Normal(_)))
}

fn named_file(path: &Path) -> bool {
    path.extension()
        .and_then(|extension| extension.to_str())
        .is_some_and(|extension| {
            (1..=LONGEST_EXTENSION).contains(&extension.len())
                && extension.starts_with(|c: char| c.is_ascii_lowercase())
                && extension
                    .chars()
                    .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit())
        })
}

fn shaped(path: PathBuf) -> To {
    let image = path
        .extension()
        .and_then(|extension| extension.to_str())
        .is_some_and(|extension| IMAGES.contains(&extension.to_ascii_lowercase().as_str()));
    match (image, path.is_dir()) {
        (true, _) => To::Image(path.into()),
        (false, true) => To::Folder(path),
        (false, false) => To::File(path),
    }
}
