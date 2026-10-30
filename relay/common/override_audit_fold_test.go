package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFoldParamOverrideAuditLines(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "collapses same rule across many indices with gaps",
			in: []string{
				"regex_replace messages.1.role from ^developer$ to user",
				"regex_replace messages.2.role from ^developer$ to user",
				"regex_replace messages.3.role from ^developer$ to user",
				"regex_replace messages.4.role from ^developer$ to user",
				"regex_replace messages.5.role from ^developer$ to user",
				"regex_replace messages.6.role from ^developer$ to user",
				"regex_replace messages.7.role from ^developer$ to user",
				"regex_replace messages.8.role from ^developer$ to user",
				"regex_replace messages.9.role from ^developer$ to user",
				"regex_replace messages.11.role from ^developer$ to user",
				"regex_replace messages.15.role from ^developer$ to user",
			},
			want: []string{
				"regex_replace messages.[1-9,11,15].role from ^developer$ to user",
			},
		},
		{
			name: "keeps distinct templates separate and preserves order",
			in: []string{
				"regex_replace system.0.text from ^\\s+|\\s+$ to ",
				"regex_replace messages.0.role from ^developer$ to user",
				"regex_replace messages.1.role from ^developer$ to user",
			},
			want: []string{
				"regex_replace system.0.text from ^\\s+|\\s+$ to ",
				"regex_replace messages.[0-1].role from ^developer$ to user",
			},
		},
		{
			name: "does not fold a diagonal set that has no lossless merge",
			in: []string{
				"set input.0.content.0.text = a",
				"set input.1.content.1.text = a",
			},
			want: []string{
				"set input.0.content.0.text = a",
				"set input.1.content.1.text = a",
			},
		},
		{
			name: "losslessly folds a ragged two-dimensional set",
			in: []string{
				"delete messages.0.content.0.endpoint_signature",
				"delete messages.0.content.1.endpoint_signature",
				"delete messages.0.content.2.endpoint_signature",
				"delete messages.1.content.0.endpoint_signature",
				"delete messages.1.content.1.endpoint_signature",
				"delete messages.2.content.0.endpoint_signature",
				"delete messages.3.content.0.endpoint_signature",
				"delete messages.3.content.1.endpoint_signature",
				"delete messages.4.content.0.endpoint_signature",
				"delete messages.5.content.0.endpoint_signature",
				"delete messages.6.content.0.endpoint_signature",
				"delete messages.7.content.0.endpoint_signature",
				"delete messages.7.content.1.endpoint_signature",
			},
			want: []string{
				"delete messages.0.content.[0-2].endpoint_signature",
				"delete messages.[1,3,7].content.[0-1].endpoint_signature",
				"delete messages.[2,4-6].content.0.endpoint_signature",
			},
		},
		{
			name: "folds the single varying position when another index is constant",
			in: []string{
				"set input.0.content.0.text = a",
				"set input.0.content.1.text = a",
				"set input.0.content.2.text = a",
			},
			want: []string{
				"set input.0.content.[0-2].text = a",
			},
		},
		{
			name: "lines without an index pass through unchanged",
			in: []string{
				"set instructions = new instruction",
				"copy system -> metadata.system_copy",
			},
			want: []string{
				"set instructions = new instruction",
				"copy system -> metadata.system_copy",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, foldParamOverrideAuditLines(tc.in))
		})
	}
}
