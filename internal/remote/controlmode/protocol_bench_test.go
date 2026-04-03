package controlmode

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

// BenchmarkUnescapeOutput measures pure string processing for %output line
// decoding. Simulates a typical 64-byte terminal line with mixed octal escapes.
func BenchmarkUnescapeOutput(b *testing.B) {
	// Mix of plain text and octal escapes: "Hello\033[31mWorld\033[0m\012"
	input := `Hello\033[31mWorld\033[0m\012more text here and padding`
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for b.Loop() {
		UnescapeOutput(input)
	}
}

// BenchmarkUnescapeOutput_Clean measures the fast path when no escapes are
// present (pure ASCII text).
func BenchmarkUnescapeOutput_Clean(b *testing.B) {
	input := "The quick brown fox jumps over the lazy dog. No escapes here!"
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for b.Loop() {
		UnescapeOutput(input)
	}
}

// BenchmarkParserThroughput feeds synthetic %output / %begin / %end lines
// through a Parser and measures events/sec.
func BenchmarkParserThroughput(b *testing.B) {
	// Build a block of protocol lines: alternating %output and %begin/%end
	var buf strings.Builder
	for i := 0; i < b.N; i++ {
		// One %output event
		fmt.Fprintf(&buf, "%%output %%0 line %d of output text padding\n", i)
		// One command response
		fmt.Fprintf(&buf, "%%begin %d %d 1\n", 1000+i, i)
		fmt.Fprintf(&buf, "response content %d\n", i)
		fmt.Fprintf(&buf, "%%end %d %d 1\n", 1000+i, i)
	}

	input := buf.String()
	b.SetBytes(int64(len(input)))
	b.ResetTimer()

	r := strings.NewReader(input)
	p := NewParser(r, nil)

	// Drain channels in background
	done := make(chan struct{})
	go func() {
		for range p.Output() {
		}
		close(done)
	}()
	go func() {
		for range p.Responses() {
		}
	}()
	go func() {
		for range p.Events() {
		}
	}()

	p.Run()
	<-done
}

// BenchmarkClassifyKeyRuns_ASCII measures classification of pure printable
// ASCII text (single literal run, no special keys).
func BenchmarkClassifyKeyRuns_ASCII(b *testing.B) {
	input := "The quick brown fox jumps over the lazy dog"
	var dst []KeyRun
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for b.Loop() {
		dst = ClassifyKeyRuns(dst, input)
	}
}

// BenchmarkClassifyKeyRuns_Mixed measures classification of mixed input:
// printable text interleaved with Enter, arrow keys, and Tab.
func BenchmarkClassifyKeyRuns_Mixed(b *testing.B) {
	// Simulate: "ls -la" + Enter + Up + Down + Tab + "cd foo" + Enter
	input := "ls -la\r\x1b[A\x1b[B\tcd foo\r"
	var dst []KeyRun
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for b.Loop() {
		dst = ClassifyKeyRuns(dst, input)
	}
}

// BenchmarkExecuteRoundtrip measures the Go-side overhead of Execute() with a
// mocked tmux via io.Pipe. This isolates mutex, FIFO queue, channel creation,
// and goroutine scheduling costs from actual tmux processing.
func BenchmarkExecuteRoundtrip(b *testing.B) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	parser := NewParser(stdoutR, nil)
	client := NewClient(stdinW, parser, nil)

	// Mock tmux: send an initial notification to signal control mode ready,
	// then read commands from stdin and immediately write %begin/%end.
	go func() {
		defer stdoutW.Close()
		// Bootstrap: emit a notification so ControlModeReady fires
		fmt.Fprintf(stdoutW, "%%session-changed $0 default\n")

		cmdID := 0
		buf := make([]byte, 4096)
		for {
			n, err := stdinR.Read(buf)
			if err != nil {
				return
			}
			for _, c := range buf[:n] {
				if c == '\n' {
					epoch := 1000 + cmdID
					fmt.Fprintf(stdoutW, "%%begin %d %d 1\n", epoch, cmdID)
					fmt.Fprintf(stdoutW, "%%end %d %d 1\n", epoch, cmdID)
					cmdID++
				}
			}
		}
	}()

	// Start the parser and client
	go parser.Run()
	client.Start()

	// Wait for control mode ready (triggered by the bootstrap notification)
	<-parser.ControlModeReady()

	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		_, _, _ = client.Execute(ctx, "list-sessions")
	}
	b.StopTimer()

	client.Close()
	stdinW.Close()
}
