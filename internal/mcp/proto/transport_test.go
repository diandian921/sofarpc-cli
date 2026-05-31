package proto

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadLineLimitedRejectsAndResyncs(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", 12)+"\n{}\n"), 4)
	if _, err := readLineLimited(reader, 8); err != ErrFrameTooLong {
		t.Fatalf("first read err = %v, want ErrFrameTooLong", err)
	}
	line, err := readLineLimited(reader, 8)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if string(line) != "{}\n" {
		t.Fatalf("second line = %q", line)
	}
}
