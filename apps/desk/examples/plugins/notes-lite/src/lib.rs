use desk_plugin_sdk::fs::{self, FsError};
use desk_plugin_sdk::{Event, Part, Plugin, button, editor, markdown, stack, text};

const NOTE: &str = "notes/a.md";
const ESCAPE: &str = "../../settings.json";
const BIG: &str = "notes/big.bin";

struct NotesLite {
    status: String,
    preview: Option<String>,
}

fn said(error: &FsError) -> String {
    match error {
        FsError::Denied(why) => format!("Denied: {why}"),
        FsError::Missing => "Missing".to_owned(),
        FsError::TooLarge(bytes) => format!("Too large: {bytes} bytes"),
        FsError::Failed(why) => format!("Failed: {why}"),
    }
}

fn note() -> Result<String, FsError> {
    fs::read(NOTE).map(|bytes| String::from_utf8_lossy(&bytes).into_owned())
}

impl Default for NotesLite {
    fn default() -> Self {
        let status = match note() {
            Ok(_) => format!("{NOTE} is ready"),
            Err(FsError::Missing) => match fs::write(NOTE, b"") {
                Ok(()) => format!("{NOTE} was made"),
                Err(error) => said(&error),
            },
            Err(error) => said(&error),
        };
        Self {
            status,
            preview: None,
        }
    }
}

impl Plugin for NotesLite {
    fn view(&self) -> Part {
        let preview = self.preview.as_ref().map(|text| markdown("preview", text));
        let toggle = match self.preview {
            Some(_) => "Hide preview",
            None => "Preview",
        };
        stack(
            "notes-lite",
            [
                text("title", format!("Notes lite: {NOTE}")),
                editor("note", NOTE, true, true),
                button("preview", toggle),
                button("files", "List notes/"),
                button("escape", format!("read {ESCAPE}")),
                button("big", format!("write 1 MB to {BIG}")),
                text("status", self.status.clone()),
            ]
            .into_iter()
            .chain(preview),
        )
    }

    fn handle(&mut self, event: Event) {
        match event {
            Event::Click(key) if key == "escape" => {
                self.status = match fs::read(ESCAPE) {
                    Ok(bytes) => format!("read {} bytes from outside", bytes.len()),
                    Err(error) => said(&error),
                };
            }
            Event::Click(key) if key == "big" => {
                self.status = match fs::write(BIG, &vec![b'x'; 1 << 20]) {
                    Ok(()) => format!("{BIG} written"),
                    Err(error) => said(&error),
                };
            }
            Event::Click(key) if key == "files" => {
                self.status = match fs::list("notes") {
                    Ok(entries) => entries
                        .iter()
                        .map(|entry| format!("{} ({} bytes)", entry.name, entry.bytes))
                        .collect::<Vec<_>>()
                        .join(", "),
                    Err(error) => said(&error),
                };
            }
            Event::Click(key) if key == "preview" => {
                self.preview = match self.preview.take() {
                    Some(_) => None,
                    None => Some(note().unwrap_or_else(|error| said(&error))),
                };
            }
            Event::Changed(path) => self.status = format!("{path} has unsaved changes"),
            Event::Saved(path) => {
                self.status = format!("{path} saved");
                if self.preview.is_some() {
                    self.preview = Some(note().unwrap_or_else(|error| said(&error)));
                }
            }
            Event::Click(_) | Event::Change(_) | Event::Submit(_) => {}
        }
    }
}

desk_plugin_sdk::plugin!(NotesLite);
