package main

import (
	"fmt"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"os"
)

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
