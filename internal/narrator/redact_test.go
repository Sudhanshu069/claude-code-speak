package narrator

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	t.Parallel()

	// Fake secrets are ASSEMBLED FROM FRAGMENTS so the committed source never
	// contains a contiguous secret-shaped literal — otherwise GitHub push
	// protection blocks the push of these very redaction tests. The split is a
	// source-level trick only; the runtime string RedactSecrets sees is the full
	// token, so the tests exercise real redaction.
	var (
		awsKey    = "AKIA" + "IOSFODNN7EXAMPLE"
		googleKey = "AIza" + "0123456789_0123456789_0123456789_01"
		ghToken   = "ghp_" + "0123456789abcdefghijklmnopqrstuvwxyz"
		skKey     = "sk-" + "abcdefghijklmnopqrstuvwxyz"
		skAntKey  = "sk-ant-" + "abcdefghijklmnop123"
		slackTok  = "xoxb-" + "12345678901-abcdefghijklmno"
		jwtTok    = "eyJhbGciOiJIUzI1Ni" + "." + "eyJzdWIiOiIxMjM0" + "." + "dozjgNryP4J3jVm"
	)

	// Each input contains a secret that must be gone after redaction; the
	// wantGone substring is a distinctive slice of that secret.
	redactCases := []struct {
		name     string
		in       string
		wantGone string
	}{
		{"aws access key", "key is " + awsKey + " ok", awsKey},
		{"google api key", googleKey + " here", googleKey},
		{"github token", "token " + ghToken + " done", ghToken},
		{"openai sk key", "use " + skKey + " now", skKey},
		{"anthropic sk-ant", "key " + skAntKey + " set", skAntKey},
		{"slack token", "slack " + slackTok + " end", slackTok},
		{"jwt", "jwt " + jwtTok + " here", "eyJhbGciOiJIUzI1Ni"},
		{"bearer", "Authorization: Bearer abcdef1234567890token done", "abcdef1234567890token"},
		{"api_key assignment", `config api_key=supersecretvalue123 loaded`, "supersecretvalue123"},
		{"password assignment", `password: hunter2secret here`, "hunter2secret"},
	}
	for _, c := range redactCases {
		t.Run(c.name, func(t *testing.T) {
			out := RedactSecrets(c.in)
			if strings.Contains(out, c.wantGone) {
				t.Errorf("secret survived redaction:\n in:  %q\n out: %q", c.in, out)
			}
			if !strings.Contains(out, redactedMark) {
				t.Errorf("no [REDACTED] marker in %q", out)
			}
		})
	}

	// Ordinary narration must pass through untouched — no false positives.
	keep := []string{
		"Let me check the config for the timeout.",
		"The function returns a value on success.",
		"Claude is editing the parser to fix the crash.",
		"The secret to good tests is clear assertions.", // "secret" with no assignment
		"I ran the tests and they all pass now.",
	}
	for _, s := range keep {
		if got := RedactSecrets(s); got != s {
			t.Errorf("benign text changed:\n in:  %q\n out: %q", s, got)
		}
	}
}

// The bearer/assignment redactors keep the surrounding shape so the sentence
// still reads sensibly (the prefix and key survive, only the value is scrubbed).
func TestRedactKeepsShape(t *testing.T) {
	t.Parallel()
	out := RedactSecrets("set api_key=supersecretvalue123 then go")
	if !strings.Contains(out, "api_key=") {
		t.Errorf("assignment key/separator not preserved: %q", out)
	}
	out = RedactSecrets("send Bearer abcdef1234567890token now")
	if !strings.Contains(strings.ToLower(out), "bearer ") {
		t.Errorf("bearer prefix not preserved: %q", out)
	}
}

// The security-critical guarantee: a secret in the assistant text must NOT reach
// Gemini — the POSTed body carries [REDACTED] instead. Exercises the full
// generate() egress path, not just RedactSecrets in isolation.
func TestGemini_RedactsBeforeEgress(t *testing.T) {
	t.Parallel()
	const secret = "AKIA" + "IOSFODNN7EXAMPLE" // split literal (see redactCases note)
	input := "the deploy key is " + secret + " and it works"

	var body string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		return resp(http.StatusOK, okBody), nil
	})

	n := newGeminiRT("k", rt)
	n.Narrate(context.Background(), input)

	if strings.Contains(body, secret) {
		t.Fatalf("secret leaked to Gemini in the request body:\n%s", body)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("expected a [REDACTED] marker in the egress body:\n%s", body)
	}
}
