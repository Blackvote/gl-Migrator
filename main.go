package main

import (
	"fmt"
	"github.com/go-git/go-git/v5"
	"github.com/manifoldco/promptui"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

const (
	finalGitDir    = ".git"
	configFileName = "gl-migrator-cfg"
)

var (
	sourceURL, // репозиторий в Gitlab, который нужно перенести в Github
	destinationURL, // пустой репозиторий в Github
	ghToken, // Токены
	glToken,
	pushToken,
	pullToken string // для передачи в Push
)

var rootCmd = &cobra.Command{
	Use:   "gl-migrator",
	Short: "migrate GL repo to GH",
	Run: func(cmd *cobra.Command, args []string) {

		usr, err := user.Current()
		if err != nil {
			panic(err)
		}

		// Конфигурация токенов
		viper.SetConfigName(configFileName)
		viper.SetConfigType("yaml")
		viper.AddConfigPath(usr.HomeDir)
		if err := viper.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); ok {
				// Config file not found; ignore error if desired
			} else {
				panic(err)
			}
		}
		if viper.GetString("credentials.github.pat") == "" && ghToken == "" {
			viper.Set("credentials.github.pat", getPAT())
			if err := viper.WriteConfigAs(filepath.Join(usr.HomeDir, configFileName+".yaml")); err != nil {
				fmt.Println("Error while saving config: " + err.Error())
			}
		}
		if viper.GetString("credentials.gitlab.pat") == "" && glToken == "" {
			viper.Set("credentials.gitlab.pat", getGLToken())
			if err := viper.WriteConfigAs(filepath.Join(usr.HomeDir, configFileName+".yaml")); err != nil {
				fmt.Println("Error while saving config: " + err.Error())
			}
		}
		if ghToken == "" {
			ghToken = viper.GetString("credentials.github.pat")
		}
		if glToken == "" {
			glToken = viper.GetString("credentials.gitlab.pat")
		}

		// Выбор токена для Push
		containsGithub := strings.Contains(destinationURL, "github")
		if containsGithub {
			pushToken = ghToken
			pullToken = glToken
		} else {
			pushToken = glToken
			pushToken = ghToken
		}

		// Получаем имя итоговой директории
		parts := strings.Split(sourceURL, "/")
		gitDir := parts[len(parts)-1]

		// Клонируем репу
		println("Removing dir content")
		removeRepo()

		if strings.HasPrefix(sourceURL, "https://") {
			sourceURL = strings.Replace(sourceURL, "https://", "", 1)
		}

		clone := exec.Command("git", "clone", "--bare", "https://oauth2:"+pullToken+"@"+sourceURL)
		output, err := clone.Output()
		println(string(output))
		if err != nil {
			log.Fatalf("Failed to clone: %v", err)
		}

		fmt.Printf("Renaming %v to %v\n", gitDir, finalGitDir)
		err = os.Rename(gitDir, finalGitDir)
		if err != nil {
			fmt.Println(err)
			return
		}

		fmt.Println("Reflog + GC")
		reflog := exec.Command("git", "reflog", "expire", "--expire-unreachable=now --all")
		output, err = reflog.Output()
		println(string(output))
		if err != nil {
			log.Fatalf("Failed to clean up reflogs: %v", err)
		}

		gc := exec.Command("git", "gc", "--prune=now")
		output, err = gc.Output()
		println(string(output))
		if err != nil {
			log.Fatalf("Failed to gc: %v", err)
		}

		// Получаем содержимое папки .git как набор параметров
		r, err := git.PlainOpen(".")
		if err != nil {
			log.Fatalf("Failed to open local repo: %v", err)
		}

		// Получаем конфиг репозитория
		cfg, err := r.Config()
		if err != nil {
			log.Fatalf("Failed to get repo config: %v", err)
		}

		// Получаем origin
		remote, ok := cfg.Remotes["origin"]
		if !ok {
			log.Fatal("Remote 'origin' not found")
		}

		fmt.Printf("Setting up origin-url from %v to %v\n", remote.URLs, "["+destinationURL+"]")
		// Меняем origin.url
		remote.URLs = []string{destinationURL}
		err = r.SetConfig(cfg)
		if err != nil {
			log.Fatalf("Failed to set remote: %v", err)
		}

		fmt.Println("Pushing to origin")
		pushRepo(finalGitDir, pushToken)

	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&sourceURL, "source", "s", "", "Source Url")
	rootCmd.PersistentFlags().StringVarP(&destinationURL, "destination", "d", "", "Dest Url")
	rootCmd.Flags().BoolP("remove", "r", false, "Remove local repo before use and after use")
	err := rootCmd.MarkPersistentFlagRequired("source")
	if err != nil {
		fmt.Println("Ошибка при установке обязательного флага:", err)
	}
	err = rootCmd.MarkPersistentFlagRequired("destination")
	if err != nil {
		fmt.Println("Ошибка при установке обязательного флага:", err)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	//if rootCmd.Flag("remove").Value.String() == "true" {
	//	removeRepo()
	//}
}

func getPAT() string {
	prompt := promptui.Prompt{
		Label: "Enter your github PAT",
		Validate: func(input string) error {
			if input == "" {
				return errors.New("PAT is required")
			}
			return nil
		},
		Mask: '*',
	}
	ghToken, err := prompt.Run()
	if err != nil {
		panic(err)
	}
	return ghToken
}

func getGLToken() string {
	prompt := promptui.Prompt{
		Label: "Enter your gitlab token",
		Validate: func(input string) error {
			if input == "" {
				return errors.New("gitlab token is required")
			}
			return nil
		},
		Mask: '*',
	}
	glToken, err := prompt.Run()
	if err != nil {
		panic(err)
	}
	return glToken
}
