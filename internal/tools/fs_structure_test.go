package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFSReadStructurePDB(t *testing.T) {
	root := t.TempDir()
	pdb := "ATOM      1  N   MET A   1      11.104  13.207  10.000  1.00  0.00           N\n" +
		"ATOM      2  CA  MET A   1      12.000  13.000  10.000  1.00  0.00           C\n" +
		"ATOM      4  CA  LYS A   2      13.000  14.000  10.000  1.00  0.00           C\n" +
		"ATOM      5  CA  THR A   3      14.000  15.000  10.000  1.00  0.00           C\n"
	if err := os.WriteFile(filepath.Join(root, "t.pdb"), []byte(pdb), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := fsReadStructure{root: root}.Execute(context.Background(), []byte(`{"path":"t.pdb"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Chains     map[string]string `json:"chains"`
		ChainCount int               `json:"chain_count"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if out.ChainCount != 1 || out.Chains["A"] != "MKT" {
		t.Errorf("chains = %#v, chain_count = %d", out.Chains, out.ChainCount)
	}
}

func TestFSReadStructureMMCIF(t *testing.T) {
	root := t.TempDir()
	mmcif := "data_test\n" +
		"loop_\n" +
		"_atom_site.group_PDB\n" +
		"_atom_site.id\n" +
		"_atom_site.label_atom_id\n" +
		"_atom_site.label_comp_id\n" +
		"_atom_site.label_asym_id\n" +
		"_atom_site.label_seq_id\n" +
		"ATOM 1 N  MET A 1\n" +
		"ATOM 2 CA MET A 1\n" +
		"ATOM 4 CA LYS A 2\n" +
		"ATOM 5 CA THR A 3\n"
	if err := os.WriteFile(filepath.Join(root, "t.cif"), []byte(mmcif), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := fsReadStructure{root: root}.Execute(context.Background(), []byte(`{"path":"t.cif"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Chains map[string]string `json:"chains"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if out.Chains["A"] != "MKT" {
		t.Errorf("chains = %#v", out.Chains)
	}
}

func TestFSReadStructureUnsupportedExtension(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "t.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (fsReadStructure{root: root}).Execute(context.Background(), []byte(`{"path":"t.txt"}`)); err == nil {
		t.Fatal("expected an error for an unsupported extension")
	}
}

func TestFSReadStructureRejectsEscape(t *testing.T) {
	if _, err := (fsReadStructure{root: t.TempDir()}).Execute(context.Background(), []byte(`{"path":"../escape.pdb"}`)); err == nil {
		t.Fatal("expected an error for a path escaping the workspace")
	}
}
