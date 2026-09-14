package claude

import (
	"encoding/json"
	"fmt"
)

// result is the document claude -p --output-format json prints. A failure
// is flagged by is_error with the message in result; subtype still says
// "success" then, so it is not consulted.
type result struct {
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// parseResult reads claude's JSON output.
func parseResult(out string) (result, error) {
	var res result
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return result{}, fmt.Errorf("unexpected claude output: %w", err)
	}
	return res, nil
}
