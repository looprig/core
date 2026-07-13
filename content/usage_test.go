package content

import (
	"errors"
	"testing"
)

const maxTokenCount TokenCount = ^TokenCount(0)

func TestUsageValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		usage     Usage
		wantField string
		wantErr   bool
	}{
		{name: "zero value", usage: Usage{}},
		{
			name: "present all-zero domain value",
			usage: Usage{
				InputTokens:         0,
				OutputTokens:        0,
				CacheReadTokens:     0,
				CacheCreationTokens: 0,
				ReasoningTokens:     0,
			},
		},
		{
			name: "reasoning equals output boundary",
			usage: Usage{
				OutputTokens:    7,
				ReasoningTokens: 7,
			},
		},
		{
			name: "reasoning exceeds output",
			usage: Usage{
				OutputTokens:    7,
				ReasoningTokens: 8,
			},
			wantField: "ReasoningTokens",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.usage.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Usage.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				return
			}

			var validationErr *UsageValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("Usage.Validate() error type = %T, want *UsageValidationError", err)
			}
			if validationErr.Field != tt.wantField {
				t.Errorf("UsageValidationError.Field = %q, want %q", validationErr.Field, tt.wantField)
			}
			if validationErr.Reason == "" {
				t.Error("UsageValidationError.Reason is empty")
			}
		})
	}
}

func TestUsageContextTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		usage     Usage
		want      TokenCount
		wantField string
		wantLeft  TokenCount
		wantRight TokenCount
		wantErr   bool
	}{
		{name: "zero", usage: Usage{}, want: 0},
		{
			name: "cache only",
			usage: Usage{
				CacheReadTokens:     11,
				CacheCreationTokens: 13,
			},
			want: 24,
		},
		{
			name: "all input categories",
			usage: Usage{
				InputTokens:         5,
				CacheReadTokens:     7,
				CacheCreationTokens: 9,
			},
			want: 21,
		},
		{
			name: "uncached plus cache read overflow",
			usage: Usage{
				InputTokens:     maxTokenCount,
				CacheReadTokens: 1,
			},
			wantField: "ContextTokens",
			wantLeft:  maxTokenCount,
			wantRight: 1,
			wantErr:   true,
		},
		{
			name: "partial input plus cache creation overflow",
			usage: Usage{
				InputTokens:         maxTokenCount - 1,
				CacheReadTokens:     1,
				CacheCreationTokens: 1,
			},
			wantField: "ContextTokens",
			wantLeft:  maxTokenCount,
			wantRight: 1,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.usage.ContextTokens()
			assertUsageCountResult(t, got, err, tt.want, tt.wantField, tt.wantLeft, tt.wantRight, tt.wantErr)
		})
	}
}

func TestUsageTotalTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		usage     Usage
		want      TokenCount
		wantField string
		wantLeft  TokenCount
		wantRight TokenCount
		wantErr   bool
	}{
		{name: "zero", usage: Usage{}, want: 0},
		{
			name: "input and output",
			usage: Usage{
				InputTokens:  17,
				OutputTokens: 19,
			},
			want: 36,
		},
		{
			name: "context overflow is preserved",
			usage: Usage{
				InputTokens:     maxTokenCount,
				CacheReadTokens: 1,
			},
			wantField: "ContextTokens",
			wantLeft:  maxTokenCount,
			wantRight: 1,
			wantErr:   true,
		},
		{
			name: "context plus output overflow",
			usage: Usage{
				InputTokens:  maxTokenCount,
				OutputTokens: 1,
			},
			wantField: "TotalTokens",
			wantLeft:  maxTokenCount,
			wantRight: 1,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.usage.TotalTokens()
			assertUsageCountResult(t, got, err, tt.want, tt.wantField, tt.wantLeft, tt.wantRight, tt.wantErr)
		})
	}
}

func TestUsageAdd(t *testing.T) {
	t.Parallel()

	zero := Usage{}
	value := Usage{
		InputTokens:         2,
		OutputTokens:        5,
		CacheReadTokens:     7,
		CacheCreationTokens: 11,
		ReasoningTokens:     3,
	}

	tests := []struct {
		name                string
		left                Usage
		right               Usage
		want                Usage
		wantValidationField string
		wantOverflowField   string
		wantErr             bool
	}{
		{
			name: "field-wise sum",
			left: value,
			right: Usage{
				InputTokens:         13,
				OutputTokens:        17,
				CacheReadTokens:     19,
				CacheCreationTokens: 23,
				ReasoningTokens:     11,
			},
			want: Usage{
				InputTokens:         15,
				OutputTokens:        22,
				CacheReadTokens:     26,
				CacheCreationTokens: 34,
				ReasoningTokens:     14,
			},
		},
		{name: "right additive identity", left: value, right: zero, want: value},
		{name: "left additive identity", left: zero, right: value, want: value},
		{
			name:                "invalid left operand",
			left:                Usage{ReasoningTokens: 1},
			right:               zero,
			wantValidationField: "ReasoningTokens",
			wantErr:             true,
		},
		{
			name:                "invalid right operand",
			left:                zero,
			right:               Usage{ReasoningTokens: 1},
			wantValidationField: "ReasoningTokens",
			wantErr:             true,
		},
		{
			name:              "input overflow",
			left:              Usage{InputTokens: maxTokenCount},
			right:             Usage{InputTokens: 1},
			wantOverflowField: "InputTokens",
			wantErr:           true,
		},
		{
			name:              "output overflow",
			left:              Usage{OutputTokens: maxTokenCount, ReasoningTokens: 1},
			right:             Usage{OutputTokens: 1},
			wantOverflowField: "OutputTokens",
			wantErr:           true,
		},
		{
			name:              "cache read overflow",
			left:              Usage{CacheReadTokens: maxTokenCount},
			right:             Usage{CacheReadTokens: 1},
			wantOverflowField: "CacheReadTokens",
			wantErr:           true,
		},
		{
			name:              "cache creation overflow",
			left:              Usage{CacheCreationTokens: maxTokenCount},
			right:             Usage{CacheCreationTokens: 1},
			wantOverflowField: "CacheCreationTokens",
			wantErr:           true,
		},
		{
			name:              "reasoning overflow",
			left:              Usage{OutputTokens: maxTokenCount, ReasoningTokens: maxTokenCount},
			right:             Usage{OutputTokens: 1, ReasoningTokens: 1},
			wantOverflowField: "ReasoningTokens",
			wantErr:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.left.Add(tt.right)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Usage.Add() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if got != tt.want {
					t.Errorf("Usage.Add() = %+v, want %+v", got, tt.want)
				}
				return
			}
			if got != zero {
				t.Errorf("Usage.Add() result on error = %+v, want zero value", got)
			}

			if tt.wantValidationField != "" {
				var validationErr *UsageValidationError
				if !errors.As(err, &validationErr) {
					t.Fatalf("Usage.Add() error type = %T, want *UsageValidationError", err)
				}
				if validationErr.Field != tt.wantValidationField {
					t.Errorf("UsageValidationError.Field = %q, want %q", validationErr.Field, tt.wantValidationField)
				}
				return
			}

			var overflowErr *UsageOverflowError
			if !errors.As(err, &overflowErr) {
				t.Fatalf("Usage.Add() error type = %T, want *UsageOverflowError", err)
			}
			if overflowErr.Field != tt.wantOverflowField {
				t.Errorf("UsageOverflowError.Field = %q, want %q", overflowErr.Field, tt.wantOverflowField)
			}
			if overflowErr.Left != maxTokenCount || overflowErr.Right != 1 {
				t.Errorf("UsageOverflowError operands = (%d, %d), want (%d, 1)", overflowErr.Left, overflowErr.Right, maxTokenCount)
			}
		})
	}
}

func assertUsageCountResult(
	t *testing.T,
	got TokenCount,
	err error,
	want TokenCount,
	wantField string,
	wantLeft TokenCount,
	wantRight TokenCount,
	wantErr bool,
) {
	t.Helper()

	if (err != nil) != wantErr {
		t.Fatalf("token count error = %v, wantErr %v", err, wantErr)
	}
	if !wantErr {
		if got != want {
			t.Errorf("token count = %d, want %d", got, want)
		}
		return
	}
	if got != 0 {
		t.Errorf("token count on error = %d, want 0", got)
	}

	var overflowErr *UsageOverflowError
	if !errors.As(err, &overflowErr) {
		t.Fatalf("token count error type = %T, want *UsageOverflowError", err)
	}
	if overflowErr.Field != wantField {
		t.Errorf("UsageOverflowError.Field = %q, want %q", overflowErr.Field, wantField)
	}
	if overflowErr.Left != wantLeft || overflowErr.Right != wantRight {
		t.Errorf("UsageOverflowError operands = (%d, %d), want (%d, %d)", overflowErr.Left, overflowErr.Right, wantLeft, wantRight)
	}
}
