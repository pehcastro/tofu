package main

type shape struct {
	agent     string
	task      string
	workspace string
}

func shapes() []shape {
	return []shape{
		{"", "make the desk faster", ""},
		{"", "fix the bug where the chat scroll jumps in interface/tui/chat.go and add tests", ""},
		{"", "rename konst.Foo to konst.Bar in internal/konst", ""},
		{"", "add a cache to internal/search, internal/turn and cmd/tofu, with tests", ""},
		{"go-dev", "add a cache to internal/search/symbols.go and a test that drives it", ""},
		{"go-dev", "fix the flaky test in internal/turn/loop_test.go", ""},
		{"rust-dev", "fix the scrollbar in apps/desk/crates/ui/src/components/scroll.rs so it eases, with a drive", ""},
		{"rust-dev", "add a react-free settings form to apps/desk/crates/desk/src/screens/theme/mod.rs", ""},
		{"qa", "write a test plan for the ask_person timeout", ""},
		{"research", "research how rivals handle themes", ""},
		{"ts-dev", "add a retry to the fetch helper in docs/site/fetch.ts", ""},
		{"go-dev", "refactor internal/turn/inbox.go: extract the judge into its own type", ""},
		{"", "the chat scroll in interface/tui/chat.go jumps back to the top after a resize, find out why it happens", ""},
		{"", "look over my last change to internal/turn/inbox.go and tell me what is wrong with it before I merge", ""},
		{"", "plan the work to add a search index across internal/search and cmd/tofu as an epic with tickets", ""},
		{"ts-dev", "fix the dialog in src/app.tsx so Escape closes it", "react"},
		{"ts-dev", "fix the counter in src/routes/+page.svelte so it updates when clicked", "svelte"},
		{"ts-dev", "fix the form in src/App.vue so it shows the saved name", "vue"},
		{"py-dev", "fix the retry in src/app/fetch.py so it closes the session on a timeout", "python"},
		{"browser", "open example.com and report the page title", ""},
		{"", "write a PRD for the login flow", ""},
		{"rust-dev", "add a dropdown menu to crates/ui/src/panel.rs that closes when I click outside it", "gpui"},
		{"rust-dev", "crates/ui/src/panel.rs shows a stale count after the list reloads", "gpui"},
		{"", "pick up the next ticket on the board and get it done", "board"},
		{"go-dev", "fix the ticket DEMO-1: the save button in main.go does nothing", "board"},
		{"", "go to example.com and tell me what the page title is", ""},
		{"", "write an AGENTS.md for this repo from the conventions it already follows", ""},
		{"", "what did you mean by [quote#ab12]", ""},
		{"", "debug why the chat scroll in interface/tui/chat.go jumps to the top after a resize", ""},
		{"", "review my last change to internal/turn/inbox.go before I merge", ""},
	}
}
