package commands

type Project struct {
	Name string
	// IsProfile marks a row in the project panel as a docker-compose profile
	// pseudo-project. Profiles live within the local compose project; selecting
	// one filters the services panel to that profile and routes Up/Down/Restart
	// through the --profile flag.
	IsProfile bool
}
