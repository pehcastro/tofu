package main

import (
	"context"
	"fmt"
	"io"
	"time"

	shipped "boji/catalog/models"
	"boji/internal/konst"
	"boji/internal/llm/cred"
	"boji/internal/llm/models"
	"boji/internal/transport"
)

const modelsUsage = "usage: boji models [--discover]"

func modelsVerb(args []string, out, errOut io.Writer) int {
	discover := false
	for _, arg := range args {
		if arg != "--discover" {
			_, _ = fmt.Fprintf(errOut, "boji models: unknown flag %q, %s\n", arg, modelsUsage)
			return exitUsage
		}
		discover = true
	}
	catalog, err := models.Load(shipped.Files())
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "boji models: %v\n", err)
		return exitUsage
	}
	if !discover {
		for _, line := range catalog.Lines() {
			_, _ = fmt.Fprintln(out, line)
		}
		return exitOK
	}
	return discoverModels(context.Background(), catalog, out)
}

func discoverModels(ctx context.Context, catalog models.Catalog, out io.Writer) int {
	store, client, err := discoveryStore()
	if err != nil {
		_, _ = fmt.Fprintf(out, "boji models: %v\n", err)
		return exitVerdict
	}
	defer func() { _ = store.Close() }()

	code := exitOK
	for _, provider := range models.Providers() {
		spec, err := cred.Lookup(string(provider))
		if err != nil {
			_, _ = fmt.Fprintf(out, "%s: %v\n", provider, err)
			code = exitVerdict
			continue
		}
		row, present, err := store.Row(cred.Provider(provider))
		if err != nil || !present {
			_, _ = fmt.Fprintf(out, "%s: no credential, run boji login %s\n", provider, provider)
			code = exitVerdict
			continue
		}
		served, err := models.Discover(ctx, client, models.Account{
			Provider:  provider,
			AccountID: row.Credential.Identity.AccountID,
			Token:     cred.NewManager(store, spec).Access,
		})
		if err != nil {
			_, _ = fmt.Fprintf(out, "%s: discovery failed under %s, so a short list is this pin rather than the account: %v\n",
				provider, served.Pin, err)
			code = exitVerdict
			continue
		}
		for _, line := range catalog.Reconcile(served).Lines() {
			_, _ = fmt.Fprintln(out, line)
		}
	}
	return code
}

func discoveryStore() (*cred.Store, *transport.Client, error) {
	path, err := cred.Path()
	if err != nil {
		return nil, nil, err
	}
	store, err := cred.Open(path)
	if err != nil {
		return nil, nil, err
	}
	client, err := transport.New(transport.Config{
		AttemptTimeout: time.Duration(konst.TurnAttemptTimeoutMillis) * time.Millisecond,
		Concurrency:    1,
	})
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	return store, client, nil
}

func selectModel(wire, id string) (models.Model, error) {
	provider := models.Provider(wire)
	if !provider.Valid() {
		return models.Model{}, fmt.Errorf(
			"the model catalog covers the subscriptions %s and %s, and --wire %s spends an api key, which is money rather than a window",
			models.Anthropic, models.Codex, wire)
	}
	catalog, err := models.Load(shipped.Files())
	if err != nil {
		return models.Model{}, err
	}
	if id == "" {
		return catalog.Default(provider)
	}
	return catalog.Select(provider, id)
}
