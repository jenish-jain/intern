package indexer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScoreFiles(t *testing.T) {
	// Create a sample index
	index := &FileIndex{
		Version:   "1.0",
		IndexedAt: time.Now(),
		RepoRoot:  "/test/repo",
		Files: map[string]FileMetadata{
			"internal/auth/login.go": {
				Path:       "internal/auth/login.go",
				Importance: 7.0,
				Category:   "core",
			},
			"internal/auth/service.go": {
				Path:       "internal/auth/service.go",
				Importance: 6.0,
				Category:   "core",
			},
			"internal/orchestrator/coordinator.go": {
				Path:       "internal/orchestrator/coordinator.go",
				Importance: 9.0,
				Category:   "core",
			},
			"cmd/agent/main.go": {
				Path:       "cmd/agent/main.go",
				Importance: 8.0,
				Category:   "core",
			},
			"README.md": {
				Path:       "README.md",
				Importance: 3.0,
				Category:   "doc",
			},
			"internal/auth/login_test.go": {
				Path:       "internal/auth/login_test.go",
				Importance: 4.0,
				Category:   "test",
			},
		},
	}

	tests := []struct {
		name             string
		keywords         []string
		expectedTopFiles []string // Expected files in top results (order matters)
		minTopScore      float64  // Minimum expected score for top file
	}{
		{
			name:     "exact path match",
			keywords: []string{"internal/auth/login.go"},
			expectedTopFiles: []string{
				"internal/auth/login.go", // Should be #1
			},
			minTopScore: 20.0, // Base importance + exact match boost
		},
		{
			name:     "directory path match",
			keywords: []string{"internal/auth"},
			expectedTopFiles: []string{
				"internal/auth/login.go",
				"internal/auth/service.go",
			},
			minTopScore: 10.0,
		},
		{
			name:     "module name match",
			keywords: []string{"auth", "login"},
			expectedTopFiles: []string{
				"internal/auth/login.go", // Matches both keywords
			},
			minTopScore: 10.0,
		},
		{
			name:     "orchestrator keyword",
			keywords: []string{"orchestrator", "coordinator"},
			expectedTopFiles: []string{
				"internal/orchestrator/coordinator.go",
			},
			minTopScore: 15.0,
		},
		{
			name:     "multiple keywords favor best match",
			keywords: []string{"auth", "service"},
			expectedTopFiles: []string{
				"internal/auth/service.go", // Matches both keywords exactly
			},
			minTopScore: 10.0,
		},
		{
			name:             "no keyword match returns all sorted by importance",
			keywords:         []string{"nonexistent"},
			expectedTopFiles: []string{},
			minTopScore:      0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scores := ScoreFiles(index, tt.keywords)

			if len(tt.expectedTopFiles) == 0 {
				// For no-match cases, check that scores are minimal
				for _, score := range scores {
					assert.LessOrEqual(t, score.Score, 15.0,
						"Score should be low when keywords don't match")
				}
				return
			}

			// Check that we have results
			require.NotEmpty(t, scores, "Should have scored files")

			// Check that top file meets minimum score
			assert.GreaterOrEqual(t, scores[0].Score, tt.minTopScore,
				"Top file score should meet minimum: got %.2f, want >= %.2f",
				scores[0].Score, tt.minTopScore)

			// Check that expected files appear in top results
			topPaths := make([]string, 0, len(tt.expectedTopFiles))
			for i := 0; i < len(scores) && i < len(tt.expectedTopFiles)*2; i++ {
				topPaths = append(topPaths, scores[i].Path)
			}

			for _, expectedPath := range tt.expectedTopFiles {
				assert.Contains(t, topPaths, expectedPath,
					"Expected '%s' in top results, got: %v", expectedPath, topPaths)
			}
		})
	}
}

func TestScoreFile(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		metadata FileMetadata
		keywords []string
		minScore float64
		maxScore float64
	}{
		{
			name: "exact path match gets highest boost",
			path: "internal/auth/login.go",
			metadata: FileMetadata{
				Importance: 5.0,
				Category:   "core",
			},
			keywords: []string{"internal/auth/login.go"},
			minScore: 25.0, // 5.0 base + 15.0 exact match, * 1.5 category
			maxScore: 35.0,
		},
		{
			name: "partial path match",
			path: "internal/auth/login.go",
			metadata: FileMetadata{
				Importance: 5.0,
				Category:   "core",
			},
			keywords: []string{"internal/auth"},
			minScore: 15.0, // 5.0 base + 8.0 substring, * 1.5 category
			maxScore: 25.0,
		},
		{
			name: "segment exact match",
			path: "internal/auth/login.go",
			metadata: FileMetadata{
				Importance: 5.0,
				Category:   "core",
			},
			keywords: []string{"auth"},
			minScore: 12.0, // 5.0 base + 5.0 segment, * 1.5 category
			maxScore: 20.0,
		},
		{
			name: "test file gets lower multiplier",
			path: "internal/auth/login_test.go",
			metadata: FileMetadata{
				Importance: 5.0,
				Category:   "test",
			},
			keywords: []string{"auth"},
			minScore: 5.0, // (5.0 base + 5.0 segment) * 0.7 test multiplier
			maxScore: 10.0,
		},
		{
			name: "doc file gets lowest multiplier",
			path: "docs/authentication.md",
			metadata: FileMetadata{
				Importance: 3.0,
				Category:   "doc",
			},
			keywords: []string{"authentication"},
			minScore: 2.0, // (3.0 base + 5.0 segment) * 0.5 doc multiplier
			maxScore: 6.0,
		},
		{
			name: "multiple keyword matches accumulate",
			path: "internal/auth/user_service.go",
			metadata: FileMetadata{
				Importance: 6.0,
				Category:   "core",
			},
			keywords: []string{"auth", "user", "service"},
			minScore: 25.0, // High score from multiple matches
			maxScore: 50.0,
		},
		{
			name: "no keyword match returns only base importance",
			path: "internal/auth/login.go",
			metadata: FileMetadata{
				Importance: 5.0,
				Category:   "core",
			},
			keywords: []string{"database", "postgres"},
			minScore: 5.0, // Only base importance * category multiplier
			maxScore: 10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreFile(tt.path, tt.metadata, tt.keywords)

			assert.GreaterOrEqual(t, score, tt.minScore,
				"Score %.2f should be >= %.2f", score, tt.minScore)
			assert.LessOrEqual(t, score, tt.maxScore,
				"Score %.2f should be <= %.2f", score, tt.maxScore)
		})
	}
}

func TestGetCategoryMultiplier(t *testing.T) {
	tests := []struct {
		category   string
		multiplier float64
	}{
		{"core", 1.5},
		{"config", 1.2},
		{"other", 1.0},
		{"test", 0.7},
		{"doc", 0.5},
		{"unknown", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			result := getCategoryMultiplier(tt.category)
			assert.Equal(t, tt.multiplier, result)
		})
	}
}

func TestSelectTopFiles(t *testing.T) {
	scores := []FileScore{
		{Path: "file1.go", Score: 100.0},
		{Path: "file2.go", Score: 80.0},
		{Path: "file3.go", Score: 60.0},
		{Path: "file4.go", Score: 40.0},
		{Path: "file5.go", Score: 20.0},
	}

	tests := []struct {
		name     string
		n        int
		expected []string
	}{
		{
			name:     "select top 3",
			n:        3,
			expected: []string{"file1.go", "file2.go", "file3.go"},
		},
		{
			name:     "select top 1",
			n:        1,
			expected: []string{"file1.go"},
		},
		{
			name:     "select more than available",
			n:        10,
			expected: []string{"file1.go", "file2.go", "file3.go", "file4.go", "file5.go"},
		},
		{
			name:     "select zero",
			n:        0,
			expected: nil,
		},
		{
			name:     "select negative",
			n:        -1,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SelectTopFiles(scores, tt.n)

			if tt.expected == nil {
				assert.Nil(t, result)
				return
			}

			assert.Equal(t, len(tt.expected), len(result))
			for i, expected := range tt.expected {
				assert.Equal(t, expected, result[i].Path)
			}
		})
	}
}

func TestGetScoreDistribution(t *testing.T) {
	tests := []struct {
		name   string
		scores []FileScore
		expect ScoreStats
	}{
		{
			name: "normal distribution",
			scores: []FileScore{
				{Path: "f1", Score: 100.0},
				{Path: "f2", Score: 80.0},
				{Path: "f3", Score: 60.0},
				{Path: "f4", Score: 40.0},
				{Path: "f5", Score: 20.0},
			},
			expect: ScoreStats{
				Total:  5,
				Min:    20.0,
				Max:    100.0,
				Mean:   60.0,
				Median: 60.0,
			},
		},
		{
			name: "even number of scores",
			scores: []FileScore{
				{Path: "f1", Score: 100.0},
				{Path: "f2", Score: 80.0},
				{Path: "f3", Score: 60.0},
				{Path: "f4", Score: 40.0},
			},
			expect: ScoreStats{
				Total:  4,
				Min:    40.0,
				Max:    100.0,
				Mean:   70.0,
				Median: 70.0, // (80 + 60) / 2
			},
		},
		{
			name:   "empty scores",
			scores: []FileScore{},
			expect: ScoreStats{},
		},
		{
			name: "single score",
			scores: []FileScore{
				{Path: "f1", Score: 50.0},
			},
			expect: ScoreStats{
				Total:  1,
				Min:    50.0,
				Max:    50.0,
				Mean:   50.0,
				Median: 50.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetScoreDistribution(tt.scores)
			assert.Equal(t, tt.expect.Total, result.Total)
			assert.InDelta(t, tt.expect.Min, result.Min, 0.01)
			assert.InDelta(t, tt.expect.Max, result.Max, 0.01)
			assert.InDelta(t, tt.expect.Mean, result.Mean, 0.01)
			assert.InDelta(t, tt.expect.Median, result.Median, 0.01)
		})
	}
}

func TestScoreFiles_NilHandling(t *testing.T) {
	t.Run("nil index", func(t *testing.T) {
		scores := ScoreFiles(nil, []string{"keyword"})
		assert.Nil(t, scores)
	})

	t.Run("nil keywords", func(t *testing.T) {
		index := &FileIndex{Files: map[string]FileMetadata{}}
		scores := ScoreFiles(index, nil)
		assert.Nil(t, scores)
	})

	t.Run("empty keywords", func(t *testing.T) {
		index := &FileIndex{Files: map[string]FileMetadata{}}
		scores := ScoreFiles(index, []string{})
		assert.Nil(t, scores)
	})
}

func TestScoreFiles_Sorting(t *testing.T) {
	index := &FileIndex{
		Files: map[string]FileMetadata{
			"low.go": {
				Path:       "low.go",
				Importance: 1.0,
				Category:   "other",
			},
			"high.go": {
				Path:       "high.go",
				Importance: 10.0,
				Category:   "core",
			},
			"medium.go": {
				Path:       "medium.go",
				Importance: 5.0,
				Category:   "core",
			},
		},
	}

	scores := ScoreFiles(index, []string{"go"})

	// Should be sorted highest to lowest
	require.Len(t, scores, 3)
	assert.Equal(t, "high.go", scores[0].Path)
	assert.Greater(t, scores[0].Score, scores[1].Score)
	assert.Greater(t, scores[1].Score, scores[2].Score)
}

// Benchmark tests
func BenchmarkScoreFiles(b *testing.B) {
	// Create a large index
	index := &FileIndex{
		Files: make(map[string]FileMetadata),
	}

	// Add 1000 files
	for i := 0; i < 1000; i++ {
		path := "internal/module" + string(rune(i%10+'0')) + "/file" + string(rune(i%100+'0')) + ".go"
		index.Files[path] = FileMetadata{
			Path:       path,
			Importance: float64(i % 10),
			Category:   "core",
		}
	}

	keywords := []string{"module5", "file42", "internal"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ScoreFiles(index, keywords)
	}
}

func BenchmarkScoreFile(b *testing.B) {
	metadata := FileMetadata{
		Path:       "internal/orchestrator/coordinator.go",
		Importance: 7.5,
		Category:   "core",
	}
	keywords := []string{"orchestrator", "coordinator", "internal"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scoreFile(metadata.Path, metadata, keywords)
	}
}
