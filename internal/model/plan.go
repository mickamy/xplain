package model

// Explain represents the root of a PostgreSQL execution plan.
type Explain struct {
	Plan          *PlanNode
	PlanningTime  float64
	ExecutionTime float64
	Settings      map[string]string
	// Extra carries additional top-level fields that we do not interpret yet.
	Extra map[string]any
}

// PlanNode captures one node in the execution plan tree.
type PlanNode struct {
	ID                 string
	NodeType           string
	RelationName       string
	Schema             string
	Alias              string
	ParentRelationship string
	StartupCost        float64
	TotalCost          float64
	PlanRows           float64
	PlanWidth          float64
	ActualStartupTime  float64
	ActualTotalTime    float64
	ActualRows         float64
	ActualLoops        float64
	WorkersPlanned     float64
	WorkersLaunched    float64
	Output             []string
	Filter             string
	JoinType           string
	IndexName          string
	HashCond           string
	MergeCond          string
	SortKey            []string
	GroupKey           []string
	Buffers            Buffers
	Extra              map[string]any
	Children           []*PlanNode
}

// Buffers holds buffer usage statistics for a node.
type Buffers struct {
	SharedHit       int64
	SharedRead      int64
	SharedDirtied   int64
	SharedWritten   int64
	LocalHit        int64
	LocalRead       int64
	LocalDirtied    int64
	LocalWritten    int64
	TempRead        int64
	TempWritten     int64
	IOReadTimeMs    float64
	IOWriteTimeMs   float64
	BlockReadTimeMs float64
}
