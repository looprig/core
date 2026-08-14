package content

import (
	"errors"
	"testing"
)

const maxTokenCount TokenCount = ^TokenCount(0)

func TestUsageValidateCompatibility(t *testing.T) {
	if err := (Usage{OutputTokens: 2, ReasoningTokens: 1}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	err := (Usage{OutputTokens: 1, ReasoningTokens: 2}).Validate()
	var validationErr *UsageValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("Validate() error = %T %v, want *UsageValidationError", err, err)
	}
}

// TestUsageReasoningWithinOutput covers the documented subset convention as
// what it is: an observable property of reported counts, not a gate. It used to
// be Usage.Validate, whose error discarded whatever generation carried the
// counts — see the ReasoningTokens field comment for why that was wrong.
func TestUsageReasoningWithinOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		usage Usage
		want  bool
	}{
		{name: "zero value", usage: Usage{}, want: true},
		{
			name: "present all-zero domain value",
			usage: Usage{
				InputTokens:         0,
				OutputTokens:        0,
				CacheReadTokens:     0,
				CacheCreationTokens: 0,
				ReasoningTokens:     0,
			},
			want: true,
		},
		{
			name:  "reasoning below output",
			usage: Usage{OutputTokens: 7, ReasoningTokens: 3},
			want:  true,
		},
		{
			name:  "reasoning equals output boundary",
			usage: Usage{OutputTokens: 7, ReasoningTokens: 7},
			want:  true,
		},
		{
			name:  "reasoning exceeds output",
			usage: Usage{OutputTokens: 7, ReasoningTokens: 8},
			want:  false,
		},
		{
			// The live counts from an OpenRouter HTTP 200 against
			// nvidia/nemotron-3-ultra-550b-a55b:free.
			name:  "reported provider divergence is observable, never fatal",
			usage: Usage{InputTokens: 31, OutputTokens: 216, ReasoningTokens: 226},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.usage.ReasoningWithinOutput(); got != tt.want {
				t.Errorf("Usage.ReasoningWithinOutput() = %v, want %v", got, tt.want)
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
		wantField UsageField
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
			name: "exact maximum succeeds",
			usage: Usage{
				InputTokens:         maxTokenCount - 2,
				CacheReadTokens:     1,
				CacheCreationTokens: 1,
			},
			want: maxTokenCount,
		},
		{
			name: "uncached plus cache read overflow",
			usage: Usage{
				InputTokens:     maxTokenCount,
				CacheReadTokens: 1,
			},
			wantField: UsageFieldContextTokens,
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
			wantField: UsageFieldContextTokens,
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
		wantField UsageField
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
			name: "exact maximum succeeds",
			usage: Usage{
				InputTokens:  maxTokenCount - 1,
				OutputTokens: 1,
			},
			want: maxTokenCount,
		},
		{
			name: "context overflow is preserved",
			usage: Usage{
				InputTokens:     maxTokenCount,
				CacheReadTokens: 1,
			},
			wantField: UsageFieldContextTokens,
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
			wantField: UsageFieldTotalTokens,
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
		name              string
		left              Usage
		right             Usage
		want              Usage
		wantOverflowField UsageField
		wantErr           bool
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
		{
			name: "exact maximum succeeds",
			left: Usage{
				InputTokens:         maxTokenCount - 1,
				OutputTokens:        maxTokenCount - 5,
				CacheReadTokens:     maxTokenCount - 2,
				CacheCreationTokens: maxTokenCount - 3,
				ReasoningTokens:     maxTokenCount - 5,
			},
			right: Usage{
				InputTokens:         1,
				OutputTokens:        5,
				CacheReadTokens:     2,
				CacheCreationTokens: 3,
				ReasoningTokens:     5,
			},
			want: Usage{
				InputTokens:         maxTokenCount,
				OutputTokens:        maxTokenCount,
				CacheReadTokens:     maxTokenCount,
				CacheCreationTokens: maxTokenCount,
				ReasoningTokens:     maxTokenCount,
			},
		},
		{name: "right additive identity", left: value, right: zero, want: value},
		{name: "left additive identity", left: zero, right: value, want: value},
		{
			// Add folds one turn's counts into a running total. Refusing an
			// operand whose reasoning exceeds its output made a single divergent
			// provider report poison every subsequent aggregate for the session,
			// so the sum is now taken field by field and the divergence travels
			// with it where ReasoningWithinOutput can still see it.
			name:  "operand diverging from the reasoning convention still sums",
			left:  Usage{OutputTokens: 216, ReasoningTokens: 226},
			right: Usage{OutputTokens: 10, ReasoningTokens: 4},
			want:  Usage{OutputTokens: 226, ReasoningTokens: 230},
		},
		{
			name:              "input overflow",
			left:              Usage{InputTokens: maxTokenCount},
			right:             Usage{InputTokens: 1},
			wantOverflowField: UsageFieldInputTokens,
			wantErr:           true,
		},
		{
			name:              "output overflow",
			left:              Usage{OutputTokens: maxTokenCount, ReasoningTokens: 1},
			right:             Usage{OutputTokens: 1},
			wantOverflowField: UsageFieldOutputTokens,
			wantErr:           true,
		},
		{
			name:              "cache read overflow",
			left:              Usage{CacheReadTokens: maxTokenCount},
			right:             Usage{CacheReadTokens: 1},
			wantOverflowField: UsageFieldCacheReadTokens,
			wantErr:           true,
		},
		{
			name:              "cache creation overflow",
			left:              Usage{CacheCreationTokens: maxTokenCount},
			right:             Usage{CacheCreationTokens: 1},
			wantOverflowField: UsageFieldCacheCreationTokens,
			wantErr:           true,
		},
		{
			name:              "reasoning overflow",
			left:              Usage{OutputTokens: maxTokenCount, ReasoningTokens: maxTokenCount},
			right:             Usage{OutputTokens: 1, ReasoningTokens: 1},
			wantOverflowField: UsageFieldReasoningTokens,
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
	wantField UsageField,
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
