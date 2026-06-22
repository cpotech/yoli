package cli

// Version is the yoli CLI version reported by the `version` and
// `--version` subcommands. The default value is overridden at build
// time via -ldflags "-X yoli/internal/cli.Version=<value>" (see
// scripts/build.sh), which populates it from `git describe --tags
// --dirty`. Builds without the linker flag report "dev".
var Version = "dev"
