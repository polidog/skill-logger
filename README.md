# skill-logger

Claude Code（および Codex）で使った **Skill** と **slash command** の利用状況を
SQLite/Turso に記録し、ターミナル UI で集計を眺めるためのツール。

- `skill-logger record` … hook から渡された JSON を読んで 1 件記録する
- `skill-logger stats`  … ランキング / 日次タイムラインを stdout に出す
- `skill-logger tui`    … Bubble Tea 製の TUI で 4 つのビューを切替表示
  - Skills / Commands / Daily / Recent
- `skill-logger sync`   … Turso embedded replicas を手動同期（ローカル SQLite では no-op）

データは既定で `~/.skill-logger/events.db` に保存される。`SKILL_LOGGER_DIR` で変更可。

## インストール

```sh
go install github.com/polidog/skill-logger@latest
```

Turso の Embedded Replicas を使いたい場合は CGO を有効にして `turso` ビルドタグ付きで:

```sh
CGO_ENABLED=1 go install -tags turso github.com/polidog/skill-logger@latest
```

このビルドで以下の環境変数が設定されていると Turso の Embedded Replica モードで動く:

| 変数 | 内容 |
| --- | --- |
| `TURSO_DATABASE_URL` | `libsql://<db>.turso.io` 形式の primary URL |
| `TURSO_AUTH_TOKEN` | Turso の auth token |

未設定なら同じバイナリでもローカル `events.db` だけで動く。

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
skill-logger tui
```

- `tab` / `←` `→` / `1`–`4`: ビュー切替
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

# 日次タイムライン (全件)
skill-logger stats --daily
```

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
