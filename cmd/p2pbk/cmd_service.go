package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"p2pbk/internal/app"
	"p2pbk/internal/secrets"
)

func newServiceCommand() *cobra.Command {
	var credentialFile string
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Install and control the native background service",
	}
	for _, action := range []string{"install", "start", "stop", "status", "uninstall"} {
		action := action
		child := &cobra.Command{
			Use:   action,
			Short: action + " the p2pbk native service",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg := currentConfig()
				output, err := app.ServiceAction(context.Background(), action, app.ServiceOptions{
					ConfigPath:     currentConfigPath(cfg),
					CredentialFile: credentialFile,
				})
				if output != "" {
					fmt.Fprintln(cmd.OutOrStdout(), output)
				}
				return err
			},
		}
		cmd.AddCommand(child)
	}
	credential := &cobra.Command{
		Use:   "credential write",
		Short: "Create a protected Windows service credential from P2PBK_REPOSITORY_PASSWORD",
		RunE: func(cmd *cobra.Command, args []string) error {
			if credentialFile == "" {
				return fmt.Errorf("--credential-file is required")
			}
			if err := app.WriteServiceCredential(credentialFile, secrets.FromEnv()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "service credential written")
			return nil
		},
	}
	cmd.AddCommand(credential)
	cmd.PersistentFlags().StringVar(&credentialFile, "credential-file", "", "OS-protected credential source or Windows DPAPI file")
	return cmd
}
