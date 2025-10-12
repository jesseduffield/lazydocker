package config

// KeybindingConfig contains all keybinding configurations for lazydocker
type KeybindingConfig struct {
	Universal  KeybindingUniversalConfig  `yaml:"universal"`
	Containers KeybindingContainersConfig `yaml:"containers"`
	Services   KeybindingServicesConfig   `yaml:"services"`
	Images     KeybindingImagesConfig     `yaml:"images"`
	Volumes    KeybindingVolumesConfig    `yaml:"volumes"`
	Networks   KeybindingNetworksConfig   `yaml:"networks"`
	Project    KeybindingProjectConfig    `yaml:"project"`
	Main       KeybindingMainConfig       `yaml:"main"`
	Menu       KeybindingMenuConfig       `yaml:"menu"`
	Filter     KeybindingFilterConfig     `yaml:"filter"`
}

// KeybindingUniversalConfig contains keybindings that are available globally
type KeybindingUniversalConfig struct {
	Quit               string `yaml:"quit,omitempty"`
	QuitAlt            string `yaml:"quitAlt,omitempty"`
	Return             string `yaml:"return,omitempty"`
	ScrollUpMain       string `yaml:"scrollUpMain,omitempty"`
	ScrollDownMain     string `yaml:"scrollDownMain,omitempty"`
	ScrollUpMainAlt1   string `yaml:"scrollUpMainAlt1,omitempty"`
	ScrollDownMainAlt1 string `yaml:"scrollDownMainAlt1,omitempty"`
	ScrollUpMainAlt2   string `yaml:"scrollUpMainAlt2,omitempty"`
	ScrollDownMainAlt2 string `yaml:"scrollDownMainAlt2,omitempty"`
	ScrollLeftMain     string `yaml:"scrollLeftMain,omitempty"`
	ScrollRightMain    string `yaml:"scrollRightMain,omitempty"`
	JumpToTopMain      string `yaml:"jumpToTopMain,omitempty"`
	AutoScrollMain     string `yaml:"autoScrollMain,omitempty"`
	OpenMenu           string `yaml:"openMenu,omitempty"`
	OpenMenuAlt        string `yaml:"openMenuAlt,omitempty"`
	CustomCommand      string `yaml:"customCommand,omitempty"`
	NextScreenMode     string `yaml:"nextScreenMode,omitempty"`
	PrevScreenMode     string `yaml:"prevScreenMode,omitempty"`
	PrevItem           string `yaml:"prevItem,omitempty"`
	NextItem           string `yaml:"nextItem,omitempty"`
	PrevItemAlt        string `yaml:"prevItemAlt,omitempty"`
	NextItemAlt        string `yaml:"nextItemAlt,omitempty"`
	PrevPanel          string `yaml:"prevPanel,omitempty"`
	NextPanel          string `yaml:"nextPanel,omitempty"`
	PrevPanelAlt       string `yaml:"prevPanelAlt,omitempty"`
	NextPanelAlt       string `yaml:"nextPanelAlt,omitempty"`
	TogglePanel        string `yaml:"togglePanel,omitempty"`
	TogglePanelAlt     string `yaml:"togglePanelAlt,omitempty"`
	EnterMain          string `yaml:"enterMain,omitempty"`
	PrevMainTab        string `yaml:"prevMainTab,omitempty"`
	NextMainTab        string `yaml:"nextMainTab,omitempty"`
	Filter             string `yaml:"filter,omitempty"`
	GoToProject        string `yaml:"goToProject,omitempty"`
	GoToServices       string `yaml:"goToServices,omitempty"`
	GoToContainers     string `yaml:"goToContainers,omitempty"`
	GoToImages         string `yaml:"goToImages,omitempty"`
	GoToVolumes        string `yaml:"goToVolumes,omitempty"`
	GoToNetworks       string `yaml:"goToNetworks,omitempty"`
}

// KeybindingContainersConfig contains keybindings for the containers panel
type KeybindingContainersConfig struct {
	Remove        string `yaml:"remove,omitempty"`
	HideStopped   string `yaml:"hideStopped,omitempty"`
	Pause         string `yaml:"pause,omitempty"`
	Stop          string `yaml:"stop,omitempty"`
	Restart       string `yaml:"restart,omitempty"`
	Attach        string `yaml:"attach,omitempty"`
	ViewLogs      string `yaml:"viewLogs,omitempty"`
	ExecShell     string `yaml:"execShell,omitempty"`
	CustomCommand string `yaml:"customCommand,omitempty"`
	BulkCommand   string `yaml:"bulkCommand,omitempty"`
	OpenInBrowser string `yaml:"openInBrowser,omitempty"`
}

// KeybindingServicesConfig contains keybindings for the services panel
type KeybindingServicesConfig struct {
	Up            string `yaml:"up,omitempty"`
	Remove        string `yaml:"remove,omitempty"`
	Stop          string `yaml:"stop,omitempty"`
	Pause         string `yaml:"pause,omitempty"`
	Restart       string `yaml:"restart,omitempty"`
	Start         string `yaml:"start,omitempty"`
	Attach        string `yaml:"attach,omitempty"`
	ViewLogs      string `yaml:"viewLogs,omitempty"`
	UpProject     string `yaml:"upProject,omitempty"`
	DownProject   string `yaml:"downProject,omitempty"`
	RestartMenu   string `yaml:"restartMenu,omitempty"`
	CustomCommand string `yaml:"customCommand,omitempty"`
	BulkCommand   string `yaml:"bulkCommand,omitempty"`
	ExecShell     string `yaml:"execShell,omitempty"`
	OpenInBrowser string `yaml:"openInBrowser,omitempty"`
}

// KeybindingImagesConfig contains keybindings for the images panel
type KeybindingImagesConfig struct {
	CustomCommand string `yaml:"customCommand,omitempty"`
	Remove        string `yaml:"remove,omitempty"`
	BulkCommand   string `yaml:"bulkCommand,omitempty"`
}

// KeybindingVolumesConfig contains keybindings for the volumes panel
type KeybindingVolumesConfig struct {
	CustomCommand string `yaml:"customCommand,omitempty"`
	Remove        string `yaml:"remove,omitempty"`
	BulkCommand   string `yaml:"bulkCommand,omitempty"`
}

// KeybindingNetworksConfig contains keybindings for the networks panel
type KeybindingNetworksConfig struct {
	CustomCommand string `yaml:"customCommand,omitempty"`
	Remove        string `yaml:"remove,omitempty"`
	BulkCommand   string `yaml:"bulkCommand,omitempty"`
}

// KeybindingProjectConfig contains keybindings for the project panel
type KeybindingProjectConfig struct {
	EditConfig string `yaml:"editConfig,omitempty"`
	OpenConfig string `yaml:"openConfig,omitempty"`
	ViewLogs   string `yaml:"viewLogs,omitempty"`
}

// KeybindingMainConfig contains keybindings for the main panel
type KeybindingMainConfig struct {
	Return         string `yaml:"return,omitempty"`
	ScrollLeft     string `yaml:"scrollLeft,omitempty"`
	ScrollRight    string `yaml:"scrollRight,omitempty"`
	ScrollLeftAlt  string `yaml:"scrollLeftAlt,omitempty"`
	ScrollRightAlt string `yaml:"scrollRightAlt,omitempty"`
}

// KeybindingMenuConfig contains keybindings for menus
type KeybindingMenuConfig struct {
	Close     string `yaml:"close,omitempty"`
	CloseAlt  string `yaml:"closeAlt,omitempty"`
	Select    string `yaml:"select,omitempty"`
	SelectAlt string `yaml:"selectAlt,omitempty"`
	Confirm   string `yaml:"confirm,omitempty"`
}

// KeybindingFilterConfig contains keybindings for the filter prompt
type KeybindingFilterConfig struct {
	Confirm string `yaml:"confirm,omitempty"`
	Escape  string `yaml:"escape,omitempty"`
}

// GetDefaultKeybindings returns the default keybinding configuration
func GetDefaultKeybindings() KeybindingConfig {
	return KeybindingConfig{
		Universal: KeybindingUniversalConfig{
			Quit:               "q",
			QuitAlt:            "<c-c>",
			Return:             "<esc>",
			ScrollUpMain:       "<pgup>",
			ScrollDownMain:     "<pgdown>",
			ScrollUpMainAlt1:   "<c-u>",
			ScrollDownMainAlt1: "<c-d>",
			ScrollUpMainAlt2:   "K",
			ScrollDownMainAlt2: "J",
			ScrollLeftMain:     "H",
			ScrollRightMain:    "L",
			JumpToTopMain:      "<home>",
			AutoScrollMain:     "<end>",
			OpenMenu:           "x",
			OpenMenuAlt:        "?",
			CustomCommand:      "X",
			NextScreenMode:     "+",
			PrevScreenMode:     "_",
			PrevItem:           "<up>",
			NextItem:           "<down>",
			PrevItemAlt:        "k",
			NextItemAlt:        "j",
			PrevPanel:          "<left>",
			NextPanel:          "<right>",
			PrevPanelAlt:       "h",
			NextPanelAlt:       "l",
			TogglePanel:        "<tab>",
			TogglePanelAlt:     "<backtab>",
			EnterMain:          "<enter>",
			PrevMainTab:        "[",
			NextMainTab:        "]",
			Filter:             "/",
			GoToProject:        "1",
			GoToServices:       "2",
			GoToContainers:     "3",
			GoToImages:         "4",
			GoToVolumes:        "5",
			GoToNetworks:       "6",
		},
		Containers: KeybindingContainersConfig{
			Remove:        "d",
			HideStopped:   "e",
			Pause:         "p",
			Stop:          "s",
			Restart:       "r",
			Attach:        "a",
			ViewLogs:      "m",
			ExecShell:     "E",
			CustomCommand: "c",
			BulkCommand:   "b",
			OpenInBrowser: "w",
		},
		Services: KeybindingServicesConfig{
			Up:            "u",
			Remove:        "d",
			Stop:          "s",
			Pause:         "p",
			Restart:       "r",
			Start:         "S",
			Attach:        "a",
			ViewLogs:      "m",
			UpProject:     "U",
			DownProject:   "D",
			RestartMenu:   "R",
			CustomCommand: "c",
			BulkCommand:   "b",
			ExecShell:     "E",
			OpenInBrowser: "w",
		},
		Images: KeybindingImagesConfig{
			CustomCommand: "c",
			Remove:        "d",
			BulkCommand:   "b",
		},
		Volumes: KeybindingVolumesConfig{
			CustomCommand: "c",
			Remove:        "d",
			BulkCommand:   "b",
		},
		Networks: KeybindingNetworksConfig{
			CustomCommand: "c",
			Remove:        "d",
			BulkCommand:   "b",
		},
		Project: KeybindingProjectConfig{
			EditConfig: "e",
			OpenConfig: "o",
			ViewLogs:   "m",
		},
		Main: KeybindingMainConfig{
			Return:         "<esc>",
			ScrollLeft:     "<left>",
			ScrollRight:    "<right>",
			ScrollLeftAlt:  "h",
			ScrollRightAlt: "l",
		},
		Menu: KeybindingMenuConfig{
			Close:     "<esc>",
			CloseAlt:  "q",
			Select:    " ",
			SelectAlt: "y",
			Confirm:   "<enter>",
		},
		Filter: KeybindingFilterConfig{
			Confirm: "<enter>",
			Escape:  "<esc>",
		},
	}
}
