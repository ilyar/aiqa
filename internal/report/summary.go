package report

import (
	"math"

	"github.com/ilyar/aiqa/internal/result"
)

func Finalize(r *result.Report) {
	for i := range r.Models {
		r.Models[i].Summary = summarizeModel(r.Models[i])
	}
	r.Summary = summarizeReport(*r)
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
	return s
}

func summarizeReport(r result.Report) result.Summary {
	var s result.Summary
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
	for _, m := range r.Models {
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
	denominator := s.Pass + s.Fail + s.Warn + s.Unsupported
	if denominator > 0 {
		s.Score = math.Round((float64(s.Pass)/float64(denominator))*1000) / 10
	}
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
