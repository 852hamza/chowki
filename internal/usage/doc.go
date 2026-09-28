// Package usage reads token usage from provider responses, JSON or
// streamed, normalizes it across API families, and computes cost and
// savings from the model catalog. When a price or the usage is unknown,
// the cost is unknown too, never guessed.
package usage
