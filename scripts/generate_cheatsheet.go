// This "script" generates a file called Keybindings_{{.LANG}}.md
// in current working directory.
//
// The content of this generated file is a keybindings cheatsheet.
//
// To generate cheatsheet in english run:
//   LANG=en go run scripts/generate_cheatsheet.go

package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jesseduffield/lazydocker/pkg/app"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/gui"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
)

type bindingSection struct {
	title    string
	bindings []*gui.Binding
}

func main() {
	mConfig, err := config.NewAppConfig("lazydocker", "", "", "", "", true, nil, "")
	if err != nil {
		panic(err)
	}

	for lang := range i18n.GetTranslationSets() {
		os.Setenv("LC_ALL", lang)
		mApp, _ := app.NewApp(mConfig)
		file, err := os.Create("./docs/keybindings/Keybindings_" + lang + ".md")
		if err != nil {
			panic(err)
		}

		bindingSections := getBindingSections(mApp)
		content := formatSections(mApp, bindingSections)
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
		}

		bindingSections = addBinding(titleMap[viewName], bindingSections, binding)
	}

	// for view, contexts := range mApp.Gui.GetContextMap() {
	// 	for contextName, contextBindings := range contexts {
	// 		translatedView := localisedTitle(mApp, view)
	// 		translatedContextName := localisedTitle(mApp, contextName)
	// 		title := fmt.Sprintf("%s (%s)", translatedView, translatedContextName)

	// 		for _, binding := range contextBindings {
	// 			bindingSections = addBinding(title, bindingSections, binding)
	// 		}
	// 	}
	// }

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

func getProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return strings.Split(dir, "lazydocker")[0] + "lazydocker"
}
