package claude

import "testing"

// fixtureAnswer is what claude -p --output-format json prints for a
// successful answer, trimmed to the fields git-why reads plus a few it
// ignores.
const fixtureAnswer = `{"type":"result","subtype":"success","is_error":false,"duration_ms":6091,"num_turns":1,
"result":"The timeout is a package-level variable rather than a constant so tests can override it.",
"session_id":"85d3d907","total_cost_usd":0.028,"usage":{"input_tokens":2,"output_tokens":363}}`

// fixtureNotLoggedIn is claude's output without credentials: exit 1, and
// the failure is flagged by is_error while subtype still says success.
const fixtureNotLoggedIn = `{"type":"result","subtype":"success","is_error":true,"duration_ms":32,
"result":"Not logged in · Please run /login","session_id":"421c2a2a","total_cost_usd":0,"terminal_reason":"api_error"}`

func TestParseResult(t *testing.T) {
	res, err := parseResult(fixtureAnswer)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || res.Result != "The timeout is a package-level variable rather than a constant so tests can override it." {
		t.Errorf("parseResult() = %+v", res)
	}

	res, err = parseResult(fixtureNotLoggedIn)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || res.Result != "Not logged in · Please run /login" {
		t.Errorf("parseResult(not logged in) = %+v", res)
	}

	for _, out := range []string{"", "not json", "Not logged in · Please run /login"} {
		if _, err := parseResult(out); err == nil {
			t.Errorf("parseResult(%q) succeeded", out)
		}
	}
}
