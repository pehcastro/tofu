package report

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func Generate(noun string, render func(machine, date string) (string, error)) error {
	date := time.Now().Format("2006-01-02")
	body, err := render(hostname(), date)
	if err != nil {
		return err
	}
	path := filepath.Join("..", fmt.Sprintf("report-%s.md", date))
	if err := Write(path, []byte(body), 0o644, noun); err != nil {
		return err
	}
	fmt.Println(path, "written")
	return nil
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}
