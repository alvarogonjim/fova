package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/alvarogonjim/proteus/internal/backends/local"
)

// localRegistry loads the embedded tool registry for the resolved Proteus home.
func localRegistry() (*local.Registry, error) {
	return local.LoadRegistry(proteusHome())
}

// newInstallCmd builds `proteus install <tool> [--dry-run] [--force] [--all]`.
func newInstallCmd() *cobra.Command {
	var dryRun, force, all bool
	cmd := &cobra.Command{
		Use:   "install <tool>",
		Short: "Install a local protein tool via uv",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := localRegistry()
			if err != nil {
				return err
			}
			inst := local.NewInstaller(reg)

			var targets []string
			switch {
			case all:
				for _, rec := range reg.Tools() {
					targets = append(targets, rec.Name)
				}
			case len(args) == 1:
				targets = []string{args[0]}
			default:
				return fmt.Errorf("specify a tool name or --all")
			}

			for _, name := range targets {
				if dryRun {
					steps, err := inst.DryRun(name)
					if err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "install %s:\n", name)
					for i, s := range steps {
						fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", i+1, s)
					}
					continue
				}
				if force {
					_ = inst.Remove(cmd.Context(), name)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "installing %s …\n", name)
				if err := inst.Install(cmd.Context(), name); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "installed %s\n", name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the install commands without running them")
	cmd.Flags().BoolVar(&force, "force", false, "wipe an existing install and reinstall")
	cmd.Flags().BoolVar(&all, "all", false, "install every tool")
	return cmd
}

// newUninstallCmd builds `proteus uninstall <tool>`.
func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <tool>",
		Short: "Remove an installed local protein tool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := localRegistry()
			if err != nil {
				return err
			}
			if err := local.NewInstaller(reg).Remove(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", args[0])
			return nil
		},
	}
}

// newListCmd builds `proteus list tools`.
func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installable resources",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "tools",
		Short: "List installable local protein tools and their status",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := localRegistry()
			if err != nil {
				return err
			}
			inst := local.NewInstaller(reg)
			for _, rec := range reg.Tools() {
				mark := "--"
				if inst.Status(rec.Name).Installed {
					mark = "ok"
				}
				gpu := ""
				if rec.RequiresGPU {
					gpu = " (GPU)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %s  %-14s %.1f GB%s\n",
					mark, rec.Name, rec.DiskGB, gpu)
			}
			return nil
		},
	})
	return cmd
}

// newDoctorCmd builds `proteus doctor`.
func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the local Proteus environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := localRegistry()
			if err != nil {
				return err
			}
			rep := local.Diagnose(reg, local.NewInstaller(reg))
			fmt.Fprint(cmd.OutOrStdout(), rep.String())
			return nil
		},
	}
}
