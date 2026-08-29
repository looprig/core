package v1

import (
	"errors"
	"testing"
)

func TestDeterministicCommandIDFrameUint32Limits(t *testing.T) {
	tests := []struct {
		name    string
		value   uint64
		code    CommandIDFramingCode
		want    uint32
		wantErr bool
	}{
		{
			name:  "maximum field count",
			value: maxUint32,
			code:  CommandIDFramingCodeTooManyFields,
			want:  uint32(maxUint32),
		},
		{
			name:    "field count overflow",
			value:   maxUint32 + 1,
			code:    CommandIDFramingCodeTooManyFields,
			wantErr: true,
		},
		{
			name:  "maximum field length",
			value: maxUint32,
			code:  CommandIDFramingCodeFieldTooLong,
			want:  uint32(maxUint32),
		},
		{
			name:    "field length overflow",
			value:   maxUint32 + 1,
			code:    CommandIDFramingCodeFieldTooLong,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := commandIDFrameUint32(tt.value, tt.code)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("commandIDFrameUint32(%d, %q) error = %v", tt.value, tt.code, err)
				}
				if got != tt.want {
					t.Errorf("commandIDFrameUint32(%d, %q) = %d, want %d", tt.value, tt.code, got, tt.want)
				}
				return
			}

			var framingErr *CommandIDFramingError
			if !errors.As(err, &framingErr) {
				t.Fatalf("commandIDFrameUint32(%d, %q) error = %T, want *CommandIDFramingError", tt.value, tt.code, err)
			}
			if framingErr.Code != tt.code {
				t.Errorf("commandIDFrameUint32(%d, %q) code = %q, want %q", tt.value, tt.code, framingErr.Code, tt.code)
			}
		})
	}
}
