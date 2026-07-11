package tts

import (
	"context"
	"strconv"
	"testing"

	"github.com/Sudhanshu069/claude-says/internal/config"
)

// ---- macOS pure arg builder (never exec say) ------------------------------

func TestSayArgs_ExactVectorAndEndOfOptionsGuard(t *testing.T) {
	cases := []struct {
		name  string
		voice string
		rate  int
		out   string
		text  string
	}{
		{"plain", "Samantha", 200, "/tmp/a.aiff", "hello world"},
		{"dash-leading text (CWE-88)", "Alex", 175, "/tmp/b.aiff", "-f/etc/passwd"},
		{"double-dash text", "Alex", 175, "/tmp/b.aiff", "--o/tmp/evil"},
		{"empty text", "Samantha", 200, "/tmp/c.aiff", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sayArgs(tc.voice, tc.rate, tc.out, tc.text)
			want := []string{
				"-v", tc.voice,
				"-r", strconv.Itoa(tc.rate),
				"-o", tc.out,
				"--",
				tc.text,
			}
			if len(got) != len(want) {
				t.Fatalf("len(sayArgs)=%d args %v, want %d %v", len(got), got, len(want), want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("arg[%d]=%q, want %q (full=%v)", i, got[i], want[i], got)
				}
			}
			// The CWE-88 guard: "--" must be second-to-last and text must be the
			// final element, so dash-leading text is never parsed as a flag.
			if got[len(got)-2] != "--" {
				t.Fatalf("second-to-last arg=%q, want the %q end-of-options guard", got[len(got)-2], "--")
			}
			if got[len(got)-1] != tc.text {
				t.Fatalf("last arg=%q, want the text %q as the final (non-flag) element", got[len(got)-1], tc.text)
			}
		})
	}
}

// ---- macOS provider defaults / overrides (asserted via sayArgs) -----------

func TestNewMacOS_DefaultsAndOverrides(t *testing.T) {
	tests := []struct {
		name      string
		cfg       config.MacosConfig
		wantVoice string
		wantRate  int
	}{
		{"zero => Samantha/200", config.MacosConfig{}, "Samantha", 200},
		{"override voice only", config.MacosConfig{Voice: "Daniel"}, "Daniel", 200},
		{"override rate only", config.MacosConfig{Rate: 300}, "Samantha", 300},
		{"override both", config.MacosConfig{Voice: "Alex", Rate: 150}, "Alex", 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov, err := newMacOS(&config.Config{Macos: tt.cfg})
			if err != nil {
				t.Fatalf("newMacOS: %v", err)
			}
			p := prov.(*MacOSProvider)
			if p.voice != tt.wantVoice {
				t.Errorf("voice=%q, want %q", p.voice, tt.wantVoice)
			}
			if p.rate != tt.wantRate {
				t.Errorf("rate=%d, want %d", p.rate, tt.wantRate)
			}
			// Confirm the defaults flow through the arg vector (never exec say).
			args := sayArgs(p.voice, p.rate, "/tmp/x.aiff", "hi")
			if args[1] != tt.wantVoice {
				t.Errorf("sayArgs voice=%q, want %q", args[1], tt.wantVoice)
			}
			if args[3] != strconv.Itoa(tt.wantRate) {
				t.Errorf("sayArgs rate=%q, want %q", args[3], strconv.Itoa(tt.wantRate))
			}
		})
	}
}

func TestMacOS_SynthesizeFormatIsAIFF(t *testing.T) {
	// Force a guaranteed-fast failure of `say` by cancelling ctx, so the real
	// binary never renders audio, yet the returned format is still AIFF.
	prov, err := newMacOS(&config.Config{})
	if err != nil {
		t.Fatalf("newMacOS: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, format, synErr := prov.Synthesize(ctx, "hi")
	if synErr == nil {
		t.Fatalf("expected error from cancelled Synthesize")
	}
	if format != FormatAIFF {
		t.Fatalf("format=%q, want %q", format, FormatAIFF)
	}
	if FormatAIFF != "aiff" {
		t.Fatalf("FormatAIFF=%q, want %q", FormatAIFF, "aiff")
	}
}

// ---- registry (macOS-only) ------------------------------------------------

func TestNew_AlwaysMacOS(t *testing.T) {
	// Any provider name — including a stale cloud provider left in an old config —
	// resolves to the macOS provider rather than erroring, so start never fails.
	for _, name := range []string{"macos", "", "google", "elevenlabs", "nope"} {
		p, err := New(&config.Config{Provider: name})
		if err != nil {
			t.Fatalf("New(%q): %v", name, err)
		}
		if _, ok := p.(*MacOSProvider); !ok {
			t.Fatalf("New(%q) = %T, want *MacOSProvider", name, p)
		}
	}
}

// Format constants are the contract the audio player relies on.
func TestFormatConstants(t *testing.T) {
	if FormatAIFF != "aiff" || FormatWAV != "wav" {
		t.Fatalf("format consts = %q/%q, want aiff/wav", FormatAIFF, FormatWAV)
	}
}
