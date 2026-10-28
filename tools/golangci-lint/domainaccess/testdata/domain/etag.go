package domain

// Etag calculation intentionally hashes the raw persisted state.
func EtagGeneration(f *Fulfillment) int { return f.generation }
