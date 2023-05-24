# GL Migrator

Utility for migrating Gitlab Repo To Github (Including PR, labels)

## Installation

Надо скачать бинарь TODO: Добавить место где они будут храниться

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
```
## Алгоритм работы:

1) Проверяем GL и GH Токены. Если их нет - запрашиваем и сохраняем в (usr.HomeDir + gl-migrator-cfg.yaml)
2) Отчищаем папку (".")
3) Клонируем репу из source
4) Переименовываем клонированную папку в .git
5) git reflog expire --expire-unreachable=now --all
6) git gc --prune=now
7) Меняем origin на destination
8) Пушим в origin. RefSpec "refs/heads/*:refs/heads/*"
9) Получаем список MR из GitLab
10) Получаем список PR из GitHub
11) Мигрируем MR'ы
11.1) Проверяем что MR открыт
11.2) Проверяем что Merge Branches существуют
11.3) Создаём PR
11.4) Получаем список лейбла из MR
11.5) Проверяем их наличие в создаваемом PR, если их нет, проверяем существуют ли они, если нет, создаём
11.6) Добавляем лейблы на PR
11.7) Добавляем комментарий в PR (main.go#L242)

## NB

Если PRов много, то можно упереться в Rate Limit от GitLab, ( Как правило допустимо пушить 8 PR за раз ) ошибка:
403 You have exceeded a secondary rate limit and have been temporarily blocked from content creation. Please retry your request again later. []
Что делать? Ждать.

Лимит на получение открытых MR - 50 записей (main.go#L181)

## Пример запуска
```bash
gl-migrator.exe -s https://git.netsrv.it/neo/ggpoker.git -p 252 -d https://github.com/deeplay-io/trainer-ggpoker.git
```