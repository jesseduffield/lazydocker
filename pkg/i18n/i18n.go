package i18n

import (
	"github.com/cloudfoundry/jibber_jabber"
	"github.com/go-errors/errors"
	"github.com/imdario/mergo"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

// Localizer will translate a message into the user's language
type Localizer struct {
	Log *logrus.Entry
	S   TranslationSet
}

func NewTranslationSetFromConfig(log *logrus.Entry, configLanguage string) (*TranslationSet, error) {
	if configLanguage == "auto" {
		language := detectLanguage(jibber_jabber.DetectLanguage)

		return NewTranslationSet(log, language), nil
	}

	if lo.Contains(getSupportedLanguages(), configLanguage) {
		return NewTranslationSet(log, configLanguage), nil
	}

	return NewTranslationSet(log, "en"), errors.New("Language not found: " + configLanguage)
}

func NewTranslationSet(log *logrus.Entry, language string) *TranslationSet {
	log.Info("language: " + language)

	baseSet := englishSet()
	otherSet := getTranslationSet(language)

	_ = mergo.Merge(&baseSet, otherSet, mergo.WithOverride)

	return &baseSet
}

// GetTranslationSets gets all the translation sets, keyed by language code
func GetTranslationSets() map[string]TranslationSet {
	return map[string]TranslationSet{
		"pl": polishSet(),
		"nl": dutchSet(),
		"de": germanSet(),
		"tr": turkishSet(),
		"en": englishSet(),
		"fr": frenchSet(),
	}
}

// getTranslationSet returns the translation set that matches the given language.
//
// It returns an english translation set if not found.
func getTranslationSet(languageCode string) TranslationSet {
	switch languageCode {
	case "pl":
		return polishSet()
	case "nl":
		return dutchSet()
	case "de":
		return germanSet()
	case "tr":
		return turkishSet()
	case "en":
		return englishSet()
	case "fr":
		return frenchSet()
	}

	return englishSet()
}

// getSupportedLanguages returns all the supported languages.
func getSupportedLanguages() []string {
	return []string{
		"pl",
		"nl",
		"de",
		"tr",
		"en",
		"fr",
	}
}

// detectLanguage extracts user language from environment
func detectLanguage(langDetector func() (string, error)) string {
	if userLang, err := langDetector(); err == nil {
		return userLang
	}

	return "C"
}
