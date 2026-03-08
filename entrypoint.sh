#!/bin/bash
set -e

git config --global user.name "${GIT_USER_NAME:-FemtoClaw Bot}"
git config --global user.email "${GIT_USER_EMAIL:-femtoclaw@bot.local}"

if [ -n "$GITHUB_TOKEN" ]; then
	git config --global credential.helper '!f() { echo "password=${GITHUB_TOKEN}"; }; f'
	git config --global url."https://${GITHUB_TOKEN}@github.com/".insteadOf "https://github.com/"
fi

exec /usr/local/bin/femtoclaw
