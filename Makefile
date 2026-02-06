# Raven Terminal Makefile
# Provides easy build, install, uninstall, and dependency management

.PHONY: all build install install-local uninstall uninstall-local clean deps deps-check help

# Application info
APP_NAME := raven-terminal
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Directories
SCRIPTS_DIR := scripts
BUILD_DIR := .

# Colors for output
BLUE := \033[0;34m
GREEN := \033[0;32m
YELLOW := \033[1;33m
RED := \033[0;31m
NC := \033[0m

# Detect OS and package manager
OS := $(shell uname -s)
DISTRO := unknown
PKG_MANAGER := unknown

ifeq ($(OS),Linux)
    ifneq ($(wildcard /etc/arch-release),)
        DISTRO := arch
        PKG_MANAGER := pacman
    else ifneq ($(wildcard /etc/debian_version),)
        DISTRO := debian
        PKG_MANAGER := apt
    else ifneq ($(wildcard /etc/fedora-release),)
        DISTRO := fedora
        PKG_MANAGER := dnf
    else ifneq ($(wildcard /etc/redhat-release),)
        DISTRO := rhel
        PKG_MANAGER := dnf
    else ifneq ($(wildcard /etc/opensuse-release),)
        DISTRO := opensuse
        PKG_MANAGER := zypper
    else ifneq ($(wildcard /etc/alpine-release),)
        DISTRO := alpine
        PKG_MANAGER := apk
    endif
else ifeq ($(OS),Darwin)
    DISTRO := macos
    PKG_MANAGER := brew
endif

# Default target
all: build

# Build the application
build:
	@echo -e "$(BLUE)[INFO]$(NC) Building $(APP_NAME)..."
	@go build -o $(APP_NAME) ./src
	@chmod +x $(APP_NAME)
	@echo -e "$(GREEN)[OK]$(NC) Build successful: ./$(APP_NAME)"

# Install system-wide (clean install - uninstalls first)
install: build
	@echo -e "$(BLUE)[INFO]$(NC) Performing clean global install..."
	@$(SCRIPTS_DIR)/uninstall.sh --all --force 2>/dev/null || true
	@$(SCRIPTS_DIR)/install.sh --global

# Install for current user (clean install - uninstalls first)
install-local: build
	@echo -e "$(BLUE)[INFO]$(NC) Performing clean user install..."
	@$(SCRIPTS_DIR)/uninstall.sh --all --force 2>/dev/null || true
	@$(SCRIPTS_DIR)/install.sh --user

# Uninstall all installations (clean uninstall)
uninstall:
	@echo -e "$(BLUE)[INFO]$(NC) Uninstalling all raven-terminal installations..."
	@$(SCRIPTS_DIR)/uninstall.sh --all --force

# Uninstall user installation
uninstall-local:
	@echo -e "$(BLUE)[INFO]$(NC) Uninstalling user installation..."
	@$(SCRIPTS_DIR)/uninstall.sh --user --force

# Uninstall everything including config
uninstall-all:
	@echo -e "$(BLUE)[INFO]$(NC) Uninstalling all installations and config..."
	@$(SCRIPTS_DIR)/uninstall.sh --all --config --force

# Clean build artifacts
clean:
	@echo -e "$(BLUE)[INFO]$(NC) Cleaning build artifacts..."
	@rm -f $(APP_NAME)
	@go clean
	@echo -e "$(GREEN)[OK]$(NC) Clean complete"

# Check if dependencies are installed
deps-check:
	@echo -e "$(BLUE)[INFO]$(NC) Checking dependencies..."
	@echo -e "$(BLUE)[INFO]$(NC) Detected OS: $(OS) ($(DISTRO))"
	@echo -e "$(BLUE)[INFO]$(NC) Package manager: $(PKG_MANAGER)"
	@echo ""
	@missing=""; \
	if ! command -v go >/dev/null 2>&1; then \
		echo -e "$(RED)[MISSING]$(NC) Go compiler"; \
		missing="$$missing go"; \
	else \
		echo -e "$(GREEN)[OK]$(NC) Go compiler: $$(go version | awk '{print $$3}')"; \
	fi; \
	if ! command -v pkg-config >/dev/null 2>&1; then \
		echo -e "$(RED)[MISSING]$(NC) pkg-config"; \
		missing="$$missing pkg-config"; \
	else \
		echo -e "$(GREEN)[OK]$(NC) pkg-config"; \
	fi; \
	if ! pkg-config --exists gl 2>/dev/null; then \
		echo -e "$(RED)[MISSING]$(NC) OpenGL development libraries"; \
		missing="$$missing opengl"; \
	else \
		echo -e "$(GREEN)[OK]$(NC) OpenGL libraries"; \
	fi; \
	if ! pkg-config --exists x11 2>/dev/null && ! pkg-config --exists wayland-client 2>/dev/null; then \
		echo -e "$(RED)[MISSING]$(NC) X11 or Wayland development libraries"; \
		missing="$$missing display"; \
	else \
		echo -e "$(GREEN)[OK]$(NC) Display libraries (X11/Wayland)"; \
	fi; \
	echo ""; \
	if [ -n "$$missing" ]; then \
		echo -e "$(YELLOW)[WARNING]$(NC) Some dependencies are missing. Run 'make deps' to install them."; \
		exit 1; \
	else \
		echo -e "$(GREEN)[OK]$(NC) All dependencies are installed!"; \
	fi

# Install dependencies based on detected OS
deps:
	@echo -e "$(BLUE)[INFO]$(NC) Installing dependencies..."
	@echo -e "$(BLUE)[INFO]$(NC) Detected OS: $(OS) ($(DISTRO))"
	@echo -e "$(BLUE)[INFO]$(NC) Package manager: $(PKG_MANAGER)"
	@echo ""
ifeq ($(DISTRO),arch)
	@echo -e "$(BLUE)[INFO]$(NC) Installing dependencies for Arch Linux..."
	sudo pacman -S --needed --noconfirm go base-devel libx11 libxcursor libxrandr libxinerama libxi mesa pkg-config
else ifeq ($(DISTRO),debian)
	@echo -e "$(BLUE)[INFO]$(NC) Installing dependencies for Debian/Ubuntu..."
	sudo apt-get update
	sudo apt-get install -y golang build-essential libgl1-mesa-dev xorg-dev pkg-config
else ifeq ($(DISTRO),fedora)
	@echo -e "$(BLUE)[INFO]$(NC) Installing dependencies for Fedora..."
	sudo dnf install -y golang mesa-libGL-devel libX11-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel pkg-config
else ifeq ($(DISTRO),rhel)
	@echo -e "$(BLUE)[INFO]$(NC) Installing dependencies for RHEL/CentOS..."
	sudo dnf install -y golang mesa-libGL-devel libX11-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel pkg-config
else ifeq ($(DISTRO),opensuse)
	@echo -e "$(BLUE)[INFO]$(NC) Installing dependencies for openSUSE..."
	sudo zypper install -y go Mesa-libGL-devel libX11-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel pkg-config
else ifeq ($(DISTRO),alpine)
	@echo -e "$(BLUE)[INFO]$(NC) Installing dependencies for Alpine Linux..."
	sudo apk add go build-base mesa-dev libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev pkgconfig
else ifeq ($(DISTRO),macos)
	@echo -e "$(BLUE)[INFO]$(NC) Installing dependencies for macOS..."
	@if ! command -v brew >/dev/null 2>&1; then \
		echo -e "$(RED)[ERROR]$(NC) Homebrew is not installed. Please install it from https://brew.sh"; \
		exit 1; \
	fi
	brew install go pkg-config librsvg
else
	@echo -e "$(RED)[ERROR]$(NC) Unknown distribution: $(DISTRO)"
	@echo -e "$(YELLOW)[INFO]$(NC) Please install the following dependencies manually:"
	@echo "  - Go compiler (golang)"
	@echo "  - OpenGL development libraries"
	@echo "  - X11 or Wayland development libraries"
	@echo "  - pkg-config"
	@exit 1
endif
	@echo ""
	@echo -e "$(GREEN)[OK]$(NC) Dependencies installed successfully!"

# Run tests
test:
	@echo -e "$(BLUE)[INFO]$(NC) Running tests..."
	@go test ./src/...

# Show help
help:
	@echo ""
	@echo -e "$(BLUE)Raven Terminal Makefile$(NC)"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build           Build the application (default)"
	@echo "  install         Clean install system-wide (uninstalls first, requires sudo)"
	@echo "  install-local   Clean install for current user (uninstalls first)"
	@echo "  uninstall       Uninstall all installations (global + user)"
	@echo "  uninstall-local Uninstall user installation only"
	@echo "  uninstall-all   Uninstall everything including config files"
	@echo "  clean           Remove build artifacts"
	@echo "  deps            Install build dependencies for your OS"
	@echo "  deps-check      Check if all dependencies are installed"
	@echo "  test            Run tests"
	@echo "  help            Show this help message"
	@echo ""
	@echo "Detected System:"
	@echo "  OS:             $(OS)"
	@echo "  Distribution:   $(DISTRO)"
	@echo "  Package Manager: $(PKG_MANAGER)"
	@echo ""
	@echo "Examples:"
	@echo "  make deps         # Install dependencies"
	@echo "  make              # Build the application"
	@echo "  make install      # Clean install system-wide (requires sudo)"
	@echo "  make uninstall    # Completely remove all installations"
	@echo ""
