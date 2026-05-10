package saga

// Capabilities — what this engine declares it offers, per spec §10
// (Conformance & capability negotiation). Single source of truth: parser,
// migration, capabilities CLI, and MCP tool all read from here.
//
// Spec-led discipline (V8 model): the spec is ambitious, the engine
// catches up. That asymmetry is what this declaration is for —
// adopters know exactly what's safe to assume from this engine vs. what
// the spec specifies but this version doesn't yet implement.

// SpecVersion is the saga-topic spec this engine targets.
const SpecVersion = "1.0"

// ConformanceLevel — see spec §11. Levels:
//
//	1 — Reader: parse and surface frontmatter, ignore relations and operators.
//	2 — Pure-metadata: parse and honour pure-metadata relation operators.
//	3 — Reference: full v1.0 reference behaviour (excludes runtime operators).
//	4 — Cognitive: implements one or more runtime-required operators.
//
// The reference engine targets Level 3 today. Runtime-required operators are
// specced but not implemented in v1.0, so we are not yet at Level 4.
const ConformanceLevel = 3

// SpecTypesImplemented — types with specialised retrieval/storage behaviour.
// These were the v1 baseline before the spec was published.
var SpecTypesImplemented = []string{
	"profile",
	"preference",
	"policy",
	"topic",
}

// SpecTypesAcceptedOpaque — types from spec §4 that v1.0 engine accepts on
// write and indexes for retrieval, but does NOT specialise behaviour for.
// They fall through to the same retrieval path as `topic`.
var SpecTypesAcceptedOpaque = []string{
	// Declarative (§4.1)
	"convention",
	"fact",
	// Procedural (§4.2)
	"workflow",
	"runbook",
	"skill",
	// Episodic (§4.3)
	"incident",
	"investigation",
	"decision",
	"observation",
	"hypothesis",
}

// SpecTypesAll — union of implemented + accepted-opaque, used by the parser
// for validation. A type outside this list is rejected (strict mode per spec
// §4.4); engines MAY switch to lenient mode in future versions.
func SpecTypesAll() []string {
	out := make([]string, 0, len(SpecTypesImplemented)+len(SpecTypesAcceptedOpaque))
	out = append(out, SpecTypesImplemented...)
	out = append(out, SpecTypesAcceptedOpaque...)
	return out
}

// PureMetadataOperators — spec §6.2 mandatory operators. All offered.
var PureMetadataOperators = []string{
	"@supersedes",
	"@deprecated",
	"@derived_from",
	"@conflicts_with",
	"@relates_to",
	"@refines",
}

// RuntimeRequiredOperatorsOffered — runtime-required operators (§7.2) that
// THIS engine implements. v1.0 reference engine offers ZERO; the operators
// are specced but await an LLM-integrated cognition layer.
//
// When a runtime operator is added (e.g. @synthesize), append its name here
// and the capabilities output will reflect availability automatically.
var RuntimeRequiredOperatorsOffered = []string{}

// RuntimeRequiredOperatorsSpecced — for visibility, the full spec list.
// Used by capabilities output to make the gap explicit ("specced but not
// offered by this engine").
var RuntimeRequiredOperatorsSpecced = []string{
	"@synthesize",
	"@summarize",
	"@reconcile",
	"@promote",
	"@retire",
}

// RetrievalFeatures — what the recall scorer composes today.
var RetrievalFeatures = []string{
	"bm25",
	"recency",
	"supersedes_skip",
	"refines_boost",
	"conflicts_annotation",
}

// EngineCapabilities is the structured declaration consumed by `saga
// capabilities`, the MCP tool of the same name, and (eventually) `saga
// doctor`. Stable JSON shape; new fields append, never rename.
type EngineCapabilities struct {
	SpecVersion          string   `json:"spec_version"`
	EngineVersion        string   `json:"engine_version"`
	ConformanceLevel     int      `json:"conformance_level"`
	TypesImplemented     []string `json:"types_implemented"`
	TypesAcceptedOpaque  []string `json:"types_accepted_opaque"`
	OperatorsPureMeta    []string `json:"operators_pure_metadata"`
	OperatorsRuntimeOff  []string `json:"operators_runtime_offered"`
	OperatorsRuntimeSpec []string `json:"operators_runtime_specced"`
	Retrieval            []string `json:"retrieval"`
}

// DescribeCapabilities returns the engine's capability declaration.
func DescribeCapabilities() EngineCapabilities {
	return EngineCapabilities{
		SpecVersion:          SpecVersion,
		EngineVersion:        VersionString(),
		ConformanceLevel:     ConformanceLevel,
		TypesImplemented:     append([]string{}, SpecTypesImplemented...),
		TypesAcceptedOpaque:  append([]string{}, SpecTypesAcceptedOpaque...),
		OperatorsPureMeta:    append([]string{}, PureMetadataOperators...),
		OperatorsRuntimeOff:  append([]string{}, RuntimeRequiredOperatorsOffered...),
		OperatorsRuntimeSpec: append([]string{}, RuntimeRequiredOperatorsSpecced...),
		Retrieval:            append([]string{}, RetrievalFeatures...),
	}
}
