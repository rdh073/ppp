package domain

// WorkflowDef is a YAML-serialisable workflow DAG definition.
// Each named def can be assigned to a task. The server loads defs from
// the filesystem or via the HTTP API, and hot-swaps them without restart.
type WorkflowDef struct {
	Name    string               `yaml:"name"    json:"name"`
	Version int                  `yaml:"version" json:"version"`
	Entry   NodeKind             `yaml:"entry"   json:"entry"`
	Nodes   map[NodeKind]NodeDef `yaml:"nodes"   json:"nodes"`
}

// NodeDef describes transitions out of one node kind.
type NodeDef struct {
	Transitions []Transition `yaml:"transitions" json:"transitions"`
}

// Transition is a conditional edge in the DAG.
// When is evaluated in order; first match wins.
// Empty/absent When means "always matches" (default, must be last).
type Transition struct {
	To   NodeKind `yaml:"to"   json:"to"`
	When string   `yaml:"when" json:"when"` // expression or ""
}
