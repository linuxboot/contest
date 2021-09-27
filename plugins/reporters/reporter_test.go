package reporter

import (
	"fmt"
	"testing"
)

func TestNew(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"Noop", nil},
		{"Target", nil},
		{"barget", fmt.Errorf("No reporter for 'barget'")},
	} {
		_, err := New(tt.name)
		// If they are both nil or both non-nil we are fine.
		if err == tt.err || (err != nil && tt.err != nil) {
			continue
		}
		t.Errorf("Getting %v: got %v, want %v", tt.name, err, tt.err)
	}
}
