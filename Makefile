.PHONY: release build
release:
	.scripts/make-release.sh
build: 
	echo "Building ..."
	.scripts/make-build.sh
