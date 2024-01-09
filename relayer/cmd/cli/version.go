package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build vars, that must be passed at build time.
var (
	VersionTag = ""
	GitCommit  = ""
)

// VersionCommand returns a CLI command to interactively print the application binary version information.
func VersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the application binary version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Printf(`
Git Tag: %s,
Git Commit: %s,
`,
				VersionTag,
				GitCommit,
			)
			return nil
		},
	}
}
