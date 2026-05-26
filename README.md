# skill-logger

Claude Code（および Codex）で使った **Skill** と **slash command** の利用状況を
SQLite/Turso に記録し、ターミナル UI で集計を眺めるためのツール。

- `skill-logger`        … Bubble Tea 製の TUI で 5 つのビューを切替表示
  - Skills / Commands / Projects / Daily / Recent
- `skill-logger record` … hook から渡された JSON を読んで 1 件記録する
- `skill-logger stats`  … ランキング / 日次タイムラインを stdout に出す
- `skill-logger sync`   … Turso embedded replicas を手動同期（ローカル SQLite では no-op）

データは既定で `~/.skill-logger/events.db` に保存される。設定は
`~/.skill-logger/config.toml`（任意）で変更可。

## インストール

`go-libsql` を内部で使うため CGO が必要:

```sh
CGO_ENABLED=1 go install github.com/polidog/skill-logger@latest
```

## Config (`~/.skill-logger/config.toml`)

config ファイルは **任意** で、無ければローカル SQLite モードで動く。
Turso の Embedded Replicas を使いたいときだけ作成する。

```toml
# "local" (default) または "turso"
mode = "turso"

# 省略時は <SKILL_LOGGER_DIR>/events.db (= ~/.skill-logger/events.db)
db_path = "~/.skill-logger/events.db"

[turso]
url = "libsql://<your-db>.turso.io"
auth_token = "..."           # env TURSO_AUTH_TOKEN が優先
sync_interval = "60s"        # 省略すると手動 sync のみ
```

設定の優先順位:

| 優先 | ソース | 説明 |
| --- | --- | --- |
| 1 | `--db` / `--config` CLI フラグ | コマンド単位で上書き |
| 2 | 環境変数 (`SKILL_LOGGER_DB`, `TURSO_DATABASE_URL`, `TURSO_AUTH_TOKEN`) | hook やシェルで一時的に切替 |
| 3 | `config.toml` | 通常の永続設定 |
| 4 | デフォルト | mode=local, `~/.skill-logger/events.db` |

`TURSO_DATABASE_URL` がセットされていると、config に `mode` が無くても自動的に turso モードになる。

## Claude Code の hook 設定

`~/.claude/settings.json` の `hooks` に以下を追加すると、Skill 呼び出しと
slash command 投入を自動で記録できる。`skill-logger` は失敗しても exit 0 を
返すので、hook が Claude Code 本体をブロックすることはない。

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Skill",
        "hooks": [
          { "type": "command", "command": "skill-logger record --quiet" }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          { "type": "command", "command": "skill-logger record --quiet" }
        ]
      }
    ]
  }
}
```

`skill-logger record` は stdin の JSON を見て自動的に種別を判定する:

- `PreToolUse` + `tool_name=Skill` → `kind=skill`, `name=tool_input.skill`
- `UserPromptSubmit` で `prompt` が `/` で始まる → `kind=command`, `name=<最初のトークン>`

それ以外の payload は無視 (exit 0)。`--source codex` で source 列を切り替えられる
ので、Codex 側でも同等の hook 仕組みがあれば同じバイナリで併用できる。

## 使い方

### TUI

```sh
skill-logger
```

- `tab` / `←` `→` / `1`–`5`: ビュー切替
- `↑` `↓` または `j` `k`: 行移動
- `r`: 再読込
- `f`: 期間切替 (All / 7d / 24h)
- `s`: source 切替 (All / Claude / Codex)
- `q` または `Ctrl+C`: 終了

### CLI 集計

```sh
# Skill ランキング上位 20 件
skill-logger stats --kind skill --limit 20

# 直近 7 日の slash command ランキング
skill-logger stats --kind command --since 7d

# プロジェクト (cwd) 別ランキング
skill-logger stats --by project

# 日次タイムライン (全件)
skill-logger stats --daily
```

`--by project` を付けると hook が記録した `cwd` ごとに集計する。表示時に
`git rev-parse --show-toplevel` を試してリポジトリのルートにまとめ、`$HOME`
配下は `~` 短縮で表示する (DB は cwd 生値のまま)。

`--since` は `30m` / `24h` / `7d` / `2w` のようなショートハンドか RFC3339 を受け付ける。

## データベーススキーマ

```sql
CREATE TABLE events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  ts         TEXT NOT NULL,       -- RFC3339Nano (UTC)
  source     TEXT NOT NULL,       -- claude | codex
  kind       TEXT NOT NULL,       -- skill | command
  name       TEXT NOT NULL,
  session_id TEXT NOT NULL DEFAULT '',
  cwd        TEXT NOT NULL DEFAULT '',
  raw        TEXT NOT NULL DEFAULT ''  -- 元の hook JSON (デバッグ用)
);
```

## ライセンス

MIT
