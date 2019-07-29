#!/bin/bash

set -ex

# see if we have a new cheatsheet
# if other docs end up being generated automatically we can chuck in the relevant scripts here
go run scripts/generate_cheatsheet.go

# commit and push if we have a change
if [[ -z $(git status -s -- docs/*) ]]; then
  echo "no changes to commit in the docs directory"
  exit 0
fi

echo "committing updated docs"

git config user.name "lazydocker bot"
git config user.email "jessedduffield@gmail.com"

git checkout master # just making sure we're up to date
git pull
git add docs/*
git commit -m "update docs"
git push -u origin master