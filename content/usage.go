package content

import "strconv"

const (
	usageFieldInputTokens         string     = "InputTokens"
	usageFieldOutputTokens        string     = "OutputTokens"
	usageFieldCacheReadTokens     string     = "CacheReadTokens"
	usageFieldCacheCreationTokens string     = "CacheCreationTokens"
	usageFieldReasoningTokens     string     = "ReasoningTokens"
	usageFieldContextTokens       string     = "ContextTokens"
	usageFieldTotalTokens         string     = "TotalTokens"
	usageReasonExceedsOutput      string     = "exceeds OutputTokens"
	maximumTokenCount             TokenCount = ^TokenCount(0)
)

// TokenCount is a normalized count of model tokens.
type TokenCount uint64

// Usage is normalized model token usage.
type Usage struct {
	InputTokens         TokenCount
	OutputTokens        TokenCount
	CacheReadTokens     TokenCount
	CacheCreationTokens TokenCount
	ReasoningTokens     TokenCount
}

// UsageValidationError reports an invalid relationship between usage fields.
type UsageValidationError struct {
	Field  string
	Reason string
}

func (e *UsageValidationError) Error() string {
	return "content: invalid usage field " + e.Field + ": " + e.Reason
}

// UsageOverflowError reports a token-count addition that cannot be represented.
type UsageOverflowError struct {
	Field string
	Left  TokenCount
	Right TokenCount
}

func (e *UsageOverflowError) Error() string {
	return "content: usage field " + e.Field + " overflow: " +
		strconv.FormatUint(uint64(e.Left), 10) + " + " +
		strconv.FormatUint(uint64(e.Right), 10)
}

// Validate verifies relationships between usage fields.
func (u Usage) Validate() error {
	if u.ReasoningTokens > u.OutputTokens {
		return &UsageValidationError{
			Field:  usageFieldReasoningTokens,
			Reason: usageReasonExceedsOutput,
		}
	}
	return nil
}

// ContextTokens returns all input tokens that occupy model context.
func (u Usage) ContextTokens() (TokenCount, error) {
	input, err := addTokenCounts(usageFieldContextTokens, u.InputTokens, u.CacheReadTokens)
	if err != nil {
		return 0, err
	}
	return addTokenCounts(usageFieldContextTokens, input, u.CacheCreationTokens)
}

// TotalTokens returns context plus output tokens.
func (u Usage) TotalTokens() (TokenCount, error) {
	contextTokens, err := u.ContextTokens()
	if err != nil {
		return 0, err
	}
	return addTokenCounts(usageFieldTotalTokens, contextTokens, u.OutputTokens)
}

// Add validates and combines two usage values field by field.
func (u Usage) Add(other Usage) (Usage, error) {
	if err := u.Validate(); err != nil {
		return Usage{}, err
	}
	if err := other.Validate(); err != nil {
		return Usage{}, err
	}

	var sum Usage
	fields := []struct {
		name  string
		left  TokenCount
		right TokenCount
		set   func(TokenCount)
	}{
		{name: usageFieldReasoningTokens, left: u.ReasoningTokens, right: other.ReasoningTokens, set: func(value TokenCount) { sum.ReasoningTokens = value }},
		{name: usageFieldInputTokens, left: u.InputTokens, right: other.InputTokens, set: func(value TokenCount) { sum.InputTokens = value }},
		{name: usageFieldOutputTokens, left: u.OutputTokens, right: other.OutputTokens, set: func(value TokenCount) { sum.OutputTokens = value }},
		{name: usageFieldCacheReadTokens, left: u.CacheReadTokens, right: other.CacheReadTokens, set: func(value TokenCount) { sum.CacheReadTokens = value }},
		{name: usageFieldCacheCreationTokens, left: u.CacheCreationTokens, right: other.CacheCreationTokens, set: func(value TokenCount) { sum.CacheCreationTokens = value }},
	}
	for _, field := range fields {
		value, err := addTokenCounts(field.name, field.left, field.right)
		if err != nil {
			return Usage{}, err
		}
		field.set(value)
	}
	return sum, nil
}

func addTokenCounts(field string, left TokenCount, right TokenCount) (TokenCount, error) {
	if right > maximumTokenCount-left {
		return 0, &UsageOverflowError{Field: field, Left: left, Right: right}
	}
	return left + right, nil
}
