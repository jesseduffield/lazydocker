package i18n

// TranslationSet is a set of localised strings for a given language
type TranslationSet struct {
	NotEnoughSpace                             string
	ProjectTitle                               string
	MainTitle                                  string
	GlobalTitle                                string
	Navigate                                   string
	Menu                                       string
	MenuTitle                                  string
	Execute                                    string
	Scroll                                     string
	Close                                      string
	Quit                                       string
	ErrorTitle                                 string
	NoViewMachingNewLineFocusedSwitchStatement string
	OpenConfig                                 string
	EditConfig                                 string
	ConfirmQuit                                string
	ConfirmUpProject                           string
	ErrorOccurred                              string
	ConnectionFailed                           string
	UnattachableContainerError                 string
	WaitingForContainerInfo                    string
	CannotAttachStoppedContainerError          string
	CannotAccessDockerSocketError              string
	CannotKillChildError                       string

	Donate                      string
	Cancel                      string
	CustomCommandTitle          string
	BulkCommandTitle            string
	Remove                      string
	HideStopped                 string
	ForceRemove                 string
	RemoveWithVolumes           string
	MustForceToRemoveContainer  string
	Confirm                     string
	Return                      string
	FocusMain                   string
	LcFilter                    string
	StopContainer               string
	RestartingStatus            string
	StartingStatus              string
	StoppingStatus              string
	UppingProjectStatus         string
	UppingServiceStatus         string
	PausingStatus               string
	RemovingStatus              string
	DowningStatus               string
	RunningCustomCommandStatus  string
	RunningBulkCommandStatus    string
	RemoveService               string
	UpService                   string
	Stop                        string
	Pause                       string
	Restart                     string
	Down                        string
	DownWithVolumes             string
	Start                       string
	Rebuild                     string
	Recreate                    string
	PreviousContext             string
	NextContext                 string
	Attach                      string
	ViewLogs                    string
	UpProject                   string
	DownProject                 string
	ServicesTitle               string
	ContainersTitle             string
	StandaloneContainersTitle   string
	TopTitle                    string
	ImagesTitle                 string
	VolumesTitle                string
	NetworksTitle               string
	NoContainers                string
	NoContainer                 string
	NoImages                    string
	NoVolumes                   string
	NoNetworks                  string
	NoServices                  string
	RemoveImage                 string
	RemoveVolume                string
	RemoveNetwork               string
	RemoveWithoutPrune          string
	RemoveWithoutPruneWithForce string
	RemoveWithForce             string
	PruneImages                 string
	PruneContainers             string
	PruneVolumes                string
	PruneNetworks               string
	ConfirmPruneContainers      string
	ConfirmStopContainers       string
	ConfirmRemoveContainers     string
	ConfirmPruneImages          string
	ConfirmPruneVolumes         string
	ConfirmPruneNetworks        string
	PruningStatus               string
	StopService                 string
	PressEnterToReturn          string
	DetachFromContainerShortCut string
	StopAllContainers           string
	RemoveAllContainers         string
	ViewRestartOptions          string
	ExecShell                   string
	RunCustomCommand            string
	ViewBulkCommands            string
	FilterList                  string
	OpenInBrowser               string
	SortContainersByState       string

	LogsTitle                   string
	ConfigTitle                 string
	EnvTitle                    string
	DockerComposeConfigTitle    string
	StatsTitle                  string
	CreditsTitle                string
	ContainerConfigTitle        string
	ContainerEnvTitle           string
	NothingToDisplay            string
	NoContainerForService       string
	CannotDisplayEnvVariables   string
	CannotManageNonLocalService string

	No  string
	Yes string

	LcNextScreenMode string
	LcPrevScreenMode string
	FilterPrompt     string

	FocusProjects   string
	FocusServices   string
	FocusContainers string
	FocusImages     string
	FocusVolumes    string
	FocusNetworks   string

	FocusSwarmSecrets string
	FocusSwarmConfigs string

	LocalTitle                    string
	SwitchLocalSwarm              string
	SwarmSelectStack              string
	SwarmConnecting               string
	SwarmConnectingPanel          string
	SwarmTitle                    string
	SwarmStacksTitle              string
	SwarmTasksTitle               string
	SwarmNodesTitle               string
	SwarmSecretsTitle             string
	SwarmConfigsTitle             string
	SwarmContentTitle             string
	SwarmNotAManager              string
	SwarmNoServices               string
	SwarmNoStacks                 string
	SwarmNoTasks                  string
	SwarmNoNodes                  string
	SwarmNoSecrets                string
	SwarmNoConfigs                string
	SwarmCannotScaleNonReplicated string
	SwarmStopService              string
	SwarmStartService             string
	SwarmScaleService             string
	SwarmRestartService           string
	SwarmUpdateImage              string
	SwarmRollbackService          string
	SwarmRemoveService            string
	SwarmConfirmStopService       string
	SwarmConfirmRestartService    string
	SwarmConfirmRollbackService   string
	SwarmConfirmRemoveService     string
	SwarmScalePrompt              string
	SwarmUpdateImagePrompt        string
	SwarmUpdatingStatus           string
	SwarmRollingBackStatus        string
	SwarmCreatingStatus           string
	SwarmDeployStack              string
	SwarmStopStack                string
	SwarmStartStack               string
	SwarmRemoveStack              string
	SwarmConfirmStopStack         string
	SwarmConfirmRemove            string
	SwarmConfirmRemoveStack       string
	SwarmDeployFilePrompt         string
	SwarmDeployNamePrompt         string
	SwarmToggleTaskHistory        string
	SwarmSetAvailability          string
	SwarmAvailabilityActive       string
	SwarmAvailabilityPause        string
	SwarmAvailabilityDrain        string
	SwarmChangeRole               string
	SwarmPromote                  string
	SwarmDemote                   string
	SwarmRemoveNode               string
	SwarmForceRemove              string
	SwarmCreateNetwork            string
	SwarmCreateSecret             string
	SwarmCreateConfig             string
	SwarmCreateFromValue          string
	SwarmCreateFromFile           string
	SwarmNamePrompt               string
	SwarmValuePrompt              string
	SwarmFilePathPrompt           string
	SwarmAttachToService          string
	SwarmDetachFromService        string
	SwarmAttachTo                 string
	SwarmDetachFrom               string
	SwarmSecretTargetPrompt       string
	SwarmConfigTargetPrompt       string
}

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

		NoViewMachingNewLineFocusedSwitchStatement: "No view matching newLineFocused switch statement",

		ErrorOccurred:                     "An error occurred! Please create an issue at https://github.com/jesseduffield/lazydocker/issues",
		ConnectionFailed:                  "connection to docker client failed. You may need to restart the docker client",
		UnattachableContainerError:        "Container does not support attaching. You must either run the service with the '-it' flag or use `stdin_open: true, tty: true` in the docker-compose.yml file",
		WaitingForContainerInfo:           "Cannot proceed until docker gives us more information about the container. Please retry in a few moments.",
		CannotAttachStoppedContainerError: "You cannot attach to a stopped container, you need to start it first (which you can actually do with the 'r' key) (yes I'm too lazy to do this automatically for you) (pretty cool that I get to communicate one-on-one with you in the form of an error message though)",
		CannotAccessDockerSocketError:     "Can't access docker socket at: unix:///var/run/docker.sock\nRun lazydocker as root or read https://docs.docker.com/install/linux/linux-postinstall/",
		CannotKillChildError:              "Waited three seconds for child process to stop. There may be an orphan process that continues to run on your system.",

		Donate:  "Donate",
		Confirm: "Confirm",

		Return:                      "return",
		FocusMain:                   "focus main panel",
		LcFilter:                    "filter list",
		Navigate:                    "navigate",
		Execute:                     "execute",
		Close:                       "close",
		Quit:                        "quit",
		Menu:                        "menu",
		MenuTitle:                   "Menu",
		Scroll:                      "scroll",
		OpenConfig:                  "open lazydocker config",
		EditConfig:                  "edit lazydocker config",
		Cancel:                      "cancel",
		Remove:                      "remove",
		HideStopped:                 "hide/show stopped containers",
		ForceRemove:                 "force remove",
		RemoveWithVolumes:           "remove with volumes",
		RemoveService:               "remove containers",
		UpService:                   "up service",
		Stop:                        "stop",
		Pause:                       "pause",
		Restart:                     "restart",
		Down:                        "down project",
		DownWithVolumes:             "down project with volumes",
		Start:                       "start",
		Rebuild:                     "rebuild",
		Recreate:                    "recreate",
		PreviousContext:             "previous tab",
		NextContext:                 "next tab",
		Attach:                      "attach",
		ViewLogs:                    "view logs",
		UpProject:                   "up project",
		DownProject:                 "down project",
		RemoveImage:                 "remove image",
		RemoveVolume:                "remove volume",
		RemoveNetwork:               "remove network",
		RemoveWithoutPrune:          "remove without deleting untagged parents",
		RemoveWithoutPruneWithForce: "remove (forced) without deleting untagged parents",
		RemoveWithForce:             "remove (forced)",
		PruneContainers:             "prune exited containers",
		PruneVolumes:                "prune unused volumes",
		PruneNetworks:               "prune unused networks",
		PruneImages:                 "prune unused images",
		StopAllContainers:           "stop all containers",
		RemoveAllContainers:         "remove all containers (forced)",
		ViewRestartOptions:          "view restart options",
		ExecShell:                   "exec shell",
		RunCustomCommand:            "run predefined custom command",
		ViewBulkCommands:            "view bulk commands",
		FilterList:                  "filter list",
		OpenInBrowser:               "open in browser (first port is http)",
		SortContainersByState:       "sort containers by state",

		GlobalTitle:                 "Global",
		MainTitle:                   "Main",
		ProjectTitle:                "Project",
		ServicesTitle:               "Services",
		ContainersTitle:             "Containers",
		StandaloneContainersTitle:   "Standalone Containers",
		ImagesTitle:                 "Images",
		VolumesTitle:                "Volumes",
		NetworksTitle:               "Networks",
		CustomCommandTitle:          "Custom Command:",
		BulkCommandTitle:            "Bulk Command:",
		ErrorTitle:                  "Error",
		LogsTitle:                   "Logs",
		ConfigTitle:                 "Config",
		EnvTitle:                    "Env",
		DockerComposeConfigTitle:    "Docker-Compose Config",
		TopTitle:                    "Top",
		StatsTitle:                  "Stats",
		CreditsTitle:                "About",
		ContainerConfigTitle:        "Container Config",
		ContainerEnvTitle:           "Container Env",
		NothingToDisplay:            "Nothing to display",
		NoContainerForService:       "No logs to show; service is not associated with a container",
		CannotDisplayEnvVariables:   "Something went wrong while displaying environment variables",
		CannotManageNonLocalService: "This service belongs to a different compose project. Run lazydocker from that project's directory to manage it.",

		NoContainers: "No containers",
		NoContainer:  "No container",
		NoImages:     "No images",
		NoVolumes:    "No volumes",
		NoNetworks:   "No networks",
		NoServices:   "No services",

		ConfirmQuit:                 "Are you sure you want to quit?",
		ConfirmUpProject:            "Are you sure you want to 'up' your docker compose project?",
		MustForceToRemoveContainer:  "You cannot remove a running container unless you force it. Do you want to force it?",
		NotEnoughSpace:              "Not enough space to render panels",
		ConfirmPruneImages:          "Are you sure you want to prune all unused images?",
		ConfirmPruneContainers:      "Are you sure you want to prune all stopped containers?",
		ConfirmStopContainers:       "Are you sure you want to stop all containers?",
		ConfirmRemoveContainers:     "Are you sure you want to remove all containers?",
		ConfirmPruneVolumes:         "Are you sure you want to prune all unused volumes?",
		ConfirmPruneNetworks:        "Are you sure you want to prune all unused networks?",
		StopService:                 "Are you sure you want to stop this service's containers?",
		StopContainer:               "Are you sure you want to stop this container?",
		PressEnterToReturn:          "Press enter to return to lazydocker (this prompt can be disabled in your config by setting `gui.returnImmediately: true`)",
		DetachFromContainerShortCut: "By default, to detach from the container press ctrl-p then ctrl-q",

		No:  "no",
		Yes: "yes",

		LcNextScreenMode: "next screen mode (normal/half/fullscreen)",
		LcPrevScreenMode: "prev screen mode",
		FilterPrompt:     "filter",

		FocusProjects:   "focus projects panel",
		FocusServices:   "focus services panel",
		FocusContainers: "focus containers panel",
		FocusImages:     "focus images panel",
		FocusVolumes:    "focus volumes panel",
		FocusNetworks:   "focus networks panel",

		FocusSwarmSecrets: "focus secrets panel (swarm)",
		FocusSwarmConfigs: "focus configs panel (swarm)",

		LocalTitle:                    "Local",
		SwitchLocalSwarm:              "switch between local and swarms",
		SwarmSelectStack:              "Select a stack first: this action does not apply to all services at once.",
		SwarmConnecting:               "connecting to swarm at %s",
		SwarmConnectingPanel:          "connecting...",
		SwarmTitle:                    "Swarm",
		SwarmStacksTitle:              "Stacks",
		SwarmTasksTitle:               "Tasks",
		SwarmNodesTitle:               "Nodes",
		SwarmSecretsTitle:             "Secrets",
		SwarmConfigsTitle:             "Configs",
		SwarmContentTitle:             "Content",
		SwarmNotAManager:              "%s is not a swarm manager",
		SwarmNoServices:               "No swarm services",
		SwarmNoStacks:                 "No stacks",
		SwarmNoTasks:                  "No tasks",
		SwarmNoNodes:                  "No nodes",
		SwarmNoSecrets:                "No secrets",
		SwarmNoConfigs:                "No configs",
		SwarmCannotScaleNonReplicated: "cannot scale service %s: only replicated services can be scaled (mode: %s)",
		SwarmStopService:              "stop (scale to 0)",
		SwarmStartService:             "start (restore replicas)",
		SwarmScaleService:             "scale",
		SwarmRestartService:           "restart (rolling, force update)",
		SwarmUpdateImage:              "update image",
		SwarmRollbackService:          "rollback to previous spec",
		SwarmRemoveService:            "remove service",
		SwarmConfirmStopService:       "Are you sure you want to stop %s? It will be scaled to 0 replicas, and starting it again will restore the current replica count.",
		SwarmConfirmRestartService:    "Are you sure you want to restart all tasks of %s? This performs a rolling update according to the service's update config.",
		SwarmConfirmRollbackService:   "Are you sure you want to roll back %s to its previous spec?",
		SwarmConfirmRemoveService:     "Are you sure you want to remove service %s?",
		SwarmScalePrompt:              "Number of replicas for %s",
		SwarmUpdateImagePrompt:        "New image for %s",
		SwarmUpdatingStatus:           "updating",
		SwarmRollingBackStatus:        "rolling back",
		SwarmCreatingStatus:           "creating",
		SwarmDeployStack:              "deploy/update stack from compose file",
		SwarmStopStack:                "stop stack (scale all services to 0)",
		SwarmStartStack:               "start stack (restore replicas)",
		SwarmRemoveStack:              "remove stack",
		SwarmConfirmStopStack:         "Are you sure you want to stop all services of stack %s (scale them to 0)?",
		SwarmConfirmRemove:            "Are you sure you want to remove %s?",
		SwarmConfirmRemoveStack:       "Are you sure you want to remove stack %s? This removes its services, networks, secrets and configs.",
		SwarmDeployFilePrompt:         "Compose file(s) to deploy (comma separated)",
		SwarmDeployNamePrompt:         "Stack name",
		SwarmToggleTaskHistory:        "show/hide task history",
		SwarmSetAvailability:          "set availability",
		SwarmAvailabilityActive:       "active (schedule tasks)",
		SwarmAvailabilityPause:        "pause (keep tasks, schedule no new ones)",
		SwarmAvailabilityDrain:        "drain (move tasks to other nodes)",
		SwarmChangeRole:               "promote/demote",
		SwarmPromote:                  "promote to manager",
		SwarmDemote:                   "demote to worker",
		SwarmRemoveNode:               "remove node",
		SwarmForceRemove:              "force remove",
		SwarmCreateNetwork:            "create overlay network",
		SwarmCreateSecret:             "create secret",
		SwarmCreateConfig:             "create config",
		SwarmCreateFromValue:          "from value",
		SwarmCreateFromFile:           "from file",
		SwarmNamePrompt:               "Name",
		SwarmValuePrompt:              "Value",
		SwarmFilePathPrompt:           "Path of the file",
		SwarmAttachToService:          "attach to service",
		SwarmDetachFromService:        "detach from service",
		SwarmAttachTo:                 "Attach %s to",
		SwarmDetachFrom:               "Detach %s from",
		SwarmSecretTargetPrompt:       "File name in /run/secrets/",
		SwarmConfigTargetPrompt:       "Path in the container",
	}
}
