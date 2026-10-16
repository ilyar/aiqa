package capability

import "testing"

func TestThinkingEarlyReturnsCannotBypassConflicts(t *testing.T) {
	for _, aliases := range [][]string{{"thinking_adaptive", "adaptive_thinking"}, {"thinking_manual", "manual_thinking"}} {
		for name, caps := range map[string]map[string]any{
			"flat aliases without nested object":   {aliases[0]: true, aliases[1]: false},
			"parent denial without types":          {aliases[0]: true, "thinking": map[string]any{"supported": false}},
			"malformed flat without nested object": {aliases[0]: "unexpected"},
			"malformed parent":                     {"thinking": map[string]any{"supported": map[string]any{"enabled": true}}},
		} {
			t.Run(aliases[0]+"/"+name, func(t *testing.T) {
				caps["text"] = true
				got := ClassifyAnthropic("opaque", map[string]any{"capabilities": caps}, Options{UnknownPolicy: "probe"})
				if !got.Unknown || got.ProbeEligible || len(got.ProbeCapabilities) != 0 {
					t.Fatalf("conflict allowed probing: %+v", got)
				}
			})
		}
	}
}
