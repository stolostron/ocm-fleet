package domain

// This file is the explicit serialization boundary described in domain.md.
func SnapshotGeneration(f *Fulfillment) int { return f.generation }
