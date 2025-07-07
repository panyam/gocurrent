package gocurrent

// IDFunc is an identity function that returns its input unchanged.
// It's commonly used as a default mapper function for pipes and other operations.
func IDFunc[T any](input T) T {
	return input
}

// Message represents a value with optional error and source information.
// It's used by channels to carry both successful values and error conditions.
type Message[T any] struct {
	Value  T     // The actual value being transmitted
	Error  error // Any error that occurred during processing
	Source any   // Optional source information for debugging
}
