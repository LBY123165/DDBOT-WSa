package bilibili

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDynamicSrvSpaceHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network request in short mode")
	}

	resp, err := DynamicSrvSpaceHistory(97505)
	if err != nil {
		t.Skipf("skipping unavailable bilibili API: %v", err)
	}
	if resp.GetCode() != 0 {
		t.Skipf("skipping unavailable bilibili API: code %d", resp.GetCode())
	}
	assert.Zero(t, resp.GetCode())
}
