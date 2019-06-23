package i18n

// TranslationSet is a set of localised strings for a given language
type TranslationSet struct {
	AddFavourite                               string
	ErrorMessage                               string
	NotEnoughSpace                             string
	StatusTitle                                string
	MainTitle                                  string
	GlobalTitle                                string
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
	ConnectionFailed                           string
	Donate                                     string
	Cancel                                     string
	CustomCommandTitle                         string
	Remove                                     string
	ForceRemove                                string
	RemoveWithVolumes                          string
	MustForceToRemoveContainer                 string
	Confirm                                    string
	StopContainer                              string
	RestartingStatus                           string
	StoppingStatus                             string
	RemovingStatus                             string
	RunningCustomCommandStatus                 string
	RemoveService                              string
	Stop                                       string
	Restart                                    string
	Rebuild                                    string
	Recreate                                   string
	PreviousContext                            string
	NextContext                                string
	Attach                                     string
	ViewLogs                                   string
	ServicesTitle                              string
	ContainersTitle                            string
	StandaloneContainersTitle                  string
	TopTitle                                   string
	ImagesTitle                                string
	VolumesTitle                               string
	NoContainers                               string
	NoContainer                                string
	NoImages                                   string
	NoVolumes                                  string
	RemoveImage                                string
	RemoveVolume                               string
	RemoveWithoutPrune                         string
	PruneImages                                string
	PruneContainers                            string
	PruneVolumes                               string
	ConfirmPruneContainers                     string
	ConfirmPruneImages                         string
	ConfirmPruneVolumes                        string
	PruningStatus                              string
	StopService                                string
	PressEnterToReturn                         string
	ViewRestartOptions                         string
	RunCustomCommand                           string

	LogsTitle            string
	ConfigTitle          string
	StatsTitle           string
	CreditsTitle         string
	ContainerConfigTitle string
}

func englishSet() TranslationSet {
	return TranslationSet{
		PruningStatus:              "pruning",
		RemovingStatus:             "removing",
		RestartingStatus:           "restarting",
		StoppingStatus:             "stopping",
		RunningCustomCommandStatus: "running custom command",

		RunningSubprocess:                          "running subprocess",
		NoViewMachingNewLineFocusedSwitchStatement: "No view matching newLineFocused switch statement",

		ErrorOccurred:    "An error occurred! Please create an issue at https://github.com/jesseduffield/lazydocker/issues",
		ConnectionFailed: "connection to docker client failed. You may need to restart the docker client",
		Donate:           "Donate",
		Confirm:          "Confirm",

		Navigate:           "navigate",
		Execute:            "execute",
		Close:              "close",
		Menu:               "menu",
		Scroll:             "scroll",
		OpenConfig:         "open config container",
		EditConfig:         "edit config container",
		Cancel:             "cancel",
		Remove:             "remove",
		ForceRemove:        "force remove",
		RemoveWithVolumes:  "remove with volumes",
		RemoveService:      "remove containers",
		Stop:               "stop",
		Restart:            "restart",
		Rebuild:            "rebuild",
		Recreate:           "recreate",
		PreviousContext:    "previous tab",
		NextContext:        "next tab",
		Attach:             "attach",
		ViewLogs:           "view logs",
		RemoveImage:        "remove image",
		RemoveVolume:       "remove volume",
		RemoveWithoutPrune: "remove without deleting untagged parents",
		PruneContainers:    "prune exited containers",
		PruneVolumes:       "prune unused volumes",
		PruneImages:        "prune unused images",
		ViewRestartOptions: "view restart options",
		RunCustomCommand:   "run predefined custom command",

		AnonymousReportingTitle:  "Help make lazydocker better",
		AnonymousReportingPrompt: "Would you like to enable anonymous reporting data to help improve lazydocker? (enter/esc)",

		GlobalTitle:               "Global",
		MainTitle:                 "Main",
		StatusTitle:               "Status",
		ServicesTitle:             "Services",
		ContainersTitle:           "Containers",
		StandaloneContainersTitle: "Standalone Containers",
		ImagesTitle:               "Images",
		VolumesTitle:              "Volumes",
		CustomCommandTitle:        "Custom Command:",
		ErrorTitle:                "Error",
		LogsTitle:                 "Logs",
		ConfigTitle:               "Config",
		TopTitle:                  "Top",
		StatsTitle:                "Stats",
		CreditsTitle:              "About",
		ContainerConfigTitle:      "Container Config",

		NoContainers: "No containers",
		NoContainer:  "No container",
		NoImages:     "No images",
		NoVolumes:    "No volumes",

		ConfirmQuit:                "Are you sure you want to quit?",
		MustForceToRemoveContainer: "You cannot remove a running container unless you force it. Do you want to force it?",
		NotEnoughSpace:             "Not enough space to render panels",
		ConfirmPruneImages:         "Are you sure you want to prune all unused images?",
		ConfirmPruneContainers:     "Are you sure you want to prune all stopped containers?",
		ConfirmPruneVolumes:        "Are you sure you want to prune all unused volumes?",
		StopService:                "Are you sure you want to stop this service's containers? (enter/esc)",
		StopContainer:              "Are you sure you want to stop this container?",
		PressEnterToReturn:         "Press enter to return to lazydocker",
	}
}
