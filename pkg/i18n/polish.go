package i18n

func polishSet() TranslationSet {
	return TranslationSet{
		PruningStatus:              "czyszczenie",
		RemovingStatus:             "usuwanie",
		RestartingStatus:           "restartowanie",
		StoppingStatus:             "zatrzymywanie",
		RunningCustomCommandStatus: "uruchamianie własnej komendty",

		NoViewMachingNewLineFocusedSwitchStatement: "Żaden widok nie odpowiada instrukcji przełączenia newLineFocused",

		ErrorOccurred:                     "Wystąpił błąd! Proszę go zgłosić na https://github.com/jesseduffield/lazydocker/issues",
		ConnectionFailed:                  "Błąd połączenia z Dockerem. Być może należy go zrestartować.",
		UnattachableContainerError:        "Kontener nie obsługuje przyczepiania (attach). Musisz albo użyć flag '-it', albo `stdin_open: true, tty: true` w pliku docker-compose.yml.",
		CannotAttachStoppedContainerError: "Nie można przyczepić się do zatrzymanego kontenera, należy go najpierw uruchomić (co można wykonać wciskając przycisk 'r')",
		CannotAccessDockerSocketError:     "Nie udało się uzyskać dostępu do unix:///var/run/docker.sock\nUruchom program jako root lub przeczytaj https://docs.docker.com/install/linux/linux-postinstall/",

		Donate:  "Dotacja",
		Confirm: "Potwierdź",

		Return:             "powrót",
		FocusMain:          "skup na głównym panelu",
		Navigate:           "nawigowanie",
		Execute:            "wykonaj",
		Close:              "zamknij",
		Menu:               "menu",
		MenuTitle:          "Menu",
		Scroll:             "przewiń",
		OpenConfig:         "otwórz konfigurację",
		EditConfig:         "edytuj konfigurację",
		Cancel:             "anuluj",
		Remove:             "usuń",
		ForceRemove:        "usuń siłą",
		RemoveWithVolumes:  "usuń z wolumenami",
		RemoveService:      "usuń kontenery",
		Stop:               "zatrzymaj",
		Restart:            "restartuj",
		Rebuild:            "przebuduj",
		Recreate:           "odtwórz",
		PreviousContext:    "poprzednia zakładka",
		NextContext:        "następna zakładka",
		Attach:             "przyczep",
		ViewLogs:           "pokaż logi",
		RemoveImage:        "usuń obraz",
		RemoveVolume:       "usuń wolumen",
		RemoveWithoutPrune: "usuń bez kasowania nieoznaczonych rodziców",
		PruneContainers:    "wyczyść kontenery",
		PruneVolumes:       "wyczyść nieużywane wolumeny",
		PruneImages:        "wyczyść nieużywane obrazy",
		ViewRestartOptions: "pokaż opcje restartu",
		RunCustomCommand:   "wykonaj predefiniowaną własną komende",

		GlobalTitle:               "Globalne",
		MainTitle:                 "Główne",
		ProjectTitle:              "Projekt",
		ServicesTitle:             "Serwisy",
		ContainersTitle:           "Kontenery",
		StandaloneContainersTitle: "Kontenery samodzielne",
		ImagesTitle:               "Obrazy",
		VolumesTitle:              "Wolumeny",
		CustomCommandTitle:        "Własna komenda:",
		ErrorTitle:                "Błąd",
		LogsTitle:                 "Logi",
		ConfigTitle:               "Konfiguracja",
		EnvTitle:                  "Env",
		DockerComposeConfigTitle:  "Konfiguracja docker-compose",
		TopTitle:                  "Top",
		StatsTitle:                "Staty",
		CreditsTitle:              "O",
		ContainerConfigTitle:      "Konfiguracja kontenera",
		ContainerEnvTitle:         "Container Env",
		NothingToDisplay:          "Nothing to display",
		CannotDisplayEnvVariables: "Something went wrong while displaying environment variables",

		NoContainers: "Brak kontenerów",
		NoContainer:  "Brak kontenera",
		NoImages:     "Brak obrazów",
		NoVolumes:    "Brak wolumenów",

		ConfirmQuit:                "Na pewno chcesz wyjść?",
		MustForceToRemoveContainer: "Nie możesz usunąć uruchomionego kontenera dopóki nie zrobisz tego siłą. Chcesz wykonać to z siłą?",
		NotEnoughSpace:             "Niedostateczna ilość miejsca do wyświetlenia paneli",
		ConfirmPruneImages:         "Na pewno wyczyścić wszystkie nieużywane obrazy?",
		ConfirmPruneContainers:     "Na pewno wyczyścić wszystkie nieuruchomione kontenery?",
		ConfirmPruneVolumes:        "Na pewno wyczyścić wszystkie nieużywane wolumeny?",
		StopService:                "Na pewno zatrzymać kontenery tego serwisu?",
		StopContainer:              "Na pewno zatrzymać ten kontener?",
		PressEnterToReturn:         "Wciśnij enter aby powrócić do lazydockera (ten komunikat może być wyłączony w konfiguracji poprzez ustawienie `gui.returnImmediately: true`)",
	}
}
