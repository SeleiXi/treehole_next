package recsys

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelRecommendUsesRecommendFlowAndModelRank(t *testing.T) {
	assert.True(t, ShouldUseRecommend("", "", "model_recommend"))
	assert.True(t, ShouldUseRecommend("model-recommend", "", ""))
	assert.True(t, ShouldUseModelRank("", "", "model_recommend"))
	assert.True(t, ShouldUseModelRank("model-recommend", "", ""))
}

func TestRuleRecommendDoesNotForceModelRank(t *testing.T) {
	assert.True(t, ShouldUseRecommend("", "recommend", "recommend"))
	assert.False(t, ShouldUseModelRank("", "recommend", "recommend"))
}
