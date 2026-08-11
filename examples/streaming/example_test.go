package streamingexample_test

import (
	"fmt"

	"github.com/looprig/core/content"
	"github.com/looprig/core/content/streamaccumulator"
)

func Example_accumulateChunks() {
	var text streamaccumulator.Text
	text.Add(&content.TextChunk{Text: "Hello, "})
	text.Add(&content.TextChunk{Text: "world!"})
	fmt.Println(text.Block().Text)

	var thinking streamaccumulator.Thinking
	thinking.Add(&content.ThinkingChunk{Thinking: "check "})
	thinking.Add(&content.ThinkingChunk{Thinking: "facts", Signature: "signed"})
	fmt.Println(thinking.Block().Thinking, thinking.Block().Signature)

	var calls streamaccumulator.ToolUses
	calls.Add(&content.ToolUseChunk{Index: 0, ID: "call_1", Name: "weather", InputJSON: `{"city":`})
	calls.Add(&content.ToolUseChunk{Index: 0, InputJSON: `"Boston"}`})
	call := calls.Blocks()[0]
	fmt.Println(call.ID, call.Name, string(call.Input))

	// Output:
	// Hello, world!
	// check facts signed
	// call_1 weather {"city":"Boston"}
}
