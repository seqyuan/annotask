#!/bin/bash
# Release script for midfile
# Usage: ./release.sh <version>
# Example: ./release.sh v1.9.6

if [ -z "$1" ]; then
    echo "Error: Version argument is required"
    echo "Usage: $0 <version>"
    echo "Example: $0 v1.9.6"
    exit 1
fi

version="$1"

# 验证版本格式（可选，检查是否以 v 开头）
if [[ ! "$version" =~ ^v ]]; then
    echo "Warning: Version should start with 'v' (e.g., v1.9.6)"
    read -p "Continue anyway? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

echo "Releasing version: $version"

# 执行 git 操作
git add -A && \
git commit -m "$version" && \
git tag "$version" && \
git push origin main && \
git push origin "$version"

if [ $? -eq 0 ]; then
    echo "Release $version completed successfully!"
else
    echo "Error: Release failed!"
    exit 1
fi
