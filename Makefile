.PHONY: all build tidy install uninstall start stop restart status package \
	prepare-release build-release release-package release-checksum

APP_NAME := dnsdist-cert-sync
BIN_DIR := bin
BIN_PATH := $(BIN_DIR)/$(APP_NAME)
TARGET_OS ?= linux
TARGET_ARCH ?= amd64
RELEASE_DIR := release
RELEASE_NAME := $(APP_NAME)-offline-$(TARGET_OS)-$(TARGET_ARCH)
RELEASE_PATH := $(RELEASE_DIR)/$(RELEASE_NAME)
RELEASE_BIN := $(RELEASE_PATH)/$(APP_NAME)
RELEASE_TAR := $(RELEASE_NAME).tar.gz

PREFIX ?= /usr/local
INSTALL_BIN := $(PREFIX)/bin/$(APP_NAME)
ETC_DIR ?= /etc/dnsdist-cert-sync
SERVICE_DIR ?= /etc/systemd/system

all: build

build:
	install -d -m 755 $(BIN_DIR)
	GOWORK=off go build -o $(BIN_PATH) .
	@echo "Built $(BIN_PATH)"

tidy:
	GOWORK=off go mod tidy

install: build
	install -m 755 $(BIN_PATH) $(INSTALL_BIN)
	install -d -m 755 $(ETC_DIR)
	[ -f $(ETC_DIR)/config.yaml ] || install -m 644 config.prod.yaml $(ETC_DIR)/config.yaml
	[ -f $(ETC_DIR)/env ] || install -m 600 /dev/null $(ETC_DIR)/env
	install -m 644 dnsdist-cert-sync.service $(SERVICE_DIR)/dnsdist-cert-sync.service
	systemctl daemon-reload
	systemctl enable dnsdist-cert-sync
	@echo "Installed $(APP_NAME). Edit $(ETC_DIR)/config.yaml and $(ETC_DIR)/env then start service."

uninstall:
	systemctl stop dnsdist-cert-sync 2>/dev/null || true
	systemctl disable dnsdist-cert-sync 2>/dev/null || true
	rm -f $(SERVICE_DIR)/dnsdist-cert-sync.service $(INSTALL_BIN)
	systemctl daemon-reload

start:
	systemctl start dnsdist-cert-sync

stop:
	systemctl stop dnsdist-cert-sync

restart:
	systemctl restart dnsdist-cert-sync

status:
	systemctl status dnsdist-cert-sync

package:
	tar -czf $(APP_NAME)-standalone.tar.gz Makefile go.mod go.sum main.go config syncer config.prod.yaml dnsdist-cert-sync.service
	@echo "Created $(APP_NAME)-standalone.tar.gz"

prepare-release:
	install -d -m 755 $(RELEASE_PATH)

build-release: prepare-release
	GOWORK=off CGO_ENABLED=0 GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) go build -o $(RELEASE_BIN) .
	install -m 644 config.prod.yaml $(RELEASE_PATH)/config.prod.yaml
	install -m 644 dnsdist-cert-sync.service $(RELEASE_PATH)/dnsdist-cert-sync.service
	install -m 644 README.md $(RELEASE_PATH)/README.md
	@echo "Prepared release directory: $(RELEASE_PATH)"

release-package: build-release
	COPYFILE_DISABLE=1 COPY_EXTENDED_ATTRIBUTES_DISABLE=1 tar --no-xattrs -czf $(RELEASE_TAR) -C $(RELEASE_DIR) $(RELEASE_NAME)
	@echo "Created $(RELEASE_TAR)"

release-checksum: release-package
	shasum -a 256 $(RELEASE_TAR)
