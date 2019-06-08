package i18n

// TranslationSet is a set of localised strings for a given language
type TranslationSet struct {
	AddFavourite                               string
	ErrorMessage                               string
	NotEnoughSpace                             string
	StatusTitle                                string
	Navigate                                   string
	Menu                                       string
	Execute                                    string
	Scroll                                     string
	Close                                      string
	ErrorTitle                                 string
	RunningSubprocess                          string
	NoViewMachingNewLineFocusedSwitchStatement string
	OpenConfig                                 string
	EditConfig                                 string
	AnonymousReportingTitle                    string
	AnonymousReportingPrompt                   string
	ConfirmQuit                                string
	ErrorOccurred                              string
	Donate                                     string
	Cancel                                     string
	CustomCommandTitle                         string
	Remove                                     string
	RemoveWithVolumes                          string
	MustForceToRemoveContainer                 string
	Confirm                                    string
	StopContainer                              string
	RestartingStatus                           string
	StoppingStatus                             string
	RemovingStatus                             string
	RemoveService                              string
	Stop                                       string
	Restart                                    string
	Rebuild                                    string
	PreviousContext                            string
	NextContext                                string
	Attach                                     string
	ViewLogs                                   string
	ServicesTitle                              string
	ContainersTitle                            string
	ImagesTitle                                string
	NoContainers                               string
	NoImages                                   string
	RemoveImage                                string
	RemoveWithoutPrune                         string
	PruneImages                                string
	PruneContainers                            string
	ConfirmPruneContainers                     string
	ConfirmPruneImages                         string
	PruningStatus                              string
	StopService                                string
	PressEnterToReturn                         string
	ViewRestartOptions                         string
}

func englishSet() TranslationSet {
	return TranslationSet{
		PruningStatus:    "pruning",
		RemovingStatus:   "removing",
		RestartingStatus: "restarting",
		StoppingStatus:   "stopping",

		RunningSubprocess:                          "running subprocess",
		NoViewMachingNewLineFocusedSwitchStatement: "No view matching newLineFocused switch statement",

		ErrorOccurred: "An error occurred! Please create an issue at https://github.com/jesseduffield/lazydocker/issues",
		Donate:        "Donate",
		Confirm:       "Confirm",

		Navigate:           "navigate",
		Execute:            "execute",
		Close:              "close",
		Menu:               "menu",
		Scroll:             "scroll",
		OpenConfig:         "open config container",
		EditConfig:         "edit config container",
		Cancel:             "cancel",
		Remove:             "remove",
		RemoveWithVolumes:  "remove with volumes",
		RemoveService:      "remove containers",
		Stop:               "stop",
		Restart:            "restart",
		Rebuild:            "rebuild",
		PreviousContext:    "previous context",
		NextContext:        "next context",
		Attach:             "attach",
		ViewLogs:           "view logs",
		RemoveImage:        "remove image",
		RemoveWithoutPrune: "remove without deleting untagged parents",
		PruneContainers:    "prune exited containers",
		PruneImages:        "prune unused images",
		ViewRestartOptions: "view restart options",

		AnonymousReportingTitle:  "Help make lazydocker better",
		AnonymousReportingPrompt: "Would you like to enable anonymous reporting data to help improve lazydocker? (enter/esc)",

		StatusTitle:        "Status",
		ServicesTitle:      "Services",
		ContainersTitle:    "Containers",
		ImagesTitle:        "Images",
		CustomCommandTitle: "Custom Command:",
		ErrorTitle:         "Error",

		NoContainers: "No containers",
		NoImages:     "No images",

		ConfirmQuit:                "Are you sure you want to quit?",
		MustForceToRemoveContainer: "You cannot remove a running container unless you force it. Do you want to force it?",
		NotEnoughSpace:             "Not enough space to render panels",
		ConfirmPruneImages:         "Are you sure you want to prune all unused images?",
		ConfirmPruneContainers:     "Are you sure you want to prune all stopped containers?",
		StopService:                "Are you sure you want to stop this service's containers? (enter/esc)",
		StopContainer:              "Are you sure you want to stop this container?",
		PressEnterToReturn:         "Press enter to return to lazydocker",
	}
}
