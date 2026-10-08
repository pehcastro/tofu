package main

import (
	"cmp"
	"encoding/json"
	"strconv"

	"tofu/interface/cli"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/session"
	"tofu/internal/widget"
)

type (
	traceCacheBreak = host.TraceCacheBreak
	traceCache      = host.TraceCache
)

type cacheUse struct {
	Read        int `json:"cache_read_tokens"`
	Write       int `json:"cache_write_tokens"`
	FiveMinutes int `json:"cache_write_5m_tokens"`
	OneHour     int `json:"cache_write_1h_tokens"`
}

func (u cacheUse) words() string {
	read := "read " + strconv.Itoa(u.Read) + " · wrote "
	fiveMinutes, oneHour := strconv.Itoa(u.FiveMinutes)+" at 5m", strconv.Itoa(u.OneHour)+" at 1h"
	switch {
	case u.FiveMinutes > 0 && u.OneHour > 0:
		return read + fiveMinutes + " and " + oneHour
	case u.FiveMinutes > 0:
		return read + fiveMinutes
	case u.OneHour > 0:
		return read + oneHour
	case u.Write > 0:
		return read + strconv.Itoa(u.Write) + ", lifetime not recorded"
	}
	return read + "nothing"
}

func cacheTrace(events []session.Event, exchanges []session.Exchange) traceCache {
	used := map[string]cacheUse{}
	for _, event := range events {
		var use cacheUse
		if event.Kind == session.EventRequest && json.Unmarshal(event.Body, &use) == nil {
			used[event.ID] = use
		}
	}
	trace := traceCache{Lifetimes: map[string]string{}}
	last := map[string]session.Exchange{}
	for _, exchange := range exchanges {
		use, known := used[exchange.Request]
		if !known || exchange.Error != "" {
			continue
		}
		if use.Read+use.Write > 0 {
			trace.Lifetimes[exchange.Request] = use.words()
		}
		before, seen := last[exchange.Agent]
		last[exchange.Agent] = exchange
		prior := used[before.Request]
		cached := prior.Read + prior.Write
		if !seen || cached == 0 || use.Read+konst.CacheBreakSlackTokens >= cached {
			continue
		}
		trace.Breaks = append(trace.Breaks, traceCacheBreak{Agent: exchange.Agent, Request: exchange.Request, After: before.Request,
			Gap: widget.Until(exchange.At.Sub(before.At)), Read: use.Read, Cached: cached, Differs: firstDifference(before, exchange)})
	}
	return trace
}

func cacheBreakRows(breaks []traceCacheBreak) []cli.Row {
	rows := make([]cli.Row, len(breaks))
	for i, cut := range breaks {
		rows[i] = cli.Row{Mark: cli.Warn, Cells: []string{cmp.Or(cut.Agent, session.AuthorOrchestrator), cut.Request,
			"read " + strconv.Itoa(cut.Read) + " of " + strconv.Itoa(cut.Cached) + " cached", cut.Gap + " after " + cut.After}, Detail: "first difference: " + cut.Differs}
	}
	return rows
}

func firstDifference(before, after session.Exchange) string {
	if before.Tools != after.Tools {
		return "the tools"
	}
	for index, hash := range before.Messages {
		if index >= len(after.Messages) || after.Messages[index] != hash {
			return "message " + strconv.Itoa(index) + " of " + strconv.Itoa(len(after.Messages))
		}
	}
	return "nothing sent changed"
}
