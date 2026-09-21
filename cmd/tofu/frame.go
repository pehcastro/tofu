package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/filmstrip"
	"tofu/interface/tui/fixture"
)

const frameUsage = "usage: tofu frame [NAME] [--width N] [--height N] [--plain] [--list]"

func frameVerb(args []string, out, errOut io.Writer) int {
	width, height, name, plain, list := fixture.Width, fixture.Height, filmstrip.Names()[0], false, false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--width":
			index++
			parsed, err := frameArgInt(args, index, errOut)
			if err != nil {
				return exitUsage
			}
			width = parsed
		case "--height":
			index++
			parsed, err := frameArgInt(args, index, errOut)
			if err != nil {
				return exitUsage
			}
			height = parsed
		case "--plain":
			plain = true
		case "--list":
			list = true
		default:
			if strings.HasPrefix(args[index], "-") {
				_, _ = fmt.Fprintf(errOut, "tofu frame: unknown flag %q\n\n%s\n", args[index], frameUsage)
				return exitUsage
			}
			name = args[index]
		}
	}
	if list {
		_, _ = fmt.Fprintln(out, strings.Join(filmstrip.Names(), "\n"))
		return exitOK
	}
	frame, found := filmstrip.Find(name, width, height)
	if !found {
		_, _ = fmt.Fprintf(errOut, "tofu frame: no frame named %q, run tofu frame --list\n", name)
		return exitUsage
	}
	text := frame.Content
	if plain {
		text = ansi.Strip(text)
	}
	_, _ = fmt.Fprintln(out, text)
	return exitOK
}

func frameArgInt(args []string, index int, errOut io.Writer) (int, error) {
	if index >= len(args) {
		_, _ = fmt.Fprintln(errOut, frameUsage)
		return 0, fmt.Errorf("tofu frame: missing value")
	}
	parsed, err := strconv.Atoi(args[index])
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "tofu frame: %q is not a number\n\n%s\n", args[index], frameUsage)
		return 0, err
	}
	return parsed, nil
}
