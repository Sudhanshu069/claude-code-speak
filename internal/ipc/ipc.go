// Package ipc is the Unix-domain-socket, newline-delimited-JSON transport
// between the hook and the daemon. It mirrors Node src/ipc.js: a 0600 socket
// inside the owner-owned config dir, an lstat (non-follow) guard against
// symlink/regular-file hijack, a 1MB per-connection buffering cap, and a total
// Send that never errors so the hook can gate its at-most-once offset advance.
package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// maxLineBytes caps per-connection buffering so a client that never sends a
// newline can't grow daemon memory without bound.
const maxLineBytes = 1 << 20 // 1MB

// liveProbeTimeout bounds the dial we use to detect whether an existing socket
// already has a daemon listening before we consider it stale.
const liveProbeTimeout = 200 * time.Millisecond

// Message is the NDJSON wire shape hooks send to the daemon.
type Message struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
	Timestamp int64  `json:"timestamp"`
}

// Server is a unix-socket NDJSON receiver. Serve is the sole producer of
// Messages.
type Server struct {
	path string
	ln   net.Listener
	out  chan Message
}

// NewServer creates the parent dir 0700, lstat-guards the path (unlink only a
// real socket; refuse a symlink/regular file), binds, and chmods the socket
// 0600.
//
// Improving on Node bug #6 (which blindly unlinked any existing socket): if the
// existing path is a socket we first probe it with a short dial. If a daemon is
// still listening we refuse rather than hijack the live path; only a stale
// socket (dial fails) is unlinked and rebound.
func NewServer(sockPath string) (*Server, error) {
	dir := filepath.Dir(sockPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	// lstat does NOT follow symlinks: only ever unlink a real socket, and
	// refuse to bind through anything else in our owner-only dir.
	if fi, err := os.Lstat(sockPath); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to use %s: it exists but is not a socket; remove it and try again", sockPath)
		}
		// A real socket exists. Detect a live daemon before hijacking it.
		if c, derr := net.DialTimeout("unix", sockPath, liveProbeTimeout); derr == nil {
			_ = c.Close()
			return nil, fmt.Errorf("refusing to use %s: a daemon appears to be already listening", sockPath)
		}
		// Stale socket from a crashed run: safe to remove and rebind.
		if err := os.Remove(sockPath); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, err
	}

	// Restrict the socket to the owning user. On macOS the filesystem
	// permission is enforced on connect, so this blocks other local accounts
	// from injecting speech into the daemon. Best-effort: listen already
	// succeeded.
	_ = os.Chmod(sockPath, 0o600)

	return &Server{
		path: sockPath,
		ln:   ln,
		out:  make(chan Message, 64),
	}, nil
}

// Messages is the read side the daemon selects on. Closed when Serve returns.
func (s *Server) Messages() <-chan Message {
	return s.out
}

// Serve accepts until ctx is cancelled, then closes the listener and removes
// the socket file. Per-connection newline framing with a maxLineBytes cap:
// oversized newline-less streams are dropped, malformed JSON lines skipped.
func (s *Server) Serve(ctx context.Context) error {
	defer close(s.out)
	defer os.Remove(s.path)
	defer s.ln.Close()

	// Unblock Accept and any in-flight connection reads on cancellation.
	go func() {
		<-ctx.Done()
		_ = s.ln.Close()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			wg.Wait()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handleConn(ctx, conn)
		}()
	}
}

// handleConn reads newline-delimited JSON messages from one client, capping
// per-line buffering at maxLineBytes. A too-long line (no newline) drops the
// connection; malformed JSON lines are skipped.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	// Close the connection on shutdown so a client that never sends can't pin
	// this goroutine past ctx cancellation.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // skip malformed messages
		}
		select {
		case s.out <- msg:
		case <-ctx.Done():
			return
		}
	}
}

// Send writes exactly one NDJSON message to sockPath. It returns delivered=true
// only on a clean handoff; a missing/non-socket path, dial failure, or timeout
// all return false (never an error) so the hook won't advance past lost text.
// The whole operation is bounded by timeout.
func Send(sockPath string, msg Message, timeout time.Duration) (delivered bool) {
	// Only ever write transcript text to a real socket. lstat does not follow
	// symlinks; if the path is missing (daemon down) or not a socket, report
	// non-delivery rather than connecting through an unexpected path.
	fi, err := os.Lstat(sockPath)
	if err != nil || fi.Mode()&os.ModeSocket == 0 {
		return false
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	data = append(data, '\n')

	deadline := time.Now().Add(timeout)
	d := net.Dialer{Timeout: timeout}
	conn, err := d.Dial("unix", sockPath)
	if err != nil {
		return false
	}
	defer conn.Close()

	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(data); err != nil {
		return false
	}
	return true
}
