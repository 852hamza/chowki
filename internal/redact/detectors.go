package redact

import (
	"math"
	"regexp"
	"strings"
)

// Types of the data that the detectors find. Placeholders and counts name
// them; they never hold the value.
const (
	TypePrivateKey   = "private_key"
	TypeAWSKey       = "aws_access_key"
	TypeGitHubToken  = "github_token"
	TypeSlackToken   = "slack_token"
	TypeAnthropicKey = "anthropic_key"
	TypeOpenAIKey    = "openai_key"
	TypeGoogleKey    = "google_api_key"
	TypeStripeKey    = "stripe_key"
	TypeJWT          = "jwt"
	TypeChowkiKey    = "chowki_key"
	TypeSecret       = "secret"
	TypeEmail        = "email"
	TypeCard         = "card_number"
	TypeIBAN         = "iban"
	TypeCNIC         = "cnic"
	TypePKMobile     = "pk_mobile"
	TypePhone        = "phone"
)

// detector finds one type of data. Running every regular expression over
// long prompts would be slow, so a detector first finds candidates cheaply:
// the positions of its keywords, or of digit runs, IBAN starts and so on.
// Its expression then runs only on windows around them.
type detector struct {
	typ string
	re  *regexp.Regexp
	// keywords occur in every match; lower means that they are matched
	// against the lowercased text.
	keywords []string
	lower    bool
	// find, when not byKeywords, names the candidate finder to use instead.
	find int
	// before and after bound how far a match reaches before and after a
	// keyword's position.
	before, after int
	// group is the submatch to redact; 0 is the whole match.
	group int
	// valid rejects matches that only look right, such as card numbers
	// that fail the Luhn check.
	valid func(value string) bool
}

// detectors run in priority order: when matches overlap, the earlier
// detector wins, so a specific type beats a generic one.
var detectors = []*detector{
	{typ: TypePrivateKey, keywords: []string{"PRIVATE KEY"}, before: 50, after: 16384,
		re: regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]{0,30}PRIVATE KEY[A-Z0-9 ]{0,10}-----.*?` +
			`(?:-----END [A-Z0-9 ]{0,30}PRIVATE KEY[A-Z0-9 ]{0,10}-----|\z)`)},
	{typ: TypeAWSKey, keywords: []string{"AKIA", "ASIA"}, before: 1, after: 21,
		re: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{typ: TypeGitHubToken, keywords: []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"}, before: 1, after: 270,
		re: regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,255}|github_pat_[A-Za-z0-9_]{50,255})\b`)},
	{typ: TypeSlackToken, keywords: []string{"xox", "hooks.slack.com"}, before: 10, after: 260,
		re: regexp.MustCompile(`\bxox[abeoprs]-[A-Za-z0-9-]{10,250}|https://hooks\.slack\.com/services/[A-Za-z0-9/_-]{20,200}`)},
	{typ: TypeAnthropicKey, keywords: []string{"sk-ant-"}, before: 1, after: 310,
		re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,300}`)},
	{typ: TypeOpenAIKey, keywords: []string{"sk-"}, before: 1, after: 310,
		re: regexp.MustCompile(`\bsk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{20,300}`)},
	{typ: TypeGoogleKey, keywords: []string{"AIza"}, before: 1, after: 40,
		re: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{typ: TypeStripeKey, keywords: []string{"_live_"}, before: 3, after: 260,
		re: regexp.MustCompile(`\b(?:sk|rk)_live_[A-Za-z0-9]{20,250}\b`)},
	{typ: TypeJWT, keywords: []string{"eyJ"}, before: 1, after: 16384,
		re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{10,}`)},
	{typ: TypeChowkiKey, keywords: []string{"chowki_"}, before: 1, after: 63,
		re: regexp.MustCompile(`\bchowki_(?:admin_)?[0-9A-Za-z]{49}\b`)},
	{typ: TypeSecret, keywords: []string{"pass", "pwd", "secret", "token", "key"}, lower: true, before: 20, after: 120,
		group: 1, valid: looksRandom,
		re: regexp.MustCompile(`(?i)\b(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|secret[_-]?key|` +
			`client[_-]?secret|auth[_-]?token)\b["']?\s*[:=]\s*["']?([A-Za-z0-9+/=_.!@#$%^&*~-]{8,64})`)},
	{typ: TypeEmail, keywords: []string{"@"}, before: 64, after: 280, valid: notExampleEmail,
		re: regexp.MustCompile(`[A-Za-z0-9._%+-]{1,64}@[A-Za-z0-9.-]{1,253}\.[A-Za-z]{2,24}\b`)},
	{typ: TypeCard, find: byDigitRuns, valid: isCard,
		re: regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`)},
	{typ: TypeIBAN, find: byIBANStarts, valid: isIBAN,
		re: regexp.MustCompile(`\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){2,7}(?: ?[A-Z0-9]{1,4})?\b`)},
	{typ: TypeCNIC, find: byDigitRuns,
		re: regexp.MustCompile(`\b\d{5}-\d{7}-\d\b`)},
	{typ: TypePKMobile, find: byDigitRuns,
		re: regexp.MustCompile(`(?:\+92|\b0092)[ -]?3\d{2}[ -]?\d{7}\b|\b03\d{2}[ -]?\d{7}\b`)},
	{typ: TypePhone, find: byDigitRuns,
		re: regexp.MustCompile(`\+[1-9](?:[ -]?\d){7,14}\b`)},
}

// looksRandom accepts values of a key = value pair that look like a
// secret rather than code or a word: letters and digits, and varied.
func looksRandom(v string) bool {
	return strings.ContainsAny(v, "0123456789") && strings.IndexFunc(v, isLetter) >= 0 && entropy(v) >= 3
}

func isLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

// entropy is the Shannon entropy of s, in bits per byte.
func entropy(s string) float64 {
	var counts [256]int
	for i := range len(s) {
		counts[s[i]]++
	}
	var h float64
	for _, c := range counts {
		if c > 0 {
			p := float64(c) / float64(len(s))
			h -= p * math.Log2(p)
		}
	}
	return h
}

// notExampleEmail rejects addresses in the domains reserved for examples
// and tests (RFC 2606), which aren't anyone's data.
func notExampleEmail(v string) bool {
	domain := strings.ToLower(v[strings.LastIndexByte(v, '@')+1:])
	for _, d := range []string{"example.com", "example.org", "example.net"} {
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return false
		}
	}
	for _, tld := range []string{".example", ".test", ".invalid", ".localhost"} {
		if strings.HasSuffix(domain, tld) {
			return false
		}
	}
	return true
}

// isCard accepts 13 to 19 digits with the prefix of a major card network
// and a valid Luhn check digit.
func isCard(v string) bool {
	digits := onlyDigits(v)
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	switch {
	case digits[0] == '4', // Visa
		digits[0] == '5' && digits[1] >= '1' && digits[1] <= '5',           // Mastercard
		digits[0] == '2' && digits[1] >= '2' && digits[1] <= '7',           // Mastercard 2-series
		digits[0] == '3' && (digits[1] == '4' || digits[1] == '7'),         // American Express
		strings.HasPrefix(digits, "6011"), strings.HasPrefix(digits, "65"): // Discover
	default:
		return false
	}
	sum := 0
	for i := range len(digits) {
		d := int(digits[len(digits)-1-i] - '0')
		if i%2 == 1 {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

// isIBAN checks the length and the ISO 13616 mod-97 check digits.
func isIBAN(v string) bool {
	s := strings.ReplaceAll(v, " ", "")
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	rem := 0
	for _, c := range s[4:] + s[:4] {
		switch {
		case c >= '0' && c <= '9':
			rem = (rem*10 + int(c-'0')) % 97
		case c >= 'A' && c <= 'Z':
			rem = (rem*100 + int(c-'A') + 10) % 97
		default:
			return false
		}
	}
	return rem == 1
}

func onlyDigits(s string) string {
	var b strings.Builder
	for i := range len(s) {
		if s[i] >= '0' && s[i] <= '9' {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// window is a range of text where a detector's expression runs.
type window struct{ start, end int }

// Candidate finders, for detectors whose matches have no keyword. Find
// runs each at most once per text.
const (
	byKeywords = iota
	byDigitRuns
	byIBANStarts
)

var finders = map[int]func(string) []window{byDigitRuns: digitRuns, byIBANStarts: ibanStarts}

// minRunDigits is the fewest digits a digit run needs to hold a phone
// number, the shortest data that the digit detectors find.
const minRunDigits = 8

// digitRuns finds runs of digits, with single spaces or dashes between
// them and an optional leading +, that hold at least minRunDigits digits.
// Each window has a byte of context on both sides, for \b.
func digitRuns(text string) []window {
	var out []window
	for i := 0; i < len(text); {
		if !isDigit(text[i]) && (text[i] != '+' || i+1 == len(text) || !isDigit(text[i+1])) {
			i++
			continue
		}
		start, digits := i, 0
	run:
		for ; i < len(text); i++ {
			switch {
			case isDigit(text[i]):
				digits++
			case i == start && text[i] == '+':
			case i > start && (text[i] == ' ' || text[i] == '-') && i+1 < len(text) && isDigit(text[i+1]):
			default:
				break run
			}
		}
		if digits >= minRunDigits {
			out = append(out, window{max(start-1, 0), min(i+1, len(text))})
		}
	}
	return out
}

// ibanStarts finds two capital letters followed by two digits at the start
// of a word, where an IBAN begins; the window covers the longest IBAN.
func ibanStarts(text string) []window {
	var out []window
	for i := 0; i+4 <= len(text); i++ {
		if isUpper(text[i]) && isUpper(text[i+1]) && isDigit(text[i+2]) && isDigit(text[i+3]) &&
			(i == 0 || !isAlnum(text[i-1])) {
			out = append(out, window{max(i-1, 0), min(i+43, len(text))}) // 34 characters and 8 spaces at most
		}
	}
	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func isAlnum(c byte) bool { return isDigit(c) || isUpper(c) || c >= 'a' && c <= 'z' }
