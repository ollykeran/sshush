package style

import (
	"bytes"
	"testing"
)

func TestOutput_PrintTo_skip_empty(t *testing.T) {
	o := NewOutput()
	var buf bytes.Buffer
	o.PrintTo(&buf)
	if buf.Len() != 0 {
		t.Fatalf("PrintTo on empty Output: want no bytes, got %q", buf.String())
	}
}
