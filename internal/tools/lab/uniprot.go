package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/transport"
)

// uniprotSearchURL is the canonical UniProtKB search endpoint. Overridable
// through UniProtClient.BaseURL for tests.
const uniprotSearchURL = "https://rest.uniprot.org/uniprotkb/search"

// UniProtRecord is the subset of a UniProtKB hit lab.targets_search exposes.
type UniProtRecord struct {
	Accession    string   `json:"accession"`
	Name         string   `json:"name"`
	Organism     string   `json:"organism,omitempty"`
	Sequence     string   `json:"sequence"`
	Length       int      `json:"length"`
	PDBCrossRefs []string `json:"pdb_cross_refs,omitempty"`
}

// UniProtClient resolves gene/protein names to a canonical UniProtKB entry.
// It is a thin wrapper around transport.Client so retries and telemetry
// are shared with the PDB path.
type UniProtClient struct {
	BaseURL string
	tc      *transport.Client
}

// NewUniProtClient builds a client that uses the shared transport.Client.
func NewUniProtClient(tc *transport.Client) *UniProtClient {
	return &UniProtClient{BaseURL: uniprotSearchURL, tc: tc}
}

// uniprotSearchResponse mirrors the relevant fields of the UniProtKB JSON
// search response.
type uniprotSearchResponse struct {
	Results []struct {
		PrimaryAccession   string `json:"primaryAccession"`
		ProteinDescription struct {
			RecommendedName struct {
				FullName struct {
					Value string `json:"value"`
				} `json:"fullName"`
			} `json:"recommendedName"`
		} `json:"proteinDescription"`
		Organism struct {
			ScientificName string `json:"scientificName"`
		} `json:"organism"`
		Sequence struct {
			Value  string `json:"value"`
			Length int    `json:"length"`
		} `json:"sequence"`
		UniProtKBCrossReferences []struct {
			Database string `json:"database"`
			ID       string `json:"id"`
		} `json:"uniProtKBCrossReferences"`
	} `json:"results"`
}

// Search returns the top reviewed UniProtKB record matching name. Returns
// (nil, nil) when no record is found.
func (u *UniProtClient) Search(ctx context.Context, name string) (*UniProtRecord, error) {
	if u == nil || u.tc == nil {
		return nil, fmt.Errorf("uniprot: client not initialised")
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("uniprot: name is required")
	}
	q := url.Values{}
	// Restrict to reviewed Swiss-Prot entries; keep the response small.
	q.Set("query", fmt.Sprintf("%s AND reviewed:true", name))
	q.Set("format", "json")
	q.Set("size", "1")
	q.Set("fields", "accession,protein_name,organism_name,sequence,xref_pdb")

	endpoint := u.BaseURL
	if endpoint == "" {
		endpoint = uniprotSearchURL
	}
	req, err := http.NewRequest(http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := u.tc.Do(ctx, req, "uniprot")
	if err != nil {
		return nil, fmt.Errorf("uniprot: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("uniprot returned %d: %s", resp.StatusCode,
			strings.TrimSpace(string(body)))
	}
	var raw uniprotSearchResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("uniprot decode: %w", err)
	}
	if len(raw.Results) == 0 {
		return nil, nil
	}
	r := raw.Results[0]
	var pdb []string
	for _, xref := range r.UniProtKBCrossReferences {
		if strings.EqualFold(xref.Database, "PDB") && xref.ID != "" {
			pdb = append(pdb, xref.ID)
		}
	}
	return &UniProtRecord{
		Accession:    r.PrimaryAccession,
		Name:         r.ProteinDescription.RecommendedName.FullName.Value,
		Organism:     r.Organism.ScientificName,
		Sequence:     r.Sequence.Value,
		Length:       r.Sequence.Length,
		PDBCrossRefs: pdb,
	}, nil
}

// NewSharedTransport returns a transport.Client with the fova User-Agent.
// Lab tools share this so retries and telemetry land consistently.
func NewSharedTransport(opts ...transport.Option) *transport.Client {
	return transport.New(append([]transport.Option{
		transport.WithPerCallTimeout(30 * time.Second),
	}, opts...)...)
}
