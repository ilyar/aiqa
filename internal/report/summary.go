package report

import (
	"strings"

	"github.com/ilyar/aiqa/internal/result"
)

func Finalize(r *result.Report) {
	for i := range r.Models {
		finalizeModel(&r.Models[i])
	}
	r.Summary = summarizeReport(*r)
}

func finalizeModel(m *result.ModelReport) {
	m.Summary = summarizeModel(*m)
	for index := range m.Modules {
		module := &m.Modules[index]
		module.TestIDs = nil
		module.Summary = nil
		var tests []result.TestResult
		for _, test := range m.Compatibility {
			isImage := strings.HasPrefix(test.ID, "images.")
			if (module.ID == "images-generations" && isImage) || (module.ID == "chat-completions" && !isImage) {
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

func summarizeTests(tests []result.TestResult) result.Summary {
	var s result.Summary
	for _, test := range tests {
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
	for _, e := range m.External {
		addStatus(&s, e.Status)
	}
	finishSummary(&s)
	if len(m.Compatibility) == 0 && len(m.Parameters) == 0 && m.Context == nil && len(m.External) == 0 {
		s.Status = "not-evaluated"
	}
	return s
}

func summarizeReport(r result.Report) result.Summary {
	var s result.Summary
	evaluatedModels := 0
	for _, t := range r.Discovery.Tests {
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
	for _, e := range r.Discovery.External {
		addStatus(&s, e.Status)
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
