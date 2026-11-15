package runner

import (
	"io"
	"testing"
)

func TestLineWriterCarriageReturn(t *testing.T) {
	var got []Line
	w := &lineWriter{emit: func(ev any) { got = append(got, ev.(Line)) }, log: io.Discard}
	_, _ = w.Write([]byte("a\n10%\r50%\r100%\r\nb\x1b[31mred\x1b[0m\npartial"))
	w.Flush()
	want := []Line{{Text: "a"}, {Text: "10%"}, {Text: "50%", Replace: true}, {Text: "100%", Replace: true}, {Text: "bred"}, {Text: "partial"}}
	if len(got) != len(want) {
		t.Fatalf("got %d lines %+v, want %+v", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
