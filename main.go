package main

import (
	"context"
	"fmt"
	"github.com/go-git/go-git/v5"
	github "github.com/google/go-github/v37/github"
	"github.com/manifoldco/promptui"
	"github.com/pkg/errors"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/xanzy/go-gitlab"
	"golang.org/x/oauth2"
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
	owner          = "deeplay-io"
)

var (
	sourceURL, // Репозиторий в Gitlab, который нужно перенести в Github
	destinationURL, // Пустой репозиторий в Github
	ghToken, // Токены
	glToken,
	pushToken,
	pullToken string // Для передачи в Push\Pull
	projectID int // ID проекта в GitLab
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

		println("Removing dir content")
		removeRepo()

		if strings.HasPrefix(sourceURL, "https://") {
			sourceURL = strings.Replace(sourceURL, "https://", "", 1)
		}

		println("Cloning Repo")
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

		fmt.Printf("Setting up origin-url from %v to %v\n", "https://"+sourceURL, destinationURL)
		// Меняем origin.url
		remote.URLs = []string{destinationURL}
		err = r.SetConfig(cfg)
		if err != nil {
			log.Fatalf("Failed to set remote: %v", err)
		}

		fmt.Println("Pushing to origin")

		pushRepo(finalGitDir, pushToken)
		githubClient := getGitHubClient(ghToken)

		srcParts := strings.Split(sourceURL, "/")
		srcRepoGroup := srcParts[len(srcParts)-2]
		srcRepo := srcParts[len(srcParts)-1]
		srcRepo = strings.Replace(srcRepo, ".git", "", 1)

		dstParts := strings.Split(destinationURL, "/")
		dstRepo := dstParts[len(dstParts)-1]
		dstRepo = strings.Replace(dstRepo, ".git", "", 1)

		if cmd.Flag("defbranch").Value.String() == "true" {
			newDefaultBranch := ""
			fmt.Println("Try to find master branch")
			_, resp, err := githubClient.Repositories.GetBranch(context.Background(), owner, dstRepo, "master", false)
			if resp.StatusCode == 404 {
				fmt.Printf("Cannot create PR. Source branch does not exist\n")
			} else if err != nil {
				fmt.Println("Cannot get Source branch. Does it exist? master")
			} else {
				fmt.Println("Master branch was found.")
				newDefaultBranch = "master"
			}

			if newDefaultBranch == "" {
				fmt.Println("Try to find main branch")
				_, resp, err := githubClient.Repositories.GetBranch(context.Background(), owner, dstRepo, "main", false)
				if resp.StatusCode == 404 {
					fmt.Printf("Cannot create PR. Source branch does not exist\n")
				} else if err != nil {
					fmt.Println("Cannot get Source branch. Does it exist? master")
				} else {
					fmt.Println("Main branch was found.")
					newDefaultBranch = "main"
				}
			}

			if newDefaultBranch == "" {
				fmt.Println("Try to find develop branch")
				_, resp, err := githubClient.Repositories.GetBranch(context.Background(), owner, dstRepo, "main", false)
				if resp.StatusCode == 404 {
					fmt.Printf("Cannot create PR. Source branch does not exist\n")
				} else if err != nil {
					fmt.Println("Cannot get Source branch. Does it exist? master")
				} else {
					fmt.Println("develop branch was found.")
					newDefaultBranch = "develop"
				}
			}
			fmt.Printf("Setting default bransh to %s\n", newDefaultBranch)
			_, _, err = githubClient.Repositories.Edit(context.Background(), owner, dstRepo, &github.Repository{
				DefaultBranch: &newDefaultBranch,
			})
			if err != nil {
				log.Fatal(err)
			}
		}

		gitlabClient, err := gitlab.NewClient(glToken, gitlab.WithBaseURL("https://git.netsrv.it/api/v4"))

		if err != nil {
			log.Fatal(err)
		}

		state := "opened"
		getMrOption := &gitlab.ListProjectMergeRequestsOptions{
			State: &state,
			ListOptions: gitlab.ListOptions{
				Page:    1,
				PerPage: 50,
			},
		}
		mergeRequests, _, err := gitlabClient.MergeRequests.ListProjectMergeRequests(projectID, getMrOption)
		if err != nil {
			log.Fatal(err)
		}

		existingPullRequests, _, err := githubClient.PullRequests.List(context.Background(), owner, dstRepo, &github.PullRequestListOptions{
			State: "open",
		})

		processedPullRequests := make(map[string]bool)
		for _, pr := range existingPullRequests {
			processedPullRequests[pr.GetTitle()] = true
		}

		// Создание PR
		for _, mergeRequest := range mergeRequests {
			if mergeRequest.State != "opened" {
				continue
			} else {
				if processedPullRequests[mergeRequest.Title] {
					fmt.Printf("Merge request \"%s\" already exist. Skip it.\n", mergeRequest.Title)
					continue
				} else {
					fmt.Printf("Merge request \"%s\" Not Exist. Cheking branch...\n", mergeRequest.Title)
					_, resp, err := githubClient.Repositories.GetBranch(context.Background(), owner, dstRepo, mergeRequest.SourceBranch, false)
					if resp.StatusCode == 404 {
						fmt.Printf("Cannot create PR. Source branch does not exist\n")
						return
					} else if err != nil {
						fmt.Printf("Cannot get Source branch. Does it exist? (%s)\n", mergeRequest.SourceBranch)
						return
					}

					_, resp, err = githubClient.Repositories.GetBranch(context.Background(), owner, dstRepo, mergeRequest.TargetBranch, false)
					if resp.StatusCode == 404 {
						fmt.Printf("Cannot create PR. Target branch does not exist\n")
						return
					} else if err != nil {
						fmt.Printf("Cannot get Target branch. Does it exist? (%s)\n", mergeRequest.TargetBranch)
						return
					}
					fmt.Printf("Branch exists. Creating PR...\n")
					pullRequest, _, err := createPullRequest(githubClient, dstRepo, mergeRequest)
					if err != nil {
						log.Println(err)
					} else {
						labels, err := getMergeRequestLabels(gitlabClient, cast.ToInt(projectID), mergeRequest.IID)
						if err != nil {
							log.Println(err)
						}

						addLabelsToPullRequest(githubClient, dstRepo, pullRequest, labels)

						assignee := ""
						if mergeRequest.Assignee != nil {
							assignee = mergeRequest.Assignee.Username
						}

						MergeRequestURL := fmt.Sprintf("https://git.netsrv.it/%s/%s/-/merge_requests/%d", srcRepoGroup, srcRepo, mergeRequest.IID)
						comment := fmt.Sprintf("Migrated from GitLab.\nAt GitLab was been assigned to: **@%s**\n%s", assignee, MergeRequestURL)
						_, _, err = githubClient.Issues.CreateComment(context.Background(), owner, dstRepo, pullRequest.GetNumber(), &github.IssueComment{
							Body: github.String(comment),
						})
						if err != nil {
							log.Printf("Error adding comment to pull request %d: %v\n", pullRequest.GetNumber(), err)
						} else {
							fmt.Printf("Comment added to pull request %d\n", pullRequest.GetNumber())
						}
					}
					continue
				}
			}

		}
		if cmd.Flag("remove").Value.String() == "true" {
			println("Removing dir content")
			removeRepo()
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&sourceURL, "source", "s", "", "Required. Source Url. Must be gitlab repo")
	rootCmd.PersistentFlags().StringVarP(&destinationURL, "destination", "d", "", "Required. Dest Url. Must be github repo")
	rootCmd.PersistentFlags().IntVarP(&projectID, "pid", "p", 0, "Required. Source project ID")
	rootCmd.Flags().BoolP("remove", "r", false, "Remove local repo before use and after use")
	rootCmd.Flags().BoolP("defbranch", "b", false, "Change default branch to master/main/develop")
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
		_, err := fmt.Fprintln(os.Stderr, err)
		if err != nil {
			fmt.Println("Some things fatal(main.cmd):", err)
			return
		}
		if err != nil {

		}
		os.Exit(1)
	}

}

func getPAT() string {
	prompt := promptui.Prompt{
		Label: "Enter your github Personal Access Token",
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

func getGitHubClient(token string) *github.Client {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	return github.NewClient(tc)
}

func createPullRequest(client *github.Client, repo string, mergeRequest *gitlab.MergeRequest) (*github.PullRequest, *github.Response, error) {

	title := mergeRequest.Title
	body := mergeRequest.Description
	head := mergeRequest.SourceBranch
	base := mergeRequest.TargetBranch

	newPullRequest := &github.NewPullRequest{
		Title: &title,
		Body:  &body,
		Head:  &head,
		Base:  &base,
	}

	pullRequest, response, err := client.PullRequests.Create(context.Background(), owner, repo, newPullRequest)
	if err != nil {
		return nil, response, err
	}
	return pullRequest, response, nil
}

func getMergeRequestLabels(client *gitlab.Client, projectID, mergeRequestID int) ([]*gitlab.Label, error) {
	mr, _, err := client.MergeRequests.GetMergeRequest(projectID, mergeRequestID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get merge request: %v", err)
	}

	labels := []*gitlab.Label{}
	for _, label := range mr.Labels {
		labels = append(labels, &gitlab.Label{
			Name:        label,
			Description: "",
			Color:       "",
		})
	}

	return labels, nil
}

func addLabelsToPullRequest(client *github.Client, repo string, pullRequest *github.PullRequest, labels []*gitlab.Label) {

	// Get the existing labels in the GitHub repository
	existingLabels, _, err := client.Issues.ListLabels(context.Background(), owner, repo, nil)
	if err != nil {
		log.Printf("Error retrieving existing labels: %v\n", err)
		return
	}

	// Create a map of existing labels
	existingLabelsMap := make(map[string]bool)
	for _, l := range existingLabels {
		existingLabelsMap[*l.Name] = true
	}

	// Add labels to the pull request if they don't exist
	for _, label := range labels {
		// Check if the label exists in GitHub
		_, ok := existingLabelsMap[label.Name]
		if !ok {
			// Create the label in GitHub
			newLabel := &github.Label{
				Name:        &label.Name,
				Description: nil, // Set a nil description
				Color:       nil, // Set a nil color
			}
			fmt.Printf("Crate label %s", &label.Name)
			_, _, err := client.Issues.CreateLabel(context.Background(), owner, repo, newLabel)
			if err != nil {
				log.Printf("Error creating label %s: %v\n", label.Name, err)
				continue
			}

			fmt.Printf("Label %s created and added to pull request %d\n", label.Name, pullRequest.GetNumber())
		}

		// Add the label to the pull request
		fmt.Printf("Adding label to PR %s", label.Name)
		_, _, err := client.Issues.AddLabelsToIssue(context.Background(), owner, repo, pullRequest.GetNumber(), []string{label.Name})
		if err != nil {
			log.Printf("Error adding label %s to pull request %d: %v\n", label.Name, pullRequest.GetNumber(), err)
			continue
		}

		fmt.Printf("Label %s added to pull request %d\n", label.Name, pullRequest.GetNumber())
	}
}
