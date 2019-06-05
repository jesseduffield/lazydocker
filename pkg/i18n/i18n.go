package i18n

import (
	"strings"

	"github.com/imdario/mergo"

	"github.com/cloudfoundry/jibber_jabber"
	"github.com/sirupsen/logrus"
)

// Localizer will translate a message into the user's language
type Localizer struct {
	language string
	Log      *logrus.Entry
	S        TranslationSet
}

// NewTranslationSet creates a new Localizer
func NewTranslationSet(log *logrus.Entry) TranslationSet {
	userLang := detectLanguage(jibber_jabber.DetectLanguage)

	log.Info("language: " + userLang)

	set := englishSet()

	userLang = "pl"

	if strings.HasPrefix(userLang, "pl") {
		mergo.Merge(&set, polishSet(), mergo.WithOverride)
	}

	if strings.HasPrefix(userLang, "nl") {
		mergo.Merge(&set, polishSet(), mergo.WithOverride)
	}

	return set
}

// detectLanguage extracts user language from environment
func detectLanguage(langDetector func() (string, error)) string {
	if userLang, err := langDetector(); err == nil {
		return userLang
	}

	return "C"
}
