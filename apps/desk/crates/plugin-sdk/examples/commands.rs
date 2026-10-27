use std::error::Error;
use std::fs;
use std::path::{Path, PathBuf};
use std::time::{Instant, SystemTime, UNIX_EPOCH};

use desk_plugin::{Home, Host, Plugin};
use wit_component::ComponentEncoder;

const RUNS: usize = 20;
const MISSING: &str = "\n[[contributes.commands]]\nname = \"commands.missing\"\ntitle = \"Named here, absent from the export\"\n";

fn component(core: &Path) -> Result<Vec<u8>, Box<dyn Error>> {
    let module = fs::read(core).map_err(|e| format!("{}: {e}", core.display()))?;
    Ok(ComponentEncoder::default()
        .validate(true)
        .module(&module)?
        .encode()?)
}

fn install(home: &Path, id: &str, manifest: &str, wasm: &[u8]) -> Result<(), Box<dyn Error>> {
    let dir = Home::at(home).plugins.join(id);
    fs::create_dir_all(&dir)?;
    fs::write(dir.join("plugin.toml"), manifest)?;
    fs::write(dir.join("plugin.wasm"), wasm)?;
    Ok(())
}

fn fresh() -> Result<PathBuf, Box<dyn Error>> {
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos();
    let home = std::env::temp_dir().join(format!("desk-379-{}-{nanos}", std::process::id()));
    fs::create_dir_all(&home)?;
    Ok(home)
}

fn main() -> Result<(), Box<dyn Error>> {
    let sdk = Path::new(env!("CARGO_MANIFEST_DIR"));
    let guest = sdk.join("guests/commands");
    let wasm = component(&guest.join("target/wasm32-unknown-unknown/release/commands.wasm"))?;
    let shipped = fs::read_to_string(guest.join("plugin.toml"))?;
    let honest = shipped
        .replacen("local.commands", "local.commands-honest", 1)
        .replace(MISSING, "");
    let lite = sdk.join("../../examples/plugins/notes-lite");
    let old = component(&lite.join("target/wasm32-unknown-unknown/release/notes_lite.wasm"))?;
    let home = fresh()?;
    println!("plugins home, made fresh: {}", home.display());
    install(&home, "local.commands", &shipped, &wasm)?;
    install(&home, "local.commands-honest", &honest, &wasm)?;
    install(
        &home,
        "local.notes-lite",
        &fs::read_to_string(lite.join("plugin.toml"))?,
        &old,
    )?;
    let host = Host::new(&Home::at(&home))?;
    let found = host.discover(None);
    for refused in &found.refused {
        println!("discover refused: {refused}");
    }
    let mut loaded: Vec<Plugin> = Vec::new();
    for plugin in &found.plugins {
        match host.load(plugin) {
            Ok((running, _)) => {
                let listed: Vec<String> = running
                    .commands()
                    .iter()
                    .map(|c| format!("{} ({}, args {})", c.id, c.title, c.args_schema))
                    .collect();
                println!(
                    "loaded {} on {:?}, commands [{}]",
                    plugin.manifest.id,
                    plugin.manifest.api,
                    listed.join(", ")
                );
                loaded.push(running);
            }
            Err(e) => println!("load {}: {e}", plugin.manifest.id),
        }
    }
    let find = |id: &str| loaded.iter().find(|p| p.id().as_str() == id);
    let honest = find("local.commands-honest").ok_or("local.commands-honest did not load")?;
    let mut took = Vec::with_capacity(RUNS);
    for run in 0..RUNS {
        let started = Instant::now();
        let answer = honest
            .run("commands.echo".into(), format!("{{\"run\":{run}}}"))
            .wait()?;
        took.push(started.elapsed().as_secs_f64() * 1000.0);
        if run == 0 {
            println!("local.commands-honest run commands.echo: {answer:?}");
        }
    }
    let unknown = honest.run("commands.missing".into(), "{}".into()).wait()?;
    println!("local.commands-honest run commands.missing: {unknown:?}");
    took.sort_by(f64::total_cmp);
    let median = took.get(RUNS / 2).copied().unwrap_or_default();
    let max = took.last().copied().unwrap_or_default();
    println!(
        "bench run commands.echo in process, {RUNS} runs: median {median:.3} ms, max {max:.3} ms"
    );
    let lite = find("local.notes-lite").ok_or("local.notes-lite did not load")?;
    println!(
        "desk@0.2 notes-lite run commands.echo: {:?}",
        lite.run("commands.echo".into(), "{}".into()).wait()?
    );
    drop(loaded);
    fs::remove_dir_all(&home)?;
    Ok(())
}
