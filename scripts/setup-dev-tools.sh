#!/usr/bin/env bash
# ssh-pushkey の build / test / lint に必要なツールを確認し、足りないものを入れる。
#
#   必須: go (go.mod の toolchain 行を満たすもの), make
#   任意: govulncheck（依存更新時の脆弱性確認）, goreleaser（.goreleaser.yaml 検証）
#   参考表示のみ: gh / glab / op（リリース・統合テスト用。自動インストールしない）
#
# 使い方: scripts/setup-dev-tools.sh [--check]   (--check は確認のみ)
set -euo pipefail

cd "$(dirname "$0")/.."
CHECK_ONLY=0
[[ "${1:-}" == "--check" ]] && CHECK_ONLY=1

missing=0
ok()   { printf '  [ok]      %s\n' "$*"; }
miss() { printf '  [missing] %s\n' "$*"; missing=1; }
info() { printf '  [info]    %s\n' "$*"; }

want_toolchain=$(awk '/^toolchain /{print $2}' go.mod)
want_go=${want_toolchain:-go$(awk '/^go /{print $2}' go.mod)}

echo "== 必須ツール"
command -v make >/dev/null && ok "make" || miss "make (apt-get install make)"

if ! command -v go >/dev/null; then
  miss "go ($want_go が必要: https://go.dev/dl/)"
else
  local_go=$(GOTOOLCHAIN=local go env GOVERSION)
  # go コマンドは go.mod の toolchain 行を見て自動ダウンロードする（GOTOOLCHAIN=auto）
  if eff=$(go env GOVERSION 2>/dev/null); then
    ok "go (local=$local_go, effective=$eff)"
  else
    miss "go toolchain $want_go (local=$local_go。自動ダウンロード失敗: proxy.golang.org / storage.googleapis.com への到達性を確認)"
  fi
fi

install_go_tool() { # name module
  if command -v "$1" >/dev/null || [[ -x "$(go env GOPATH 2>/dev/null)/bin/$1" ]]; then
    ok "$1"
  elif (( CHECK_ONLY )); then
    miss "$1 (go install $2)"
  else
    echo "  -> go install $2"
    if go install "$2"; then ok "$1 (installed to $(go env GOPATH)/bin)"; else miss "$1 (go install 失敗)"; fi
  fi
}

echo "== 任意ツール（CLAUDE.md の手順で使用）"
if command -v go >/dev/null && go env GOVERSION >/dev/null 2>&1; then
  install_go_tool govulncheck golang.org/x/vuln/cmd/govulncheck@latest
  install_go_tool goreleaser  github.com/goreleaser/goreleaser/v2@latest
else
  miss "govulncheck / goreleaser (Go toolchain が使えないためスキップ)"
fi

echo "== 参考（リリース・統合テスト用。自動インストールしない）"
for t in gh glab op; do
  command -v "$t" >/dev/null && ok "$t" || info "$t なし（必要時のみ手動で導入）"
done

case ":$PATH:" in *":$(go env GOPATH 2>/dev/null)/bin:"*) ;; *)
  command -v go >/dev/null && info "PATH に $(go env GOPATH)/bin を追加してください" ;;
esac

exit $missing
