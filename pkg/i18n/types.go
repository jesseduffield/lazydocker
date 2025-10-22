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

	LogsTitle                 string
	ConfigTitle               string
	EnvTitle                  string
	DockerComposeConfigTitle  string
	StatsTitle                string
	CreditsTitle              string
	ContainerConfigTitle      string
	ContainerEnvTitle         string
	NothingToDisplay          string
	NoContainerForService     string
	CannotDisplayEnvVariables string

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
}

// LanguageMetadata contains metadata about a language
type LanguageMetadata struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// TranslationFile represents the structure of a translation JSON file
type TranslationFile struct {
	Code         string            `json:"code"`
	Name         string            `json:"name"`
	Translations map[string]string `json:"translations"`
}
