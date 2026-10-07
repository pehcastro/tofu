use desk_tiling::{
    Action, Mods, Module, Preset, Rect, Refusal, Side, Target, Workspace, action, readout,
};

fn rects(title: &str, workspace: &Workspace, at: Rect) -> Vec<String> {
    let lines: Vec<String> = workspace
        .tiles(at)
        .into_iter()
        .map(|(_, rect)| {
            format!(
                "x {:.0} y {:.0} w {:.0} h {:.0}",
                rect.x, rect.y, rect.w, rect.h
            )
        })
        .collect();
    println!("== {title} {:?}", readout(workspace.tree()));
    for line in &lines {
        println!("   {line}");
    }
    lines
}

fn press(workspace: &Workspace, key: &str) -> Workspace {
    let mods = Mods {
        ctrl: true,
        alt: true,
        shift: false,
    };
    match action(mods, key) {
        Some(Action::Reset) => workspace.reset(),
        Some(Action::Even) => workspace.even(),
        other => panic!("Ctrl Alt {key} is {other:?}"),
    }
}

fn main() -> Result<(), Refusal> {
    let at = Rect {
        x: 0.0,
        y: 0.0,
        w: 1100.0,
        h: 760.0,
    };
    let preset = Workspace::new("work", Preset::Work);
    let preset_rects = rects("preset", &preset, at);
    let mut changed = preset.open_at(Module::Terminal, Target::Edge(Side::Bottom), at)?;
    changed.locked = true;
    let before = rects("locked, before keys", &changed, at);
    let reset = press(&changed, "r");
    let after_reset = rects("locked, after Ctrl Alt R", &reset, at);
    let even = press(&reset, "e");
    let after_even = rects("locked, after Ctrl Alt E", &even, at);
    println!(
        "locked unchanged: {}",
        before == after_reset && before == after_even && even == changed
    );
    let mut unlocked = even;
    unlocked.locked = false;
    let reset = press(&unlocked, "r");
    let after = rects("unlocked, after Ctrl Alt R", &reset, at);
    println!("unlocked equals preset: {}", after == preset_rects);
    Ok(())
}
