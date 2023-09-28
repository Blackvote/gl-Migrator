package main

import (
	"fmt"
	"github.com/go-git/go-git/v5"
	"github.com/google/go-github/v37/github"
	"github.com/manifoldco/promptui"
	"github.com/pkg/errors"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/xanzy/go-gitlab"
	"golang.org/x/net/context"
	"log"
	"net/url"
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
	sourceURL, // Репозиторий в Gitlab, который нужно перенести в Github
	destinationURL, // Пустой репозиторий в Github
	ghToken, // Токены
	glToken string // Для передачи в Push\Pull
)

var rootCmd = &cobra.Command{
	Use:   "gl-migrator",
	Short: "migrate GL repo to GH",
	Run: func(cmd *cobra.Command, args []string) {

		_, err := url.ParseRequestURI(sourceURL)
		if err != nil {
			log.Fatalf("Source must be a valid URL %v", err)
		}
		_, err = url.ParseRequestURI(destinationURL)
		if err != nil {
			log.Fatalf("Destination must be a valid URL. %v", err)
		}

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

		// Получаем имя итоговой директории
		parts := strings.Split(sourceURL, "/")
		gitDir := parts[len(parts)-1]
		dir, err := os.Getwd()
		if err != nil {
			fmt.Println("Working dir setup error", err)
		}
		log.Printf("Working setup to: \"%s\"", dir)

		removeRepo(dir)

		if strings.HasPrefix(sourceURL, "https://") {
			sourceURL = strings.Replace(sourceURL, "https://", "", 1)
		}

		log.Printf("Cloning Repo \"%s\"", sourceURL)
		clone := exec.Command("git", "clone", "--bare", "https://oauth2:"+glToken+"@"+sourceURL)
		output, err := clone.Output()
		if string(output) != "" {
			fmt.Println(string(output))
		}
		if err != nil {
			log.Fatalf("Failed to clone: %v", err)
		}

		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			gitDir = gitDir + ".git"
		}
		log.Printf("Renaming %v to %v", gitDir, finalGitDir)
		err = os.Rename(gitDir, finalGitDir)
		if err != nil {
			log.Fatalf("Failed to rename: %v", err)
		}

		log.Println("Reflog + GC")
		reflog := exec.Command("git", "reflog", "expire", "--expire-unreachable=now --all")
		output, err = reflog.Output()
		if string(output) != "" {
			fmt.Println(string(output))
		}
		if err != nil {
			log.Fatalf("Failed to clean up reflogs: %v", err)
		}

		gc := exec.Command("git", "gc", "--prune=now")
		output, err = gc.Output()
		if string(output) != "" {
			fmt.Println(string(output))
		}
		if err != nil {
			log.Fatalf("Failed to gc: %v", err)
		}

		// Получаем содержимое папки .git как набор параметров
		log.Println("Validate cloned repo")
		r, err := git.PlainOpen(".")
		if err != nil {
			log.Fatalf("Failed to open local repo: %v", err)
		}

		headRef, err := r.Head()
		if err != nil {
			log.Fatalf("HEAD getting error: %v\n", err)
		}
		newDefaultBranch := headRef.Name().Short()

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

		log.Printf("Setting up origin-url from %v to %v\n", "https://"+sourceURL, destinationURL)
		// Меняем origin.url
		remote.URLs = []string{destinationURL}
		err = r.SetConfig(cfg)
		if err != nil {
			log.Fatalf("Failed to set remote: %v", err)
		}

		srcParts := strings.Split(sourceURL, "/")
		gitlabURL := "https://" + srcParts[len(srcParts)-3]
		srcRepoGroup := srcParts[len(srcParts)-2]
		srcRepo := srcParts[len(srcParts)-1]
		srcRepoName := strings.Replace(srcRepo, ".git", "", 1)

		dstParts := strings.Split(destinationURL, "/")
		owner := dstParts[len(dstParts)-2]
		dstRepo := dstParts[len(dstParts)-1]
		dstRepo = strings.Replace(dstRepo, ".git", "", 1)

		githubClient := getGitHubClient(ghToken)
		gitlabClient, err := gitlab.NewClient(glToken, gitlab.WithBaseURL(gitlabURL))

		// Получаем PID
		log.Println("Getting remote repo PID")
		projectListOptions := &gitlab.ListProjectsOptions{
			ListOptions: gitlab.ListOptions{
				PerPage: 1, // 100 - доступный максимум
			},
			Search: &srcRepoName,
		}

		project, _, err := gitlabClient.Projects.ListProjects(projectListOptions)
		if err != nil {
			log.Fatal(err)
		}
		if len(project) == 0 {
			log.Fatal("Cant find project")
		}
		projectID := project[0].ID

		log.Printf("PID = %d", projectID)

		log.Println("Pushing to origin")
		//pushRepo(finalGitDir, ghToken)

		log.Printf("Setting default branсh to %s\n", newDefaultBranch)
		_, _, err = githubClient.Repositories.Edit(context.Background(), owner, dstRepo, &github.Repository{
			DefaultBranch: &newDefaultBranch,
		})
		if err != nil {
			log.Fatal(err)
		}

		var allMergeRequests []*gitlab.MergeRequest
		getMrOption := &gitlab.ListProjectMergeRequestsOptions{
			ListOptions: gitlab.ListOptions{
				PerPage: 100, // 100 - доступный максимум
			},
		}
		for {
			mergeRequests, response, err := gitlabClient.MergeRequests.ListProjectMergeRequests(projectID, getMrOption)
			if err != nil {
				log.Fatal(err)
			}

			allMergeRequests = append(allMergeRequests, mergeRequests...)

			if response.CurrentPage >= response.TotalPages {
				break
			}

			getMrOption.Page = response.NextPage
		}

		existingPullRequests, _, err := githubClient.PullRequests.List(context.Background(), owner, dstRepo, &github.PullRequestListOptions{})
		processedPullRequests := make(map[string]bool)
		for _, pr := range existingPullRequests {
			processedPullRequests[pr.GetTitle()] = true
		}

		//Создание PR
		for _, mergeRequest := range allMergeRequests {
			if processedPullRequests[mergeRequest.Title] {
				fmt.Printf("Merge request \"%s\" already exist. Skip it.\n", mergeRequest.Title)
				continue
			}
			fmt.Printf("Merge request \"%s\" Not Exist. Cheking branch...\n", mergeRequest.Title)
			_, resp, err := githubClient.Repositories.GetBranch(context.Background(), owner, dstRepo, mergeRequest.SourceBranch, false)
			if resp.StatusCode == 404 {
				fmt.Printf("Cannot create PR. Source branch(%s) does not exist\n", mergeRequest.SourceBranch)
				continue
			} else if err != nil {
				fmt.Printf("Cannot get Source branch. Does it exist? (%s)\n", mergeRequest.SourceBranch)
			}

			_, resp, err = githubClient.Repositories.GetBranch(context.Background(), owner, dstRepo, mergeRequest.TargetBranch, false)
			if resp.StatusCode == 404 {
				fmt.Printf("Cannot create PR. Target branch(%s) does not exist\n", mergeRequest.TargetBranch)
			} else if err != nil {
				fmt.Printf("Cannot get Target branch. Does it exist? (%s)\n", mergeRequest.TargetBranch)
			}
			fmt.Printf("Branch exists. Creating PR...\n")
			pullRequest, _, err := createPullRequest(githubClient, owner, dstRepo, mergeRequest)
			if err != nil {
				log.Println(err)
			} else {
				labels, err := getMergeRequestLabels(gitlabClient, cast.ToInt(projectID), mergeRequest.IID)
				if err != nil {
					log.Println(err)
				}

				addLabelsToPullRequest(githubClient, owner, dstRepo, pullRequest, labels)

				assignee := ""
				if mergeRequest.Assignee != nil {
					assignee = mergeRequest.Assignee.Username
				}

				MergeRequestURL := fmt.Sprintf(gitlabURL+"/%s/%s/-/merge_requests/%d", srcRepoGroup, srcRepo, mergeRequest.IID)
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
		// Получение Issues из Gitlab
		gitlabIssues, _, err := gitlabClient.Issues.ListProjectIssues(projectID, &gitlab.ListProjectIssuesOptions{})
		if err != nil {
			print(err)
		}

		// Взято из chat.openai.com
		// Разворачиваем срез с Issue'ами GitLab для сохранения порядка
		// предполагая, что порядок основан на времени создания, поэтому обрабатываем их от старых к новым
		reverseGitLabIssues(gitlabIssues)

		// Получение Issues из GitHub
		githubIssues, _, err := githubClient.Issues.ListByRepo(context.Background(), owner, dstRepo, &github.IssueListByRepoOptions{})
		if err != nil {
			print(err)
		}

		// Мапа с Tittle'ами Issus'ов из Github для сравнения
		githubIssueTitles := make(map[string]bool)
		for _, issue := range githubIssues {
			githubIssueTitles[strings.ToLower(*issue.Title)] = true
		}

		// Получаем содержимое Issues для отправки GitHub
		for _, issue := range gitlabIssues {
			title := issue.Title
			body := issue.Description

			if githubIssueTitles[strings.ToLower(title)] {
				fmt.Printf("GitHub issue with title '%s' already exists, skipping...\n", title)
				continue
			}

			// Создание GitHub issue
			newIssue := &github.IssueRequest{
				Title: &title,
				Body:  &body,
			}
			_, _, err = githubClient.Issues.Create(context.Background(), owner, dstRepo, newIssue)
			if err != nil {
				fmt.Printf("Failed to create GitHub issue for GitLab issue #%d: %v\n", issue.IID, err)
				continue
			}
			fmt.Printf("Successfully migrated GitLab issue #%d to GitHub\n", issue.IID)
		}

		fmt.Println("Get Gitlab Tags")
		gitlabTags, err := getGitLabTags(projectID, gitlabClient)
		if err != nil {
			log.Fatalf("Failed to get tags from GitLab: %v", err)
		}

		fmt.Println("Create Github Tags")
		err = createGitHubTags(context.Background(), *githubClient, owner, dstRepo, gitlabTags)
		if err != nil {
			log.Fatalf("Failed to create tags in GitHub: %v", err)
		}

	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&sourceURL, "source", "s", "", "Required. Source Url. Must be gitlab repo")
	rootCmd.PersistentFlags().StringVarP(&destinationURL, "destination", "d", "", "Required. Dest Url. Must be github repo")
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
		_, err := fmt.Println(os.Stderr, err)
		if err != nil {
			fmt.Println("Some things fatal(main.cmd):", err)
			return
		}
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
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

func reverseGitLabIssues(issues []*gitlab.Issue) {
	for i, j := 0, len(issues)-1; i < j; i, j = i+1, j-1 {
		issues[i], issues[j] = issues[j], issues[i]
	}
}
