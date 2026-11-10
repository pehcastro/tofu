package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	intervalAlpha = 0.2
	bandMass      = 0.95
	pred25        = 1.25
	fractionSteps = 300
	bisectSteps   = 100
)

var phases = append([]string{"model"}, toolKinds...)

func (r row) actualS() float64 {
	return float64(r.WallMS) / 1000
}

func (r row) logError(guess float64) float64 {
	return math.Log(r.actualS() / guess)
}

func (r row) insideP80() bool {
	return r.Prediction.P80LowS <= r.actualS() && r.actualS() <= r.Prediction.P80HighS
}

func (r row) intervalScore() float64 {
	low, high, y := math.Log(r.Prediction.P80LowS), math.Log(r.Prediction.P80HighS), math.Log(r.actualS())
	return high - low + 2/intervalAlpha*max(low-y, 0) + 2/intervalAlpha*max(y-high, 0)
}

func (r row) phaseMS(phase string) int64 {
	if phase == "model" {
		return r.ModelMS
	}
	return r.Calls[slices.Index(toolKinds, phase)].MS
}

func writeReport(args []string) error {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	batch := flags.String("batch", "", "folder under runs that holds the rows")
	if err := flags.Parse(args); err != nil {
		return err
	}
	rows, err := loadRows(*batch)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.Credential != rows[0].Credential {
			return fmt.Errorf("%s ran on %q and %s on %q; a batch compares one credential kind", rows[0].name(), rows[0].Credential, r.name(), r.Credential)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# tempo, batch %s\n\nTimes in seconds. Actual is the wall time of the claude process, from start to exit; active is the transcript's line span with gaps capped at %s. Log error is ln(actual / P50): 0 is exact, +0.69 took twice as long. No call was discarded as a warm-up.\n\n", *batch, idleCap)
	b.WriteString("| task | rep | P50 | P80 range | self guess | actual | active | log error | guess log error | inside P80 | stalls | check |\n|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		p := r.Prediction
		fmt.Fprintf(&b, "| %s | %d | %.1f | %.1f to %.1f | %.1f | %.1f | %.1f | %+.2f | %+.2f | %s | %d | %s |\n", r.Task, r.Rep, p.P50S, p.P80LowS, p.P80HighS, p.SelfGuessS,
			r.actualS(), float64(r.ActiveMS)/1000, r.logError(p.P50S), r.logError(p.SelfGuessS), yesNo(r.insideP80(), "yes", "no"), r.TestStalls, yesNo(r.Check.Passed, "pass", "fail"))
	}
	var errs, guessErrs, signed, scores, stalledWalls, cleanWalls []float64
	inside, passed := 0, 0
	for _, r := range rows {
		errs = append(errs, math.Abs(r.logError(r.Prediction.P50S)))
		guessErrs = append(guessErrs, math.Abs(r.logError(r.Prediction.SelfGuessS)))
		signed = append(signed, r.logError(r.Prediction.P50S))
		scores = append(scores, r.intervalScore())
		inside += boolCount(r.insideP80())
		passed += boolCount(r.Check.Passed)
		if r.TestStalls > 0 {
			stalledWalls = append(stalledWalls, r.actualS())
		} else {
			cleanWalls = append(cleanWalls, r.actualS())
		}
	}
	n := len(rows)
	low, high := jeffreysBand(inside, n)
	within := 0
	for _, e := range errs {
		within += boolCount(e <= math.Log(pred25))
	}
	fmt.Fprintf(&b, "\n## Batch\n\n| figure | value |\n|---|---|\n| runs | %d |\n| MdALE, median abs log error | %.2f (factor %.2f) |\n| PRED25, share with abs log error within ln 1.25 | %.2f |\n", n, median(errs), math.Exp(median(errs)), float64(within)/float64(n))
	fmt.Fprintf(&b, "| P80 coverage | %d of %d = %.2f, Jeffreys %.0f%% band %.2f to %.2f |\n| mean interval score, log scale, alpha %.1f | %.2f |\n", inside, n, float64(inside)/float64(n), bandMass*100, low, high, intervalAlpha, mean(scores))
	fmt.Fprintf(&b, "| self-guess MdALE, the baseline | %.2f (factor %.2f) |\n| mean signed log error, negative predicted too long | %+.2f |\n| checks passed | %d of %d |\n", median(guessErrs), math.Exp(median(guessErrs)), mean(signed), passed, n)
	fmt.Fprintf(&b, "| median wall, stalled against clean, a stall is a bun test call answered %s or more after it was made | %s over %d stalled, %s over %d clean |\n",
		stallFrom, medianOrNone(stalledWalls), len(stalledWalls), medianOrNone(cleanWalls), len(cleanWalls))
	b.WriteString("\n## Phase share, predicted against measured\n\nMeasured share is the phase's milliseconds over the sum of all phases: model time per request plus tool time per kind.\n\n| task | rep |")
	for _, phase := range phases {
		b.WriteString(" " + phase + " |")
	}
	b.WriteString("\n|---|---|" + strings.Repeat("---|", len(phases)) + "\n")
	for _, r := range rows {
		var total int64
		for _, phase := range phases {
			total += r.phaseMS(phase)
		}
		fmt.Fprintf(&b, "| %s | %d |", r.Task, r.Rep)
		for _, phase := range phases {
			fmt.Fprintf(&b, " %.2f / %.2f |", r.Prediction.PhaseShare[phase], float64(r.phaseMS(phase))/float64(max(total, 1)))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n## Conditions\n\n| task | rep | model build | credential | claude | wire | machine | started |\n|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s | %s | %s |\n", r.Task, r.Rep, strings.Join(r.Models, ", "), r.Credential, r.Version, r.Wire, r.Machine, r.StartedAt.Format(time.RFC3339))
	}
	path := filepath.Join(here(), "runs", *batch, "report-"+time.Now().Format("2006-01-02")+".md")
	fmt.Println("wrote", path)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func yesNo(v bool, yes, no string) string {
	if v {
		return yes
	}
	return no
}

func boolCount(v bool) int {
	if v {
		return 1
	}
	return 0
}

func median(values []float64) float64 {
	sorted := slices.Sorted(slices.Values(values))
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func medianOrNone(values []float64) string {
	if len(values) == 0 {
		return "none"
	}
	return fmt.Sprintf("%.1f", median(values))
}

func mean(values []float64) float64 {
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func jeffreysBand(k, n int) (float64, float64) {
	a, b := float64(k)+0.5, float64(n-k)+0.5
	low, high := 0.0, 1.0
	if k > 0 {
		low = betaQuantile((1-bandMass)/2, a, b)
	}
	if k < n {
		high = betaQuantile((1+bandMass)/2, a, b)
	}
	return low, high
}

func betaQuantile(q, a, b float64) float64 {
	low, high := 0.0, 1.0
	for range bisectSteps {
		if middle := (low + high) / 2; regularizedBeta(middle, a, b) < q {
			low = middle
		} else {
			high = middle
		}
	}
	return (low + high) / 2
}

func regularizedBeta(x, a, b float64) float64 {
	if x > (a+1)/(a+b+2) {
		return 1 - regularizedBeta(1-x, b, a)
	}
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	lab, _ := math.Lgamma(a + b)
	front := math.Exp(lab - la - lb + a*math.Log(x) + b*math.Log(1-x))
	return front * betaFraction(x, a, b) / a
}

func betaFraction(x, a, b float64) float64 {
	nonzero := func(v float64) float64 {
		if math.Abs(v) < math.SmallestNonzeroFloat64 {
			return math.SmallestNonzeroFloat64
		}
		return v
	}
	c, d := 1.0, 1/nonzero(1-(a+b)*x/(a+1))
	f := d
	for step := 1; step <= fractionSteps; step++ {
		m := float64(step)
		for _, term := range []float64{m * (b - m) * x / ((a + 2*m - 1) * (a + 2*m)), -(a + m) * (a + b + m) * x / ((a + 2*m) * (a + 2*m + 1))} {
			d = 1 / nonzero(1+term*d)
			c = nonzero(1 + term/c)
			f *= c * d
		}
	}
	return f
}
