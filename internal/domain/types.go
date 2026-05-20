// Package domain holds the core data types shared across Proteus.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/alvarogonjim/proteus/internal/version"
	"github.com/google/uuid"
)

// --- Identifiers ---

type DesignID string
type PlanID string
type JobID string
type SessionID string
type ProjectID string
type ExperimentID string

// --- Application areas ---

type Application string

const (
	AppBinder   Application = "binder"
	AppAntibody Application = "antibody"
	AppEnzyme   Application = "enzyme"
	AppRedesign Application = "redesign"
)

// --- Tool / job kinds ---

type JobKind string

const (
	JobCompute JobKind = "compute"
	JobLab     JobKind = "lab"
)

type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

// --- Sequence and structure ---

// Sequence is one or more amino-acid chains keyed by chain ID.
type Sequence struct {
	Chains map[string]string `json:"chains"`
}

// validResidues lists the 20 standard amino-acid one-letter codes.
const validResidues = "ACDEFGHIKLMNPQRSTVWY"

// ValidAA reports whether s is a non-empty string of standard amino-acid codes.
func ValidAA(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(validResidues, r) {
			return false
		}
	}
	return true
}

// Validate checks every chain holds a valid amino-acid sequence.
func (s Sequence) Validate() error {
	if len(s.Chains) == 0 {
		return errors.New("sequence has no chains")
	}
	for id, seq := range s.Chains {
		if !ValidAA(seq) {
			return errors.New("chain " + id + " is not a valid amino-acid sequence")
		}
	}
	return nil
}

type ResidueRef struct {
	Chain    string `json:"chain"`
	Position int    `json:"position"`
	AA       string `json:"aa,omitempty"`
}

type PDBReference struct {
	PDBID    string       `json:"pdb_id,omitempty"`
	FilePath string       `json:"file_path,omitempty"`
	URL      string       `json:"url,omitempty"`
	Chain    string       `json:"chain,omitempty"`
	Epitope  []ResidueRef `json:"epitope,omitempty"`
}

// --- Design ---

type Design struct {
	ID            DesignID           `json:"id"`
	ProjectID     ProjectID          `json:"project_id"`
	PlanID        PlanID             `json:"plan_id"`
	Created       time.Time          `json:"created"`
	Origin        DesignOrigin       `json:"origin"`
	Application   Application        `json:"application"`
	Sequence      Sequence           `json:"sequence"`
	StructureFile string             `json:"structure_file,omitempty"`
	Scores        map[string]float64 `json:"scores"`
	LabResults    []ExperimentResult `json:"lab_results,omitempty"`
	Provenance    []ToolCallRef      `json:"provenance"`
	Tags          []string           `json:"tags,omitempty"`
	Notes         string             `json:"notes,omitempty"`
}

type DesignOrigin string

const (
	OriginBindCraft   DesignOrigin = "bindcraft"
	OriginRFDiffMPNN  DesignOrigin = "rfdiff_mpnn"
	OriginRFAntibody  DesignOrigin = "rfantibody"
	OriginChai2       DesignOrigin = "chai2"
	OriginRFDiff2MPNN DesignOrigin = "rfdiff2_ligandmpnn"
	OriginManual      DesignOrigin = "manual"
)

// ToolCallRef records the provenance of one tool invocation.
type ToolCallRef struct {
	CallID    string    `json:"call_id"`
	Tool      string    `json:"tool"`
	InputHash string    `json:"input_hash"`
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
}

// NewToolCallRef builds a provenance record for a tool call with the given input.
func NewToolCallRef(tool string, input []byte) ToolCallRef {
	sum := sha256.Sum256(input)
	return ToolCallRef{
		CallID:    uuid.NewString(),
		Tool:      tool,
		InputHash: hex.EncodeToString(sum[:]),
		Version:   version.String(),
		Timestamp: time.Now().UTC(),
	}
}

// --- Scoring ---

type FilterConfig struct {
	MinPLDDT         float64 `json:"min_plddt,omitempty"`
	MinPLDDTMin      float64 `json:"min_plddt_min,omitempty"`
	MinIPSAE         float64 `json:"min_ipsae,omitempty"`
	MaxPAEInterface  float64 `json:"max_pae_interface,omitempty"`
	MinIPTM          float64 `json:"min_iptm,omitempty"`
	MinPDockQ        float64 `json:"min_pdockq,omitempty"`
	MaxRMSDtoModel   float64 `json:"max_rmsd_to_model,omitempty"`
	MaxMotifRMSD     float64 `json:"max_motif_rmsd,omitempty"`
	MinRosettaScore  float64 `json:"min_rosetta_score,omitempty"`
	MaxESMPerplexity float64 `json:"max_esm_perplexity,omitempty"`
}

type DesignScore struct {
	DesignID DesignID           `json:"design_id"`
	Metrics  map[string]float64 `json:"metrics"`
}

// --- Plan ---

type DesignPlan struct {
	ID             PlanID       `json:"id"`
	ProjectID      ProjectID    `json:"project_id"`
	Created        time.Time    `json:"created"`
	Target         PDBReference `json:"target"`
	Application    Application  `json:"application"`
	Method         string       `json:"method"`
	FallbackMethod string       `json:"fallback_method,omitempty"`
	Filters        FilterConfig `json:"filters"`
	ShortlistSize  int          `json:"shortlist_size"`
	ComputeBackend string       `json:"compute_backend"`
	EstimatedCost  float64      `json:"estimated_cost_usd"`
	EstimatedTime  string       `json:"estimated_time"`
	Rationale      string       `json:"rationale"`
	EvidencePapers []PaperRef   `json:"evidence_papers,omitempty"`
	Approved       bool         `json:"approved"`
	ApprovedAt     *time.Time   `json:"approved_at,omitempty"`
}

type PaperRef struct {
	DOI   string `json:"doi,omitempty"`
	PMCID string `json:"pmcid,omitempty"`
	Title string `json:"title"`
	Year  int    `json:"year"`
	URL   string `json:"url,omitempty"`
}

// --- Corpus ---

// CorpusPaper is one paper in a project's per-project literature corpus.
type CorpusPaper struct {
	ID        string    `json:"id"` // DOI or PMCID — the corpus key
	ProjectID ProjectID `json:"project_id"`
	Title     string    `json:"title"`
	Authors   string    `json:"authors,omitempty"`
	Year      int       `json:"year,omitempty"`
	Source    string    `json:"source"`
	FullText  string    `json:"full_text,omitempty"`
	Metadata  string    `json:"metadata"`
	Added     time.Time `json:"added"`
}

// --- Job ---

type Job struct {
	ID              JobID      `json:"id"`
	Kind            JobKind    `json:"kind"`
	Tool            string     `json:"tool"`
	Status          JobStatus  `json:"status"`
	Created         time.Time  `json:"created"`
	Started         *time.Time `json:"started,omitempty"`
	Finished        *time.Time `json:"finished,omitempty"`
	Progress        float64    `json:"progress"`
	Backend         string     `json:"backend"`
	CostUSD         float64    `json:"cost_usd"`
	Input           []byte     `json:"input"`
	Output          []byte     `json:"output,omitempty"`
	Error           string     `json:"error,omitempty"`
	ProducedDesigns []DesignID `json:"produced_designs,omitempty"`
}

// --- Experiment (wet-lab) ---

type Experiment struct {
	ID          ExperimentID       `json:"id"`
	ProjectID   ProjectID          `json:"project_id"`
	Backend     string             `json:"backend"`
	ExternalID  string             `json:"external_id"`
	AssayType   string             `json:"assay_type"`
	TargetID    string             `json:"target_id"`
	TargetName  string             `json:"target_name"`
	Designs     []DesignID         `json:"designs"`
	SubmittedAt time.Time          `json:"submitted_at"`
	Status      string             `json:"status"`
	CostUSD     float64            `json:"cost_usd"`
	Results     []ExperimentResult `json:"results,omitempty"`
}

type ExperimentResult struct {
	DesignID        DesignID `json:"design_id"`
	Kd              *float64 `json:"kd,omitempty"`
	KdUnits         string   `json:"kd_units,omitempty"`
	Kon             *float64 `json:"kon,omitempty"`
	Koff            *float64 `json:"koff,omitempty"`
	BindingStrength string   `json:"binding_strength,omitempty"`
	RSquared        *float64 `json:"r_squared,omitempty"`
	NReplicates     int      `json:"n_replicates,omitempty"`
	IsControl       bool     `json:"is_control"`
}

// --- Session and messages ---

type Session struct {
	ID        SessionID `json:"id"`
	ProjectID ProjectID `json:"project_id"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	Model     string    `json:"model"`
	Provider  string    `json:"provider"`
}

type Message struct {
	ID         string     `json:"id"`
	SessionID  SessionID  `json:"session_id"`
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Created    time.Time  `json:"created"`
	Tokens     int        `json:"tokens"`
	CostUSD    float64    `json:"cost_usd"`
}

type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
