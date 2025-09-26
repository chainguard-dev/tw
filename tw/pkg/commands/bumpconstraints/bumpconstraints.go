package bumpconstraints

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pep440 "github.com/aquasecurity/go-pep440-version"
	"github.com/chainguard-dev/clog"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cobra"
)

type cfg struct {
	ConstraintsFile string
	OnlyReplace     bool
	Packages        []string
	UpdatesFile     string
	ShowDiff        bool
}

type Constraint struct {
	Package  string
	Operator string
	Version  string
	Comment  string
}

type PackageUpdate struct {
	Package string
	Version string
	Comment string
}

// UpdateErrors accumulates multiple errors during processing
type UpdateErrors struct {
	Errors []error
}

func (e *UpdateErrors) Error() string {
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	var msgs []string
	for _, err := range e.Errors {
		msgs = append(msgs, "  • "+err.Error())
	}
	return fmt.Sprintf("encountered %d error(s):\n%s", len(e.Errors), strings.Join(msgs, "\n"))
}

func (e *UpdateErrors) Add(err error) {
	e.Errors = append(e.Errors, err)
}

func (e *UpdateErrors) HasErrors() bool {
	return len(e.Errors) > 0
}

func Command() *cobra.Command {
	cfg := &cfg{}

	cmd := &cobra.Command{
		Use:   "bumpconstraints [PACKAGE==VERSION ...]",
		Short: "Bump Python constraint pins in constraints.txt file",
		Long: `Update Python package versions in a constraints file.

Package specifications should be in the format: package==version # comment
Comments (starting with #) are optional but recommended for documenting why versions are being bumped.

Package updates can be provided as arguments or read from a file using -u/--updates-file.

Examples:
  tw bumpconstraints "requests==2.31.0 # Security update CVE-2023-XXXXX"
  tw bumpconstraints -c requirements.txt "django==4.2.0 # LTS version"
  tw bumpconstraints -u updates.txt -c constraints.txt
  tw bumpconstraints --only-replace=false "newpackage==1.0.0 # Adding new dependency"`,
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.Packages = args
			return cfg.Run(cmd)
		},
	}

	cmd.Flags().StringVarP(&cfg.ConstraintsFile, "constraints-file", "c", "constraints.txt", "Path to the constraints file to update")
	cmd.Flags().StringVarP(&cfg.UpdatesFile, "updates-file", "u", "", "Path to file containing package updates (one per line)")
	cmd.Flags().BoolVar(&cfg.OnlyReplace, "only-replace", true, "Only update packages already in the constraints file")
	cmd.Flags().BoolVar(&cfg.ShowDiff, "show-diff", false, "Show diff of changes made")

	return cmd
}

func (c *cfg) Run(cmd *cobra.Command) error {
	ctx := cmd.Context()
	log := clog.FromContext(ctx)

	// Load package updates from file if specified
	if c.UpdatesFile != "" {
		fileUpdates, err := c.loadUpdatesFromFile()
		if err != nil {
			return fmt.Errorf("failed to load updates from file: %w", err)
		}
		c.Packages = append(c.Packages, fileUpdates...)
	}

	// Check that we have updates to process
	if len(c.Packages) == 0 {
		return fmt.Errorf("no package updates specified (provide as arguments or use -u/--updates-file)")
	}

	// Parse package updates from arguments
	updates, err := c.parsePackageUpdates()
	if err != nil {
		return fmt.Errorf("failed to parse package updates: %w", err)
	}

	// Check if constraints file exists
	fileInfo, err := os.Stat(c.ConstraintsFile)
	if os.IsNotExist(err) {
		return fmt.Errorf("constraints file '%s' not found", c.ConstraintsFile)
	}
	if err != nil {
		return fmt.Errorf("failed to stat constraints file: %w", err)
	}

	log.InfoContextf(ctx, "Updating constraints in: %s", c.ConstraintsFile)

	// Create backup with original permissions
	backupFile := c.ConstraintsFile + ".bak"
	if err := c.createBackup(backupFile, fileInfo.Mode()); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	log.InfoContextf(ctx, "Created backup: %s", backupFile)

	// Ensure cleanup on error
	success := false
	defer func() {
		if !success {
			// Attempt to restore from backup on failure
			if restoreErr := c.restoreFromBackup(backupFile); restoreErr != nil {
				log.ErrorContextf(ctx, "Failed to restore from backup: %v", restoreErr)
			}
		}
	}()

	// Read and parse existing constraints
	constraints, lines, err := c.parseConstraintsFile()
	if err != nil {
		return fmt.Errorf("failed to parse constraints file: %w", err)
	}

	// Build a map of existing constraints for quick lookup
	constraintMap := make(map[string]*Constraint)
	for i := range constraints {
		constraintMap[constraints[i].Package] = &constraints[i]
	}

	// Process updates
	updateErrors := &UpdateErrors{}
	updatedPackages := make(map[string]bool)
	newLines := make([]string, len(lines))
	copy(newLines, lines)

	for _, update := range updates {
		log.InfoContextf(ctx, "Processing: %s -> %s", update.Package, update.Version)

		existingConstraint, exists := constraintMap[update.Package]

		// Check if package should be updated
		if c.OnlyReplace && !exists {
			err := fmt.Errorf("package '%s' not found in constraints file (use --only-replace=false to add new packages)", update.Package)
			updateErrors.Add(err)
			log.ErrorContext(ctx, err.Error())
			continue
		}

		if exists {
			// Compare versions using PEP 440
			existingVer, existingErr := pep440.Parse(existingConstraint.Version)
			newVer, newErr := pep440.Parse(update.Version)

			// If both versions parse successfully, do proper comparison
			if existingErr == nil && newErr == nil {
				comparison := existingVer.Compare(newVer)

				if comparison > 0 {
					// Existing version is greater (downgrade)
					err := fmt.Errorf("cannot downgrade %s from %s to %s", update.Package, existingConstraint.Version, update.Version)
					updateErrors.Add(err)
					log.ErrorContext(ctx, err.Error())
					continue
				}

				if comparison == 0 {
					// Versions are equal
					err := fmt.Errorf("constraint for %s already matches version %s (can be removed from update list)", update.Package, update.Version)
					updateErrors.Add(err)
					log.ErrorContext(ctx, err.Error())
					continue
				}
			} else {
				// Fall back to string comparison if parsing fails
				if existingErr != nil {
					log.WarnContextf(ctx, "Could not parse existing version for %s: %s (using string comparison)", update.Package, existingConstraint.Version)
				}
				if newErr != nil {
					log.WarnContextf(ctx, "Could not parse new version for %s: %s (using string comparison)", update.Package, update.Version)
				}

				if existingConstraint.Version == update.Version {
					err := fmt.Errorf("constraint for %s already matches version %s (can be removed from update list)", update.Package, update.Version)
					updateErrors.Add(err)
					log.ErrorContext(ctx, err.Error())
					continue
				}
			}

			// Update existing constraint
			newConstraint := fmt.Sprintf("%s%s%s", update.Package, existingConstraint.Operator, update.Version)
			if update.Comment != "" {
				newConstraint += " # " + update.Comment
			} else if existingConstraint.Comment != "" {
				// Preserve existing comment if no new comment provided
				newConstraint += " # " + existingConstraint.Comment
			}

			// Find and update the correct line
			lineIndex := c.findConstraintLine(lines, update.Package)
			if lineIndex >= 0 {
				oldLine := newLines[lineIndex]
				newLines[lineIndex] = newConstraint
				log.InfoContextf(ctx, "  Updated: %s -> %s", strings.TrimSpace(oldLine), newConstraint)
				updatedPackages[update.Package] = true
			} else {
				err := fmt.Errorf("internal error: could not find line for package %s", update.Package)
				updateErrors.Add(err)
				log.ErrorContext(ctx, err.Error())
			}
		} else {
			// Add new constraint
			newConstraint := fmt.Sprintf("%s==%s", update.Package, update.Version)
			if update.Comment != "" {
				newConstraint += " # " + update.Comment
			}
			newLines = append(newLines, newConstraint)
			log.InfoContextf(ctx, "  Added: %s", newConstraint)
			updatedPackages[update.Package] = true
		}
	}

	// Check if there were any errors
	if updateErrors.HasErrors() {
		return updateErrors
	}

	// Write updated constraints file with original permissions
	if err := c.writeConstraintsFile(newLines, fileInfo.Mode()); err != nil {
		return fmt.Errorf("failed to write updated constraints file: %w", err)
	}

	success = true
	log.InfoContextf(ctx, "Successfully updated %s", c.ConstraintsFile)

	// Show diff if requested
	if c.ShowDiff {
		if err := c.showDiff(backupFile); err != nil {
			log.WarnContextf(ctx, "Could not show diff: %v", err)
		}
	}

	return nil
}

// findConstraintLine finds the line index for a specific package
func (c *cfg) findConstraintLine(lines []string, packageName string) int {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			if constraint := c.parseLine(trimmed); constraint != nil {
				if constraint.Package == packageName {
					return i
				}
			}
		}
	}
	return -1
}

func (c *cfg) parsePackageUpdates() ([]PackageUpdate, error) {
	var updates []PackageUpdate
	seen := make(map[string]bool)

	for _, pkg := range c.Packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}

		// Split by comment marker
		parts := strings.SplitN(pkg, "#", 2)
		specPart := strings.TrimSpace(parts[0])
		comment := ""
		if len(parts) > 1 {
			comment = strings.TrimSpace(parts[1])
		}

		// Parse package specification
		if !strings.Contains(specPart, "==") {
			return nil, fmt.Errorf("invalid package specification '%s'. Use format: package==version", specPart)
		}

		pkgParts := strings.SplitN(specPart, "==", 2)
		packageName := strings.TrimSpace(pkgParts[0])
		version := strings.TrimSpace(pkgParts[1])

		if packageName == "" || version == "" {
			return nil, fmt.Errorf("invalid package specification '%s'. Use format: package==version", specPart)
		}

		// Check for duplicates
		if seen[packageName] {
			return nil, fmt.Errorf("duplicate package specification for '%s'", packageName)
		}
		seen[packageName] = true

		// Validate package name (basic validation)
		if strings.ContainsAny(packageName, " \t\n\r") {
			return nil, fmt.Errorf("invalid package name '%s': contains whitespace", packageName)
		}

		updates = append(updates, PackageUpdate{
			Package: packageName,
			Version: version,
			Comment: comment,
		})
	}

	return updates, nil
}

func (c *cfg) parseConstraintsFile() ([]Constraint, []string, error) {
	file, err := os.Open(c.ConstraintsFile)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	var constraints []Constraint
	var lines []string

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		lines = append(lines, line)

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		constraint := c.parseLine(trimmed)
		if constraint != nil {
			constraints = append(constraints, *constraint)
		} else if trimmed != "" {
			// Log lines that look like constraints but couldn't be parsed
			clog.FromContext(nil).WarnContextf(nil, "Could not parse constraint on line %d: %s", lineNum, trimmed)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}

	return constraints, lines, nil
}

func (c *cfg) parseLine(line string) *Constraint {
	// Remove inline comments
	parts := strings.SplitN(line, "#", 2)
	constraintPart := strings.TrimSpace(parts[0])
	comment := ""
	if len(parts) > 1 {
		comment = strings.TrimSpace(parts[1])
	}

	// Parse the constraint
	// Support various operators: ==, >=, <=, !=, ~=, >, <
	// Order matters: check longer operators first
	operators := []string{"==", ">=", "<=", "!=", "~=", ">", "<"}

	// Sort operators by length (descending) to check longer ones first
	sort.Slice(operators, func(i, j int) bool {
		return len(operators[i]) > len(operators[j])
	})

	for _, op := range operators {
		if idx := strings.Index(constraintPart, op); idx > 0 {
			packageName := strings.TrimSpace(constraintPart[:idx])
			version := strings.TrimSpace(constraintPart[idx+len(op):])

			if packageName != "" && version != "" {
				return &Constraint{
					Package:  packageName,
					Operator: op,
					Version:  version,
					Comment:  comment,
				}
			}
		}
	}

	return nil
}

func (c *cfg) createBackup(backupFile string, mode os.FileMode) error {
	input, err := os.ReadFile(c.ConstraintsFile)
	if err != nil {
		return err
	}
	return os.WriteFile(backupFile, input, mode)
}

func (c *cfg) restoreFromBackup(backupFile string) error {
	input, err := os.ReadFile(backupFile)
	if err != nil {
		return err
	}
	info, err := os.Stat(backupFile)
	if err != nil {
		return err
	}
	return os.WriteFile(c.ConstraintsFile, input, info.Mode())
}

func (c *cfg) writeConstraintsFile(lines []string, mode os.FileMode) error {
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(c.ConstraintsFile, []byte(content), mode)
}

func (c *cfg) loadUpdatesFromFile() ([]string, error) {
	file, err := os.Open(c.UpdatesFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var updates []string
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and pure comment lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		updates = append(updates, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading updates file at line %d: %w", lineNum, err)
	}

	return updates, nil
}

func (c *cfg) showDiff(backupFile string) error {
	// Get the absolute paths for better output
	absBackup, _ := filepath.Abs(backupFile)
	absConstraints, _ := filepath.Abs(c.ConstraintsFile)

	fmt.Println("\nChanges made:")

	// Read both files
	oldContent, err := os.ReadFile(backupFile)
	if err != nil {
		return err
	}

	newContent, err := os.ReadFile(c.ConstraintsFile)
	if err != nil {
		return err
	}

	// Split content into lines for difflib
	oldLines := strings.Split(string(oldContent), "\n")
	newLines := strings.Split(string(newContent), "\n")

	// Generate unified diff using go-difflib
	unifiedDiff := difflib.UnifiedDiff{
		A:        oldLines,
		B:        newLines,
		FromFile: absBackup,
		ToFile:   absConstraints,
		Context:  3, // Show 3 lines of context around changes
	}

	diffText, err := difflib.GetUnifiedDiffString(unifiedDiff)
	if err != nil {
		return fmt.Errorf("failed to generate diff: %w", err)
	}

	if diffText != "" {
		fmt.Print(diffText)
	} else {
		fmt.Println("No changes detected")
	}

	return nil
}
