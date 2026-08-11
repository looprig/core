package usageexample_test

import (
	"errors"
	"fmt"

	"github.com/looprig/core/content"
)

func Example_usageAccounting() {
	first := content.Usage{
		InputTokens:     80,
		OutputTokens:    20,
		CacheReadTokens: 10,
		ReasoningTokens: 5,
	}
	second := content.Usage{InputTokens: 40, OutputTokens: 10}

	total, err := first.Add(second)
	if err != nil {
		panic(err)
	}
	contextTokens, err := total.ContextTokens()
	if err != nil {
		panic(err)
	}
	totalTokens, err := total.TotalTokens()
	if err != nil {
		panic(err)
	}
	fmt.Println(contextTokens, total.OutputTokens, totalTokens)

	invalid := content.Usage{OutputTokens: 2, ReasoningTokens: 3}
	var validationErr *content.UsageValidationError
	fmt.Println(errors.As(invalid.Validate(), &validationErr), validationErr.Field)

	// Output:
	// 130 30 160
	// true ReasoningTokens
}
