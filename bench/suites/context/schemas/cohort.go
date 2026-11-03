package schemas

const CurrentToolsetFirstWriteFloor = 6000

type Cohort struct {
	Dir                string
	UsableTurns        int
	SkippedTurns       int
	SkipReasons        map[string]int
	WithCacheWrite     int
	SingleStep         int
	MultiStep          int
	TotalCacheWrite    int
	TotalCacheRead     int
	TotalFreshInput    int
	FirstWriteSum      int
	CurrentToolsetRuns int
	CurrentSingleStep  int
}

func BuildCohort(dir string) (Cohort, error) {
	usable, skipped, err := ReadSessions(dir)
	if err != nil {
		return Cohort{}, err
	}
	cohort := Cohort{Dir: dir, UsableTurns: len(usable), SkippedTurns: len(skipped), SkipReasons: map[string]int{}}
	for _, s := range skipped {
		cohort.SkipReasons[s.Reason]++
	}
	for _, turn := range usable {
		write, hasWrite := turn.FirstCacheWrite()
		if !hasWrite {
			continue
		}
		cohort.WithCacheWrite++
		cohort.TotalCacheWrite += turn.SumCacheWrite()
		cohort.TotalCacheRead += turn.SumCacheRead()
		cohort.TotalFreshInput += turn.SumFreshInput()
		cohort.FirstWriteSum += write
		if len(turn.Steps) <= 1 {
			cohort.SingleStep++
		} else {
			cohort.MultiStep++
		}
		if write >= CurrentToolsetFirstWriteFloor {
			cohort.CurrentToolsetRuns++
			if len(turn.Steps) <= 1 {
				cohort.CurrentSingleStep++
			}
		}
	}
	return cohort, nil
}
