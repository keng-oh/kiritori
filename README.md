# キリトリ (kiritori)

YouTube動画の好きな場面を時間指定でクリップし、リンクで共有するWebサービス。
動画は保存せず、動画ID・開始/終了秒・タイトルだけを保存する。再生はYouTube公式の埋め込みプレーヤー。

## 方針

- Go（標準ライブラリ中心）+ PostgreSQL、画面はSSR（`html/template`）+ 少量のJS
- クリップは最長60秒、時間は整数秒
- 匿名でも作成できるが、編集・削除・一覧/ランキング掲載はログイン（Googleのみ）ユーザーのクリップのみ
- 匿名クリップは30日間再生されなければ自動削除
- ランキングは総視聴回数と直近7日の再生数（ホット）
- 将来Twitch Clipにも対応するため、`clips.platform` / `source_id` で動画ソースを抽象化する

## 開発

```
cp .env.example .env   # 初回のみ。POSTGRES_PASSWORD を変更し、DATABASE_URL も揃える
docker compose up -d   # PostgreSQL 起動（ホスト側ポートは POSTGRES_PORT、既定 5433）
set -a; . ./.env; set +a
make run               # 起動時に migration を自動適用。http://localhost:8080
make test              # DATABASE_URL があればDB統合テストも実行
make check             # CIと同じチェック（gofmt・go vet・テスト）
```

環境変数は `.env.example` を参照。

## 構成

```
cmd/kiritori/   エントリポイント
internal/       server, store, auth, youtube(oEmbed), jobs
migrations/     SQLマイグレーション
web/            templates, static
```
