package performance

// TrendDirection classifies how an associate's recent scored performance
// compares to their all-time baseline.
type TrendDirection string

const (
	TrendImproving        TrendDirection = "IMPROVING"
	TrendDeclining        TrendDirection = "DECLINING"
	TrendStable           TrendDirection = "STABLE"
	TrendInsufficientData TrendDirection = "INSUFFICIENT_DATA"
)

// trendThresholdPct is how many percentage points recentMean must move
// away from baselineMean before the difference counts as a real trend
// rather than noise. 5 points is a deliberately conservative threshold:
// EfficiencyPct already has natural task-to-task variance (task
// difficulty, interruptions), so a smaller threshold would flag "trend"
// on noise most days. Chosen as a documented constant rather than a
// magic number so it can be revisited with real fleet data later.
const trendThresholdPct = 5.0

// minScoredForTrend is the fewest scored (non-nil EfficiencyPct) recent
// tasks required before this function will call a trend at all — below
// this, "recent mean" is too noisy a statistic to act on.
const minScoredForTrend = 3

// ClassifyTrend compares a recent-window mean EfficiencyPct against the
// associate's all-time baseline mean and returns a direction. Either
// input being nil (no scored tasks in that window) always yields
// TrendInsufficientData — never a fabricated direction from a missing
// number. recentScoredCount is the count of SCORED tasks the recent mean
// was computed over (distinct from total recent task count, which may
// include unscored rows) — below minScoredForTrend, the result is also
// TrendInsufficientData regardless of how large the raw difference looks,
// since a mean of 1-2 tasks is not a trend.
func ClassifyTrend(recentMean, baselineMean *float64, recentScoredCount int) TrendDirection {
	if recentMean == nil || baselineMean == nil || recentScoredCount < minScoredForTrend {
		return TrendInsufficientData
	}

	diff := *recentMean - *baselineMean
	if diff >= trendThresholdPct {
		return TrendImproving
	}
	if diff <= -trendThresholdPct {
		return TrendDeclining
	}
	return TrendStable
}

// coachingConsecutiveThreshold is how many consecutive scored tasks must
// all fall below coachingEfficiencyFloor before a CoachingFlag is raised.
// 3 mirrors the everyday floor-supervisor heuristic ("three in a row,
// time for a word") rather than reacting to a single bad task, which
// could just be a hard order. Documented constant, not a magic number.
const coachingConsecutiveThreshold = 3

// coachingEfficiencyFloor is the EfficiencyPct below which a task counts
// as "below standard" for coaching-flag purposes. 85% (not 100%) is
// deliberate: EfficiencyPct is a ratio against an engineered standard,
// and consistently landing a little under 100% is normal variance, not a
// coaching-worthy pattern — this context flags a genuine, sustained
// shortfall, not routine noise. Mirrors ADR-0002's "visibility, not
// enforcement" discipline: the floor exists to surface a real signal,
// not to punish falling short of a perfectly-tuned standard.
const coachingEfficiencyFloor = 85.0

// DetectCoachingFlag reports whether the most recent
// coachingConsecutiveThreshold SCORED tasks (nil-EfficiencyPct rows are
// skipped entirely — they carry no signal either way, never counted as
// "below standard") are ALL below coachingEfficiencyFloor.
// recentScoredEfficiencyPctsChronological must be ordered oldest-first
// (ascending CompletedAt) with only scored (non-nil) values already
// extracted — the caller (application layer) is responsible for that
// filtering, keeping this function a pure, directly-testable predicate
// over plain floats. Returns false whenever fewer than
// coachingConsecutiveThreshold scored values are available — a flag
// requires enough real signal to mean something.
func DetectCoachingFlag(recentScoredEfficiencyPctsChronological []float64) bool {
	n := len(recentScoredEfficiencyPctsChronological)
	if n < coachingConsecutiveThreshold {
		return false
	}

	for _, pct := range recentScoredEfficiencyPctsChronological[n-coachingConsecutiveThreshold:] {
		if pct >= coachingEfficiencyFloor {
			return false
		}
	}
	return true
}
