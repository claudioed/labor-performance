package performance_test

import (
	"testing"

	"github.com/claudioed/labor-performance/internal/domain/performance"
)

func floatPtr(f float64) *float64 { return &f }

func TestClassifyTrend(t *testing.T) {
	tests := []struct {
		name              string
		recentMean        *float64
		baselineMean      *float64
		recentScoredCount int
		want              performance.TrendDirection
	}{
		{
			name:              "nil recent mean is insufficient data, never fabricated",
			recentMean:        nil,
			baselineMean:      floatPtr(90),
			recentScoredCount: 10,
			want:              performance.TrendInsufficientData,
		},
		{
			name:              "nil baseline mean is insufficient data",
			recentMean:        floatPtr(90),
			baselineMean:      nil,
			recentScoredCount: 10,
			want:              performance.TrendInsufficientData,
		},
		{
			name:              "below minimum scored count is insufficient data even with a big diff",
			recentMean:        floatPtr(100),
			baselineMean:      floatPtr(50),
			recentScoredCount: 2,
			want:              performance.TrendInsufficientData,
		},
		{
			name:              "recent mean 10pts above baseline is improving",
			recentMean:        floatPtr(95),
			baselineMean:      floatPtr(85),
			recentScoredCount: 5,
			want:              performance.TrendImproving,
		},
		{
			name:              "recent mean exactly at the threshold above baseline is improving",
			recentMean:        floatPtr(90),
			baselineMean:      floatPtr(85),
			recentScoredCount: 3,
			want:              performance.TrendImproving,
		},
		{
			name:              "recent mean 10pts below baseline is declining",
			recentMean:        floatPtr(75),
			baselineMean:      floatPtr(85),
			recentScoredCount: 5,
			want:              performance.TrendDeclining,
		},
		{
			name:              "recent mean exactly at the threshold below baseline is declining",
			recentMean:        floatPtr(80),
			baselineMean:      floatPtr(85),
			recentScoredCount: 3,
			want:              performance.TrendDeclining,
		},
		{
			name:              "recent mean within the noise band of baseline is stable",
			recentMean:        floatPtr(87),
			baselineMean:      floatPtr(85),
			recentScoredCount: 5,
			want:              performance.TrendStable,
		},
		{
			name:              "identical means is stable",
			recentMean:        floatPtr(85),
			baselineMean:      floatPtr(85),
			recentScoredCount: 3,
			want:              performance.TrendStable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := performance.ClassifyTrend(tt.recentMean, tt.baselineMean, tt.recentScoredCount)
			if got != tt.want {
				t.Errorf("ClassifyTrend() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectCoachingFlag(t *testing.T) {
	tests := []struct {
		name   string
		scores []float64
		want   bool
	}{
		{
			name:   "fewer than 3 scored tasks never flags, regardless of values",
			scores: []float64{50, 40},
			want:   false,
		},
		{
			name:   "empty slice never flags",
			scores: []float64{},
			want:   false,
		},
		{
			name:   "exactly 3 consecutive below floor flags",
			scores: []float64{80, 70, 60},
			want:   true,
		},
		{
			name:   "3 below floor at the end of a longer history flags, ignoring earlier good scores",
			scores: []float64{120, 110, 130, 80, 70, 60},
			want:   true,
		},
		{
			name:   "3 below floor NOT at the end (a later good task breaks the streak) does not flag",
			scores: []float64{80, 70, 60, 120},
			want:   false,
		},
		{
			name:   "one of the last 3 exactly at the floor does not flag (floor is inclusive of the boundary as OK)",
			scores: []float64{80, 70, 85},
			want:   false,
		},
		{
			name:   "one of the last 3 just above the floor does not flag",
			scores: []float64{80, 70, 85.01},
			want:   false,
		},
		{
			name:   "all last 3 just under the floor flags",
			scores: []float64{80, 70, 84.99},
			want:   true,
		},
		{
			name:   "exactly at the floor for all 3 does not flag (floor itself counts as meeting standard)",
			scores: []float64{85, 85, 85},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := performance.DetectCoachingFlag(tt.scores)
			if got != tt.want {
				t.Errorf("DetectCoachingFlag(%v) = %v, want %v", tt.scores, got, tt.want)
			}
		})
	}
}
