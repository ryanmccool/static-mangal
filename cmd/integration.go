package cmd

import (
	"fmt"
	"github.com/AlecAivazis/survey/v2"
	"github.com/ryanmccool/static-mangal/config"
	"github.com/ryanmccool/static-mangal/icon"
	"github.com/ryanmccool/static-mangal/integration/anilist"
	"github.com/ryanmccool/static-mangal/key"
	"github.com/ryanmccool/static-mangal/log"
	"github.com/ryanmccool/static-mangal/open"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	rootCmd.AddCommand(integrationCmd)
	integrationCmd.AddCommand(integrationAnilistCmd)
	integrationAnilistCmd.Flags().BoolP("disable", "d", false, "Disable Anilist integration")
}

var integrationCmd = &cobra.Command{
	Use:   "integration",
	Short: "Integration with other services",
	Long:  `Integration with other services`,
}

var askAnilist = survey.AskOne

var integrationAnilistCmd = &cobra.Command{
	Use:   "anilist",
	Short: "Integration with Anilist",
	Long: `Integration with Anilist.
See https://github.com/ryanmccool/static-mangal/tree/main/docs for current integration documentation`,
	Run: func(cmd *cobra.Command, args []string) {
		if lo.Must(cmd.Flags().GetBool("disable")) {
			viper.Set(key.AnilistEnable, false)
			viper.Set(key.AnilistCode, "")
			viper.Set(key.AnilistSecret, "")
			viper.Set(key.AnilistID, "")
			log.Info("Anilist integration disabled")
			handleErr(config.WriteExisting())
		}

		if !viper.GetBool(key.AnilistEnable) {
			confirm := survey.Confirm{
				Message: "Anilist is disabled. Enable?",
				Default: false,
			}
			var response bool
			err := askAnilist(&confirm, &response)
			handleErr(err)

			if !response {
				return
			}

			viper.Set(key.AnilistEnable, response)
			err = config.Write()
			if err != nil {
				handleErr(err)
				log.Error(err)
			}
		}

		if viper.GetString(key.AnilistID) == "" {
			input := survey.Input{
				Message: "Anilsit client ID is not set. Please enter it:",
				Help:    "",
			}
			var response string
			err := askAnilist(&input, &response)
			handleErr(err)

			if response == "" {
				return
			}

			viper.Set(key.AnilistID, response)
			err = config.WriteExisting()
			handleErr(err)
		}

		if config.SensitiveValue(key.AnilistSecret) == "" {
			input := anilistSecretPrompt()
			var response string
			err := askAnilist(input, &response)
			handleErr(err)

			if response == "" {
				return
			}

			viper.Set(key.AnilistSecret, response)
			err = config.WriteExisting()
			handleErr(err)
		}

		if config.SensitiveValue(key.AnilistCode) == "" {
			authURL := anilist.New().AuthURL()
			confirmOpenInBrowser := survey.Confirm{
				Message: "Open browser to authenticate with Anilist?",
				Default: false,
			}

			var openInBrowser bool
			err := askAnilist(&confirmOpenInBrowser, &openInBrowser)
			if err == nil && openInBrowser {
				err = open.Start(authURL)
			}

			if err != nil || !openInBrowser {
				fmt.Println("Please open the following URL in your browser:")
				fmt.Println(authURL)
			}

			input := anilistCodePrompt()

			var response string
			err = askAnilist(input, &response)
			handleErr(err)

			if response == "" {
				return
			}

			viper.Set(key.AnilistCode, response)
			err = config.WriteExisting()
			handleErr(err)
		}

		fmt.Printf("%s Anilist integration was set up\n", icon.Get(icon.Success))
	},
}

func anilistSecretPrompt() *survey.Password {
	return &survey.Password{
		Message: "Anilsit client secret is not set. Please enter it:",
		Help:    "",
	}
}

func anilistCodePrompt() *survey.Password {
	return &survey.Password{
		Message: "Anilsit code is not set. Please copy it from the link and paste in here:",
		Help:    "",
	}
}
