package domainconstruct

import (
	"slices"

	"golang.org/x/tools/go/ssa"
)

// errorGuardChecked proves a call's original error nil under assumptions about
// a forwarded error. A non-nil replacement can exclude an incoming branch even
// when the payload itself was not merged with that branch.
func (c *zeroChecker) errorGuardChecked(value ssa.Value, context flowContext) bool {
	merges := c.errorMergeValues(value.Parent())
	if len(merges) == 0 {
		return false
	}
	// Bound predecessor exploration. An incomplete proof retains the warning.
	remaining := 64
	return c.nilErrorOnPaths(value, context, merges, make(map[*ssa.Phi]bool), &remaining)
}

func (c *zeroChecker) nilErrorOnPaths(value ssa.Value, context flowContext, merges []*ssa.Phi, visiting map[*ssa.Phi]bool, remaining *int) bool {
	if state := c.valueTruth(value, context, make(map[ssa.Value]bool)); state != unknown {
		return state == isZero
	}
	for _, merge := range merges {
		if context.facts[merge] != isZero || visiting[merge] {
			continue
		}
		visiting[merge] = true
		possible, checked := false, true
		for index := range merge.Edges {
			if *remaining == 0 {
				delete(visiting, merge)
				return false
			}
			*remaining--
			edge := c.edgeContext(context, merge.Block(), index)
			if !edge.possible {
				continue
			}
			possible = true
			if !c.nilErrorOnPaths(value, edge, merges, visiting, remaining) {
				checked = false
				break
			}
		}
		delete(visiting, merge)
		if possible && checked {
			return true
		}
	}
	return false
}

// Index immutable SSA once. Later merges are tried first because a forwarded
// error commonly depends on earlier merges. Loop-carried errors stay unknown.
func (c *zeroChecker) errorMergeValues(function *ssa.Function) []*ssa.Phi {
	if merges, found := c.errorMerges[function]; found {
		return merges
	}
	var merges []*ssa.Phi
	for _, block := range function.Blocks {
		loop := false
		for _, predecessor := range block.Preds {
			loop = loop || block.Dominates(predecessor)
		}
		if loop {
			continue
		}
		for _, instruction := range block.Instrs {
			if phi, ok := instruction.(*ssa.Phi); ok && isErrorType(phi.Type()) {
				merges = append(merges, phi)
			}
		}
	}
	slices.Reverse(merges)
	if c.errorMerges == nil {
		c.errorMerges = make(map[*ssa.Function][]*ssa.Phi)
	}
	c.errorMerges[function] = merges
	return merges
}
