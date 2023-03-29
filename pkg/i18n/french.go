package i18n

func frenchSet() TranslationSet {
	return TranslationSet{
		PruningStatus:              "destruction",
		RemovingStatus:             "supression",
		RestartingStatus:           "redémarrage",
		StartingStatus:             "démarrage",
		StoppingStatus:             "arrêt",
		PausingStatus:              "mise en pause",
		RunningCustomCommandStatus: "execution de la commande personalisée",
		RunningBulkCommandStatus:   "execution de la commande groupée",

		NoViewMachingNewLineFocusedSwitchStatement: "No view matching newLineFocused switch statement",

		ErrorOccurred:              "Une erreur s'est produite! Veuillez créer un rapport d'erreur sur https://github.com/jesseduffield/lazydocker/issues",
		ConnectionFailed:           "Erreur lors de la connexion au client docker. Essayez de redémarrer votre client docker",
		UnattachableContainerError: "Le conteneur ne peut pas être attaché. Vous devez executer le service avec le drapeau 'it' ou bien utiliser `stdin_open: true, tty: true` dans votre fichier docker-compose.yaml",
		WaitingForContainerInfo:    "Le processus ne peut pas continuer avant que docker donne plus d'informations. Veuillez réessayer dans quelques instants.",

		CannotAttachStoppedContainerError: "Vous ne pouvez pas vous attacher à un conteneur en arrêt, vous devez le démarrer avant (ce que vous pouvez faire avec la touche 'r') (oui, je suis trop paresseux pour le faire automatiquement pour vous) (plutôt cool que je puisse communiquer en tête-à-tête avec vous au travers d'un message d'erreur)",
		CannotAccessDockerSocketError:     "Impossible d'accèder à la socket docker à: unix:///var/run/docker.sock\nLancez lazydocker en tant que root ou alors lisez https://docs.docker.com/install/linux/linux-postinstall/",
		CannotKillChildError:              "Trois secondes ont étés attendu pour l'arrêt des processus fils. Il y a possiblement un processus orphelin qui continue à tourner sur votre systeme.",

		Donate:  "Donner",
		Confirm: "Confirmer",

		Return:                      "retour",
		FocusMain:                   "focus paneau principal",
		Navigate:                    "naviguer",
		Execute:                     "executer",
		Close:                       "fermer",
		Menu:                        "menu",
		MenuTitle:                   "Menu",
		Scroll:                      "faire défiler",
		OpenConfig:                  "ouvrire la configuration lazydocker",
		EditConfig:                  "modifier la configuration lazydocker",
		Cancel:                      "annuler",
		Remove:                      "supprimer",
		HideStopped:                 "cacher/montrer les conteneurs arrêtés",
		ForceRemove:                 "forcer la supression",
		RemoveWithVolumes:           "supprimer avec les volumes",
		RemoveService:               "supprimer les conteneurs",
		Stop:                        "arrêter",
		Pause:                       "pause",
		Restart:                     "redémarrer",
		Start:                       "démarrer",
		Rebuild:                     "reconstruire",
		Recreate:                    "recréer",
		PreviousContext:             "onglet précédent",
		NextContext:                 "onglet suivant",
		Attach:                      "attacher",
		ViewLogs:                    "voir les enregistrements",
		RemoveImage:                 "supprimer l'image",
		RemoveVolume:                "supprimer le volume",
		RemoveNetwork:               "supprimer le réseau",
		RemoveWithoutPrune:          "supprimer sans effacer les parents non étiqueté",
		RemoveWithoutPruneWithForce: "supprimer (forcer) sans effacer les parents non étiqueté",
		RemoveWithForce:             "supprimer (forcer)",
		PruneContainers:             "détruire les conteneurs arrêtes",
		PruneVolumes:                "détruire les volumes non utilisés",
		PruneNetworks:               "détruire les réseaux non utilisés",
		PruneImages:                 "détruire les images non utilisés",
		StopAllContainers:           "arrêter tous les conteneurs",
		RemoveAllContainers:         "supprimer tous les conteneurs (forcer)",
		ViewRestartOptions:          "voir les options de redémarrage",
		ExecShell:                   "executer le shell",
		RunCustomCommand:            "executer une commande prédéfinie",
		ViewBulkCommands:            "voir les commandes groupés",
		OpenInBrowser:               "ouvrir dans le navgateur (le premier port est http)",
		SortContainersByState:       "ordonner les conteneurs par état",

		GlobalTitle:               "Global",
		MainTitle:                 "Principal",
		ProjectTitle:              "Projet",
		ServicesTitle:             "Services",
		ContainersTitle:           "Conteneurs",
		StandaloneContainersTitle: "Conteneurs Autonomes",
		ImagesTitle:               "Images",
		VolumesTitle:              "Volumes",
		NetworksTitle:             "Réseaux",
		CustomCommandTitle:        "Commande personalisé:",
		BulkCommandTitle:          "Commande groupée:",
		ErrorTitle:                "Erreur",
		LogsTitle:                 "Enregistrements",
		ConfigTitle:               "Config",
		EnvTitle:                  "Env",
		DockerComposeConfigTitle:  "Config Docker-Compose",
		TopTitle:                  "Top",
		StatsTitle:                "Statistiques",
		CreditsTitle:              "A propos",
		ContainerConfigTitle:      "Config Conteneur",
		ContainerEnvTitle:         "Env Conteneur",
		NothingToDisplay:          "Rien à afficher",
		CannotDisplayEnvVariables: "Quelque chose à échoué lors de l'affichage des variables d'environnement",

		NoContainers: "Aucun conteneurs",
		NoContainer:  "Aucun conteneur",
		NoImages:     "Aucune images",
		NoVolumes:    "Aucun volumes",
		NoNetworks:   "Aucun réseaux",

		ConfirmQuit:                "Êtes vous certain de vouloir quitter?",
		MustForceToRemoveContainer: "Vous ne pouvez pas supprimer un conteneur qui tourne sans le forcer. Voulez-vous le forcer ?",
		NotEnoughSpace:             "Manque d'espace pour afficher les differents panneaux",
		ConfirmPruneImages:         "Êtes-vous certain de vouloir détruire toutes les images non utilisés ?",
		ConfirmPruneContainers:     "Êtes-vous certain de vouloir détruire tous les conteneurs arrêtés ?",
		ConfirmStopContainers:      "Êtes-vous certain de vouloir arrêter tous les conteneurs ?",
		ConfirmRemoveContainers:    "Êtes-vous certain de vouloir supprimer tous les conteneurs ?",
		ConfirmPruneVolumes:        "Êtes-vous certain de vouloir détruire tous les volumes non utilisés ?",
		ConfirmPruneNetworks:       "Êtes-vous certain de vouloir détruire tous les réseaux non utilisés ?",
		StopService:                "Êtes-vous certain de vouloir arrêter le conteneur de ce service ?",
		StopContainer:              "Êtes-vous certain de vouloir arrêter ce conteneur ?",
		PressEnterToReturn:         "Appuiez sur Enter pour revenir à lazydocker (ce message peut être désactivé dans vos configurations en appliquant `gui.returnImmediately: true`)",

		No:  "non",
		Yes: "oui",
	}
}
