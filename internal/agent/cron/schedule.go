package cron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
)

type Kind string

const (
	KindCron      Kind = "cron"
	KindEvery     Kind = "every"
	KindOnce      Kind = "once"
	KindAfterTurn Kind = "after each turn"
)

const (
	everyPrefix = "every "
	oncePrefix  = "once at "
	dayLayout   = "2006-01-02 15:04"
	clockLayout = "15:04"
	daySuffix   = "d"
)

const scheduleForms = "a schedule is a 5-field cron line (minute hour day month weekday), @hourly, @daily, @weekly, @monthly, every <duration>, once at <15:04 or 2006-01-02 15:04>, or after each turn"

type field uint64

func (f field) has(value int) bool { return f&(1<<value) != 0 }

type Schedule struct {
	Kind       Kind
	Text       string
	every      time.Duration
	at         time.Time
	minutes    field
	hours      field
	days       field
	months     field
	weekdays   field
	anyDay     bool
	anyWeekday bool
}

func parseDuration(text string) (time.Duration, error) {
	if days, isDays := strings.CutSuffix(text, daySuffix); isDays {
		count, err := strconv.Atoi(days)
		if err != nil || count < 1 {
			return 0, fmt.Errorf("%q is not a whole number of days", text)
		}
		return time.Duration(count) * 24 * time.Hour, nil
	}
	return time.ParseDuration(text)
}

func Parse(text string, now time.Time) (Schedule, error) {
	text = strings.TrimSpace(text)
	if text == string(KindAfterTurn) {
		return Schedule{Kind: KindAfterTurn, Text: text}, nil
	}
	if interval, isEvery := strings.CutPrefix(text, everyPrefix); isEvery {
		every, err := parseDuration(strings.TrimSpace(interval))
		if err != nil {
			return Schedule{}, err
		}
		if every < konst.CronMinIntervalSeconds*time.Second {
			return Schedule{}, fmt.Errorf("every %s is under the %ds minimum", interval, konst.CronMinIntervalSeconds)
		}
		return Schedule{Kind: KindEvery, Text: text, every: every}, nil
	}
	if when, isOnce := strings.CutPrefix(text, oncePrefix); isOnce {
		at, err := parseAt(strings.TrimSpace(when), now)
		if err != nil {
			return Schedule{}, err
		}
		return Schedule{Kind: KindOnce, Text: oncePrefix + at.Format(dayLayout), at: at}, nil
	}
	return parseLine(text, now)
}

func parseAt(when string, now time.Time) (time.Time, error) {
	if at, err := time.ParseInLocation(dayLayout, when, now.Location()); err == nil {
		return at, nil
	}
	clock, err := time.ParseInLocation(clockLayout, when, now.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("once at %q: %s", when, scheduleForms)
	}
	at := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	if !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}
	return at, nil
}

func parseLine(text string, now time.Time) (Schedule, error) {
	macros := map[string]string{"@hourly": "0 * * * *", "@daily": "0 0 * * *", "@midnight": "0 0 * * *", "@weekly": "0 0 * * 0", "@monthly": "0 0 1 * *"}
	line := text
	if expanded, isMacro := macros[text]; isMacro {
		line = expanded
	}
	parts := strings.Fields(line)
	if len(parts) != 5 {
		return Schedule{}, fmt.Errorf("%q: %s", text, scheduleForms)
	}
	s := Schedule{Kind: KindCron, Text: text, anyDay: strings.HasPrefix(parts[2], "*"), anyWeekday: strings.HasPrefix(parts[4], "*")}
	var errs [5]error
	s.minutes, errs[0] = parseField(parts[0], 0, 59)
	s.hours, errs[1] = parseField(parts[1], 0, 23)
	s.days, errs[2] = parseField(parts[2], 1, 31)
	s.months, errs[3] = parseField(parts[3], 1, 12)
	s.weekdays, errs[4] = parseField(parts[4], 0, 7)
	if err := errors.Join(errs[:]...); err != nil {
		return Schedule{}, fmt.Errorf("%q: %w", text, err)
	}
	if s.weekdays.has(7) {
		s.weekdays |= 1
	}
	if s.Next(now).IsZero() {
		return Schedule{}, fmt.Errorf("%q matches no date in the next %d days, so it never fires", text, konst.CronSearchDays)
	}
	return s, nil
}

func parseField(text string, low, high int) (field, error) {
	var set field
	for _, part := range strings.Split(text, ",") {
		span, stepText, stepped := strings.Cut(part, "/")
		step, from, to := 1, low, high
		var err error
		if stepped {
			if step, err = strconv.Atoi(stepText); err != nil || step < 1 {
				return 0, fmt.Errorf("%q steps by %q, which is not a whole number above 0", part, stepText)
			}
		}
		if span != "*" {
			first, last, ranged := strings.Cut(span, "-")
			from, err = strconv.Atoi(first)
			to = from
			switch {
			case ranged && err == nil:
				to, err = strconv.Atoi(last)
			case stepped:
				to = high
			}
			if err != nil {
				return 0, fmt.Errorf("%q is not a number or a range", part)
			}
		}
		if from < low || to > high || from > to {
			return 0, fmt.Errorf("%q is outside %d-%d", part, low, high)
		}
		for value := from; value <= to; value += step {
			set |= 1 << value
		}
	}
	return set, nil
}

func (s Schedule) dayMatches(at time.Time) bool {
	day, weekday := s.days.has(at.Day()), s.weekdays.has(int(at.Weekday()))
	if !s.anyDay && !s.anyWeekday {
		return day || weekday
	}
	return day && weekday
}

func (s Schedule) Next(after time.Time) time.Time {
	switch s.Kind {
	case KindAfterTurn:
		return time.Time{}
	case KindEvery:
		return after.Add(s.every)
	case KindOnce:
		if s.at.After(after) {
			return s.at
		}
		return time.Time{}
	case KindCron:
	default:
		panic("cron: unknown schedule kind " + string(s.Kind))
	}
	at := after.Truncate(time.Minute).Add(time.Minute)
	for end := after.AddDate(0, 0, konst.CronSearchDays); at.Before(end); {
		switch {
		case !s.months.has(int(at.Month())) || !s.dayMatches(at):
			at = time.Date(at.Year(), at.Month(), at.Day()+1, 0, 0, 0, 0, at.Location())
		case !s.hours.has(at.Hour()):
			at = time.Date(at.Year(), at.Month(), at.Day(), at.Hour()+1, 0, 0, 0, at.Location())
		case !s.minutes.has(at.Minute()):
			at = at.Add(time.Minute)
		default:
			return at
		}
	}
	return time.Time{}
}
