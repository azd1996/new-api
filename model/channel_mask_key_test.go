package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMaskKey locks the display-masking contract: the full secret is never
// returned, only the first maskKeyRevealPrefix and last maskKeyRevealSuffix
// characters survive, and keys short enough to otherwise leak are fully masked.
func TestMaskKey(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "short fully masked", in: "sk-123", want: "******"},
		{name: "exactly prefix+suffix fully masked", in: "ABCDEFGHIJKLMNOP", want: "****************"},
		{name: "one over keeps single star", in: "ABCDEFGHIJKLMNOPQ", want: "ABCDEFGHIJ*LMNOPQ"},
		{name: "long key head and tail", in: "ABCDEFGHIJ1234567890KLMNOP", want: "ABCDEFGHIJ**********KLMNOP"},
		{
			name: "multi key masked per line",
			in:   "ABCDEFGHIJ1234567890KLMNOP\nshort",
			want: "ABCDEFGHIJ**********KLMNOP\n*****",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, MaskKey(tc.in))
		})
	}
}
