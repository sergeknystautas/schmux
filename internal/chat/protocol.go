package chat

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sergeknystautas/schmux/internal/detect"
)

// Protocol names, as written in descriptor chat.protocol and persisted on
// state.Session.ChatProtocol.
const (
	ProtocolClaude = "claude-stream-json"
	ProtocolCodex  = "codex-app-server"
)

// ErrNotAddressable is returned by an encoder when the harness cannot take
// the line yet (Codex before its thread id and account check). The runtime
// holds the user_message record and encodes it once Addressable is true.
var ErrNotAddressable = errors.New("chat: harness not addressable yet")

// LaunchOpts is what Launch needs to build argv and the handshake.
type LaunchOpts struct {
	Adapter    detect.ToolAdapter // model flag, auto-approve args
	ModelValue string             // resolved runner model value, "" when none
	ResumeID   string
	Fenced     bool
	Cwd        string // workspace path
}

// Protocol is what differs between harnesses behind the same bridge, record,
// socket, and page: launch, encode, observe. Nothing here decides what the
// page renders; the page's reducer for the same protocol does that from the
// verbatim record. Implementations are not concurrency-safe; the Runtime
// serializes every call under its mutex.
type Protocol interface {
	Name() string

	// Launch returns argv appended after the descriptor's chat base args and
	// the lines written to the input file before the harness starts.
	Launch(o LaunchOpts) (argv []string, handshake [][]byte)

	// LiveOnly reports an output line that is fanned out but never recorded
	// because a later durable line carries the complete content.
	LiveOnly(line []byte) bool

	// ResumeID returns the harness conversation id carried by an output line, or "".
	ResumeID(line []byte) string

	// Observe updates addressing state from one recorded output line.
	Observe(line []byte)

	// Rebuild restores addressing state from the bridge files and the record,
	// and returns the user_message records whose input line was never written
	// (those after the last session record with no matching line in the input
	// file). Called once at Runtime.Start.
	Rebuild(p Paths, records []Record) (unsent []Record, err error)

	// Addressable reports whether the encoders can produce a line now. Claude
	// always can; Codex can once its thread id and account check are in.
	Addressable() bool

	// Encoders: the line to append to the input file for each user action.
	// UserMessage returns (nil, ErrNotAddressable) while !Addressable(); no
	// request id is allocated for a line that is not written.
	UserMessage(id, text string, images []Image) ([]byte, error)
	Interrupt() ([]byte, error)
	Permission(requestID string, allow bool, updatedInput json.RawMessage, message string) ([]byte, error)
	Answer(requestID string, answers map[string][]string, input json.RawMessage) ([]byte, error)
}

// ProtocolFor returns a fresh Protocol for a descriptor chat.protocol name.
// Each runtime gets its own instance; the Codex one carries addressing state.
func ProtocolFor(name string) (Protocol, error) {
	switch name {
	case ProtocolClaude:
		return claudeProtocol{}, nil
	case ProtocolCodex:
		return newCodexProtocol(), nil
	default:
		return nil, fmt.Errorf("chat: unknown protocol %q", name)
	}
}
