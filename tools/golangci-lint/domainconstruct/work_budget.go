package domainconstruct

const (
	maxInferenceWork = 2_048
	maxFlowWork      = 4_096
	maxLateCallWork  = 256
)

// A budget is shared across the branches of a query. Nil allows a caller
// that performs only a local, non-recursive check to omit the budget.
type workBudget int

func (b *workBudget) take() bool {
	if b == nil {
		return true
	}
	if *b <= 0 {
		return false
	}
	*b--
	return true
}

func (b *workBudget) exhausted() bool {
	return b != nil && *b <= 0
}

func (c *zeroChecker) resetWork() {
	if c.work != nil {
		*c.work = maxFlowWork
	}
}
