package entities

// QuadletInstallOptions contains options to the `podman quadlet install` command
type QuadletInstallOptions struct {
	// Whether to reload systemd after installation is completed
	ReloadSystemd bool
	// Replace the installation even if the quadlet already exists
	Replace bool
}

// QuadletInstallReport contains the output of the `quadlet install` command
// including what files were successfully installed (and to where), and what
// files errored (and why).
type QuadletInstallReport struct {
	// InstalledQuadlets is a map of the path of the quadlet file to be installed
	// to where it was installed to.
	InstalledQuadlets map[string]string
	// QuadletErrors is a map of the path of the quadlet file to be installed
	// to the error that occurred attempting to install it
	QuadletErrors map[string]error
}

// QuadletListOptions contains options to the `podman quadlet list` command.
type QuadletListOptions struct {
	// Filters contains filters that will limit what Quadlets are displayed
	Filters []string
}

// A ListQuadlet is a single Quadlet to be listed by `podman quadlet list`
type ListQuadlet struct {
	// Name is the name of the Quadlet file
	Name string
	// UnitName is the name of the systemd unit created from the Quadlet.
	// May be empty if systemd has not be reloaded since it was installed.
	UnitName string
	// Path to the Quadlet on disk
	Path string
	// What is the status of the Quadlet - if present in systemd, will be a
	// systemd status, else will mention if the Quadlet has syntax errors
	Status string
	// If multiple quadlets were installed together they will belong
	// to common App.
	App string
}

// QuadletRemoveOptions contains parameters for removing Quadlets
type QuadletRemoveOptions struct {
	// Force indicates that running quadlets should be removed as well
	Force bool
	// All indicates all quadlets should be removed
	All bool
	// Ignore indicates that missing quadlets should not cause an error
	Ignore bool
	// ReloadSystemd determines whether systemd will be reloaded after the Quadlet is removed.
	ReloadSystemd bool
}

// QuadletRemoveReport contains the results of an operation to remove obe or more quadlets
type QuadletRemoveReport struct {
	// Removed is a list of quadlets that were successfully removed
	Removed []string
	// Errors is a map of Quadlet name to error that occurred during removal.
	Errors map[string]error
}
