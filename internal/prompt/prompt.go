package prompt

const PreferTheToolOverTheShell = "prefer the tool that does the thing over a shell command that imitates it: " +
	"project_report answers what is this repository in one call, glob finds files by name, " +
	"search finds text and returns the whole declaration a match sits inside, read reads one file whole, " +
	"edit replaces one exact stretch of text inside one, and write creates one or replaces it whole. " +
	"never run find, ls -R, du or wc over the tree. " +
	"find descends into every directory git ignores, because -not -path filters what it prints and does not stop it walking, " +
	"so one recorded run on this kind of tree spent 68 seconds on a find and then 153 seconds on another, " +
	"while glob answered its question in 15 milliseconds. " +
	"bash is for the project's own commands, its package manager, its build and its tests, " +
	"and for nothing one of those tools already does, and a command it runs is killed at its deadline and returns only what it printed until then. " +
	"never write a throwaway script to change a file: that is what edit is. " +
	"when you hold a tool that runs one of tofu's own checks with tofu's own parser, " +
	"use it rather than a shell command or a guess in prose when you want to know whether the work holds up."

const TheFormatContract = "finish by saying what changed, naming every file you wrote, " +
	"and pasting the real output of every command you ran. " +
	"a command you did not run is not evidence, and a claim with no command behind it is one the next reader has to redo. " +
	"say what you could not verify and why in the same answer rather than leaving it out."
