#!/usr/bin/env bash
# Run the app's unit tests (Swift Testing).
#
# With full Xcode `swift test` just works. With only the Command Line Tools the Testing framework is installed but
# SwiftPM does not look for it, so point the compiler and the linker at it.
set -euo pipefail
cd "$(dirname "$0")"
FLAGS=()
if [[ "$(xcode-select -p 2>/dev/null)" == /Library/Developer/CommandLineTools* ]]; then
  F=/Library/Developer/CommandLineTools/Library/Developer/Frameworks
  FLAGS=(-Xswiftc "-F$F" -Xlinker "-F$F" -Xlinker -rpath -Xlinker "$F")
fi
exec swift test ${FLAGS[@]+"${FLAGS[@]}"} "$@"
