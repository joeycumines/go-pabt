# MIT License
#
# Copyright (c) 2025 Joseph Cumines
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

# Source: https://gist.github.com/joeycumines/3352c393c1bf43df72b120ae9134168d
# Example: https://github.com/joeycumines/go-utilpkg

# Usage
# ---
# Extensible multi-module Makefile tailored for Go projects.
#
# This Makefile is designed to be used in a monorepo, where multiple Go modules
# are contained within a single repository. The implementor's primary use case
# was easing overhead of managing many _personal_ projects, as one author.
# The `grit` tool is a key factor in this, see comments in-source, for more
# details.
#
# Go Module Targets
# ---
# Each of these targets has multiple corresponding sub-targets, and with the
# primary target being to run all of them, in parallel, if enabled.
# The relative paths of modules are mapped to period-separated slugs, which may
# be used as a suffix, for any of these targets.
#
# For example:
#   make -j4 $(GO_TARGET_PREFIX)all                                  # all checks for all modules with parallelism of 4
#   make $(GO_TARGET_PREFIX)staticcheck.dir1.dir-2.modulerootdir     # staticcheck in ./dir1/dir-2/modulerootdir
#   make $(GO_TARGET_PREFIX)all.dir1.dir-2.modulerootdir             # all checks in ./dir1/dir-2/modulerootdir
#
# Sub-directory Makefiles
# ---
# In a similar vein to Go modules, Makefiles are also discovered, and exposed
# using pattern and implicit rules, to implement targets to:
#
#   1. Run the default target in the subdirectory (e.g. `make $(GO_TARGET_PREFIX)run.dir1`)
#   2. Run a specific target in the subdirectory (e.g. `make $(GO_TARGET_PREFIX)run-<target>.dir1`)
#
# Please be aware that these targets are primarily for convenience. Limitations
# exist, e.g. each of these invocations are separate, and therefore cannot
# avoid duplicated work, and may be at risk of concurrency-related problems.
#
# Customization
# ---
# The behavior of this implementation is quite configurable, e.g. commands,
# flags, and settings controlling certain behavior, are exposed, and documented
# in the source. Makefiles are also composable by nature, though global scoping
# can cause issues. Be mindful, when choosing how to integrate this Makefile.
# While no guarantees are provided, an effort to maintain compatibility has,
# and will continue, to be made, e.g. as features are added, or tweaked.
#
# Multiple customization patterns are supported, including:
#
#   1. Environment variables or command line arguments
#   2. Creating a ./config.mk (uncommitted, user-specific)
#   3. Creating a ./project.mk (committed, project-specific)
#   4. Including* this Makefile from another Makefile
#
# (*) This is the most likely to break, e.g. ROOT_MAKEFILE would likely need to
#     be set in the including Makefile, as would PROJECT_ROOT.
#
# Make Subprocesses Reevaluating This Makefile
# ---
# Beware: Some targets use `$(MAKE) ... -f $(ROOT_MAKEFILE) ...`, running in
# independent subprocesses. This is intentional but can affect behavior.
# Valid uses:
#
#   1. Target acts as a script with arguments
#   2. Optional prerequisites (not possible using order-only prerequisites)
#
# Details:
# Case 1 is simple convenience - "script" implies no dependencies.
# Case 2 enables multiple configurations/ordering (e.g., `test` standalone
# vs. after `build`). This sometimes necessitates alias targets and accepts
# known tradeoffs like isolated dependencies and potential duplicate work.

# What is `grit` and how to use it?
# ---
# Godoc: https://pkg.go.dev/github.com/grailbio/grit
#
# Preface:
#
# The tooling provided by this Makefile allows you to publish modules to their
# own repositories, from a single monorepo. This is useful when there are
# tricky dependencies between modules, but you want to be able to publish them
# independently, manage GitHub issues independently, etc.
#
# Caveats:
#
# - Currently only supports configuring the same branch for all modules,
#   source and target
# - Does not currently provide tooling to support bi-directional syncing;
#   though `grit` does support it, it's a bit... hairy
#
# Usage:
#
# 1. Prepare the target repository (presumably the canonical one per go.mod)
# 2. Update `project.mk`, setting `GRIT_SRC` (if you haven't already) and
#   `GRIT_DST`, optionally setting `GRIT_BRANCH` (defaults to "main")
# 3. Sync _from_ the target to the source (this repository) like
#   `make grit-init GRIT_INIT_TARGET=go-module-slug`, where go-module-slug is
#   the map key, used in GRIT_DST, note that you may need to specify
#   GRIT_FLAGS='-push -linearize' (see the docs)
# 4. Add the new module to the Go workspace
# 5. Run (either automatically or manually) `make grit` to sync _to_ the
#   configured target(s), to propagate

# simple variables that either represent invariants, or need to be interacted
# with in an imperative manner, e.g. "building" values across includes, without
# separating the output of each include into its own discrete variable

# windows gnu make seems to append includes to the end of MAKEFILE_LIST
# hence the simple variable assignment, prior to any includes
ifeq ($(ROOT_MAKEFILE),)
ROOT_MAKEFILE := $(abspath $(lastword $(MAKEFILE_LIST)))
endif

# used to support changing the working directory + resolve relative paths
ifeq ($(PROJECT_ROOT),)
PROJECT_ROOT := $(patsubst %/,%,$(dir $(ROOT_MAKEFILE)))
endif

# N.B. this is a multi-platform makefile
# so far only two switching cases have been required (Windows and Unix)
ifeq ($(IS_WINDOWS),)
ifeq ($(OS),Windows_NT)
IS_WINDOWS := true
else
IS_WINDOWS := false
endif
endif

# ---

# includes, for optionally-configurable, standalone use
# N.B. The file extension .mk is somewhat standard. The file extension .mak is
# supported for historical reasons, applicable to a specific project.

# optional project-specific (committed) overrides and extensions
-include $(PROJECT_ROOT)/project.mk $(PROJECT_ROOT)/project.mak
# user-specific (uncommitted) overrides and extensions
# N.B. to use this, add the following to .gitignore: /config.mk
#      and, if you use docker, add the following to .dockerignore: config.mk
-include $(PROJECT_ROOT)/config.mk $(PROJECT_ROOT)/config.mak

# ---

# special-case post-include variables

# There is a built-in help target. This variable is to mitigate collisions.
# Setting SKIP_FURTHER_MAKEFILE_HELP=true will disable the help target.
ifeq ($(SKIP_FURTHER_MAKEFILE_HELP),)
SKIP_FURTHER_MAKEFILE_HELP := false
endif

# If set, then ALL targets except:
#  help, h, run.<./**/Makefile path as slug>, run-%.<./**/Makefile path as slug>
# will be prefixed with this value.
# If you wish to use this as part of a larger project, you might set this like:
#   GO_TARGET_PREFIX := go.
# Within your root Makefile (which would also need to set ROOT_MAKEFILE), or
# within project.mk.
ifeq ($(GO_TARGET_PREFIX),)
GO_TARGET_PREFIX :=
GO_MK_VAR_PREFIX :=
else
# Simply-expand, attempt to ensure value is set consistently, avoid re-eval.
GO_TARGET_PREFIX := $(GO_TARGET_PREFIX)
# This is a mitigation for collisions. For use in monorepo multi-lang projects.
# Only applies to certain variables, e.g. CLEAN_PATHS.
GO_MK_VAR_PREFIX := GO_
endif
# export the prefixes (normally not necessary, just for sanity)
export GO_TARGET_PREFIX
export GO_MK_VAR_PREFIX

# ---

# intended to be provided on the command line, for certain targets

# additional make flags to be used by the pattern targets like run.%, and implicit targets like run-%.<path>
# (used to run subdir makefiles)
RUN_FLAGS ?=

# determines the output of the debug-vars target
# N.B. only _defined_ variables will be present in the output
$(eval $(GO_MK_VAR_PREFIX)DEBUG_VARS ?= ROOT_MAKEFILE PROJECT_ROOT PROJECT_NAME IS_WINDOWS GO_MODULE_PATHS GO_MODULE_SLUGS GO_MODULE_SLUGS_NO_PACKAGES GO_MODULE_SLUGS_EXCL_NO_PACKAGES GO_MODULE_SLUGS_NO_UPDATE GO_MODULE_SLUGS_EXCL_NO_UPDATE GO_MODULE_SLUGS_GRIT_DST GO_MODULE_SLUGS_EXCL_GRIT_DST SUBDIR_MAKEFILE_PATHS SUBDIR_MAKEFILE_SLUGS GO_TARGET_PREFIX MAKEFILE_TARGET_PREFIXES $$(MAKEFILE_TARGET_PREFIXES) $$(foreach v,CLEAN_PATHS ALL_TARGETS BUILD_TARGETS LINT_TARGETS VET_TARGETS STATICCHECK_TARGETS BETTERALIGN_TARGETS DEADCODE_TARGETS TEST_TARGETS COVER_TARGETS FMT_TARGETS GENERATE_TARGETS FIX_TARGETS UPDATE_TARGETS TIDY_TARGETS GRIT_TARGETS,$$(GO_MK_VAR_PREFIX)$$v))

# ---

# intended to be configurable via config.mk

PROJECT_NAME ?= $(notdir $(PROJECT_ROOT))
# set (build) these to support dynamically building the help target with replacements
MAKEFILE_TARGET_PREFIXES ?=
GO ?= go
GO_FLAGS ?=
GO_TEST_FLAGS ?=
# callable variables, with param $1 being a go module slug (see go_module_slug_to_path)
GO_TEST ?= $(GO) -C $(call go_module_slug_to_path,$1) test $(GO_FLAGS) $(GO_TEST_FLAGS)
GO_BUILD ?= $(GO) -C $(call go_module_slug_to_path,$1) build $(GO_FLAGS)
GO_VET ?= $(GO) -C $(call go_module_slug_to_path,$1) vet $(GO_FLAGS)
# simple command variables
GO_FMT ?= $(GO) fmt
GO_GENERATE ?= $(GO) generate
GO_FIX ?= $(GO) fix
GO_COVERAGE_MODULE_FILE ?= coverage.out
GO_COVERAGE_ALL_MODULES_FILE ?= coverage-all.out
GO_TOOL_COVER ?= $(GO) tool cover
GODOC ?= $(GO) tool $(GO_PKG_GODOC)
_GODOC_FLAGS := -http=localhost:6060 # ignore this (use GODOC_FLAGS)
GODOC_FLAGS ?= $(_GODOC_FLAGS)
GRIT ?= $(GO) tool $(GO_PKG_GRIT)
GRIT_FLAGS ?= -push
GRIT_BRANCH ?= main
GRIT_SRC ?=
GRIT_DST ?=
GRIT_INIT_TARGET ?=
STATICCHECK ?= $(call go_tool_binary_path,$(GO_PKG_STATICCHECK))
STATICCHECK_FLAGS ?=
BETTERALIGN ?= $(call go_tool_binary_path,$(GO_PKG_BETTERALIGN))
BETTERALIGN_FLAGS ?=
DEADCODE ?= $(if $(or $(filter true,$(DEADCODE_ERROR_ON_UNIGNORED)),$(and $(DEADCODE_IGNORE_PATTERNS_FILE),$(wildcard $(DEADCODE_IGNORE_PATTERNS_FILE)))),$(call go_tool_binary_path,$(GO_PKG_SIMPLE_COMMAND_OUTPUT_FILTER)) -v $(and $(filter true,$(DEADCODE_ERROR_ON_UNIGNORED)),-e on-content) $(addprefix -f ,$(and $(DEADCODE_IGNORE_PATTERNS_FILE),$(wildcard $(DEADCODE_IGNORE_PATTERNS_FILE)))) -- ,)$(call go_tool_binary_path,$(GO_PKG_DEADCODE))
DEADCODE_FLAGS ?=
# N.B. If set, by default, used with simple-command-output-filter to exclude false-positives.
# Contains glob-like patterns to excluded lines from the deadcode output. See that tool's docs:
#   https://pkg.go.dev/github.com/joeycumines/simple-command-output-filter#section-readme
# The file's path is relative to the module root, where the ignores apply.
DEADCODE_IGNORE_PATTERNS_FILE ?= # .deadcodeignore
# If set to true then, by default, the simple-command-output-filter tool will
# be used to treat any detected deadcode, not otherwise ignored, as an error.
DEADCODE_ERROR_ON_UNIGNORED ?= false
# for the tools target, to update the root go.mod (only relevant when setting up or updating this makefile)
GO_TOOLS ?= $(GO_TOOLS_DEFAULT)
# Used to prune _paths_ when searching for go modules. Single wildcard (%) supported. May match intermediate directories.
# Example: %/vendor %/node_modules ./managed-separately
GO_MODULE_PATHS_EXCLUDE_PATTERNS ?=
# used to special-case modules for tools which fail if they find no packages (e.g. go vet)
GO_MODULE_SLUGS_NO_PACKAGES ?=
# used to exclude modules from the update* targets
GO_MODULE_SLUGS_NO_UPDATE ?=
# used to exclude modules from the betteralign targets
GO_MODULE_SLUGS_NO_BETTERALIGN ?=
# used to include modules in the deadcode targets
GO_MODULE_SLUGS_USE_DEADCODE ?=

# configurable, but unlikely to need to be configured

# separates keys and values, see also the map_* functions
MAP_SEPARATOR ?= :
# path separator (/ replacement) for slugs
SLUG_SEPARATOR ?= .
GO_TOOLS_DEFAULT ?= \
		$(GO_PKG_BETTERALIGN) \
		$(GO_PKG_GRIT) \
		$(GO_PKG_GODOC) \
		$(GO_PKG_STATICCHECK) \
		$(if $(GO_MODULE_SLUGS_USE_DEADCODE),$(GO_PKG_DEADCODE) $(if $(or $(filter true,$(DEADCODE_ERROR_ON_UNIGNORED)),$(DEADCODE_IGNORE_PATTERNS_FILE)),$(GO_PKG_SIMPLE_COMMAND_OUTPUT_FILTER),),)
GO_PKG_BETTERALIGN ?= github.com/dkorunic/betteralign/cmd/betteralign
GO_PKG_GRIT ?= github.com/grailbio/grit
GO_PKG_GODOC ?= golang.org/x/tools/cmd/godoc
GO_PKG_STATICCHECK ?= honnef.co/go/tools/cmd/staticcheck
GO_PKG_DEADCODE ?= golang.org/x/tools/cmd/deadcode
GO_PKG_SIMPLE_COMMAND_OUTPUT_FILTER ?= github.com/joeycumines/simple-command-output-filter
# paths to be deleted on clean - use $($(GO_MK_VAR_PREFIX)CLEAN_PATHS) to get
$(eval $(GO_MK_VAR_PREFIX)CLEAN_PATHS ?= $$(GO_COVERAGE_ALL_MODULES_FILE) $$(addsuffix /$$(GO_COVERAGE_MODULE_FILE),$$(GO_MODULE_PATHS)))

# ---

# Recursive wildcard match function, with support for optional pruning.
#
# Signature: $(call rwildcard,<dir>,<pattern>,[filter-out])
#
# $1: directory to search (requires a trailing slash, e.g., "src/")
# $2: pattern to match (e.g., "*.go")
# $3: (optional) A whitespace-separated list of patterns to $(filter-out ...).
#     Applied to both files and directories, "guarding" further recursion.
rwildcard = \
$(call _rwildcard_filter_out,$(wildcard $1$2),$3) \
$(foreach d,\
$(call _rwildcard_filter_out,$(patsubst %/./,%,$(wildcard $1*/./)),$3),\
$(call rwildcard,$d/,$2,$3)\
)
_rwildcard_filter_out = $(if $2,$(filter-out $2,$1),$1)

# looks up a value in a map, $1 is the map, $2 is the key associated with the value
map_value_by_key = $(patsubst $2$(MAP_SEPARATOR)%,%,$(filter $2$(MAP_SEPARATOR)%,$1))
# looks up a key in a map, $1 is the map, $2 is the value associated with the key
map_key_by_value = $(patsubst %$(MAP_SEPARATOR)$2,%,$(filter %$(MAP_SEPARATOR)$2,$1))
# builds a new map, from a set of keys, using a transform function to build values from the keys
# $1 are the keys, $2 is the transform function
map_transform_keys = $(foreach v,$1,$v$(MAP_SEPARATOR)$(call $2,$v))
# extracts only the keys from a map variable, $1 is the map variable
map_keys = $(foreach v,$1,$(word 1,$(subst $(MAP_SEPARATOR), ,$v)))

# convert a path to a slug, e.g. ./logiface/logrus -> logiface.logrus, with special case for root
slug_transform = $(if $(filter .,$1),root,$(subst /,$(SLUG_SEPARATOR),$(patsubst ./%,%,$1)))
# attempts to perform the opposite of slug_parse, note that it may not be possible to recover the original path
slug_parse = $(if $(filter root,$1),$(SLUG_SEPARATOR),$(SLUG_SEPARATOR)/$(subst $(SLUG_SEPARATOR),/,$(filter-out root,$1)))

# escaping for use in recipies, e.g.: echo $(call escape_command_arg,$(MESSAGE))
# WARNING: you may get unexpected results under windows, e.g. if MESSAGE is empty, in the above example
ifeq ($(IS_WINDOWS),true)
escape_command_arg ?= $(strip $(subst %,%%,$(subst |,^|,$(subst >,^>,$(subst <,^<,$(subst &,^&,$(subst ^,^^,$1)))))))
else
escape_command_arg ?= '$(subst ','\'',$1)'
endif

# includes workaround for https://github.com/golang/go/issues/72824
# (the workaround is running go tool -n _twice_)
go_tool_binary_path = $(if $(shell $(GO) tool -C $(PROJECT_ROOT) -n $1),$(shell $(GO) tool -C $(PROJECT_ROOT) -n $1),$(error no go tool found for $1))

go_module_path_to_slug = $(call map_value_by_key,$(_GO_MODULE_MAP),$1)
go_module_slug_to_path = $(call map_key_by_value,$(_GO_MODULE_MAP),$1)

subdir_makefile_path_to_slug = $(call map_value_by_key,$(_SUBDIR_MAKEFILE_MAP),$1)
subdir_makefile_slug_to_path = $(call map_key_by_value,$(_SUBDIR_MAKEFILE_MAP),$1)

go_module_slug_to_grit_src = $(GRIT_SRC),$(patsubst ./%,%,$(or $(call go_module_slug_to_path,$1),$(error no go module found for $1)))/,$(GRIT_BRANCH)
go_module_slug_to_grit_dst = $(or $(call map_value_by_key,$(GRIT_DST),$1),$(error no GRIT_DST entry for $1)),,$(GRIT_BRANCH)

# paths formatted like ". ./logiface ./logiface/logrus ./logiface/testsuite ./logiface/zerolog"
GO_MODULE_PATHS := $(patsubst %/go.mod,%,$(call rwildcard,./,go.mod,$(GO_MODULE_PATHS_EXCLUDE_PATTERNS)))
# used by go_module_path_to_slug and go_module_slug_to_path to lookup an associated path/slug
_GO_MODULE_MAP := $(call map_transform_keys,$(GO_MODULE_PATHS),slug_transform)
# example: root logiface logiface.logrus logiface.testsuite logiface.zerolog
GO_MODULE_SLUGS := $(foreach d,$(GO_MODULE_PATHS),$(call go_module_path_to_slug,$d))
# sanity check the path and slug lookups
ifneq ($(GO_MODULE_PATHS),$(foreach d,$(GO_MODULE_SLUGS),$(call go_module_slug_to_path,$d)))
$(error GO_MODULE_PATHS contains unsupported paths)
endif
ifneq ($(GO_MODULE_SLUGS),$(foreach d,$(GO_MODULE_PATHS),$(call go_module_path_to_slug,$d)))
$(error GO_MODULE_SLUGS contains unsupported paths)
endif
GO_MODULE_SLUGS_EXCL_NO_PACKAGES := $(filter-out $(GO_MODULE_SLUGS_NO_PACKAGES),$(GO_MODULE_SLUGS))
GO_MODULE_SLUGS_EXCL_NO_UPDATE := $(filter-out $(GO_MODULE_SLUGS_NO_UPDATE),$(GO_MODULE_SLUGS))
GO_MODULE_SLUGS_EXCL_NO_BETTERALIGN := $(filter-out $(GO_MODULE_SLUGS_NO_BETTERALIGN),$(GO_MODULE_SLUGS_EXCL_NO_PACKAGES))
# because GO_MODULE_SLUGS_EXCL_NO_BETTERALIGN is composite (with no packages), and we need a target for _all_ modules
GO_MODULE_SLUGS_INCL_NO_BETTERALIGN := $(filter-out $(GO_MODULE_SLUGS_EXCL_NO_BETTERALIGN),$(GO_MODULE_SLUGS))
GO_MODULE_SLUGS_INCL_USE_DEADCODE := $(filter $(GO_MODULE_SLUGS_USE_DEADCODE),$(GO_MODULE_SLUGS_EXCL_NO_PACKAGES))
# because GO_MODULE_SLUGS_INCL_USE_DEADCODE is composite (with no packages), and we need a target for _all_ modules
GO_MODULE_SLUGS_EXCL_USE_DEADCODE := $(filter-out $(GO_MODULE_SLUGS_INCL_USE_DEADCODE),$(GO_MODULE_SLUGS))
GO_MODULE_SLUGS_GRIT_DST := $(filter $(call map_keys,$(GRIT_DST)),$(GO_MODULE_SLUGS))
GO_MODULE_SLUGS_EXCL_GRIT_DST := $(filter-out $(GO_MODULE_SLUGS_GRIT_DST),$(GO_MODULE_SLUGS))

# subdirectories which contain a file named "Makefile", formatted with a leading ".", and no trailing slash
# note that the root Makefile (this file) is excluded
SUBDIR_MAKEFILE_PATHS := $(filter-out .,$(patsubst %/Makefile,%,$(call rwildcard,./,Makefile)))
# used by subdir_makefile_path_to_slug and subdir_makefile_slug_to_path to lookup an associated path/slug
_SUBDIR_MAKEFILE_MAP := $(call map_transform_keys,$(SUBDIR_MAKEFILE_PATHS),slug_transform)
# slugs for subdirectories, without a leading ./, / replaced with ., and the path . mapped to root
SUBDIR_MAKEFILE_SLUGS := $(foreach d,$(SUBDIR_MAKEFILE_PATHS),$(call subdir_makefile_path_to_slug,$d))

# sanity check the path and slug lookups
ifneq ($(SUBDIR_MAKEFILE_PATHS),$(foreach d,$(SUBDIR_MAKEFILE_SLUGS),$(call subdir_makefile_slug_to_path,$d)))
$(error SUBDIR_MAKEFILE_PATHS contains unsupported paths)
endif
ifneq ($(SUBDIR_MAKEFILE_SLUGS),$(foreach d,$(SUBDIR_MAKEFILE_PATHS),$(call subdir_makefile_path_to_slug,$d)))
$(error SUBDIR_MAKEFILE_SLUGS contains unsupported paths)
endif

# ---

##@ Standard Targets

.PHONY: $(GO_TARGET_PREFIX)all
$(GO_TARGET_PREFIX)all: ## Builds all, and (non-standard per GNU) runs all checks.
	@

.PHONY: $(GO_TARGET_PREFIX)clean
$(GO_TARGET_PREFIX)clean: ## Cleans up outputs of other targets, e.g. removing coverage files.
ifeq ($(IS_WINDOWS),true)
	del /Q /S $(subst /,\,$($(GO_MK_VAR_PREFIX)CLEAN_PATHS))
else
	rm -rf $($(GO_MK_VAR_PREFIX)CLEAN_PATHS)
endif

# ---

##@ Go Module Targets

# all, all.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)ALL_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)all.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)all
$(GO_TARGET_PREFIX)all: $($(GO_MK_VAR_PREFIX)ALL_TARGETS) ## Builds, then lints and tests (modules in parallel, two stages).

.PHONY: $($(GO_MK_VAR_PREFIX)ALL_TARGETS)
$($(GO_MK_VAR_PREFIX)ALL_TARGETS): $(GO_TARGET_PREFIX)all.%: $(GO_TARGET_PREFIX)_all__build.% $(GO_TARGET_PREFIX)_all__lint.% $(GO_TARGET_PREFIX)_all__test.%

.PHONY: $(addprefix $(GO_TARGET_PREFIX)_all__build.,$(GO_MODULE_SLUGS))
$(addprefix $(GO_TARGET_PREFIX)_all__build.,$(GO_MODULE_SLUGS)): $(GO_TARGET_PREFIX)_all__build.%:
	@$(MAKE) --no-print-directory $(GO_TARGET_PREFIX)build.$*

.PHONY: $(addprefix $(GO_TARGET_PREFIX)_all__lint.,$(GO_MODULE_SLUGS))
$(addprefix $(GO_TARGET_PREFIX)_all__lint.,$(GO_MODULE_SLUGS)): $(GO_TARGET_PREFIX)_all__lint.%: $(GO_TARGET_PREFIX)_all__build.%
	@$(MAKE) --no-print-directory $(GO_TARGET_PREFIX)lint.$*

.PHONY: $(addprefix $(GO_TARGET_PREFIX)_all__test.,$(GO_MODULE_SLUGS))
$(addprefix $(GO_TARGET_PREFIX)_all__test.,$(GO_MODULE_SLUGS)): $(GO_TARGET_PREFIX)_all__test.%: $(GO_TARGET_PREFIX)_all__build.%
	@$(MAKE) --no-print-directory $(GO_TARGET_PREFIX)test.$*

# build, build.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)BUILD_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)build.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)build
$(GO_TARGET_PREFIX)build: $($(GO_MK_VAR_PREFIX)BUILD_TARGETS) ## Runs the go build tool.

.PHONY: $($(GO_MK_VAR_PREFIX)BUILD_TARGETS)
$($(GO_MK_VAR_PREFIX)BUILD_TARGETS): $(GO_TARGET_PREFIX)build.%:
	$(call GO_BUILD,$*) ./...

# lint, lint.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)LINT_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)lint.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)lint
$(GO_TARGET_PREFIX)lint: $($(GO_MK_VAR_PREFIX)LINT_TARGETS) ## Runs the vet, staticcheck, betteralign, and deadcode targets.

.PHONY: $($(GO_MK_VAR_PREFIX)LINT_TARGETS)
$($(GO_MK_VAR_PREFIX)LINT_TARGETS): $(GO_TARGET_PREFIX)lint.%: $(GO_TARGET_PREFIX)vet.% $(GO_TARGET_PREFIX)staticcheck.% $(GO_TARGET_PREFIX)betteralign.% $(GO_TARGET_PREFIX)deadcode.%

# vet, vet.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)VET_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)vet.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)vet
$(GO_TARGET_PREFIX)vet: $($(GO_MK_VAR_PREFIX)VET_TARGETS) ## Runs the go vet tool.

.PHONY: $(addprefix $(GO_TARGET_PREFIX)vet.,$(GO_MODULE_SLUGS_EXCL_NO_PACKAGES))
$(addprefix $(GO_TARGET_PREFIX)vet.,$(GO_MODULE_SLUGS_EXCL_NO_PACKAGES)): $(GO_TARGET_PREFIX)vet.%:
	$(call GO_VET,$*) ./...

.PHONY: $(addprefix $(GO_TARGET_PREFIX)vet.,$(GO_MODULE_SLUGS_NO_PACKAGES))
$(addprefix $(GO_TARGET_PREFIX)vet.,$(GO_MODULE_SLUGS_NO_PACKAGES)): $(GO_TARGET_PREFIX)vet.%:

# staticcheck, staticcheck.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)STATICCHECK_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)staticcheck.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)staticcheck
$(GO_TARGET_PREFIX)staticcheck: $($(GO_MK_VAR_PREFIX)STATICCHECK_TARGETS) ## Runs the staticcheck tool.

.PHONY: $($(GO_MK_VAR_PREFIX)STATICCHECK_TARGETS)
$($(GO_MK_VAR_PREFIX)STATICCHECK_TARGETS): $(GO_TARGET_PREFIX)staticcheck.%:
	$(MAKE) -s -C $(call go_module_slug_to_path,$*) -f $(ROOT_MAKEFILE) $(GO_TARGET_PREFIX)_staticcheck STATICCHECK_FLAGS=$(call escape_command_arg,$(STATICCHECK_FLAGS))

.PHONY: $(GO_TARGET_PREFIX)_staticcheck
$(GO_TARGET_PREFIX)_staticcheck:
	$(STATICCHECK) $(STATICCHECK_FLAGS) ./...

# betteralign, betteralign.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)BETTERALIGN_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)betteralign.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)betteralign
$(GO_TARGET_PREFIX)betteralign: $($(GO_MK_VAR_PREFIX)BETTERALIGN_TARGETS) ## Runs the betteralign tool.

.PHONY: $(addprefix $(GO_TARGET_PREFIX)betteralign.,$(GO_MODULE_SLUGS_EXCL_NO_BETTERALIGN))
$(addprefix $(GO_TARGET_PREFIX)betteralign.,$(GO_MODULE_SLUGS_EXCL_NO_BETTERALIGN)): $(GO_TARGET_PREFIX)betteralign.%:
	$(MAKE) -s -C $(call go_module_slug_to_path,$*) -f $(ROOT_MAKEFILE) $(GO_TARGET_PREFIX)_betteralign BETTERALIGN_FLAGS=$(call escape_command_arg,$(BETTERALIGN_FLAGS))

.PHONY: $(addprefix $(GO_TARGET_PREFIX)betteralign.,$(GO_MODULE_SLUGS_INCL_NO_BETTERALIGN))
$(addprefix $(GO_TARGET_PREFIX)betteralign.,$(GO_MODULE_SLUGS_INCL_NO_BETTERALIGN)): $(GO_TARGET_PREFIX)betteralign.%:

.PHONY: $(GO_TARGET_PREFIX)_betteralign
$(GO_TARGET_PREFIX)_betteralign:
	$(BETTERALIGN) $(BETTERALIGN_FLAGS) ./...

# deadcode, deadcode.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)DEADCODE_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)deadcode.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)deadcode
$(GO_TARGET_PREFIX)deadcode: $($(GO_MK_VAR_PREFIX)DEADCODE_TARGETS) ## Runs the deadcode tool.

.PHONY: $(addprefix $(GO_TARGET_PREFIX)deadcode.,$(GO_MODULE_SLUGS_INCL_USE_DEADCODE))
$(addprefix $(GO_TARGET_PREFIX)deadcode.,$(GO_MODULE_SLUGS_INCL_USE_DEADCODE)): $(GO_TARGET_PREFIX)deadcode.%:
	$(MAKE) -s -C $(call go_module_slug_to_path,$*) -f $(ROOT_MAKEFILE) $(GO_TARGET_PREFIX)_deadcode DEADCODE_FLAGS=$(call escape_command_arg,$(DEADCODE_FLAGS))

.PHONY: $(addprefix $(GO_TARGET_PREFIX)deadcode.,$(GO_MODULE_SLUGS_EXCL_USE_DEADCODE))
$(addprefix $(GO_TARGET_PREFIX)deadcode.,$(GO_MODULE_SLUGS_EXCL_USE_DEADCODE)): $(GO_TARGET_PREFIX)deadcode.%:

.PHONY: $(GO_TARGET_PREFIX)_deadcode
$(GO_TARGET_PREFIX)_deadcode:
	$(DEADCODE) $(DEADCODE_FLAGS) ./...

# test, test.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)TEST_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)test.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)test
$(GO_TARGET_PREFIX)test: $($(GO_MK_VAR_PREFIX)TEST_TARGETS) ## Runs the go test tool.

.PHONY: $(addprefix $(GO_TARGET_PREFIX)test.,$(GO_MODULE_SLUGS_EXCL_NO_PACKAGES))
$(addprefix $(GO_TARGET_PREFIX)test.,$(GO_MODULE_SLUGS_EXCL_NO_PACKAGES)): $(GO_TARGET_PREFIX)test.%:
	$(call GO_TEST,$*) ./...

.PHONY: $(addprefix $(GO_TARGET_PREFIX)test.,$(GO_MODULE_SLUGS_NO_PACKAGES))
$(addprefix $(GO_TARGET_PREFIX)test.,$(GO_MODULE_SLUGS_NO_PACKAGES)): $(GO_TARGET_PREFIX)test.%:

# cover, cover.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)COVER_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)cover.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)cover
$(GO_TARGET_PREFIX)cover: $($(GO_MK_VAR_PREFIX)COVER_TARGETS) ## Runs the go test tool with -covermode=count and generates a coverage report.
	echo mode: count >$(GO_COVERAGE_ALL_MODULES_FILE)
	$(foreach d,$(GO_MODULE_SLUGS_EXCL_NO_PACKAGES),$(cover__TEMPLATE))
	$(GO_TOOL_COVER) -html=$(GO_COVERAGE_ALL_MODULES_FILE)
ifeq ($(IS_WINDOWS),true)
define cover__TEMPLATE =
type $(subst /,\,$(call go_module_slug_to_path,$d)/$(GO_COVERAGE_MODULE_FILE)) | more +1 | findstr /v /r "^$$" >>$(GO_COVERAGE_ALL_MODULES_FILE)

endef
else
define cover__TEMPLATE =
tail -n +2 $(call go_module_slug_to_path,$d)/$(GO_COVERAGE_MODULE_FILE) >>$(GO_COVERAGE_ALL_MODULES_FILE)

endef
endif

.PHONY: $(addprefix $(GO_TARGET_PREFIX)cover.,$(GO_MODULE_SLUGS_EXCL_NO_PACKAGES))
$(addprefix $(GO_TARGET_PREFIX)cover.,$(GO_MODULE_SLUGS_EXCL_NO_PACKAGES)): $(GO_TARGET_PREFIX)cover.%:
	$(call GO_TEST,$*) -coverprofile=$(GO_COVERAGE_MODULE_FILE) -covermode=count ./...

.PHONY: $(addprefix $(GO_TARGET_PREFIX)cover.,$(GO_MODULE_SLUGS_NO_PACKAGES))
$(addprefix $(GO_TARGET_PREFIX)cover.,$(GO_MODULE_SLUGS_NO_PACKAGES)): $(GO_TARGET_PREFIX)cover.%:

# fmt, fmt.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)FMT_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)fmt.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)fmt
$(GO_TARGET_PREFIX)fmt: $($(GO_MK_VAR_PREFIX)FMT_TARGETS) ## Runs the go fmt command.

.PHONY: $($(GO_MK_VAR_PREFIX)FMT_TARGETS)
$($(GO_MK_VAR_PREFIX)FMT_TARGETS): $(GO_TARGET_PREFIX)fmt.%:
	$(MAKE) -s -C $(call go_module_slug_to_path,$*) -f $(ROOT_MAKEFILE) $(GO_TARGET_PREFIX)_fmt

.PHONY: $(GO_TARGET_PREFIX)_fmt
$(GO_TARGET_PREFIX)_fmt:
	$(GO_FMT) ./...

# generate, generate.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)GENERATE_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)generate.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)generate
$(GO_TARGET_PREFIX)generate: $($(GO_MK_VAR_PREFIX)GENERATE_TARGETS) ## Runs the go generate command.

.PHONY: $($(GO_MK_VAR_PREFIX)GENERATE_TARGETS)
$($(GO_MK_VAR_PREFIX)GENERATE_TARGETS): $(GO_TARGET_PREFIX)generate.%:
	$(MAKE) -s -C $(call go_module_slug_to_path,$*) -f $(ROOT_MAKEFILE) $(GO_TARGET_PREFIX)_generate

.PHONY: $(GO_TARGET_PREFIX)_generate
$(GO_TARGET_PREFIX)_generate:
	$(GO_GENERATE) ./...

# fix, fix.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)FIX_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)fix.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)fix
$(GO_TARGET_PREFIX)fix: $($(GO_MK_VAR_PREFIX)FIX_TARGETS) ## Runs the go fix command.

.PHONY: $($(GO_MK_VAR_PREFIX)FIX_TARGETS)
$($(GO_MK_VAR_PREFIX)FIX_TARGETS): $(GO_TARGET_PREFIX)fix.%:
	$(MAKE) -s -C $(call go_module_slug_to_path,$*) -f $(ROOT_MAKEFILE) $(GO_TARGET_PREFIX)_fix

.PHONY: $(GO_TARGET_PREFIX)_fix
$(GO_TARGET_PREFIX)_fix:
	$(GO_FIX) ./...

# update, update.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)UPDATE_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)update.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)update
$(GO_TARGET_PREFIX)update: $($(GO_MK_VAR_PREFIX)UPDATE_TARGETS) ## Runs go get -u -t ./..., go get -u tool, then go mod tidy.

.PHONY: $(addprefix $(GO_TARGET_PREFIX)update.,$(GO_MODULE_SLUGS_EXCL_NO_UPDATE))
$(addprefix $(GO_TARGET_PREFIX)update.,$(GO_MODULE_SLUGS_EXCL_NO_UPDATE)): $(GO_TARGET_PREFIX)update.%:
	@$(MAKE) -C $(call go_module_slug_to_path,$*) -f $(ROOT_MAKEFILE) $(GO_TARGET_PREFIX)_update

.PHONY: $(addprefix $(GO_TARGET_PREFIX)update.,$(GO_MODULE_SLUGS_NO_UPDATE))
$(addprefix $(GO_TARGET_PREFIX)update.,$(GO_MODULE_SLUGS_NO_UPDATE)): $(GO_TARGET_PREFIX)update.%: $(GO_TARGET_PREFIX)tidy.%

# N.B. Uses the "tool" reserved package - see `go help packages | less`.
.PHONY: $(GO_TARGET_PREFIX)_update
$(GO_TARGET_PREFIX)_update:
	$(GO) get -u -t ./...
	$(GO) get -u tool
	$(GO) mod tidy

# tidy, tidy.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)TIDY_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)tidy.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)tidy
$(GO_TARGET_PREFIX)tidy: $($(GO_MK_VAR_PREFIX)TIDY_TARGETS) ## Runs go mod tidy.

.PHONY: $($(GO_MK_VAR_PREFIX)TIDY_TARGETS)
$($(GO_MK_VAR_PREFIX)TIDY_TARGETS): $(GO_TARGET_PREFIX)tidy.%:
	@$(MAKE) -C $(call go_module_slug_to_path,$*) -f $(ROOT_MAKEFILE) $(GO_TARGET_PREFIX)_tidy

.PHONY: $(GO_TARGET_PREFIX)_tidy
$(GO_TARGET_PREFIX)_tidy:
	$(GO) mod tidy

# grit, grit.<go module slug>

$(eval $(GO_MK_VAR_PREFIX)GRIT_TARGETS := $$(addprefix $$(GO_TARGET_PREFIX)grit.,$$(GO_MODULE_SLUGS)))

.PHONY: $(GO_TARGET_PREFIX)grit
$(GO_TARGET_PREFIX)grit: $($(GO_MK_VAR_PREFIX)GRIT_TARGETS) ## Runs grit to sync modules to defined target repositories.

.PHONY: $(addprefix $(GO_TARGET_PREFIX)grit.,$(GO_MODULE_SLUGS_GRIT_DST))
$(addprefix $(GO_TARGET_PREFIX)grit.,$(GO_MODULE_SLUGS_GRIT_DST)): $(GO_TARGET_PREFIX)grit.%:
	$(GRIT) $(GRIT_FLAGS) $(call go_module_slug_to_grit_src,$*) $(call go_module_slug_to_grit_dst,$*)

.PHONY: $(addprefix $(GO_TARGET_PREFIX)grit.,$(GO_MODULE_SLUGS_EXCL_GRIT_DST))
$(addprefix $(GO_TARGET_PREFIX)grit.,$(GO_MODULE_SLUGS_EXCL_GRIT_DST)): $(GO_TARGET_PREFIX)grit.%:

# ---

##@ Sub-Makefile Targets

##+ run.<./**/Makefile path as slug>: Runs make at the given path.
# This is a pattern rule. These per-makefile default targets will show up in
# shell completion. They're a separate process, i.e. are independent.

SUBDIR_MAKEFILE_TARGETS := $(addprefix run.,$(SUBDIR_MAKEFILE_SLUGS))

.PHONY: $(SUBDIR_MAKEFILE_TARGETS)
$(SUBDIR_MAKEFILE_TARGETS): run.%:
	@$(MAKE) -C $(call subdir_makefile_slug_to_path,$*) $(RUN_FLAGS)

# makefile implicit rules

##+ run-%.<./**/Makefile path as slug>: Runs make target at the given path.
# Note that eval is necessary to make this work properly, as a pattern rule can
# only be used once. The $(GO_TARGET_PREFIX)FORCE target is used as a dummy,
# since GNU Make requires .PHONY targets to be explicit (not implicit).
define _run_TEMPLATE =
run-%.$2: $(GO_TARGET_PREFIX)FORCE
	@$$(MAKE) -C $1 $(RUN_FLAGS) $$*

endef
# warning: simply-expanded
$(foreach d,$(SUBDIR_MAKEFILE_PATHS),$(eval $(call _run_TEMPLATE,$d,$(call subdir_makefile_path_to_slug,$d))))

# ---

##@ Other Targets

.PHONY: $(GO_TARGET_PREFIX)tools
$(GO_TARGET_PREFIX)tools: ## Uses go get -tool to add the tools for _this_ Makefile to go.mod.
	$(foreach tool,$(GO_TOOLS),$(_tools_TEMPLATE))
define _tools_TEMPLATE =
$(GO) get -tool $(tool)

endef

.PHONY: $(GO_TARGET_PREFIX)godoc
$(GO_TARGET_PREFIX)godoc: ## Runs the godoc tool, serving on localhost.
ifeq ($(GODOC_FLAGS),$(_GODOC_FLAGS))
	@echo '#################################################'
	@echo '## Serving godoc on http://localhost:6060/pkg/ ##'
	@echo '## Press Ctrl+C to stop godoc server.          ##'
	@echo '#################################################'
	@echo
endif
	$(GODOC) $(GODOC_FLAGS)

.PHONY: $(GO_TARGET_PREFIX)grit-init
$(GO_TARGET_PREFIX)grit-init: ## Runs grit to initialize a new GRIT_DST, see Makefile for docs.
ifeq ($(IS_WINDOWS),true)
	if exist $(_grit_init_DIR) exit 1
else
	if [ -d $(_grit_init_DIR) ]; then exit 1; fi
endif
	$(GRIT) $(GRIT_FLAGS) $(_grit_init_SRC) $(_grit_init_DST)

_grit_init_SRC = $(or $(and $(GRIT_INIT_TARGET),$(call go_module_slug_to_grit_dst,$(GRIT_INIT_TARGET))),$(error GRIT_INIT_TARGET is not set))
_grit_init_DIR = $(or $(patsubst ./%,%,$(or $(and $(GRIT_INIT_TARGET),$(or $(call slug_parse,$(GRIT_INIT_TARGET)),$(error failed to determine grit dir: invalid GRIT_INIT_TARGET: $(GRIT_INIT_TARGET)))),$(error GRIT_INIT_TARGET is not set))),$(error failed to determine grit dir: invalid GRIT_INIT_TARGET: $(GRIT_INIT_TARGET)))
_grit_init_DST = $(GRIT_SRC),$(_grit_init_DIR)/,$(GRIT_BRANCH)

.PHONY: $(GO_TARGET_PREFIX)debug-vars
$(GO_TARGET_PREFIX)debug-vars: ## Prints the values of the specified variables.
	$(foreach debug_var,$($(GO_MK_VAR_PREFIX)DEBUG_VARS),$(_debug_vars_TEMPLATE))
define _debug_vars_TEMPLATE =
@echo $(debug_var)=$(call escape_command_arg,$($(debug_var)))

endef

ifneq ($(IS_WINDOWS),true)
ifneq ($(SKIP_FURTHER_MAKEFILE_HELP),true)
SKIP_FURTHER_MAKEFILE_HELP := true
ifndef MAKEFILE_HELP_SCRIPT
define _MAKEFILE_HELP_SCRIPT :=
# Run a command with args, auto-detecting color stripping and paging of stdout.
run_with_smart_human_readable_output() {
  if ! [ $$# -ge 1 ]; then
    echo "Usage: run_with_smart_human_readable_output <command> [args...]" >&2
    return 2
  fi

  # ansi color stripping sed script https://stackoverflow.com/a/51141872
  # N.B. local variables are not POSIX
  _run_with_smart_human_readable_output_strip_color='s/\x1B\[[0-9;]\{1,\}[A-Za-z]//g'

  if ! [ -t 1 ]; then
    # non-terminal output (e.g., piped or redirected) - strip color
    "$$@" | sed "$$_run_with_smart_human_readable_output_strip_color"
    return "$$?"
  fi

  # terminal output...

  # check if color is supported
  command -v tput >/dev/null 2>&1 &&
  _run_with_smart_human_readable_output_tput_colors="$$(tput colors 2>/dev/null)" ||
  _run_with_smart_human_readable_output_tput_colors=0

  # run command, with pager, if available
  if command -v less >/dev/null 2>&1; then
    if [ "$$_run_with_smart_human_readable_output_tput_colors" -gt 0 ]; then
      "$$@" | less -R
    else
      "$$@" | sed "$$_run_with_smart_human_readable_output_strip_color" | less
    fi
  elif command -v more >/dev/null; then
    # note 1: the above deliberately leaves stderr alone, so that error from at
    # least one `command` builtin is shown (idk, might be some weird shells
    # around)
    # note 2: didnt bother checking if `more` (consistently) supports color
    "$$@" | sed "$$_run_with_smart_human_readable_output_strip_color" | more
  elif [ "$$_run_with_smart_human_readable_output_tput_colors" -gt 0 ]; then
    "$$@"
  else
    "$$@" | sed "$$_run_with_smart_human_readable_output_strip_color"
  fi
} &&
generate_help='
BEGIN {
  FS = ":.*##";
  # Print the initial usage message
  printf "\nUsage:\n  $(or $(notdir $(MAKE)),make) \033[36m<target>\033[0m\n";
  in_usage_block = 0;       # Flag: currently inside a documentation block
  usage_marker_found = 0;   # Flag: Saw "# Usage" line, looking for "# ---"
  current_doc_file = "";    # Key (relative path or special marker) for storing docs
  doc_separator = "\n\n";   # Separator between doc blocks if multiple in one file
  # Format for target help lines.
  # - \033[36m: ANSI escape code for cyan color
  # - %-18s: Left-justify string in a field of 18 characters
  # - \033[0m: ANSI escape code to reset color/attributes
  target_format = "  \033[36m%-35s\033[0m %s\n";
}

# Match section headers (##@ ) - Print only when not capturing docs.
/^##@ / {
  if (!in_usage_block) {
    printf "\n\033[1m%s\033[0m\n", substr($$0, 5);
  }
}

# Match target lines (target: ... ## description) - Print only when not capturing docs.
# N.B. Inclusive of target prefix variables - that is handled afterwards.
/^[\$$\(\)a-zA-Z0-9._%-]+:.*?##/ {
  if (!in_usage_block) {
    printf target_format, $$1, $$2;
  }
}

# Manually documented targets, like "##+ target: [description]".
/^##\+.*:/ {
  if (!in_usage_block) {
    colon_pos = index($$0, ":");
    if (colon_pos > 0) {
      manual_target = substr($$0, 1, colon_pos - 1);
      sub(/^##\+ */, "", manual_target);
      sub(/ +$$/, "", manual_target);

      manual_description = substr($$0, colon_pos + 1);
      sub(/^ +/, "", manual_description);
      sub(/ +$$/, "", manual_description);

      printf target_format, manual_target, manual_description;
    }
  }
}

# --- Documentation Block Parsing Logic ---

# Detect start of documentation block: line 1 "# Usage"
# Note: Exact match, case-sensitive. Anchored start/end.
/^# Usage$$/ {
  usage_marker_found = 1; # Mark that we found the first part
  next; # Skip processing this line further, move to the next line
}

# Detect start of documentation block: line 2 "# ---" (only if line 1 matched)
# Note: Exact match, case-sensitive. Anchored start/end.
/^# ---$$/ && usage_marker_found {
  in_usage_block = 1;       # We are now officially inside a documentation block
  usage_marker_found = 0;   # Reset the marker for the next potential block

  # Calculate relative path for this file
  rel_path = FILENAME;
  # Ensure project_root has no trailing slash for safety, then remove prefix
  # Use gsub for global replacement in case project_root appears elsewhere, though unlikely here.
  gsub(/\/$$/, "", project_root); # Remove trailing slash from project_root if exists
  # Escape potential regex special chars in project_root before using in sub() if needed,
  # but direct string prefix removal is usually safe here.
  # Add ^ to anchor the substitution at the beginning.
  sub("^" project_root "/", "", rel_path);

  # Determine the storage key: special for "Makefile", relative path otherwise
  # Use tolower() for case-insensitive comparison of the filename part
  if (tolower(rel_path) == "makefile") {
    current_doc_file = "__MAIN_MAKEFILE__"; # Special key for root Makefile
  } else {
    current_doc_file = rel_path; # Use relative path as key
  }

  # Initialize documentation storage if this is the first block for this file
  if (!(current_doc_file in makefile_docs)) {
    makefile_docs[current_doc_file] = "";
  } else if (makefile_docs[current_doc_file] != "") {
    # If adding another block from the *same* file, add a separator
    makefile_docs[current_doc_file] = makefile_docs[current_doc_file] doc_separator;
  }
  next; # Skip processing the "---" line itself
}

# Capture documentation lines (lines starting with '#' while inside a block)
in_usage_block && /^#/ {
  line = $$0;
  # Remove leading "# " or just "#"
  sub(/^# ?/, "", line);
  # Append the cleaned line, prefixed with indent, suffixed with a newline character
  makefile_docs[current_doc_file] = makefile_docs[current_doc_file] "  " line "\n";
  next; # Process next line
}

# End of documentation block (non-comment line encountered while inside a block)
in_usage_block && !/^#/ {
  in_usage_block = 0;     # Exit documentation capture mode
  current_doc_file = "";  # Clear the current file key
  # Reset the initial marker just in case, though !/^#/ below also handles it
  usage_marker_found = 0;
  # IMPORTANT: This line itself is NOT processed further in this cycle.
  # If it were a target or header, it would be missed. This is a simplification:
  # assumes documentation blocks are not immediately followed by lines
  # that *also* need processing by other rules in the same cycle.
  # The checks `if (!in_usage_block)` in other rules prevent them from running
  # while capturing, so this non-comment line effectively stops capture and is ignored.
}

# Reset usage marker if we see a non-matching line while waiting for "# ---"
!/^# ---$$/ && usage_marker_found {
  usage_marker_found = 0; # Failed to find "# ---" immediately after "# Usage"
}

# --- END Block: Print Collected Documentation ---

END {
  # Check if any documentation was collected
  doc_exists = 0;
  for (file_key in makefile_docs) {
    # Remove trailing newline from the collected block before checking if empty
    gsub(/\n$$/, "", makefile_docs[file_key]);
    if (makefile_docs[file_key] != "") {
      doc_exists = 1;
      break;
    }
  }

  if (doc_exists) {
    # Print a main header for the documentation section
    printf "\n\033[1mNotes\033[0m\n";

    # Print main Makefile documentation first if it exists and is not empty
    main_doc_key = "__MAIN_MAKEFILE__";
    if (main_doc_key in makefile_docs && makefile_docs[main_doc_key] != "") {
      # Header with underline
      printf "\n\033[4mMakefile:\033[0m\n%s\n", makefile_docs[main_doc_key];
    }

    # Print documentation from other Makefiles
    # Awk array iteration order is not guaranteed, but often insertion order or hash order.
    # Sorting keys would require GNU awk typically. For simplicity, accept awk default order.
    for (file_key in makefile_docs) {
      if (file_key != main_doc_key && makefile_docs[file_key] != "") {
        # Header with underline, showing the relative path
        printf "\n\033[4m%s:\033[0m\n%s\n", file_key, makefile_docs[file_key];
      }
    }
  }
}
' &&
deduplicated_makefile_list="$$(printf '%s\n' "$$MAKEFILE_LIST" | awk '{for(i=NF;i>=1;i--)if(!a[$$i]++)s=$$i (s==""?"":" ")s;printf "%s",s}')" &&
help_text="$$(awk -v project_root=$(call escape_command_arg,$(PROJECT_ROOT)) "$$generate_help" $${deduplicated_makefile_list})" &&
help_text="$$(echo "$$help_text" | sed $(foreach target_prefix,GO_TARGET_PREFIX $(MAKEFILE_TARGET_PREFIXES), -e s/\$$\($(call escape_command_arg,$(target_prefix))\)/$(call escape_command_arg,$($(target_prefix)))/g\;))" &&
run_with_smart_human_readable_output echo "$$help_text"
endef
export _MAKEFILE_HELP_SCRIPT
MAKEFILE_HELP_SCRIPT := eval "$$_MAKEFILE_HELP_SCRIPT"
endif

.PHONY: help
help: ## Display this help.
	@export MAKEFILE_LIST=$(call escape_command_arg,$(MAKEFILE_LIST)); $(MAKEFILE_HELP_SCRIPT)

.PHONY: h
h: help ## Alias for help.
endif
endif

# ---

# misc targets users can ignore

# we use .PHONY, but there's an edge case requiring this pattern
.PHONY: $(GO_TARGET_PREFIX)FORCE
$(GO_TARGET_PREFIX)FORCE:
