package main

import (
	"fmt"
	"reflect"

	"github.com/jesseduffield/lazydocker/pkg/i18n"
)

func main() {
	fmt.Println(getOutstandingTranslations())
}

// adapted from https://github.com/a8m/reflect-examples#read-struct-tags
func getOutstandingTranslations() string {
	output := ""
	for languageCode, translationSet := range i18n.GetTranslationSets() {
		output += languageCode + ":\n"
		v := reflect.ValueOf(translationSet)

		for i := 0; i < v.NumField(); i++ {
			value := v.Field(i).String()
			if value == "" {
				output += v.Type().Field(i).Name + "\n"
			}
		}
		output += "\n"
	}
	return output
}
