// Package catalog embeds the model catalog, models.json, in the binary.
// internal/catalog parses and validates it.
//
// Every entry lists an official pricing page in "source". Prices are in USD
// per million tokens. A model missing from the catalog still works; its
// requests are recorded as unpriced.
package catalog

import _ "embed"

// ModelsJSON is the content of models.json.
//
//go:embed models.json
var ModelsJSON []byte
