package content

import "strconv"

// TokenCount is a normalized count of model tokens.
type TokenCount uint64

// UsageField identifies a normalized usage value or derived total.
type UsageField string

const (
	UsageFieldInputTokens         UsageField = "InputTokens"
	UsageFieldOutputTokens        UsageField = "OutputTokens"
	UsageFieldCacheReadTokens     UsageField = "CacheReadTokens"
	UsageFieldCacheCreationTokens UsageField = "CacheCreationTokens"
	UsageFieldReasoningTokens     UsageField = "ReasoningTokens"
	UsageFieldContextTokens       UsageField = "ContextTokens"
	UsageFieldTotalTokens         UsageField = "TotalTokens"
)

// UsageValidationReason identifies why normalized usage is invalid.
// Deprecated: use Usage.ReasoningWithinOutput when only a predicate is needed.
type UsageValidationReason string

const UsageValidationReasonReasoningExceedsOutput UsageValidationReason = "exceeds OutputTokens"

const maximumTokenCount TokenCount = ^TokenCount(0)

// Usage is normalized model token usage.
type Usage struct {
	InputTokens         TokenCount
	OutputTokens        TokenCount
	CacheReadTokens     TokenCount
	CacheCreationTokens TokenCount
	// ReasoningTokens is the part of OutputTokens the model spent on internal
	// reasoning. It is a SUBSET of OutputTokens, never a separate addend: this
	// is why TotalTokens adds only context and output, and why a distinct
	// reasoning price applies to ReasoningTokens with the output price applying
	// to OutputTokens-ReasoningTokens rather than to the whole of it.
	//
	// The convention was undocumented until it destroyed a generation, so it is
	// recorded here against each format's own published contract:
	//
	//   - OpenAI Chat Completions — INSIDE. completion_tokens_details is a
	//     "Breakdown of tokens used in a completion", and its sibling
	//     rejected_prediction_tokens states the relationship outright: such
	//     tokens, "like reasoning tokens, ... are still counted in the total
	//     completion tokens for purposes of billing, output, and context window
	//     limits" (github.com/openai/openai-openapi, CompletionUsage).
	//   - OpenAI Responses — INSIDE. Reasoning tokens "are billed as output
	//     tokens", and the guide's worked example reports input_tokens 75 with
	//     output_tokens 1186 and total_tokens 1261 while 1024 of that output is
	//     reasoning, so reasoning is not a third addend
	//     (developers.openai.com/api/docs/guides/reasoning).
	//   - Anthropic Messages — INSIDE, and stated most explicitly of all:
	//     "output_tokens remains the inclusive, authoritative total used for
	//     billing", with thinking_tokens "Always <= output_tokens"
	//     (platform.claude.com/docs/en/api/messages).
	//   - Gemini — OUTSIDE, and the one format that must be converted.
	//     totalTokenCount is documented as "prompt + thoughts + response
	//     candidates", naming thoughts as an addend alongside candidates, so
	//     codec/geminiapi adds thoughtsTokenCount to candidatesTokenCount on
	//     that documented basis before filling this field
	//     (generativelanguage.googleapis.com discovery document, UsageMetadata).
	//   - Bedrock Converse — silent, and moot: com.amazonaws.bedrockruntime
	//     #TokenUsage carries no reasoning member at all, so this stays zero
	//     even for a Claude model whose reply contains reasoning content.
	//
	// A provider can contradict its own documentation, and one does. OpenRouter
	// documents the subset relationship — reasoning tokens are "considered
	// output tokens and charged accordingly", completion_tokens_details is a
	// "Breakdown of completion tokens", and total_tokens is the "Sum of the
	// above two fields", prompt and completion, with no reasoning addend
	// (openrouter.ai/docs/use-cases/reasoning-tokens,
	// openrouter.ai/docs/api-reference/overview) — yet returned
	// completion_tokens=216 alongside reasoning_tokens=226 on a complete HTTP
	// 200. No published arithmetic reconciles those two numbers, so counts that
	// break the convention are carried exactly as reported, are observable
	// through ReasoningWithinOutput, and never discard the generation they
	// describe.
	ReasoningTokens TokenCount
}

// UsageValidationError reports an invalid relationship between usage fields.
// It remains available for source compatibility; decoding and aggregation no
// longer use this condition as a fatal gate.
type UsageValidationError struct {
	Field  UsageField
	Reason UsageValidationReason
}

func (e *UsageValidationError) Error() string {
	return "content: invalid usage field " + string(e.Field) + ": " + string(e.Reason)
}

// ReasoningWithinOutput reports whether these counts satisfy the subset
// convention documented on ReasoningTokens.
//
// It is deliberately a predicate rather than a validation error. This check
// used to be Usage.Validate, called fatally from every decode, serialization
// and aggregation path, so a single provider whose reasoning count disagreed
// with its output count cost the caller a completed generation, made a stored
// transcript undecodable, and poisoned a session's running total. An accounting
// field is a metric; the content is the product. Report the divergence here and
// price or annotate around it — nothing gates on it.
func (u Usage) ReasoningWithinOutput() bool {
	return u.ReasoningTokens <= u.OutputTokens
}

// Validate verifies the historical reasoning/output relationship.
// Deprecated: prefer ReasoningWithinOutput. This method is retained so the
// compatible API addition does not break callers compiled against core v0.5.
func (u Usage) Validate() error {
	if !u.ReasoningWithinOutput() {
		return &UsageValidationError{
			Field:  UsageFieldReasoningTokens,
			Reason: UsageValidationReasonReasoningExceedsOutput,
		}
	}
	return nil
}

// UsageOverflowError reports a token-count addition that cannot be represented.
type UsageOverflowError struct {
	Field UsageField
	Left  TokenCount
	Right TokenCount
}

func (e *UsageOverflowError) Error() string {
	return "content: usage field " + string(e.Field) + " overflow: " +
		strconv.FormatUint(uint64(e.Left), 10) + " + " +
		strconv.FormatUint(uint64(e.Right), 10)
}

// ContextTokens returns all input tokens that occupy model context.
func (u Usage) ContextTokens() (TokenCount, error) {
	input, err := addTokenCounts(UsageFieldContextTokens, u.InputTokens, u.CacheReadTokens)
	if err != nil {
		return 0, err
	}
	return addTokenCounts(UsageFieldContextTokens, input, u.CacheCreationTokens)
}

// TotalTokens returns context plus output tokens.
func (u Usage) TotalTokens() (TokenCount, error) {
	contextTokens, err := u.ContextTokens()
	if err != nil {
		return 0, err
	}
	return addTokenCounts(UsageFieldTotalTokens, contextTokens, u.OutputTokens)
}

// Add combines two usage values field by field. The only failure is a sum that
// TokenCount cannot represent, which is a representability fault rather than an
// accounting one: an operand whose reasoning exceeds its output is summed like
// any other, because refusing it would let one divergent provider report
// invalidate every later aggregate that folds it in.
func (u Usage) Add(other Usage) (Usage, error) {
	left, right := u, other
	var sum Usage
	var err error
	if sum.ReasoningTokens, err = addTokenCounts(UsageFieldReasoningTokens, left.ReasoningTokens, right.ReasoningTokens); err != nil {
		return Usage{}, err
	}
	if sum.InputTokens, err = addTokenCounts(UsageFieldInputTokens, left.InputTokens, right.InputTokens); err != nil {
		return Usage{}, err
	}
	if sum.OutputTokens, err = addTokenCounts(UsageFieldOutputTokens, left.OutputTokens, right.OutputTokens); err != nil {
		return Usage{}, err
	}
	if sum.CacheReadTokens, err = addTokenCounts(UsageFieldCacheReadTokens, left.CacheReadTokens, right.CacheReadTokens); err != nil {
		return Usage{}, err
	}
	if sum.CacheCreationTokens, err = addTokenCounts(UsageFieldCacheCreationTokens, left.CacheCreationTokens, right.CacheCreationTokens); err != nil {
		return Usage{}, err
	}
	return sum, nil
}

func addTokenCounts(field UsageField, left TokenCount, right TokenCount) (TokenCount, error) {
	if right > maximumTokenCount-left {
		return 0, &UsageOverflowError{Field: field, Left: left, Right: right}
	}
	return left + right, nil
}
