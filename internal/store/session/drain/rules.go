package drain

import (
	"strconv"
	"strings"
	"time"
)

const (
	pressureShare     = 0.75
	pressureWarnShare = 0.9
	jumpShare         = 0.25
	refillShare       = 0.75
	concentrateShare  = 0.5
	concentrateTurns  = 3
	unreadShare       = 0.05
	idleGap           = 5 * time.Minute
	idleWarnCount     = 2
	failStreakMin     = 3
	failStreakWarn    = 4
	redundantReadMin  = 4
	redundantReadWarn = 8
	oneFileReads      = 4
	redundantFrom     = 3
	percent           = 100
)

type Rule string

const (
	RulePressure    Rule = "context-pressure"
	RuleJump        Rule = "context-jump"
	RuleIdleRewrite Rule = "idle-cache-rewrite"
	RuleRefill      Rule = "post-compaction-refill"
	RuleThrash      Rule = "edit-test-thrash"
	RuleRereads     Rule = "repeated-file-reads"
	RuleConcentrate Rule = "turn-cost-concentration"
	RuleUnread      Rule = "cache-writes-never-read"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
)

type Finding struct {
	Rule     Rule     `json:"rule"`
	Severity Severity `json:"severity"`
	Title    string   `json:"title"`
	Evidence string   `json:"evidence"`
	Agent    string   `json:"agent,omitempty"`
	Turn     string   `json:"turn"`
	At       string   `json:"at,omitempty"`
}

type where struct {
	agent, turn, call string
}

type measured struct {
	where
	value, from int
}

type signals struct {
	peak, jump, refill    measured
	refillShare           float64
	idleCount, idleTokens int
	idleFirst             where
	streak                measured
	redundant             int
	fileReads             map[string]int
	topFile               string
	topAt                 where
	unreadTop             float64
	unreadAt              where
}

func (s *signals) sized(th *thread, context int, at where) {
	if context > s.peak.value {
		s.peak = measured{where: at, value: context}
	}
	if grew := context - th.context; th.context > 0 && grew > s.jump.value-s.jump.from {
		s.jump = measured{where: at, value: context, from: th.context}
	}
	if th.refill > 0 {
		if share := float64(context) / float64(th.refill); share > s.refillShare {
			s.refill, s.refillShare = measured{where: at, value: context, from: th.refill}, share
		}
		th.refill = 0
	}
}

func (s *signals) idleRewrite(gap time.Duration, written int, at where) {
	if gap < idleGap || written <= 0 {
		return
	}
	if s.idleCount == 0 {
		s.idleFirst = at
	}
	s.idleCount, s.idleTokens = s.idleCount+1, s.idleTokens+written
}

func (s *signals) tested(th *thread, failed bool, at where) {
	if !failed {
		th.failing = 0
		return
	}
	th.failing++
	if th.failing > s.streak.value {
		s.streak = measured{where: at, value: th.failing}
	}
}

func (s *signals) reread(th *thread, path string, at where) {
	th.reads[path]++
	if th.reads[path] >= redundantFrom {
		s.redundant++
	}
	if s.fileReads == nil {
		s.fileReads = map[string]int{}
	}
	s.fileReads[path]++
	if s.fileReads[path] > s.fileReads[s.topFile] || path == s.topFile {
		s.topFile, s.topAt = path, at
	}
}

func (s *signals) unread(weighted float64, at where) {
	if weighted > s.unreadTop {
		s.unreadTop, s.unreadAt = weighted, at
	}
}

func share(part, whole float64) string {
	return strconv.FormatFloat(part/whole*percent, 'f', 0, 64) + "%"
}

func found(rule Rule, severity Severity, title, evidence string, at where) Finding {
	return Finding{Rule: rule, Severity: severity, Title: title, Evidence: evidence, Agent: at.agent, Turn: at.turn, At: at.call}
}

func warnAt(warn bool) Severity {
	if warn {
		return SeverityWarning
	}
	return SeverityInfo
}

func (s *signals) findings(report Report, turns map[string]float64) []Finding {
	findings := []Finding{}
	ceiling := float64(report.Ceiling)
	if peak := float64(s.peak.value); peak >= ceiling*pressureShare {
		findings = append(findings, found(RulePressure, warnAt(peak >= ceiling*pressureWarnShare), "context window is under pressure",
			"one call peaked at "+strconv.Itoa(s.peak.value)+" tokens, "+share(peak, ceiling)+" of the "+strconv.Itoa(report.Ceiling)+" ceiling", s.peak.where))
	}
	if grew := float64(s.jump.value - s.jump.from); s.jump.from > 0 && grew >= ceiling*jumpShare {
		findings = append(findings, found(RuleJump, SeverityWarning, "one call added a large block of context",
			"grew from "+strconv.Itoa(s.jump.from)+" to "+strconv.Itoa(s.jump.value)+" tokens, "+share(grew, ceiling)+" of the ceiling", s.jump.where))
	}
	if s.idleCount > 0 {
		findings = append(findings, found(RuleIdleRewrite, warnAt(s.idleCount >= idleWarnCount), "idle gaps were followed by cache rewrites",
			strconv.Itoa(s.idleCount)+" lead calls after a gap of 5m or more wrote "+strconv.Itoa(s.idleTokens)+" cache tokens; the first is named", s.idleFirst))
	}
	if s.refillShare >= refillShare {
		findings = append(findings, found(RuleRefill, SeverityWarning, "compaction recovered little headroom",
			"the first call after compaction refilled to "+strconv.Itoa(s.refill.value)+" tokens, "+share(float64(s.refill.value), float64(s.refill.from))+" of the "+strconv.Itoa(s.refill.from)+" before it", s.refill.where))
	}
	if s.streak.value >= failStreakMin {
		findings = append(findings, found(RuleThrash, warnAt(s.streak.value >= failStreakWarn), "edit-test loop repeated without progress",
			strconv.Itoa(s.streak.value)+" consecutive failing test runs without a pass; the last is named", s.streak.where))
	}
	if top := s.fileReads[s.topFile]; s.redundant >= redundantReadMin || top >= oneFileReads {
		findings = append(findings, found(RuleRereads, warnAt(s.redundant >= redundantReadWarn), "the same files are being re-read",
			strconv.Itoa(s.redundant)+" redundant reads (3rd or later read of a file by one agent); the most read is "+s.topFile+", "+strconv.Itoa(top)+" reads, last named", s.topAt))
	}
	var costliest string
	for id, weighted := range turns {
		if costliest == "" || weighted > turns[costliest] || weighted == turns[costliest] && id < costliest {
			costliest = id
		}
	}
	if len(turns) >= concentrateTurns && report.Total.Weighted > 0 && turns[costliest] >= report.Total.Weighted*concentrateShare {
		findings = append(findings, found(RuleConcentrate, SeverityInfo, "most spend landed in one turn",
			"the turn took "+share(turns[costliest], report.Total.Weighted)+" of the session's weighted tokens, sub-agents included", where{turn: costliest}))
	}
	if report.Unread >= report.Total.Weighted*unreadShare && report.Unread > 0 {
		findings = append(findings, found(RuleUnread, SeverityWarning, "cache writes were never read back",
			strconv.FormatFloat(report.Unread, 'f', 0, 64)+" weighted tokens of cache writes, "+share(report.Unread, report.Total.Weighted)+" of the session, were not read by the next call; the largest is named", s.unreadAt))
	}
	return findings
}

func runsTests(command string) bool {
	for segment := range strings.FieldsFuncSeq(command, func(r rune) bool { return strings.ContainsRune("&|;\n", r) }) {
		words := strings.Fields(segment)
		for len(words) > 0 && (strings.Contains(words[0], "=") || words[0] == "rtk" || words[0] == "proxy" || words[0] == "env") {
			words = words[1:]
		}
		if len(words) == 0 {
			continue
		}
		switch words[0] {
		case "pytest", "jest", "vitest", "rspec", "phpunit", "ctest":
			return true
		case "go", "cargo", "mvn", "make", "gradle", "./gradlew", "gradlew", "bun", "npm", "pnpm", "yarn", "deno":
			rest := words[1:]
			if len(rest) > 1 && rest[0] == "run" {
				rest = rest[1:]
			}
			if len(rest) > 0 && rest[0] == "test" {
				return true
			}
		}
	}
	return false
}
