package main

import (
	"context"
	"fmt"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v37/github"
	"github.com/xanzy/go-gitlab"
	"golang.org/x/oauth2"
	"log"
	"os"
	"strings"
)

func getGitHubClient(token string) *github.Client {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	return github.NewClient(tc)
}

func pushRepo(gitDir, token string) {
	// Открываем репозиторий
	repo, err := git.PlainOpen(gitDir)
	if err != nil {
		fmt.Println("Open repo err:", err)
		return
	}

	// Получаем конфигурацию репозитория
	cfg, err := repo.Config()
	if err != nil {
		fmt.Println("Repo config err:", err)
		return
	}

	// Получаем удаленный репозиторий "origin"
	remote, ok := cfg.Remotes["origin"]
	if !ok {
		fmt.Println("Origin not found")
		return
	}

	// Определяем аутентификационные данные
	creds := &http.BasicAuth{
		Username: "mustNotBeEmpty",
		Password: token,
	}

	// Определяем опцию для отправки всех веток
	opts := &git.PushOptions{
		RemoteName: remote.Name,
		Auth:       creds,
		RefSpecs:   []config.RefSpec{"refs/heads/*:refs/heads/*"},
	}

	// Отправляем изменения
	err = repo.Push(opts)
	if err != nil {
		fmt.Println("Push fail:", err)
		return
	}

}

func removeRepo() {
	files, err := os.ReadDir(".")
	if err != nil {
		fmt.Println("Workdir error:", err)
		return
	}

	// Удаление файлов и поддиректорий
	for _, file := range files {
		err = os.RemoveAll(file.Name())
		if err != nil {
			fmt.Printf("Error while deleted %s: %v\n", file.Name(), err)
		} else {
			fmt.Printf("Deleted: %s\n", file.Name())
		}
	}
}

func createPullRequest(client *github.Client, owner, repo string, mergeRequest *gitlab.MergeRequest) (*github.PullRequest, *github.Response, error) {

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
			Description: "Migrated",
			Color:       "#FCA326",
		})
	}

	return labels, nil
}

func addLabelsToPullRequest(client *github.Client, owner, repo string, pullRequest *github.PullRequest, labels []*gitlab.Label) {

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

func getGitLabTags(projectId int, gitlabClient *gitlab.Client) ([]*gitlab.Tag, error) {

	tags, _, err := gitlabClient.Tags.ListTags(projectId, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get tags from GitLab: %v", err)
	}

	return tags, nil
}

func createGitHubTags(ctx context.Context, githubClient github.Client, owner, repo string, tags []*gitlab.Tag) error {

	for _, tag := range tags {
		tagName := tag.Name
		newTag := &github.Tag{
			Tag:     &tagName,
			Message: &tag.Message,
			Tagger: &github.CommitAuthor{
				Date:  tag.Commit.AuthoredDate,
				Name:  &tag.Commit.AuthorName,
				Email: &tag.Commit.CommitterEmail,
			},
			Object: &github.GitObject{
				Type: github.String("commit"),
				SHA:  &tag.Commit.ID,
			},
		}

		_, _, err := githubClient.Git.CreateTag(ctx, owner, repo, newTag)
		if err != nil {
			fmt.Errorf("failed to create tag in GitHub: %v", err)
			continue
		}

		ref := fmt.Sprintf("refs/tags/%s", tagName)
		reference := &github.Reference{
			Ref:    github.String(ref),
			Object: &github.GitObject{SHA: &tag.Commit.ID},
		}
		_, _, err = githubClient.Git.CreateRef(ctx, owner, repo, reference)
		if err != nil {
			if strings.Contains(err.Error(), "Reference already exists") {
				// Тег уже существует в GitHub, пропускаем его создание
				fmt.Printf("Tag '%s' already exists in GitHub. Skipping...\n", tagName)
				continue
			}
			return fmt.Errorf("failed to create tag reference in GitHub: %v", err)
		}

		fmt.Printf("Tag '%s' successfully created in GitHub.\n", tagName)
	}

	return nil
}
