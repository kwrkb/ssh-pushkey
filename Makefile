# ssh-pushkey 開発用タスク
#
# 統合テストは2つの env ファイルから認証情報を読み込む（いずれも gitignore 済み）:
#   .env.integration       … 管理者アカウント (例: pushadmin)
#   .env.integration.user  … 一般アカウント   (例: pushuser)
# 各ファイルは `export SSH_TEST_HOST=... / SSH_TEST_USER=... / SSH_TEST_PASSWORD=...` を定義する。
# ターゲットが自分でファイルを読み込むため、手動 `source` は不要。

.PHONY: build vet test itest itest-admin itest-user itest-all check help

# ビルド（version は dev 固定）
build:
	go build -ldflags "-X main.version=dev" ./...

# 静的解析
vet:
	go vet ./...

# ユニットテスト
test:
	go test ./...

# 統合テスト（管理者アカウント）。`make itest` のエイリアス。
# 特定テストのみ: make itest RUN=TestIntegration_AdminDetection
itest: itest-admin

itest-admin:
	@$(call run_itest,.env.integration,管理者)

itest-user:
	@$(call run_itest,.env.integration.user,一般)

# 管理者→一般の順で両アカウントを通す
itest-all: itest-admin itest-user

# 指定 env ファイルを読み込んで統合テストを実行する。$(1)=ファイル, $(2)=ラベル
#
# env ファイルが 1Password の `op://` 参照を含む場合、シェルの `.`（source）では
# 解決できないため `op run --env-file` に委譲する。op が無ければ従来どおり source する。
define run_itest
	if [ ! -f $(1) ]; then \
		echo "ERROR: $(1) がありません（$(2)アカウント用の認証情報）。"; \
		echo "  export SSH_TEST_HOST=127.0.0.1 / SSH_TEST_USER=... / SSH_TEST_PASSWORD=... を記述してください。"; \
		echo "  パスワードは op://<vault>/<item>/<field> 形式の 1Password 参照でも可（op CLI 必須）。"; \
		exit 1; \
	fi; \
	if grep -q 'op://' $(1); then \
		if ! command -v op >/dev/null 2>&1; then \
			echo "ERROR: $(1) が op:// 参照を含みますが op CLI がありません。"; \
			echo '  1Password CLI を導入して op signin 済みにしてください。'; \
			exit 1; \
		fi; \
		echo "=> 統合テスト（$(2): op run で $(1) を解決）"; \
		op run --env-file=$(1) -- go test -tags=integration -v $(if $(RUN),-run "$(RUN)",) ./...; \
	else \
		set -a; . ./$(1); set +a; \
		: "$${SSH_TEST_HOST:?$(1) に SSH_TEST_HOST が未定義}"; \
		: "$${SSH_TEST_USER:?$(1) に SSH_TEST_USER が未定義}"; \
		: "$${SSH_TEST_PASSWORD:?$(1) に SSH_TEST_PASSWORD が未定義}"; \
		echo "=> 統合テスト（$(2): $$SSH_TEST_USER@$$SSH_TEST_HOST）"; \
		go test -tags=integration -v $(if $(RUN),-run "$(RUN)",) ./...; \
	fi
endef

# PR 前チェック（build tag 付きファイルのコンパイル崩れ検知含む）
check: build vet test
	go build -tags=integration ./...
	@echo "=== all checks passed ==="

help:
	@echo "make build       - ビルド"
	@echo "make vet         - go vet"
	@echo "make test        - ユニットテスト"
	@echo "make itest       - 統合テスト（管理者アカウント / .env.integration）"
	@echo "make itest-user  - 統合テスト（一般アカウント / .env.integration.user）"
	@echo "make itest-all   - 両アカウントで統合テスト"
	@echo "make check       - PR 前チェック一式"
	@echo "  RUN=<TestName> で特定テストのみ実行可"
