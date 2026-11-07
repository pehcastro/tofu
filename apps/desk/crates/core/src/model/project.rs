use std::path::{Path, PathBuf};
use std::sync::Arc;

pub struct ProjectRow {
    pub folder: PathBuf,
    pub name: Arc<str>,
    pub branch: Arc<str>,
    pub changed: usize,
    pub ahead: u32,
    pub folded: bool,
}

impl ProjectRow {
    fn new(folder: PathBuf) -> Self {
        ProjectRow {
            name: project_name(&folder).into(),
            folder,
            branch: Arc::from(""),
            changed: 0,
            ahead: 0,
            folded: false,
        }
    }
}

pub fn project_name(folder: &Path) -> String {
    folder.file_name().map_or_else(
        || folder.display().to_string(),
        |name| name.to_string_lossy().into_owned(),
    )
}

fn same_folder(one: &Path, two: &Path) -> bool {
    let mut one = one.components();
    let mut two = two.components();
    loop {
        match (one.next(), two.next()) {
            (None, None) => return true,
            (Some(a), Some(b)) => {
                let (a, b) = (a.as_os_str(), b.as_os_str());
                let same = match cfg!(windows) {
                    true => a.eq_ignore_ascii_case(b),
                    false => a == b,
                };
                if !same {
                    return false;
                }
            }
            _ => return false,
        }
    }
}

#[derive(Default)]
pub struct Projects {
    rows: Vec<ProjectRow>,
    selected: u32,
}

impl Projects {
    pub fn rows(&self) -> &[ProjectRow] {
        &self.rows
    }

    pub fn selected_at(&self) -> Option<usize> {
        let at = usize::try_from(self.selected).ok()?;
        (at < self.rows.len()).then_some(at)
    }

    pub fn selected(&self) -> Option<&ProjectRow> {
        self.rows.get(self.selected_at()?)
    }

    pub fn folder(&self) -> Option<&Path> {
        self.selected().map(|row| row.folder.as_path())
    }

    pub fn find(&self, folder: &Path) -> Option<usize> {
        self.rows
            .iter()
            .position(|row| same_folder(&row.folder, folder))
    }

    pub fn row_mut(&mut self, folder: &Path) -> Option<&mut ProjectRow> {
        let at = self.find(folder)?;
        self.rows.get_mut(at)
    }

    pub fn open(&mut self, folder: PathBuf) {
        let at = match self.find(&folder) {
            Some(at) => at,
            None => {
                self.rows.push(ProjectRow::new(folder));
                self.rows.len() - 1
            }
        };
        self.selected = u32::try_from(at).unwrap_or(u32::MAX);
    }

    pub fn fold(&mut self, at: usize) -> Option<bool> {
        let row = self.rows.get_mut(at)?;
        row.folded = !row.folded;
        Some(row.folded)
    }
}
