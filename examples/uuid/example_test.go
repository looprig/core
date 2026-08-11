package uuidexample_test

import (
	"encoding/json"
	"fmt"

	"github.com/looprig/core/uuid"
)

func Example_parseAndEncode() {
	id, err := uuid.Parse("550E8400-E29B-41D4-A716-446655440000")
	if err != nil {
		panic(err)
	}
	fmt.Println(id.String())

	wire, err := json.Marshal(struct {
		ID uuid.UUID `json:"id"`
	}{ID: id})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(wire))

	// Output:
	// 550e8400-e29b-41d4-a716-446655440000
	// {"id":"550e8400-e29b-41d4-a716-446655440000"}
}
