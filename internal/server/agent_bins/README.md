# Generated Agent binaries

Agent executables are intentionally not committed to the source repository.

`scripts/build-release.sh` and `scripts/build-release.ps1` build every supported Agent target, copy the results into this directory, and only then compile the release server so the final release binary can still serve matching Agent downloads directly.

In an ordinary source checkout, `/download/vps-agent-*` redirects to the configured GitHub Release asset instead.
