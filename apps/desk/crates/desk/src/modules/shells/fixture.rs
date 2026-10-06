use gpui::Rgba;

use super::paint::{LIVE, WARN, hex};

pub const SUMMARY: &str = "4 running · 1 failed · 1 waiting on you";

#[derive(Clone, Copy)]
pub enum State {
    Running,
    Failed,
    Waiting,
}

impl State {
    pub fn dot(self) -> Rgba {
        match self {
            State::Running => LIVE,
            State::Failed => DEL_DOT,
            State::Waiting => WARN,
        }
    }
}

const DEL_DOT: Rgba = hex(0xf1737d);

pub struct Shell {
    pub command: &'static str,
    pub by: &'static str,
    pub ink: Rgba,
    pub state: State,
    pub port: &'static str,
    pub age: &'static str,
    pub pid: &'static str,
    pub output: &'static [&'static str],
}

pub const SHELLS: [Shell; 6] = [
    Shell {
        command: "vite dev",
        by: "ts-dev 1",
        ink: hex(0xb9a6ea),
        state: State::Running,
        port: ":5871",
        age: "running 14m",
        pid: "pid 21811",
        output: &[
            "> notes-web@0.4.0 dev",
            "> vite",
            "",
            "  VITE v6.2.0  ready in 312 ms",
            "",
            "  ➜  Local:   http://localhost:5871/",
            "  ➜  Network: use --host to expose",
            "",
            "15:09:12 [vite] page reload src/count.ts",
            "15:11:40 [vite] hmr update /src/NoteList.tsx",
            "15:14:02 [vite] hmr update /src/NoteList.tsx",
            "15:19:55 [vite] page reload src/Header.tsx",
            "15:22:31 [vite] hmr update /src/Header.tsx",
        ],
    },
    Shell {
        command: "go test ./store/... -run SQLite",
        by: "go-dev 3",
        ink: hex(0x79c0ff),
        state: State::Running,
        port: "",
        age: "running under a minute",
        pid: "pid 22107",
        output: &[
            "=== RUN   TestSQLiteAll",
            "=== RUN   TestSQLiteAll/empty",
            "--- PASS: TestSQLiteAll/empty (0.00s)",
            "=== RUN   TestSQLiteAll/one",
            "--- PASS: TestSQLiteAll/one (0.01s)",
            "=== RUN   TestSQLiteAll/many",
            "--- PASS: TestSQLiteAll/many (0.04s)",
            "=== RUN   TestSQLiteAll/deleted",
        ],
    },
    Shell {
        command: "npx playwright test delete.spec.ts",
        by: "qa 1",
        ink: hex(0xf0a3b5),
        state: State::Running,
        port: "",
        age: "running 1m",
        pid: "pid 22240",
        output: &[
            "Running 3 tests using 1 worker",
            "",
            "  ✓  1 delete.spec.ts:8 › delete removes the row (1.2s)",
            "  ✓  2 delete.spec.ts:21 › badge goes down (0.9s)",
            "  …  3 delete.spec.ts:34 › undo restores the row",
        ],
    },
    Shell {
        command: "uvicorn api:app --reload",
        by: "py-dev 2",
        ink: hex(0xc7d97a),
        state: State::Running,
        port: ":8000",
        age: "running 6m",
        pid: "pid 21844",
        output: &[
            "INFO:     Uvicorn running on http://127.0.0.1:8000 (Press CTRL+C to quit)",
            "INFO:     Started reloader process [21844]",
            "INFO:     Application startup complete.",
            "INFO:     127.0.0.1:52011 - \"GET /notes HTTP/1.1\" 200 OK",
            "INFO:     127.0.0.1:52011 - \"POST /seed HTTP/1.1\" 201 Created",
        ],
    },
    Shell {
        command: "go test ./notes/ -run TestDelete",
        by: "go-dev 5",
        ink: hex(0x79c0ff),
        state: State::Failed,
        port: "",
        age: "exited 3m ago",
        pid: "pid 22311",
        output: &[
            "=== RUN   TestDelete",
            "=== RUN   TestDelete/soft",
            "    notes_test.go:88: Count after delete: want 2, got 3",
            "--- FAIL: TestDelete/soft (0.02s)",
            "--- FAIL: TestDelete (0.02s)",
            "FAIL    notes-app/notes 0.214s",
            "exit status 1",
        ],
    },
    Shell {
        command: "npm run build",
        by: "lead",
        ink: hex(0xd9d6d0),
        state: State::Waiting,
        port: "",
        age: "running under a minute",
        pid: "pid 22402",
        output: &["waiting for your answer in the chat: allow once, deny, or always here"],
    },
];
