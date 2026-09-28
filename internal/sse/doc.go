// Package sse parses server-sent event streams and relays them event by
// event, flushing each one, so a client sees every chunk as soon as the
// provider sends it. The relay forwards the exact bytes it received.
package sse
