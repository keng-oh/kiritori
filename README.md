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

### Google ログイン（任意）

`GOOGLE_CLIENT_ID` を設定したときだけログイン機能が有効になる（未設定なら `/login` は 404 で、ヘッダーにも出ない）。
取得するのは Google の `sub`（識別子）だけで、氏名・メールアドレスは要求も保存もしない。

1. [Google Cloud Console](https://console.cloud.google.com/) で OAuth 同意画面を作り、公開状態は「テスト」のまま、自分をテストユーザーに追加する
2. 「認証情報」から OAuth クライアント ID（種類: ウェブアプリケーション）を作る
3. 「承認済みのリダイレクト URI」に `http://localhost:8080/auth/callback` を登録する
4. `.env` に `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` を書き、`KIRITORI_BASE_URL=http://localhost:8080` を設定する

`KIRITORI_BASE_URL` に `/auth/callback` を足したものがリダイレクト URI になる。クライアントシークレットはコミットしない。

## 構成

```
cmd/kiritori/   エントリポイント
internal/       server, store, auth, youtube(oEmbed), jobs
migrations/     SQLマイグレーション
web/            templates, static
```
