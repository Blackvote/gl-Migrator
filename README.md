# GL Migrator

A simple tool for managing Protobuf dependencies.

## Installation

Надо скачать бинарь TODO: Добавить место где они будут храниться

## Флаги
```
-d, --destination string   Dest Url
-h, --help                 help for gl-migrator
-r, --remove               Remove local repo before use and after use
-s, --source string        Source Url
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
