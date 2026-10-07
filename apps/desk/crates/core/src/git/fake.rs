use std::collections::HashMap;

use super::{Against, BlameLine, Entry, Git, GitError, Hunk};

#[derive(Debug, Clone, Default)]
pub struct FakeGit {
    pub entries: Vec<Entry>,
    pub hunks: HashMap<String, Vec<Hunk>>,
    pub blame: HashMap<String, Vec<BlameLine>>,
    pub branches: Vec<String>,
    pub current: Option<String>,
}

impl Git for FakeGit {
    fn status(&self) -> Result<Vec<Entry>, GitError> {
        Ok(self.entries.clone())
    }

    fn diff(&self, path: &str, _: Against) -> Result<Vec<Hunk>, GitError> {
        Ok(self.hunks.get(path).cloned().unwrap_or_default())
    }

    fn blame(&self, path: &str, _: Option<&str>) -> Result<Vec<BlameLine>, GitError> {
        Ok(self.blame.get(path).cloned().unwrap_or_default())
    }

    fn stage(&self, _: &[String]) -> Result<(), GitError> {
        Ok(())
    }

    fn unstage(&self, _: &[String]) -> Result<(), GitError> {
        Ok(())
    }

    fn discard(&self, _: &[String]) -> Result<(), GitError> {
        Ok(())
    }

    fn commit(&self, _: &str) -> Result<(), GitError> {
        Ok(())
    }

    fn branches(&self) -> Result<Vec<String>, GitError> {
        Ok(self.branches.clone())
    }

    fn current_branch(&self) -> Result<Option<String>, GitError> {
        Ok(self.current.clone())
    }

    fn switch(&self, _: &str) -> Result<(), GitError> {
        Ok(())
    }
}
