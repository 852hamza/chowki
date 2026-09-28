// Package secretbox manages the master key and, with it, encrypts provider
// keys and cache entries with AES-256-GCM envelopes. The master key lives in
// a file readable only by its owner, or in the CHOWKI_MASTER_KEY
// environment variable.
package secretbox
