package cmd

import (
	"github.com/ryanmccool/static-mangal/color"
	"github.com/ryanmccool/static-mangal/config"
	"github.com/ryanmccool/static-mangal/key"
	"github.com/ryanmccool/static-mangal/style"
	"github.com/ryanmccool/static-mangal/where"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"golang.org/x/exp/slices"
	"os"
)

func init() {
	rootCmd.AddCommand(envCmd)
	envCmd.Flags().BoolP("set-only", "s", false, "only show variables that are set")
	envCmd.Flags().BoolP("unset-only", "u", false, "only show variables that are unset")

	envCmd.MarkFlagsMutuallyExclusive("set-only", "unset-only")
}

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Show available environment variables",
	Long:  `Show available environment variables.`,
	Run: func(cmd *cobra.Command, args []string) {
		setOnly := lo.Must(cmd.Flags().GetBool("set-only"))
		unsetOnly := lo.Must(cmd.Flags().GetBool("unset-only"))

		config.EnvExposed = append(config.EnvExposed, where.EnvConfigPath)
		slices.Sort(config.EnvExposed)
		for _, env := range config.EnvExposed {
			configKey := env
			sensitive := key.IsSensitive(configKey)
			if env != where.EnvConfigPath {
				env = config.EnvName(env)
			}
			value := os.Getenv(env)
			present := value != ""

			if setOnly || unsetOnly {
				if !present && setOnly {
					continue
				}

				if present && unsetOnly {
					continue
				}
			}

			cmd.Print(style.New().Bold(true).Foreground(color.Purple).Render(env))
			cmd.Print("=")

			if present {
				if sensitive {
					value = config.RedactedValue
				}
				cmd.Println(style.Fg(color.Green)(value))
			} else {
				cmd.Println(style.Fg(color.Red)("unset"))
			}
		}
	},
}
