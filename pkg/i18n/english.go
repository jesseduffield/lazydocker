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
	Error                                      string
	ResizingPopupPanel                         string
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
	CustomCommand                              string
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
	ConfirmPruneImages                         string
	PruningStatus                              string
	StopService                                string
	PressEnterToReturn                         string
}

func englishSet() TranslationSet {
	return TranslationSet{
		NotEnoughSpace:     "Not enough space to render panels",
		StatusTitle:        "Status",
		Navigate:           "navigate",
		Menu:               "menu",
		Execute:            "execute",
		Scroll:             "scroll",
		Close:              "close",
		Error:              "Error",
		ResizingPopupPanel: "resizing popup panel",
		RunningSubprocess:  "running subprocess",
		NoViewMachingNewLineFocusedSwitchStatement: "No view matching newLineFocused switch statement",
		OpenConfig:                 "open config container",
		EditConfig:                 "edit config container",
		AnonymousReportingTitle:    "Help make lazydocker better",
		AnonymousReportingPrompt:   "Would you like to enable anonymous reporting data to help improve lazydocker? (enter/esc)",
		ConfirmQuit:                "Are you sure you want to quit?",
		ErrorOccurred:              "An error occurred! Please create an issue at https://github.com/jesseduffield/lazydocker/issues",
		Donate:                     "Donate",
		Cancel:                     "cancel",
		CustomCommand:              "Custom Command:",
		Remove:                     "remove",
		RemoveWithVolumes:          "remove with volumes",
		MustForceToRemoveContainer: "You cannot remove a running container unless you force it. Do you want to force it?",
		Confirm:                    "Confirm",
		StopContainer:              "Are you sure you want to stop this container?",
		RestartingStatus:           "restarting",
		StoppingStatus:             "stopping",
		RemovingStatus:             "removing",
		RemoveService:              "remove containers",
		Stop:                       "stop",
		Restart:                    "restart",
		PreviousContext:            "previous context",
		NextContext:                "next context",
		Attach:                     "attach",
		ViewLogs:                   "view logs",
		ServicesTitle:              "Services",
		ContainersTitle:            "Containers",
		ImagesTitle:                "Images",
		NoContainers:               "No containers",
		NoImages:                   "No images",
		RemoveImage:                "remove image",
		RemoveWithoutPrune:         "remove without deleting untagged parents",
		PruneImages:                "prune unused images",
		ConfirmPruneImages:         "Are you sure you want to prune all unused images?",
		PruningStatus:              "pruning",
		StopService:                "Are you sure you want to stop this service's containers? (enter/esc)",
		PressEnterToReturn:         "Press enter to return to lazydocker",
	}
}
