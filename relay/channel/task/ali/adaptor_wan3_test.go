package ali

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestModelList_ContainsWan3 verifies that wan3.0-video and wan3.0-video-prime
// are registered in the ali adaptor's ModelList so channel auto-detection and
// billing estimation recognize them.
func TestModelList_ContainsWan3(t *testing.T) {
	want := map[string]bool{
		"wan3.0-video":       true,
		"wan3.0-video-prime": true,
	}
	for _, m := range ModelList {
		delete(want, m)
	}
	assert.Empty(t, want, "ModelList should contain all wan3.0 models, missing: %v", want)
}
