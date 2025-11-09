package indexer

import (
	"sort"
	"strings"
)

// ScoreFiles ranks files in the index based on relevance to the given keywords
// Returns a sorted slice of FileScore (highest scores first)
func ScoreFiles(index *FileIndex, keywords []string) []FileScore {
	if index == nil || len(keywords) == 0 {
		return nil
	}

	scores := make([]FileScore, 0, len(index.Files))

	// Score each file
	for path, metadata := range index.Files {
		score := scoreFile(path, metadata, keywords)
		if score > 0 {
			scores = append(scores, FileScore{
				Path:  path,
				Score: score,
			})
		}
	}

	// Sort by score (highest first)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	return scores
}

// scoreFile calculates relevance score for a single file
func scoreFile(path string, metadata FileMetadata, keywords []string) float64 {
	score := 0.0

	// Start with base importance from metadata
	score += metadata.Importance

	// Normalize path for matching
	lowerPath := strings.ToLower(path)
	pathWithoutExt := strings.TrimSuffix(lowerPath, ".go")
	pathWithoutExt = strings.TrimSuffix(pathWithoutExt, ".md")
	pathWithoutExt = strings.TrimSuffix(pathWithoutExt, ".json")

	for _, keyword := range keywords {
		keyword = strings.ToLower(keyword)

		// HIGHEST BOOST: Exact path match
		// e.g., keyword "internal/auth/login.go" matches path exactly
		if lowerPath == keyword {
			score += 15.0
			continue
		}

		// HIGH BOOST: Path contains full keyword as substring
		// e.g., keyword "internal/auth" matches "internal/auth/login.go"
		if strings.Contains(lowerPath, keyword) {
			score += 8.0
			continue
		}

		// MEDIUM BOOST: Path segment matches keyword
		// e.g., keyword "auth" matches any path with "/auth/" or ending in "auth.go"
		pathSegments := strings.Split(lowerPath, "/")
		for _, segment := range pathSegments {
			segmentWithoutExt := strings.TrimSuffix(segment, ".go")
			segmentWithoutExt = strings.TrimSuffix(segmentWithoutExt, ".md")
			segmentWithoutExt = strings.TrimSuffix(segmentWithoutExt, ".json")

			if segmentWithoutExt == keyword {
				score += 5.0
				break
			}

			// SMALL BOOST: Segment contains keyword
			// e.g., keyword "user" matches "user_service.go"
			if strings.Contains(segmentWithoutExt, keyword) {
				score += 2.0
				break
			}
		}
	}

	// Apply category multipliers
	score *= getCategoryMultiplier(metadata.Category)

	return score
}

// getCategoryMultiplier returns a multiplier based on file category
// Core files are most relevant, docs/tests are least relevant
func getCategoryMultiplier(category string) float64 {
	switch category {
	case "core":
		return 1.5 // Boost core files significantly
	case "config":
		return 1.2 // Config files often relevant
	case "other":
		return 1.0 // Neutral
	case "test":
		return 0.7 // Tests less likely to be needed for understanding
	case "doc":
		return 0.5 // Docs rarely needed in code context
	default:
		return 1.0
	}
}

// SelectTopFiles returns the top N files by score
func SelectTopFiles(scores []FileScore, n int) []FileScore {
	if n <= 0 || len(scores) == 0 {
		return nil
	}

	if n >= len(scores) {
		return scores
	}

	return scores[:n]
}

// GetScoreDistribution returns statistics about score distribution
// Useful for debugging and tuning scoring algorithm
func GetScoreDistribution(scores []FileScore) ScoreStats {
	if len(scores) == 0 {
		return ScoreStats{}
	}

	stats := ScoreStats{
		Total: len(scores),
		Min:   scores[len(scores)-1].Score, // Already sorted
		Max:   scores[0].Score,
	}

	sum := 0.0
	for _, s := range scores {
		sum += s.Score
	}
	stats.Mean = sum / float64(len(scores))

	// Calculate median
	mid := len(scores) / 2
	if len(scores)%2 == 0 {
		stats.Median = (scores[mid-1].Score + scores[mid].Score) / 2
	} else {
		stats.Median = scores[mid].Score
	}

	return stats
}

// ScoreStats contains statistics about score distribution
type ScoreStats struct {
	Total  int     // Total number of scored files
	Min    float64 // Minimum score
	Max    float64 // Maximum score
	Mean   float64 // Average score
	Median float64 // Median score
}
