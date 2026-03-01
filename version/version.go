package version

// Version holds the build version, can be set via ldflags during build
// Default to current version for backwards compatibility with existing builds
// Harness CI builds will start at 1.0.0 and follow semantic versioning
var Version = "0.5.70"
