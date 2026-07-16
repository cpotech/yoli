package cli

// Version is the yoli CLI version reported by the `version` and
// `--version` subcommands. The default value is overridden at build
// time via -ldflags "-X yoli/internal/cli.Version=<value>", populated
// from `git describe --tags --dirty --always` by scripts/build.sh
// (and stamped into every artifact by scripts/release.sh).
// With a reachable tag this yields e.g. v0.1.0 or v0.1.0-3-ga011326-dirty;
// without one it falls back to the short commit sha (optionally -dirty);
// builds with neither git nor a linker flag report "dev".
var Version = "dev"
