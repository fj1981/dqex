#!/usr/bin/env bash
# 一键同步 dqex 到 Gitee 镜像：推送 main 分支 + 全部 tags。
# 用法：bash scripts/mirror-gitee.sh
# 认证二选一（不落盘）：
#   A) 设置环境变量 GITEE_TOKEN（私人令牌）：
#        GITEE_TOKEN=xxx bash scripts/mirror-gitee.sh
#   B) 本机 osxkeychain 已缓存 Gitee 凭据（先手动 push 一次）。
set -euo pipefail

GE_REMOTE="gitee"
GE_URL="https://gitee.com/fjcn/dqex.git"

cd "$(dirname "$0")/.."

# 确保 gitee remote 存在
if ! git remote get-url "$GE_REMOTE" >/dev/null 2>&1; then
  echo ">> 添加 remote $GE_REMOTE -> $GE_URL"
  git remote add "$GE_REMOTE" "$GE_URL"
fi

# 若提供 GITEE_TOKEN，则用一次性 URL（oauth2:token）推送，不写入 config；否则走本地缓存凭据
if [[ -n "${GITEE_TOKEN:-}" ]]; then
  GE_PUSH_URL="https://oauth2:${GITEE_TOKEN}@gitee.com/fjcn/dqex.git"
  echo ">> 使用 GITEE_TOKEN 认证"
else
  GE_PUSH_URL="$GE_URL"
  echo ">> 使用本地缓存的 Gitee 凭据"
fi

echo ">> 推送 main -> gitee/main"
git push "$GE_PUSH_URL" main

echo ">> 推送全部 tags -> gitee (含 v1.7.4 等)"
git push "$GE_PUSH_URL" --tags

echo ">> 同步完成。"
echo "   GitHub: https://github.com/fj1981/dqex"
echo "   Gitee : $GE_URL"