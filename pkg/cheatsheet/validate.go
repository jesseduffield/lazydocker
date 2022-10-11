package cheatsheet

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jesseduffield/lazycore/pkg/utils"
	"github.com/pmezard/go-difflib/difflib"
)

func Check() {
	dir := GetKeybindingsDir()
	tmpDir := filepath.Join(os.TempDir(), "lazydocker_cheatsheet")

	err := os.RemoveAll(tmpDir)
	if err != nil {
		log.Fatalf("Error occurred while checking if cheatsheets are up to date: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err = os.Mkdir(tmpDir, 0o700); err != nil {
		log.Fatalf("Error occurred while checking if cheatsheets are up to date: %v", err)
	}

	generateAtDir(tmpDir)

	actualContent := obtainContent(dir)
	expectedContent := obtainContent(tmpDir)

	if expectedContent == "" {
		log.Fatal("empty expected content")
	}

	if actualContent != expectedContent {
		if err := difflib.WriteUnifiedDiff(os.Stdout, difflib.UnifiedDiff{
			A:        difflib.SplitLines(expectedContent),
			B:        difflib.SplitLines(actualContent),
			FromFile: "Expected",
			FromDate: "",
			ToFile:   "Actual",
			ToDate:   "",
			Context:  1,
		}); err != nil {
			log.Fatalf("Error occurred while checking if cheatsheets are up to date: %v", err)
		}
		fmt.Printf(
			"\nCheatsheets are out of date. Please run `%s` at the project root and commit the changes. "+
				"If you run the script and no keybindings files are updated as a result, try rebasing onto master"+
				"and trying again.\n",
			generateCheatsheetCmd,
		)
		os.Exit(1)
	}

	fmt.Println("\nCheatsheets are up to date")
}

func GetKeybindingsDir() string {
	return utils.GetLazyRootDirectory() + "/docs/keybindings"
}

func obtainContent(dir string) string {
	re := regexp.MustCompile(`Keybindings_\w+\.md$`)

	content := ""
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if re.MatchString(path) {
			bytes, err := os.ReadFile(path)
			if err != nil {
				log.Fatalf("Error occurred while checking if cheatsheets are up to date: %v", err)
			}
			content += fmt.Sprintf("\n%s\n\n", filepath.Base(path))
			content += string(bytes)
		}

		return nil
	})
	if err != nil {
		log.Fatalf("Error occurred while checking if cheatsheets are up to date: %v", err)
	}

	return content
}
