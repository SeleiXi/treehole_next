package ranking

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHotScoreUsesBoundedRecencyInsteadOfAbsoluteTimestamp(t *testing.T) {
	expr := hotScore("mysql")

	assert.NotContains(t, expr.SQL, "UNIX_TIMESTAMP")
	assert.Contains(t, expr.SQL, "TIMESTAMPDIFF")
	require.Len(t, expr.Vars, 7)
	assert.Equal(t, 12.0, expr.Vars[0])
	assert.Equal(t, 24.0, expr.Vars[2])
	assert.Equal(t, 8.0, expr.Vars[6])
}

func TestRecommendScoreUsesBoundedRecencyInsteadOfAbsoluteTimestamp(t *testing.T) {
	expr := recommendScore("mysql")

	assert.NotContains(t, expr.SQL, "UNIX_TIMESTAMP")
	assert.Equal(t, 2, strings.Count(expr.SQL, "TIMESTAMPDIFF"))
	require.Len(t, expr.Vars, 10)
	assert.Equal(t, 30.0, expr.Vars[2])
	assert.Equal(t, 4.0, expr.Vars[6])
	assert.Equal(t, 6.0, expr.Vars[9])
}

func TestNormalizeStrategyAcceptsModelRecommendAlias(t *testing.T) {
	assert.Equal(t, StrategyRecommend, NormalizeStrategy("model_recommend"))
	assert.Equal(t, StrategyRecommend, NormalizeStrategy("model-recommend"))
}
