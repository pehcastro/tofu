package stopcheck

type Label struct {
	Want Answer
	Note string
}

const LabelSource = "hand labels written for BOJI-079 by reading every step of every session in .boji/sessions, one labeller, no second opinion. the question each label answers is: given the task and the steps up to and including this one, should the loop have run another model step? stop means no. extended on 2026-09-19 for BOJI-098, same method and same labeller, over the turns the v2 maintenance run and the arm runs recorded. a step whose right answer the recorded command and result do not settle is listed in Unlabelled rather than guessed."

func Labels() map[string]Label {
	labels := map[string]Label{
		"turn-18d690e5f16b7090#1":  {Continue, "the task asks for three things and the setup step named in notes.txt is still undone"},
		"turn-18d690e5f16b7090#2":  {Stop, "the model returned neither text nor a tool call, and nothing further came of the turn"},
		"turn-18d6913a528f4528#9":  {Stop, "bun --version had already run in step 8"},
		"turn-18d6913a528f4528#10": {Stop, "dir node_modules lists what ls node_modules already listed in step 8"},
		"turn-18d6913a528f4528#11": {Continue, "the turn went back to writing the files the task asked for"},
		"turn-18d6913a528f4528#12": {Continue, "the second file had just been written and the routes were not yet tested"},
		"turn-18d6913cafb9c294#1":  {Stop, "the model answered nothing and called no tool, so another step had nothing to work from"},
		"turn-18d69b4a2c474464#1":  {Stop, "the task was to say ok and the model said it"},
		"turn-18d6a067066b0a60#1":  {Stop, "the task was to say ok and call no tool, and the model said ok and called none"},
		"turn-18d6a5df2caeac68#1":  {Continue, "the file had been read but the marker it was read for had not been reported"},
		"turn-18d6a5df2caeac68#2":  {Stop, "the turn reported the marker the task asked for and the task asked for nothing more"},
		"turn-18d6a27c7dfb1644#32": {Continue, "the tests and the type check had just passed but the turn had not reported back"},
		"turn-18d6a27c7dfb1644#33": {Continue, "the diff had been read but the turn still had not reported back"},
		"turn-18d6a27c7dfb1644#35": {Stop, "the model returned neither text nor a tool call, and the turn hit the decision cap on the step after"},
		"turn-18d69bfed2bf52a0#2":  {Continue, "the first exploration returned little, so a broader one was worth a step"},
		"turn-18d69bfed2bf52a0#9":  {Continue, "the tests had just been written and had not been run against the routes"},
		"turn-18d69bfed2bf52a0#12": {Continue, "package.json had changed since the tests last ran"},
		"turn-18d69bfed2bf52a0#13": {Stop, "the turn reported the routes, the tests and the typecheck, and the task asked for nothing more"},
	}
	for _, step := range []int{1, 3, 4, 5, 6, 7, 8, 10, 11} {
		labels[stepKey("turn-18d69bfed2bf52a0", step)] = Label{Continue, "the turn was reading or writing the files the task asks for"}
	}
	for _, turn := range []string{
		"turn-18d690d537180c90",
		"turn-18d690dd45903d58",
		"turn-18d6915ed8f2eaec",
		"turn-18d69163a5ba8018",
		"turn-18d69a57fbdac8e8",
		"turn-18d69b4eeadf40e4",
	} {
		labels[turn+"#1"] = Label{Continue, "the file had been written but the turn had not reported back"}
		labels[turn+"#2"] = Label{Stop, "the turn reported the file was written and the task asked for nothing more"}
	}
	for step := 1; step <= 8; step++ {
		labels[stepKey("turn-18d6913a528f4528", step)] = Label{Continue, "reading the project the task is to be built on"}
	}
	for step := 1; step <= 31; step++ {
		labels[stepKey("turn-18d6a27c7dfb1644", step)] = Label{Continue, "the task lists seven changes to a project that already exists and at least one of them was still unmade"}
	}
	return labels
}

func Unlabelled() map[string]string {
	return map[string]string{
		"turn-18d6a27c7dfb1644#34": "git diff src/index.ts and git log after the checks passed at 32 and the diff was read at 33. reading a file the turn never wrote is either a last check worth a step or archaeology the task never asked for, and the recorded command and its result do not say which. left unlabelled rather than guessed",
	}
}
