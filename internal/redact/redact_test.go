package redact

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/852hamza/chowki/internal/testutil"
)

func TestDetectors(t *testing.T) {
	for _, secret := range testutil.Secrets() {
		t.Run(secret.Type+" "+secret.Value[:12], func(t *testing.T) {
			text := "Some text, " + secret.Context + secret.Value + " and more."
			got := Find(text)
			if len(got) != 1 || got[0].Type != secret.Type || text[got[0].Start:got[0].End] != secret.Value {
				t.Errorf("Find(%q) = %+v; want one %s covering %q", text, got, secret.Type, secret.Value)
			}
		})
	}
}

func TestNoFalsePositives(t *testing.T) {
	for _, text := range []string{
		"Write to user@example.com or ops@mail.example.org, or admin@site.test.",
		"password = changeme",
		"token = get_token() and api_key = os.environ",
		"a card 4111 1111 1111 1112 fails the Luhn check",
		"an IBAN GB82 WEST 1234 5698 7654 33 fails its check digits",
		"task-queue-worker-pool-size and the risk-assessment-report",
		"xAKIA" + "IOSFODNN7EXAMPLE is part of a longer word",
		"the year 2026, pi 3.14159, call 12345, id abc12345678901234",
		"@Override public void run() and npm i @types/node",
		"go get github.com/foo/bar@v1.2.3",
	} {
		if got := Find(text); len(got) != 0 {
			t.Errorf("Find(%q) = %+v; want nothing", text, got)
		}
	}
}

func TestOverlapsPreferTheSpecificType(t *testing.T) {
	key := "sk-" + "ant-api03-" + strings.Repeat("Ab1-Cd2_", 5)
	if got := Find("key " + key); len(got) != 1 || got[0].Type != TypeAnthropicKey {
		t.Errorf("Find() = %+v; want one anthropic_key, not also an openai_key", got)
	}
}

var placeholderRE = regexp.MustCompile(`^\[REDACTED:[a-z_]+:[0-9a-f]{8}\]$`)

func TestPlaceholders(t *testing.T) {
	r := New([]byte("server secret"), ModeMask)
	p := r.Placeholder(TypeEmail, "jane.doe@company.io")
	if !placeholderRE.MatchString(p) || !strings.HasPrefix(p, "[REDACTED:email:") {
		t.Errorf("Placeholder() = %q", p)
	}
	// AC: placeholders are deterministic, and differ by value, type and
	// server secret.
	if again := New([]byte("server secret"), ModeMask).Placeholder(TypeEmail, "jane.doe@company.io"); again != p {
		t.Errorf("the same value got %q and %q", p, again)
	}
	for name, other := range map[string]string{
		"value":  r.Placeholder(TypeEmail, "john@company.io"),
		"type":   r.Placeholder(TypeSecret, "jane.doe@company.io"),
		"secret": New([]byte("other"), ModeMask).Placeholder(TypeEmail, "jane.doe@company.io"),
	} {
		if other[len(other)-9:] == p[len(p)-9:] {
			t.Errorf("another %s got the same hash: %q", name, other)
		}
	}
}

func TestMask(t *testing.T) {
	r := New([]byte("k"), ModeMask)
	text := "Mail jane.doe@company.io, card 4111 1111 1111 1111."
	got := r.Mask(text, Find(text))
	want := "Mail " + r.Placeholder(TypeEmail, "jane.doe@company.io") + ", card " +
		r.Placeholder(TypeCard, "4111 1111 1111 1111") + "."
	if got != want {
		t.Errorf("Mask() = %q, want %q", got, want)
	}
}

func TestMode(t *testing.T) {
	r := New(nil, ModeMask)
	if r.Mode("") != ModeMask || r.Mode(ModeBlock) != ModeBlock {
		t.Errorf("Mode() doesn't fall back to the default only for keys without a mode")
	}
}

// Fuzz tests must show linear running time: every detector scans
// windows that are merged when they overlap, so dense keywords don't
// multiply the work.
func TestLinearTime(t *testing.T) {
	for name, unit := range map[string]string{
		"keywords":    "sk-AKIAghp_eyJ@x.",
		"digits":      "1 2-3 ",
		"pairs":       "password=a1",
		"iban starts": "GB82 ",
	} {
		timeOf := func(n int) time.Duration {
			text := strings.Repeat(unit, n/len(unit))
			start := time.Now()
			Find(text)
			return time.Since(start)
		}
		small, large := timeOf(64<<10), timeOf(512<<10)
		if large > 20*small+100*time.Millisecond {
			t.Errorf("%s: 8 times the text took %v, against %v", name, large, small)
		}
	}
}

func FuzzFind(f *testing.F) {
	for _, secret := range testutil.Secrets() {
		f.Add(secret.Context + secret.Value)
	}
	f.Add("é@é.co 0300-1234567 +92 3")
	f.Fuzz(func(t *testing.T, text string) {
		last := 0
		for _, fd := range Find(text) {
			if fd.Start < last || fd.End <= fd.Start || fd.End > len(text) {
				t.Fatalf("Find(%q) returned %+v after offset %d", text, fd, last)
			}
			last = fd.End
		}
	})
}
