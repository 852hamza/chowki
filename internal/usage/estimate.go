package usage

import "bytes"

const (
	// bytesPerToken is the usual rule of thumb for English text and code;
	// other scripts take more bytes per token, which errs on the safe side.
	bytesPerToken = 4
	// mediaTokens is the estimate for one inline image, audio clip or
	// document, whose size in bytes says little about its tokens.
	mediaTokens = 1600
	// minBase64 is the length from which a string of base64 characters
	// counts as inline media rather than text.
	minBase64 = 1024
)

// EstimateTokens estimates the input tokens of a JSON request body without
// a tokenizer: one token per 4 bytes outside whitespace, and a fixed amount
// for each inline media file, sent as a data: URL or as base64 text. It's
// meant for pre-checks, such as budgets; reports only use the usage that
// providers report. It accepts any input, valid JSON or not.
func EstimateTokens(body []byte) int64 {
	var text, media int64
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case ' ', '\t', '\n', '\r':
		case '"':
			end := stringEnd(body, i+1)
			if isMedia(body[i+1 : end]) {
				media++
			} else {
				text += int64(min(end+1, len(body)) - i)
			}
			i = end
		default:
			text++
		}
	}
	return (text+bytesPerToken-1)/bytesPerToken + media*mediaTokens
}

// stringEnd returns the position of the quote that ends the JSON string
// starting at i, or len(b) when the string doesn't end.
func stringEnd(b []byte, i int) int {
	for ; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return len(b)
}

// isMedia reports whether s, the raw content of a JSON string, is a
// base64 data: URL, or base64 text long enough to be a file.
func isMedia(s []byte) bool {
	if rest, ok := bytes.CutPrefix(s, []byte("data:")); ok {
		_, data, ok := bytes.Cut(rest, []byte(";base64,"))
		return ok && isBase64(data)
	}
	return len(s) >= minBase64 && isBase64(s)
}

// isBase64 reports whether s holds only characters of the standard or URL
// base64 alphabets, and the escaped slashes and line breaks that JSON
// encoders may add.
func isBase64(s []byte) bool {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '+', c == '/', c == '=', c == '-', c == '_':
		case c == '\\' && i+1 < len(s) && (s[i+1] == '/' || s[i+1] == 'n' || s[i+1] == 'r'):
			i++
		default:
			return false
		}
	}
	return true
}
