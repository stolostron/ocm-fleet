// Package unrelated deliberately resembles the domain model but is not in scope.
package unrelated

type Value struct{ generation int }

func (v Value) Generation() int { return v.generation }
func Read(v Value) int          { return v.generation }
