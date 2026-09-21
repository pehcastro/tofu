package main

import (
	"fmt"

	"tofu/internal/llm/cred"
)

func send(key cred.LLMKey) string { return key.Prompt() }

func main() {
	key, err := cred.NewJevKey(cred.TypeSafe, "a-secret-that-is-not-real")
	if err != nil {
		panic(err)
	}
	fmt.Println(send(key))
}
