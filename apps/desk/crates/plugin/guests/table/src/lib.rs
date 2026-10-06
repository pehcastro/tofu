wit_bindgen::generate!({ path: "../../../../wit/since_v0.1.0", world: "plugin" });

use tofu::desk::host::notify;
use tofu::desk::types::{Kind, Node, Table};

const ROWS: u64 = 10_000;
const STATUSES: [&str; 3] = ["idle", "running", "done"];

struct Rows;

fn node(key: String, kind: Kind, children: Vec<u32>) -> Node {
    Node {
        key,
        kind,
        children,
    }
}

impl Guest for Rows {
    fn view() -> Vec<Node> {
        let columns = ["id", "name", "status"].map(String::from).to_vec();
        vec![node(
            "table".into(),
            Kind::Table(Table {
                columns,
                row_count: ROWS,
            }),
            vec![],
        )]
    }

    fn rows(start: u64, end: u64) -> Vec<Node> {
        let span = start..end.min(ROWS);
        let mut tree = vec![node("rows".into(), Kind::Group("rows".into()), Vec::new())];
        for row in span {
            let at = tree.len() as u32;
            tree[0].children.push(at);
            let status = STATUSES[(row % 3) as usize];
            tree.push(node(
                format!("r{row}"),
                Kind::Row,
                vec![at + 1, at + 2, at + 3],
            ));
            tree.push(node(
                format!("r{row}.id"),
                Kind::Text(row.to_string()),
                vec![],
            ));
            tree.push(node(
                format!("r{row}.name"),
                Kind::Text(format!("session {row}")),
                vec![],
            ));
            tree.push(node(
                format!("r{row}.status"),
                Kind::Chip(status.into()),
                vec![],
            ));
        }
        tree
    }

    fn handle(event: Event) {
        if let Event::Click(key) = event {
            notify("table", &format!("clicked {key}"))
                .unwrap_or_else(|denied| panic!("{}", denied.permission));
        }
    }
}

export!(Rows);
