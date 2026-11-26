build_dir := "build"
binary := build_dir / "sshush"
binaryd := build_dir / "sshushd"
version := env("VERSION", "dev")
ldflags := "-X github.com/ollykeran/sshush/internal/version.Version=" + version

# Recipes with a script start with #!/bin/bash, not #!/usr/bin/env bash. On
# Windows just turns the interpreter's path into Git's with cygpath, so
# /bin/bash runs Git Bash; env would look bash up on PATH instead, and from
# PowerShell find WSL's bash.exe in System32 first, which cannot read the
# Windows path of the script just hands it. Every bash this runs on, the
# 3.2 macOS ships included, has /bin/bash.

import 'just/build.just'
import 'just/test.just'
import 'just/lint.just'
import 'just/dev.just'
import 'just/ci.just'
import 'just/go.just'
import 'just/demo.just'
