package report

import (
	"strings"

	"github.com/ilyar/aiqa/internal/result"
)

func Finalize(r *result.Report) {
	for i := range r.Models {
		finalizeModel(&r.Models[i])
	}
	for _, run := range r.Extensions {
		if run.Subject.Kind != "model" {
			continue
		}
		for index := range r.Models {
			if r.Models[index].ID == run.Subject.ID {
				addExtensionRun(&r.Models[index].Summary, run)
			}
		}
	}
	for index := range r.Models {
		if r.Models[index].Summary.Status != "not-evaluated" || hasModelExtension(r.Extensions, r.Models[index].ID) {
			finishSummary(&r.Models[index].Summary)
		}
	}
	r.Summary = summarizeReport(*r)
}

func hasModelExtension(runs []result.ExtensionRun, model string) bool {
	for _, run := range runs {
		if run.Subject.Kind == "model" && run.Subject.ID == model && len(run.Suites) > 0 {
			return true
		}
	}
	return false
}

func addExtensionRun(summary *result.Summary, run result.ExtensionRun) {
	for _, suite := range run.Suites {
		addStatus(summary, suite.Status)
		summary.Usage = summary.Usage.Add(suite.Usage)
	}
}

func finalizeModel(m *result.ModelReport) {
	m.Summary = summarizeModel(*m)
	for index := range m.Modules {
		module := &m.Modules[index]
		module.TestIDs = nil
		module.Summary = nil
		var tests []result.TestResult
		for _, test := range m.Compatibility {
			if testBelongsToModule(test.ID, module.ID) {
				tests = append(tests, test)
				module.TestIDs = append(module.TestIDs, test.ID)
			}
		}
		if len(tests) > 0 {
			summary := summarizeTests(tests)
			module.Summary = &summary
		}
	}
	if m.Summary.Status == "not-evaluated" {
		m.Assessment.Verdict = "not-evaluated"
	} else {
		m.Assessment.Verdict = m.Summary.Status
	}
}

func testBelongsToModule(testID, moduleID string) bool {
	switch moduleID {
	case "images-generations":
		return strings.HasPrefix(testID, "images.")
	case "chat-completions":
		return !strings.HasPrefix(testID, "images.") && !strings.HasPrefix(testID, "anthropic.")
	case "anthropic.messages":
		return strings.HasPrefix(testID, "anthropic.messages.") || strings.HasPrefix(testID, "anthropic.error.") || strings.HasPrefix(testID, "anthropic.header.") || strings.HasPrefix(testID, "anthropic.auth.") || testID == "anthropic.models.retrieve"
	case "anthropic.tools":
		return strings.HasPrefix(testID, "anthropic.tools.") && testID != "anthropic.tools.strict_schema"
	case "anthropic.token-counting":
		return strings.HasPrefix(testID, "anthropic.tokens.")
	case "anthropic.structured-output":
		return strings.HasPrefix(testID, "anthropic.structured.") || testID == "anthropic.tools.strict_schema"
	case "anthropic.vision":
		return strings.HasPrefix(testID, "anthropic.vision.")
	case "anthropic.thinking":
		return strings.HasPrefix(testID, "anthropic.thinking.")
	case "google-genai.interactions":
		return testID == "google.models.get" || strings.HasPrefix(testID, "google.models.metadata.") || testID == "google.model-param.unsupported" || testID == "google.interactions.basic" || testID == "google.interactions.schema" || testID == "google.interactions.output_text" || testID == "google.interactions.usage" || testID == "google.interactions.store_false" || testID == "google.interactions.status"
	case "google-genai.interactions-tools":
		return strings.HasPrefix(testID, "google.interactions.tools.")
	case "google-genai.interactions-structured-output":
		return strings.HasPrefix(testID, "google.interactions.structured.")
	case "google-genai.interactions-vision":
		return strings.HasPrefix(testID, "google.interactions.vision.")
	case "google-genai.interactions-thinking":
		return strings.HasPrefix(testID, "google.interactions.thinking.")
	case "google-genai.interactions-stateful":
		return strings.HasPrefix(testID, "google.interactions.stateful.")
	case "google-genai.interactions-stream":
		return strings.HasPrefix(testID, "google.interactions.stream.")
	case "google-genai.generate-content":
		return strings.HasPrefix(testID, "google.generate-content.") || strings.HasPrefix(testID, "google.generate.")
	case "google-genai.generate-content-stream":
		return strings.HasPrefix(testID, "google.generate-content.stream.")
	case "google-genai.generate-content-tools":
		return strings.HasPrefix(testID, "google.generate-content.tools.")
	case "google-genai.generate-content-structured-output":
		return strings.HasPrefix(testID, "google.generate-content.structured.")
	case "google-genai.token-counting":
		return strings.HasPrefix(testID, "google.tokens.")
	case "google-genai.embeddings":
		return strings.HasPrefix(testID, "google.embeddings.")
	case "google-genai.vision":
		return strings.HasPrefix(testID, "google.vision.")
	case "google-genai.thinking":
		return strings.HasPrefix(testID, "google.generate-content.thinking.")
	default:
		return false
	}
}

func summarizeTests(tests []result.TestResult) result.Summary {
	var s result.Summary
	for _, test := range tests {
		if test.Applicability == result.ApplicabilityNotApplicable {
			continue
		}
		addStatus(&s, test.Status)
		s.Usage = s.Usage.Add(test.Usage)
		if test.Required {
			if test.Status == result.StatusPass {
				s.RequiredPass++
			} else if test.Status != result.StatusSkip {
				s.RequiredFail++
			}
		}
	}
	finishSummary(&s)
	return s
}

func summarizeModel(m result.ModelReport) result.Summary {
	var s result.Summary
	for _, t := range m.Compatibility {
		if t.Applicability == result.ApplicabilityNotApplicable {
			continue
		}
		addStatus(&s, t.Status)
		s.Usage = s.Usage.Add(t.Usage)
		if t.Required {
			if t.Status == result.StatusPass {
				s.RequiredPass++
			} else if t.Status != result.StatusSkip {
				s.RequiredFail++
			}
		}
	}
	for _, p := range m.Parameters {
		addStatus(&s, p.Status)
		s.Usage = s.Usage.Add(p.Usage)
	}
	if m.Context != nil {
		addStatus(&s, m.Context.Status)
		s.ContextProbeTokens += m.Context.ConsumedTokens
	}
	finishSummary(&s)
	if len(m.Compatibility) == 0 && len(m.Parameters) == 0 && m.Context == nil {
		s.Status = "not-evaluated"
	}
	return s
}

func summarizeReport(r result.Report) result.Summary {
	var s result.Summary
	evaluatedModels := 0
	for _, t := range r.Discovery.Tests {
		if t.Applicability == result.ApplicabilityNotApplicable {
			continue
		}
		addStatus(&s, t.Status)
		s.Usage = s.Usage.Add(t.Usage)
		if t.Required {
			if t.Status == result.StatusPass {
				s.RequiredPass++
			} else if t.Status != result.StatusSkip {
				s.RequiredFail++
			}
		}
	}
	for _, t := range r.Discovery.Compatibility {
		if t.Applicability == result.ApplicabilityNotApplicable {
			continue
		}
		addStatus(&s, t.Status)
		s.Usage = s.Usage.Add(t.Usage)
		if t.Required {
			if t.Status == result.StatusPass {
				s.RequiredPass++
			} else if t.Status != result.StatusSkip {
				s.RequiredFail++
			}
		}
	}
	for _, m := range r.Models {
		if m.Summary.Status != "not-evaluated" {
			evaluatedModels++
		}
		s.Pass += m.Summary.Pass
		s.Fail += m.Summary.Fail
		s.Warn += m.Summary.Warn
		s.Unsupported += m.Summary.Unsupported
		s.Skip += m.Summary.Skip
		s.Error += m.Summary.Error
		s.RequiredPass += m.Summary.RequiredPass
		s.RequiredFail += m.Summary.RequiredFail
		s.Usage = s.Usage.Add(m.Summary.Usage)
		s.ContextProbeTokens += m.Summary.ContextProbeTokens
	}
	for _, run := range r.Extensions {
		if run.Subject.Kind == "model" {
			continue
		}
		addExtensionRun(&s, run)
	}
	finishSummary(&s)
	if len(r.Models) > 0 && evaluatedModels == 0 && s.RequiredFail == 0 && s.Fail == 0 && s.Error == 0 && s.Warn == 0 && s.Unsupported == 0 {
		s.Status = "not-evaluated"
	}
	return s
}

func addStatus(s *result.Summary, status result.Status) {
	switch status {
	case result.StatusPass:
		s.Pass++
	case result.StatusFail:
		s.Fail++
	case result.StatusWarn:
		s.Warn++
	case result.StatusUnsupported:
		s.Unsupported++
	case result.StatusSkip:
		s.Skip++
	case result.StatusError:
		s.Error++
	}
}

func finishSummary(s *result.Summary) {
	switch {
	case s.RequiredFail > 0:
		s.Status = "fail"
	case s.Error > 0 || s.Fail > 0:
		s.Status = "partial"
	case s.Warn > 0 || s.Unsupported > 0:
		s.Status = "pass-with-gaps"
	default:
		s.Status = "pass"
	}
}
