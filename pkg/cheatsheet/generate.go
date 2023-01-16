// This "script" generates a file called Keybindings_{{.LANG}}.md
// in current working directory.
//
// The content of this generated file is a keybindings cheatsheet.
//
// To generate cheatsheet in english run:
//   LANG=en go run scripts/cheatsheet/main.go generate

package cheatsheet

import (
	"fmt"
	"log"
	"os"

	"github.com/jesseduffield/lazydocker/pkg/app"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/gui"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
)

const (
	generateCheatsheetCmd = "go run scripts/cheatsheet/main.go generate"
)

type bindingSection struct {
	title    string
	bindings []*gui.Binding
}

func Generate() {
	generateAtDir(GetKeybindingsDir())
}

func generateAtDir(dir string) {
	mConfig, err := config.NewAppConfig("lazydocker", "", "", "", "", true, nil, "")
	if err != nil {
		panic(err)
	}

	for lang := range i18n.GetTranslationSets() {
		os.Setenv("LC_ALL", lang)
		mApp, _ := app.NewApp(mConfig)
		mApp.Gui.SetupFakeGui()

		file, err := os.Create(dir + "/Keybindings_" + lang + ".md")
		if err != nil {
			panic(err)
		}

		bindingSections := getBindingSections(mApp)
		content := formatSections(mApp, bindingSections)
		content = fmt.Sprintf(
			"_This file is auto-generated. To update, make the changes in the "+
				"pkg/i18n directory and then run `%s` from the project root._\n\n%s",
			generateCheatsheetCmd,
			content,
		)
		writeString(file, content)
	}
}

func writeString(file *os.File, str string) {
	_, err := file.WriteString(str)
	if err != nil {
		log.Fatal(err)
	}
}

func formatTitle(title string) string {
	return fmt.Sprintf("\n## %s\n\n", title)
}

func formatBinding(binding *gui.Binding) string {
	return fmt.Sprintf("  <kbd>%s</kbd>: %s\n", binding.GetKey(), binding.Description)
}

func getBindingSections(mApp *app.App) []*bindingSection {
	bindingSections := []*bindingSection{}

	for _, binding := range mApp.Gui.GetInitialKeybindings() {
		if binding.Description == "" {
			continue
		}

		viewName := binding.ViewName
		if viewName == "" {
			viewName = "global"
		}

		titleMap := map[string]string{
			"global":     mApp.Tr.GlobalTitle,
			"main":       mApp.Tr.MainTitle,
			"project":    mApp.Tr.ProjectTitle,
			"services":   mApp.Tr.ServicesTitle,
			"containers": mApp.Tr.ContainersTitle,
			"images":     mApp.Tr.ImagesTitle,
			"volumes":    mApp.Tr.VolumesTitle,
			"networks":   mApp.Tr.NetworksTitle,
		}

		bindingSections = addBinding(titleMap[viewName], bindingSections, binding)
	}

	return bindingSections
}

func addBinding(title string, bindingSections []*bindingSection, binding *gui.Binding) []*bindingSection {
	if binding.Description == "" {
		return bindingSections
	}

	for _, section := range bindingSections {
		if title == section.title {
			section.bindings = append(section.bindings, binding)
			return bindingSections
		}
	}

	section := &bindingSection{
		title:    title,
		bindings: []*gui.Binding{binding},
	}

	return append(bindingSections, section)
}

func formatSections(mApp *app.App, bindingSections []*bindingSection) string {
	content := fmt.Sprintf("# Lazydocker %s\n", mApp.Tr.Menu)

	for _, section := range bindingSections {
		content += formatTitle(section.title)
		content += "<pre>\n"
		for _, binding := range section.bindings {
			content += formatBinding(binding)
		}
		content += "</pre>\n"
	}

	return content
}
