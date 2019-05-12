package i18n

import (
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

func addPolish(i18nObject *i18n.Bundle) error {

	return i18nObject.AddMessages(language.Polish,
		&i18n.Message{
			ID:    "NotEnoughSpace",
			Other: "Za mało miejsca do wyświetlenia paneli",
		})
}
