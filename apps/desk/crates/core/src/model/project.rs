use std::path::{Path, PathBuf};
use std::sync::Arc;

pub struct ProjectRow {
    pub folder: PathBuf,
    pub name: Arc<str>,
    pub branch: Arc<str>,
    pub changed: usize,
    pub ahead: u32,
}

impl ProjectRow {
    fn new(folder: PathBuf) -> Self {
        ProjectRow {
            name: project_name(&folder).into(),
            folder,
            branch: Arc::from(""),
            changed: 0,
            ahead: 0,
        }
    }
}

pub fn project_name(folder: &Path) -> String {
    folder.file_name().map_or_else(
        || folder.display().to_string(),
        |name| name.to_string_lossy().into_owned(),
    )
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

    pub fn selected(&self) -> Option<&ProjectRow> {
        self.rows.get(usize::try_from(self.selected).ok()?)
    }

    pub fn folder(&self) -> Option<&Path> {
        self.selected().map(|row| row.folder.as_path())
    }

    pub fn row_mut(&mut self, folder: &Path) -> Option<&mut ProjectRow> {
        self.rows.iter_mut().find(|row| row.folder == folder)
    }

    pub fn recent(&mut self, folders: Vec<PathBuf>) {
        let open = self.folder().map(Path::to_path_buf);
        let mut known = std::mem::take(&mut self.rows);
        self.rows = folders
            .into_iter()
            .map(
                |folder| match known.iter().position(|row| row.folder == folder) {
                    Some(at) => known.swap_remove(at),
                    None => ProjectRow::new(folder),
                },
            )
            .collect();
        if let Some(open) = open {
            self.select(open);
        }
    }

    pub fn open(&mut self, folder: PathBuf) {
        let row = self.select(folder);
        row.branch = Arc::from("");
        row.changed = 0;
        row.ahead = 0;
    }

    fn select(&mut self, folder: PathBuf) -> &mut ProjectRow {
        let at = match self.rows.iter().position(|row| row.folder == folder) {
            Some(at) => at,
            None => {
                self.rows.push(ProjectRow::new(folder));
                self.rows.len() - 1
            }
        };
        self.selected = u32::try_from(at).unwrap_or(u32::MAX);
        &mut self.rows[at]
    }
}
