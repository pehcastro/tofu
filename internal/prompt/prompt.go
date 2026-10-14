package prompt

const PreferTheToolOverTheShell = "prefer the tool that does the thing over a shell command that imitates it. " +
	"pick edit over sed -i, perl -pi or a redirect into a source file; glob over find, ls -R, du and wc over the tree; " +
	"search over grep and rg; read over cat, head and tail. " +
	"never write a throwaway script to change a file: that is what edit is. " +
	"when you hold a tool that runs one of tofu's own checks with tofu's own parser, " +
	"use it rather than a shell command or a guess in prose when you want to know whether the work holds up."

const TheFormatContract = "finish by saying what changed, naming every file you wrote, " +
	"and pasting the real output of every command you ran. " +
	"a command you did not run is not evidence, and a claim with no command behind it is one the next reader has to redo. " +
	"say what you could not verify and why in the same answer rather than leaving it out."
