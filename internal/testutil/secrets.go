package testutil

import "strings"

// Secret is an example of the data that Chowki's redaction finds, in the
// context that its detector needs.
type Secret struct {
	Type    string // the redaction type, such as "email"
	Context string // text that must come right before Value
	Value   string
}

// Secrets returns an example of each type of secret and personal data
// that Chowki's redaction finds. The values are assembled from pieces, so
// that secret scanners, such as GitHub's push protection, don't flag the
// source; none of them is real. None says "example" either, which Chowki's
// scanner reads as a placeholder.
func Secrets() []Secret {
	return []Secret{
		{"private_key", "", "-----BEGIN RSA " + "PRIVATE KEY-----\n" +
			"MIIEowIBAAKCAQEAu1SU1LfVLPHCozMxH2Mo4lgOEePzNm0tRgeLezV6ffAt0gun\n-----END RSA " + "PRIVATE KEY-----"},
		{"aws_access_key", "", "AKIA" + "QX7RT2LM9VZ3PK4W"},
		{"github_token", "", "ghp" + "_" + strings.Repeat("A1b2C3d4E5f6", 3)},
		{"slack_token", "", "xox" + "b-123456789012-1234567890123-" + strings.Repeat("AbCdEfGh", 3)},
		{"anthropic_key", "", "sk-" + "ant-api03-" + strings.Repeat("Ab1-Cd2_", 5)},
		{"openai_key", "", "sk-" + "proj-" + strings.Repeat("Ab1Cd2Ef3", 5)},
		{"google_api_key", "", "AI" + "za" + strings.Repeat("Ab1_", 8) + "Cd2"},
		{"stripe_key", "", "sk" + "_live_" + strings.Repeat("Ab1Cd2Ef3", 3)},
		{"jwt", "", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9" + ".eyJzdWIiOiIxMjM0NTY3ODkwIn0" +
			".dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"},
		{"chowki_key", "", "chowki" + "_" + strings.Repeat("aB3", 16) + "x"},
		{"chowki_key", "", "chowki" + "_admin_" + strings.Repeat("aB3", 16) + "x"},
		{"secret", "password = ", "S3cr3tValue9x"},
		{"email", "", "jane.doe@company.io"},
		{"card_number", "", "4111 1111 1111 1111"},
		{"card_number", "", "5555-5555-5555-4444"},
		{"iban", "", "GB82 WEST 1234 5698 7654 32"},
		{"iban", "", "PK36SCBL0000001123456702"},
		{"cnic", "", "35202-1234567-1"},
		{"pk_mobile", "", "0300-1234567"},
		{"pk_mobile", "", "+92 300 1234567"},
		{"phone", "", "+14155552671"},
	}
}
