package mutate

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	greedyChildEnv = "MUTATE_TEST_GREEDY_CHILD"
	treeParentEnv  = "MUTATE_TEST_TREE_PARENT"
	treeLeafEnv    = "MUTATE_TEST_TREE_LEAF"
)

func TestMain(m *testing.M) {
	switch {
	case os.Getenv(greedyChildEnv) == "1":
		greedyChild()
	case os.Getenv(treeLeafEnv) == "1":
		time.Sleep(time.Hour)
	case os.Getenv(treeParentEnv) == "1":
		treeParent()
	default:
		os.Exit(m.Run())
	}
}

func greedyChild() {
	const chunkBytes = 32 * 1024 * 1024
	const hardCapBytes = 8 * 1024 * 1024 * 1024
	var chunks [][]byte
	for len(chunks)*chunkBytes < hardCapBytes {
		chunk := make([]byte, chunkBytes)
		for i := range chunk {
			chunk[i] = 1
		}
		chunks = append(chunks, chunk)
	}
}

func treeParent() {
	for range 2 {
		leaf := exec.Command(os.Args[0])
		leaf.Env = append(os.Environ(), treeLeafEnv+"=1")
		if err := leaf.Start(); err != nil {
			os.Exit(1)
		}
		fmt.Println("child", leaf.Process.Pid)
	}
	fmt.Println("ready")
	time.Sleep(time.Hour)
}

func TestAChildThatCrossesTheMemoryCeilingIsKilled(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the memory ceiling is enforced by a windows job object")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(os.Environ(), greedyChildEnv+"=1")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := run(ctx, cmd, 64*1024*1024)
	if err == nil {
		t.Fatal("run returned no error for a child that should have crossed the memory ceiling")
	}
	if cmd.ProcessState == nil {
		t.Fatal("the child never finished")
	}
	if cmd.ProcessState.Success() {
		t.Fatal("the greedy child exited clean, want it killed for crossing the ceiling")
	}
}

func TestKillingTheRunKillsEveryProcessUnderIt(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the process tree kill is enforced by a windows job object")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(os.Environ(), treeParentEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}

	reported := make(chan []int, 1)
	go func() {
		var children []int
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "ready" {
				reported <- children
				return
			}
			if pid, ok := strings.CutPrefix(line, "child "); ok {
				if n, err := strconv.Atoi(pid); err == nil {
					children = append(children, n)
				}
			}
		}
		reported <- children
	}()

	done := make(chan error, 1)
	go func() {
		done <- run(ctx, cmd, 256*1024*1024)
	}()

	children := <-reported
	if len(children) != 2 {
		t.Fatalf("the parent reported %d children, want 2", len(children))
	}
	all := append([]int{cmd.Process.Pid}, children...)
	for _, pid := range all {
		if !processAlive(pid) {
			t.Fatalf("pid %d is not alive before the kill", pid)
		}
	}

	cancel()
	<-done

	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range all {
		for processAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if processAlive(pid) {
			t.Fatalf("pid %d is still alive after killing the run", pid)
		}
	}
}
