### Release configuration #################################
# Path to folder in S3 (without slash at end)
BUCKET = s3://dist.dividat.ch/releases/driver2

# where the BUCKET folder is accessible for getting updates (needs to end with a slash)
RELEASE_URL = https://dist.dividat.com/releases/driver2/


### Basic setup ###########################################
# Set GOPATH to repository path
CWD = $(dir $(realpath $(firstword $(MAKEFILE_LIST))))
GOPATH ?= $(CWD)

# Main source to build
SRC = ./src/dividat-driver/main.go

# Default location where built binary will be placed
OUT ?= bin/dividat-driver

# Only channel is main now
CHANNEL := main

VERSION := $(shell git describe --always HEAD)

CHECKSUM_SIGNING_CERT ?= ./keys/checksumsign.private.pem


### Simple build ##########################################
.PHONY: build
build:
		@./build.sh -i $(SRC) -o $(OUT) -v $(VERSION)


### Test suite ############################################
.PHONY: test
test: build
	npm install
	npm test


### Formatting ############################################
.PHONY: format
format:
	gofmt -w src/


### Helper to quickly run the driver
.PHONY: run
run: build
	$(OUT)

### Helper to start the recorder
.PHONY: record
record:
	@go run src/dividat-driver/recorder/main.go ws://localhost:8382/senso

### Helper to start the recorder for Flex
.PHONY: record-flex
record-flex:
	@go run src/dividat-driver/recorder/main.go ws://localhost:8382/flex

### Cross compilation #####################################
LINUX_BIN = bin/dividat-driver-linux-amd64
.PHONY: $(LINUX_BIN)
$(LINUX_BIN):
	nix develop '.#crossBuild.x86_64-linux' --command bash -c "VERBOSE=1 ./build.sh -i $(SRC) -o $(LINUX_BIN) -v $(VERSION) "

WINDOWS_BIN = bin/dividat-driver-windows-amd64.exe
.PHONY: $(WINDOWS_BIN)
$(WINDOWS_BIN):
	nix develop '.#crossBuild.x86_64-windows' --command bash -c "VERBOSE=1 ./build.sh -i $(SRC) -o $(WINDOWS_BIN) -v $(VERSION)"

crossbuild: $(LINUX_BIN) $(WINDOWS_BIN)

### macOS cross-compilation and app bundle #############

MACOS_ARM_BIN = bin/dividat-driver-darwin-arm64
.PHONY: $(MACOS_ARM_BIN)
$(MACOS_ARM_BIN):
	nix develop '.#crossBuild.darwin.aarch64' --command bash -c "VERBOSE=1 ./build.sh -i $(SRC) -o $@ -v $(VERSION)"

MACOS_X86_BIN = bin/dividat-driver-darwin-amd64
.PHONY: $(MACOS_X86_BIN)
$(MACOS_X86_BIN):
	nix develop '.#crossBuild.darwin.x86_64' --command bash -c "VERBOSE=1 ./build.sh -i $(SRC) -o $@ -v $(VERSION)"

MACOS_ARM_APP_BUNDLE = bin/DividatDriver-arm64.app
MACOS_ARM_APP_BUNDLE_ZIP = $(MACOS_ARM_APP_BUNDLE).zip
MACOS_X86_APP_BUNDLE = bin/DividatDriver-amd64.app
MACOS_X86_APP_BUNDLE_ZIP = $(MACOS_X86_APP_BUNDLE).zip

bin/DividatDriver-%.app: bin/dividat-driver-darwin-% macos/Info.plist macos/launcher
	mkdir -p $@/Contents/MacOS
	cp $< $@/Contents/MacOS/driver
	chmod +x $@/Contents/MacOS/driver
	cp macos/Info.plist $@/Contents/Info.plist
	cp macos/launcher $@/Contents/MacOS/launcher
	chmod +x $@/Contents/MacOS/launcher
	sudo codesign --deep --force --sign "-" $@

bin/DividatDriver-%.app.zip: bin/DividatDriver-%.app
	zip -r -y $@ $<

.PHONY: crossbuild_mac
crossbuild_mac: $(MACOS_X86_BIN) $(MACOS_ARM_BIN)

.PHONY: crossbuild_mac_bundles
crossbuild_mac_bundles: $(MACOS_X86_APP_BUNDLE_ZIP) $(MACOS_ARM_APP_BUNDLE_ZIP)

### Release ###############################################

RELEASE_DIR = release/$(CHANNEL)/$(VERSION)

# Helper to create signature
define write-signature
	openssl dgst -sha256 -sign $(CHECKSUM_SIGNING_CERT) $(1) | openssl base64 -A -out $(1).sig
	# make sure signature file exists and is non-zero
	test -s $(1).sig
endef

# write the pointer to the latest release
LATEST = release/$(CHANNEL)/latest
.PHONY: $(LATEST)
$(LATEST):
	mkdir -p `dirname $(LATEST)`
	echo $(VERSION) > $@
	$(call write-signature,$@)

LINUX_RELEASE = $(RELEASE_DIR)/$(notdir $(LINUX_BIN))-$(VERSION)
$(LINUX_RELEASE): $(RELEASE_DIR) $(LINUX_BIN)
	cp $(LINUX_BIN) $(LINUX_RELEASE)
	upx $(LINUX_RELEASE)
	$(call write-signature,$(LINUX_RELEASE))

#DARWIN_RELEASE = $(RELEASE_DIR)/$(notdir $(DARWIN_BIN))-$(VERSION)
#$(DARWIN_RELEASE): $(RELEASE_DIR) $(DARWIN_BIN)
	#cp bin/$(DARWIN_BIN) $(DARWIN_RELEASE)
	#upx $(DARWIN_RELEASE)
	#$ (call write-signature,$(DARWIN_RELEASE))

WINDOWS_RELEASE = $(RELEASE_DIR)/dividat-driver-windows-amd64-$(VERSION).exe
$(WINDOWS_RELEASE): $(RELEASE_DIR) $(WINDOWS_BIN)
	cp $(WINDOWS_BIN) $(WINDOWS_RELEASE)
	upx $(WINDOWS_RELEASE)
	$(call write-signature,$(WINDOWS_RELEASE))

$(RELEASE_DIR):
	mkdir -p $(RELEASE_DIR)

# sign and copy binaries to release folders
.PHONY: release
release: $(LINUX_RELEASE) $(WINDOWS_RELEASE) release/$(CHANNEL)/latest


### Deploy ################################################

SEMVER_REGEX = ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(\-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$

deploy: release
	# Check if version is in semver format
	[[ $(VERSION) =~ $(SEMVER_REGEX) ]]

	# Print information about channel heads and confirm
	@echo "Channel '$(CHANNEL)' is currently at:"
	@echo "  $(shell git show --oneline --decorate --quiet $$(curl -s "$(RELEASE_URL)$(CHANNEL)/latest" | tr -d '\n') | tail -1)"
	@echo
	@echo "About to deploy $(VERSION) to '$(CHANNEL)'. Proceed? [y/N]" && read ans && [ $${ans:-N} == y ]

	aws s3 cp $(RELEASE_DIR) $(BUCKET)/$(CHANNEL)/$(VERSION)/ --recursive \
		--acl public-read \
		--cache-control max-age=0
	aws s3 cp $(LATEST) $(BUCKET)/$(CHANNEL)/latest \
		--acl public-read \
		--cache-control max-age=0
	aws s3 cp $(LATEST).sig $(BUCKET)/$(CHANNEL)/latest.sig \
		--acl public-read \
		--cache-control max-age=0

clean:
	rm -rf release/
	rm -rf bin/
	go clean src/dividat-driver/main.go
