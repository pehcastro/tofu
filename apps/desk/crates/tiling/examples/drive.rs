use desk_tiling::{
    Grab, Mods, Module, Preset, Preview, Rect, Refusal, SHORTCUTS, Side, Spawned, Store, Target,
    TileId, Workspace, Zone, action, aim, readout, target,
};

fn area(w: f32, h: f32) -> Rect {
    Rect {
        x: 0.0,
        y: 0.0,
        w,
        h,
    }
}

fn show(title: &str, workspace: &Workspace, at: Rect) {
    println!("== {title}");
    for line in readout(workspace.tree()) {
        println!("   {line}");
    }
    for (stack, rect) in workspace.tiles(at) {
        let name = stack.modules.get(stack.active).map_or("", Module::name);
        println!(
            "   tile {} {name}: x {:.0} y {:.0} w {:.0} h {:.0}",
            stack.id.0, rect.x, rect.y, rect.w, rect.h
        );
    }
}

fn main() -> Result<(), Refusal> {
    let big = area(1100.0, 760.0);
    let work = Workspace::new("work", Preset::Work);
    show("work preset", &work, big);
    let strip = work.open_at(Module::Terminal, Target::Edge(Side::Bottom), big)?;
    show(
        "terminal dropped on the bottom edge, 1100 by 760",
        &strip,
        big,
    );
    show(
        "same, window grown to 1400 by 900",
        &strip,
        area(1400.0, 900.0),
    );
    show(
        "same, window shrunk to 900 by 600",
        &strip,
        area(900.0, 600.0),
    );

    let before = readout(strip.tree());
    let divider = strip
        .dividers(big)
        .into_iter()
        .find(|divider| divider.path == [0])
        .ok_or(Refusal::NoSuchTile)?;
    let dragged = strip.resize(&divider, divider.rect.x + 80.0, divider.rect.y)?;
    show("divider dragged 80 px right", &dragged, big);
    let nudged = dragged
        .focus_tile(desk_tiling::TileId(0))?
        .nudge(Side::Right, 32.0, big)?;
    show("tile 0 nudged right by 32", &nudged, big);
    let swapped = nudged.swap(desk_tiling::TileId(0), desk_tiling::TileId(1))?;
    show("tiles 0 and 1 swapped", &swapped, big);
    let zoomed = swapped.zoom()?;
    show("zoomed", &zoomed, big);
    let unzoomed = zoomed.zoom()?;
    let closed = unzoomed.close_tile(desk_tiling::TileId(2), big)?;
    show("terminal strip closed", &closed, big);
    let undone = closed.undo()?.undo()?.undo()?.undo()?;
    show("undo four times", &undone, big);
    println!(
        "== undo returns the original tree: {}",
        readout(undone.tree()) == before
    );

    let room = area(1600.0, 1000.0);
    let mut many = Workspace::new("grid", Preset::Editor);
    let refusal = loop {
        match many.open(Module::Shells, room) {
            Ok(next) => many = next,
            Err(refusal) => break refusal,
        }
    };
    println!(
        "== open until refused at 1600 by 1000: {} tiles, refused: {refusal}",
        many.tiles(room).len()
    );
    let mut twelve = many.clone();
    let refusal = loop {
        let target = Target::Edge(Side::Left);
        match twelve.open_at(Module::Browser, target, area(4000.0, 1000.0)) {
            Ok(next) => twelve = next,
            Err(refusal) => break refusal,
        }
    };
    println!(
        "== then left edge strips on a wide area: {} tiles, refused: {refusal}",
        twelve.tiles(area(4000.0, 1000.0)).len()
    );

    let screen = area(1100.0, 700.0);
    let mut tabs = vec![Workspace::new("work", Preset::Work)];
    for module in [
        Module::Browser,
        Module::Editor,
        Module::Browser,
        Module::Chat,
        Module::Browser,
    ] {
        let before = tabs.len();
        let active = tabs.last().ok_or(Refusal::NoSuchTile)?;
        let (shown, verdict) = match active.spawn(module.clone(), screen)? {
            Spawned::Here(next) => {
                *tabs.last_mut().ok_or(Refusal::NoSuchTile)? = next;
                (tabs.len() - 1, "here")
            }
            Spawned::NewTab(fresh) => {
                tabs.push(fresh);
                (tabs.len() - 1, "new tab")
            }
        };
        let name = &tabs.get(shown).ok_or(Refusal::NoSuchTile)?.name;
        println!(
            "== spawn {} at 1100 by 700: tabs {before} -> {}, {verdict}, page shows tab {name}",
            module.name(),
            tabs.len()
        );
    }
    for tab in &tabs {
        show(&format!("tab {}", tab.name), tab, screen);
    }

    let mut clicked = vec![Workspace::new("fresh", Preset::Empty)];
    let mut active = 0;
    let mut menu = Module::BUILT_IN.to_vec();
    menu.push(Module::Plugin("data-studio".to_owned()));
    println!("== Spawn menu clicks on a fresh workspace at 1100 by 700");
    for module in menu {
        let current = clicked.get(active).ok_or(Refusal::NoSuchTile)?;
        match current.spawn(module.clone(), screen)? {
            Spawned::Here(next) => *clicked.get_mut(active).ok_or(Refusal::NoSuchTile)? = next,
            Spawned::NewTab(fresh) => {
                clicked.push(fresh);
                active = clicked.len() - 1;
            }
        }
        let shown = clicked.get(active).ok_or(Refusal::NoSuchTile)?;
        println!(
            "   click {:<15} tab {} of {}, tiles in that tab {}",
            module.name(),
            active + 1,
            clicked.len(),
            shown.tiles(screen).len()
        );
    }

    let corner_area = area(1100.0, 760.0);
    let strip = Workspace::new("work", Preset::Work).open_at(
        Module::Terminal,
        Target::Edge(Side::Bottom),
        corner_area,
    )?;
    let corner = strip
        .corners(corner_area)
        .into_iter()
        .find(|corner| {
            corner.tile == TileId(0)
                && corner.horizontal == Side::Right
                && corner.vertical == Side::Bottom
        })
        .ok_or(Refusal::NoSuchTile)?;
    let press = (
        corner.rect.x + corner.rect.w / 2.0,
        corner.rect.y + corner.rect.h / 2.0,
    );
    println!(
        "== Alt + right press at x {:.0} y {:.0}, inside the corner zone {:?} of tile 0, touching {} dividers",
        press.0,
        press.1,
        corner.rect,
        corner.dividers.len()
    );
    let positions = |workspace: &Workspace| -> Vec<String> {
        workspace
            .dividers(corner_area)
            .iter()
            .filter(|divider| {
                corner
                    .dividers
                    .iter()
                    .any(|own| own.path == divider.path && own.index == divider.index)
            })
            .map(|divider| match divider.axis {
                desk_tiling::Axis::Row => format!("Row divider at x {:.0}", divider.rect.x),
                desk_tiling::Axis::Column => format!("Column divider at y {:.0}", divider.rect.y),
            })
            .collect()
    };
    println!("   before: {:?}", positions(&strip));
    for (dx, dy) in [(120.0, -90.0), (-2000.0, -2000.0), (2000.0, 2000.0)] {
        let dragged = strip.resize_corner(&corner, dx, dy)?;
        println!(
            "   dragged to x {:.0} y {:.0}: {:?}",
            press.0 + dx,
            press.1 + dy,
            positions(&dragged)
        );
        for (stack, rect) in dragged.tiles(corner_area) {
            println!("      tile {} w {:.0} h {:.0}", stack.id.0, rect.w, rect.h);
        }
    }

    placement(big)?;

    let pair = Workspace::new("pair", Preset::Work);
    let divider = pair
        .dividers(big)
        .into_iter()
        .next()
        .ok_or(Refusal::NoSuchTile)?;
    let shrink = pair.resize(&divider, 100.0, 0.0);
    println!(
        "== drag the divider to x 100: {:?}",
        shrink.err().map(|refusal| refusal.to_string())
    );

    let mut six = Workspace::new("six", Preset::Work);
    for module in [
        Module::Editor,
        Module::Browser,
        Module::Terminal,
        Module::SourceControl,
    ] {
        six = six.open(module, big)?;
    }
    let small = area(452.0, 250.0);
    show("six tiles squeezed into 452 by 250", &six, small);
    let inside = six.tiles(small).iter().all(|(_, rect)| {
        rect.x >= small.x - 0.01
            && rect.y >= small.y - 0.01
            && rect.x + rect.w <= small.x + small.w + 0.01
            && rect.y + rect.h <= small.y + small.h + 0.01
    });
    println!("== every tile inside the area: {inside}");

    if let Some(path) = std::env::args().nth(1) {
        stored(&path, &strip);
    }
    Ok(())
}

fn placement(big: Rect) -> Result<(), Refusal> {
    let work = Workspace::new("work", Preset::Work);
    let tiles: Vec<(TileId, Rect)> = work
        .tiles(big)
        .into_iter()
        .map(|(stack, rect)| (stack.id, rect))
        .collect();
    let probe = |label: &str, grab: &Grab, x: f32, y: f32| -> Option<(Target, Option<Preview>)> {
        let found = target(work.tree(), big, x, y);
        let preview = found.and_then(|found| work.preview(grab, found, big));
        println!("   {label} at ({x}, {y}): {found:?} -> {preview:?}");
        found.map(|found| (found, preview))
    };
    let sub_agents = Grab::Placed {
        tile: TileId(1),
        index: 0,
    };
    let chat = Grab::Placed {
        tile: TileId(0),
        index: 0,
    };
    println!("== placement probes on the work preset at 1100 by 760, after");
    probe("F1 Sub-agents onto Chat's header", &sub_agents, 300.0, 10.0);
    probe(
        "F1b Sub-agents onto Chat's header lower",
        &sub_agents,
        300.0,
        30.0,
    );
    probe(
        "F2 Sub-agents onto Chat's centre",
        &sub_agents,
        300.0,
        380.0,
    );
    probe(
        "F4 Sub-agents 10 px inside Chat's body, left",
        &sub_agents,
        10.0,
        400.0,
    );
    probe(
        "F5a edge drop, 10 px left of the area",
        &sub_agents,
        -10.0,
        400.0,
    );
    probe("F5b side split of Chat, left", &sub_agents, 40.0, 400.0);
    probe(
        "F6 Chat onto its own body (one module)",
        &chat,
        300.0,
        380.0,
    );
    probe(
        "F7 Sub-agents onto its own header",
        &sub_agents,
        800.0,
        10.0,
    );
    probe(
        "F8 Sub-agents onto one-module Chat header",
        &sub_agents,
        100.0,
        10.0,
    );
    probe(
        "F9 drag from own header, 5 px past threshold",
        &sub_agents,
        765.0,
        10.0,
    );
    probe(
        "F10a near bottom edge, inside Chat body",
        &sub_agents,
        300.0,
        745.0,
    );
    probe("F10b 10 px below the area", &sub_agents, 300.0, 770.0);
    probe("F11 30 px below the area", &sub_agents, 300.0, 790.0);
    probe(
        "F12 in the gap between tiles, mid height",
        &sub_agents,
        604.0,
        380.0,
    );
    probe(
        "F13 in the gap between tiles, 10 px from the bottom",
        &sub_agents,
        604.0,
        750.0,
    );

    println!(
        "== sweep a new Terminal across tile 0 from header to centre to each side to the edge"
    );
    let terminal = Grab::New(Module::Terminal);
    let mut stack_full = false;
    let mut edge_in_body = false;
    for (label, x, y) in [
        ("header", 300.0, 20.0),
        ("centre", 300.0, 380.0),
        ("left", 60.0, 380.0),
        ("right", 560.0, 380.0),
        ("top", 300.0, 120.0),
        ("bottom", 300.0, 680.0),
        ("left workspace edge", -12.0, 380.0),
        ("bottom workspace edge", 300.0, 772.0),
        ("top workspace edge", 300.0, -12.0),
    ] {
        let Some((found, preview)) = probe(label, &terminal, x, y) else {
            continue;
        };
        let in_body = tiles.iter().any(|(_, rect)| rect.contains(x, y));
        edge_in_body |= matches!(found, Target::Edge(_)) && in_body;
        stack_full |= matches!(preview, Some(Preview::Split(rect)) if matches!(found, Target::Tile(_, Zone::Stack { .. })) && tiles.iter().any(|(_, tile)| *tile == rect));
    }
    println!(
        "   stack ever previews a full tile: {stack_full}; edge ever inside a tile body: {edge_in_body}"
    );

    println!("== hold: 1 px steps right from x 175 to 200, then left to 160, at y 380 on tile 0");
    let mut held = None;
    let steps = (175..=200).chain((160..200).rev());
    for x in steps {
        let x = x as f32;
        held = aim(work.tree(), big, x, 380.0, held);
        let fresh = target(work.tree(), big, x, 380.0);
        println!("   x {x:.0}: held {held:?}, without hold {fresh:?}");
    }

    println!("== Sub-agents dropped into its own header at index 3: a reorder");
    let reordered = work.dropped(
        &sub_agents,
        Target::Tile(TileId(1), Zone::Stack { at: 3 }),
        big,
    )?;
    show("reordered", &reordered, big);
    let stacked = work.dropped(&chat, Target::Tile(TileId(1), Zone::Stack { at: 1 }), big)?;
    show("Chat stacked into tile 1 at index 1", &stacked, big);

    println!("== Ctrl Shift Enter: tile 1 to a new workspace, then undo");
    let (left, fresh) = work.send_to_new(TileId(1), big)?;
    show("source after", &left, big);
    show(&format!("new workspace {}", fresh.name), &fresh, big);
    show("source after undo", &left.undo()?, big);

    println!("== shortcut table");
    for shortcut in SHORTCUTS {
        println!(
            "   {:<18} {:<26} {:?}",
            shortcut.keys, shortcut.label, shortcut.action
        );
    }
    let mods = |ctrl, alt, shift| Mods { ctrl, alt, shift };
    for (held, key) in [
        (mods(true, false, false), "t"),
        (mods(true, false, false), "3"),
        (mods(true, false, true), "enter"),
        (mods(true, true, false), "enter"),
        (mods(true, true, false), "w"),
        (mods(true, true, false), "e"),
        (mods(true, true, false), "r"),
        (mods(true, true, false), "l"),
        (mods(true, false, false), "z"),
        (mods(false, false, false), "escape"),
        (mods(true, true, false), "z"),
        (mods(true, true, false), "1"),
        (mods(true, true, false), "n"),
        (mods(true, true, false), "o"),
    ] {
        println!("   {held:?} {key}: {:?}", action(held, key));
    }
    Ok(())
}

fn stored(path: &str, strip: &Workspace) {
    let v1 = "tofu-desk-layout 1\n(row 0.62 (tile 0 chat) 0.38 (column 0.5 (tile 1 sub-agents file-edits) 0.5 (tile 0 shells)))\n";
    let store = Store::at(path.into());
    let wrote = std::fs::write(path, v1);
    println!("== a version 1 file written: {wrote:?}");
    match store.load() {
        Ok(Some(loaded)) => {
            for workspace in &loaded {
                show(
                    &format!("version 1 loaded as workspace {}", workspace.name),
                    workspace,
                    area(1100.0, 760.0),
                );
            }
        }
        other => println!("== version 1 did not load: {other:?}"),
    }
    let all = [
        strip.clone(),
        Workspace::new("editor", Preset::Editor),
        Workspace::new("scratch", Preset::Empty),
    ];
    println!(
        "== saved as version 2: {:?}",
        store.save(&all).map_err(|error| error.to_string())
    );
    println!("{}", std::fs::read_to_string(path).unwrap_or_default());
    match store.load() {
        Ok(Some(loaded)) => println!(
            "== version 2 round trip keeps every tree: {}",
            loaded
                .iter()
                .zip(&all)
                .all(|(a, b)| a.tree() == b.tree() && a.name == b.name && a.focus() == b.focus())
        ),
        other => println!("== version 2 did not load: {other:?}"),
    }
}
