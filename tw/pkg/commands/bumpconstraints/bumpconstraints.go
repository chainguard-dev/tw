package bumpconstraints

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/chainguard-dev/clog"
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
	Line     string
}

type PackageUpdate struct {
	Package string
	Version string
	Comment string
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
	if _, err := os.Stat(c.ConstraintsFile); os.IsNotExist(err) {
		return fmt.Errorf("constraints file '%s' not found", c.ConstraintsFile)
	}

	log.InfoContextf(ctx, "Updating constraints in: %s", c.ConstraintsFile)

	// Create backup
	backupFile := c.ConstraintsFile + ".bak"
	if err := c.createBackup(backupFile); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	log.InfoContextf(ctx, "Created backup: %s", backupFile)

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
	var errors []error
	updatedPackages := make(map[string]bool)
	newLines := make([]string, len(lines))
	copy(newLines, lines)

	for _, update := range updates {
		log.InfoContextf(ctx, "Processing: %s -> %s", update.Package, update.Version)

		existingConstraint, exists := constraintMap[update.Package]

		// Check if package should be updated
		if c.OnlyReplace && !exists {
			err := fmt.Errorf("package '%s' not found in constraints file (use --no-only-replace to add new packages)", update.Package)
			errors = append(errors, err)
			log.ErrorContext(ctx, err.Error())
			continue
		}

		if exists {
			// Compare versions
			existingVer, err := semver.NewVersion(existingConstraint.Version)
			if err != nil {
				log.WarnContextf(ctx, "Could not parse existing version for %s: %s (treating as string comparison)", update.Package, existingConstraint.Version)
			}

			newVer, err := semver.NewVersion(update.Version)
			if err != nil {
				log.WarnContextf(ctx, "Could not parse new version for %s: %s (treating as string comparison)", update.Package, update.Version)
			}

			// Check for downgrades (if both versions are valid semver)
			if existingVer != nil && newVer != nil {
				if newVer.LessThan(existingVer) {
					err := fmt.Errorf("cannot downgrade %s from %s to %s", update.Package, existingConstraint.Version, update.Version)
					errors = append(errors, err)
					log.ErrorContext(ctx, err.Error())
					continue
				}

				// Check if versions are equal
				if newVer.Equal(existingVer) {
					err := fmt.Errorf("constraint for %s already matches version %s (can be removed from update list)", update.Package, update.Version)
					errors = append(errors, err)
					log.ErrorContext(ctx, err.Error())
					continue
				}
			} else if existingConstraint.Version == update.Version {
				// String comparison for non-semver versions
				err := fmt.Errorf("constraint for %s already matches version %s (can be removed from update list)", update.Package, update.Version)
				errors = append(errors, err)
				log.ErrorContext(ctx, err.Error())
				continue
			}

			// Update existing constraint
			newConstraint := fmt.Sprintf("%s%s%s", update.Package, existingConstraint.Operator, update.Version)
			if update.Comment != "" {
				newConstraint += " # " + update.Comment
			} else if existingConstraint.Comment != "" {
				// Preserve existing comment if no new comment provided
				newConstraint += " # " + existingConstraint.Comment
			}

			// Find and update the line
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, update.Package) && !strings.HasPrefix(trimmed, "#") {
					// Check if this is the right line by parsing it
					if constraint := c.parseLine(trimmed); constraint != nil && constraint.Package == update.Package {
						oldLine := newLines[i]
						newLines[i] = newConstraint
						log.InfoContextf(ctx, "  Updated: %s -> %s", strings.TrimSpace(oldLine), newConstraint)
						updatedPackages[update.Package] = true
						break
					}
				}
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
	if len(errors) > 0 {
		return fmt.Errorf("encountered %d error(s) during processing", len(errors))
	}

	// Write updated constraints file
	if err := c.writeConstraintsFile(newLines); err != nil {
		return fmt.Errorf("failed to write updated constraints file: %w", err)
	}

	log.InfoContextf(ctx, "Successfully updated %s", c.ConstraintsFile)

	// Show diff if requested
	if c.ShowDiff {
		if err := c.showDiff(backupFile); err != nil {
			log.WarnContextf(ctx, "Could not show diff: %v", err)
		}
	}

	return nil
}

func (c *cfg) parsePackageUpdates() ([]PackageUpdate, error) {
	var updates []PackageUpdate

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
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if constraint := c.parseLine(trimmed); constraint != nil {
			constraint.Line = line
			constraints = append(constraints, *constraint)
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
	// Support various operators: ==, >=, <=, >, <, !=, ~=
	operators := []string{"==", ">=", "<=", "!=", "~=", ">", "<"}

	for _, op := range operators {
		if strings.Contains(constraintPart, op) {
			parts := strings.SplitN(constraintPart, op, 2)
			if len(parts) == 2 {
				return &Constraint{
					Package:  strings.TrimSpace(parts[0]),
					Operator: op,
					Version:  strings.TrimSpace(parts[1]),
					Comment:  comment,
				}
			}
		}
	}

	return nil
}

func (c *cfg) createBackup(backupFile string) error {
	input, err := os.ReadFile(c.ConstraintsFile)
	if err != nil {
		return err
	}
	return os.WriteFile(backupFile, input, 0644)
}

func (c *cfg) writeConstraintsFile(lines []string) error {
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(c.ConstraintsFile, []byte(content), 0644)
}

func (c *cfg) loadUpdatesFromFile() ([]string, error) {
	file, err := os.Open(c.UpdatesFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var updates []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and pure comment lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		updates = append(updates, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
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

	// Simple line-by-line diff
	oldLines := strings.Split(string(oldContent), "\n")
	newLines := strings.Split(string(newContent), "\n")

	fmt.Printf("--- %s\n", absBackup)
	fmt.Printf("+++ %s\n", absConstraints)

	// Find the differences
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}

	for i := 0; i < maxLen; i++ {
		oldLine := ""
		newLine := ""

		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine != newLine {
			if oldLine != "" && i < len(oldLines) {
				fmt.Printf("-%s\n", oldLine)
			}
			if newLine != "" && i < len(newLines) {
				fmt.Printf("+%s\n", newLine)
			}
		}
	}

	return nil
}
