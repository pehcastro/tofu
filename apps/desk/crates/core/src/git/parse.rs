use std::collections::HashMap;

use super::{BlameLine, Conflict, Entry, GitError, Hunk, Lines, Mark, State};

const UNCOMMITTED: &str = "0000000000000000000000000000000000000000";

fn unreadable(what: &'static str, text: &str) -> GitError {
    GitError::Parse {
        what,
        text: text.to_owned(),
    }
}

fn mark(code: char, record: &str) -> Result<Mark, GitError> {
    match code {
        '.' => Ok(Mark::Unmodified),
        'M' => Ok(Mark::Modified),
        'T' => Ok(Mark::TypeChanged),
        'A' => Ok(Mark::Added),
        'D' => Ok(Mark::Deleted),
        'R' => Ok(Mark::Renamed),
        'C' => Ok(Mark::Copied),
        _ => Err(unreadable("status code", record)),
    }
}

fn marks(xy: &str, record: &str) -> Result<(Mark, Mark), GitError> {
    let mut codes = xy.chars();
    match (codes.next(), codes.next(), codes.next()) {
        (Some(x), Some(y), None) => Ok((mark(x, record)?, mark(y, record)?)),
        _ => Err(unreadable("status code", record)),
    }
}

fn conflict(xy: &str, record: &str) -> Result<Conflict, GitError> {
    match xy {
        "DD" => Ok(Conflict::BothDeleted),
        "AU" => Ok(Conflict::AddedByUs),
        "UD" => Ok(Conflict::DeletedByThem),
        "UA" => Ok(Conflict::AddedByThem),
        "DU" => Ok(Conflict::DeletedByUs),
        "AA" => Ok(Conflict::BothAdded),
        "UU" => Ok(Conflict::BothModified),
        _ => Err(unreadable("conflict code", record)),
    }
}

pub fn status(out: &str) -> Result<Vec<Entry>, GitError> {
    let mut records = out.split('\0').filter(|record| !record.is_empty());
    let mut entries = Vec::new();
    while let Some(record) = records.next() {
        let width = match record.as_bytes().first() {
            Some(b'1') => 9,
            Some(b'2') => 10,
            Some(b'u') => 11,
            _ => 2,
        };
        let fields: Vec<&str> = record.splitn(width, ' ').collect();
        let entry = match fields.as_slice() {
            ["1", xy, _, _, _, _, _, _, path] => {
                let (index, worktree) = marks(xy, record)?;
                Entry {
                    path: (*path).to_owned(),
                    state: State::Tracked { index, worktree },
                }
            }
            ["2", xy, _, _, _, _, _, _, _, path] => {
                let (index, worktree) = marks(xy, record)?;
                let from = records
                    .next()
                    .ok_or_else(|| unreadable("rename without its source", record))?;
                Entry {
                    path: (*path).to_owned(),
                    state: State::Renamed {
                        from: from.to_owned(),
                        index,
                        worktree,
                    },
                }
            }
            ["u", xy, _, _, _, _, _, _, _, _, path] => Entry {
                path: (*path).to_owned(),
                state: State::Conflicted(conflict(xy, record)?),
            },
            _ => match record.split_once(' ') {
                Some(("?", path)) => Entry {
                    path: path.to_owned(),
                    state: State::Untracked,
                },
                Some(("!", path)) => Entry {
                    path: path.to_owned(),
                    state: State::Ignored,
                },
                _ => return Err(unreadable("status record", record)),
            },
        };
        entries.push(entry);
    }
    Ok(entries)
}

fn range(text: &str, line: &str) -> Result<Lines, GitError> {
    let number = |part: &str| {
        part.parse::<u32>()
            .map_err(|_| unreadable("hunk header", line))
    };
    match text.split_once(',') {
        Some((start, count)) => Ok(Lines {
            start: number(start)?,
            count: number(count)?,
        }),
        None => Ok(Lines {
            start: number(text)?,
            count: 1,
        }),
    }
}

pub fn diff(out: &str) -> Result<Vec<Hunk>, GitError> {
    let mut hunks: Vec<Hunk> = Vec::new();
    for line in out.lines() {
        if let Some(header) = line.strip_prefix("@@ -") {
            let mut parts = header.split(' ');
            let (Some(old), Some(new)) = (parts.next(), parts.next()) else {
                return Err(unreadable("hunk header", line));
            };
            let new = new
                .strip_prefix('+')
                .ok_or_else(|| unreadable("hunk header", line))?;
            hunks.push(Hunk {
                old: range(old, line)?,
                new: range(new, line)?,
                removed: Vec::new(),
                added: Vec::new(),
            });
            continue;
        }
        let Some(hunk) = hunks.last_mut() else {
            continue;
        };
        if let Some(text) = line.strip_prefix('+') {
            hunk.added.push(text.to_owned());
        } else if let Some(text) = line.strip_prefix('-') {
            hunk.removed.push(text.to_owned());
        }
    }
    Ok(hunks)
}

pub fn blame(out: &str) -> Result<Vec<BlameLine>, GitError> {
    let mut known: HashMap<&str, (String, i64)> = HashMap::new();
    let mut lines = Vec::new();
    let mut sha = "";
    let mut author = String::new();
    let mut time = 0;
    for line in out.lines() {
        if let Some(text) = line.strip_prefix('\t') {
            let (author, time) = known
                .entry(sha)
                .or_insert_with(|| (author.clone(), time))
                .clone();
            lines.push(BlameLine {
                commit: (sha != UNCOMMITTED).then(|| sha.to_owned()),
                author,
                time,
                text: text.to_owned(),
            });
        } else if let Some(name) = line.strip_prefix("author ") {
            name.clone_into(&mut author);
        } else if let Some(seconds) = line.strip_prefix("author-time ") {
            time = seconds
                .parse()
                .map_err(|_| unreadable("blame time", line))?;
        } else if let Some(first) = line.split(' ').next()
            && first.len() == UNCOMMITTED.len()
            && first.bytes().all(|b| b.is_ascii_hexdigit())
        {
            sha = first;
        }
    }
    Ok(lines)
}
