package chat

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestClaude_UserMessageGateCommitsAfterDispatch(t *testing.T) {
	p, err := ProtocolFor(ProtocolClaude)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Addressable() {
		t.Fatal("a fresh Claude protocol must be addressable")
	}

	first, err := p.UserMessage("u1", "first", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 {
		t.Fatal("first user message must encode")
	}
	if !p.Addressable() {
		t.Fatal("encoding must not commit active state before the input write")
	}

	p.CommitUserMessage("u1")
	if p.Addressable() {
		t.Fatal("a committed Claude dispatch user must make the protocol active")
	}

	second, err := p.UserMessage("u2", "second", nil)
	if !errors.Is(err, ErrNotAddressable) || second != nil {
		t.Fatalf("active turn must hold message: line=%q err=%v", second, err)
	}

	p.Observe([]byte(`{"type":"result","subtype":"success","queued_turn_count":0}`))
	if !p.Addressable() {
		t.Fatal("terminal result must release the next held message")
	}
}

func TestClaude_TerminalResultReleasesRegardlessOfQueueCount(t *testing.T) {
	p, err := ProtocolFor(ProtocolClaude)
	if err != nil {
		t.Fatal(err)
	}
	p.CommitUserMessage("u1")
	if p.Addressable() {
		t.Fatal("committed dispatch must make the protocol active")
	}
	p.Observe([]byte(`{"type":"result","subtype":"success","queued_turn_count":0}`))
	if !p.Addressable() {
		t.Fatal("terminal result must release the gate")
	}

	nonzero, err := ProtocolFor(ProtocolClaude)
	if err != nil {
		t.Fatal(err)
	}
	nonzero.CommitUserMessage("u1")
	nonzero.Observe([]byte(`{"type":"result","subtype":"success","queued_turn_count":1}`))
	if !nonzero.Addressable() {
		t.Fatal("queued_turn_count must not keep a terminal result closed")
	}
}

func TestClaude_RebuildKeepsDispatchedWorkActive(t *testing.T) {
	dir := t.TempDir()
	paths := PathsFor(dir)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}

	a := NewUserMessage("A", nil)
	b := NewUserMessage("B", nil)
	c := NewUserMessage("C", nil)
	aLine, _ := UserMessageLine("A", nil)
	bLine, _ := UserMessageLine("B", nil)
	inputBytes := append(append([]byte{}, aLine...), '\n')
	inputBytes = append(inputBytes, bLine...)
	inputBytes = append(inputBytes, '\n')
	os.WriteFile(paths.Input, inputBytes, 0o644)
	os.WriteFile(paths.Output, []byte(`{"type":"result","subtype":"success","queued_turn_count":0}`+"\n"), 0o644)

	records := []Record{
		a,
		NewUserMessageDispatch(a.ID),
		NewHarness([]byte(`{"type":"result","subtype":"success","queued_turn_count":0}`)),
		c,
		b,
		NewUserMessageDispatch(b.ID),
	}
	p := newClaudeProtocol()
	unsent, err := p.Rebuild(paths, records)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsent) != 1 || unsent[0].ID != c.ID {
		t.Fatalf("unsent = %+v, want only C", unsent)
	}
	if p.Addressable() {
		t.Fatal("confirmed B dispatch without a later result must remain active")
	}
}

func TestClaude_RebuildMatchesUnrecordedLaterResult(t *testing.T) {
	dir := t.TempDir()
	paths := PathsFor(dir)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}

	a := NewUserMessage("A", nil)
	b := NewUserMessage("B", nil)
	aLine, _ := UserMessageLine("A", nil)
	bLine, _ := UserMessageLine("B", nil)
	inputBytes := append(append([]byte{}, aLine...), '\n')
	inputBytes = append(inputBytes, bLine...)
	inputBytes = append(inputBytes, '\n')
	os.WriteFile(paths.Input, inputBytes, 0o644)
	os.WriteFile(paths.Output, []byte(
		`{"type":"result","subtype":"success","queued_turn_count":0}`+"\n"+
			`{"type":"result","subtype":"success","queued_turn_count":0}`+"\n"), 0o644)

	records := []Record{
		a,
		NewUserMessageDispatch(a.ID),
		b,
		NewUserMessageDispatch(b.ID),
	}
	p := newClaudeProtocol()
	unsent, err := p.Rebuild(paths, records)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsent) != 0 {
		t.Fatalf("unrecorded later result must finish idle: %+v", unsent)
	}
	if !p.Addressable() {
		t.Fatal("must finish idle when an unrecorded terminal result is replayed")
	}
}

func TestClaude_RebuildReturnsUndeliveredMarkerAsUnsent(t *testing.T) {
	dir := t.TempDir()
	paths := PathsFor(dir)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}

	b := NewUserMessage("B", nil)
	os.WriteFile(paths.Input, nil, 0o644)
	os.WriteFile(paths.Output, nil, 0o644)

	records := []Record{
		b,
		NewUserMessageDispatch(b.ID),
	}
	p := newClaudeProtocol()
	unsent, err := p.Rebuild(paths, records)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsent) != 1 || unsent[0].ID != b.ID {
		t.Fatalf("undelivered B must be returned as unsent: %+v", unsent)
	}
	if !p.Addressable() {
		t.Fatal("no terminal result means still addressable")
	}
}

func TestClaude_RebuildDistinguishesIdenticalTextByOrder(t *testing.T) {
	dir := t.TempDir()
	paths := PathsFor(dir)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}

	a := NewUserMessage("same", nil)
	b := NewUserMessage("same", nil)
	aLine, _ := UserMessageLine("same", nil)
	bLine, _ := UserMessageLine("same", nil)
	inputBytes := append(append([]byte{}, aLine...), '\n')
	inputBytes = append(inputBytes, bLine...)
	inputBytes = append(inputBytes, '\n')
	os.WriteFile(paths.Input, inputBytes, 0o644)
	os.WriteFile(paths.Output, []byte(`{"type":"result","subtype":"success"}`+"\n"), 0o644)

	records := []Record{
		a,
		NewUserMessageDispatch(a.ID),
		b,
		NewUserMessageDispatch(b.ID),
	}
	p := newClaudeProtocol()
	unsent, err := p.Rebuild(paths, records)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsent) != 0 {
		t.Fatalf("both identical messages dispatched: %+v", unsent)
	}

	// Remove the second input line; rebuild must report only B as undelivered.
	os.WriteFile(paths.Input, append(append([]byte{}, aLine...), '\n'), 0o644)
	unsent, err = p.Rebuild(paths, records)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsent) != 1 || unsent[0].ID != b.ID {
		t.Fatalf("only B must be unsent after dropping second line: %+v", unsent)
	}
}

func TestClaude_RebuildDoesNotResendOrReinterpretLegacyQueue(t *testing.T) {
	dir := t.TempDir()
	paths := PathsFor(dir)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}

	texts := []string{"one", "two", "three", "four"}
	var records []Record
	var input strings.Builder
	for _, text := range texts {
		rec := NewUserMessage(text, nil)
		records = append(records, rec)
		line, _ := UserMessageLine(text, nil)
		input.Write(line)
		input.WriteByte('\n')
	}
	os.WriteFile(paths.Input, []byte(input.String()), 0o644)
	os.WriteFile(paths.Output, []byte(
		`{"type":"result","subtype":"success","queued_turn_count":1}`+"\n"+
			`{"type":"result","subtype":"success","queued_turn_count":0}`+"\n"), 0o644)

	p := newClaudeProtocol()
	unsent, err := p.Rebuild(paths, records)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsent) != 0 {
		t.Fatalf("legacy inputs must not be resent: %+v", unsent)
	}
	if !p.Addressable() {
		t.Fatal("drained legacy queue must finish idle")
	}
}
