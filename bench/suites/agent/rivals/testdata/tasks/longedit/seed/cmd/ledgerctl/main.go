package main

import (
	"fmt"
	"os"
	"strings"

	"example.com/ledger/account"
	"example.com/ledger/report"
	"example.com/ledger/store"
)

func main() {
	s := store.New()
	for _, arg := range os.Args[1:] {
		id, owner, found := strings.Cut(arg, "=")
		if !found {
			fmt.Fprintf(os.Stderr, "ledgerctl: %q is not id=owner\n", arg)
			os.Exit(2)
		}
		s.Put(account.New(id, owner))
	}
	if first, ok := s.Get("main"); ok {
		fmt.Println("main account:", first.Owner)
	}
	fmt.Print(report.Totals(s))
}
