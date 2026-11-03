package main

import (
	"encoding/json"
	"fmt"
	"os"

	"example.com/atlas/classify"
	"example.com/atlas/dispatch"
	"example.com/atlas/intake"
	"example.com/atlas/route"
	"example.com/atlas/sink"
)

func main() {
	stage := dispatch.NewStage(route.Legacy(), sink.Shared())
	decoder := json.NewDecoder(os.Stdin)
	for decoder.More() {
		var o intake.Order
		if err := decoder.Decode(&o); err != nil {
			fmt.Fprintln(os.Stderr, "replay:", err)
			os.Exit(1)
		}
		o.Class = classify.FromHeader(os.Getenv("X_PRIORITY"))
		if err := stage.Run(&o); err != nil {
			fmt.Fprintln(os.Stderr, "replay:", err)
		}
	}
}
