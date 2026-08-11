package contentexample_test

import (
	"fmt"

	"github.com/looprig/core/content"
)

func Example_blocksAndMessages() {
	request := &content.UserMessage{Message: content.Message{
		Role: content.RoleUser,
		Blocks: []content.Block{
			&content.TextBlock{Text: "Summarize the report."},
			&content.DocumentBlock{
				MediaType: content.MediaTypeDocumentText,
				Name:      "report.txt",
				Text:      "Revenue increased.",
			},
		},
	}}

	thread := content.AgenticMessages{request}
	text := thread[0].(*content.UserMessage).Blocks[0].(*content.TextBlock)
	fmt.Println(request.Role, text.Text)

	wire, err := content.MarshalBlock(request.Blocks[1])
	if err != nil {
		panic(err)
	}
	fmt.Println(string(wire))

	decoded, err := content.UnmarshalBlock(wire)
	if err != nil {
		panic(err)
	}
	document := decoded.(*content.DocumentBlock)
	fmt.Println(document.Name, document.Text)

	// Output:
	// user Summarize the report.
	// {"Data":null,"MediaType":"text/plain","Name":"report.txt","Text":"Revenue increased.","type":"document"}
	// report.txt Revenue increased.
}
