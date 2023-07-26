# GL Migrator

Utility for migrating Gitlab Repo To Github (Including PR, labels)

## Installation

warn: Существует прямая зависимость от Git на устройстве.
Некоторые команды запускаются путём передачи в консоль команды типа "git clone --bare URL".
Убедитесь что Git установлен и находится в PATH.
warn: Утилита не работает с SSH урлами до репозиториев

Создать GL и GH токены
Создать GH репозиторий
Создать пустую папку
Закинуть в пустую папку бинарь
Запустить с обязательными флагами -s, -d, -p

## Флаги
```
Flags:
-s, --source string        Required. Source Url. Must be gitlab repo
-p, --pid int              Required. Source project ID
-d, --destination string   Required. Dest Url. Must be github repo
-h, --help                 help for gl-migrator
-r, --remove               Remove local repo before use and after use
-b, --defbranch            Change default branch to master/main/develop
```
## Алгоритм работы приложения:

1) Проверяет GL и GH Токены. Если их нет - запрашивает и сохраняет в (usr.HomeDir + gl-migrator-cfg.yaml) (Приложение само создаёт конфиг)
2) Отчищает папку (".")
3) Клонирует репу из source
4) Переименовывает клонированную папку в .git
5) git reflog expire --expire-unreachable=now --all
6) git gc --prune=now
7) Меняет origin на destination
8) Пушит в origin. RefSpec 'refs/heads/*:refs/heads/*' ( все ветки )
9) Если активен -b, ищет ветку master, устанавливает её как Default branch, если master ветки нет, то main, если нет, то develop
10) Получает список MR из GitLab
11) Получает список PR из GitHub
12) Получает список Tags из GitLab
13) Мигрирует MR'ы,
14) Мигрирует Issue's
15) Мигрирует Tags
16) Если активен -r, отчищает папку

Цикл обработки Merge Request'a, с целью создания из него PR, приложение:
1) Проверяет что MR имеет state=opened ( Не закрыт )
2) Проверяет список PRов на наличие в нём PR с именем MR ( проверяем что создаваемый PR не создан ранее, чтобы не дублироваться)
3) Проверяет что Merge Branches существуют
4) Создаёт PR
5) Получает список лейбла из MR
6) Проверяет их наличие в создаваемом PR, если их нет, проверяет существуют ли они, если нет, создаёт
7) Добавляет лейблы на PR
8) Добавляет комментарий в PR (main.go#L242)

Цикл обработки Issue's
1) Запрашивает все Issues по ProjectID из GitLab
2) Запрашивает все Issues по RepoName из GitHub
3) Сравнивает каждую Issue из Gitlab с Issue из Github, по Tittle, если есть совпадение, пропускает
4) Создаёт переменную NewIssue
5) Отправляет NewIssue в GitHub API

Цикл обработки Tags
1) Достаёт из GitLab Tag Имя, сообщение, commit_sha, Автора, дату создания, почту.
2) На основе данных полученных из GitLab Tag создаёт GitHub Tag
3) Создаёт ссылку на Tag. Если такая ссылка уже есть, пропускает

## NB

Если PRов много, то можно упереться в Rate Limit от GitLab, ( Как правило допустимо пушить 8 PR за раз ) ошибка:
403 You have exceeded a secondary rate limit and have been temporarily blocked from content creation. Please retry your request again later. []
Что делать? Ждать. И пробовать ещё

Лимит на получение открытых MR - 50 записей (main.go#L181)

## Пример запуска
```bash
gl-migrator.exe -s <Gitlab_repo_Url> -p <PrjID> -d <GitHub_repo_Url>
```

# TODO:
```
Сборка исполняемых файлов
```