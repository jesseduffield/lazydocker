package i18n

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/cloudfoundry/jibber_jabber"
	"github.com/go-errors/errors"
	"github.com/imdario/mergo"
	"github.com/sirupsen/logrus"
)

// Localizer will translate a message into the user's language
type Localizer struct {
	Log *logrus.Entry
	S   TranslationSet
}

// TranslationLoader handles dynamic loading of translations from JSON files
type TranslationLoader struct {
	translationsPath string
	log              *logrus.Entry
	cache            map[string]TranslationSet
}

// NewTranslationLoader creates a new translation loader
func NewTranslationLoader(log *logrus.Entry, translationsPath string) *TranslationLoader {
	if translationsPath == "" {
		// Default path - can be configured
		translationsPath = "./translations"
	}

	return &TranslationLoader{
		translationsPath: translationsPath,
		log:              log,
		cache:            make(map[string]TranslationSet),
	}
}

// LoadTranslationFromJSON loads a translation set from a JSON file
func (tl *TranslationLoader) LoadTranslationFromJSON(languageCode string) (*TranslationSet, error) {
	// Check cache first
	if cached, exists := tl.cache[languageCode]; exists {
		tl.log.Debugf("Loading translation for '%s' from cache", languageCode)
		return &cached, nil
	}

	filePath := filepath.Join(tl.translationsPath, languageCode+".json")

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("translation file not found: %s", filePath)
	}

	// Read the JSON file
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read translation file %s: %w", filePath, err)
	}

	// Parse JSON
	var translationFile TranslationFile
	if err := json.Unmarshal(data, &translationFile); err != nil {
		return nil, fmt.Errorf("failed to parse translation file %s: %w", filePath, err)
	}

	// Convert map to TranslationSet
	translationSet := mapToTranslationSet(translationFile.Translations)

	// Cache the translation
	tl.cache[languageCode] = translationSet

	tl.log.Infof("Successfully loaded translation for '%s' from %s", languageCode, filePath)

	return &translationSet, nil
}

// GetAvailableLanguages returns list of available languages from JSON files
func (tl *TranslationLoader) GetAvailableLanguages() ([]LanguageMetadata, error) {
	languages := []LanguageMetadata{}

	// Check if translations directory exists
	if _, err := os.Stat(tl.translationsPath); os.IsNotExist(err) {
		return languages, fmt.Errorf("translations directory not found: %s", tl.translationsPath)
	}

	// Read all JSON files in the directory
	files, err := ioutil.ReadDir(tl.translationsPath)
	if err != nil {
		return languages, fmt.Errorf("failed to read translations directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(tl.translationsPath, file.Name())
		data, err := ioutil.ReadFile(filePath)
		if err != nil {
			tl.log.Warnf("Failed to read translation file %s: %v", filePath, err)
			continue
		}

		var translationFile TranslationFile
		if err := json.Unmarshal(data, &translationFile); err != nil {
			tl.log.Warnf("Failed to parse translation file %s: %v", filePath, err)
			continue
		}

		languages = append(languages, LanguageMetadata{
			Code: translationFile.Code,
			Name: translationFile.Name,
		})
	}

	return languages, nil
}

// mapToTranslationSet converts a map[string]string to TranslationSet using reflection
func mapToTranslationSet(translations map[string]string) TranslationSet {
	ts := TranslationSet{}

	v := reflect.ValueOf(&ts).Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldName := t.Field(i).Name

		if field.Kind() == reflect.String && field.CanSet() {
			if val, ok := translations[fieldName]; ok {
				field.SetString(val)
			}
		}
	}

	return ts
}

// NewTranslationSetFromConfig creates a translation set from config with dynamic loading
func NewTranslationSetFromConfig(log *logrus.Entry, configLanguage string, translationsPath string) (*TranslationSet, error) {
	loader := NewTranslationLoader(log, translationsPath)

	var language string
	if configLanguage == "auto" {
		language = detectLanguage(jibber_jabber.DetectLanguage)
	} else {
		language = configLanguage
	}

	log.Info("language: " + language)

	// Try to load from JSON first
	translationSet, err := loader.LoadTranslationFromJSON(language)
	if err != nil {
		log.Warnf("Failed to load translation from JSON for '%s': %v. Falling back to English.", language, err)
		// Fallback to English
		translationSet, err = loader.LoadTranslationFromJSON("en")
		if err != nil {
			return nil, errors.New("Failed to load default English translation: " + err.Error())
		}
	}

	// Always merge with English as base to ensure all fields are populated
	baseSet, _ := loader.LoadTranslationFromJSON("en")
	if baseSet != nil {
		_ = mergo.Merge(translationSet, baseSet)
	}

	return translationSet, nil
}

// NewTranslationSet creates a translation set (keeping for backward compatibility)
func NewTranslationSet(log *logrus.Entry, language string) *TranslationSet {
	translationSet, err := NewTranslationSetFromConfig(log, language, "./translations")
	if err != nil {
		log.Errorf("Failed to load translation: %v", err)
		// Return English as ultimate fallback
		baseSet := englishSet()
		return &baseSet
	}
	return translationSet
}

// GetTranslationSets gets all the translation sets dynamically from JSON files
func GetTranslationSets(log *logrus.Entry, translationsPath string) map[string]TranslationSet {
	loader := NewTranslationLoader(log, translationsPath)
	languages, err := loader.GetAvailableLanguages()

	if err != nil {
		log.Warnf("Failed to get available languages: %v", err)
		return map[string]TranslationSet{
			"en": englishSet(),
		}
	}

	translationSets := make(map[string]TranslationSet)

	for _, lang := range languages {
		translationSet, err := loader.LoadTranslationFromJSON(lang.Code)
		if err != nil {
			log.Warnf("Failed to load translation for %s: %v", lang.Code, err)
			continue
		}
		translationSets[lang.Code] = *translationSet
	}

	return translationSets
}

// detectLanguage extracts user language from environment
func detectLanguage(langDetector func() (string, error)) string {
	if userLang, err := langDetector(); err == nil {
		return userLang
	}
	return "C"
}

// Fallback to hardcoded English if JSON loading fails
func englishSet() TranslationSet {
	return TranslationSet{
		PruningStatus:              "pruning",
		RemovingStatus:             "removing",
		RestartingStatus:           "restarting",
		StartingStatus:             "starting",
		StoppingStatus:             "stopping",
		UppingServiceStatus:        "upping service",
		UppingProjectStatus:        "upping project",
		DowningStatus:              "downing",
		PausingStatus:              "pausing",
		RunningCustomCommandStatus: "running custom command",
		RunningBulkCommandStatus:   "running bulk command",
		ErrorOccurred:              "An error occurred! Please create an issue at https://github.com/jesseduffield/lazydocker/issues",
		ConnectionFailed:           "connection to docker client failed. You may need to restart the docker client",
		Donate:                     "Donate",
		Confirm:                    "Confirm",
		Return:                     "return",
		FocusMain:                  "focus main panel",
		Navigate:                   "navigate",
		Execute:                    "execute",
		Close:                      "close",
		Quit:                       "quit",
		Menu:                       "menu",
		MenuTitle:                  "Menu",
		Cancel:                     "cancel",
		Remove:                     "remove",
		Stop:                       "stop",
		Restart:                    "restart",
		GlobalTitle:                "Global",
		MainTitle:                  "Main",
		ProjectTitle:               "Project",
		ServicesTitle:              "Services",
		ContainersTitle:            "Containers",
		ImagesTitle:                "Images",
		VolumesTitle:               "Volumes",
		NetworksTitle:              "Networks",
		ErrorTitle:                 "Error",
		LogsTitle:                  "Logs",
		No:                         "no",
		Yes:                        "yes",
	}
}
