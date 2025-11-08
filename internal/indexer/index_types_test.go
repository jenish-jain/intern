package indexer_test

import (
	"encoding/json"
	"intern/internal/indexer"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileIndex_JSONSerialization(t *testing.T) {
	now := time.Now().Truncate(time.Second)

	index := &indexer.FileIndex{
		Version:   "1.0",
		IndexedAt: now,
		RepoRoot:  "/test/repo",
		Files: map[string]indexer.FileMetadata{
			"main.go": {
				Path:         "main.go",
				Size:         1024,
				Importance:   10.0,
				Category:     "core",
				Dependencies: []string{"fmt", "os"},
				LastModified: now,
				Summary:      "Main entry point",
			},
		},
		Modules: map[string][]string{
			"cmd": {"main.go"},
		},
	}

	// Serialize to JSON
	data, err := json.Marshal(index)
	require.NoError(t, err)
	assert.Contains(t, string(data), "main.go")
	assert.Contains(t, string(data), "Main entry point")

	// Deserialize from JSON
	var decoded indexer.FileIndex
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, index.Version, decoded.Version)
	assert.Equal(t, index.RepoRoot, decoded.RepoRoot)
	assert.Equal(t, index.IndexedAt.Unix(), decoded.IndexedAt.Unix())
	assert.Len(t, decoded.Files, 1)
	assert.Equal(t, "main.go", decoded.Files["main.go"].Path)
	assert.Equal(t, 10.0, decoded.Files["main.go"].Importance)
}

func TestFileMetadata_DefaultValues(t *testing.T) {
	meta := indexer.FileMetadata{
		Path: "test.go",
		Size: 500,
	}

	assert.Equal(t, "test.go", meta.Path)
	assert.Equal(t, int64(500), meta.Size)
	assert.Equal(t, 0.0, meta.Importance)
	assert.Equal(t, "", meta.Category)
	assert.Nil(t, meta.Dependencies)
}

func TestContextStrategy_Constants(t *testing.T) {
	assert.Equal(t, indexer.ContextStrategy("full"), indexer.ContextStrategyFull)
	assert.Equal(t, indexer.ContextStrategy("smart"), indexer.ContextStrategySmart)
}

func TestContextTier_Values(t *testing.T) {
	assert.Equal(t, 0, int(indexer.ContextTierBaseline))
	assert.Equal(t, 1, int(indexer.ContextTierSmart))
	assert.Equal(t, 2, int(indexer.ContextTierFull))
}

func TestContextBuildOptions_DefaultBehavior(t *testing.T) {
	opts := indexer.ContextBuildOptions{
		Strategy:        indexer.ContextStrategySmart,
		Tier:            indexer.ContextTierSmart,
		MaxFiles:        15,
		MaxBytesPerFile: 100000,
		UseCache:        true,
	}

	assert.Equal(t, indexer.ContextStrategySmart, opts.Strategy)
	assert.Equal(t, indexer.ContextTierSmart, opts.Tier)
	assert.Equal(t, 15, opts.MaxFiles)
	assert.True(t, opts.UseCache)
}

func TestFileScore_Comparison(t *testing.T) {
	scores := []indexer.FileScore{
		{Path: "high.go", Score: 10.0},
		{Path: "medium.go", Score: 5.0},
		{Path: "low.go", Score: 1.0},
	}

	assert.Greater(t, scores[0].Score, scores[1].Score)
	assert.Greater(t, scores[1].Score, scores[2].Score)
}
