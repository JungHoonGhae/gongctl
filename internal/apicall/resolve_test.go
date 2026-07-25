package apicall

import "testing"

// The same field arrives with two vocabularies depending on which source described
// the API: the embedded Swagger document says 필수/옵션, the rendered HTML table
// says the portal's own 필/옵. A check written against one of them silently passes
// everything from the other.
func TestIsRequiredAcceptsBothVocabularies(t *testing.T) {
	for v, want := range map[string]bool{
		"필수": true, "필": true, "  필  ": true, "Y": true,
		"옵션": false, "옵": false, "": false, "N": false,
	} {
		if got := isRequired(v); got != want {
			t.Errorf("isRequired(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestMissingRequired(t *testing.T) {
	op := &Operation{Params: []Param{
		{Name: "ServiceKey", Required: "필"}, // gongctl injects this one
		{Name: "pageNo", Required: "필"},
		{Name: "numOfRows", Required: "필수"},
		{Name: "bas_yy", Required: "옵"},
	}}
	missing := MissingRequired(op, map[string]string{"pageNo": "1"})
	if len(missing) != 1 || missing[0] != "numOfRows" {
		t.Errorf("missing = %v, want [numOfRows]", missing)
	}
	if m := MissingRequired(op, map[string]string{"pageNo": "1", "NUMOFROWS": "10"}); len(m) != 0 {
		t.Errorf("parameter names should match case-insensitively, still missing %v", m)
	}
	if m := MissingRequired(op, map[string]string{"pageNo": "1", "numOfRows": "10", "bas_yy": "2019"}); len(m) != 0 {
		t.Errorf("all required supplied, still missing %v", m)
	}
}

// A dataset whose parameters the portal never published must stay callable: an
// undocumented spec is not evidence that nothing is required.
func TestMissingRequiredSilentWhenSpecHasNoParams(t *testing.T) {
	if m := MissingRequired(&Operation{}, nil); m != nil {
		t.Errorf("undocumented operation should not block a call, got %v", m)
	}
	if m := MissingRequired(nil, nil); m != nil {
		t.Errorf("nil operation should not block a call, got %v", m)
	}
}

func TestOperationName(t *testing.T) {
	for endpoint, want := range map[string]string{
		"http://apis.data.go.kr/1741000/HeatWaveCasualtiesRegion/getHeatWaveCasualtiesRegionList": "getHeatWaveCasualtiesRegionList",
		"http://apis.data.go.kr/a/b/getX/": "getX",
		"":                                 "",
	} {
		if got := OperationName(Operation{Endpoint: endpoint}); got != want {
			t.Errorf("OperationName(%q) = %q, want %q", endpoint, got, want)
		}
	}
}

// The portal renders some 상세기능 blocks twice (a PC table and a mobile one), which
// made a single-operation dataset report two identical operations — and then refuse
// to call it as "ambiguous", listing the same name twice as the choices.
func TestDedupeOperationsCollapsesIdenticalDuplicates(t *testing.T) {
	dup := Operation{
		Name:     "지역별 폭염 인명피해를 조회한다.",
		Endpoint: "http://apis.data.go.kr/1741000/HeatWaveCasualtiesRegion/getHeatWaveCasualtiesRegionList",
		Params:   []Param{{Name: "pageNo", Required: "필"}, {Name: "numOfRows", Required: "필"}},
	}
	if got := dedupeOperations([]Operation{dup, dup}); len(got) != 1 {
		t.Errorf("identical duplicates → %d operations, want 1", len(got))
	}
	// Same endpoint, different documented variables: genuinely two operations.
	other := dup
	other.Params = []Param{{Name: "pageNo", Required: "필"}}
	if got := dedupeOperations([]Operation{dup, other}); len(got) != 2 {
		t.Errorf("operations differing in parameters → %d, want 2", len(got))
	}
}
