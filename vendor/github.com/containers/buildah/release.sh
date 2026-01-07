#!/bin/sh
#
# Cut a buildah release.  Usage:
#
#   $ hack/release.sh <version> <next-version>
#
# For example:
#
#   $ hack/release.sh 1.2.3 1.3.0
#
# for "I'm cutting 1.2.3, and want to use 1.3.0-dev for future work".

VERSION="$1"
NEXT_VERSION="$2"
DATE=$(date '+%Y-%m-%d')
LAST_TAG=$(git describe --tags --abbrev=0)

write_go_version()
{
	LOCAL_VERSION="$1"
	sed -i "s/^\(.*Version = \"\).*/\1${LOCAL_VERSION}\"/" define/types.go
}

write_makefile_epoch()
{
	LOCAL_EPOCH="$1"
	sed -i "s/^\(EPOCH_TEST_COMMIT ?= \).*/\1${LOCAL_EPOCH}/" Makefile
}

write_changelog()
{
	echo "- Changelog for v${VERSION} (${DATE})" >.changelog.txt &&
	git log --no-merges --format='  * %s' "${LAST_TAG}..HEAD" >>.changelog.txt &&
	echo >>.changelog.txt &&
	cat changelog.txt >>.changelog.txt &&
	mv -f .changelog.txt changelog.txt

	echo "
## v${VERSION} (${DATE})
" >.CHANGELOG.md &&
	git log --no-merges --format='    %s' "${LAST_TAG}..HEAD" >>.CHANGELOG.md &&
	sed -i -e '/# Changelog/r .CHANGELOG.md'  CHANGELOG.md &&
	rm -f .CHANGELOG.md
}

release_commit()
{
	write_go_version "${VERSION}" &&
	write_changelog &&
	git commit -asm "Bump to v${VERSION}

[NO TESTS NEEDED]
"
}

dev_version_commit()
{
	write_go_version "${NEXT_VERSION}-dev" &&
	git commit -asm "Bump to v${NEXT_VERSION}-dev

[NO TESTS NEEDED]
"
}

epoch_commit()
{
	LOCAL_EPOCH="$1"
	write_makefile_epoch "${LOCAL_EPOCH}" &&
	git commit -asm 'Bump gitvalidation epoch

	[NO TESTS NEEDED]
'
}

git fetch origin &&
git checkout -b "bump-${VERSION}" origin/main &&
EPOCH=$(git rev-parse HEAD) &&
release_commit &&
git tag -s -m "version ${VERSION}" "v${VERSION}" &&
dev_version_commit &&
epoch_commit "${EPOCH}"
