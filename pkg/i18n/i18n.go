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

// ISO 639-1 supported language codes.
const (
	// Polish
	PL = "pl"
	// Dutch
	NL = "nl"
	// German
	DE = "de"
	// Turkish
	TR = "tr"
	// English
	EN = "en"
	// French
	FR = "fr"
)

func NewTranslationSetFromConfig(log *logrus.Entry, configLanguage string) (*TranslationSet, error) {
	if configLanguage == "auto" {
		language := detectLanguage(jibber_jabber.DetectLanguage)

		return NewTranslationSet(log, language), nil
	}

	if lo.Contains(getSupportedLanguages(), configLanguage) {
		return NewTranslationSet(log, configLanguage), nil
	}

	return NewTranslationSet(log, EN), errors.New("Language not found: " + configLanguage)
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
		PL: polishSet(),
		NL: dutchSet(),
		DE: germanSet(),
		TR: turkishSet(),
		EN: englishSet(),
		FR: frenchSet(),
	}
}

// getTranslationSet returns the translation set that matches the given language.
//
// It returns an english translation set if not found.
func getTranslationSet(languageCode string) TranslationSet {
	switch languageCode {
	case PL:
		return polishSet()
	case NL:
		return dutchSet()
	case DE:
		return germanSet()
	case TR:
		return turkishSet()
	case EN:
		return englishSet()
	case FR:
		return frenchSet()
	}

	return englishSet()
}

// getSupportedLanguages returns all the supported languages.
func getSupportedLanguages() []string {
	return []string{
		PL,
		NL,
		DE,
		TR,
		EN,
		FR,
	}
}

// detectLanguage extracts user language from environment
func detectLanguage(langDetector func() (string, error)) string {
	if userLang, err := langDetector(); err == nil {
		return userLang
	}

	return "C"
}
