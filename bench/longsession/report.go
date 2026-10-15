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
	fmt.Println("| arm | agents | workers | phase | minute | working MB | private MB | CPU s | cores | requests | worker steps |")
	fmt.Println("|---|---:|---:|---|---:|---:|---:|---:|---:|---:|---:|")
	var conditions []string
	for _, r := range rows {
		fmt.Printf("| %s | %d | %d | %s | %d | %.0f | %.0f | %.1f | %.2f | %d | %d |\n", r.Arm, r.Agents, r.Workers, r.Phase, r.Minute, r.WorkingMB, r.PrivateMB, r.CPUSeconds, r.Cores, r.Requests, r.WorkSteps)
		said := fmt.Sprintf("%s: tofu %s at %s, %s, %s, GOMAXPROCS %s, %s, %s", r.Arm, r.Version, r.Commit, r.Credential, r.Wire, r.GOMAXPROCS, r.Machine, r.Date[:10])
		if !slices.Contains(conditions, said) {
			conditions = append(conditions, said)
		}
	}
	fmt.Println()
	for _, said := range conditions {
		fmt.Println("- " + said)
	}
	return nil
}
