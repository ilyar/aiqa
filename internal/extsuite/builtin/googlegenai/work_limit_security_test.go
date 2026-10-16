package googlegenai

import (
	"fmt"
	"testing"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/extsuite"
	"github.com/ilyar/aiqa/internal/result"
)

func TestApplyErrorPreservesWrappedWorkLimitAsExecutionGap(t *testing.T) {
	e := extsuite.SuiteEvent{}
	applyError(&e, fmt.Errorf("sdk: %w", api.ErrWorkLimit))
	if e.Outcome != result.ExtensionOutcomeExecutionError || len(e.Signals) != 1 || e.Signals[0].Code != "execution.work_limit" {
		t.Fatalf("event=%+v", e)
	}
}
