package migrations

import "fmt"

// miseReshimSuffix rebuilds mise shims after a global npm install: when npm
// itself is mise-managed, package binaries land inside the node install dir
// and are invisible on PATH until shims are regenerated.
const miseReshimSuffix = `; command -v mise >/dev/null 2>&1 && mise reshim >/dev/null 2>&1 || true`

// npmCLIInstallTemplate installs a global npm package. {{base_url}} becomes
// --registry when a CLI install source is configured; empty means npm default.
func npmCLIInstallTemplate(pkg, label string) string {
	return fmt.Sprintf(
		`version="{{version}}"; base="{{base_url}}"; reg=""; [ -n "$base" ] && reg="--registry $base"; echo "installing %s ${base:+registry=$base }version=$version"; command -v npm >/dev/null 2>&1 || { echo 'npm is required'; exit 1; }; npm install -g %s${version:+@$version} $reg`+miseReshimSuffix,
		label, pkg,
	)
}

func npmCLIUpgradeTemplate(pkg, label string) string {
	return fmt.Sprintf(
		`version="{{version}}"; base="{{base_url}}"; reg=""; [ -n "$base" ] && reg="--registry $base"; echo "upgrading %s ${base:+registry=$base }version=$version"; command -v npm >/dev/null 2>&1 || { echo 'npm is required'; exit 1; }; npm install -g %s@${version:-latest} $reg`+miseReshimSuffix,
		label, pkg,
	)
}

func npmCLIUninstallTemplate(pkg string) string {
	return fmt.Sprintf(
		`command -v npm >/dev/null 2>&1 && npm uninstall -g %s || true`+miseReshimSuffix,
		pkg,
	)
}
