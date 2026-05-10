package saga

import "testing"

func TestDescribeCapabilities_basicShape(t *testing.T) {
	caps := DescribeCapabilities()
	if caps.SpecVersion != "1.0" {
		t.Errorf("SpecVersion = %q, want 1.0", caps.SpecVersion)
	}
	if caps.ConformanceLevel != 3 {
		t.Errorf("ConformanceLevel = %d, want 3", caps.ConformanceLevel)
	}
	if got, want := len(caps.TypesImplemented), 4; got != want {
		t.Errorf("len(TypesImplemented) = %d, want %d", got, want)
	}
	if got, want := len(caps.TypesAcceptedOpaque), 10; got != want {
		t.Errorf("len(TypesAcceptedOpaque) = %d, want %d", got, want)
	}
	if got, want := len(caps.OperatorsPureMeta), 6; got != want {
		t.Errorf("len(OperatorsPureMeta) = %d, want %d", got, want)
	}
	if len(caps.OperatorsRuntimeOff) != 0 {
		t.Errorf("v1.0 engine should offer 0 runtime operators; got %v", caps.OperatorsRuntimeOff)
	}
	if got, want := len(caps.OperatorsRuntimeSpec), 5; got != want {
		t.Errorf("len(OperatorsRuntimeSpec) = %d, want %d", got, want)
	}
}

func TestSpecTypesAll_unionMatchesParts(t *testing.T) {
	all := SpecTypesAll()
	if got, want := len(all), len(SpecTypesImplemented)+len(SpecTypesAcceptedOpaque); got != want {
		t.Errorf("SpecTypesAll() len = %d, want %d", got, want)
	}
	have := map[string]bool{}
	for _, t := range all {
		have[t] = true
	}
	for _, t := range SpecTypesImplemented {
		if !have[t] {
			panic("missing implemented type: " + t)
		}
	}
	for _, t := range SpecTypesAcceptedOpaque {
		if !have[t] {
			panic("missing accepted-opaque type: " + t)
		}
	}
}
