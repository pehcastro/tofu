use std::error::Error;
use std::time::Instant;

use desk_plugin::{Event, Host, Manifest};
use wit_component::ComponentEncoder;

const ROWS: u64 = 10_000;
const RUNS: usize = 50;

fn main() -> Result<(), Box<dyn Error>> {
    let mut args = std::env::args().skip(1);
    let (Some(toml), Some(wasm)) = (args.next(), args.next()) else {
        return Err("usage: drive <plugin.toml> <core.wasm>".into());
    };
    let manifest = Manifest::parse(&std::fs::read_to_string(toml)?)?;
    let component = ComponentEncoder::default()
        .validate(true)
        .module(&std::fs::read(wasm)?)?
        .encode()?;
    let host = Host::new()?;
    let fresh = || host.load(manifest.clone(), &component);
    println!(
        "plugin {} {}, permissions {:?}",
        manifest.name, manifest.version, manifest.permissions
    );

    match fresh()?.view() {
        Ok(tree) => println!(
            "view: {} nodes, root {:?}",
            tree.len(),
            tree.first().map(|n| &n.kind)
        ),
        Err(e) => println!("view: {e}"),
    }

    let mut plugin = fresh()?;
    let mut times = Vec::with_capacity(RUNS);
    for _ in 0..RUNS {
        let started = Instant::now();
        let outcome = plugin.rows(0..ROWS);
        times.push(started.elapsed());
        if let Err(e) = outcome {
            println!(
                "rows(0..{ROWS}): returned after {:?}: {e}",
                started.elapsed()
            );
            break;
        }
    }
    if times.len() == RUNS {
        let nodes = plugin.rows(0..ROWS)?.len();
        times.sort();
        println!(
            "rows(0..{ROWS}): {nodes} nodes, {RUNS} calls, min {:?} median {:?} max {:?}",
            times[0],
            times[RUNS / 2],
            times[RUNS - 1]
        );
    }

    let mut plugin = fresh()?;
    match plugin.handle(&Event::Click("r0".into())) {
        Ok(()) => println!("handle(click r0): ok, notices {:?}", plugin.take_notices()),
        Err(e) => println!("handle(click r0): {e}"),
    }
    Ok(())
}
