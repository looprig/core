package usageexample_test

import (
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

	// ReasoningTokens is documented as a subset of OutputTokens, and a provider
	// can still report otherwise. That is a fact about the report, not a reason
	// to reject it: the counts stay exactly as received and the divergence is
	// observable.
	divergent := content.Usage{OutputTokens: 216, ReasoningTokens: 226}
	fmt.Println(divergent.ReasoningWithinOutput(), divergent.OutputTokens, divergent.ReasoningTokens)

	// Output:
	// 130 30 160
	// false 216 226
}
