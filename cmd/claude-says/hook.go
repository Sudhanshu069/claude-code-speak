package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Sudhanshu069/claude-code-speak/internal/config"
	"github.com/Sudhanshu069/claude-code-speak/internal/ipc"
)

// hookOverallTimeout bounds the whole hook op so it never blocks Claude's Stop
// (Node used a 3s hard timeout).
const hookOverallTimeout = 3 * time.Second

// hookSendTimeout bounds the socket handoff (Node used 100ms).
const hookSendTimeout = 100 * time.Millisecond

// maxHookInputBytes caps stdin so a rogue payload can't blow up memory (Node
// used 10MB).
const maxHookInputBytes = 10 * 1024 * 1024

// blockSeparator joins consecutive assistant text blocks so the daemon's
// sentence splitter still fires at block seams (Node bug #15: run-on sentences).
const blockSeparator = "\n"

// HookInput is the JSON Claude Code pipes to a Stop hook on stdin.
type HookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
}

// hookTranscriptEntry is the subset of a transcript JSONL record the hook reads.
type hookTranscriptEntry struct {
	Type    string `json:"type"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// runHook is the Cobra entry point: it wires stdin and the resolved paths into
// RunHook with an overall deadline.
func runHook(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), hookOverallTimeout)
	defer cancel()
	sockPath, stateDir, claudeDir, err := hookPaths()
	if err != nil {
		return err
	}
	debug, _ := cmd.Flags().GetBool("debug")
	return RunHook(ctx, os.Stdin, sockPath, stateDir, claudeDir, debug)
}

// hookPaths resolves the socket path, the offset state dir, and the ~/.claude
// confinement root.
func hookPaths() (sockPath, stateDir, claudeDir string, err error) {
	sockPath, err = config.SocketPath()
	if err != nil {
		return "", "", "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", "", err
	}
	stateDir = filepath.Join(os.TempDir(), "claude-says-state")
	claudeDir = filepath.Join(home, ".claude")
	return sockPath, stateDir, claudeDir, nil
}

// RunHook reads HookInput from r, reads the transcript tail from the saved byte
// offset, forwards assistant text via ipc.Send, and advances the persisted
// offset only on delivery (at-most-once). ctx bounds the whole op. claudeDir
// confines transcript_path; stateDir holds <sid>.offset (dir 0700).
//
// Errors are swallowed (returns nil) the way the Node hook exits 0 on failure:
// a hook must never block or fail Claude's Stop event. When debug is set the raw
// stdin payload is dumped to a temp log first (Node bin/debug-hook.js parity).
func RunHook(ctx context.Context, r io.Reader, sockPath, stateDir, claudeDir string, debug bool) error {
	// Bounded stdin read: cap SIZE via LimitReader(cap+1) AND TIME via ctx. The
	// io.ReadAll is not context-aware, so a stdin that never reaches EOF would
	// block past the 3s hookOverallTimeout and hang Claude's Stop. Reading in a
	// goroutine and racing ctx.Done() makes the 3s deadline actually bound the
	// whole hook (Node's hard timeout). The channel is buffered so the reader
	// never blocks on send after we've abandoned it.
	type readResult struct {
		data []byte
		err  error
	}
	rc := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(r, maxHookInputBytes+1))
		rc <- readResult{data: data, err: err}
	}()

	var data []byte
	select {
	case res := <-rc:
		if res.err != nil {
			return nil
		}
		data = res.data
	case <-ctx.Done():
		return nil // deadline hit before stdin EOF: exit 0 like the Node hook
	}

	if debug {
		writeHookDebug(data)
	}

	if len(data) > maxHookInputBytes {
		return nil
	}

	var in HookInput
	if err := json.Unmarshal(data, &in); err != nil {
		return nil
	}
	if in.TranscriptPath == "" {
		return nil
	}

	// Confine the transcript under ~/.claude, resolving symlinks and .. BEFORE
	// the prefix check (tighter than Node's plain resolve). Fail closed on any
	// resolution error rather than reading an unverifiable path.
	realTranscript, err := filepath.EvalSymlinks(in.TranscriptPath)
	if err != nil {
		return nil
	}
	realClaude := claudeDir
	if rc, rerr := filepath.EvalSymlinks(claudeDir); rerr == nil {
		realClaude = rc
	}
	realClaude = filepath.Clean(realClaude)
	if realTranscript != realClaude && !strings.HasPrefix(realTranscript, realClaude+string(os.PathSeparator)) {
		return nil
	}

	sid := sanitizeSessionID(in.SessionID)

	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil
	}
	offPath := offsetPath(stateDir, sid)
	off := readOffset(offPath)

	text, newOff, err := tailAssistantText(realTranscript, off)
	if err != nil {
		return nil
	}

	// Only advance the offset once the text has actually been handed off (or
	// there was nothing to send). A transient failure (daemon down / timeout)
	// must not skip this text forever.
	delivered := true
	if strings.TrimSpace(text) != "" {
		delivered = ipc.Send(sockPath, ipc.Message{
			Type:      "text",
			SessionID: sid,
			Text:      text,
			Timestamp: time.Now().UnixMilli(),
		}, hookSendTimeout)
	}
	if delivered {
		_ = writeOffset(offPath, newOff)
	}
	return nil
}

// writeHookDebug appends the raw stdin payload (with a timestamp header) to a
// 0600 temp log — the dev-only replacement for Node's bin/debug-hook.js, enabled
// by `hook --debug`. Best-effort: any error is swallowed, since a hook must never
// fail Claude's Stop event.
func writeHookDebug(data []byte) {
	p := filepath.Join(os.TempDir(), "claude-says-hook-debug.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n===== %s =====\n", time.Now().Format(time.RFC3339))
	f.Write(data)
	f.WriteString("\n")
}

// sanitizeSessionID replaces anything that isn't [A-Za-z0-9-] with '_' so the id
// can't traverse out of stateDir (a "../" payload becomes "___"). An empty result
// falls back to "unknown", matching the Node hook.
func sanitizeSessionID(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" {
		return "unknown"
	}
	return s
}

// offsetPath returns stateDir/<sid>.offset.
func offsetPath(stateDir, sid string) string {
	return filepath.Join(stateDir, sid+".offset")
}

// readOffset parses the saved byte offset, or 0 when absent/invalid.
func readOffset(p string) int64 {
	data, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	off, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || off < 0 {
		return 0
	}
	return off
}

// writeOffset persists the byte offset with 0600 perms.
func writeOffset(p string, off int64) error {
	return os.WriteFile(p, []byte(strconv.FormatInt(off, 10)), 0o600)
}

// tailAssistantText seeks to off and reads to EOF, collecting assistant text
// blocks joined with a sentence-boundary-safe separator (fix #15). It resets to
// 0 when off > size (truncation). newOff is always the file's end (byte size) so
// the caller advances past everything read.
func tailAssistantText(path string, off int64) (text string, newOff int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", off, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return "", off, err
	}
	size := fi.Size()
	if off > size {
		// Transcript truncated/rotated in place: restart from the top.
		off = 0
	}
	if size <= off {
		// Nothing new; still report the end so the offset stays at EOF.
		return "", size, nil
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return "", off, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", off, err
	}

	var blocks []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e hookTranscriptEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // truncated/malformed line — skip
		}
		if e.Type != "assistant" {
			continue
		}
		for _, block := range e.Message.Content {
			if block.Type == "text" && block.Text != "" {
				blocks = append(blocks, block.Text)
			}
		}
	}
	return strings.Join(blocks, blockSeparator), size, nil
}
