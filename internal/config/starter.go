package config

import "strings"

// Starter returns the configuration file that `chowki init` writes, with a
// link to docsURL in its header.
func Starter(docsURL string) []byte {
	return []byte(strings.Replace(starter, "{{DOCS_URL}}", docsURL, 1))
}

const starter = `# Chowki configuration, created by chowki init.
# Every setting can also come from an environment variable: CHOWKI_ plus the
# setting's path in capitals, such as CHOWKI_SERVER_LISTEN for server.listen.
# Documentation: {{DOCS_URL}}

server:
  listen: ":8080"          # address and port to listen on
  max_body_mb: 20          # largest request body, in MiB
  upstream_timeout: 600s   # limit for one provider call, including a whole stream

storage:
  driver: sqlite
  dsn: "file:data/chowki.db"

security:
  master_key_file: ".chowki/master.key"   # created by chowki init; keep it private
  allow_private_upstreams: true           # allow providers on localhost or a private network

log:
  level: info              # debug, info, warn or error

retention_days: 90         # days to keep request metadata

# The providers that Chowki forwards requests to. Keys never go in this file:
# api_key_env names the environment variable, or .env entry, that holds each key.
providers:
  - name: openai
    type: openai
    base_url: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_API_KEY
  # Any OpenAI-compatible API works the same way, for example a local Ollama server:
  # - name: local
  #   type: openai
  #   base_url: http://localhost:11434/v1
`
