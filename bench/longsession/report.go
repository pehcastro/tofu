package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
)

func report(args []string) error {
	if len(args) != 1 {
		return errors.New("report needs the rows jsonl")
	}
	file, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	var rows []row
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		var r row
		if err := json.Unmarshal(lines.Bytes(), &r); err != nil {
			return err
		}
		rows = append(rows, r)
	}
	if err := lines.Err(); err != nil || len(rows) == 0 {
		return errors.Join(err, errors.New("no rows"))
	}
	fmt.Println("| arm | agents | workers | phase | minute | working MB | private MB | CPU s | cores | host pid | host private MB | host CPU s | requests | worker steps |")
	fmt.Println("|---|---:|---:|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	var conditions []string
	var phases [][]row
	for _, r := range rows {
		host := "| - | - | -"
		if r.HostPID != 0 {
			host = fmt.Sprintf("| %d | %.0f | %.1f", r.HostPID, r.HostPrivateMB, r.HostCPU)
		}
		fmt.Printf("| %s | %d | %d | %s | %d | %.0f | %.0f | %.1f | %.2f %s | %d | %d |\n", r.Arm, r.Agents, r.Workers, r.Phase, r.Minute, r.WorkingMB, r.PrivateMB, r.CPUSeconds, r.Cores, host, r.Requests, r.WorkSteps)
		at := slices.IndexFunc(phases, func(p []row) bool { return p[0].Arm == r.Arm && p[0].Date == r.Date && p[0].Phase == r.Phase })
		if at < 0 {
			at, phases = len(phases), append(phases, nil)
		}
		phases[at] = append(phases[at], r)
		said := fmt.Sprintf("%s: tofu %s at %s, %s, %s, GOMAXPROCS %s, %s, %s", r.Arm, r.Version, r.Commit, r.Credential, r.Wire, r.GOMAXPROCS, r.Machine, r.Date[:10])
		if !slices.Contains(conditions, said) {
			conditions = append(conditions, said)
		}
	}
	fmt.Println()
	fmt.Println("| arm | phase | minutes | private MB per minute | host private MB per minute | host pid |")
	fmt.Println("|---|---|---:|---:|---:|---|")
	for _, phase := range phases {
		first, last := phase[0], phase[len(phase)-1]
		spanned := float64(last.Minute - first.Minute)
		app, host := "-", "-"
		if spanned > 0 {
			app = fmt.Sprintf("%+.1f", (last.PrivateMB-first.PrivateMB)/spanned)
		}
		if spanned > 0 && first.HostPID != 0 && first.HostPID == last.HostPID {
			host = fmt.Sprintf("%+.1f", (last.HostPrivateMB-first.HostPrivateMB)/spanned)
		}
		fmt.Printf("| %s | %s | %d to %d | %s | %s | %d |\n", first.Arm, first.Phase, first.Minute, last.Minute, app, host, last.HostPID)
	}
	fmt.Println()
	for _, said := range conditions {
		fmt.Println("- " + said)
	}
	return nil
}
