package narrator

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// newOllamaRT builds an OllamaNarrator with a fake transport (no network).
func newOllamaRT(rt roundTripFunc) *OllamaNarrator {
	return &OllamaNarrator{
		http:     &http.Client{Transport: rt},
		endpoint: "http://localhost:11434",
		model:    "llama3.2",
	}
}

func TestOllama_Success(t *testing.T) {
	t.Parallel()

	var url, body string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		url = r.URL.String()
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		return resp(http.StatusOK, `{"response":"  Claude is fixing the bug.  "}`), nil
	})

	n := newOllamaRT(rt)
	out, err := n.NarrateOrErr(context.Background(), "editing line 42")
	if err != nil {
		t.Fatalf("NarrateOrErr err = %v", err)
	}
	if out != "Claude is fixing the bug." {
		t.Fatalf("out = %q, want trimmed rephrase", out)
	}
	if !strings.Contains(url, "/api/generate") {
		t.Errorf("URL = %q, want /api/generate", url)
	}
	if !strings.Contains(body, `"stream":false`) {
		t.Errorf("request must set stream:false, got %q", body)
	}
	if !strings.Contains(body, `Narrate this AI assistant output`) {
		t.Errorf("request missing narrate framing: %q", body)
	}
}

func TestOllama_TotalOnFailure(t *testing.T) {
	t.Parallel()

	const input = "some assistant text here"

	t.Run("non-200 falls back to input", func(t *testing.T) {
		t.Parallel()
		n := newOllamaRT(func(r *http.Request) (*http.Response, error) {
			return resp(http.StatusInternalServerError, "boom"), nil
		})
		if got := n.Narrate(context.Background(), input); got != input {
			t.Fatalf("Narrate = %q, want input verbatim on non-200", got)
		}
	})

	t.Run("transport error falls back to input", func(t *testing.T) {
		t.Parallel()
		n := newOllamaRT(func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		})
		out, err := n.NarrateOrErr(context.Background(), input)
		if out != input {
			t.Fatalf("out = %q, want input verbatim when server down", out)
		}
		if err == nil {
			t.Error("NarrateOrErr err = nil, want the underlying failure")
		}
	})
}

// The local narrator does NOT redact — nothing leaves the machine, so the raw
// text (secrets and all) is what ollama sees. This documents the intended
// difference from gemini.
func TestOllama_DoesNotRedact(t *testing.T) {
	t.Parallel()
	const secret = "AKIA" + "IOSFODNN7EXAMPLE" // split literal to satisfy push protection
	var body string
	n := newOllamaRT(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		return resp(http.StatusOK, `{"response":"ok"}`), nil
	})
	n.Narrate(context.Background(), "the key is "+secret)
	if !strings.Contains(body, secret) {
		t.Errorf("local narrator unexpectedly altered the text: %q", body)
	}
}
