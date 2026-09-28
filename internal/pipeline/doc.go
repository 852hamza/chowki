// Package pipeline composes the stages that every gateway request passes
// through: authenticate, parse, route, call the provider, relay the
// response, and record usage and cost. It keeps request bodies as the
// client sent them, except where a stage must change a field.
package pipeline
