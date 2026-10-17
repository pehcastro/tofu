use std::env;
use std::error::Error;
use std::fs;
use std::path::PathBuf;

fn main() -> Result<(), Box<dyn Error>> {
    println!("cargo::rerun-if-env-changed=TOFU_DESKTOP_CHANNEL");
    let icon = match env::var("TOFU_DESKTOP_CHANNEL").as_deref() {
        Ok("release") => "release.ico",
        _ => "dev.ico",
    };
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("icons")
        .join(icon);
    println!("cargo::rerun-if-changed={}", path.display());
    let rc = PathBuf::from(env::var("OUT_DIR")?).join("icon.rc");
    let quoted = path.display().to_string().replace('\\', "/");
    fs::write(&rc, format!("1 ICON \"{quoted}\"\n"))?;
    embed_resource::compile(&rc, embed_resource::NONE).manifest_required()?;
    Ok(())
}
