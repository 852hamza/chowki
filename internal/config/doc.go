// Package config loads the gateway configuration: defaults, the YAML file
// (chowki.yaml), .env files and CHOWKI_ environment variable overrides, then
// validates it. Provider keys come only from environment variables and are
// held as Secret values, which never print.
package config
