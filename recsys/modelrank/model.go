package modelrank

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"sync"
	"time"
)

type LinearModel struct {
	Version   string             `json:"version"`
	Task      string             `json:"task,omitempty"`
	Intercept float64            `json:"intercept"`
	Weights   map[string]float64 `json:"weights"`
	MinScore  *float64           `json:"min_score,omitempty"`
	MaxScore  *float64           `json:"max_score,omitempty"`
}

func (model *LinearModel) Score(features map[string]float64) float64 {
	if model == nil {
		return 0
	}
	score := model.Intercept
	for name, value := range features {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		score += model.Weights[name] * value
	}
	if model.MinScore != nil && score < *model.MinScore {
		return *model.MinScore
	}
	if model.MaxScore != nil && score > *model.MaxScore {
		return *model.MaxScore
	}
	return score
}

type cacheEntry struct {
	model   *LinearModel
	err     error
	mtime   time.Time
	size    int64
	missing bool
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

func Load(path string) (*LinearModel, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	cacheMu.Lock()
	if entry, ok := cache[path]; ok && entry.mtime.Equal(info.ModTime()) && entry.size == info.Size() {
		cacheMu.Unlock()
		return entry.model, entry.err
	}
	cacheMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		store(path, cacheEntry{err: err, mtime: info.ModTime(), size: info.Size()})
		return nil, err
	}
	var model LinearModel
	if err := json.Unmarshal(data, &model); err != nil {
		store(path, cacheEntry{err: err, mtime: info.ModTime(), size: info.Size()})
		return nil, err
	}
	if model.Weights == nil {
		model.Weights = map[string]float64{}
	}
	store(path, cacheEntry{model: &model, mtime: info.ModTime(), size: info.Size()})
	return &model, nil
}

func store(path string, entry cacheEntry) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache[path] = entry
}
