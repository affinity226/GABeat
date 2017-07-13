BEATNAME=gabeat
#BEAT_DIR=github.com/GeneralElectric/GABeat
BEAT_DIR=github.com/affinity226/GABeat
SYSTEM_TESTS=false
TEST_ENVIRONMENT=false
ES_BEATS=./vendor/github.com/elastic/beats
#GOPACKAGES=$(shell glide novendor)
GOPACKAGES=$(shell go list ${BEAT_DIR}/... | grep -v /vendor/)
PREFIX?=.

# Path to the libbeat Makefile
-include $(ES_BEATS)/libbeat/scripts/Makefile

.PHONY: init
init:
	#glide update  --no-recursive
	glide update
	make update
	git init

# Initial beat setup
.PHONY: setup
setup: copy-vendor
	make update

# Copy beats into vendor directory
.PHONY: copy-vendor
copy-vendor:
	#mkdir -p vendor/github.com/elastic/
	#cp -R ${GOPATH}/src/github.com/elastic/beats vendor/github.com/elastic/
	#rm -rf vendor/github.com/elastic/beats/.git

.PHONY: git-init
git-init:
	git init
	git add README.md CONTRIBUTING.md
	git commit -m "Initial commit"
	git add LICENSE
	git commit -m "Add the LICENSE"
	git add .gitignore
	git commit -m "Add git settings"
	git add .
	git reset -- .travis.yml
	git commit -m "Add gabeat"
	git add .travis.yml
	git commit -m "Add Travis CI"

# This is called by the beats packer before building starts
.PHONY: before-build
before-build:
.PHONY: update-deps
update-deps:
	#glide update  --no-recursive
	glide update

# Collects all dependencies and then calls update
.PHONY: collect
collect:
	glide install
