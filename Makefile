IMPORT_PREFIX=github.com/CoreumFoundation
ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
SCAN_FILES := $(shell find . -type f -name '*.go' -not -name '*mock.go' -not -name '*_gen.go' -not -path "*/vendor/*")

###############################################################################
###                               Development                               ###
###############################################################################

.PHONY: fmt
fmt:
	which gofumpt || @go install mvdan.cc/gofumpt@v0.5.0
	which gogroup || @go install github.com/vasi-stripe/gogroup/cmd/gogroup@v0.0.0-20200806161525-b5d7f67a97b5
	@gofumpt -lang=v2.1 -extra -w $(SCAN_FILES)
	@gogroup -order std,other,prefix=$(IMPORT_PREFIX) -rewrite $(SCAN_FILES)

.PHONY: lint
lint:
	crust lint/current-dir
