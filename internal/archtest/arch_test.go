package archtest_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

type PackageInfo struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
	Deps       []string `json:"Deps"`
}

const modulePrefix = "github.com/Alex84K/sm_smart_home_tg/"
const contractModulePrefix = "github.com/Alex84K/sm_smart_home_core_go/contract"

func TestArchitectureImports(t *testing.T) {
	cmd := exec.Command("go", "list", "-json", "./...")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("go list failed: %v: %s", err, stderr.String())
	}

	decoder := json.NewDecoder(&stdout)
	var packages []PackageInfo

	for decoder.More() {
		var p PackageInfo
		if err := decoder.Decode(&p); err != nil {
			t.Fatalf("failed to decode go list output: %v", err)
		}
		if strings.HasPrefix(p.ImportPath, modulePrefix) {
			packages = append(packages, p)
		}
	}

	if len(packages) == 0 {
		t.Fatal("no packages found in module")
	}

	for _, p := range packages {
		relPath := strings.TrimPrefix(p.ImportPath, modulePrefix)
		if strings.HasPrefix(relPath, "internal/archtest") {
			continue
		}

		t.Run(relPath, func(t *testing.T) {
			for _, imp := range p.Imports {
				checkDirectImportRule(t, relPath, imp)
			}
		})
	}
}

func checkDirectImportRule(t *testing.T, from, imp string) {
	t.Helper()

	isContract := imp == contractModulePrefix || strings.HasPrefix(imp, contractModulePrefix+"/")
	isInternal := strings.HasPrefix(imp, modulePrefix)
	if !isInternal && !isContract {
		// External / stdlib dependency
		return
	}

	relImp := strings.TrimPrefix(imp, modulePrefix)

	// Rule 1: platform/* can only import platform/*
	if strings.HasPrefix(from, "internal/platform/") {
		if !strings.HasPrefix(relImp, "internal/platform/") {
			t.Errorf("violation of ADR-0016/D1: platform package %s cannot import %s", from, imp)
		}
		return
	}

	// Rule 2: coreclient can import platform/* and contract
	if strings.HasPrefix(from, "internal/coreclient") {
		if !strings.HasPrefix(relImp, "internal/platform/") && !isContract {
			t.Errorf("violation of ADR-0016/D1: coreclient package %s cannot import %s", from, imp)
		}
		return
	}

	// Rule 3: telegram/app can import everything inside gateway and contract
	if strings.HasPrefix(from, "internal/telegram/app") {
		// Can import internal/* and contract
		return
	}

	// Rule 4: cmd/tg-gateway can only import its own app and platform/*
	if strings.HasPrefix(from, "cmd/tg-gateway") {
		if relImp != "internal/telegram/app" && !strings.HasPrefix(relImp, "internal/telegram/app/") && !strings.HasPrefix(relImp, "internal/platform/") {
			t.Errorf("violation of ADR-0016/D1: cmd/tg-gateway cannot import %s (only its app and platform/*)", imp)
		}
		return
	}
}
