package i18n

// TranslationSet is a set of localised strings for a given language
type TranslationSet struct {
	NotEnoughSpace                             string
	ProjectTitle                               string
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
	UnattachableContainerError                 string
	CannotAttachStoppedContainerError          string
	CannotAccessDockerSocketError              string
	CannotKillChildError                       string

	Donate                     string
	Cancel                     string
	CustomCommandTitle         string
	BulkCommandTitle           string
	Remove                     string
	HideStopped                string
	ForceRemove                string
	RemoveWithVolumes          string
	MustForceToRemoveContainer string
	Confirm                    string
	Return                     string
	FocusMain                  string
	StopContainer              string
	RestartingStatus           string
	StoppingStatus             string
	RemovingStatus             string
	RunningCustomCommandStatus string
	RunningBulkCommandStatus   string
	RemoveService              string
	Stop                       string
	Restart                    string
	Rebuild                    string
	Recreate                   string
	PreviousContext            string
	NextContext                string
	Attach                     string
	ViewLogs                   string
	ServicesTitle              string
	ContainersTitle            string
	StandaloneContainersTitle  string
	TopTitle                   string
	ImagesTitle                string
	VolumesTitle               string
	NoContainers               string
	NoContainer                string
	NoImages                   string
	NoVolumes                  string
	RemoveImage                string
	RemoveVolume               string
	RemoveWithoutPrune         string
	PruneImages                string
	PruneContainers            string
	PruneVolumes               string
	ConfirmPruneContainers     string
	ConfirmStopContainers      string
	ConfirmRemoveContainers    string
	ConfirmPruneImages         string
	ConfirmPruneVolumes        string
	PruningStatus              string
	StopService                string
	PressEnterToReturn         string
	StopAllContainers          string
	RemoveAllContainers        string
	ViewRestartOptions         string
	RunCustomCommand           string
	ViewBulkCommands           string
	OpenInBrowser              string

	LogsTitle                string
	ConfigTitle              string
	DockerComposeConfigTitle string
	StatsTitle               string
	CreditsTitle             string
	ContainerConfigTitle     string

	No  string
	Yes string
}

func englishSet() TranslationSet {
	return TranslationSet{
		PruningStatus:              "pruning",
		RemovingStatus:             "removing",
		RestartingStatus:           "restarting",
		StoppingStatus:             "stopping",
		RunningCustomCommandStatus: "running custom command",
		RunningBulkCommandStatus:   "running bulk command",

		RunningSubprocess:                          "running subprocess",
		NoViewMachingNewLineFocusedSwitchStatement: "No view matching newLineFocused switch statement",

		ErrorOccurred:                     "An error occurred! Please create an issue at https://github.com/jesseduffield/lazydocker/issues",
		ConnectionFailed:                  "connection to docker client failed. You may need to restart the docker client",
		UnattachableContainerError:        "Container does not support attaching. You must either run the service with the '-it' flag or use `stdin_open: true, tty: true` in the docker-compose.yml file",
		CannotAttachStoppedContainerError: "You cannot attach to a stopped container, you need to start it first (which you can actually do with the 'r' key) (yes I'm too lazy to do this automatically for you) (pretty cool that I get to communicate one-on-one with you in the form of an error message though)",
		CannotAccessDockerSocketError:     "Can't access docker socket at: unix:///var/run/docker.sock\nRun lazydocker as root or read https://docs.docker.com/install/linux/linux-postinstall/",
		CannotKillChildError:              "Waited three seconds for child process to stop. There may be an orphan process that continues to run on your system.",

		Donate:  "Donate",
		Confirm: "Confirm",

		Return:              "return",
		FocusMain:           "focus main panel",
		Navigate:            "navigate",
		Execute:             "execute",
		Close:               "close",
		Menu:                "menu",
		Scroll:              "scroll",
		OpenConfig:          "open lazydocker config",
		EditConfig:          "edit lazydocker config",
		Cancel:              "cancel",
		Remove:              "remove",
		HideStopped:         "Hide/Show stopped containers",
		ForceRemove:         "force remove",
		RemoveWithVolumes:   "remove with volumes",
		RemoveService:       "remove containers",
		Stop:                "stop",
		Restart:             "restart",
		Rebuild:             "rebuild",
		Recreate:            "recreate",
		PreviousContext:     "previous tab",
		NextContext:         "next tab",
		Attach:              "attach",
		ViewLogs:            "view logs",
		RemoveImage:         "remove image",
		RemoveVolume:        "remove volume",
		RemoveWithoutPrune:  "remove without deleting untagged parents",
		PruneContainers:     "prune exited containers",
		PruneVolumes:        "prune unused volumes",
		PruneImages:         "prune unused images",
		StopAllContainers:   "stop all containers",
		RemoveAllContainers: "remove all containers (forced)",
		ViewRestartOptions:  "view restart options",
		RunCustomCommand:    "run predefined custom command",
		ViewBulkCommands:    "view bulk commands",
		OpenInBrowser:       "open in browser (first port is http)",

		AnonymousReportingTitle:  "Help make lazydocker better",
		AnonymousReportingPrompt: "Would you like to enable anonymous reporting data to help improve lazydocker?",

		GlobalTitle:               "Global",
		MainTitle:                 "Main",
		ProjectTitle:              "Project",
		ServicesTitle:             "Services",
		ContainersTitle:           "Containers",
		StandaloneContainersTitle: "Standalone Containers",
		ImagesTitle:               "Images",
		VolumesTitle:              "Volumes",
		CustomCommandTitle:        "Custom Command:",
		BulkCommandTitle:          "Bulk Command:",
		ErrorTitle:                "Error",
		LogsTitle:                 "Logs",
		ConfigTitle:               "Config",
		DockerComposeConfigTitle:  "Docker-Compose Config",
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
		ConfirmStopContainers:      "Are you sure you want to stop all containers?",
		ConfirmRemoveContainers:    "Are you sure you want to remove all containers?",
		ConfirmPruneVolumes:        "Are you sure you want to prune all unused volumes?",
		StopService:                "Are you sure you want to stop this service's containers?",
		StopContainer:              "Are you sure you want to stop this container?",
		PressEnterToReturn:         "Press enter to return to lazydocker (this prompt can be disabled in your config by setting `gui.returnImmediately: true`)",

		No:  "no",
		Yes: "yes",
	}
}
