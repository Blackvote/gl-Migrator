# GL Migrator

A simple tool for managing Protobuf dependencies.

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

1) Проверяем GL и GH Токены. Если их нет - запрашиваем и сохраняем в usr.HomeDir + gl-migrator-cfg.yaml)
2) Отчищаем папку (".")
3) Клонируем репу из source
4) Переименовываем клонированную папку в .git
5) git reflog expire --expire-unreachable=now --all
6) git gc --prune=now
7) Меняем origin на destination
8) Пушим репо из .git в origin. RefSpec "refs/heads/*:refs/heads/*"


## Пример запуска
```bash
gl-migrator.exe -s https://git.netsrv.it/neo/ggpoker.git -p 252 -d https://github.com/deeplay-io/trainer-ggpoker.git
```