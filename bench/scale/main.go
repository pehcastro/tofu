package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	sampleEvery = 250 * time.Millisecond
	mebibyte    = 1 << 20
	pageBytes   = 4096
)

type row struct {
	Tool          string
	Version       string
	Arm           string
	Agents        int
	Answered      int
	Model         string
	Credential    string
	Wire          string
	Machine       string
	Date          string
	Exit          int
	WallS         float64
	CPUS          float64
	PeakRSSMB     float64
	SteadyRSSMB   float64
	PeakCommitMB  float64
	PeakProcesses int
	Samples       int
	ExpectMB      float64
	ExpectCPUS    float64
}

func machine() string {
	return fmt.Sprintf("%s/%s %d logical CPUs", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
}

func measure(cmd *exec.Cmd, into *row) error {
	start := time.Now()
	tree, err := startTree(cmd)
	if err != nil {
		return err
	}
	defer tree.close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(sampleEvery)
	defer ticker.Stop()
	var samples []uint64
	for waiting := true; waiting; {
		rss, members := tree.sample()
		if members > 0 {
			samples = append(samples, rss)
		}
		into.PeakProcesses = max(into.PeakProcesses, members)
		select {
		case <-done:
			waiting = false
		case <-ticker.C:
		}
	}
	into.WallS = time.Since(start).Seconds()
	cpu, commit := tree.totals(cmd.ProcessState)
	into.CPUS, into.PeakCommitMB = cpu.Seconds(), float64(commit)/mebibyte
	into.Exit, into.Samples, into.Machine, into.Date = cmd.ProcessState.ExitCode(), len(samples), machine(), time.Now().Format(time.DateOnly)
	if len(samples) == 0 {
		return errors.New("the tree ended before its first sample")
	}
	into.PeakRSSMB = float64(slices.Max(samples)) / mebibyte
	slices.Sort(samples)
	into.SteadyRSSMB = float64(samples[len(samples)/2]) / mebibyte
	return nil
}

func appendRow(path string, measured row) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	line, _ := json.Marshal(measured)
	_, err = file.Write(append(line, '\n'))
	return errors.Join(err, file.Close())
}

func firstLines(project string, agents int) []string {
	var lines []string
	for k := min(agents, 1); k <= agents; k++ {
		raw, _ := os.ReadFile(filepath.Join(project, fmt.Sprintf("f%d.txt", k)))
		lines = append(lines, strings.SplitN(string(raw), "\n", 2)[0])
	}
	return lines
}

func sample(args []string) error {
	flags := flag.NewFlagSet("sample", flag.ExitOnError)
	var measured row
	flags.StringVar(&measured.Tool, "tool", "", "the tool measured")
	flags.StringVar(&measured.Version, "version", "", "the tool's version and commit")
	flags.StringVar(&measured.Arm, "arm", "live", "live or cassette")
	flags.IntVar(&measured.Agents, "agents", 0, "sub-agents asked for")
	flags.StringVar(&measured.Model, "model", "", "the model asked for, kept when -model-from is not given")
	modelFrom := flags.String("model-from", "", "a pattern whose first group is a model build the transcript reports")
	flags.StringVar(&measured.Credential, "credential", "subscription", "subscription or none")
	flags.StringVar(&measured.Wire, "wire", "", "the wire the tool spoke")
	project := flags.String("project", "", "the scratch project holding f0.txt to f10.txt")
	out := flags.String("out", "", "the results file a row is appended to")
	transcript := flags.String("transcript", "", "where the tool's standard output is kept")
	_ = flags.Parse(args)
	command := flags.Args()
	if len(command) == 0 || *out == "" || *transcript == "" {
		return errors.New("sample needs -out, -transcript and a command after --")
	}
	output, err := os.Create(*transcript)
	if err != nil {
		return err
	}
	defer func() { _ = output.Close() }()
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = *project, output, output
	if err := measure(cmd, &measured); err != nil {
		return err
	}
	said, _ := os.ReadFile(*transcript)
	if *modelFrom != "" {
		var reported []string
		for _, match := range regexp.MustCompile(*modelFrom).FindAllStringSubmatch(string(said), -1) {
			if !slices.Contains(reported, match[1]) {
				reported = append(reported, match[1])
			}
		}
		measured.Model = strings.Join(reported, "+")
	}
	for _, line := range firstLines(*project, measured.Agents) {
		if strings.Contains(string(said), line) {
			measured.Answered++
		}
	}
	fmt.Printf("%s %s agents=%d answered=%d exit=%d wall=%.1fs cpu=%.2fs peak=%.0fMB steady=%.0fMB commit=%.0fMB processes=%d\n",
		measured.Tool, measured.Arm, measured.Agents, measured.Answered, measured.Exit, measured.WallS, measured.CPUS, measured.PeakRSSMB, measured.SteadyRSSMB, measured.PeakCommitMB, measured.PeakProcesses)
	return appendRow(*out, measured)
}

func fixture(args []string) error {
	if len(args) != 1 {
		return errors.New("fixture needs the project directory")
	}
	if err := os.MkdirAll(args[0], 0o755); err != nil {
		return err
	}
	seed := time.Now().UnixNano()
	for k := range 11 {
		body := fmt.Sprintf("marker-%d-%x\nthe second line is not the answer\n", k, seed+int64(k)*7919)
		if err := os.WriteFile(filepath.Join(args[0], fmt.Sprintf("f%d.txt", k)), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

type cassetteTool struct {
	Name string            `json:"name"`
	Args map[string]string `json:"args"`
}

type cassetteReply struct {
	Agent string         `json:"agent,omitempty"`
	Text  string         `json:"text"`
	Tools []cassetteTool `json:"tools,omitempty"`
}

func cassette(args []string) error {
	if len(args) != 3 {
		return errors.New("cassette needs the project, the agents and the cassette path")
	}
	agents, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	answers := firstLines(args[0], agents)
	all := strings.Join(answers, "\n")
	replies := []cassetteReply{{Text: "reading it", Tools: []cassetteTool{{Name: "read", Args: map[string]string{"path": "f0.txt"}}}}, {Text: all}}
	if agents > 0 {
		replies = []cassetteReply{{Text: "spawning"}}
		for k := 1; k <= agents; k++ {
			file := fmt.Sprintf("f%d.txt", k)
			replies[0].Tools = append(replies[0].Tools, cassetteTool{Name: "spawn", Args: map[string]string{"task": "read " + file + " and answer with its first line exactly"}})
			caller := fmt.Sprintf("c%d", k)
			replies = append(replies,
				cassetteReply{Agent: caller, Text: "reading it", Tools: []cassetteTool{{Name: "read", Args: map[string]string{"path": file}}}},
				cassetteReply{Agent: caller, Text: answers[k-1]})
		}
		for range agents + 1 {
			replies = append(replies, cassetteReply{Text: all})
		}
	}
	var deck strings.Builder
	for _, reply := range replies {
		line, _ := json.Marshal(reply)
		deck.Write(line)
		deck.WriteByte('\n')
	}
	return os.WriteFile(args[2], []byte(deck.String()), 0o644)
}

func alloc(args []string) error {
	flags := flag.NewFlagSet("alloc", flag.ExitOnError)
	size := flags.Int("mb", 0, "MiB to allocate and touch")
	spin := flags.Duration("cpu", 0, "how long to spin one core")
	hold := flags.Duration("hold", 2*time.Second, "how long to hold the memory afterwards")
	kids := flags.Int("kids", 0, "children started with the same size and spin, none of their own")
	_ = flags.Parse(args)
	held := make([]byte, *size*mebibyte)
	for at := 0; at < len(held); at += pageBytes {
		held[at] = 1
	}
	var children []*exec.Cmd
	for range *kids {
		child := exec.Command(os.Args[0], "alloc", "-mb", strconv.Itoa(*size), "-cpu", spin.String(), "-hold", hold.String())
		if err := child.Start(); err != nil {
			return err
		}
		children = append(children, child)
	}
	for begun := time.Now(); time.Since(begun) < *spin; {
	}
	time.Sleep(*hold)
	for _, child := range children {
		if err := child.Wait(); err != nil {
			return err
		}
	}
	runtime.KeepAlive(held)
	return nil
}

func check(args []string) error {
	if len(args) != 1 {
		return errors.New("check needs the results file")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	for _, known := range []struct {
		size, kids int
		spin       time.Duration
	}{{0, 0, 0}, {256, 0, time.Second}, {0, 2, 0}, {128, 2, 500 * time.Millisecond}} {
		processes := float64(1 + known.kids)
		measured := row{Tool: "known child", Arm: "known", Agents: known.kids, Credential: "none", Model: "none", Wire: "none", ExpectMB: processes * float64(known.size), ExpectCPUS: processes * known.spin.Seconds()}
		cmd := exec.Command(self, "alloc", "-mb", strconv.Itoa(known.size), "-cpu", known.spin.String(), "-kids", strconv.Itoa(known.kids))
		if err := measure(cmd, &measured); err != nil {
			return err
		}
		fmt.Printf("known child mb=%d kids=%d spin=%s: peak=%.1fMB steady=%.1fMB commit=%.1fMB cpu=%.2fs processes=%d exit=%d\n",
			known.size, known.kids, known.spin, measured.PeakRSSMB, measured.SteadyRSSMB, measured.PeakCommitMB, measured.CPUS, measured.PeakProcesses, measured.Exit)
		if err := appendRow(args[0], measured); err != nil {
			return err
		}
	}
	return nil
}

func readRows(path string) ([]row, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var rows []row
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var read row
		if err := json.Unmarshal(scanner.Bytes(), &read); err != nil {
			return nil, err
		}
		rows = append(rows, read)
	}
	return rows, scanner.Err()
}

func main() {
	verbs := map[string]func([]string) error{"sample": sample, "fixture": fixture, "cassette": cassette, "alloc": alloc, "check": check, "report": report, "wall": wall, "retrace": retrace, "wallreport": wallreport}
	if len(os.Args) < 2 || verbs[os.Args[1]] == nil {
		fmt.Fprintln(os.Stderr, "usage: scale sample|fixture|cassette|alloc|check|report|wall|retrace|wallreport")
		os.Exit(2)
	}
	if err := verbs[os.Args[1]](os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "bench/scale:", err)
		os.Exit(1)
	}
}
