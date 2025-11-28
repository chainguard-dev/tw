package checkers

import (
	"fmt"

	"github.com/chainguard-dev/cg-tw/package-type-check/pkg/utils"
)

func CheckDebugPackage(pkg string) error {
	fmt.Printf("Checking if package %s is a valid debug package\n", pkg)

	// Check 1: Package name contains debug indicators
	hasDebugName := utils.HasDebugPackageName(pkg)
	if !hasDebugName {
		return fmt.Errorf("FAIL [1/2]: Debug package [%s] does not contain '-dbg' or '-debug' in its name.\n"+
			"Debug packages should have '-dbg' or '-debug' in their name", pkg)
	}
	fmt.Printf("PASS [1/2]: Debug package [%s] has debug indicator in name\n", pkg)

	// Check 2: Package contains .debug files in /usr/lib/debug
	debugFiles, err := utils.GetDebugSymbolFiles(pkg)
	if err != nil {
		return err
	}
	if len(debugFiles) == 0 {
		return fmt.Errorf("FAIL [2/2]: Debug package [%s] does not contain any .debug files in /usr/lib/debug/.\n"+
			"Debug packages must contain debug symbol files", pkg)
	}
	fmt.Printf("PASS [2/2]: Debug package [%s] contains %d debug symbol files\n", pkg, len(debugFiles))

	return nil
}
