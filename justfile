build_dir := "build"
binary := build_dir / "sshush"
binaryd := build_dir / "sshushd"
version := env("VERSION", "dev")
ldflags := "-X github.com/ollykeran/sshush/internal/version.Version=" + version

import 'just/build.just'
import 'just/test.just'
import 'just/lint.just'
import 'just/dev.just'
import 'just/ci.just'
