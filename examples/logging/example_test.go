package loggingexample_test

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/looprig/core/logging"
)

func Example_structuredLogger() {
	level, err := logging.ParseLevel(" debug ")
	if err != nil {
		panic(err)
	}

	var output bytes.Buffer
	logger := logging.New(logging.Config{Writer: &output, Level: level})
	logger.Debug("turn started", "agent", "coding")

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		panic(err)
	}
	fmt.Println(record["level"], record["msg"], record["agent"])

	// Output:
	// DEBUG turn started coding
}
