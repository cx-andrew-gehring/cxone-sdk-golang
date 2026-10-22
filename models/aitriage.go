package models

import (
	"bytes"
	"encoding/json"
	"time"
)

// TriageBucket groups the results of one scanner type to triage.
// ScannerType is "sast" or "sca" (case-insensitive); ResultIDs are the
// alternateId values returned by GET /api/results.
type TriageBucket struct {
	ScannerType string   `json:"scannerType"`
	ResultIDs   []string `json:"resultIDs"`
}

// TriageRequest is the body of POST /api/ai-triage/triage.
type TriageRequest struct {
	ScanID  string         `json:"scanID"`
	Buckets []TriageBucket `json:"buckets"`
}

// TriageAccepted is the 202 response of POST /api/ai-triage/triage.
//
// Published is false when an identical triage job already exists; that is a
// success (the request is idempotent). ExistingTriageState then carries the
// state of the existing job.
type TriageAccepted struct {
	ScanID              string  `json:"scanID"`
	Status              string  `json:"status"`
	TriageID            string  `json:"triageID"`
	Published           bool    `json:"published"`
	ExistingTriageState *string `json:"existingTriageState"`
}

// UnmarshalJSON applies the documented default of published=true when the
// field is absent.
func (t *TriageAccepted) UnmarshalJSON(data []byte) error {
	type plain TriageAccepted
	p := plain{Published: true}
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*t = TriageAccepted(p)
	return nil
}

// TriageConfidence is the confidence score of a triage analysis.
type TriageConfidence struct {
	Score       float64 `json:"score"`
	Explanation string  `json:"explanation,omitempty"`
}

// TriageStatusReasoning is a status plus reasoning (reachability or
// exploitability).
type TriageStatusReasoning struct {
	Status    string `json:"status"`
	Reasoning string `json:"reasoning,omitempty"`
}

// TriageAnalysis is the structured analysis of one triage result.
type TriageAnalysis struct {
	Confidence     TriageConfidence      `json:"confidence"`
	Reachability   TriageStatusReasoning `json:"reachability"`
	Exploitability TriageStatusReasoning `json:"exploitability"`
	UsageLocations []string              `json:"usage_locations,omitempty"`
}

// TriageMetadata is metadata about the vulnerable component.
type TriageMetadata struct {
	Component      *string `json:"component,omitempty"`
	Version        *string `json:"version,omitempty"`
	DependencyType *string `json:"dependency_type,omitempty"`
}

// TriageVerificationStep is one step of the agent's reasoning trace.
type TriageVerificationStep struct {
	Task           string   `json:"task"`
	Category       string   `json:"category"`
	Status         string   `json:"status"`
	Thoughts       []string `json:"thoughts,omitempty"`
	Conclusion     string   `json:"conclusion,omitempty"`
	UsageLocations []string `json:"usage_locations,omitempty"`
}

// TriageRepositoryInfo is the repository technology snapshot in a trace.
type TriageRepositoryInfo struct {
	Path                        string   `json:"path,omitempty"`
	ExcludedSubdirectories      []string `json:"excluded_subdirectories,omitempty"`
	Description                 string   `json:"description,omitempty"`
	ProgrammingLanguages        []string `json:"programming_languages,omitempty"`
	Frameworks                  []string `json:"frameworks,omitempty"`
	DependencyManagementSystems []string `json:"dependency_management_systems,omitempty"`
	DependencyFiles             []string `json:"dependency_files,omitempty"`
	BuildProcessInfo            *string  `json:"build_process_info,omitempty"`
	BuildSystem                 *string  `json:"build_system,omitempty"`
	Documentation               []string `json:"documentation,omitempty"`
	KeyDirectories              []string `json:"key_directories,omitempty"`
}

// TriageReasoningTrace is the optional detailed reasoning trace.
type TriageReasoningTrace struct {
	VerificationSteps []TriageVerificationStep `json:"verification_steps,omitempty"`
	RepositoryInfo    *TriageRepositoryInfo    `json:"repository_info,omitempty"`
}

// TriageResultDetail is the completed triage result
// (TriageResultDetailSchema).
type TriageResultDetail struct {
	ResultID             string                `json:"resultID"`
	Scanner              string                `json:"scanner"`
	TriageStatus         string                `json:"triageStatus"`
	ReachabilityStatus   string                `json:"reachabilityStatus"`
	ExploitabilityStatus string                `json:"exploitabilityStatus"`
	AttackabilityStatus  string                `json:"attackabilityStatus,omitempty"`
	Summary              string                `json:"summary,omitempty"`
	TriagedAt            time.Time             `json:"triagedAt"`
	Analysis             TriageAnalysis        `json:"analysis"`
	Metadata             TriageMetadata        `json:"metadata"`
	ReasoningTrace       *TriageReasoningTrace `json:"reasoningTrace,omitempty"`
	MockOrigin           bool                  `json:"mockOrigin,omitempty"`
	GroupID              *string               `json:"groupId,omitempty"`
	ProjectID            *string               `json:"projectId,omitempty"`
	SourceProjectID      *string               `json:"sourceProjectId,omitempty"`
}

// TriageResultEnvelope is the response of
// GET /api/ai-triage/triage/{project_id}/{group_id}, which is a oneOf:
//
//   - in-progress shape: {projectID, groupID, jobStatus: IN_PROGRESS|FAILED};
//     JobStatus is set and Detail is nil.
//   - detail shape: the full result; Detail is set and JobStatus is empty.
//
// The shapes are told apart by the presence of "jobStatus".
type TriageResultEnvelope struct {
	ProjectID string
	GroupID   string
	JobStatus string
	Detail    *TriageResultDetail
}

// UnmarshalJSON decodes either oneOf shape. Unknown fields are ignored.
func (e *TriageResultEnvelope) UnmarshalJSON(data []byte) error {
	var probe struct {
		ProjectID string  `json:"projectID"`
		GroupID   string  `json:"groupID"`
		JobStatus *string `json:"jobStatus"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	*e = TriageResultEnvelope{}
	if probe.JobStatus != nil {
		e.ProjectID, e.GroupID, e.JobStatus = probe.ProjectID, probe.GroupID, *probe.JobStatus
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	var d TriageResultDetail
	if err := json.Unmarshal(data, &d); err != nil {
		return err
	}
	e.Detail = &d
	if d.ProjectID != nil {
		e.ProjectID = *d.ProjectID
	}
	if d.GroupID != nil {
		e.GroupID = *d.GroupID
	}
	return nil
}
