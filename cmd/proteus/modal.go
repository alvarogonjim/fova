package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/alvarogonjim/proteus/internal/backends/modal"
)

// newModalCmd builds `proteus modal deploy`.
func newModalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "modal",
		Short: "Manage the Modal compute backend",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "deploy",
		Short: "Write and deploy the Proteus Modal functions app",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := filepath.Join(proteusHome(), "modal")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			path := filepath.Join(dir, "functions.py")
			if err := os.WriteFile(path, []byte(modal.FunctionsPy), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)

			if _, err := exec.LookPath("modal"); err != nil {
				return fmt.Errorf("the Modal CLI is not installed — run `pip install modal` "+
					"and `modal token new`, then deploy with: modal deploy %s", path)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "deploying to Modal …")
			dep := exec.CommandContext(cmd.Context(), "modal", "deploy", path)
			dep.Stdout = cmd.OutOrStdout()
			dep.Stderr = cmd.ErrOrStderr()
			if err := dep.Run(); err != nil {
				return fmt.Errorf("modal deploy failed: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(),
				"deployed. Set PROTEUS_MODAL_ENDPOINT to the web-endpoint URL Modal printed.")
			return nil
		},
	})
	return cmd
}
